package agent

import (
	"context"
	"regexp"
	"strconv"
	"sync"
	"time"

	"github.com/buildkite/agent/v3/logger"
	"github.com/buildkite/agent/v3/status"
)

// If you change header parsing here make sure to change it in the
// buildkite.com frontend logic, too

var (
	headerRE          = regexp.MustCompile(`^(---|\+\+\+|~~~)\s`)
	headerExpansionRE = regexp.MustCompile(`^\^\^\^\s\+\+\+`)
	ansiColourRE      = regexp.MustCompile(`\x1b\[([;\d]+)?[mK]`)
)

type headerTimesStreamer struct {
	// The logger instance to use
	logger logger.Logger

	// The callback that will be called when a header time is ready for
	// upload
	uploadCallback func(context.Context, int, int, map[string]string)

	// The times that have found while scanning lines
	times      []string
	timesMutex sync.Mutex

	// Every time we get a new time, we increment the wait group, and
	// decrement it after it has been uploaded.
	uploadWaitGroup sync.WaitGroup

	// Every time we go to scan a line, we increment the wait group, then
	// decrement after it's finished scanning. That way when we stop the
	// streamer, we can wait for all the lines to finish scanning first.
	scanWaitGroup sync.WaitGroup

	// A boolean to keep track if we're currently streaming header times
	streaming      bool
	streamingMutex sync.Mutex

	// We store the last index we uploaded to, so we don't have to keep
	// uploading the same times
	cursor int
}

func newHeaderTimesStreamer(l logger.Logger, upload func(context.Context, int, int, map[string]string)) *headerTimesStreamer {
	return &headerTimesStreamer{
		logger:         l,
		uploadCallback: upload,
	}
}

func (h *headerTimesStreamer) Run(ctx context.Context) {
	ctx, setStatus, done := status.AddSimpleItem(ctx, "Header Times Streamer")
	defer done()
	setStatus("🏃 Starting...")

	h.streamingMutex.Lock()
	h.streaming = true
	h.streamingMutex.Unlock()

	h.logger.Debug("[HeaderTimesStreamer] Streamer has started...")

	for {
		// Break out of streaming if it's finished. We also
		// need to acquire a read lock on the flag because it
		// can be modified by other routines.
		h.streamingMutex.Lock()
		if !h.streaming {
			break
		}
		h.streamingMutex.Unlock()

		setStatus("📡 Uploading any pending header times")

		// Upload any pending header times
		h.Upload(ctx)

		setStatus("😴 Sleeping for a bit")

		// Sleep for a second and try upload some more later
		select {
		case <-ctx.Done():
			return
		case <-time.After(1 * time.Second):
		}
	}

	h.logger.Debug("[HeaderTimesStreamer] Streamer has finished...")
}

// Scan takes a line of log output and tracks a time if it's a header.
// Returns true for header lines or header expansion lines.
func (h *headerTimesStreamer) Scan(line string) bool {
	// Keep track of how many line scans we need to do
	h.scanWaitGroup.Add(1)
	defer h.scanWaitGroup.Done()

	// Make sure all ANSI colours are removed from the string before we
	// check to see if it's a header (sometimes a colour escape sequence may
	// be the first thing on the line, which will cause the regex to ignore it)
	line = ansiColourRE.ReplaceAllString(line, "")

	if !headerRE.MatchString(line) {
		// It's not a header, but could be a header expansion.
		return headerExpansionRE.MatchString(line)
	}

	h.logger.Debug("[HeaderTimesStreamer] Found header %q", line)

	// Acquire a lock on the times and then add the current time to
	// our times slice.
	h.timesMutex.Lock()
	h.times = append(h.times, time.Now().UTC().Format(time.RFC3339Nano))
	h.timesMutex.Unlock()

	// Add the time to the wait group
	h.uploadWaitGroup.Add(1)
	return true
}

func (h *headerTimesStreamer) Upload(ctx context.Context) {
	// Store the current cursor value
	c := h.cursor

	// Grab only the times that we haven't uploaded yet. We need to acquire
	// a lock since other routines may be adding to it.
	h.timesMutex.Lock()
	length := len(h.times)
	times := h.times[h.cursor:length]
	h.timesMutex.Unlock()

	// Construct the payload to send to the server
	payload := map[string]string{}
	for index, time := range times {
		payload[strconv.Itoa(h.cursor+index)] = time
	}

	// Save the cursor we're up to
	h.cursor = length

	// How many times are we uploading this time
	timesToUpload := len(times)

	// Do we even have some times to upload
	if timesToUpload == 0 {
		return
	}

	// Call our callback with the times for upload
	h.logger.Debug("[HeaderTimesStreamer] Uploading header times %d..%d", c, length-1)
	h.uploadCallback(ctx, c, length, payload)
	h.logger.Debug("[HeaderTimesStreamer] Finished uploading header times %d..%d", c, length-1)

	// Decrement the wait group for every time we've uploaded.
	h.uploadWaitGroup.Add(timesToUpload * -1)
}

func (h *headerTimesStreamer) Stop() {
	h.logger.Debug("[HeaderTimesStreamer] Waiting for all the lines to be scanned")
	h.scanWaitGroup.Wait()

	h.logger.Debug("[HeaderTimesStreamer] Waiting for all the header times to be uploaded")
	h.uploadWaitGroup.Wait()

	// Since we're modifying the waitGroup and the streaming flag, we need
	// to acquire a write lock.
	h.streamingMutex.Lock()
	h.streaming = false
	h.streamingMutex.Unlock()
}
