// Command serve runs the GitHub device-flow HTTP API locally: the same
// router.NewRouter() the deployed Cloud Function serves, wrapped in a
// plain net/http server instead. It talks to GitHub's own OAuth endpoints
// directly and has no GCP dependency at all, so it's useful both for local
// development and for CI that wants to exercise a real device/refresh flow
// without needing GCP credentials or the deployed (private) Cloud Run
// endpoint reachable at all.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/dash-xd/github-device-auth/router"
)

func main() {
	if os.Getenv("GITHUB_CLIENT_ID") == "" {
		log.Fatal("GITHUB_CLIENT_ID is required")
	}

	host := os.Getenv("HOST")
	if host == "" {
		// Loopback only - this is meant to be reached from the same
		// machine/job that started it, not the network.
		host = "127.0.0.1"
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8089"
	}

	srv := &http.Server{
		Addr:    host + ":" + port,
		Handler: router.NewRouter(),
	}

	serveErr := make(chan error, 1)

	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
			return
		}
		close(serveErr)
	}()

	log.Printf("github-device-auth router listening on http://%s", srv.Addr)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-serveErr:
		log.Fatalf("serve: %v", err)

	case <-stop:
		log.Print("shutting down")

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := srv.Shutdown(ctx); err != nil {
			log.Fatalf("shutdown: %v", err)
		}
	}
}
