package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPipelineParser(t *testing.T) {
	var err error
	var result interface{}
	var j []byte

	os.Setenv("ENV_VAR_FRIEND", "\"friend\"")

	// It parses pipelines with .yml filenames
	result, err = PipelineParser{Filename: "awesome.yml", Pipeline: []byte("steps:\n  - label: \"hello ${ENV_VAR_FRIEND}\"")}.Parse()
	assert.Nil(t, err)
	j, err = json.Marshal(result)
	assert.Equal(t, `{"steps":[{"label":"hello \"friend\""}]}`, string(j))

	// It parses complex YAML files
	result, err = PipelineParser{Filename: "awesome.yml", Pipeline: []byte(`base_step: &base_step
  type: script
  agent_query_rules:
    - queue=default

steps:
- <<: *base_step
  name: ':docker: building image'
  command: docker build .
  agents:
    queue: default`)}.Parse()
	assert.Nil(t, err)
	j, err = json.Marshal(result)
	assert.Equal(t, `{"base_step":{"agent_query_rules":["queue=default"],"type":"script"},"steps":[{"agent_query_rules":["queue=default"],"agents":{"queue":"default"},"command":"docker build .","name":":docker: building image","type":"script"}]}`, string(j))

	// It parses pipelines with .yaml filenames
	result, err = PipelineParser{Filename: "awesome.yaml", Pipeline: []byte("steps:\n  - label: \"hello ${ENV_VAR_FRIEND}\"")}.Parse()
	assert.Nil(t, err)
	j, err = json.Marshal(result)
	assert.Equal(t, `{"steps":[{"label":"hello \"friend\""}]}`, string(j))

	// Returns YAML parsing errors
	result, err = PipelineParser{Filename: "awesome.yml", Pipeline: []byte("steps: %blah%")}.Parse()
	assert.NotNil(t, err)
	assert.Equal(t, `Failed to parse YAML: found character that cannot start any token`, fmt.Sprintf("%s", err))

	// Returns JSON parsing errors
	result, err = PipelineParser{Filename: "awesome.json", Pipeline: []byte("{")}.Parse()
	assert.NotNil(t, err)
	assert.Equal(t, `Failed to parse JSON: unexpected end of JSON input`, fmt.Sprintf("%s", err))

	// It parses pipelines with .json filenames
	result, err = PipelineParser{Filename: "thing.json", Pipeline: []byte("\n\n     \n  { \"foo\": \"bye ${ENV_VAR_FRIEND}\" }\n")}.Parse()
	assert.Nil(t, err)
	j, err = json.Marshal(result)
	assert.Equal(t, `{"foo":"bye \"friend\""}`, string(j))

	// It parses unknown YAML
	result, err = PipelineParser{Pipeline: []byte("steps:\n  - label: \"hello ${ENV_VAR_FRIEND}\"")}.Parse()
	assert.Nil(t, err)
	j, err = json.Marshal(result)
	assert.Equal(t, `{"steps":[{"label":"hello \"friend\""}]}`, string(j))

	// Returns YAML parsing errors if the content looks like YAML
	result, err = PipelineParser{Pipeline: []byte("steps: %blah%")}.Parse()
	assert.NotNil(t, err)
	assert.Equal(t, `Failed to parse YAML: found character that cannot start any token`, fmt.Sprintf("%s", err))

	// It parses unknown JSON objects
	result, err = PipelineParser{Pipeline: []byte("\n\n     \n  { \"foo\": \"bye ${ENV_VAR_FRIEND}\" }\n")}.Parse()
	assert.Nil(t, err)
	j, err = json.Marshal(result)
	assert.Equal(t, `{"foo":"bye \"friend\""}`, string(j))

	// It parses unknown JSON arrays
	result, err = PipelineParser{Pipeline: []byte("\n\n     \n  [ { \"foo\": \"bye ${ENV_VAR_FRIEND}\" } ]\n")}.Parse()
	assert.Nil(t, err)
	j, err = json.Marshal(result)
	assert.Equal(t, `[{"foo":"bye \"friend\""}]`, string(j))

	// Returns JSON parsing errors if the content looks like JSON
	result, err = PipelineParser{Pipeline: []byte("{")}.Parse()
	assert.NotNil(t, err)
	assert.Equal(t, `Failed to parse JSON: unexpected end of JSON input`, fmt.Sprintf("%s", err))
}
