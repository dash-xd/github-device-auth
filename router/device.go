package router

import (
	"context"
	"net/http"
	"os"
	"time"

	"github.com/dash-xd/github-device-auth/internal/ghdeviceflow"
)

type deviceResponse struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete,omitempty"`

	ExpiresIn int `json:"expires_in"`
	Interval  int `json:"interval"`

	Instruction string `json:"instruction"`
}

func handleDevice(w http.ResponseWriter, r *http.Request) {
	clientID := os.Getenv("GITHUB_CLIENT_ID")

	if clientID == "" {
		http.Error(
			w,
			"GitHub client ID is not configured",
			http.StatusInternalServerError,
		)
		return
	}

	ctx, cancel := context.WithTimeout(
		r.Context(),
		15*time.Second,
	)
	defer cancel()

	device, err := ghdeviceflow.RequestDeviceCode(
		ctx,
		clientID,
	)
	if err != nil {
		http.Error(
			w,
			"failed to start GitHub device flow",
			http.StatusBadGateway,
		)
		return
	}

	response := deviceResponse{
		DeviceCode:              device.DeviceCode,
		UserCode:                device.UserCode,
		VerificationURI:         device.VerificationURI,
		VerificationURIComplete: device.VerificationURIComplete,
		ExpiresIn:               device.ExpiresIn,
		Interval:                device.Interval,
		Instruction: "Open " +
			device.VerificationURI +
			" and enter the code " +
			device.UserCode,
	}

	writeJSON(
		w,
		http.StatusOK,
		response,
	)
}
