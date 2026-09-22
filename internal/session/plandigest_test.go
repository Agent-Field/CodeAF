package session

// The digest that opens a person's turn while a run is live: what it carries,
// what it refuses to carry, its bound, and the two conditions that switch it
// off. Every fixture seeds the store through its own API and calls no model.

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/plandb"
)

// armLiveRun is a test agent whose hand-offs are runs in a plan store: the belt
// asked for, an engine registered, and the graph pointed at a seeded store.
// Those are the three halves of [Config.oneTaskRoad], and all three are needed
// or the digest is correctly absent.
func armLiveRun(t *testing.T, specs ...plandb.TaskSpec) (*Agent, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, planStoreFilename)
	seedPlanStore(t, path, "chat-a", specs...)
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	registerBeltRunEngine(t, &beltRunDouble{})
	armPlanStore(t, agent, path, "chat-a")
	return agent, path
}

// A PERSON WHO SPEAKS WHILE WORK RUNS IS TOLD WHAT IS RUNNING. The digest names
// each row the way the person sees it, says its state, carries its newest note,
// and ends on the sentence that says what to do about it.
func TestTheDigestNamesEveryLiveRowItsStateAndItsNewestNote(t *testing.T) {
	agent, path := armLiveRun(t,
		plandb.TaskSpec{ID: "alpha", Title: "Move the schema"},
		plandb.TaskSpec{ID: "beta", Title: "Rewrite the importer"},
	)
	store, err := plandb.Open(path, "", "", "", "")
	if err != nil {
		t.Fatalf("open the store to leave a note: %v", err)
	}
	if _, err := store.AddNote("alpha", "t-beta", "the schema move cannot work, the column is gone"); err != nil {
		t.Fatalf("leave the note: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close the seeding handle: %v", err)
	}

	digest := agent.planDigest()
	for _, want := range []string{
		planDigestHeading,
		"Move the schema",
		"Rewrite the importer",
		"note: the schema move cannot work, the column is gone",
		planDigestRule,
	} {
		if !strings.Contains(digest, want) {
			t.Fatalf("the digest does not carry %q:\n%s", want, digest)
		}
	}
}

// AND IT CARRIES NOTHING ELSE. Results, steps and transcripts are what `tasks`
// is for; a digest that grew with the work would spend the context it exists to
// protect. This is the design's "must not", asserted against a store that holds
// all three.
func TestTheDigestCarriesNoResultNoStepAndNoTranscript(t *testing.T) {
	agent, path := armLiveRun(t,
		plandb.TaskSpec{ID: "alpha", Title: "Move the schema"},
		plandb.TaskSpec{ID: "beta", Title: "Rewrite the importer"},
	)
	store, err := plandb.Open(path, "", "", "", "")
	if err != nil {
		t.Fatalf("open the store: %v", err)
	}
	if _, err := store.Claim("alpha", "alpha"); err != nil {
		t.Fatalf("claim the task: %v", err)
	}
	if _, err := store.Done("alpha", "alpha", "THE-RESULT-NOBODY-ASKED-FOR", nil, nil); err != nil {
		t.Fatalf("finish the task: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close the handle: %v", err)
	}
	writePlanTrajectory(t, filepath.Dir(path), "alpha",
		`{"kind":"step","step":1,"command":"THE-COMMAND-NOBODY-ASKED-FOR","observation":"THE-OUTPUT-NOBODY-ASKED-FOR"}`)

	digest := agent.planDigest()
	for _, unwanted := range []string{"THE-RESULT-NOBODY-ASKED-FOR", "THE-COMMAND-NOBODY-ASKED-FOR", "THE-OUTPUT-NOBODY-ASKED-FOR"} {
		if strings.Contains(digest, unwanted) {
			t.Fatalf("the digest carries %q, which belongs to `tasks` and not here:\n%s", unwanted, digest)
		}
	}
}

// THE BOUND IS A CONSTANT AND THE CONSTANT IS THE TEST. A plan wider than
// [planDigestRows] draws that many rows, says how many more there are, and
// leaves the chat to ask — because forty lines in front of every sentence the
// person types is the failure this bound exists to prevent.
func TestTheDigestStopsAtItsBoundAndCountsWhatItLeftOut(t *testing.T) {
	var specs []plandb.TaskSpec
	const wide = planDigestRows + 5
	for i := 0; i < wide; i++ {
		specs = append(specs, plandb.TaskSpec{ID: "task" + strconv.Itoa(i), Title: "Part " + strconv.Itoa(i)})
	}
	agent, _ := armLiveRun(t, specs...)

	digest := agent.planDigest()
	// The root row rides with them, so the plan is one wider than the specs.
	rows := 0
	for _, line := range strings.Split(digest, "\n") {
		if strings.HasPrefix(line, "#") {
			rows++
		}
	}
	if rows != planDigestRows {
		t.Fatalf("the digest drew %d rows, want the bound of %d:\n%s", rows, planDigestRows, digest)
	}
	over := wide + 1 - planDigestRows
	if want := fmt.Sprintf("… and %d more", over); !strings.Contains(digest, want) {
		t.Fatalf("the digest does not say %q:\n%s", want, digest)
	}
}

