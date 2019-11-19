package agent

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/buildkite/agent/v3/api"
	"github.com/buildkite/agent/v3/logger"
	"github.com/stretchr/testify/assert"
)

func TestArtifactDownloaderConnectsToEndpoint(t *testing.T) {
	defer os.Remove("llamas.txt")

	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case `/builds/my-build/artifacts/search`:
			fmt.Fprintf(rw, `[{
				"id": "4600ac5c-5a13-4e92-bb83-f86f218f7b32",
				"file_size": 3,
				"absolute_path": "llamas.txt",
				"path": "llamas.txt",
				"url": "http://%s/download"
			}]`, req.Host)
		case `/download`:
			fmt.Fprintln(rw, "OK")
		default:
			http.Error(rw, "Not found", http.StatusNotFound)
		}
	}))
	defer server.Close()

	ac := api.NewClient(logger.Discard, api.Config{
		Endpoint: server.URL,
		Token:    `llamasforever`,
	})

	d := NewArtifactDownloader(logger.Discard, ac, ArtifactDownloaderConfig{
		Destination: ".",
		BuildID:     "my-build",
	})

	err := d.Download()
	if err != nil {
		t.Fatal(err)
	}
}

func TestArtifactDownloaderHandlesAbsolutePaths(t *testing.T) {
	defer os.RemoveAll("/tmp/agent-artifact-download")

	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case `/builds/my-build/artifacts/search`:
			fmt.Fprintf(rw, `[{
				"id": "4600ac5c-5a13-4e92-bb83-f86f218f7b32",
				"file_size": 3,
				"absolute_path": "/tmp/agent-artifact-download/llamas.txt",
				"path": "/tmp/agent-artifact-download/llamas.txt",
				"url": "http://%s/download"
			}]`, req.Host)
		case `/download`:
			fmt.Fprintln(rw, "OK")
		default:
			http.Error(rw, "Not found", http.StatusNotFound)
		}
	}))
	defer server.Close()

	ac := api.NewClient(logger.Discard, api.Config{
		Endpoint: server.URL,
		Token:    `llamasforever`,
	})

	d := NewArtifactDownloader(logger.Discard, ac, ArtifactDownloaderConfig{
		Destination: ".",
		BuildID:     "my-build",
	})

	err := d.Download()
	if err != nil {
		t.Fatal(err)
	}

	assert.FileExists(t, "/tmp/agent-artifact-download/llamas.txt")
}
