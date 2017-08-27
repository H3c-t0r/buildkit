package shell

import (
	"os"
	"path"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/buildkite/agent/env"
	"github.com/mattn/go-shellwords"
)

type Command struct {
	// The command for process
	Command string

	// The arguments that need to be passed to the command
	Args []string

	// The environment to use for the command
	Env *env.Environment

	// The directory to run the command from
	Dir string
}

// Creates a command by parsing a string like `ls -lsa`
func CommandFromString(str string) (*Command, error) {
	args, err := shellwords.Parse(str)
	if err != nil {
		return nil, err
	}

	return &Command{Command: args[0], Args: args[1:]}, nil
}

// The absolute path to this commands executable
func (c *Command) AbsolutePath() (string, error) {
	// Is the path already absolute?
	if path.IsAbs(c.Command) {
		return c.Command, nil
	}

	var envPath string
	var fileExtensions string // For searching .exe, .bat, etc on Windows

	// Default to the operation system's PATH if a custom environment
	// hasn't been provided
	if c.Env == nil {
		envPath = os.Getenv("PATH")
		fileExtensions = os.Getenv("PATHEXT")
	} else {
		envPath = c.Env.Get("PATH")
		fileExtensions = c.Env.Get("PATHEXT")
	}

	// Use our custom lookPath that takes a specific path
	absolutePath, err := lookPath(c.Command, envPath, fileExtensions)

	if err != nil {
		return "", err
	}

	// Since the path returned by LookPath is relative to the
	// current working directory, we need to get the absolute
	// version of that.
	return filepath.Abs(absolutePath)
}

func (c *Command) String() string {
	s := []string{c.Command}
	for _, a := range c.Args {
		if strings.Contains(a, "\n") || strings.Contains(a, " ") {
			aa := strings.Replace(strings.Replace(a, "\n", "", -1), "\"", "\\", -1)
			s = append(s, "\""+truncate(aa, 40)+"\"")
		} else {
			s = append(s, a)
		}
	}

	return strings.Join(s, " ")
}

func truncate(s string, i int) string {
	if len(s) < i {
		return s
	}

	if utf8.ValidString(s[:i]) {
		return s[:i] + "..."
	}

	return s[:i+1] + "..." // or i-1
}
