package mock

import (
	"context"
	"fmt"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// THE MADE-UP FLOOR READS AN ITEM AGAIN THE WAY A REAL ONE WOULD LOOK FROM
// THE SURFACE: the door answers at once with what the read costs, the item is
// busy on the snapshot for a few minutes of the mock's own clock, and then it
// is not. A whole-floor re-read staggers its items, so the handover's count of
// what is done moves while a person watches.

// readCost is what the mock says one cheap read costs.
const readCost = 0.0004

// readTakes is how long one read takes on the mock's clock, and readStagger
// how far apart a whole-floor re-read starts its items.
const (
	readTakes   = 10 * time.Minute
	readStagger = 2 * time.Minute
)

// refreshDoors fills the forge's own doors on s.
func (w *World) refreshDoors(s *factory.Seam) {
	s.Open = func(id int) string {
		w.mu.Lock()
		defer w.mu.Unlock()
		if it := w.item(id); it != nil {
			return itemURL(*it)
		}
		return ""
	}
	s.Refresh = func(ctx context.Context, id int) (float64, error) {
		w.mu.Lock()
		defer w.mu.Unlock()
		if w.item(id) == nil {
			return 0, fmt.Errorf("no item %d on the floor", id)
		}
		if factory.IsDryRun(ctx) {
			return readCost, nil
		}
		w.read(id, w.now.Add(readTakes))
		return readCost, nil
	}
	s.RefreshAll = func(ctx context.Context) (int, float64, error) {
		w.mu.Lock()
		defer w.mu.Unlock()
		var ids []int
		for _, it := range w.items {
			if it.State != factory.StateDismissed {
				ids = append(ids, it.ID)
			}
		}
		if factory.IsDryRun(ctx) || len(ids) == 0 {
			return len(ids), float64(len(ids)) * readCost, nil
		}
		for n, id := range ids {
			w.read(id, w.now.Add(readTakes+time.Duration(n)*readStagger))
		}
		w.readingAll = len(ids)
		return len(ids), float64(len(ids)) * readCost, nil
	}
}

func (w *World) read(id int, until time.Time) {
	if w.reading == nil {
		w.reading = map[int]time.Time{}
	}
	w.reading[id] = until
}

// busyInto writes the reads still out onto a snapshot, and forgets the ones
// the clock has passed.
func (w *World) busyInto(snap *factory.Snapshot) {
	for id, until := range w.reading {
		if !w.now.Before(until) {
			delete(w.reading, id)
			continue
		}
		if snap.Busy == nil {
			snap.Busy = map[int]string{}
		}
		snap.Busy[id] = "refreshing"
	}
	if w.readingAll > 0 {
		left := len(w.reading)
		if left == 0 {
			w.readingAll = 0
			return
		}
		snap.BusyAll = fmt.Sprintf("refreshing %d items · %d done", w.readingAll, w.readingAll-left)
	}
}

// itemURL is an item's page on the forge, made up from its repo and number:
// an issue's or a pull request's, and none for work that lives only here.
func itemURL(it factory.Item) string {
	if it.URL != "" {
		return it.URL
	}
	if it.Origin != factory.OriginForge || it.Num <= 0 || it.Repo == "" {
		return ""
	}
	kind := "issues"
	if it.Kind == factory.KindPR {
		kind = "pull"
	}
	return fmt.Sprintf("https://github.com/%s/%s/%d", it.Repo, kind, it.Num)
}
