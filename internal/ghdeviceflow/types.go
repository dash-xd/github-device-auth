// Package ghdeviceflow implements GitHub's OAuth device authorization
// flow: requesting a device code, polling for the resulting token, and
// refreshing an access token with a refresh token.
package ghdeviceflow

import "errors"

const tokenURL = "https://github.com/login/oauth/access_token"

// TokenResponse is GitHub's response shape for both the device-flow poll
// and the refresh-token exchange.
type TokenResponse struct {
	AccessToken  string `json:"access_token,omitempty"`
	RefreshToken string `json:"refresh_token,omitempty"`

	TokenType string `json:"token_type,omitempty"`
	Scope     string `json:"scope,omitempty"`

	ExpiresIn             int `json:"expires_in,omitempty"`
	RefreshTokenExpiresIn int `json:"refresh_token_expires_in,omitempty"`

	Error       string `json:"error,omitempty"`
	Description string `json:"error_description,omitempty"`
	Interval    int    `json:"interval,omitempty"`
}

var (
	ErrAuthorizationPending       = errors.New("authorization pending")
	ErrSlowDown                   = errors.New("github requested slower polling")
	ErrExpiredToken               = errors.New("device code expired")
	ErrAccessDenied               = errors.New("user denied authorization")
	ErrInvalidRefreshToken        = errors.New("refresh token is invalid or expired")
	ErrIncorrectClientCredentials = errors.New("github rejected the client credentials for this refresh token")
)
