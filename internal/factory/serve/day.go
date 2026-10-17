package serve

import (
	"net/http"
	"sort"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/record"
)

func (h *Handler) factoryDay(w http.ResponseWriter, r *http.Request) {
	if !h.require(w, h.seam.Load != nil) {
		return
	}
	snap, err := h.seam.Load()
	if h.failed(w, err) {
		return
	}
	snap, _, _ = h.forViewer(r, snap)
	if h.authFailed(w, r) {
		return
	}
	now := h.clock()
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	end := start.AddDate(0, 0, 1)
	today := func(t time.Time) bool { return !t.IsZero() && !t.Before(start) && t.Before(end) && !t.After(now) }
	out := api.FactoryDay{ID: start.Format("2006-01-02"), Host: h.factory.Address}
	visible := map[int]bool{}
	started, posted, merged := map[int]bool{}, map[int]bool{}, map[int]bool{}
	waiting := []float64{}
	for _, it := range snap.Items {
		visible[it.ID] = true
		if it.Stream != nil && today(it.Stream.Started) {
			started[it.ID] = true
		}
		if it.PullRequest != nil && it.PullRequest.State == "merged" && today(it.PullRequest.MergedAt) {
			merged[it.ID] = true
		}
		if it.PublishStatus != nil && it.PublishStatus.State == "opened" && today(it.PublishStatus.At) {
			posted[it.ID] = true
		}
		if it.Asking != nil && !it.Asking.At.IsZero() && !it.Asking.At.After(now) {
			waiting = append(waiting, now.Sub(it.Asking.At).Seconds())
		}
	}
	if h.config.Record != nil {
		events, err := h.config.Record.Since(r.Context(), start)
		if h.failed(w, err) {
			return
		}
		for _, event := range events {
			if !visible[event.Job] || !today(event.At) {
				continue
			}
			switch event.Kind {
			case record.Started, record.AutoStarted:
				started[event.Job] = true
			case record.Posted:
				posted[event.Job] = true
			case record.PRMerged:
				merged[event.Job] = true
			}
		}
	}
	out.Started, out.Posted, out.Merged = len(started), len(posted), len(merged)
	if len(waiting) > 0 {
		sort.Float64s(waiting)
		median := waiting[len(waiting)/2]
		if len(waiting)%2 == 0 {
			median = (median + waiting[len(waiting)/2-1]) / 2
		}
		out.WaitingMedianSeconds = &median
	}
	h.write(w, 200, out)
}
