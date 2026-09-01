package router

import (
	"net/http"
	"os"
	"strings"
)

const githubAppClientIDHeader = "X-GitHub-App-Client-ID"

// githubClientID resolves the GitHub App/OAuth client ID for a request.
// Deployment configuration remains authoritative: GITHUB_CLIENT_ID wins when
// present. Browser/static clients may supply X-GitHub-App-Client-ID only when
// the deployment has no configured client ID.
func githubClientID(r *http.Request) string {
	if clientID := strings.TrimSpace(os.Getenv("GITHUB_CLIENT_ID")); clientID != "" {
		return clientID
	}
	return strings.TrimSpace(r.Header.Get(githubAppClientIDHeader))
}
