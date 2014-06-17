package buildbox

// Logic for this file is largely based on:
// https://github.com/jarib/childprocess/blob/783f7a00a1678b5d929062564ef5ae76822dfd62/lib/childprocess/unix/process.rb

import (
	"bytes"
	"fmt"
	"github.com/kr/pty"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sync"
	"syscall"
	"time"
)

type Process struct {
	Output     string
	Pid        int
	Running    bool
	ExitStatus string
	command    *exec.Cmd
}

// Implement the Stringer thingy
func (p Process) String() string {
	return fmt.Sprintf("Process{Pid: %d, Running: %t, ExitStatus: %s}", p.Pid, p.Running, p.ExitStatus)
}

func (p Process) Kill() error {
	p.signal(syscall.SIGTERM)

	return nil
}

func (p Process) signal(sig os.Signal) error {
	Logger.Debugf("Sending `%s` to PID: `%d`", sig.String(), p.Pid)

	err := p.command.Process.Signal(syscall.SIGTERM)
	if err != nil {
		Logger.Errorf("Failed to send `%s` to PID: `%d` (%T: %v)", sig.String(), p.Pid, err, err)
		return err
	}

	return nil
}

func RunScript(dir string, script string, env []string, callback func(Process)) (*Process, error) {
	// Create a new instance of our process struct
	var process Process

	// Find the script to run
	absoluteDir, _ := filepath.Abs(dir)
	pathToScript := path.Join(absoluteDir, script)

	Logger.Infof("Starting to run script `%s` from inside %s", script, absoluteDir)

	process.command = exec.Command(pathToScript)
	process.command.Dir = absoluteDir

	// Children of the forked process will inherit its process group
	// This is to make sure that all grandchildren dies when this Process instance is killed
	process.command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	// Copy the current processes ENV and merge in the
	// new ones. We do this so the sub process gets PATH
	// and stuff.
	// TODO: Is this correct?
	currentEnv := os.Environ()
	process.command.Env = append(currentEnv, env...)

	// Start our process
	pty, err := pty.Start(process.command)
	if err != nil {
		// The process essentially failed, so we'll just make up
		// and exit status.
		process.ExitStatus = "1"

		return &process, err
	}

	process.Pid = process.command.Process.Pid
	process.Running = true

	Logger.Infof("Process is running with PID: %d", process.Pid)

	var buffer bytes.Buffer
	var w sync.WaitGroup
	w.Add(2)

	go func() {
		Logger.Debug("Starting to copy PTY to the buffer")

		// Copy the pty to our buffer. This will block until it EOF's
		// or something breaks.
		_, err = io.Copy(&buffer, pty)
		if err != nil {
			Logger.Errorf("io.Copy failed with error: %T: %v", err, err)
		} else {
			Logger.Debug("io.Copy finsihed")
		}

		w.Done()
	}()

	go func() {
		for process.Running {
			Logger.Debug("Copying buffer to the process output")

			// Convert the stdout buffer to a string
			process.Output = buffer.String()

			// Call the callback and pass in our process object
			callback(process)

			// Sleep for 1 second
			time.Sleep(1000 * time.Millisecond)
		}

		Logger.Debug("Finished routine that copies the buffer to the process output")

		w.Done()
	}()

	// Wait until the process has finished. The returned error is nil if the command runs,
	// has no problems copying stdin, stdout, and stderr, and exits with a zero exit status.
	waitResult := process.command.Wait()

	// The process is no longer running at this point
	process.Running = false

	// Determine the exit status (if waitResult is an error, that means that the process
	// returned a non zero exit status)
	if waitResult != nil {
		if werr, ok := waitResult.(*exec.ExitError); ok {
			// This returns a string like: `exit status 123`
			exitString := werr.Error()
			exitStringRegex := regexp.MustCompile(`([0-9]+)$`)

			if exitStringRegex.MatchString(exitString) {
				process.ExitStatus = exitStringRegex.FindString(exitString)
			} else {
				Logger.Errorf("Weird looking exit status: %s", exitString)

				// If the exit status isn't what I'm looking for, provide a generic one.
				process.ExitStatus = "-1"
			}
		} else {
			Logger.Errorf("Could not determine exit status. %T: %v", waitResult, waitResult)

			// Not sure what to provide as an exit status if one couldn't be determined.
			process.ExitStatus = "-1"
		}
	} else {
		process.ExitStatus = "0"
	}

	Logger.Debugf("Process with PID: %d finished with Exit Status: %s", process.Pid, process.ExitStatus)

	// Make a chanel that we'll use as a timeout
	c := make(chan int, 1)

	// Start waiting for the routines to finish
	Logger.Debug("Waiting for io.Copy and incremental output to finish")
	go func() {
		w.Wait()
		c <- 1
	}()

	// Sometimes (in docker containers) io.Copy never seems to finish. This is a mega
	// hack around it. If it doesn't finish after 1 second, just continue.
	// TODO: Whyyyyy!?!?!?
	select {
	case _ = <-c:
		// nothing, wait finished fine.
	case <-time.After(1 * time.Second):
		Logger.Error("Timed out waiting for the routines to finish. Forcefully moving on.")
	}

	// Copy the final output back to the process
	process.Output = buffer.String()

	// No error occured so we can return nil
	return &process, nil
}
