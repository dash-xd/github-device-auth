package router

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestDecideTokenAction(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	buffer := accessTokenRefreshBuffer

	tests := []struct {
		name   string
		cached cachedToken
		want   tokenAction
	}{
		{
			name: "access token comfortably valid",
			cached: cachedToken{
				AccessTokenExpiresAt:  now.Add(1 * time.Hour),
				RefreshTokenExpiresAt: now.Add(30 * 24 * time.Hour),
			},
			want: actionServeCached,
		},
		{
			name: "access token expired, refresh token valid",
			cached: cachedToken{
				AccessTokenExpiresAt:  now.Add(-1 * time.Minute),
				RefreshTokenExpiresAt: now.Add(30 * 24 * time.Hour),
			},
			want: actionRefresh,
		},
		{
			name: "access token inside the refresh buffer counts as expired",
			cached: cachedToken{
				AccessTokenExpiresAt:  now.Add(buffer / 2),
				RefreshTokenExpiresAt: now.Add(30 * 24 * time.Hour),
			},
			want: actionRefresh,
		},
		{
			name: "both access and refresh tokens expired",
			cached: cachedToken{
				AccessTokenExpiresAt:  now.Add(-1 * time.Hour),
				RefreshTokenExpiresAt: now.Add(-1 * time.Minute),
			},
			want: actionReauthRequired,
		},
		{
			name: "access token expiry unknown (zero value) forces a refresh check",
			cached: cachedToken{
				RefreshTokenExpiresAt: now.Add(30 * 24 * time.Hour),
			},
			want: actionRefresh,
		},
		{
			name: "refresh token expiry unknown (zero value) treated as still valid",
			cached: cachedToken{
				AccessTokenExpiresAt: now.Add(-1 * time.Hour),
			},
			want: actionRefresh,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := decideTokenAction(tt.cached, now)
			if got != tt.want {
				t.Errorf("decideTokenAction() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHandleToken_ClientIDNotConfigured(t *testing.T) {
	srv := httptest.NewServer(NewRouter())
	t.Cleanup(srv.Close)

	resp, err := http.Post(srv.URL+"/auth/github/token", "", nil)
	if err != nil {
		t.Fatalf("POST /auth/github/token: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusInternalServerError)
	}
}

func TestHandleToken_BucketNotConfigured(t *testing.T) {
	t.Setenv("GITHUB_CLIENT_ID", "test-client-id")

	srv := httptest.NewServer(NewRouter())
	t.Cleanup(srv.Close)

	resp, err := http.Post(srv.URL+"/auth/github/token", "", nil)
	if err != nil {
		t.Fatalf("POST /auth/github/token: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusInternalServerError)
	}
}

// TestHandleToken_CacheReadFailureSurfacesAsBadGateway verifies that a
// GCS read failure degrades to a clean 502 rather than hanging or
// crashing. It does not verify reading an actual cached token - that
// needs a real bucket/credentials, unavailable in this test
// environment (same caveat as the write-side cache tests).
func TestHandleToken_CacheReadFailureSurfacesAsBadGateway(t *testing.T) {
	t.Setenv("GITHUB_CLIENT_ID", "test-client-id")
	t.Setenv("TENANT_ID", "does-not-exist-test-tenant")
	t.Setenv("REGION", "us-central1")

	srv := httptest.NewServer(NewRouter())
	t.Cleanup(srv.Close)

	resp, err := http.Post(srv.URL+"/auth/github/token", "", nil)
	if err != nil {
		t.Fatalf("POST /auth/github/token: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusBadGateway)
	}
}
