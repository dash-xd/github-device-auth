package router

import (
	"net/http/httptest"
	"testing"
)

func TestGitHubClientIDResolution(t *testing.T) {
	t.Run("environment wins over header", func(t *testing.T) {
		t.Setenv("GITHUB_CLIENT_ID", "env-client")
		r := httptest.NewRequest("POST", "/auth/github/device", nil)
		r.Header.Set(githubAppClientIDHeader, "header-client")
		if got := githubClientID(r); got != "env-client" {
			t.Fatalf("githubClientID() = %q, want env-client", got)
		}
	})

	t.Run("header is fallback when environment is absent", func(t *testing.T) {
		t.Setenv("GITHUB_CLIENT_ID", "")
		r := httptest.NewRequest("POST", "/auth/github/device", nil)
		r.Header.Set(githubAppClientIDHeader, "header-client")
		if got := githubClientID(r); got != "header-client" {
			t.Fatalf("githubClientID() = %q, want header-client", got)
		}
	})

	t.Run("surrounding whitespace is ignored", func(t *testing.T) {
		t.Setenv("GITHUB_CLIENT_ID", "   ")
		r := httptest.NewRequest("POST", "/auth/github/device", nil)
		r.Header.Set(githubAppClientIDHeader, "  header-client  ")
		if got := githubClientID(r); got != "header-client" {
			t.Fatalf("githubClientID() = %q, want header-client", got)
		}
	})

	t.Run("missing both remains unconfigured", func(t *testing.T) {
		t.Setenv("GITHUB_CLIENT_ID", "")
		r := httptest.NewRequest("POST", "/auth/github/device", nil)
		if got := githubClientID(r); got != "" {
			t.Fatalf("githubClientID() = %q, want empty", got)
		}
	})
}

func TestCORSAllowsGitHubAppClientIDHeader(t *testing.T) {
	r := httptest.NewRequest("OPTIONS", "/auth/github/device", nil)
	w := httptest.NewRecorder()
	NewRouter().ServeHTTP(w, r)

	if w.Code != 204 {
		t.Fatalf("OPTIONS status = %d, want 204", w.Code)
	}
	if got := w.Header().Get("Access-Control-Allow-Headers"); got != "Content-Type, X-GitHub-App-Client-ID" {
		t.Fatalf("Access-Control-Allow-Headers = %q", got)
	}
}
