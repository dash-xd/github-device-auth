package router

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/dash-xd/github-device-auth/internal/ghdeviceflow"
)

// mockGitHubConfig controls how a mock GitHub server (started by
// startMockGitHub) responds to the token endpoint.
type mockGitHubConfig struct {
	// pollErrorUntil is how many times the token endpoint returns
	// authorization_pending before succeeding. 0 succeeds on the first
	// call - PollForToken sleeps a full interval between pending
	// responses, so tests should avoid needing more than one.
	pollErrorUntil int

	// finalError, if set, makes the token endpoint always return this
	// error code (e.g. "expired_token"), regardless of pollErrorUntil.
	finalError string
}

// startMockGitHub serves fake device-code and token endpoints for the
// duration of the test.
func startMockGitHub(t *testing.T, cfg mockGitHubConfig) *httptest.Server {
	t.Helper()

	var pollCount int32

	mux := http.NewServeMux()

	mux.HandleFunc("/device/code", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"device_code":      "mock-device-code",
			"user_code":        "ABCD-1234",
			"verification_uri": "https://github.com/login/device",
			"expires_in":       900,
			"interval":         1,
		})
	})

	mux.HandleFunc("/oauth/access_token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if cfg.finalError != "" {
			_ = json.NewEncoder(w).Encode(map[string]any{"error": cfg.finalError})
			return
		}

		if n := atomic.AddInt32(&pollCount, 1); int(n) <= cfg.pollErrorUntil {
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "authorization_pending"})
			return
		}

		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "mock-access-token",
			"refresh_token": "mock-refresh-token",
			"token_type":    "bearer",
		})
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return srv
}

// useMockGitHub points ghdeviceflow at srv for the duration of the
// test, restoring the real GitHub URLs afterward. Not safe under
// t.Parallel(): DeviceCodeURL/TokenURL are shared package-level vars.
func useMockGitHub(t *testing.T, srv *httptest.Server) {
	t.Helper()

	originalDeviceCodeURL := ghdeviceflow.DeviceCodeURL
	originalTokenURL := ghdeviceflow.TokenURL

	ghdeviceflow.DeviceCodeURL = srv.URL + "/device/code"
	ghdeviceflow.TokenURL = srv.URL + "/oauth/access_token"

	t.Cleanup(func() {
		ghdeviceflow.DeviceCodeURL = originalDeviceCodeURL
		ghdeviceflow.TokenURL = originalTokenURL
	})
}

type sseEvent struct {
	Event string
	Data  string
}

// parseSSEEvents splits a full SSE response body into its ordered
// event/data frames, skipping keepalive comment lines.
func parseSSEEvents(body string) []sseEvent {
	var events []sseEvent
	var current sseEvent

	for _, line := range strings.Split(body, "\n") {
		switch {
		case strings.HasPrefix(line, "event: "):
			current.Event = strings.TrimPrefix(line, "event: ")

		case strings.HasPrefix(line, "data: "):
			current.Data = strings.TrimPrefix(line, "data: ")

		case line == "":
			if current.Event != "" {
				events = append(events, current)
				current = sseEvent{}
			}
		}
	}

	return events
}
