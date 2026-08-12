package head

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// The bug, replayed from the user's own journal (2026-08-11): "get me a list
// of new dev tool companies…" commissioned a splice; the workforce had not
// reached it yet when the person said "ok start it", and the loop — whose
// one-turn memory held nothing — commissioned a twin. The pending queue is the
// memory that survives the turn.
func TestAskingAgainWhileTheSpliceWaitsCommissionsNothing(t *testing.T) {
	graph := openHeadStore(t)
	first := postUser(t, graph, "room",
		"get me a list of new dev tool companies startups that have come out of top vc firms in the last 2 months please")
	firstRun := &beltRun{head: New(nil, graph), user: first}
	if answer, failed := firstRun.execute(beltToolSpawn, beltArguments(t, map[string]any{
		"instruction": "Find dev tool startups that came out of top VC firms in the last two months and deliver a written list",
	})); failed {
		t.Fatalf("the first ask was refused: %s", answer)
	}

	// A new turn: the one-turn commissioned map is empty, only the store
	// remembers. "ok start it" spawns with the loop's re-reading of the ask.
	second := postUser(t, graph, "room", "ok start it")
	secondRun := &beltRun{head: New(nil, graph), user: second}
	answer, failed := secondRun.execute(beltToolSpawn, beltArguments(t, map[string]any{
		"instruction": "Find dev tool startups from top VC firms in the last two months and deliver the written list",
	}))
	if failed {
		t.Fatalf("the re-ask was refused outright: %s", answer)
	}
	commands, err := graph.PendingCommands(0)
	if err != nil {
		t.Fatal(err)
	}
	splices := 0
	for _, command := range commands {
		if command.Kind == store.CommandSplice {
			splices++
		}
	}
	if splices != 1 {
		t.Fatalf("the re-ask minted a twin: %d pending splices", splices)
	}
	if !strings.Contains(answer, "already queued") {
		t.Fatalf("the loop was not told the work already waits: %q", answer)
	}
}

// A genuinely different ask in the same window still commissions — a false
// twin would silently swallow a new job, which is worse than a duplicate.
func TestADifferentAskStillCommissionsWhileAnotherWaits(t *testing.T) {
	graph := openHeadStore(t)
	first := postUser(t, graph, "room", "research the dev tool startup market")
	firstRun := &beltRun{head: New(nil, graph), user: first}
	if answer, failed := firstRun.execute(beltToolSpawn, beltArguments(t, map[string]any{
		"instruction": "Research recent dev tool startups from top venture firms and write a report",
	})); failed {
		t.Fatalf("the first ask was refused: %s", answer)
	}

	second := postUser(t, graph, "room", "also check the weather in toronto tomorrow")
	secondRun := &beltRun{head: New(nil, graph), user: second}
	if answer, failed := secondRun.execute(beltToolSpawn, beltArguments(t, map[string]any{
		"instruction": "Report tomorrow's weather in Toronto from an authoritative source",
	})); failed {
		t.Fatalf("the unrelated ask was refused: %s", answer)
	}
	commands, err := graph.PendingCommands(0)
	if err != nil {
		t.Fatal(err)
	}
	splices := 0
	for _, command := range commands {
		if command.Kind == store.CommandSplice {
			splices++
		}
	}
	if splices != 2 {
		t.Fatalf("two different asks journaled %d pending splices", splices)
	}
}
