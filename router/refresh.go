package router

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"time"

	"github.com/dash-xd/github-device-auth/internal/ghdeviceflow"
	"github.com/dash-xd/github-device-auth/internal/tokencache"
)

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

func handleRefresh(w http.ResponseWriter, r *http.Request) {
	clientID := os.Getenv("GITHUB_CLIENT_ID")

	if clientID == "" {
		http.Error(
			w,
			"GitHub client ID is not configured",
			http.StatusInternalServerError,
		)
		return
	}

	// GITHUB_CLIENT_SECRET is optional: refresh tokens issued via the
	// device flow belong to a public client and refresh without one.
	clientSecret := os.Getenv("GITHUB_CLIENT_SECRET")

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

	// Validated before calling GitHub: a device-flow refresh token is
	// single-use and rotates on every exchange, so a cache misconfiguration
	// caught only after the exchange would mean the old refresh token is
	// already burned and the newly issued one has nowhere to go.
	bucket, cacheKey, cacheRequested, ok := parseCacheRequest(w, r)
	if !ok {
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

		case errors.Is(err, ghdeviceflow.ErrIncorrectClientCredentials):
			http.Error(
				w,
				"GitHub refresh token requires client credentials this request did not provide",
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

	if cacheRequested {
		if err := tokencache.Store(ctx, bucket, cacheKey, token); err != nil {
			http.Error(
				w,
				"failed to cache GitHub token",
				http.StatusBadGateway,
			)
			return
		}

		writeJSON(
			w,
			http.StatusOK,
			cacheConfirmation{
				Cached: true,
				Bucket: bucket,
				Object: cacheKey,
			},
		)
		return
	}

	writeJSON(
		w,
		http.StatusOK,
		token,
	)
}
