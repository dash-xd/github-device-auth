package router

import (
	"context"
	"errors"
	"net/http"
	"os"
	"time"

	"cloud.google.com/go/storage"

	"github.com/dash-xd/github-device-auth/internal/ghdeviceflow"
)

// handleToken serves the current cached GitHub access token, refreshing
// it first if it's expired but the cached refresh token isn't. Unlike
// handleRefresh, this takes no request body and never fails a genuine
// double-refresh race back to the caller: the goal here is "hand me a
// usable access token," and if a concurrent caller already refreshed it
// for us, that goal is already met.
func handleToken(w http.ResponseWriter, r *http.Request) {
	clientID := os.Getenv("GITHUB_CLIENT_ID")

	if clientID == "" {
		http.Error(
			w,
			"GitHub client ID is not configured",
			http.StatusInternalServerError,
		)
		return
	}

	bucket, err := resolveCacheBucket(r.Context())
	if err != nil {
		http.Error(
			w,
			"token cache bucket is not configured: "+err.Error(),
			http.StatusInternalServerError,
		)
		return
	}

	// GITHUB_CLIENT_SECRET is optional: refresh tokens issued via the
	// device flow belong to a public client and refresh without one.
	clientSecret := os.Getenv("GITHUB_CLIENT_SECRET")

	key := cacheObjectKey(clientID)

	ctx, cancel := context.WithTimeout(
		r.Context(),
		15*time.Second,
	)
	defer cancel()

	cached, err := loadCachedToken(ctx, bucket, key)
	if err != nil {
		if errors.Is(err, storage.ErrObjectNotExist) {
			http.Error(
				w,
				"no cached GitHub token found; run the device flow first",
				http.StatusNotFound,
			)
			return
		}

		http.Error(
			w,
			"failed to read cached GitHub token",
			http.StatusBadGateway,
		)
		return
	}

	switch decideTokenAction(cached, time.Now()) {
	case actionServeCached:
		writeJSON(w, http.StatusOK, cached.publicResponse())

	case actionReauthRequired:
		http.Error(
			w,
			"cached GitHub refresh token is expired; run the device flow again",
			http.StatusUnauthorized,
		)

	case actionRefresh:
		refreshAndServe(ctx, w, clientID, clientSecret, bucket, key, cached)
	}
}

func refreshAndServe(
	ctx context.Context,
	w http.ResponseWriter,
	clientID, clientSecret, bucket, key string,
	cached cachedToken,
) {
	refreshed, err := ghdeviceflow.RefreshAccessToken(
		ctx,
		clientID,
		clientSecret,
		cached.RefreshToken,
	)
	if err != nil {
		if errors.Is(err, ghdeviceflow.ErrInvalidRefreshToken) {
			// Most likely a concurrent caller already refreshed and
			// rotated this exact refresh token out from under us.
			// Re-read once: if the cache now holds a different refresh
			// token and a currently-valid access token, the goal (a
			// usable access token) is already satisfied - use it
			// instead of failing. This leniency is specific to this
			// cache-driven path; the explicit /auth/github/refresh
			// endpoint (a caller-supplied refresh_token) never does
			// this, since a real double-refresh conflict there is the
			// correct signal to surface, not paper over.
			if retry, retryErr := loadCachedToken(ctx, bucket, key); retryErr == nil &&
				retry.RefreshToken != cached.RefreshToken &&
				retry.AccessTokenValid(time.Now(), accessTokenRefreshBuffer) {
				writeJSON(w, http.StatusOK, retry.publicResponse())
				return
			}
		}

		status, message := refreshErrorResponse(err)
		http.Error(w, message, status)
		return
	}

	newCached := newCachedToken(refreshed, time.Now())

	if err := storeCachedToken(ctx, bucket, key, newCached); err != nil {
		http.Error(
			w,
			"refreshed the GitHub token but failed to update the cache",
			http.StatusBadGateway,
		)
		return
	}

	writeJSON(w, http.StatusOK, newCached.publicResponse())
}
