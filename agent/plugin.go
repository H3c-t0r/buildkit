package agent

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"github.com/buildkite/agent/env"
	"github.com/buildkite/agent/logger"
)

type Plugin struct {
	// Where the plugin can be found (can either be a file system path, or
	// a git repository)
	Location string

	// The version of the plugin that should be running
	Version string

	// The clone method
	Scheme string

	// Any authentication attached to the repository
	Authentication string

	// Configuration for the plugin
	Configuration map[string]interface{}
}

var locationSchemeRegex = regexp.MustCompile(`^[a-z\+]+://`)

func CreatePlugin(location string, config map[string]interface{}) (*Plugin, error) {
	plugin := &Plugin{Configuration: config}

	u, err := url.Parse(location)
	if err != nil {
		return nil, err
	}

	plugin.Scheme = u.Scheme
	plugin.Location = u.Host + u.Path
	plugin.Version = u.Fragment

	if plugin.Version != "" && strings.Count(plugin.Version, "#") > 0 {
		return nil, fmt.Errorf("Too many #'s in \"%s\"", location)
	}

	if u.User != nil {
		plugin.Authentication = u.User.String()
	}

	return plugin, nil
}

// ResolvePluginLocation takes a short plugin location like "docker-compose" and resolves
// it to it's fully qualified http://github.com/buildkite-plugins/docker-compose#a34fa34. In future
// this would be better suited to a server-side API to allow for more intelligence here
func ResolvePluginLocation(location string) (string, error) {
	var u *url.URL
	var err error

	if locationSchemeRegex.MatchString(location) {
		u, err = url.Parse(location)
		if err != nil {
			return "", err
		}
	} else {
		parts := strings.Split(location, "/")
		switch {
		// domain.com/user/rep, missing scheme
		case len(parts) >= 3:
			location = "https://" + location
		// username/repo
		case len(parts) == 2:
			location = "https://github.com/" + location
		// repo
		case len(parts) == 1:
			location = "https://github.com/buildkite-plugins/" + location
		}
		u, err = url.Parse(location)
		if err != nil {
			return "", err
		}
	}

	if u.Fragment == "" {
		u.Fragment = "master"
	}

	if u.Scheme == "" {
		u.Scheme = "https"
	}

	if u == nil {
		return "", fmt.Errorf("Failed to resolve location %q", location)
	}

	return u.String(), nil
}

// CreatePluginsFromList takes a list of locations and
// converts it to a slice of Plugins
func CreatePluginsFromList(names []string) ([]*Plugin, error) {
	plugins := []*Plugin{}
	for _, part := range names {
		resolved, err := ResolvePluginLocation(strings.TrimSpace(part))
		if err != nil {
			return nil, err
		}
		plugin, err := CreatePlugin(resolved, nil)
		if err != nil {
			return nil, err
		}
		plugins = append(plugins, plugin)
	}

	return plugins, nil
}

// Given a JSON structure, convert it to an array of plugins
func CreatePluginsFromJSON(j string) ([]*Plugin, error) {
	// Use more versatile number decoding
	decoder := json.NewDecoder(strings.NewReader(j))
	decoder.UseNumber()

	// Parse the JSON
	var f interface{}
	err := decoder.Decode(&f)
	if err != nil {
		return nil, err
	}

	// Try and convert the structure to an array
	m, ok := f.([]interface{})
	if !ok {
		return nil, fmt.Errorf("JSON structure was not an array")
	}

	// Convert the JSON elements to plugins
	plugins := []*Plugin{}
	for _, v := range m {
		switch vv := v.(type) {
		case string:
			// Add the plugin with no config to the array
			plugin, err := CreatePlugin(string(vv), map[string]interface{}{})
			if err != nil {
				return nil, err
			}
			plugins = append(plugins, plugin)
		case map[string]interface{}:
			for location, config := range vv {
				// Ensure the config is a hash
				config, ok := config.(map[string]interface{})
				if !ok {
					return nil, fmt.Errorf("Configuration for \"%s\" is not a hash", location)
				}

				// Add the plugin with config to the array
				plugin, err := CreatePlugin(string(location), config)
				if err != nil {
					return nil, err
				}
				plugins = append(plugins, plugin)
			}
		default:
			return nil, fmt.Errorf("Unknown type in plugin definition (%s)", vv)
		}
	}

	return plugins, nil
}

func MergePlugins(jobPlugins, agentPlugins []*Plugin) []*Plugin {
	var result []*Plugin
	var pluginMap map[string]struct{}

	for _, plugin := range agentPlugins {
		pluginMap[plugin.Location] = struct{}{}
		result = append(result, plugin)
	}

	for _, plugin := range jobPlugins {
		if _, exists := pluginMap[plugin.Location]; exists {
			logger.Debug("Plugin %q exists in agent and job plugins, using agent version", plugin.Location)
		} else {
			result = append(result, plugin)
		}
	}

	return result
}

