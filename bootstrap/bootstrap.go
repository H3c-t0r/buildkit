package bootstrap

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/buildkite/agent/agent"
	"github.com/buildkite/agent/bootstrap/shell"
	"github.com/buildkite/agent/env"
)

// Bootstrap represents the phases of execution in a Buildkite Job. It's run
// as a sub-process of the buildkite-agent and finishes at the conclusion of a job.
// Historically (prior to v3) the bootstrap was a shell script, but was ported to
// Golang for portability and testability
type Bootstrap struct {
	// Config provides the bootstrap configuration
	Config

	// Shell is the shell environment for the bootstrap
	shell *shell.Shell

	// Plugins are the plugins that are created in the PluginPhase
	plugins []*agent.Plugin
}

// Start runs the bootstrap and exits when finished
func (b *Bootstrap) Start() {
	if b.shell == nil {
		var err error
		b.shell, err = shell.New()
		if err != nil {
			fmt.Printf("Error creating shell: %v", err)
			os.Exit(1)
		}
	}

	// Initialize the environment
	if err := b.setUp(); err != nil {
		b.shell.Errorf("Error setting up bootstrap: %v", err)
		os.Exit(1)
	}

	// Tear down the environment (and fire pre-exit hook) before we exit
	defer func() {
		if err := b.tearDown(); err != nil {
			b.shell.Errorf("Error tearing down bootstrap %v", err)
		}
	}()

	// These are the "Phases of bootstrap execution". They are designed to be
	// run independently at some later stage (think buildkite-agent bootstrap checkout)
	var phases = []func() error{
		b.PluginPhase,
		b.CheckoutPhase,
		b.CommandPhase,
	}

	for _, phase := range phases {
		if err := phase(); err != nil {
			if b.Debug {
				b.shell.Commentf("Firing exit handler with %v", err)
			}
			os.Exit(shell.GetExitCode(err))
		}
	}
}

// executeScript executes a script in a Shell, but the target is an interpreted script
// so it has extra checks applied to make sure it's executable.
func (b *Bootstrap) executeScript(path string) error {
	if runtime.GOOS == "windows" {
		return b.shell.Run(path)
	}

	// If you run a script on Linux that doesn't have the
	// #!/bin/bash thingy at the top, it will fail to run with a
	// "exec format error" error. You can solve it by adding the
	// #!/bin/bash line to the top of the file, but that's
	// annoying, and people generally forget it, so we'll make it
	// easy on them and add it for them here.
	//
	// We also need to make sure the script we pass has quotes
	// around it, otherwise `/bin/bash -c run script with space.sh`
	// fails.
	return b.shell.Run("/bin/bash -c %q", path)
}

// executeHook runs a hook script with the hookRunner
func (b *Bootstrap) executeHook(name string, hookPath string, environ *env.Environment) error {
	b.shell.Headerf("Running %s hook", name)
	if !fileExists(hookPath) {
		if b.Debug {
			b.shell.Commentf("Skipping, no hook script found at \"%s\"", hookPath)
		}
		return nil
	}

	// We need a script to wrap the hook script so that we can snaffle the changed
	// environment variables
	script := newHookScript(hookPath)
	defer script.Close()

	if b.Debug {
		b.shell.Commentf("A hook runner was written to \"%s\" with the following:", script.Path())
		b.shell.Printf("%s", hookPath)
	}

	// Create a copy of the current env
	previousEnviron := b.shell.Env.Copy()

	// Apply any new environment
	b.shell.Env = b.shell.Env.Merge(environ)

	// Restore the previous env later
	defer func() {
		b.shell.Env = previousEnviron
	}()

	b.shell.Commentf("Executing \"%s\"", script.Path())

	// Run the wrapper script
	if err := b.executeScript(script.Path()); err != nil {
		b.shell.Env.Set("BUILDKITE_LAST_HOOK_EXIT_STATUS", fmt.Sprintf("%d", shell.GetExitCode(err)))
		b.shell.Errorf("The %s hook exited with an error: %v", name, err)
		return err
	}

	b.shell.Env.Set("BUILDKITE_LAST_HOOK_EXIT_STATUS", "0")

	// Get changed environent
	changes, err := script.Environment()
	if err != nil {
		b.shell.Errorf("Failed to get environment: %v", err)
		return err
	}

	// Finally, apply changes to the current shell and config
	b.applyEnvironmentChanges(changes)
	return nil
}

