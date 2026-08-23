package router

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleDeviceFull_Success(t *testing.T) {
	t.Setenv("GITHUB_CLIENT_ID", "test-client-id")

	mockGitHub := startMockGitHub(t, mockGitHubConfig{})
	useMockGitHub(t, mockGitHub)

	srv := httptest.NewServer(NewRouter())
	t.Cleanup(srv.Close)

	resp, err := http.Post(srv.URL+"/auth/github/device?full", "", nil)
	if err != nil {
		t.Fatalf("POST /auth/github/device?full: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading response body: %v", err)
	}

	events := parseSSEEvents(string(body))
	if len(events) != 2 {
		t.Fatalf("got %d SSE events, want 2: %+v", len(events), events)
	}

	if events[0].Event != "device_code" {
		t.Errorf("events[0].Event = %q, want %q", events[0].Event, "device_code")
	}

	var deviceCode deviceResponse
	if err := json.Unmarshal([]byte(events[0].Data), &deviceCode); err != nil {
		t.Fatalf("unmarshaling device_code event: %v", err)
	}

	if deviceCode.DeviceCode != "mock-device-code" {
		t.Errorf("DeviceCode = %q, want %q", deviceCode.DeviceCode, "mock-device-code")
	}

	if events[1].Event != "token" {
		t.Errorf("events[1].Event = %q, want %q", events[1].Event, "token")
	}

	var token struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.Unmarshal([]byte(events[1].Data), &token); err != nil {
		t.Fatalf("unmarshaling token event: %v", err)
	}

	if token.AccessToken != "mock-access-token" {
		t.Errorf("AccessToken = %q, want %q", token.AccessToken, "mock-access-token")
	}

	if token.RefreshToken != "mock-refresh-token" {
		t.Errorf("RefreshToken = %q, want %q", token.RefreshToken, "mock-refresh-token")
	}
}

func TestHandleDeviceFull_ExpiredToken(t *testing.T) {
	t.Setenv("GITHUB_CLIENT_ID", "test-client-id")

	mockGitHub := startMockGitHub(t, mockGitHubConfig{finalError: "expired_token"})
	useMockGitHub(t, mockGitHub)

	srv := httptest.NewServer(NewRouter())
	t.Cleanup(srv.Close)

	resp, err := http.Post(srv.URL+"/auth/github/device?full", "", nil)
	if err != nil {
		t.Fatalf("POST /auth/github/device?full: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading response body: %v", err)
	}

	events := parseSSEEvents(string(body))
	if len(events) != 2 {
		t.Fatalf("got %d SSE events, want 2: %+v", len(events), events)
	}

	if events[0].Event != "device_code" {
		t.Errorf("events[0].Event = %q, want %q", events[0].Event, "device_code")
	}

	if events[1].Event != "error" {
		t.Errorf("events[1].Event = %q, want %q", events[1].Event, "error")
	}

	var errEvent sseError
	if err := json.Unmarshal([]byte(events[1].Data), &errEvent); err != nil {
		t.Fatalf("unmarshaling error event: %v", err)
	}

	if errEvent.Message != "GitHub device code expired" {
		t.Errorf("Message = %q, want %q", errEvent.Message, "GitHub device code expired")
	}
}

func TestHandleDeviceFull_CacheBucketNotConfigured(t *testing.T) {
	t.Setenv("GITHUB_CLIENT_ID", "test-client-id")

	srv := httptest.NewServer(NewRouter())
	t.Cleanup(srv.Close)

	resp, err := http.Post(srv.URL+"/auth/github/device?full&cache", "", nil)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusInternalServerError)
	}
}

// TestHandleDeviceFull_CacheWriteFailureSurfacesAsError verifies that a
// GCS write failure degrades cleanly to an SSE error event rather than
// hanging or crashing. It does NOT verify that a real GCS write ever
// succeeds - this test runs with no GCP credentials available (true on
// any plain CI runner), so tokencache.Store is expected to fail every
// time. An actual successful write can only be verified against a real
// bucket at deployment time.
func TestHandleDeviceFull_CacheWriteFailureSurfacesAsError(t *testing.T) {
	t.Setenv("GITHUB_CLIENT_ID", "test-client-id")
	t.Setenv("GITHUB_TOKEN_CACHE_BUCKET", "does-not-exist-test-bucket")

	mockGitHub := startMockGitHub(t, mockGitHubConfig{})
	useMockGitHub(t, mockGitHub)

	srv := httptest.NewServer(NewRouter())
	t.Cleanup(srv.Close)

	resp, err := http.Post(srv.URL+"/auth/github/device?full&cache", "", nil)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading response body: %v", err)
	}

	events := parseSSEEvents(string(body))
	if len(events) != 2 {
		t.Fatalf("got %d SSE events, want 2: %+v", len(events), events)
	}

	if events[0].Event != "device_code" {
		t.Errorf("events[0].Event = %q, want %q", events[0].Event, "device_code")
	}

	if events[1].Event != "error" {
		t.Errorf("events[1].Event = %q, want %q", events[1].Event, "error")
	}

	var errEvent sseError
	if err := json.Unmarshal([]byte(events[1].Data), &errEvent); err != nil {
		t.Fatalf("unmarshaling error event: %v", err)
	}

	if errEvent.Message != "failed to cache GitHub token" {
		t.Errorf("Message = %q, want %q", errEvent.Message, "failed to cache GitHub token")
	}
}
