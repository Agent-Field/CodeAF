package tui3

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
)

// A CONVERSATION'S BILL ON HOME IS COUNTED ONCE, read from real rows on disk.
// The session stamps its books on meta.json, and those books already hold every
// run and every closed task it folded in; the run's own row in the project's
// index carries the same dollars again. The card and the project's facts added
// the two, so a conversation whose only spend was a $2.30 senior-dev run read
// `spent $4.60`.
func TestHomeCountsARunsDollarsOnce(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	transcript := lab.session("-tmp-alpha", "aaaa000000000001", "hand it off", "/tmp/alpha", now.Add(-time.Hour))
	dir := filepath.Dir(transcript)
	meta, err := session.LoadMeta(dir)
	if err != nil {
		t.Fatal(err)
	}
	// What the session's own stamp writes once the run has folded: the books
	// hold the run, and they hold the talking beside it.
	meta.SpentUSD, meta.Tokens = 2.30, 1200
	if err := session.SaveMeta(dir, meta); err != nil {
		t.Fatal(err)
	}
	// What the run's own row in the index writes: the run's dollars alone.
	lab.task("-tmp-alpha", session.TaskIndexEntry{
		ID: "1", Name: "hand-it-off", Label: "Hand it off", Title: "Hand it off",
		Status: string(session.TaskDone), Cost: 2.30, SessionID: "aaaa000000000001",
		StartedAt: now.Add(-50 * time.Minute), EndedAt: now.Add(-30 * time.Minute),
	})

	world := session.ReadWorld(lab.root)
	if len(world.Projects) != 1 || len(world.Projects[0].Sessions) != 1 {
		t.Fatalf("read %+v, want one project holding one conversation", world.Projects)
	}
	project := world.Projects[0]
	row := project.Sessions[0]
	if row.Spend != 2.30 || row.Tasks.Spend != 2.30 {
		t.Fatalf("the rows read books %v and index %v, want $2.30 in both", row.Spend, row.Tasks.Spend)
	}
	if card := homeFacts(row, now); !strings.Contains(card, "spent $2.30") {
		t.Fatalf("the card reads %q, want the run's $2.30 once", card)
	}
	if facts := projectFacts(project, now); !strings.Contains(facts, "spent $2.30") {
		t.Fatalf("the project's facts read %q, want the run's $2.30 once", facts)
	}
}
