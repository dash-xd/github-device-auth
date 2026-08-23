package router

import (
	_ "embed"
	"net/http"
)

// deviceFlowTestPage is the standalone vanilla HTML/JS test client for
// this router's own endpoints (see router/webui/device-flow-test.html).
// Embedded rather than served off disk so it deploys as part of the
// same Cloud Function binary with nothing extra to package or host -
// visiting the deployed function's own /device-flow-test path is enough
// to exercise the full device flow against itself.
//
//go:embed webui/device-flow-test.html
var deviceFlowTestPage []byte

func handleDeviceFlowTestPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(deviceFlowTestPage)
}