// Returns the name of the plugin
func (p *Plugin) Name() string {
	if p.Location != "" {
		// Grab the last part of the location
		parts := strings.Split(p.Location, "/")
		name := parts[len(parts)-1]

		// Clean up the name
		name = strings.ToLower(name)
		name = regexp.MustCompile(`\s+`).ReplaceAllString(name, " ")
		name = regexp.MustCompile(`[^a-zA-Z0-9]`).ReplaceAllString(name, "-")
		name = strings.Replace(name, "-buildkite-plugin-git", "", -1)
		name = strings.Replace(name, "-buildkite-plugin", "", -1)

		return name
	} else {
		return ""
	}
}

// Returns and ID for the plugin that can be used as a folder name
func (p *Plugin) Identifier() (string, error) {
	nonIdCharacterRegex := regexp.MustCompile(`[^a-zA-Z0-9]`)
	removeDoubleUnderscore := regexp.MustCompile(`-+`)

	id := p.Label()
	id = nonIdCharacterRegex.ReplaceAllString(id, "-")
	id = removeDoubleUnderscore.ReplaceAllString(id, "-")
	id = strings.Trim(id, "-")

	return id, nil
}

// Returns the repository host where the code is stored
func (p *Plugin) Repository() (string, error) {
	s, err := p.constructRepositoryHost()
	if err != nil {
		return "", err
	}

	// Add the authentication if there is one
	if p.Authentication != "" {
		s = p.Authentication + "@" + s
	}

	// If it's not a file system plugin, add the scheme
	if !strings.HasPrefix(s, "/") {
		if p.Scheme != "" {
			s = p.Scheme + "://" + s
		} else {
			s = "https://" + s
		}
	}

	return s, nil
}

// Returns the subdirectory path that the plugin is in
func (p *Plugin) RepositorySubdirectory() (string, error) {
	repository, err := p.constructRepositoryHost()
	if err != nil {
		return "", err
	}

	dir := strings.TrimPrefix(p.Location, repository)

	return strings.TrimPrefix(dir, "/"), nil
}

// Converts the plugin configuration values to environment variables
func (p *Plugin) ConfigurationToEnvironment() (*env.Environment, error) {
	envSlice := []string{}

	toDashRegex := regexp.MustCompile(`-|\s+`)
	removeWhitespaceRegex := regexp.MustCompile(`\s+`)
	removeDoubleUnderscore := regexp.MustCompile(`_+`)

	for k, v := range p.Configuration {
		k = removeWhitespaceRegex.ReplaceAllString(k, " ")
		name := strings.ToUpper(toDashRegex.ReplaceAllString(fmt.Sprintf("BUILDKITE_PLUGIN_%s_%s", p.Name(), k), "_"))
		name = removeDoubleUnderscore.ReplaceAllString(name, "_")

		switch vv := v.(type) {
		case string:
			envSlice = append(envSlice, fmt.Sprintf("%s=%s", name, vv))
		case bool:
			envSlice = append(envSlice, fmt.Sprintf("%s=%t", name, vv))
		case json.Number:
			envSlice = append(envSlice, fmt.Sprintf("%s=%s", name, vv.String()))
		case []string:
			for i := range vv {
				envSlice = append(envSlice, fmt.Sprintf("%s_%d=%s", name, i, vv[i]))
			}
		case []interface{}:
			for i := range vv {
				switch vvv := vv[i].(type) {
				case json.Number:
					envSlice = append(envSlice, fmt.Sprintf("%s_%d=%s", name, i, vvv.String()))
				case string:
					envSlice = append(envSlice, fmt.Sprintf("%s_%d=%s", name, i, vvv))
				default:
					fmt.Printf("Unknown type %T %v", vvv, vvv)
					// unknown type
				}
			}
		default:
			fmt.Printf("Unknown type %T %v", vv, vv)
			// unknown type
		}
	}

	// Sort them into a consistent order
	sort.Strings(envSlice)

	return env.FromSlice(envSlice), nil
}

// Pretty name for the plugin
func (p *Plugin) Label() string {
	if p.Version != "" {
		return p.Location + "#" + p.Version
	} else {
		return p.Location
	}
}

func (p *Plugin) constructRepositoryHost() (string, error) {
	if p.Location == "" {
		return "", fmt.Errorf("Missing plugin location")
	}

	parts := strings.Split(p.Location, "/")
	if len(parts) < 2 {
		return "", fmt.Errorf("Incomplete plugin path \"%s\"", p.Location)
	}

	var s string

	if parts[0] == "github.com" || parts[0] == "bitbucket.org" || parts[0] == "gitlab.com" {
		if len(parts) < 3 {
			return "", fmt.Errorf("Incomplete %s path \"%s\"", parts[0], p.Location)
		}

		s = strings.Join(parts[:3], "/")
	} else {
		repo := []string{}

		for _, p := range parts {
			repo = append(repo, p)

			if strings.HasSuffix(p, ".git") {
				break
			}
		}

		s = strings.Join(repo, "/")
	}

	return s, nil
}
