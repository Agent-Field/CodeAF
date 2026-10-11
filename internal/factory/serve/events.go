package serve

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory/live"
)

// eventKeepalive keeps an idle HTTP connection alive without waking a re-read.
const eventKeepalive = 20 * time.Second

// events preserves changed ticks and adds durable, typed notifications. Joining
// before reading the cursor closes the replay-to-live race: an unread tick makes
// the next read catch anything appended while replay was being sent.
func (h *Handler) events(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		h.fail(w, http.StatusInternalServerError, "this connection cannot stream events")
		return
	}
	var ticks chan string
	if h.watch != nil {
		ticks = h.watch.hub.join()
		defer h.watch.hub.leave(ticks)
	}
	root := h.eventRoot()
	events, cursor, err := live.Read(root)
	if h.failed(w, err) {
		return
	}
	last := r.Header.Get("Last-Event-ID")
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()
	if last != "" {
		if !h.sendLive(w, r, events, last) {
			return
		}
		known := false
		for _, e := range events {
			if e.ID == last {
				known = true
				break
			}
		}
		if !known {
			if _, err := fmt.Fprint(w, "event: changed\ndata: *\n\n"); err != nil {
				return
			}
		}
		flusher.Flush()
	}
	keepalive := time.NewTicker(eventKeepalive)
	defer keepalive.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-keepalive.C:
			if _, err := fmt.Fprint(w, ": keepalive\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case data := <-ticks:
			events, next, err := live.Read(root)
			if err != nil {
				return
			}
			if !h.sendLive(w, r, events, cursor) {
				return
			}
			cursor = next
			if h.viewerWantsTick(r, data) {
				if _, err := fmt.Fprintf(w, "event: changed\ndata: %s\n\n", data); err != nil {
					return
				}
			}
			if state, ok := r.Context().Value(authErrorKey{}).(*authErrorState); ok && state.err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func (h *Handler) eventRoot() string {
	if h.config.Store != nil {
		return h.config.Store.Root()
	}
	if h.config.Root != "" {
		return h.config.Root
	}
	return h.config.Record.Root()
}

// sendLive replays strictly after a known cursor. An expired or unknown id
// replays the retained window; changed then asks the app for current state.
func (h *Handler) sendLive(w http.ResponseWriter, r *http.Request, events []live.Event, cursor string) bool {
	start := 0
	for i, e := range events {
		if e.ID == cursor {
			start = i + 1
			break
		}
	}
	for _, e := range events[start:] {
		visible := h.viewerWantsLive(r, e)
		if state, ok := r.Context().Value(authErrorKey{}).(*authErrorState); ok && state.err != nil {
			return false
		}
		if !visible {
			continue
		}
		if _, err := fmt.Fprintf(w, "id: %s\nevent: %s\ndata: %s\n\n", e.ID, e.Kind, e.Data); err != nil {
			return false
		}
	}
	return true
}

func (h *Handler) viewerWantsLive(r *http.Request, e live.Event) bool {
	if e.Kind == "machine" && e.Job == 0 {
		handle, err := h.callerHandle(r)
		return err == nil && handle == e.Handle
	}
	return h.viewerWantsTick(r, strconv.Itoa(e.Job))
}
