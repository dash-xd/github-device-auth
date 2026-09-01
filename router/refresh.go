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

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

func handleRefresh(w http.ResponseWriter, r *http.Request) {
	clientID := githubClientID(r)

	if clientID == "" {
		http.Error(
			w,
			"GitHub client ID is not configured",
			http.StatusInternalServerError,
		)
		return
	}

	clientSecret := os.Getenv("GITHUB_CLIENT_SECRET")

	var request refreshRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "invalid JSON request", http.StatusBadRequest)
		return
	}
	if request.RefreshToken == "" {
		http.Error(w, "refresh_token is required", http.StatusBadRequest)
		return
	}

	bucket, cacheKey, cacheRequested, ok := parseCacheRequest(w, r, clientID)
	if !ok {
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	token, err := ghdeviceflow.RefreshAccessToken(ctx, clientID, clientSecret, request.RefreshToken)
	if err != nil {
		status, message := refreshErrorResponse(err)
		http.Error(w, message, status)
		return
	}

	if cacheRequested {
		if err := storeCachedToken(ctx, bucket, cacheKey, newCachedToken(token, time.Now())); err != nil {
			http.Error(w, "failed to cache GitHub token", http.StatusBadGateway)
			return
		}
		writeJSON(w, http.StatusOK, cacheConfirmation{Cached: true, Bucket: bucket, Object: cacheKey})
		return
	}

	writeJSON(w, http.StatusOK, token)
}

func refreshErrorResponse(err error) (status int, message string) {
	switch {
	case errors.Is(err, ghdeviceflow.ErrInvalidRefreshToken):
		return http.StatusUnauthorized, "GitHub refresh token is invalid or expired; run the device flow again"
	case errors.Is(err, ghdeviceflow.ErrIncorrectClientCredentials):
		return http.StatusUnauthorized, "GitHub refresh token requires client credentials this request did not provide"
	case errors.Is(err, context.DeadlineExceeded):
		return http.StatusGatewayTimeout, "GitHub token refresh timed out"
	case errors.Is(err, context.Canceled):
		return http.StatusRequestTimeout, "request canceled"
	default:
		return http.StatusBadGateway, "GitHub token refresh failed"
	}
}
