// Package public preserves the historical github-device-auth facade.
//
// New non-HTTP callers should import github.com/dash-xd/github-device-auth/deviceauth.
// NewRouter remains here for compatibility with callers that want the HTTP
// adapter.
package public

import (
	"context"
	"time"

	"github.com/dash-xd/github-device-auth/deviceauth"
	"github.com/dash-xd/github-device-auth/router"
	"github.com/go-chi/chi/v5"
)

type DeviceCodeResponse = deviceauth.DeviceCodeResponse
type TokenResponse = deviceauth.TokenResponse

var (
	ErrAuthorizationPending       = deviceauth.ErrAuthorizationPending
	ErrSlowDown                   = deviceauth.ErrSlowDown
	ErrExpiredToken               = deviceauth.ErrExpiredToken
	ErrAccessDenied               = deviceauth.ErrAccessDenied
	ErrInvalidRefreshToken        = deviceauth.ErrInvalidRefreshToken
	ErrIncorrectClientCredentials = deviceauth.ErrIncorrectClientCredentials
)

func RequestDeviceCode(ctx context.Context, clientID string) (*DeviceCodeResponse, error) {
	return deviceauth.RequestDeviceCode(ctx, clientID)
}

func PollForToken(ctx context.Context, clientID, deviceCode string, interval time.Duration) (*TokenResponse, error) {
	return deviceauth.PollForToken(ctx, clientID, deviceCode, interval)
}

func RefreshAccessToken(ctx context.Context, clientID, clientSecret, refreshToken string) (*TokenResponse, error) {
	return deviceauth.RefreshAccessToken(ctx, clientID, clientSecret, refreshToken)
}

func NewRouter() *chi.Mux {
	return router.NewRouter()
}
