package tracetools

import (
	"bytes"
	"encoding/base64"
	"encoding/gob"
	"encoding/json"

	"github.com/opentracing/opentracing-go"
	"gopkg.in/DataDog/dd-trace-go.v1/ddtrace/tracer"
)

// EnvVarTraceContextKey is the env var key that will be used to store/retrieve the
// encoded trace context information into env var maps.
const EnvVarTraceContextKey = "BUILDKITE_TRACE_CONTEXT"

// EncodeTraceContext will serialize and encode tracing data into a string and place
// it into the given env vars map.
func EncodeTraceContext(span opentracing.Span, env map[string]string, useJsonTraceContext bool) error {
	textmap := tracer.TextMapCarrier{}
	if err := span.Tracer().Inject(span.Context(), opentracing.TextMap, &textmap); err != nil {
		return err
	}

	buf := bytes.NewBuffer([]byte{})
	// Unfortunately there's no general Encoder interface so we can't DRY the Encode call.
	var err error
	if useJsonTraceContext {
		err = json.NewEncoder(buf).Encode(textmap)
	} else {
		err = gob.NewEncoder(buf).Encode(textmap)
	}
	if err != nil {
		return err
	}

	env[EnvVarTraceContextKey] = base64.URLEncoding.EncodeToString(buf.Bytes())
	return nil
}

// DecodeTraceContext will decode, deserialize, and extract the tracing data from the
// given env var map.
func DecodeTraceContext(env map[string]string, useJsonTraceContext bool) (opentracing.SpanContext, error) {
	s, has := env[EnvVarTraceContextKey]
	if !has {
		return nil, opentracing.ErrSpanContextNotFound
	}

	contextBytes, err := base64.URLEncoding.DecodeString(s)
	if err != nil {
		return nil, err
	}

	buf := bytes.NewBuffer(contextBytes)
	textmap := opentracing.TextMapCarrier{}
	// Unfortunately there's no general Decoder interface so we can't DRY the Decode call.
	if useJsonTraceContext {
		err = json.NewDecoder(buf).Decode(&textmap)
	} else {
		err = gob.NewDecoder(buf).Decode(&textmap)
	}
	if err != nil {
		return nil, err
	}

	return opentracing.GlobalTracer().Extract(opentracing.TextMap, textmap)
}
