package clicommand

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/buildkite/agent/v3/agent"
	"github.com/buildkite/agent/v3/api"
	"github.com/buildkite/agent/v3/cliconfig"
	"github.com/buildkite/agent/v3/env"
	"github.com/buildkite/agent/v3/internal/job/shell"
	"github.com/buildkite/agent/v3/internal/pipeline"
	"github.com/buildkite/agent/v3/internal/redactor"
	"github.com/buildkite/agent/v3/internal/stdin"
	"github.com/urfave/cli"
	"gopkg.in/yaml.v3"
)

const pipelineUploadHelpDescription = `Usage:

   buildkite-agent pipeline upload [file] [options...]

Description:

   Allows you to change the pipeline of a running build by uploading either a
   YAML (recommended) or JSON configuration file. If no configuration file is
   provided, the command looks for the file in the following locations:

   - buildkite.yml
   - buildkite.yaml
   - buildkite.json
   - .buildkite/pipeline.yml
   - .buildkite/pipeline.yaml
   - .buildkite/pipeline.json
   - buildkite/pipeline.yml
   - buildkite/pipeline.yaml
   - buildkite/pipeline.json

   You can also pipe build pipelines to the command allowing you to create
   scripts that generate dynamic pipelines. The configuration file has a
   limit of 500 steps per file. Configuration files with over 500 steps
   must be split into multiple files and uploaded in separate steps.

Example:

   $ buildkite-agent pipeline upload
   $ buildkite-agent pipeline upload my-custom-pipeline.yml
   $ ./script/dynamic_step_generator | buildkite-agent pipeline upload`

type PipelineUploadConfig struct {
	FilePath          string   `cli:"arg:0" label:"upload paths"`
	Replace           bool     `cli:"replace"`
	Job               string   `cli:"job"` // required, but not in dry-run mode
	DryRun            bool     `cli:"dry-run"`
	DryRunFormat      string   `cli:"format"`
	NoInterpolation   bool     `cli:"no-interpolation"`
	RedactedVars      []string `cli:"redacted-vars" normalize:"list"`
	RejectSecrets     bool     `cli:"reject-secrets"`
	SigningKeyFile    string   `cli:"signing-key"`
	SigningAlgorithm  string   `cli:"signing-algorithm"`
	GenerateAIPrompts bool     `cli:"generate-ai-prompts"`

	// Global flags
	Debug       bool     `cli:"debug"`
	LogLevel    string   `cli:"log-level"`
	NoColor     bool     `cli:"no-color"`
	Experiments []string `cli:"experiment" normalize:"list"`
	Profile     string   `cli:"profile"`

	// API config
	DebugHTTP        bool   `cli:"debug-http"`
	AgentAccessToken string `cli:"agent-access-token"` // required, but not in dry-run mode
	Endpoint         string `cli:"endpoint" validate:"required"`
	NoHTTP2          bool   `cli:"no-http2"`
}

