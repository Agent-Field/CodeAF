package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

type automaticRefuser interface{ AutomaticRefusal(string) string }

// managerFloor is a floor of n jobs where one, the last, is managed by a
// conversation whose transcript exists.
func managerFloor(t *testing.T, n int) (*store.Store, automaticRefuser, string, int) {
	t.Helper()
	st, web, _ := talkLab(t)
	talk := filepath.Join(t.TempDir(), "manager.jsonl")
	if err := os.WriteFile(talk, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	var id int
	for i := 0; i < n; i++ {
		it := factory.Item{Repo: "web", Title: fmt.Sprintf("job %d", i), Body: "a body long enough to cost something to decode"}
		if i == n-1 {
			it.Talk = talk
		}
		made, err := st.Add(context.Background(), it)
		if err != nil {
			t.Fatal(err)
		}
		id = made
	}
	door, ok := runDoor(st, web, "").(automaticRefuser)
	if !ok {
		t.Fatal("the run door does not answer automatic refusals")
	}
	return st, door, talk, id
}

func TestAutomaticRefusalSeesAPersonStopOnTheNextAsk(t *testing.T) {
	st, door, talk, id := managerFloor(t, 5)
	if got := door.AutomaticRefusal(talk); got != "" {
		t.Fatalf("a running job refused: %q", got)
	}
	if err := st.Update(id, func(cur *factory.Item) error { factory.MarkPersonStopped(cur); return nil }); err != nil {
		t.Fatal(err)
	}
	if got := door.AutomaticRefusal(talk); got != factory.PersonStopReason {
		t.Fatalf("the ask after a person's Stop = %q, want the stop", got)
	}
	if got := door.AutomaticRefusal(filepath.Join(t.TempDir(), "helper.jsonl")); got != "" {
		t.Fatalf("a conversation that manages nothing refused: %q", got)
	}
}

func TestAutomaticRefusalFollowsAConversationHandedToAnotherJob(t *testing.T) {
	st, door, talk, id := managerFloor(t, 3)
	if got := door.AutomaticRefusal(talk); got != "" {
		t.Fatalf("a running job refused: %q", got)
	}
	// The conversation moves to a new job, which a person then stops.
	if err := st.Update(id, func(cur *factory.Item) error { cur.Talk = ""; return nil }); err != nil {
		t.Fatal(err)
	}
	next, err := st.Add(context.Background(), factory.Item{Repo: "web", Title: "the next one", Talk: talk})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Update(next, func(cur *factory.Item) error { factory.MarkPersonStopped(cur); return nil }); err != nil {
		t.Fatal(err)
	}
	if got := door.AutomaticRefusal(talk); got != factory.PersonStopReason {
		t.Fatalf("the conversation's new job was not read: %q", got)
	}
}

// Every open job conversation asks ten times a second. An ask must not decode
// the floor: its work stays flat however many jobs the floor holds.
func TestAutomaticRefusalWorkDoesNotGrowWithTheFloor(t *testing.T) {
	allocs := func(n int, conversation func(talk string) string) float64 {
		_, door, talk, _ := managerFloor(t, n)
		ask := conversation(talk)
		door.AutomaticRefusal(ask)
		return testing.AllocsPerRun(20, func() { door.AutomaticRefusal(ask) })
	}
	manager := func(talk string) string { return talk }
	helper := func(talk string) string { return filepath.Join(filepath.Dir(talk), "helper.jsonl") }
	for _, c := range []struct {
		name string
		ask  func(string) string
	}{{"manager", manager}, {"helper", helper}} {
		small, large := allocs(4, c.ask), allocs(80, c.ask)
		if large > small+8 {
			t.Fatalf("%s ask allocations grew with the floor: %v at 4 jobs, %v at 80", c.name, small, large)
		}
		if large > 100 {
			t.Fatalf("%s ask allocates %v times; it decodes documents", c.name, large)
		}
	}
}
