package serve

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/live"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

func TestPolicyBookkeepingLeavesAuthorAccessUnchanged(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodPut} {
		t.Run(method, func(t *testing.T) {
			f := waitsSetup(t)
			job := f.addJob(t, 1)
			f.edit(t, job.ID, func(it *factory.Item) {
				it.Author, it.Group, it.State = "author", "g", factory.StateRunning
			})
			var lookups atomic.Int32
			f.st.SetTierResolver(func(context.Context, factory.Item) (factory.Tier, time.Time, error) {
				lookups.Add(1)
				return factory.TierCollab, time.Time{}, nil
			})
			var out api.Policy
			var body any
			if method == http.MethodPut {
				body = map[string]any{"stopped": true, "allowances": []factory.Allowance{policyAllowance(f, job.ID)}}
			}
			f.request(t, method, "/v1/groups/g/policy", "operator-secret", body, http.StatusOK, &out)
			got, err := f.st.GetSaved(job.ID)
			if err != nil || lookups.Load() != 0 || got.Tier != "" || got.Failure != nil {
				t.Fatalf("policy bookkeeping: lookups=%d tier=%q failure=%+v err=%v", lookups.Load(), got.Tier, got.Failure, err)
			}
			if method == http.MethodPut && !got.StopAfterStep {
				t.Fatal("stopping the group did not arm its job's step boundary")
			}
		})
	}
}

func TestWaitsIgnoreTransientAuthorLookupFailure(t *testing.T) {
	for _, scan := range []bool{false, true} {
		t.Run(map[bool]string{false: "set", true: "release"}[scan], func(t *testing.T) {
			f := waitsSetup(t)
			parent, child := f.addJob(t, 1), f.addJob(t, 0)
			f.edit(t, parent.ID, func(it *factory.Item) {
				it.Author, it.State = "author", factory.StateShipped
				it.CommentURL = "https://github.com/owner/api/issues/1#issuecomment-1"
			})
			f.st.SetTierResolver(func(context.Context, factory.Item) (factory.Tier, time.Time, error) {
				return "", time.Time{}, errors.New("permission lookup unavailable")
			})
			if read, err := f.st.Get(parent.ID); err != nil || read.Failure == nil {
				t.Fatalf("lookup failure fixture: failure=%+v err=%v", read.Failure, err)
			}
			if scan {
				f.edit(t, child.ID, func(it *factory.Item) { it.WaitsOn = []int{parent.ID} })
				f.scan(t)
			} else {
				f.set(t, child.ID, []int{parent.ID}, http.StatusOK)
			}
			got, err := f.st.GetSaved(child.ID)
			if err != nil || len(got.WaitingOn) != 0 || got.Blocked != nil || got.WaitsReleased != scan {
				t.Fatalf("transient lookup changed waits: %+v err=%v", got, err)
			}
		})
	}
}

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
	h.ServeHTTP(w, r)
	got := decodeJobEvents(t, w)
	if got.Reset || got.Cursor != 3 || len(got.Events) != 2 || got.Events[0].Job != 1 || got.Events[1].Job != 3 || lookups.Load() != 0 {
		t.Fatalf("saved visibility: %+v lookups=%d", got, lookups.Load())
	}
}
