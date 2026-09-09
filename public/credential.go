package public

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const CredentialBundleVersion = 1

// CredentialBundle is one atomic device-flow credential checkpoint. Access and
// refresh tokens intentionally live in one document because GitHub rotates the
// pair together during refresh.
type CredentialBundle struct {
	Version               int       `json:"version"`
	ClientID              string    `json:"client_id"`
	AccessToken           string    `json:"access_token"`
	RefreshToken          string    `json:"refresh_token"`
	AccessTokenExpiresAt  time.Time `json:"access_token_expires_at"`
	RefreshTokenExpiresAt time.Time `json:"refresh_token_expires_at"`
	TokenType             string    `json:"token_type,omitempty"`
	Scope                 string    `json:"scope,omitempty"`
}

// NewCredentialBundle converts a successful GitHub token response into the
// durable bundle format using now as the expiry reference point.
func NewCredentialBundle(clientID string, token *TokenResponse, now time.Time) (CredentialBundle, error) {
	clientID = strings.TrimSpace(clientID)
	if clientID == "" {
		return CredentialBundle{}, errors.New("client ID is required")
	}
	if token == nil {
		return CredentialBundle{}, errors.New("token response is required")
	}
	if token.AccessToken == "" || token.RefreshToken == "" {
		return CredentialBundle{}, errors.New("GitHub token response must contain both access and refresh tokens")
	}
	if token.ExpiresIn <= 0 || token.RefreshTokenExpiresIn <= 0 {
		return CredentialBundle{}, errors.New("GitHub token response must contain positive access and refresh expiries")
	}
	now = now.UTC()
	return CredentialBundle{
		Version:               CredentialBundleVersion,
		ClientID:              clientID,
		AccessToken:           token.AccessToken,
		RefreshToken:          token.RefreshToken,
		AccessTokenExpiresAt:  now.Add(time.Duration(token.ExpiresIn) * time.Second),
		RefreshTokenExpiresAt: now.Add(time.Duration(token.RefreshTokenExpiresIn) * time.Second),
		TokenType:             token.TokenType,
		Scope:                 token.Scope,
	}, nil
}

// Validate rejects incomplete or unsupported checkpoints before they become
// authentication authority.
func (b CredentialBundle) Validate() error {
	if b.Version != CredentialBundleVersion {
		return fmt.Errorf("unsupported credential bundle version %d", b.Version)
	}
	if strings.TrimSpace(b.ClientID) == "" {
		return errors.New("credential bundle client_id is required")
	}
	if b.AccessToken == "" || b.RefreshToken == "" {
		return errors.New("credential bundle requires both access_token and refresh_token")
	}
	if b.AccessTokenExpiresAt.IsZero() || b.RefreshTokenExpiresAt.IsZero() {
		return errors.New("credential bundle requires both expiry boundaries")
	}
	return nil
}

// AccessFresh reports whether the current access token remains usable outside
// the requested safety margin.
func (b CredentialBundle) AccessFresh(now time.Time, safetyMargin time.Duration) bool {
	if b.Validate() != nil {
		return false
	}
	if safetyMargin < 0 {
		safetyMargin = 0
	}
	return now.UTC().Add(safetyMargin).Before(b.AccessTokenExpiresAt.UTC())
}

// Refresh exchanges the current refresh token using the public-client device
// flow and returns a complete replacement bundle. The old bundle is left
// untouched if GitHub does not return a complete new token pair.
func (b CredentialBundle) Refresh(ctx context.Context, now time.Time) (CredentialBundle, error) {
	if err := b.Validate(); err != nil {
		return CredentialBundle{}, err
	}
	if !now.UTC().Before(b.RefreshTokenExpiresAt.UTC()) {
		return CredentialBundle{}, ErrInvalidRefreshToken
	}
	token, err := RefreshAccessToken(ctx, b.ClientID, "", b.RefreshToken)
	if err != nil {
		return CredentialBundle{}, err
	}
	return NewCredentialBundle(b.ClientID, token, now)
}

// Marshal returns the exact repository-secret/checkpoint representation.
func (b CredentialBundle) Marshal() ([]byte, error) {
	if err := b.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(b)
}

// ParseCredentialBundle parses one strict JSON checkpoint. Unknown fields and
// trailing JSON are rejected so provider-specific or ambiguous state cannot be
// silently treated as authentication authority.
func ParseCredentialBundle(data []byte) (CredentialBundle, error) {
	var b CredentialBundle
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&b); err != nil {
		return CredentialBundle{}, err
	}
	if dec.More() {
		return CredentialBundle{}, errors.New("credential bundle contains trailing JSON")
	}
	var extra any
	if err := dec.Decode(&extra); err == nil {
		return CredentialBundle{}, errors.New("credential bundle contains trailing JSON")
	}
	if err := b.Validate(); err != nil {
		return CredentialBundle{}, err
	}
	return b, nil
}

// LoadCredentialBundle reads a local checkpoint.
func LoadCredentialBundle(path string) (CredentialBundle, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return CredentialBundle{}, err
	}
	return ParseCredentialBundle(data)
}

// SaveCredentialBundle atomically replaces a local checkpoint with mode 0600.
func SaveCredentialBundle(path string, b CredentialBundle) error {
	data, err := b.Marshal()
	if err != nil {
		return err
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return errors.New("credential bundle path is required")
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".github-device-token-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}
