package router

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func postRefresh(t *testing.T, srv *httptest.Server, path, refreshToken string) *http.Response {
	t.Helper()

	body, err := json.Marshal(map[string]string{"refresh_token": refreshToken})
	if err != nil {
		t.Fatalf("marshaling request body: %v", err)
	}

	resp, err := http.Post(srv.URL+path, "application/json", strings.NewReader(string(body)))
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}

	return resp
}

func TestHandleRefresh_PlainRequestReturnsToken(t *testing.T) {
	t.Setenv("GITHUB_CLIENT_ID", "test-client-id")

	mockGitHub := startMockGitHub(t, mockGitHubConfig{})
	useMockGitHub(t, mockGitHub)

	srv := httptest.NewServer(NewRouter())
	t.Cleanup(srv.Close)

	resp := postRefresh(t, srv, "/auth/github/refresh", "mock-refresh-token-in")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var got struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decoding response: %v", err)
	}

	if got.AccessToken != "mock-access-token" {
		t.Errorf("AccessToken = %q, want %q", got.AccessToken, "mock-access-token")
	}
}

func TestHandleRefresh_CacheBucketNotConfigured(t *testing.T) {
	t.Setenv("GITHUB_CLIENT_ID", "test-client-id")

	srv := httptest.NewServer(NewRouter())
	t.Cleanup(srv.Close)

	resp := postRefresh(t, srv, "/auth/github/refresh?cache", "some-token")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusInternalServerError)
	}
}

// TestHandleRefresh_CacheWriteFailureSurfacesAsError verifies that a
// GCS write failure degrades to a clean 502 rather than hanging or
// crashing. See the equivalent device_full_test.go comment: this does
// not verify a real GCS write, since no GCP credentials are available
// in this test environment.
func TestHandleRefresh_CacheWriteFailureSurfacesAsError(t *testing.T) {
	t.Setenv("GITHUB_CLIENT_ID", "test-client-id")
	t.Setenv("GITHUB_TOKEN_CACHE_BUCKET", "does-not-exist-test-bucket")

	mockGitHub := startMockGitHub(t, mockGitHubConfig{})
	useMockGitHub(t, mockGitHub)

	srv := httptest.NewServer(NewRouter())
	t.Cleanup(srv.Close)

	resp := postRefresh(t, srv, "/auth/github/refresh?cache", "some-token")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusBadGateway)
	}
}
