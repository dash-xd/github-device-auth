package router

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// writeSSEEvent marshals data as JSON and writes it as a Server-Sent
// Event of the given type, flushing so the client receives it
// immediately. Any write error is returned so the caller can stop
// sending further events on a dead connection.
func writeSSEEvent(
	w http.ResponseWriter,
	flusher http.Flusher,
	event string,
	data any,
) error {
	payload, err := json.Marshal(data)
	if err != nil {
		return err
	}

	if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, payload); err != nil {
		return err
	}

	flusher.Flush()

	return nil
}
