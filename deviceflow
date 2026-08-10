package deviceflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	deviceCodeURL = "https://github.com/login/device/code"
	tokenURL      = "https://github.com/login/oauth/access_token"

	deviceGrantType = "urn:ietf:params:oauth:grant-type:device_code"
)

type DeviceCodeResponse struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete,omitempty"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
}

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

var ErrAuthorizationPending = errors.New("authorization pending")
var ErrSlowDown = errors.New("github requested slower polling")
var ErrExpiredToken = errors.New("device code expired")
var ErrAccessDenied = errors.New("user denied authorization")

func RequestDeviceCode(
	ctx context.Context,
	clientID string,
) (*DeviceCodeResponse, error) {
	form := url.Values{}
	form.Set("client_id", clientID)

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		deviceCodeURL,
		strings.NewReader(form.Encode()),
	)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf(
			"github device code request returned HTTP %d",
			resp.StatusCode,
		)
	}

	var result DeviceCodeResponse

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	if result.DeviceCode == "" {
		return nil, errors.New("github returned an empty device_code")
	}

	if result.UserCode == "" {
		return nil, errors.New("github returned an empty user_code")
	}

	if result.VerificationURI == "" {
		return nil, errors.New("github returned an empty verification_uri")
	}

	return &result, nil
}

func PollForToken(
	ctx context.Context,
	clientID string,
	deviceCode string,
	interval time.Duration,
) (*TokenResponse, error) {
	if interval <= 0 {
		interval = 5 * time.Second
	}

	for {
		result, err := requestToken(
			ctx,
			clientID,
			deviceCode,
		)
		if err != nil {
			return nil, err
		}

		switch result.Error {
		case "":
			if result.AccessToken == "" {
				return nil, errors.New(
					"github returned success without an access token",
				)
			}

			return result, nil

		case "authorization_pending":
			// Continue polling using the current interval.

		case "slow_down":
			// GitHub requires five additional seconds.
			interval += 5 * time.Second

			// Some GitHub responses may also provide an interval.
			if result.Interval > 0 {
				interval = time.Duration(result.Interval) * time.Second
			}

		case "expired_token":
			return nil, ErrExpiredToken

		case "access_denied":
			return nil, ErrAccessDenied

		default:
			return nil, fmt.Errorf(
				"github authentication failed: %s: %s",
				result.Error,
				result.Description,
			)
		}

		timer := time.NewTimer(interval)

		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}

			return nil, ctx.Err()

		case <-timer.C:
		}
	}
}

func requestToken(
	ctx context.Context,
	clientID string,
	deviceCode string,
) (*TokenResponse, error) {
	form := url.Values{}
	form.Set("client_id", clientID)
	form.Set("device_code", deviceCode)
	form.Set("grant_type", deviceGrantType)

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		tokenURL,
		strings.NewReader(form.Encode()),
	)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result TokenResponse

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return &result, nil
}
