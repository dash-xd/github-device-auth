// Package deviceauth exposes GitHub OAuth device-flow operations without any
// HTTP router dependency.
package deviceauth

import (
	"context"
	"time"

	"github.com/dash-xd/github-device-auth/internal/ghdeviceflow"
)

// DeviceCodeResponse is GitHub's response to a device-code request.
type DeviceCodeResponse = ghdeviceflow.DeviceCodeResponse

// TokenResponse is GitHub's device-flow/refresh token response.
type TokenResponse = ghdeviceflow.TokenResponse

var (
	ErrAuthorizationPending       = ghdeviceflow.ErrAuthorizationPending
	ErrSlowDown                   = ghdeviceflow.ErrSlowDown
	ErrExpiredToken               = ghdeviceflow.ErrExpiredToken
	ErrAccessDenied               = ghdeviceflow.ErrAccessDenied
	ErrInvalidRefreshToken        = ghdeviceflow.ErrInvalidRefreshToken
	ErrIncorrectClientCredentials = ghdeviceflow.ErrIncorrectClientCredentials
)

// RequestDeviceCode starts GitHub's OAuth device authorization flow.
func RequestDeviceCode(ctx context.Context, clientID string) (*DeviceCodeResponse, error) {
	return ghdeviceflow.RequestDeviceCode(ctx, clientID)
}

// PollForToken polls GitHub until device authorization resolves or ctx is
// canceled. A non-positive interval uses the implementation default.
func PollForToken(ctx context.Context, clientID, deviceCode string, interval time.Duration) (*TokenResponse, error) {
	return ghdeviceflow.PollForToken(ctx, clientID, deviceCode, interval)
}

// RefreshAccessToken exchanges a refresh token for a new access token.
// clientSecret may be empty for public-client device-flow refresh tokens.
func RefreshAccessToken(ctx context.Context, clientID, clientSecret, refreshToken string) (*TokenResponse, error) {
	return ghdeviceflow.RefreshAccessToken(ctx, clientID, clientSecret, refreshToken)
}
