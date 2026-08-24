package router

import (
	"context"
	"errors"
	"net/http"
	"os"
	"time"

	"cloud.google.com/go/storage"
)

// cachedTokenContextKey is unexported so only this package can place or
// retrieve a value under it - a caller can't spoof
// cachedTokenFromContext by setting the same key from outside.
type cachedTokenContextKey struct{}

// cachedTokenFromContext retrieves the cachedToken RequireValidCachedToken
// placed in the request context. ok is false only if called on a request
// that didn't go through that middleware - every request that did is
// guaranteed one, since the middleware never calls next without it.
func cachedTokenFromContext(ctx context.Context) (cachedToken, bool) {
	token, ok := ctx.Value(cachedTokenContextKey{}).(cachedToken)
	return token, ok
}

func withCachedToken(r *http.Request, token cachedToken) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), cachedTokenContextKey{}, token))
}

// RequireValidCachedToken is chi middleware guaranteeing that, by the
// time the wrapped handler runs, a currently-valid GitHub access token
// for GITHUB_CLIENT_ID is available via cachedTokenFromContext:
//
//   - If the cached access token is still valid, it's used as-is.
//   - If it's expired but the cached refresh token isn't, this
//     transparently refreshes it (updating the cache) before continuing.
//   - If there's no cached token yet, the refresh token itself has
//     expired, or the refresh attempt fails, the wrapped handler is
//     never invoked - this writes the failure response itself and tells
//     the caller to rerun the device flow where that's the actual fix.
//   - A request with a bare ?force_refresh query parameter forces the
//     refresh branch even when the cached access token is still valid -
//     useful for testing the refresh path on demand. It still can't
//     override a genuinely expired refresh token: that's still
//     reauth-required regardless.
//
// This is the same expiry-check-then-refresh state machine
// /auth/github/token has always run, pulled out so any future route
// that needs a valid cached token (not just that one) can require one
// with r.With(RequireValidCachedToken) instead of re-implementing this
// check.
func RequireValidCachedToken(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

		// GITHUB_CLIENT_SECRET is optional: refresh tokens issued via
		// the device flow belong to a public client and refresh
		// without one.
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

		forceRefresh := r.URL.Query().Has("force_refresh")

		switch decideTokenAction(cached, time.Now(), forceRefresh) {
		case actionServeCached:
			next.ServeHTTP(w, withCachedToken(r, cached))

		case actionReauthRequired:
			http.Error(
				w,
				"cached GitHub refresh token is expired; run the device flow again",
				http.StatusUnauthorized,
			)

		case actionRefresh:
			refreshed, ok := ensureFreshToken(ctx, w, clientID, clientSecret, bucket, key, cached)
			if !ok {
				// Failure response already written by ensureFreshToken.
				return
			}
			next.ServeHTTP(w, withCachedToken(r, refreshed))
		}
	})
}
