package router

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleDevice_PlainRequestReturnsDeviceCode(t *testing.T) {
	t.Setenv("GITHUB_CLIENT_ID", "test-client-id")

	mockGitHub := startMockGitHub(t, mockGitHubConfig{})
	useMockGitHub(t, mockGitHub)

	srv := httptest.NewServer(NewRouter())
	t.Cleanup(srv.Close)

	resp, err := http.Post(srv.URL+"/auth/github/device", "", nil)
	if err != nil {
		t.Fatalf("POST /auth/github/device: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var got deviceResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decoding response: %v", err)
	}

	if got.DeviceCode != "mock-device-code" {
		t.Errorf("DeviceCode = %q, want %q", got.DeviceCode, "mock-device-code")
	}

	if got.UserCode != "ABCD-1234" {
		t.Errorf("UserCode = %q, want %q", got.UserCode, "ABCD-1234")
	}

	if got.Instruction != "Open https://github.com/login/device and enter the code ABCD-1234" {
		t.Errorf("unexpected Instruction: %q", got.Instruction)
	}
}

func TestHandleDevice_CacheWithoutFullIsRejected(t *testing.T) {
	t.Setenv("GITHUB_CLIENT_ID", "test-client-id")

	srv := httptest.NewServer(NewRouter())
	t.Cleanup(srv.Close)

	resp, err := http.Post(srv.URL+"/auth/github/device?cache", "", nil)
	if err != nil {
		t.Fatalf("POST /auth/github/device?cache: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}