func (b *Bootstrap) applyEnvironmentChanges(environ *env.Environment) {
	b.shell.Headerf("Applying environment changes")
	b.shell.Env = b.shell.Env.Merge(environ)

	if environ == nil {
		b.shell.Printf("No changes to apply")
		return
	}

	// Apply the changed environment to the config
	changes := b.Config.ReadFromEnvironment(environ)

	if len(changes) > 0 {
		b.shell.Headerf("Bootstrap configuration has changed")
	}

	// Print out the env vars that changed
	for _, envKey := range changes {
		switch envKey {
		case `BUILDKITE_ARTIFACT_PATHS`:
			b.shell.Commentf("%s is now %q", envKey, b.Config.AutomaticArtifactUploadPaths)
		case `BUILDKITE_ARTIFACT_UPLOAD_DESTINATION`:
			b.shell.Commentf("%s is now %q", envKey, b.Config.ArtifactUploadDestination)
		case `BUILDKITE_GIT_CLEAN_FLAGS`:
			b.shell.Commentf("%s is now %q", envKey, b.Config.GitCleanFlags)
		case `BUILDKITE_GIT_CLONE_FLAGS`:
			b.shell.Commentf("%s is now %q", envKey, b.Config.GitCloneFlags)
		case `BUILDKITE_REFSPEC`:
			b.shell.Commentf("%s is now %q", envKey, b.Config.RefSpec)
		}
	}
}

// Returns the absolute path to a global hook
func (b *Bootstrap) globalHookPath(name string) string {
	return filepath.Join(b.HooksPath, normalizeScriptFileName(name))
}

// Executes a global hook
func (b *Bootstrap) executeGlobalHook(name string) error {
	return b.executeHook("global "+name, b.globalHookPath(name), nil)
}

// Returns the absolute path to a local hook
func (b *Bootstrap) localHookPath(name string) string {
	return filepath.Join(b.shell.Getwd(), ".buildkite", "hooks", normalizeScriptFileName(name))
}

// Executes a local hook
func (b *Bootstrap) executeLocalHook(name string) error {
	return b.executeHook("local "+name, b.localHookPath(name), nil)
}

// Returns the absolute path to a plugin hook
func (b *Bootstrap) pluginHookPath(plugin *agent.Plugin, name string) (string, error) {
	id, err := plugin.Identifier()
	if err != nil {
		return "", err
	}

	dir, err := plugin.RepositorySubdirectory()
	if err != nil {
		return "", err
	}

	return filepath.Join(b.PluginsPath, id, dir, "hooks", normalizeScriptFileName(name)), nil
}

// Executes a plugin hook
func (b *Bootstrap) executePluginHook(plugins []*agent.Plugin, name string) error {
	for _, p := range plugins {
		path, err := b.pluginHookPath(p, name)
		if err != nil {
			return err
		}

		env, _ := p.ConfigurationToEnvironment()
		if err := b.executeHook("plugin "+p.Label()+" "+name, path, env); err != nil {
			return err
		}
	}
	return nil
}

// If a plugin hook exists with this name
func (b *Bootstrap) pluginHookExists(plugins []*agent.Plugin, name string) bool {
	for _, p := range plugins {
		path, err := b.pluginHookPath(p, name)
		if err != nil {
			return false
		}
		if fileExists(path) {
			return true
		}
	}

	return false
}

// Returns whether or not a file exists on the filesystem. We consider any
// error returned by os.Stat to indicate that the file doesn't exist. We could
// be speciifc and use os.IsNotExist(err), but most other errors also indicate
// that the file isn't there (or isn't available) so we'll just catch them all.
func fileExists(filename string) bool {
	_, err := os.Stat(filename)
	return err == nil
}

// Returns a platform specific filename for scripts
func normalizeScriptFileName(filename string) string {
	if runtime.GOOS == "windows" {
		return filename + ".bat"
	}
	return filename
}

func dirForAgentName(agentName string) string {
	badCharsPattern := regexp.MustCompile("[[:^alnum:]]")
	return badCharsPattern.ReplaceAllString(agentName, "-")
}

