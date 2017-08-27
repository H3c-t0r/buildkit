package bootstrap

import (
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"github.com/buildkite/agent/process"
	"github.com/buildkite/agent/shell"
)

// process is a simpler version of process.Process specifically for the bootstrap
// to use to create sub-processes
type subprocess struct {
	// The command to run in the process
	Command *shell.Command

	// Where the STDOUT + STDERR of a command will be written to
	Writer io.Writer

	// Whether or not the command should be run in a PTY
	PTY bool

	// The exit status of the process
	exitStatus int
}

func (p *subprocess) Run() error {
	// Windows has a hard time finding files that are located in folders
	// that you've added dynmically to PATH, so we'll use `AbsolutePath`
	// method (that looks for files in PATH) and use the path from that
	// instead.
	absolutePathToCommand, err := p.Command.AbsolutePath()
	if err != nil {
		return err
	}

	// fmt.Printf("The command dir is: %s\n", p.Command.Dir)
	// fmt.Printf("The absolute path to the command is: %s\n", absolutePathToCommand)
	// fmt.Printf("The arguments are: %s\n", p.Command.Args)

	cmd := exec.Command(absolutePathToCommand, p.Command.Args...)

	if p.Command.Env != nil {
		cmd.Env = p.Command.Env.ToSlice()
	}

	if p.Command.Dir != "" {
		cmd.Dir = p.Command.Dir
	}

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt,
		syscall.SIGHUP,
		syscall.SIGTERM,
		syscall.SIGINT,
		syscall.SIGQUIT)

	go func() {
		// forward signals to the process
		for sig := range signals {
			if err = signalProcess(cmd, sig); err != nil {
				log.Println("Error passing signal to child process", err)
			}
		}
	}()
	defer signal.Stop(signals)

	if p.PTY {
		pty, err := process.StartPTY(cmd)
		if err != nil {
			return fmt.Errorf("Failed to start PTY (%v)", err)
		}

		// Copy the pty to our buffer. This will block until it EOF's
		// or something breaks.
		_, err = io.Copy(p.Writer, pty)
		if e, ok := err.(*os.PathError); ok && e.Err == syscall.EIO {
			// We can safely ignore this error, because it's just
			// the PTY telling us that it closed successfully.
			// See:
			// https://github.com/buildkite/agent/pull/34#issuecomment-46080419
			err = nil
		}
	} else {
		cmd.Stdout = p.Writer
		cmd.Stderr = p.Writer
		cmd.Stdin = nil

		err := cmd.Start()
		if err != nil {
			return err
		}
	}

	// Wait for the command to finish
	waitResult := cmd.Wait()

	// Get the exit status
	// https://github.com/hnakamur/commango/blob/fe42b1cf82bf536ce7e24dceaef6656002e03743/os/executil/executil.go#L29
	if waitResult != nil {
		if err, ok := waitResult.(*exec.ExitError); ok {
			if s, ok := err.Sys().(syscall.WaitStatus); ok {
				p.exitStatus = s.ExitStatus()
			} else {
				return errors.New("Unimplemented for system where exec.ExitError.Sys() is not syscall.WaitStatus.")
			}
		}
	} else {
		p.exitStatus = 0
	}

	return nil
}

func (p *subprocess) ExitStatus() int {
	return p.exitStatus
}
