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

// RefreshAccessToken exchanges a refresh token for a new access token.
//
// clientSecret is optional: a refresh token issued via the device flow
// belongs to a public client and refreshes without one. Pass an empty
// string for those. Confidential clients (traditional OAuth Apps, or
// GitHub Apps that issued the refresh token some other way) still require
// their client secret; if one is required but missing or wrong, GitHub
// reports it as ErrIncorrectClientCredentials rather than succeeding.
func RefreshAccessToken(
	ctx context.Context,
	clientID string,
	clientSecret string,
	refreshToken string,
) (*TokenResponse, error) {
	form := url.Values{}
	form.Set("client_id", clientID)
	form.Set("grant_type", refreshGrantType)
	form.Set("refresh_token", refreshToken)

	if clientSecret != "" {
		form.Set("client_secret", clientSecret)
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		TokenURL,
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

	case "incorrect_client_credentials":
		return nil, ErrIncorrectClientCredentials

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