// Given a repostory, it will add the host to the set of SSH known_hosts on the machine
func addRepositoryHostToSSHKnownHosts(sh *shell.Shell, repository string) {
	knownHosts, err := findKnownHosts(sh)
	if err != nil {
		sh.Warningf("Failed to find SSH known_hosts file: %v", err)
		return
	}
	defer knownHosts.Unlock()

	if err = knownHosts.AddFromRepository(repository); err != nil {
		sh.Warningf("%v", err)
	}
}

// setUp is run before all the phases run. It's responsible for initializing the
// bootstrap environment
func (b *Bootstrap) setUp() error {
	// Create an empty env for us to keep track of our env changes in
	b.shell.Env = env.FromSlice(os.Environ())

	// Add the $BUILDKITE_BIN_PATH to the $PATH if we've been given one
	if b.BinPath != "" {
		b.shell.Env.Set("PATH", fmt.Sprintf("%s%s%s", b.BinPath, string(os.PathListSeparator), b.shell.Env.Get("PATH")))
	}

	b.shell.Env.Set("BUILDKITE_BUILD_CHECKOUT_PATH", filepath.Join(b.BuildPath, dirForAgentName(b.AgentName), b.OrganizationSlug, b.PipelineSlug))

	if b.Debug {
		b.shell.Headerf("Build environment variables")
		for _, e := range b.shell.Env.ToSlice() {
			if strings.HasPrefix(e, "BUILDKITE") || strings.HasPrefix(e, "CI") || strings.HasPrefix(e, "PATH") {
				b.shell.Printf("%s", strings.Replace(e, "\n", "\\n", -1))
			}
		}
	}

	// Disable any interactive Git/SSH prompting
	b.shell.Env.Set("GIT_TERMINAL_PROMPT", "0")

	// It's important to do this before checking out plugins, in case you want
	// to use the global environment hook to whitelist the plugins that are
	// allowed to be used.
	if err := b.executeGlobalHook("environment"); err != nil {
		return nil
	}

	return nil
}

func (b *Bootstrap) tearDown() error {
	if err := b.executeGlobalHook("pre-exit"); err != nil {
		return err
	}

	if err := b.executeLocalHook("pre-exit"); err != nil {
		return err
	}

	if err := b.executePluginHook(b.plugins, "pre-exit"); err != nil {
		return err
	}

	return nil
}

// PluginPhase is where plugins that weren't filtered in the Environment phase are
// checked out and made available to later phases
func (b *Bootstrap) PluginPhase() error {
	if b.Plugins == "" {
		return nil
	}

	b.shell.Headerf("Setting up plugins")

	// Make sure we have a plugin path before trying to do anything
	if b.PluginsPath == "" {
		return fmt.Errorf("Can't checkout plugins without a `plugins-path`")
	}

	var err error
	b.plugins, err = agent.CreatePluginsFromJSON(b.Plugins)
	if err != nil {
		return fmt.Errorf("Failed to parse plugin definition (%s)", err)
	}

	for _, p := range b.plugins {
		// Get the identifer for the plugin
		id, err := p.Identifier()
		if err != nil {
			return err
		}

		// Create a path to the plugin
		directory := filepath.Join(b.PluginsPath, id)
		pluginGitDirectory := filepath.Join(directory, ".git")

		// Has it already been checked out?
		if !fileExists(pluginGitDirectory) {
			// Make the directory
			err = os.MkdirAll(directory, 0777)
			if err != nil {
				return err
			}

			// Try and lock this particular plugin while we check it out (we create
			// the file outside of the plugin directory so git clone doesn't have
			// a cry about the directory not being empty)
			pluginCheckoutHook, err := shell.LockFileWithTimeout(b.shell, filepath.Join(b.PluginsPath, id+".lock"), time.Minute*5)
			if err != nil {
				return err
			}

			// Once we've got the lock, we need to make sure another process didn't already
			// checkout the plugin
			if fileExists(pluginGitDirectory) {
				pluginCheckoutHook.Unlock()
				b.shell.Commentf("Plugin \"%s\" found", p.Label())
				continue
			}

			repo, err := p.Repository()
			if err != nil {
				return err
			}

			b.shell.Commentf("Plugin \"%s\" will be checked out to \"%s\"", p.Location, directory)

			if b.Debug {
				b.shell.Commentf("Checking if \"%s\" is a local repository", repo)
			}

			// Switch to the plugin directory
			previousWd := b.shell.Getwd()
			if err = b.shell.Chdir(directory); err != nil {
				return err
			}

			b.shell.Commentf("Switching to the plugin directory")

			// If it's not a local repo, and we can perform
			// SSH fingerprint verification, do so.
			if !fileExists(repo) && b.SSHFingerprintVerification {
				addRepositoryHostToSSHKnownHosts(b.shell, repo)
			}

			// Plugin clones shouldn't use custom GitCloneFlags
			if err = b.shell.Run("git clone -v -- %q .", repo); err != nil {
				return err
			}

			// Switch to the version if we need to
			if p.Version != "" {
				b.shell.Commentf("Checking out `%s`", p.Version)

				if err = b.shell.Run("git clone -v -- %q .", repo); err != nil {
					return err
				}

				if err = b.shell.Run("git checkout -f %q", p.Version); err != nil {
					return err
				}
			}

			// Switch back to the previous working directory
			if err = b.shell.Chdir(previousWd); err != nil {
				return err
			}

			// Now that we've succefully checked out the
			// plugin, we can remove the lock we have on
			// it.
			pluginCheckoutHook.Unlock()
		} else {
			b.shell.Commentf("Plugin \"%s\" found", p.Label())
		}
	}

	// Now we can run plugin environment hooks too
	if err := b.executePluginHook(b.plugins, "environment"); err != nil {
		return err
	}

	return nil
}