// OFF WHEN NOTHING IS LIVE. A run whose every row has ended is a run nobody can
// steer, so its rows in front of every sentence would be history nobody asked
// for — and a conversation that never handed work out reads nothing at all.
func TestTheDigestIsAbsentWithNoRunAndWithAFinishedOne(t *testing.T) {
	bare, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	registerBeltRunEngine(t, &beltRunDouble{})
	if digest := bare.planDigest(); digest != "" {
		t.Fatalf("a conversation with no run drew a digest:\n%s", digest)
	}

	agent, path := armLiveRun(t, plandb.TaskSpec{ID: "alpha", Title: "Move the schema"})
	store, err := plandb.Open(path, "", "", "", "")
	if err != nil {
		t.Fatalf("open the store: %v", err)
	}
	// The run ends the way a person's stop ends it — the root and everything
	// open under it in one write — which is the shape of an over run the digest
	// has to be silent about.
	if err := store.StopRoot("stopped"); err != nil {
		t.Fatalf("end the run: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close the handle: %v", err)
	}
	if digest := agent.planDigest(); digest != "" {
		t.Fatalf("a run whose every row has ended drew a digest:\n%s", digest)
	}
}

// AND ABSENT ON THE NODE ROAD, where there is no plan to digest at all. The
// engine is what makes a hand-off a run ([Config.oneTaskRoad]); with none
// registered the same armed store must draw nothing, because a conversation
// whose work is nodes of its own tree has no rows of this kind to be told about.
func TestTheDigestIsAbsentWhereHandOffsAreNotRuns(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, planStoreFilename)
	seedPlanStore(t, path, "chat-a", plandb.TaskSpec{ID: "alpha", Title: "Move the schema"})
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	registerBeltRunEngine(t, nil)
	armPlanStore(t, agent, path, "chat-a")
	if digest := agent.planDigest(); digest != "" {
		t.Fatalf("a conversation whose hand-offs are not runs drew a digest:\n%s", digest)
	}
}

// THE PERSON NEVER SEES IT. The model reads the digest and their sentence; the
// journal keeps their sentence alone, which is what makes a resume, an export
// and the transcript on screen show what they actually typed.
func TestTheDigestRidesInTheMessageAndNotInTheJournal(t *testing.T) {
	const said = "actually, skip the migration"
	message := planDigested("WORK RUNNING RIGHT NOW, while the person is speaking:\n#1 · Move the schema · running", said)
	if got := messageContentText(message.message); !strings.Contains(got, "Move the schema") || !strings.Contains(got, said) {
		t.Fatalf("what the model reads = %q, want the digest and their sentence", got)
	}
	if message.said != said {
		t.Fatalf("what the journal keeps = %q, want their own sentence alone", message.said)
	}
}

// A ROW OF THE LIVE RUN IS A ROW THE CHAT CAN END. This is the door the digest
// exists to be acted on through, and until it was routed the tool could not find
// these rows at all: measured on the real binary, a conversation that had
// correctly worked out which row the person's change of mind had made wrong was
// answered `No task "2" in this project` four times, and then told the person it
// had stopped work it had not stopped.
//
// A part the run made for itself has no number of its own, so it goes through the
// store the way the task page's own stop does; both are asserted here, because
// the two kinds of row are different things and one working proved nothing about
// the other.
func TestTheChatCanEndOneRowOfALiveRun(t *testing.T) {
	agent, path := armLiveRun(t,
		plandb.TaskSpec{ID: "2", Title: "Move the schema"},
		plandb.TaskSpec{ID: "inner", ParentID: "2", Title: "The part it made for itself"},
	)

	// The part with no number of its own: `#2.1` in the words the model reads.
	answer, ok := agent.stopPlanRow("2.1", "the person dropped it")
	if !ok {
		t.Fatalf("the tool did not recognise #2.1 as a row of this run; it answered %q", answer)
	}
	if !strings.Contains(answer, "stopped") {
		t.Fatalf("the answer to a stop does not say it stopped: %q", answer)
	}
	store, err := plandb.Open(path, "", "", "", "")
	if err != nil {
		t.Fatalf("open the store: %v", err)
	}
	defer func() { _ = store.Close() }()
	if status := store.Task("inner").Status; status != plandb.StatusCancelled {
		t.Fatalf("the part's row is %q after the stop, want cancelled", status)
	}

	// AND A TOKEN THAT NAMES NO ROW OF THIS RUN FALLS THROUGH, so the session
	// tree's own reader answers it exactly as it did before this door existed.
	if answer, ok := agent.stopPlanRow("94", ""); ok {
		t.Fatalf("a token naming no row of this run was claimed by the run: %q", answer)
	}
}
