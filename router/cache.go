package router

import (
	"net/http"
	"os"
)

const cacheBucketEnvVar = "GITHUB_TOKEN_CACHE_BUCKET"

type cacheConfirmation struct {
	Cached bool   `json:"cached"`
	Bucket string `json:"bucket"`
	Object string `json:"object"`
}

// parseCacheRequest inspects the cache/cache_key query params and the
// configured cache bucket.
//
// If cache wasn't requested, requested is false and ok is true - there's
// nothing more to do. If cache was requested but is missing cache_key or
// the bucket isn't configured, ok is false and an error response has
// already been written to w; the caller must return immediately without
// writing anything else. Otherwise requested and ok are both true, and
// bucket/key are ready to pass to tokencache.Store.
func parseCacheRequest(w http.ResponseWriter, r *http.Request) (bucket, key string, requested, ok bool) {
	if !r.URL.Query().Has("cache") {
		return "", "", false, true
	}

	key = r.URL.Query().Get("cache_key")

	if key == "" {
		http.Error(
			w,
			"cache_key is required when cache is set",
			http.StatusBadRequest,
		)
		return "", "", true, false
	}

	bucket = os.Getenv(cacheBucketEnvVar)

	if bucket == "" {
		http.Error(
			w,
			"token cache bucket is not configured",
			http.StatusInternalServerError,
		)
		return "", "", true, false
	}

	return bucket, key, true, true
}
