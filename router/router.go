// Package router builds the chi router for the GitHub device-flow HTTP
// API. It wires routes to handlers and delegates all GitHub device-flow
// and token logic to the ghdeviceflow library.
//
// It exports NewRouter so a generic Cloud Functions entry point (such as
// gospace-minimal) can import this package and serve it, without this
// repo needing to know anything about how or where it's deployed.
package router

import (
	"github.com/go-chi/chi/v5"
)

// NewRouter builds the chi router for the GitHub device-flow HTTP API.
func NewRouter() *chi.Mux {
	r := chi.NewRouter()

	r.Use(corsMiddleware)

	r.Post("/auth/github/device", handleDevice)
	r.Post("/auth/github/poll", handlePoll)
	r.Post("/auth/github/refresh", handleRefresh)
	r.Post("/auth/github/token", handleToken)

	r.Get("/device-flow-test", handleDeviceFlowTestPage)

	return r
}
