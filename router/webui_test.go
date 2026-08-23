package router

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandleDeviceFlowTestPage(t *testing.T) {
	srv := httptest.NewServer(NewRouter())
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/device-flow-test")
	if err != nil {
		t.Fatalf("GET /device-flow-test: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type = %q, want text/html prefix", ct)
	}
}
