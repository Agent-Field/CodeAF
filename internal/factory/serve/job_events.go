package serve

import (
	"errors"
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
		if errors.Is(err, errJobVisibilityUnknown) {
			// An ended sign-in keeps its own answer; anything else is a retry.
			if !h.authFailed(w, r) {
				h.fail(w, http.StatusServiceUnavailable, err.Error())
			}
			return
		}
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
	root := h.eventRoot()
	got, err := live.ReadOutcomes(root, after, limit)
	// A RESET FROM A GAP IS GIVEN ONCE. Left open, the gap an older writer
	// leaves reset every read until this Factory's own next write.
	if err == nil && got.Reset {
		if healed, healErr := live.HealOutcomes(root); healErr != nil {
			return api.JobEvents{}, healErr
		} else if healed {
			got, err = live.ReadOutcomes(root, after, limit)
		}
	}
	out := api.JobEvents{Events: []api.JobEvent{}, Cursor: got.Cursor, Reset: got.Reset, More: got.More}
	passed := int64(0)
	if after != nil {
		passed = *after
	}
	for _, e := range got.Events {
		visible, known := h.jobEventVisible(r, e.Job)
		if !known {
			// AN OUTCOME NOBODY COULD DECIDE IS NOT PASSED. The cursor
			// used to move past it as past a hidden one, and the caller,
			// who saves the cursor, lost the outcome for good when its
			// repository check had only failed for a moment.
			if len(out.Events) == 0 && after != nil && passed == *after {
				return api.JobEvents{}, errJobVisibilityUnknown
			}
			out.Cursor, out.More = passed, true
			break
		}
		if visible {
			out.Events = append(out.Events, e)
		}
		passed = e.Seq
	}
	return out, err
}

// errJobVisibilityUnknown is the read that would start at an outcome whose
// repository GitHub could not answer for: the caller tries again later, from
// the same cursor.
var errJobVisibilityUnknown = errors.New("GitHub did not answer whether you can see a job's repository; try again")

// jobEventVisible checks a job's repository against the caller's access from
// the saved job. A cursor read is bookkeeping: it must not look up the job's
// author on GitHub, or write the job, as a person's own read of it does.
// known is false when GitHub gave no answer for that repository just now.
func (h *Handler) jobEventVisible(r *http.Request, id int) (visible, known bool) {
	if h.config.Store == nil {
		return h.viewerWantsLive(r, live.Event{Kind: live.OutcomeKind, Job: id}), true
	}
	if _, filter := h.repoAccess(r, nil); !filter {
		return true, true
	}
	it, err := h.config.Store.GetSaved(id)
	if err != nil {
		return false, true
	}
	repo := itemRepo(it)
	allow, unknown, filter := h.repoAccessDecided(r, []string{repo})
	if !filter {
		return true, true
	}
	if unknown[repo] {
		return false, false
	}
	return repo != "" && allow[repo], true
}
