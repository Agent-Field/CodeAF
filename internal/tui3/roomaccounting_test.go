package tui3

// ── A TASK'S CALLS ARE THE SESSION'S CALLS ──────────────────────────────────
//
// The header instrument counts a node's calls and the Σ segment counts the
// session's background work, and until this landed they were two clocks: the
// room's reducer had no `closed` hook at all, so a task could start three
// servers and the segment said nothing, the quit guard let a person walk away
// from them, and any figure the status line was already showing went on being
// served from a cache no task event could drop.
//
// docs/design/lens/DESIGN.md, Decision 4: "a room's closed tools feed the same
// HUD/spend walk chat's do, so the header instrument and the ambient counters
// are one set of numbers."

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// nodeRunsBackgroundJob drives one background `bash` to a clean close on the
// open room's lane — the shape that starts a process and leaves it running.
func nodeRunsBackgroundJob(t *testing.T, a *app, id, output string) {
	t.Helper()
	drive(t, a, roomEventMsg{gen: a.room.gen, ev: session.Event{
		Kind: session.EventToolBegin, Tool: "bash", CallID: id, Hint: "bash serve",
		Args: `{"cmd":"npm run dev","background":"true"}`,
	}})
	drive(t, a, roomEventMsg{gen: a.room.gen, ev: session.Event{
		Kind: session.EventToolEnd, Tool: "bash", CallID: id, Output: output,
	}})
}

// A JOB A TASK STARTED IS A JOB THIS SESSION STARTED. It reaches the same count
// the conversation's own background work reaches, because it is the same fact
// about the same machine.
func TestABackgroundJobATaskStartedReachesTheAmbientCounts(t *testing.T) {
	a, _, _ := roomApp(t)
	a.openRoom(7, "Port the loader")
	if before := a.hudStats().jobs; before != 0 {
		t.Fatalf("the session already counts %d jobs", before)
	}

	nodeRunsBackgroundJob(t, a, "j1", "job 3 started; log at /tmp/3.log")

	if got := a.hudStats().jobs; got != 1 {
		t.Fatalf("a task started a background job and the session counts %d", got)
	}
	// AND THE QUIT WARNING NAMES IT. This is the reason the count is worth
	// having: walking away from a server a task started is the same mistake as
	// walking away from one you started yourself, and the warning is the only
	// place the surface says so.
	if word := quitStoppingWord(a); !strings.Contains(word, "job") {
		t.Errorf("the quit warning says %q while a task's background job is up", word)
	}
}

// AND IT SURVIVES CLOSING THE PAGE. A room's entries are dropped when it closes
// ([app.closeRoom]), so a count that walked them would fall to zero the moment
// somebody stopped looking — which is a count nobody can act on.
func TestATasksJobStaysCountedAfterItsPageIsClosed(t *testing.T) {
	a, _, _ := roomApp(t)
	a.openRoom(7, "Port the loader")
	nodeRunsBackgroundJob(t, a, "j1", "job 3 started; log at /tmp/3.log")
	a.closeRoom()
	if got := a.hudStats().jobs; got != 1 {
		t.Fatalf("closing the page took the job away: %d", got)
	}
}

// AND A JOB THE TASK STARTED BEFORE ANYBODY LOOKED IS COUNTED THE MOMENT SOMEBODY
// DOES. This is the case the first cut of this got wrong: the reducer only ever
// sees a node's LIVE calls, so hooking it alone counted nothing for the ordinary
// visit — you open a task's page after it has been working, not before.
func TestAJobATaskStartedBeforeThePageWasOpenedIsCountedOnOpening(t *testing.T) {
	a, fake, _ := roomApp(t)
	fake.journal = roomJournal(t,
		`{"type":"message","role":"user","content":"Bring the server up"}`,
		`{"type":"message","role":"assistant","content":"Starting it.","toolCalls":[{"id":"b1","function":{"name":"bash","arguments":"{\"cmd\":\"npm run dev\",\"background\":\"true\"}"}}]}`,
		`{"type":"message","role":"tool","toolCallId":"b1","content":"job 3 started; log at /tmp/3.log"}`,
		`{"type":"message","role":"assistant","content":"It is up on 3000."}`,
	)
	if before := a.hudStats().jobs; before != 0 {
		t.Fatalf("the session already counts %d jobs", before)
	}
	a.openRoom(7, "Bring the server up")
	if got := a.hudStats().jobs; got != 1 {
		t.Fatalf("opening the page did not learn the job the task had already started: %d", got)
	}
	// AND OPENING IT AGAIN DOES NOT COUNT IT TWICE, which is why the tally is a
	// re-count rather than an accumulator.
	a.closeRoom()
	a.openRoom(7, "Bring the server up")
	if got := a.hudStats().jobs; got != 1 {
		t.Fatalf("a second visit counted the same job again: %d", got)
	}
}

// AND A KILL TAKES AWAY THE JOB IT NAMES, matched against the starts THAT NODE
// made rather than against whatever the conversation started last.
func TestATaskKillingItsOwnJobTakesItOutOfTheCounts(t *testing.T) {
	a, _, _ := roomApp(t)
	a.openRoom(7, "Port the loader")
	nodeRunsBackgroundJob(t, a, "j1", "job 3 started; log at /tmp/3.log")

	drive(t, a, roomEventMsg{gen: a.room.gen, ev: session.Event{
		Kind: session.EventToolBegin, Tool: "jobs", CallID: "k1", Hint: "jobs kill 3",
		Args: `{"action":"kill","id":"3"}`,
	}})
	drive(t, a, roomEventMsg{gen: a.room.gen, ev: session.Event{
		Kind: session.EventToolEnd, Tool: "jobs", CallID: "k1", Output: "job 3 killed",
	}})

	if got := a.hudStats().jobs; got != 0 {
		t.Fatalf("the task killed its job and the session still counts %d", got)
	}
}

// THE HEADER AND THE COUNTS AGREE ABOUT WHAT A FINISHED CALL IS. The header
// counts every call that closed, either way; the ambient sums narrow that to the
// ones that worked, because a failed edit wrote nothing — and both ask
// [callClosed] rather than each carrying its own idea of "finished".
func TestTheHeadersCallCountAndTheAmbientCountsShareOneDefinitionOfFinished(t *testing.T) {
	base := []entry{
		{kind: entryTool, tool: "bash", status: toolOK},
		{kind: entryTool, tool: "bash", status: toolFailed},
		{kind: entryTool, tool: "bash", status: toolRunning},
		{kind: entryAssistant, text: "done", settled: true},
	}
	if got := roomWorkOf(base).calls; got != 2 {
		t.Fatalf("the header counted %d finished calls, want the two that closed", got)
	}
	for i := range base {
		if callClosed(&base[i]) != (i < 2) {
			t.Errorf("callClosed disagrees with the header about entry %d", i)
		}
	}
}
