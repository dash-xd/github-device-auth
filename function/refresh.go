package function

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"time"

	ghdeviceflow "github.com/dash-xd/github-device-auth"
)

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

func handleRefresh(w http.ResponseWriter, r *http.Request) {
	clientID := os.Getenv("GITHUB_CLIENT_ID")
	clientSecret := os.Getenv("GITHUB_CLIENT_SECRET")

	if clientID == "" || clientSecret == "" {
		http.Error(
			w,
			"GitHub client credentials are not configured",
			http.StatusInternalServerError,
		)
		return
	}

	var request refreshRequest

	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(
			w,
			"invalid JSON request",
			http.StatusBadRequest,
		)
		return
	}

	if request.RefreshToken == "" {
		http.Error(
			w,
			"refresh_token is required",
			http.StatusBadRequest,
		)
		return
	}

	ctx, cancel := context.WithTimeout(
		r.Context(),
		15*time.Second,
	)
	defer cancel()

	token, err := ghdeviceflow.RefreshAccessToken(
		ctx,
		clientID,
		clientSecret,
		request.RefreshToken,
	)
	if err != nil {
		switch {
		case errors.Is(err, ghdeviceflow.ErrInvalidRefreshToken):
			http.Error(
				w,
				"GitHub refresh token is invalid or expired",
				http.StatusUnauthorized,
			)

		case errors.Is(err, context.DeadlineExceeded):
			http.Error(
				w,
				"GitHub token refresh timed out",
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
				"GitHub token refresh failed",
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
