package router

import (
	"context"
	"errors"
	"net/http"
	"os"
	"time"

	"cloud.google.com/go/storage"
)

type cachedTokenContextKey struct{}

func cachedTokenFromContext(ctx context.Context) (cachedToken, bool) {
	token, ok := ctx.Value(cachedTokenContextKey{}).(cachedToken)
	return token, ok
}

func withCachedToken(r *http.Request, token cachedToken) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), cachedTokenContextKey{}, token))
}

// RequireValidCachedToken guarantees a valid cached GitHub access token for
// the request's resolved client ID. GITHUB_CLIENT_ID remains authoritative;
// X-GitHub-App-Client-ID is accepted only when that environment variable is
// absent.
func RequireValidCachedToken(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clientID := githubClientID(r)
		if clientID == "" {
			http.Error(w, "GitHub client ID is not configured", http.StatusInternalServerError)
			return
		}

		bucket, err := resolveCacheBucket(r.Context())
		if err != nil {
			http.Error(w, "token cache bucket is not configured: "+err.Error(), http.StatusInternalServerError)
			return
		}

		clientSecret := os.Getenv("GITHUB_CLIENT_SECRET")
		key := cacheObjectKey(clientID)

		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()

		cached, err := loadCachedToken(ctx, bucket, key)
		if err != nil {
			if errors.Is(err, storage.ErrObjectNotExist) {
				http.Error(w, "no cached GitHub token found; run the device flow first", http.StatusNotFound)
				return
			}
			http.Error(w, "failed to read cached GitHub token", http.StatusBadGateway)
			return
		}

		forceRefresh := r.URL.Query().Has("force_refresh")
		switch decideTokenAction(cached, time.Now(), forceRefresh) {
		case actionServeCached:
			next.ServeHTTP(w, withCachedToken(r, cached))
		case actionReauthRequired:
			http.Error(w, "cached GitHub refresh token is expired; run the device flow again", http.StatusUnauthorized)
		case actionRefresh:
			refreshed, ok := ensureFreshToken(ctx, w, clientID, clientSecret, bucket, key, cached)
			if !ok {
				return
			}
			next.ServeHTTP(w, withCachedToken(r, refreshed))
		}
	})
}
