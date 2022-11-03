package utils

import "os"

var pathToSelf = "buildkite-agent"

func init() {
	p, err := os.Executable()
	if err != nil {
		return
	}
	pathToSelf = p
}

// BuildkiteAgentPath returns a file path to buildkite-agent. If an absolute
// path cannot be found, it defaults to "buildkite-agent" on the assumption it
// is in $PATH. Self-executing with this path can still fail if someone is
// playing games (e.g. unlinking the binary after starting it).
func BuildkiteAgentPath() string {
	return pathToSelf
}
