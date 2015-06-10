package clicommand

import (
	"fmt"
	"github.com/buildkite/agent/agent"
	"github.com/buildkite/agent/api"
	"github.com/buildkite/agent/cliconfig"
	"github.com/buildkite/agent/logger"
	"github.com/buildkite/agent/retry"
	"github.com/codegangsta/cli"
	"time"
)

var MetaDataGetHelpDescription = `Usage:

   buildkite-agent meta-data get <key> [arguments...]

Description:

   Get data from a builds key/value store.

Example:

   $ buildkite-agent meta-data get "foo"`

type MetaDataGetConfig struct {
	Key              string `cli:"arg:0" label:"meta-data key" validate:"required"`
	Job              string `cli:"job" validate:"required"`
	AgentAccessToken string `cli:"agent-access-token" validate:"required"`
	Endpoint         string `cli:"endpoint" validate:"required"`
	NoColor          bool   `cli:"no-color"`
	Debug            bool   `cli:"debug"`
}

var MetaDataGetCommand = cli.Command{
	Name:        "get",
	Usage:       "Get data from a build",
	Description: MetaDataGetHelpDescription,
	Flags: []cli.Flag{
		cli.StringFlag{
			Name:   "job",
			Value:  "",
			Usage:  "Which job should the meta-data be retrieved from",
			EnvVar: "BUILDKITE_JOB_ID",
		},
		AgentAccessTokenFlag,
		EndpointFlag,
		DebugFlag,
		NoColorFlag,
	},
	Action: func(c *cli.Context) {
		// The configuration will be loaded into this struct
		cfg := MetaDataGetConfig{}

		// Load the configuration
		if err := cliconfig.Load(c, &cfg); err != nil {
			logger.Fatal("%s", err)
		}

		// Setup the any global configuration options
		HandleGlobalFlags(cfg)

		// Create the API client
		client := agent.APIClient{
			Endpoint: cfg.Endpoint,
			Token:    cfg.AgentAccessToken,
		}.Create()

		// Find the meta data value
		var metaData *api.MetaData
		var err error
		err = retry.Do(func(s *retry.Stats) error {
			metaData, _, err = client.MetaData.Get(cfg.Job, cfg.Key)
			if err != nil {
				logger.Warn("%s (%s)", err, s)
			}

			return err
		}, &retry.Config{Maximum: 10, Interval: 1 * time.Second})
		if err != nil {
			logger.Fatal("Failed to get meta-data: %s", err)
		}

		// Output the value to STDOUT
		fmt.Print(metaData.Value)
	},
}
