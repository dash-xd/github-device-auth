package router

import "net/http"

// corsMiddleware allows this API to be called from a browser page served
// from a different origin than the deployed function. The API has no
// cookie/session state. X-GitHub-App-Client-ID is allowed so a static client
// can supply a public GitHub App client ID when the deployment intentionally
// leaves GITHUB_CLIENT_ID unset.
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-GitHub-App-Client-ID")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}
