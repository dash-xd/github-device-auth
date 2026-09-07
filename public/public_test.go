package public

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dash-xd/github-device-auth/internal/ghdeviceflow"
)

func TestErrorAliasesPreserveIdentity(t *testing.T) {
	if !errors.Is(ErrExpiredToken, ghdeviceflow.ErrExpiredToken) {
		t.Fatal("ErrExpiredToken must preserve internal error identity")
	}
	if !errors.Is(ErrAccessDenied, ghdeviceflow.ErrAccessDenied) {
		t.Fatal("ErrAccessDenied must preserve internal error identity")
	}
}

func TestFacadeFunctionSignaturesCompile(t *testing.T) {
	var request func(context.Context, string) (*DeviceCodeResponse, error) = RequestDeviceCode
	var poll func(context.Context, string, string, time.Duration) (*TokenResponse, error) = PollForToken
	var refresh func(context.Context, string, string, string) (*TokenResponse, error) = RefreshAccessToken

	if request == nil || poll == nil || refresh == nil {
		t.Fatal("public facade functions must be available")
	}
}

func TestNewRouterWrapsExistingRouter(t *testing.T) {
	if NewRouter() == nil {
		t.Fatal("NewRouter returned nil")
	}
}
