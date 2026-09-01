package router

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/dash-xd/github-device-auth/internal/ghdeviceflow"
)

type pollRequest struct {
	DeviceCode string `json:"device_code"`
}

func handlePoll(w http.ResponseWriter, r *http.Request) {
	clientID := githubClientID(r)

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

	ctx, cancel := context.WithTimeout(r.Context(), 14*time.Minute)
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

	writeJSON(w, http.StatusOK, token)
}

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
		return http.StatusBadGateway, err.Error()
	}
}