var PipelineUploadCommand = cli.Command{
	Name:        "upload",
	Usage:       "Uploads a description of a build pipeline adds it to the currently running build after the current job",
	Description: pipelineUploadHelpDescription,
	Flags: []cli.Flag{
		cli.BoolFlag{
			Name:   "replace",
			Usage:  "Replace the rest of the existing pipeline with the steps uploaded. Jobs that are already running are not removed.",
			EnvVar: "BUILDKITE_PIPELINE_REPLACE",
		},
		cli.StringFlag{
			Name:   "job",
			Value:  "",
			Usage:  "The job that is making the changes to its build",
			EnvVar: "BUILDKITE_JOB_ID",
		},
		cli.BoolFlag{
			Name:   "dry-run",
			Usage:  "Rather than uploading the pipeline, it will be echoed to stdout",
			EnvVar: "BUILDKITE_PIPELINE_UPLOAD_DRY_RUN",
		},
		cli.StringFlag{
			Name:   "format",
			Usage:  "In dry-run mode, specifies the form to output the pipeline in. Must be one of: json,yaml",
			Value:  "json",
			EnvVar: "BUILDKITE_PIPELINE_UPLOAD_DRY_RUN_FORMAT",
		},
		cli.BoolFlag{
			Name:   "no-interpolation",
			Usage:  "Skip variable interpolation the pipeline when uploaded",
			EnvVar: "BUILDKITE_PIPELINE_NO_INTERPOLATION",
		},
		cli.BoolFlag{
			Name:   "reject-secrets",
			Usage:  "When true, fail the pipeline upload early if the pipeline contains secrets",
			EnvVar: "BUILDKITE_AGENT_PIPELINE_UPLOAD_REJECT_SECRETS",
		},
		cli.StringFlag{
			Name:   "signing-key",
			Usage:  "Path to a file containing a signing key. Passing this flag enables pipeline signing. The format of the file depends on the chosen signing algorithm",
			EnvVar: "BUILDKITE_PIPELINE_SIGNING_KEY_FILE",
		},
		cli.StringFlag{
			Name:   "signing-algorithm",
			Usage:  "Signing algorithm to use when signing parts of the pipeline. Available algorithms are: hmac-sha256",
			Value:  "hmac-sha256",
			EnvVar: "BUILDKITE_PIPELINE_SIGNING_ALGORITHM",
		},
		cli.BoolFlag{
			Name:   "generate-ai-prompts",
			Usage:  "When used with signing, includes generative AI prompts in the output based on the signature value",
			EnvVar: "BUILDKITE_PIPELINE_SIGNING_GENERATE_AI_PROMPTS",
		},

		// API Flags
		AgentAccessTokenFlag,
		EndpointFlag,
		NoHTTP2Flag,
		DebugHTTPFlag,

		// Global flags
		NoColorFlag,
		DebugFlag,
		LogLevelFlag,
		ExperimentsFlag,
		ProfileFlag,
		RedactedVars,
	},
	Action: func(c *cli.Context) {
		ctx := context.Background()

		// The configuration will be loaded into this struct
		cfg := PipelineUploadConfig{}

		loader := cliconfig.Loader{CLI: c, Config: &cfg}
		warnings, err := loader.Load()
		if err != nil {
			fmt.Printf("%s", err)
			os.Exit(1)
		}

		l := CreateLogger(&cfg)

		// Now that we have a logger, log out the warnings that loading config generated
		for _, warning := range warnings {
			l.Warn("%s", warning)
		}

		// Setup any global configuration options
		done := HandleGlobalFlags(l, cfg)
		defer done()

		// Find the pipeline either from STDIN or the first argument
		var input *os.File
		var filename string

		switch {
		case cfg.FilePath != "":
			l.Info("Reading pipeline config from %q", cfg.FilePath)

			filename = filepath.Base(cfg.FilePath)
			file, err := os.Open(cfg.FilePath)
			if err != nil {
				l.Fatal("Failed to read file: %v", err)
			}
			defer file.Close()
			input = file

		case stdin.IsReadable():
			l.Info("Reading pipeline config from STDIN")

			// Actually read the file from STDIN
			input = os.Stdin

		default:
			l.Info("Searching for pipeline config...")

			paths := []string{
				"buildkite.yml",
				"buildkite.yaml",
				"buildkite.json",
				filepath.FromSlash(".buildkite/pipeline.yml"),
				filepath.FromSlash(".buildkite/pipeline.yaml"),
				filepath.FromSlash(".buildkite/pipeline.json"),
				filepath.FromSlash("buildkite/pipeline.yml"),
				filepath.FromSlash("buildkite/pipeline.yaml"),
				filepath.FromSlash("buildkite/pipeline.json"),
			}

			// Collect all the files that exist
			exists := []string{}
			for _, path := range paths {
				if _, err := os.Stat(path); err == nil {
					exists = append(exists, path)
				}
			}

			// If more than 1 of the config files exist, throw an
			// error. There can only be one!!
			if len(exists) > 1 {
				l.Fatal("Found multiple configuration files: %s. Please only have 1 configuration file present.", strings.Join(exists, ", "))
			}
			if len(exists) == 0 {
				l.Fatal("Could not find a default pipeline configuration file. See `buildkite-agent pipeline upload --help` for more information.")
			}

			found := exists[0]

			l.Info("Found config file %q", found)

			// Read the default file
			filename = path.Base(found)
			file, err := os.Open(found)
			if err != nil {
				l.Fatal("Failed to read file %q: %v", found, err)
			}
			defer file.Close()
			input = file
		}

		// Make sure the file actually has something in it
		if input != os.Stdin {
			fi, err := input.Stat()
			if err != nil {
				l.Fatal("Couldn't stat pipeline configuration file %q: %v", input.Name(), err)
			}
			if fi.Size() == 0 {
				l.Fatal("Pipeline file %q is empty", input.Name())
			}
		}

		var environ *env.Environment
		if !cfg.NoInterpolation {
			// Load environment to pass into parser
			environ = env.FromSlice(os.Environ())

			// resolve BUILDKITE_COMMIT based on the local git repo
			if commitRef, ok := environ.Get("BUILDKITE_COMMIT"); ok {
				cmdOut, err := exec.Command("git", "rev-parse", commitRef).Output()
				if err != nil {
					l.Warn("Error running git rev-parse %q: %v", commitRef, err)
				} else {
					trimmedCmdOut := strings.TrimSpace(string(cmdOut))
					l.Info("Updating BUILDKITE_COMMIT to %q", trimmedCmdOut)
					environ.Set("BUILDKITE_COMMIT", trimmedCmdOut)
				}
			}
		}

		src := filename
		if src == "" {
			src = "(stdin)"
		}

		// Parse the pipeline
		result, err := pipeline.Parse(input)
		if err != nil {
			l.Fatal("Pipeline parsing of %q failed: %v", src, err)
		}
		if !cfg.NoInterpolation {
			if err := result.Interpolate(environ); err != nil {
				l.Fatal("Pipeline interpolation of %q failed: %v", src, err)
			}
		}

		if len(cfg.RedactedVars) > 0 {
			needles := redactor.VarsToRedact(shell.StderrLogger, cfg.RedactedVars, env.FromSlice(os.Environ()).Dump())

			serialisedPipeline, err := result.MarshalJSON()
			if err != nil {
				l.Fatal("Couldn’t scan the %q pipeline for redacted variables. This parsed pipeline could not be serialized, ensure the pipeline YAML is valid, or ignore interpolated secrets for this upload by passing --redacted-vars=''. (%s)", src, err)
			}

			stringifiedserialisedPipeline := string(serialisedPipeline)

			secretsFound := make([]string, 0, len(needles))
			for needleKey, needle := range needles {
				if strings.Contains(stringifiedserialisedPipeline, needle) {
					secretsFound = append(secretsFound, needleKey)
				}
			}

			if len(secretsFound) > 0 {
				if cfg.RejectSecrets {
					l.Fatal("Pipeline %q contains values interpolated from the following secret environment variables: %v, and cannot be uploaded to Buildkite", src, secretsFound)
				} else {
					l.Warn("Pipeline %q contains values interpolated from the following secret environment variables: %v, which could leak sensitive information into the Buildkite UI.", src, secretsFound)
					l.Warn("This pipeline will still be uploaded, but if you'd like to to prevent this from happening, you can use the `--reject-secrets` cli flag, or the `BUILDKITE_AGENT_PIPELINE_UPLOAD_REJECT_SECRETS` environment variable, which will make the `buildkite-agent pipeline upload` command fail if it finds secrets in the pipeline.")
					l.Warn("The behaviour in the above flags will become default in Buildkite Agent v4")
				}
			}
		}

		if cfg.SigningKeyFile != "" {
			key, err := os.ReadFile(cfg.SigningKeyFile)
			if err != nil {
				l.Fatal("Couldn't read the signing key file: %v", err)
			}

			// TODO: Parse the key based on the algorithm, or put key parsing
			// into pipeline.New{Signer,Verifier}.
			// For now it must be hmac-sha256, which takes []byte.

			signer, err := pipeline.NewSigner(cfg.SigningAlgorithm, key)
			if err != nil {
				l.Fatal("Couldn't create a pipeline signer for the chosen algorithm or key: %v", err)
			}

			if err := result.Sign(signer, cfg.GenerateAIPrompts); err != nil {
				l.Fatal("Couldn't sign pipeline with the chosen algorithm or key: %v", err)
			}
		}

		// In dry-run mode we just output the generated pipeline to stdout.
		if cfg.DryRun {
			var encode func(any) error

			switch cfg.DryRunFormat {
			case "json":
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				encode = enc.Encode

			case "yaml":
				encode = yaml.NewEncoder(os.Stdout).Encode

			default:
				l.Fatal("Unknown output format %q", cfg.DryRunFormat)
			}

			// All logging happens to stderr.
			// So this can be used with other tools to get interpolated, signed
			// JSON or YAML.
			if err := encode(result); err != nil {
				l.Fatal("%#v", err)
			}

			return
		}

		// Check we have a job id set if not in dry run
		if cfg.Job == "" {
			l.Fatal("Missing job parameter. Usually this is set in the environment for a Buildkite job via BUILDKITE_JOB_ID.")
		}

		// Check we have an agent access token if not in dry run
		if cfg.AgentAccessToken == "" {
			l.Fatal("Missing agent-access-token parameter. Usually this is set in the environment for a Buildkite job via BUILDKITE_AGENT_ACCESS_TOKEN.")
		}

		uploader := &agent.PipelineUploader{
			Client: api.NewClient(l, loadAPIClientConfig(cfg, "AgentAccessToken")),
			JobID:  cfg.Job,
			Change: &api.PipelineChange{
				UUID:     api.NewUUID(),
				Replace:  cfg.Replace,
				Pipeline: result,
			},
			RetrySleepFunc: time.Sleep,
		}
		if err := uploader.Upload(ctx, l); err != nil {
			l.Fatal("%v", err)
		}

		l.Info("Successfully uploaded and parsed pipeline config")
	},
}
