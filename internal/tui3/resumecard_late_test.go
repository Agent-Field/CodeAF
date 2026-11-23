package tui3

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/inventory"
	machine "github.com/Agent-Field/codeaf/internal/preflight"
)

// A chat that moved here opens before its files arrive, so the card it opens
// with cannot say what this computer lacks for the project or what is
// uncommitted in the folder. When the files are in and the look is taken, the
// same card says it.
func TestTheSetupCardGainsWhatTheFilesShowOnceTheyHaveArrived(t *testing.T) {
	const chat = "01MOVEDCHATFORTHELATELOOK"
	a, _ := movedApp(t)
	a.file = filepath.Join(t.TempDir(), chat, "transcript.jsonl")
	opened := machine.Resume{
		From:    "laptop",
		Missing: []inventory.Withheld{{Path: ".venv", Lock: "uv.lock"}},
	}
	a.offerSetup(opened)
	if reason := askedCard(t, a).question.Reason; strings.Contains(reason, "also needed") || strings.Contains(reason, "uncommitted") {
		t.Fatalf("the card the open raised already says what only the files can show:\n%s", reason)
	}

	whole := opened
	whole.Lacks = []machine.Item{{Bucket: machine.Installable, Name: "node", Want: "v22.4.0"}}
	whole.Uncommitted = []string{"src/main.go"}
	desk := NewNotices()
	a.useNotices(desk)
	desk.Arrived(chat, whole)
	a.route(noticesMsg{})

	reason := askedCard(t, a).question.Reason
	for _, want := range []string{"also needed: needs node 22.4 — agent can set it up", "src/main.go", "not brought along: .venv (uv sync)"} {
		if !strings.Contains(reason, want) {
			t.Errorf("once the files arrived the card does not say %q:\n%s", want, reason)
		}
	}
	cards := 0
	for _, q := range a.questionOpen() {
		if q.question.Kind == resumeKind {
			cards++
		}
	}
	if cards != 1 {
		t.Fatalf("%d setup cards are up, want the one card carrying the rest", cards)
	}
}

// A look that finishes before the window has landed in the chat is not lost:
// the landing raises the card from it.
func TestALookThatFinishedBeforeTheLandingRaisesTheCardWithIt(t *testing.T) {
	const chat = "01MOVEDCHATLANDSLATE"
	a, _ := movedApp(t)
	a.file = filepath.Join(t.TempDir(), "elsewhere", "transcript.jsonl")
	whole := machine.Resume{From: "laptop", Lacks: []machine.Item{{Bucket: machine.Installable, Name: "node", Want: "v22.4.0"}}}
	desk := NewNotices()
	a.useNotices(desk)
	desk.Arrived(chat, whole)
	a.route(noticesMsg{})
	if _, ok := a.questionHead(); ok {
		t.Fatal("a look about a chat the person is not in raised a card in the chat they are in")
	}
	late, ok := a.takeHeldArrival(chat)
	if !ok || len(late.Lacks) != 1 {
		t.Fatalf("the look was dropped instead of held for the landing: %+v, %v", late, ok)
	}
}

// A look that adds nothing to the card already up raises nothing.
func TestALookThatAddsNothingLeavesTheCardAlone(t *testing.T) {
	const chat = "01MOVEDCHATSAMEFACTS"
	a, _ := movedApp(t)
	a.file = filepath.Join(t.TempDir(), chat, "transcript.jsonl")
	opened := webResume()
	a.offerSetup(opened)
	before := askedCard(t, a)
	desk := NewNotices()
	a.useNotices(desk)
	desk.Arrived(chat, opened)
	a.route(noticesMsg{})
	if after := askedCard(t, a); after.shown != before.shown || after.question.Reason != before.question.Reason {
		t.Fatalf("the card moved under a hand with nothing new to say:\n%+v\n%+v", before, after)
	}
}
