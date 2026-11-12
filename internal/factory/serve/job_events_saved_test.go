package serve

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/live"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// The outcome cursor hides a job by its saved repository: reading it looks up
// no author access on GitHub, however many jobs the page crosses.
func TestJobEventsVisibilityUsesSavedAuthorAccess(t *testing.T) {
	h, clock, github := visibilityFloor(t, map[string]map[string]bool{"priya": {"acme/api": true}})
	token := signViewer(t, h, github, clock, "priya")
	st, err := store.Open(h.eventRoot())
	if err != nil {
		t.Fatal(err)
	}
	snap, err := h.seam.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range snap.Items {
		it.ID, it.Num, it.Author = 0, 1, "author"
		if _, err := st.Create(it); err != nil {
			t.Fatal(err)
		}
	}
	h.config.Store = st
	h.seam.Load = func() (factory.Snapshot, error) {
		items, err := st.List()
		return factory.Snapshot{Items: items}, err
	}
	var lookups atomic.Int32
	st.SetTierResolver(func(context.Context, factory.Item) (factory.Tier, time.Time, error) {
		lookups.Add(1)
		return factory.TierCollab, time.Time{}, nil
	})
	for _, id := range []int{1, 2, 3} {
		if err := live.Append(st.Root(), live.New(live.OutcomeKind, id, live.Outcome{Job: id, Kind: "finished"})); err != nil {
			t.Fatal(err)
		}
	}
	r := httptest.NewRequest(http.MethodGet, "/v1/job-events?after=0", nil)
	r.RemoteAddr = "192.0.2.1:9"
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	// The handler is called directly: request settlement reads the queue for
	// the person before routing, and this is the cursor's own check.
	h.jobEvents(w, authRequest(r))
	got := decodeJobEvents(t, w)
	if got.Reset || got.Cursor != 3 || len(got.Events) != 2 || got.Events[0].Job != 1 || got.Events[1].Job != 3 || lookups.Load() != 0 {
		t.Fatalf("saved visibility: %+v lookups=%d", got, lookups.Load())
	}
}
