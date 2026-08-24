package router

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"time"

	"github.com/dash-xd/github-device-auth/internal/ghdeviceflow"
)

type pollRequest struct {
	DeviceCode string `json:"device_code"`
}

func handlePoll(w http.ResponseWriter, r *http.Request) {
	clientID := os.Getenv("GITHUB_CLIENT_ID")

	if clientID == "" {
		http.Error(
			w,
			"GitHub client ID is not configured",
			http.StatusInternalServerError,
		)
		return
	}

	var request pollRequest

	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(
			w,
			"invalid JSON request",
			http.StatusBadRequest,
		)
		return
	}

	if request.DeviceCode == "" {
		http.Error(
			w,
			"device_code is required",
			http.StatusBadRequest,
		)
		return
	}

	/*
		The device flow normally expires after 900 seconds.

		Use a server-side timeout so a function invocation can never
		poll indefinitely.

		The deployed Cloud Function should have a timeout greater than
		this value.
	*/
	ctx, cancel := context.WithTimeout(
		r.Context(),
		14*time.Minute,
	)
	defer cancel()

	token, err := ghdeviceflow.PollForToken(
		ctx,
		clientID,
		request.DeviceCode,
		5*time.Second,
	)
	if err != nil {
		status, message := pollErrorResponse(err)
		http.Error(w, message, status)
		return
	}

	writeJSON(
		w,
		http.StatusOK,
		token,
	)
}

// pollErrorResponse maps a PollForToken error to the HTTP status and
// message it should produce. Shared with handleDeviceFull, which can
// only use the message half - its response's status is already 200 by
// the time polling can fail, since it's mid-SSE-stream.
func pollErrorResponse(err error) (status int, message string) {
	switch {
	case errors.Is(err, ghdeviceflow.ErrExpiredToken):
		return http.StatusGone, "GitHub device code expired"

	case errors.Is(err, ghdeviceflow.ErrAccessDenied):
		return http.StatusForbidden, "GitHub authorization was denied"

	case errors.Is(err, context.DeadlineExceeded):
		return http.StatusGatewayTimeout, "GitHub authorization polling timed out"

	case errors.Is(err, context.Canceled):
		return http.StatusRequestTimeout, "request canceled"

	default:
		// PollForToken's own default case already wraps GitHub's error
		// and error_description into err's text (e.g. "github
		// authentication failed: incorrect_client_credentials: ...") -
		// surface that instead of a fixed string, so a caller can tell
		// *why* GitHub rejected the poll (wrong client_id/secret,
		// Device Flow not enabled on the app, etc.) instead of hitting
		// a dead end.
		return http.StatusBadGateway, err.Error()
	}
}
