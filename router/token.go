package router

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/dash-xd/github-device-auth/internal/ghdeviceflow"
)

// handleToken serves the current cached GitHub access token.
// RequireValidCachedToken (see router/tokenmiddleware.go) has already
// guaranteed a currently-valid token is available by the time this
// runs - refreshing it first if it was expired but the cached refresh
// token wasn't - so there is nothing left for this handler to do but
// return it.
func handleToken(w http.ResponseWriter, r *http.Request) {
	cached, ok := cachedTokenFromContext(r.Context())
	if !ok {
		// Unreachable in practice: RequireValidCachedToken never calls
		// next without first placing a valid token in the context, or
		// else it writes a failure response itself and returns before
		// reaching next at all. Guarded anyway rather than assumed, so
		// a future routing mistake (this handler mounted without the
		// middleware) fails loudly instead of panicking on a zero
		// value.
		http.Error(
			w,
			"no valid cached GitHub token available",
			http.StatusInternalServerError,
		)
		return
	}

	writeJSON(w, http.StatusOK, cached.publicResponse())
}

// ensureFreshToken refreshes cached's access token using its stored
// refresh token, updates the cache with the result, and returns the
// refreshed token. On any failure - the refresh request itself, or
// writing the new value back to the cache - it writes the appropriate
// error response to w itself and returns ok=false; the caller must
// return immediately without writing anything else, the same contract
// parseCacheRequest uses elsewhere in this package.
func ensureFreshToken(
	ctx context.Context,
	w http.ResponseWriter,
	clientID, clientSecret, bucket, key string,
	cached cachedToken,
) (refreshed cachedToken, ok bool) {
	token, err := ghdeviceflow.RefreshAccessToken(
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
				return retry, true
			}
		}

		status, message := refreshErrorResponse(err)
		http.Error(w, message, status)
		return cachedToken{}, false
	}

	newCached := newCachedToken(token, time.Now())

	if err := storeCachedToken(ctx, bucket, key, newCached); err != nil {
		http.Error(
			w,
			"refreshed the GitHub token but failed to update the cache",
			http.StatusBadGateway,
		)
		return cachedToken{}, false
	}

	return newCached, true
}
