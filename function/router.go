// Package function is the lightweight HTTP entry point deployed as a
// GCP Cloud Function (2nd gen). It wires routes to handlers and delegates
// all GitHub device-flow and token logic to the ghdeviceflow library.
package function

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

var router = newRouter()

func newRouter() *chi.Mux {
	r := chi.NewRouter()

	r.Post("/auth/github/device", handleDevice)
	r.Post("/auth/github/poll", handlePoll)
	r.Post("/auth/github/refresh", handleRefresh)

	return r
}

// Main is the exported entry point invoked by the Cloud Functions runtime.
func Main(w http.ResponseWriter, r *http.Request) {
	router.ServeHTTP(w, r)
}
