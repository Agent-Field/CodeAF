package main

import (
	"context"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/workstream"
)

func TestWorkstreamBriefNeverWaitsForTheStore(t *testing.T) {
	b := &appBackend{}
	door := &workstreamAgentDoor{backend: b, agentID: "unheld"}
	b.collectionsMu.Lock()
	defer b.collectionsMu.Unlock()
	finished := make(chan bool, 1)
	guard.Go("app/workstream-brief-test", func() {
		brief, ok := door.Brief()
		finished <- !ok && brief == (session.WorkstreamBrief{})
	})
	select {
	case fallback := <-finished:
		if !fallback {
			t.Fatal("a missing snapshot must use the prompt fallback")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the brief waited for the store")
	}
}

func TestWorkstreamBriefNeverWaitsForItsCache(t *testing.T) {
	b := &appBackend{}
	door := &workstreamAgentDoor{backend: b, agentID: "held"}
	b.workstreamRememberBrief(workstream.Workstream{AgentID: "held", Name: "Cached", Why: "A reason"})
	b.workstreams.briefMu.Lock()
	defer b.workstreams.briefMu.Unlock()
	finished := make(chan bool, 1)
	guard.Go("app/workstream-brief-cache-test", func() {
		_, ok := door.Brief()
		finished <- ok
	})
	select {
	case ok := <-finished:
		if ok {
			t.Fatal("a busy snapshot must use the fallback")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the brief waited for its cache")
	}
}

func TestWorkstreamBriefTracksCreateAndRename(t *testing.T) {
	b, _, _, _ := workstreamBackend(t)
	created, err := b.WorkstreamCreate(appwire.WorkstreamCreateParams{Name: "Original", Why: workstreamPtr("Explain usage")})
	if err != nil {
		t.Fatal(err)
	}
	door := &workstreamAgentDoor{backend: b, agentID: created.Workstream.AgentID}
	workstreamCheckBrief(t, door, "Original", "Explain usage")
	_, err = b.WorkstreamUpdate(appwire.WorkstreamUpdateParams{ID: created.Workstream.ID, Patch: appwire.WorkstreamPatch{Name: workstreamPtr("Renamed"), Why: workstreamPtr("Review usage")}})
	if err != nil {
		t.Fatal(err)
	}
	workstreamCheckBrief(t, door, "Renamed", "Review usage")
}

func TestWorkstreamBriefRefreshesWhereTheAppResolvesItsOwner(t *testing.T) {
	cases := []struct {
		name string
		read func(*appBackend, string, *workstreamAgentDoor) error
	}{
		{"get", func(b *appBackend, id string, _ *workstreamAgentDoor) error {
			_, err := b.WorkstreamGet(appwire.WorkstreamGetParams{ID: id})
			return err
		}},
		{"list", func(b *appBackend, _ string, _ *workstreamAgentDoor) error {
			_, err := b.WorkstreamList(appwire.WorkstreamListParams{})
			return err
		}},
		{"done", func(b *appBackend, id string, _ *workstreamAgentDoor) error {
			_, err := b.WorkstreamMarkDone(appwire.WorkstreamMarkDoneParams{ID: id})
			return err
		}},
		{"reopen", func(b *appBackend, id string, _ *workstreamAgentDoor) error {
			_, err := b.WorkstreamReopen(appwire.WorkstreamReopenParams{ID: id})
			return err
		}},
		{"door", func(_ *appBackend, _ string, d *workstreamAgentDoor) error {
			_, err := d.Plan(context.Background())
			return err
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b, _, w, _ := workstreamAgentFixture(t)
			s, err := b.workstreamStore(false)
			if err != nil {
				t.Fatal(err)
			}
			_, _, err = s.Update(context.Background(), w.ID, workstream.Patch{Name: workstreamPtr("Refreshed"), Why: workstreamPtr("Fresh reason")})
			if err != nil {
				t.Fatal(err)
			}
			door := &workstreamAgentDoor{backend: b, agentID: w.AgentID}
			if err := tc.read(b, w.ID, door); err != nil {
				t.Fatal(err)
			}
			workstreamCheckBrief(t, door, "Refreshed", "Fresh reason")
		})
	}
}

func workstreamCheckBrief(t *testing.T, door *workstreamAgentDoor, name, why string) {
	t.Helper()
	brief, ok := door.Brief()
	if !ok || brief.Name != name || brief.Why != why {
		t.Fatalf("brief: %+v %v; want %q / %q", brief, ok, name, why)
	}
}
