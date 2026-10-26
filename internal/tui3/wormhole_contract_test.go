package tui3

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/chatlist"
	machine "github.com/Agent-Field/codeaf/internal/preflight"
	"github.com/Agent-Field/codeaf/internal/session"
)

// TestC5OnlineContinueOpensTheArrivedCopy proves C5 through the home panel:
// the recorded arrival's workspace outranks a stale row's project bucket.
func TestC5OnlineContinueOpensTheArrivedCopy(t *testing.T) {
	lab := newHomeLab(t)
	ws := lab.workspace("project")
	mine := lab.session("project", heldCell, "Mine", ws, time.Now())
	lab.session("copies", "c-run", "Arrived", filepath.Join(lab.root, "gone"), time.Now())
	a := lab.app(mine)
	a.width, a.height = 200, 70
	drain(t, a, a.openHome())
	at := filepath.Join(lab.root, "copies", "c-run")
	work := filepath.Join(at, "work")
	transcript := filepath.Join(at, ".cell", "transcript.jsonl")
	if err := os.MkdirAll(filepath.Dir(transcript), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(work, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(transcript, []byte("{}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	src := &continuationListing{row: continueRows[4]}
	a.machines = src
	a.taker = &fakeTaker{taken: Taken{Transcript: transcript, Elapsed: 3 * time.Second, Resume: machine.Resume{From: "host-a", Now: work, Uncommitted: []string{"README.md"}}}, on: func(id string) {
		src.change(func(row *chatlist.Row) { row.Mine, row.Status = true, chatlist.Here })
		// This is the stale derived row read immediately after the handoff. It has
		// the right transcript and an old or unavailable project path.
		if err := session.SaveMeta(at, session.Meta{ID: id, Title: "Arrived", Workspace: filepath.Join(lab.root, "gone"), Created: time.Now(), LastUserAt: time.Now()}); err != nil {
			t.Fatal(err)
		}
	}}
	drain(t, a, a.askMachines())
	if _, ok := a.homeLineOf("c-run"); !ok {
		t.Fatal("fixture did not expose the local stale row")
	}
	standOn(t, a, "c-run")
	drive(t, a, key("enter"))
	if a.at(pageHome) || a.file != transcript || a.workspace != work {
		t.Fatalf("continue here stayed on home or opened %s; wanted %s\n%s", a.file, transcript, homeText(a))
	}
	if got := plain(lastNote(t, a)); !strings.Contains(got, "Moved from host-a in 3s") {
		t.Fatalf("no arrival banner: %s", got)
	}
	q := askedCard(t, a).question
	if !strings.Contains(q.Reason, "README.md") {
		t.Fatalf("no resume facts after arrival: %+v", q)
	}
}

// TestC18OwnershipReadPreservesTheSendChord proves C18: checking a paired
// chat's holder cannot turn steering or stop-and-replace into a queued message.
func TestC18OwnershipReadPreservesTheSendChord(t *testing.T) {
	for _, chord := range []string{"enter", bargeKey} {
		t.Run(chord, func(t *testing.T) {
			a, agent := bargeable(t, "still answering")
			a.file = filepath.Join(t.TempDir(), "current", ".cell", "transcript.jsonl")
			a.machines = chatlist.Static{{Cell: "current", Mine: true, Status: chatlist.Here}}
			typeInto(t, a, "same holder")
			drive(t, a, key(chord))
			if chord == "enter" && (len(agent.steered) != 1 || agent.steered[0] != "same holder") {
				t.Fatalf("ownership read changed steering into %+v; steered=%v", a.parks, agent.steered)
			}
			if chord == bargeKey && agent.stops != 1 {
				t.Fatalf("ownership read swallowed stop-and-replace; stops=%d parks=%+v", agent.stops, a.parks)
			}
		})
	}
}

// TestC5OwnershipReadKeepsDisplacedChords proves C5: a move arriving before
// the change frame blocks both in-turn send chords without consuming the draft.
func TestC5OwnershipReadKeepsDisplacedChords(t *testing.T) {
	for _, chord := range []string{"enter", bargeKey} {
		t.Run(chord, func(t *testing.T) {
			a, agent := bargeable(t, "still answering")
			a.file = filepath.Join(t.TempDir(), "current", ".cell", "transcript.jsonl")
			a.machines = chatlist.Static{{Cell: "current", Device: "host-a", Status: chatlist.Running}}
			a.taker = &fakeTaker{}
			typeInto(t, a, "keep my draft")
			drive(t, a, key(chord))
			if len(agent.steered) != 0 || agent.stops != 0 || a.input.String() != "keep my draft" || !a.at(pageHome) {
				t.Fatalf("displaced chord steered=%v stops=%d draft=%q home=%v", agent.steered, agent.stops, a.input.String(), a.at(pageHome))
			}
		})
	}
}
