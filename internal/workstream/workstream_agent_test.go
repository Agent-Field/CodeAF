package workstream

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"
)

// Contract (a) resolves a conversation to its workstream without creating one.
func TestWorkstreamByAgentResolvesOnlyItsOwner(t *testing.T) {
	s, _, _ := testStore(t)
	ctx := context.Background()
	for _, id := range []string{"", "missing"} {
		if _, err := s.ByAgent(ctx, id); !errors.Is(err, ErrNotFound) {
			t.Fatalf("unheld agent %q: %v", id, err)
		}
	}
	w := createStream(t, s, "Agent")
	got, err := s.ByAgent(ctx, w.AgentID)
	if err != nil || !reflect.DeepEqual(got, w) {
		t.Fatalf("owner: %+v %v", got, err)
	}
}

// Contract (d) includes ownership changing after the version was read.
func TestWorkstreamAgentCannotSaveThePersonsPage(t *testing.T) {
	s, _, _ := testStore(t)
	ctx := context.Background()
	w := createStream(t, s, "Ownership")
	for _, race := range []bool{false, true} {
		owner := "you"
		if race {
			owner = "agent"
		}
		p, _, err := s.CreatePage(ctx, PageCreate{WorkstreamID: w.ID, Title: "Notes", Owner: owner})
		if err != nil {
			t.Fatal(err)
		}
		base := p.Version
		if race {
			you := "you"
			if _, _, err := s.UpdatePage(ctx, p.ID, PagePatch{Owner: &you}); err != nil {
				t.Fatal(err)
			}
		}
		before, _ := s.Page(ctx, p.ID, false)
		history, _ := s.PageHistory(ctx, p.ID, 50)
		stream, _ := s.Get(ctx, w.ID)
		_, change, err := s.SavePage(ctx, p.ID, base, json.RawMessage(`{"type":"doc","content":[]}`), Attribution{By: "agent"})
		if !errors.Is(err, ErrNotAgentPage) || !reflect.DeepEqual(change, Change{}) {
			t.Fatalf("race=%v: %+v %v", race, change, err)
		}
		after, _ := s.Page(ctx, p.ID, false)
		afterHistory, _ := s.PageHistory(ctx, p.ID, 50)
		afterStream, _ := s.Get(ctx, w.ID)
		if !reflect.DeepEqual(before, after) || !reflect.DeepEqual(history, afterHistory) || !reflect.DeepEqual(stream, afterStream) {
			t.Fatalf("race=%v changed a refused page", race)
		}
	}
	_, c, err := s.CreatePage(ctx, PageCreate{WorkstreamID: w.ID, Title: "Wrong owner", Owner: "you", Attribution: Attribution{By: "agent"}})
	if !errors.Is(err, ErrInvalid) || c.ID != "" {
		t.Fatalf("agent creation with person owner: %+v %v", c, err)
	}
}

// Contract (e) keeps the same source and clock in the page and its history.
func TestWorkstreamAgentPagesCarryAttributionAndTime(t *testing.T) {
	s, _, now := testStore(t)
	ctx := context.Background()
	w := createStream(t, s, "Attribution")
	source := &Source{Kind: "chat", ID: w.AgentID, Label: "Plan discussion"}
	who := Attribution{By: "agent", From: source}
	p, _, err := s.CreatePage(ctx, PageCreate{WorkstreamID: w.ID, Title: "Agent notes", Owner: "agent", Attribution: who})
	if err != nil {
		t.Fatal(err)
	}
	workstreamCheckAgentPage(t, p, *now, source)
	*now = now.Add(time.Minute)
	p, _, err = s.SavePage(ctx, p.ID, p.Version, json.RawMessage(emptyDoc), who)
	if err != nil {
		t.Fatal(err)
	}
	workstreamCheckAgentPage(t, p, *now, source)
	history, err := s.PageHistory(ctx, p.ID, 50)
	if err != nil || len(history) != 2 {
		t.Fatalf("history: %+v %v", history, err)
	}
	for _, v := range history {
		if v.SavedBy != "agent" || !reflect.DeepEqual(v.From, source) {
			t.Fatalf("history attribution: %+v", v)
		}
	}
	if !history[0].SavedAt.Equal(*now) {
		t.Fatalf("history clock: %+v", history[0])
	}
}

func workstreamCheckAgentPage(t *testing.T, p Page, now time.Time, source *Source) {
	t.Helper()
	if p.Owner != "agent" || p.ReadByJobs || p.UpdatedBy != "agent" || !p.UpdatedAt.Equal(now) || !reflect.DeepEqual(p.UpdatedFrom, source) {
		t.Fatalf("agent mark: %+v", p)
	}
}
