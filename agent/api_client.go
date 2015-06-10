package agent

import (
	"net/url"
	"runtime"
	"time"

	"github.com/buildkite/agent/api"
	"github.com/buildkite/agent/logger"
	"github.com/facebookgo/httpcontrol"
)

var debug = false

type APIClient struct {
	Endpoint   string
	Token      string
	StatusFunc func(status string, timeTaken time.Duration)
}

func APIClientEnableHTTPDebug() {
	debug = true
}

func (a APIClient) Create() *api.Client {
	// Create the transport used when making the Buildkite Agent API calls
	transport := &api.AuthenticatedTransport{
		Token: a.Token,
		Transport: &httpcontrol.Transport{
			DialTimeout:           2 * time.Minute,
			ResponseHeaderTimeout: 2 * time.Minute,
			RequestTimeout:        2 * time.Minute,
			RetryAfterTimeout:     false,
			DisableCompression:    false,
			Stats: func(s *httpcontrol.Stats) {
				logger.Info("%s (%s)", s, s.Duration.Header+s.Duration.Body)
				if a.StatusFunc != nil {
					a.StatusFunc(s.String(), s.Duration.Header+s.Duration.Body)
				}
			},
		},
	}

	// Create the Buildkite Agent API Client
	client := api.NewClient(transport.Client())
	client.BaseURL, _ = url.Parse(a.Endpoint)
	client.UserAgent = a.UserAgent()
	client.Debug = debug
	client.Logger = func(s string, err error) {
		logger.Debug(s)
	}

	return client
}

func (a APIClient) UserAgent() string {
	return "buildkite-agent/" + Version() + " (" + runtime.GOOS + "; " + runtime.GOARCH + ")"
}
