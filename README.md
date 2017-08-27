# Buildkite Agent ![Build status](https://badge.buildkite.com/08e4e12a0a1e478f0994eb1e8d51822c5c74d395.svg?branch=master)

_Note: This is the 3.0 development branch of the buildkite-agent, and may not contain files or code in the current stable release. To see code or submit PRs for stable agent versions, please use the corresponding maintenance branch: [2.6.x](https://github.com/buildkite/agent/tree/2-6-stable)_.

The buildkite-agent is a small, reliable, and cross-platform build runner that makes it easy to run automated builds on your own infrastructure. It’s main responsibilities are polling [buildkite.com](https://buildkite.com/) for work, running build jobs, reporting back the status code and output log of the job, and uploading the job's artifacts.

Full documentation is available at [buildkite.com/docs/agent](https://buildkite.com/docs/agent)

```
$ buildkite-agent --help
Usage:

  buildkite-agent <command> [arguments...]

Available commands are:

  start		Starts a Buildkite agent
  artifact	Upload/download artifacts from Buildkite jobs
  meta-data	Get/set data from Buildkite jobs
  pipeline	Make changes to the pipeline of the currently running build
  bootstrap	Run a Buildkite job locally
  help, h	Shows a list of commands or help for one command

Use "buildkite-agent <command> --help" for more information about a command.
```

## Installing

The agents page on Buildkite has personalised instructions for installing the agent with Ubuntu (via apt), Debian (via apt), macOS (via homebrew), Windows and Linux. You can also run the agent [via Docker](https://hub.docker.com/r/buildkite/agent).

## Starting

To start an agent all you need is your agent token, which you can find on your Agents page within Buildkite.

```
$ buildkite-agent start --token
```

## Development

### With Docker

```bash
$ docker-compose run agent bash
root@d854f845511a:/go/src/github.com/buildkite/agent# go run *.go start --token xxx --debug
```

### Without Docker

```bash
# Make sure you have go installed.
brew install go

# Setup your GOPATH
export GOPATH="$HOME/Code/go"
export PATH="$HOME/Code/go/bin:$PATH"

# Checkout the code
go get github.com/buildkite/agent
cd "$GOPATH/src/github.com/buildkite/agent"
```

To test the commands locally:

```bash
go run main.go start --debug --token "abc123"
```

### Dependency management

We're using [govendor](https://github.com/kardianos/govendor) to manage our Go depenencies. Install it with:

```bash
go get github.com/kardianos/govendor
```

If you introduce a new package, just add the import to your source file and run:

```bash
govendor fetch +missing
```

Or explicitly fetch it with a version using:

```bash
govendor fetch github.com/buildkite/go-buildkite@v2.0.0
```

Check out the [govendor repo](https://github.com/kardianos/govendor) and [dev guide](https://github.com/kardianos/govendor/blob/master/doc/dev-guide.md) for more docs.

## Contributing

1. Fork it
2. Create your feature branch (`git checkout -b my-new-feature`)
3. Commit your changes (`git commit -am 'Add some feature'`)
4. Push to the branch (`git push origin my-new-feature`)
5. Create new Pull Request

## Copyright

Copyright (c) 2014-2016 Keith Pitt, Buildkite Pty Ltd. See LICENSE for details.
