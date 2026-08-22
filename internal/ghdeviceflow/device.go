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

const deviceCodeURL = "https://github.com/login/device/code"

// DeviceCodeResponse is GitHub's response to a device code request.
type DeviceCodeResponse struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete,omitempty"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
}

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