func (b *Bootstrap) DefaultCheckoutPhase() error {
	if b.SSHFingerprintVerification {
		addRepositoryHostToSSHKnownHosts(b.shell, b.Repository)
	}

	// Do we need to do a git checkout?
	existingGitDir := filepath.Join(b.shell.Getwd(), ".git")
	if fileExists(existingGitDir) {
		// Update the the origin of the repository so we can gracefully handle repository renames
		if err := b.shell.Run("git remote set-url origin %q", b.Repository); err != nil {
			return err
		}
	} else {
		if err := b.shell.Run("git clone %s -- %q .", b.GitCloneFlags, b.Repository); err != nil {
			return err
		}
	}

	// Git clean prior to checkout
	if err := gitClean(b.shell, b.GitCleanFlags, b.GitSubmodules); err != nil {
		return err
	}

	// If a refspec is provided then use it instead.
	// i.e. `refs/not/a/head`
	if b.RefSpec != "" {
		b.shell.Commentf("Fetch and checkout custom refspec")
		if err := b.shell.Run("git fetch -v --prune origin %s", b.RefSpec); err != nil {
			return err
		}

		if err := b.shell.Run("git checkout -f %q", b.Commit); err != nil {
			return err
		}

		// GitHub has a special ref which lets us fetch a pull request head, whether
		// or not there is a current head in this repository or another which
		// references the commit. We presume a commit sha is provided. See:
		// https://help.github.com/articles/checking-out-pull-requests-locally/#modifying-an-inactive-pull-request-locally
	} else if b.PullRequest != "false" && strings.Contains(b.PipelineProvider, "github") {
		b.shell.Commentf("Fetch and checkout pull request head")
		if err := b.shell.Run("git fetch -v origin 'refs/pull/%s/head'", b.PullRequest); err != nil {
			return err
		}

		gitFetchHead, _ := b.shell.RunAndCapture("git rev-parse FETCH_HEAD")
		b.shell.Commentf("FETCH_HEAD is now `%s`", gitFetchHead)

		if err := b.shell.Run("git checkout -f %q", b.Commit); err != nil {
			return err
		}

		// If the commit is "HEAD" then we can't do a commit-specific fetch and will
		// need to fetch the remote head and checkout the fetched head explicitly.
	} else if b.Commit == "HEAD" {
		b.shell.Commentf("Fetch and checkout remote branch HEAD commit")
		if err := b.shell.Run("git fetch -v --prune origin %q", b.Branch); err != nil {
			return err
		}
		if err := b.shell.Run("git checkout -f FETCH_HEAD"); err != nil {
			return err
		}

		// Otherwise fetch and checkout the commit directly. Some repositories don't
		// support fetching a specific commit so we fall back to fetching all heads
		// and tags, hoping that the commit is included.
	} else {
		b.shell.Commentf("Fetch and checkout commit")
		if err := b.shell.Run("git fetch -v origin %q", b.Commit); err != nil {
			// By default `git fetch origin` will only fetch tags which are
			// reachable from a fetches branch. git 1.9.0+ changed `--tags` to
			// fetch all tags in addition to the default refspec, but pre 1.9.0 it
			// excludes the default refspec.
			gitFetchRefspec, _ := b.shell.RunAndCapture("git config remote.origin.fetch")
			if err := b.shell.Run("git fetch -v --prune origin %q %q", gitFetchRefspec, "+refs/tags/*:refs/tags/*"); err != nil {
				return err
			}
		}
		if err := b.shell.Run("git checkout -f %q", b.Commit); err != nil {
			return err
		}
	}

	if b.GitSubmodules {
		// submodules might need their fingerprints verified too
		if b.SSHFingerprintVerification {
			b.shell.Commentf("Checking to see if submodule urls need to be added to known_hosts")
			submoduleRepos, err := gitEnumerateSubmoduleURLs(b.shell)
			if err != nil {
				b.shell.Warningf("Failed to enumerate git submodules: %v", err)
			} else {
				for _, repository := range submoduleRepos {
					addRepositoryHostToSSHKnownHosts(b.shell, repository)
				}
			}
		}

		// `submodule sync` will ensure the .git/config
		// matches the .gitmodules file.  The command
		// is only available in git version 1.8.1, so
		// if the call fails, continue the bootstrap
		// script, and show an informative error.
		if err := b.shell.Run("git submodule sync --recursive"); err != nil {
			gitVersionOutput, _ := b.shell.RunAndCapture("git", "--version")
			b.shell.Warningf("Failed to recursively sync git submodules. This is most likely because you have an older version of git installed (" + gitVersionOutput + ") and you need version 1.8.1 and above. If you're using submodules, it's highly recommended you upgrade if you can.")
		}

		if err := b.shell.Run("git submodule update --init --recursive --force"); err != nil {
			return err
		}
		if err := b.shell.Run("git submodule foreach --recursive git reset --hard"); err != nil {
			return err
		}
	}

	// Git clean after checkout
	if err := gitClean(b.shell, b.GitCleanFlags, b.GitSubmodules); err != nil {
		return err
	}

	if b.shell.Env.Get("BUILDKITE_AGENT_ACCESS_TOKEN") == "" {
		b.shell.Warningf("Skipping sending Git information to Buildkite as $BUILDKITE_AGENT_ACCESS_TOKEN is missing")
		return nil
	}

	// Grab author and commit information and send
	// it back to Buildkite. But before we do,
	// we'll check to see if someone else has done
	// it first.
	b.shell.Commentf("Checking to see if Git data needs to be sent to Buildkite")
	if err := b.shell.Run("buildkite-agent meta-data exists buildkite:git:commit"); err != nil {
		b.shell.Commentf("Sending Git commit information back to Buildkite")

		gitCommitOutput, _ := b.shell.RunAndCapture("git show HEAD -s --format=fuller --no-color")
		gitBranchOutput, _ := b.shell.RunAndCapture("git branch --contains HEAD --no-color")

		if err = b.shell.Run("buildkite-agent meta-data set buildkite:git:commit %q", gitCommitOutput); err != nil {
			return err
		}
		if err = b.shell.Run("buildkite-agent meta-data set buildkite:git:branch %q", gitBranchOutput); err != nil {
			return err
		}
	}

	return nil
}

