package router

import "net/http"

// corsMiddleware allows this API to be called from a browser page served
// from a different origin than the deployed function - e.g. the
// standalone web/device-flow-test.html page in this repo, opened
// straight off disk or served from any static host. This API has no
// cookies/session state (every credential is either in the request body
// or resolved server-side from the cache bucket), so a permissive
// Access-Control-Allow-Origin carries none of the cross-site-credential
// risk it would for a cookie-authenticated API; it only widens who can
// reach these endpoints at all, which is already gated separately by
// whatever IAM/ingress settings the deployment applies (see
// terraform/device-auth-router).
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}
