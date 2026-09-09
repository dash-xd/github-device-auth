package public

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCredentialBundleRoundTripAndFreshness(t *testing.T) {
	now := time.Date(2026, 9, 9, 8, 0, 0, 0, time.UTC)
	bundle, err := NewCredentialBundle("client", &TokenResponse{
		AccessToken:           "access",
		RefreshToken:          "refresh",
		ExpiresIn:             8 * 60 * 60,
		RefreshTokenExpiresIn: 180 * 24 * 60 * 60,
		TokenType:             "bearer",
		Scope:                 "repo",
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if !bundle.AccessFresh(now.Add(7*time.Hour), 5*time.Minute) {
		t.Fatal("expected access token to remain fresh")
	}
	if bundle.AccessFresh(now.Add(7*time.Hour+56*time.Minute), 5*time.Minute) {
		t.Fatal("expected token inside safety margin to be stale")
	}

	path := filepath.Join(t.TempDir(), "credentials", "github-device.json")
	if err := SaveCredentialBundle(path, bundle); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("mode = %o, want 600", got)
	}
	loaded, err := LoadCredentialBundle(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ClientID != bundle.ClientID || loaded.AccessToken != bundle.AccessToken || loaded.RefreshToken != bundle.RefreshToken {
		t.Fatalf("loaded bundle = %#v, want %#v", loaded, bundle)
	}
}

func TestParseCredentialBundleRejectsUnknownAndIncompleteState(t *testing.T) {
	valid := `{"version":1,"client_id":"client","access_token":"access","refresh_token":"refresh","access_token_expires_at":"2026-09-09T16:00:00Z","refresh_token_expires_at":"2027-03-08T08:00:00Z"}`
	if _, err := ParseCredentialBundle([]byte(valid)); err != nil {
		t.Fatalf("valid bundle rejected: %v", err)
	}
	if _, err := ParseCredentialBundle([]byte(valid[:len(valid)-1] + `,"zone_id":"provider-specific"}`)); err == nil {
		t.Fatal("expected unknown field to be rejected")
	}
	if _, err := ParseCredentialBundle([]byte(`{"version":1,"client_id":"client"}`)); err == nil {
		t.Fatal("expected incomplete pair to be rejected")
	}
	if _, err := ParseCredentialBundle([]byte(valid + ` {}`)); err == nil {
		t.Fatal("expected trailing JSON to be rejected")
	}
	if _, err := ParseCredentialBundle([]byte(valid + ` nope`)); err == nil {
		t.Fatal("expected malformed trailing data to be rejected")
	}
}
