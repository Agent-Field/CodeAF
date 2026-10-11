package serve

import (
	"fmt"
	"net/http"
)

// events is GET /v1/events. The body is a server-sent stream of changed
// ticks. Nothing about a job rides on a tick: the data is a job id, or *,
// and the client reads the floor again.
func (h *Handler) events(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		h.fail(w, http.StatusInternalServerError, "this connection cannot stream events")
		return
	}
	// Join before the headers, so a write that lands once the client has
	// seen the response is not missed. A client that connects late re-reads
	// the floor; the stream does not replay.
	var ticks chan string
	if h.watch != nil {
		ticks = h.watch.hub.join()
		defer h.watch.hub.leave(ticks)
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()
	if ticks == nil {
		<-r.Context().Done()
		return
	}
	for {
		select {
		case <-r.Context().Done():
			return
		case data := <-ticks:
			fmt.Fprintf(w, "event: changed\ndata: %s\n\n", data)
			flusher.Flush()
		}
	}
}
