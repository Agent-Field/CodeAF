package serve

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/live"
)

type jobEventsQuery struct {
	after *int64
	limit int
	wait  time.Duration
}

func parseJobEvents(q url.Values) (jobEventsQuery, error) {
	out := jobEventsQuery{limit: 100}
	if q.Has("after") {
		n, err := strconv.ParseInt(q.Get("after"), 10, 64)
		if err != nil || n < 0 || len(q["after"]) != 1 {
			return out, fmt.Errorf("after must be an integer from 0")
		}
		out.after = &n
	}
	if q.Has("limit") {
		n, err := strconv.Atoi(q.Get("limit"))
		if err != nil || n < 1 || n > 500 || len(q["limit"]) != 1 {
			return out, fmt.Errorf("limit must be an integer from 1 to 500")
		}
		out.limit = n
	}
	if q.Has("wait") {
		n, err := strconv.Atoi(q.Get("wait"))
		if err != nil || n < 0 || n > 15 || len(q["wait"]) != 1 {
			return out, fmt.Errorf("wait must be an integer from 0 to 15")
		}
		out.wait = time.Duration(n) * time.Second
	}
	return out, nil
}

// jobEvents joins before reading so an outcome between the read and wait is kept.
func (h *Handler) jobEvents(w http.ResponseWriter, r *http.Request) {
	q, err := parseJobEvents(r.URL.Query())
	if err != nil {
		h.fail(w, http.StatusBadRequest, err.Error())
		return
	}
	var ticks chan string
	if q.wait > 0 && h.watch != nil {
		ticks = h.watch.hub.join()
		defer h.watch.hub.leave(ticks)
	}
	timer := time.NewTimer(q.wait)
	defer timer.Stop()
	for {
		out, err := h.visibleJobEvents(r, q.after, q.limit)
		if h.failed(w, err) || h.authFailed(w, r) {
			return
		}
		if out.Reset || len(out.Events) != 0 || ticks == nil {
			h.write(w, http.StatusOK, out)
			return
		}
		// Move past hidden outcomes even when the long poll continues waiting.
		cursor := out.Cursor
		q.after = &cursor
		if out.More {
			continue
		}
		select {
		case <-r.Context().Done():
			return
		case <-timer.C:
			h.write(w, http.StatusOK, out)
			return
		case <-ticks:
		}
	}
}

func (h *Handler) visibleJobEvents(r *http.Request, after *int64, limit int) (api.JobEvents, error) {
	got, err := live.ReadOutcomes(h.eventRoot(), after, limit)
	out := api.JobEvents{Events: []api.JobEvent{}, Cursor: got.Cursor, Reset: got.Reset, More: got.More}
	for _, e := range got.Events {
		if h.jobEventVisible(r, e.Job) {
			out.Events = append(out.Events, e)
		}
	}
	return out, err
}

// Outcome cursors check repository visibility without hydrating author access.
func (h *Handler) jobEventVisible(r *http.Request, id int) bool {
	if h.config.Store == nil {
		return h.viewerWantsLive(r, live.Event{Kind: live.OutcomeKind, Job: id})
	}
	if _, filter := h.repoAccess(r, nil); !filter {
		return true
	}
	it, err := h.config.Store.GetSaved(id)
	return err == nil && h.repoVisible(r, itemRepo(it))
}