// CheckoutPhase creates the build directory and makes sure we're running the
// build at the right commit.
func (b *Bootstrap) CheckoutPhase() error {
	if err := b.executeGlobalHook("pre-checkout"); err != nil {
		return err
	}

	if err := b.executeLocalHook("pre-checkout"); err != nil {
		return err
	}

	if err := b.executePluginHook(b.plugins, "pre-checkout"); err != nil {
		return err
	}

	// Remove the checkout directory if BUILDKITE_CLEAN_CHECKOUT is present
	if b.CleanCheckout {
		b.shell.Headerf("Cleaning pipeline checkout")
		b.shell.Commentf("Removing %s", b.shell.Env.Get("BUILDKITE_BUILD_CHECKOUT_PATH"))

		if err := os.RemoveAll(b.shell.Env.Get("BUILDKITE_BUILD_CHECKOUT_PATH")); err != nil {
			return fmt.Errorf("Failed to remove \"%s\" (%s)", b.shell.Env.Get("BUILDKITE_BUILD_CHECKOUT_PATH"), err)
		}
	}

	b.shell.Headerf("Preparing build directory")

	// Create the build directory
	if !fileExists(b.shell.Env.Get("BUILDKITE_BUILD_CHECKOUT_PATH")) {
		b.shell.Commentf("Creating \"%s\"", b.shell.Env.Get("BUILDKITE_BUILD_CHECKOUT_PATH"))
		os.MkdirAll(b.shell.Env.Get("BUILDKITE_BUILD_CHECKOUT_PATH"), 0777)
	}

	// Change to the new build checkout path
	if err := b.shell.Chdir(b.shell.Env.Get("BUILDKITE_BUILD_CHECKOUT_PATH")); err != nil {
		return err
	}

	// Run a custom `checkout` hook if it's present
	if fileExists(b.globalHookPath("checkout")) {
		if err := b.executeGlobalHook("checkout"); err != nil {
			return err
		}
	} else if b.pluginHookExists(b.plugins, "checkout") {
		if err := b.executePluginHook(b.plugins, "checkout"); err != nil {
			return err
		}
	} else {
		if err := b.DefaultCheckoutPhase(); err != nil {
			return err
		}
	}

	// Store the current value of BUILDKITE_BUILD_CHECKOUT_PATH, so we can detect if
	// one of the post-checkout hooks changed it.
	previousCheckoutPath := b.shell.Env.Get("BUILDKITE_BUILD_CHECKOUT_PATH")

	// Run post-checkout hooks
	if err := b.executeGlobalHook("post-checkout"); err != nil {
		return err
	}

	if err := b.executeLocalHook("post-checkout"); err != nil {
		return err
	}

	if err := b.executePluginHook(b.plugins, "post-checkout"); err != nil {
		return err
	}

	// Capture the new checkout path so we can see if it's changed.
	newCheckoutPath := b.shell.Env.Get("BUILDKITE_BUILD_CHECKOUT_PATH")

	// If the working directory has been changed by a hook, log and switch to it
	if previousCheckoutPath != "" && previousCheckoutPath != newCheckoutPath {
		b.shell.Headerf("A post-checkout hook has changed the working directory to \"%s\"", newCheckoutPath)

		if err := b.shell.Chdir(newCheckoutPath); err != nil {
			return err
		}
	}

	return nil
}

