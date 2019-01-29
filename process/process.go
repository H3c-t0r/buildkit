package process

import (
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/buildkite/agent/logger"
)

type ProcessConfig struct {
	PTY       bool
	Timestamp bool
	Script    []string
	Env       []string

	// Handler is called with each line of output
	Handler func(string)
}

type Process struct {
	// Outputs available after the process starts
	Pid        int
	ExitStatus string

	conf          ProcessConfig
	logger        *logger.Logger
	command       *exec.Cmd
	mu            sync.Mutex
	started, done chan struct{}
}

func NewProcess(l *logger.Logger, c ProcessConfig) *Process {
	return &Process{
		logger: l,
		conf:   c,
	}
}

// Start executes the command and blocks until it finishes
func (p *Process) Start() error {
	if p.command != nil {
		return fmt.Errorf("Process is already running")
	}

	// Create a command
	p.command = exec.Command(p.conf.Script[0], p.conf.Script[1:]...)

	// Setup the process to create a process group if supported
	// See https://github.com/kr/pty/issues/35 for context
	if !p.conf.PTY {
		SetupProcessGroup(p.command)
	}

	// Create channels for signalling started and done
	p.mu.Lock()
	if p.done == nil {
		p.done = make(chan struct{})
	}
	if p.started == nil {
		p.started = make(chan struct{})
	}
	p.mu.Unlock()

	// Copy the current processes ENV and merge in the new ones. We do this
	// so the sub process gets PATH and stuff. We merge our path in over
	// the top of the current one so the ENV from Buildkite and the agent
	// take precedence over the agent
	currentEnv := os.Environ()
	p.command.Env = append(currentEnv, p.conf.Env...)

	var waitGroup sync.WaitGroup

	lineReaderPipe, lineWriterPipe := io.Pipe()

	// Toggle between running in a pty
	if p.conf.PTY {
		pty, err := StartPTY(p.command)
		if err != nil {
			p.ExitStatus = "1"
			return err
		}

		p.Pid = p.command.Process.Pid

		// Signal waiting consumers in Started() by closing the started channel
		close(p.started)

		waitGroup.Add(1)

		go func() {
			p.logger.Debug("[Process] Starting to copy PTY to the buffer")

			// Copy the pty to our buffer. This will block until it
			// EOF's or something breaks.
			_, err = io.Copy(lineWriterPipe, pty)
			if e, ok := err.(*os.PathError); ok && e.Err == syscall.EIO {
				// We can safely ignore this error, because
				// it's just the PTY telling us that it closed
				// successfully.  See:
				// https://github.com/buildkite/agent/pull/34#issuecomment-46080419
				err = nil
			}

			if err != nil {
				p.logger.Error("[Process] PTY output copy failed with error: %T: %v", err, err)
			} else {
				p.logger.Debug("[Process] PTY has finished being copied to the buffer")
			}

			waitGroup.Done()
		}()
	} else {
		p.command.Stdout = lineWriterPipe
		p.command.Stderr = lineWriterPipe
		p.command.Stdin = nil

		err := p.command.Start()
		if err != nil {
			p.ExitStatus = "1"
			return err
		}

		p.Pid = p.command.Process.Pid

		// Signal waiting consumers in Started() by closing the started channel
		close(p.started)
	}

	p.logger.Info("[Process] Process is running with PID: %d", p.Pid)

	if p.conf.Handler != nil {
		// Add the scanner the waitGroup
		waitGroup.Add(1)

		scanner := NewScanner(p.logger)

		// Start the Scanner
		go func() {
			defer waitGroup.Done()
			if err := scanner.ScanLines(lineReaderPipe, p.conf.Handler); err != nil {
				p.logger.Error("[Process] Scanner failed with %v", err)
			}
		}()
	} else {
		go io.Copy(ioutil.Discard, lineReaderPipe)
	}

	// Wait until the process has finished. The returned error is nil if the command runs,
	// has no problems copying stdin, stdout, and stderr, and exits with a zero exit status.
	waitResult := p.command.Wait()

	// Close the line writer pipe
	lineWriterPipe.Close()

	// Signal waiting consumers in Done() by closing the done channel
	close(p.done)

	// Find the exit status of the script
	p.ExitStatus = p.getExitStatus(waitResult)

	p.logger.Info("Process with PID: %d finished with Exit Status: %s", p.Pid, p.ExitStatus)

	// Sometimes (in docker containers) io.Copy never seems to finish. This is a mega
	// hack around it. If it doesn't finish after 1 second, just continue.
	p.logger.Debug("[Process] Waiting for routines to finish")
	err := timeoutWait(&waitGroup)
	if err != nil {
		p.logger.Debug("[Process] Timed out waiting for wait group: (%T: %v)", err, err)
	}

	// No error occurred so we can return nil
	return nil
}

// Done returns a channel that is closed when the process finishes
func (p *Process) Done() <-chan struct{} {
	p.mu.Lock()
	// We create this here in case this is called before Start()
	if p.done == nil {
		p.done = make(chan struct{})
	}
	d := p.done
	p.mu.Unlock()
	return d
}

// Started returns a channel that is closed when the process is started
func (p *Process) Started() <-chan struct{} {
	p.mu.Lock()
	// We create this here in case this is called before Start()
	if p.started == nil {
		p.started = make(chan struct{})
	}
	d := p.started
	p.mu.Unlock()
	return d
}

// Interrupt the process on platforms that support it, terminate otherwise
func (p *Process) Interrupt() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.command == nil || p.command.Process == nil {
		p.logger.Debug("[Process] No process to interrupt yet")
		return nil
	}

	// interrupt the process (ctrl-c or SIGINT)
	if err := InterruptProcessGroup(p.command.Process, p.logger); err != nil {
		p.logger.Error("[Process] Failed to interrupt process %d: %v", p.Pid, err)

		// Fallback to terminating if we get an error
		if termErr := TerminateProcessGroup(p.command.Process, p.logger); termErr != nil {
			return termErr
		}
	}

	return nil
}

// Terminate the process
func (p *Process) Terminate() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.command == nil || p.command.Process == nil {
		p.logger.Debug("[Process] No process to terminate yet")
		return nil
	}

	return TerminateProcessGroup(p.command.Process, p.logger)
}

// https://github.com/hnakamur/commango/blob/fe42b1cf82bf536ce7e24dceaef6656002e03743/os/executil/executil.go#L29
// TODO: Can this be better?
func (p *Process) getExitStatus(waitResult error) string {
	exitStatus := -1

	if waitResult != nil {
		if err, ok := waitResult.(*exec.ExitError); ok {
			if s, ok := err.Sys().(syscall.WaitStatus); ok {
				exitStatus = s.ExitStatus()
			} else {
				p.logger.Error("[Process] Unimplemented for system where exec.ExitError.Sys() is not syscall.WaitStatus.")
			}
		} else {
			p.logger.Error("[Process] Unexpected error type in getExitStatus: %#v", waitResult)
		}
	} else {
		exitStatus = 0
	}

	return fmt.Sprintf("%d", exitStatus)
}

func timeoutWait(waitGroup *sync.WaitGroup) error {
	// Make a chanel that we'll use as a timeout
	c := make(chan int, 1)

	// Start waiting for the routines to finish
	go func() {
		waitGroup.Wait()
		c <- 1
	}()

	select {
	case _ = <-c:
		return nil
	case <-time.After(10 * time.Second):
		return errors.New("Timeout")
	}
}
