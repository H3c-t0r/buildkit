package clicommand

import "github.com/urfave/cli"

var BuildkiteAgentCommands = []cli.Command{
	AcknowledgementsCommand,
	AgentStartCommand,
	AnnotateCommand,
	{
		Name:  "annotation",
		Usage: "Make changes to an annotation on the currently running build",
		Subcommands: []cli.Command{
			AnnotationRemoveCommand,
		},
	},
	{
		Name:  "artifact",
		Usage: "Upload/download artifacts from Buildkite jobs",
		Subcommands: []cli.Command{
			ArtifactUploadCommand,
			ArtifactDownloadCommand,
			ArtifactSearchCommand,
			ArtifactShasumCommand,
		},
	},
	{
		Name:  "env",
		Usage: "Process environment subcommands",
		Subcommands: []cli.Command{
			EnvDumpCommand,
			EnvGetCommand,
			EnvSetCommand,
			EnvUnsetCommand,
		},
	},
	{
		Name:  "lock",
		Usage: "Process lock subcommands",
		Subcommands: []cli.Command{
			LockAcquireCommand,
			LockDoCommand,
			LockDoneCommand,
			LockGetCommand,
			LockReleaseCommand,
		},
	},
	{
		Name:  "meta-data",
		Usage: "Get/set data from Buildkite jobs",
		Subcommands: []cli.Command{
			MetaDataSetCommand,
			MetaDataGetCommand,
			MetaDataExistsCommand,
			MetaDataKeysCommand,
		},
	},
	{
		Name:  "oidc",
		Usage: "Interact with Buildkite OpenID Connect (OIDC)",
		Subcommands: []cli.Command{
			OIDCRequestTokenCommand,
		},
	},
	{
		Name:  "pipeline",
		Usage: "Make changes to the pipeline of the currently running build",
		Subcommands: []cli.Command{
			PipelineUploadCommand,
		},
	},
	{
		Name:  "step",
		Usage: "Get or update an attribute of a build step",
		Subcommands: []cli.Command{
			StepGetCommand,
			StepUpdateCommand,
		},
	},
	BootstrapCommand,
	{
		Name:  "job",
		Usage: "Commands invoked as part of a Buildkite job. Typically, these are called automatically by the agent, and not by a user directly",
		Subcommands: []cli.Command{
			JobWriteHookWrapperCommand,
		},
	},
	{
		Name:  "tool",
		Usage: "Utility commands, intended for users and operators of the agent to run directly on their machines, and not as part of a Buildkite job",
		Subcommands: []cli.Command{
			ToolKeygenCommand,
			ToolSignCommand,
		},
	},
}