func (b *Bootstrap) DefaultCommandPhase() error {
	// Make sure we actually have a command to run
	if b.Command == "" {
		return fmt.Errorf("No command has been defined. Please go to \"Pipeline Settings\" and configure your build step's \"Command\"")
	}

	scriptFileName := strings.Replace(b.Command, "\n", "", -1)
	pathToCommand, err := filepath.Abs(filepath.Join(b.shell.Getwd(), scriptFileName))
	commandIsScript := err == nil && fileExists(pathToCommand)

	// If the command isn't a script, then it's something we need
	// to eval. But before we even try running it, we should double
	// check that the agent is allowed to eval commands.
	if !commandIsScript && !b.CommandEval {
		b.shell.Commentf("No such file: \"%s\"", scriptFileName)
		return fmt.Errorf("This agent is not allowed to evaluate console commands. To allow this, re-run this agent without the `--no-command-eval` option, or specify a script within your repository to run instead (such as scripts/test.sh).")
	}

	// Also make sure that the script we've resolved is definitely within this
	// repository checkout and isn't elsewhere on the system.
	if commandIsScript && !b.CommandEval && !strings.HasPrefix(pathToCommand, b.shell.Getwd()+string(os.PathSeparator)) {
		b.shell.Commentf("No such file: \"%s\"", scriptFileName)
		return fmt.Errorf("This agent is only allowed to run scripts within your repository. To allow this, re-run this agent without the `--no-command-eval` option, or specify a script within your repository to run instead (such as scripts/test.sh).")
	}

	var headerLabel string
	var buildScriptPath string
	var promptDisplay string

	// Come up with the contents of the build script. While we
	// generate the script, we need to handle the case of running a
	// script vs. a command differently
	if commandIsScript {
		headerLabel = "Running build script"

		if runtime.GOOS == "windows" {
			promptDisplay = b.Command
		} else {
			// Show a prettier (more accurate version) of
			// what we're doing on Linux
			promptDisplay = "./\"" + b.Command + "\""
		}

		buildScriptPath = pathToCommand
	} else {
		headerLabel = "Running command"

		// Create a build script that will output each line of the command, and run it.
		var buildScriptContents string
		if runtime.GOOS == "windows" {
			buildScriptContents = "@echo off\n"
			for _, k := range strings.Split(b.Command, "\n") {
				if k != "" {
					buildScriptContents = buildScriptContents +
						fmt.Sprintf("ECHO %s\n", shell.BatchEscape("\033[90m>\033[0m "+k)) +
						k + "\n" +
						"if %errorlevel% neq 0 exit /b %errorlevel%\n"
				}
			}
		} else {
			buildScriptContents = "#!/bin/bash\nset -e\n"
			for _, k := range strings.Split(b.Command, "\n") {
				if k != "" {
					buildScriptContents = buildScriptContents +
						fmt.Sprintf("echo '\033[90m$\033[0m %s'\n", strings.Replace(k, "'", "'\\''", -1)) +
						k + "\n"
				}
			}
		}

		// Create a temporary file where we'll run a program from
		buildScriptPath = filepath.Join(b.shell.Getwd(), normalizeScriptFileName("buildkite-script-"+b.JobID))

		if b.Debug {
			b.shell.Headerf("Preparing build script")
			b.shell.Commentf("A build script is being written to \"%s\" with the following:", buildScriptPath)
			b.shell.Printf("%s", buildScriptContents)
		}

		// Write the build script to disk
		err := ioutil.WriteFile(buildScriptPath, []byte(buildScriptContents), 0644)
		if err != nil {
			return fmt.Errorf("Failed to write to \"%s\" (%s)", buildScriptPath, err)
		}
	}

	// Show we're running the script
	b.shell.Headerf("%s", headerLabel)
	if promptDisplay != "" {
		b.shell.Promptf("%s", promptDisplay)
	}

	return b.executeScript(buildScriptPath)
}

