package router

import (
	"context"
	"net/http"
	"time"

	"github.com/dash-xd/github-device-auth/internal/ghdeviceflow"
)

// sseKeepAliveInterval is how often handleDeviceFull sends an SSE
// comment line while waiting on the poll goroutine, so intermediary
// proxies/load balancers don't treat the connection as idle and close it
// during a long-running device-flow authorization.
const sseKeepAliveInterval = 15 * time.Second

type pollOutcome struct {
	token *ghdeviceflow.TokenResponse
	err   error
}

// handleDeviceFull is the full device-flow-over-SSE variant of
// handleDevice: it starts the device flow, streams the device code and
// verification instructions to the client immediately, polls for the
// resulting token in the background, and streams the outcome back on
// the same connection - either the token itself, or (if cache was
// requested) confirmation that it was written to the configured GCS
// bucket instead.
func handleDeviceFull(w http.ResponseWriter, r *http.Request) {
	clientID := githubClientID(r)

	if clientID == "" {
		http.Error(
			w,
			"GitHub client ID is not configured",
			http.StatusInternalServerError,
		)
		return
	}

	bucket, cacheKey, cacheRequested, ok := parseCacheRequest(w, r, clientID)
	if !ok {
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(
			w,
			"streaming is not supported",
			http.StatusInternalServerError,
		)
		return
	}

	deviceCtx, cancel := context.WithTimeout(
		r.Context(),
		15*time.Second,
	)
	device, err := ghdeviceflow.RequestDeviceCode(
		deviceCtx,
		clientID,
	)
	cancel()
	if err != nil {
		http.Error(
			w,
			"failed to start GitHub device flow",
			http.StatusBadGateway,
		)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	if err := writeSSEEvent(w, flusher, "device_code", newDeviceResponse(device)); err != nil {
		return
	}

	/*
		Same 14-minute ceiling as handlePoll: the device flow normally
		expires after 900 seconds, and a server-side timeout keeps this
		invocation from polling forever. The deployed Cloud Function
		should have a timeout greater than this value.
	*/
	pollCtx, pollCancel := context.WithTimeout(
		r.Context(),
		14*time.Minute,
	)
	defer pollCancel()

	outcomeCh := make(chan pollOutcome, 1)

	go func() {
		token, err := ghdeviceflow.PollForToken(
			pollCtx,
			clientID,
			device.DeviceCode,
			5*time.Second,
		)
		outcomeCh <- pollOutcome{token, err}
	}()

	keepAlive := time.NewTicker(sseKeepAliveInterval)
	defer keepAlive.Stop()

	for {
		select {
		case outcome := <-outcomeCh:
			writeDeviceFullOutcome(pollCtx, w, flusher, outcome, bucket, cacheKey, cacheRequested)
			return

		case <-keepAlive.C:
			if _, err := w.Write([]byte(": keepalive\n\n")); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func writeDeviceFullOutcome(
	ctx context.Context,
	w http.ResponseWriter,
	flusher http.Flusher,
	outcome pollOutcome,
	bucket, cacheKey string,
	cacheRequested bool,
) {
	if outcome.err != nil {
		_, message := pollErrorResponse(outcome.err)
		_ = writeSSEEvent(w, flusher, "error", sseError{Message: message})
		return
	}

	if !cacheRequested {
		_ = writeSSEEvent(w, flusher, "token", outcome.token)
		return
	}

	if err := storeCachedToken(ctx, bucket, cacheKey, newCachedToken(outcome.token, time.Now())); err != nil {
		_ = writeSSEEvent(w, flusher, "error", sseError{Message: "failed to cache GitHub token"})
		return
	}

	_ = writeSSEEvent(w, flusher, "cached", cacheConfirmation{
		Cached: true,
		Bucket: bucket,
		Object: cacheKey,
	})
}

type sseError struct {
	Message string `json:"message"`
}
