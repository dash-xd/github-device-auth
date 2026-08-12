package ghdeviceflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

const refreshGrantType = "refresh_token"

var ErrInvalidRefreshToken = errors.New("refresh token is invalid or expired")

// RefreshAccessToken exchanges a refresh token for a new access token.
//
// Unlike the device flow, this grant requires the app's client secret,
// since only confidential clients (GitHub Apps with "Expire user access
// tokens" enabled) issue refresh tokens.
func RefreshAccessToken(
	ctx context.Context,
	clientID string,
	clientSecret string,
	refreshToken string,
) (*TokenResponse, error) {
	form := url.Values{}
	form.Set("client_id", clientID)
	form.Set("client_secret", clientSecret)
	form.Set("grant_type", refreshGrantType)
	form.Set("refresh_token", refreshToken)

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

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf(
			"github token refresh returned HTTP %d",
			resp.StatusCode,
		)
	}

	var result TokenResponse

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	switch result.Error {
	case "":
		// No error reported; fall through to the success check below.

	case "bad_refresh_token":
		return nil, ErrInvalidRefreshToken

	default:
		return nil, fmt.Errorf(
			"github authentication failed: %s: %s",
			result.Error,
			result.Description,
		)
	}

	if result.AccessToken == "" {
		return nil, errors.New(
			"github returned success without an access token",
		)
	}

	return &result, nil
}
