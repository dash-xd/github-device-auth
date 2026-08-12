package router

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"time"

	ghdeviceflow "github.com/dash-xd/github-device-auth"
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
		switch {
		case errors.Is(err, ghdeviceflow.ErrExpiredToken):
			http.Error(
				w,
				"GitHub device code expired",
				http.StatusGone,
			)

		case errors.Is(err, ghdeviceflow.ErrAccessDenied):
			http.Error(
				w,
				"GitHub authorization was denied",
				http.StatusForbidden,
			)

		case errors.Is(err, context.DeadlineExceeded):
			http.Error(
				w,
				"GitHub authorization polling timed out",
				http.StatusGatewayTimeout,
			)

		case errors.Is(err, context.Canceled):
			http.Error(
				w,
				"request canceled",
				http.StatusRequestTimeout,
			)

		default:
			http.Error(
				w,
				"GitHub authentication failed",
				http.StatusBadGateway,
			)
		}

		return
	}

	writeJSON(
		w,
		http.StatusOK,
		token,
	)
}