// CommandPhase determines how to run the build, and then runs it
func (b *Bootstrap) CommandPhase() error {
	if err := b.executeGlobalHook("pre-command"); err != nil {
		return err
	}

	if err := b.executeLocalHook("pre-command"); err != nil {
		return err
	}

	if err := b.executePluginHook(b.plugins, "pre-command"); err != nil {
		return err
	}

	var commandExitError error

	// Run a custom `command` hook if it's present
	if fileExists(b.globalHookPath("command")) {
		commandExitError = b.executeGlobalHook("command")
	} else if fileExists(b.localHookPath("command")) {
		commandExitError = b.executeGlobalHook("command")
	} else if b.pluginHookExists(b.plugins, "command") {
		commandExitError = b.executePluginHook(b.plugins, "command")
	} else {
		commandExitError = b.DefaultCommandPhase()
	}

	// Expand the command header if it fails
	if commandExitError != nil {
		b.shell.Printf("^^^ +++")
	}

	// Save the command exit status to the env so hooks + plugins can access it
	b.shell.Env.Set("BUILDKITE_COMMAND_EXIT_STATUS", fmt.Sprintf("%d", shell.GetExitCode(commandExitError)))

	// Run post-command hooks
	if err := b.executeGlobalHook("post-command"); err != nil {
		return err
	}

	if err := b.executeLocalHook("post-command"); err != nil {
		return err
	}

	if err := b.executePluginHook(b.plugins, "post-command"); err != nil {
		return err
	}

	return commandExitError
}

func (b *Bootstrap) ArtifactPhase() error {
	if b.AutomaticArtifactUploadPaths != "" {
		// Run pre-artifact hooks
		if err := b.executeGlobalHook("pre-artifact"); err != nil {
			return err
		}

		if err := b.executeLocalHook("pre-artifact"); err != nil {
			return err
		}

		if err := b.executePluginHook(b.plugins, "pre-artifact"); err != nil {
			return err
		}

		// Run the artifact upload command
		b.shell.Headerf("Uploading artifacts")
		if err := b.shell.Run("buildkite-agent", "artifact", "upload", b.AutomaticArtifactUploadPaths, b.ArtifactUploadDestination); err != nil {
			return err
		}

		// Run post-artifact hooks
		if err := b.executeGlobalHook("post-artifact"); err != nil {
			return err
		}

		if err := b.executeLocalHook("post-artifact"); err != nil {
			return err
		}

		if err := b.executePluginHook(b.plugins, "post-artifact"); err != nil {
			return err
		}
	}

	return nil
}
