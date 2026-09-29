package session

import (
	"context"
	"testing"
)

// RunScriptedSession drives one real turn of a real agent (real journal, real
// meta.json stamps, real ledger rows) against a scripted model, inside a cell
// folder at dir, and closes it. It is the writer half of the index-rebuild
// test in package session_test, which cannot reach newTestAgent.
func RunScriptedSession(t *testing.T, dir, text string) {
	t.Helper()
	a, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
		c.SessionFile = Place{Dir: dir}.Transcript()
		c.Place = Place{Dir: dir, Workspace: c.Workspace}
	})
	events, err := a.Submit(context.Background(), text)
	if err != nil {
		t.Fatal(err)
	}
	collect(t, events)
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	a.SettleWrites()
	CloseUsage() // the exit door: no writer may hold the ledger a test then removes
}
