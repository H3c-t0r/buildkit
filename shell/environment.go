package shell

import (
	"fmt"
	"io/ioutil"
	"regexp"
	"runtime"
	"sort"
	"strings"
)

type Environment struct {
	env map[string]string
}

var EnvironmentVariableLineRegex = regexp.MustCompile("\\A([a-zA-Z]+[a-zA-Z0-9_]*)=(.+)\\z")

// Creates a new environment from a string slice of KEY=VALUE
func EnvironmentFromSlice(s []string) *Environment {
	env := &Environment{env: make(map[string]string)}

	var currentKeyName string
	var currentValueSlice []string

	for _, l := range s {
		//parts := strings.SplitN(l, "=", 2)

		matches := EnvironmentVariableLineRegex.FindStringSubmatch(l)
		if len(matches) == 3 {

			if currentKeyName != "" && len(currentValueSlice) > 0 {
				env.Set(currentKeyName, strings.Join(currentValueSlice, "\n"))
				currentValueSlice = nil
			}

			currentKeyName = matches[1]
			currentValueSlice = append(currentValueSlice, matches[2])
		} else {
			if currentKeyName != "" {
				currentValueSlice = append(currentValueSlice, l)
			}
		}

		//if len(parts) == 2 {
		//	env.Set(parts[0], parts[1])
		//}
	}

	fmt.Println(currentKeyName)
	fmt.Println(currentValueSlice)

	if currentKeyName != "" && len(currentValueSlice) > 0 {
		env.Set(currentKeyName, strings.Join(currentValueSlice, "\n"))
	}

	return env
}

// Creates a new environment from a file with format KEY=VALUE\n
func EnvironmentFromFile(path string) (*Environment, error) {
	body, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}

	return EnvironmentFromSlice(strings.Split(string(body), "\n")), nil
}

// Returns a key from the environment
func (e *Environment) Get(key string) string {
	return e.env[key]
}

// Returns true/false depending on whether or not the key exists in the env
func (e *Environment) Exists(key string) bool {
	_, ok := e.env[key]

	return ok
}

// Sets a key in the environment
func (e *Environment) Set(key string, value string) string {
	// Trim the values
	key = strings.TrimSpace(key)
	value = strings.TrimSpace(value)

	// Check if we've got quoted values
	if (strings.HasPrefix(value, `"`) && strings.HasSuffix(value, `"`)) ||
		(strings.HasPrefix(value, "'") && strings.HasSuffix(value, "'")) {
		// Pull the quotes off the edges
		value = strings.Trim(value, "\"'")

		// Expand quotes
		value = strings.Replace(value, "\\\"", "\"", -1)

		// Expand newlines
		value = strings.Replace(value, "\\n", "\n", -1)
	}

	// Environment variable keys are case-insensitive on Windows, so we'll
	// just convert them all to uppercase.
	if runtime.GOOS == "windows" {
		key = strings.ToUpper(key)
	}

	e.env[key] = value

	return value
}

// Remove a key from the Environment and return it's value
func (e *Environment) Remove(key string) string {
	value := e.Get(key)
	delete(e.env, key)
	return value
}

// Returns the length of the environment
func (e *Environment) Length() int {
	return len(e.env)
}

// Returns a new environment with all the variables that have changed
func (e *Environment) Diff(other *Environment) *Environment {
	diff := &Environment{env: make(map[string]string)}

	for k, v := range e.env {
		if other.Get(k) != v {
			diff.Set(k, v)
		}
	}

	return diff
}

// Merges another env into this one and returns the result
func (e *Environment) Merge(other *Environment) *Environment {
	c := e.Copy()

	for k, v := range other.ToMap() {
		c.Set(k, v)
	}

	return c
}

// Returns a copy of the env
func (e *Environment) Copy() *Environment {
	c := make(map[string]string)

	for k, v := range e.env {
		c[k] = v
	}

	return &Environment{env: c}
}

// Returns a slice representation of the environment
func (e *Environment) ToSlice() []string {
	s := []string{}
	for k, v := range e.env {
		s = append(s, fmt.Sprintf("%v=%v", k, v))
	}

	// Ensure they are in a consistent order (helpful for tests)
	sort.Strings(s)

	return s
}

// Returns a map representation of the environment
func (e *Environment) ToMap() map[string]string {
	return e.env
}
