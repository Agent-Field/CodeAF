package tui3

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
)

type keptFileAgent struct {
	*fakeAgent
	text   string
	files  []remote.WireFile
	images []session.Image
}

func (a *keptFileAgent) SubmitFiles(ctx context.Context, text string, files []remote.WireFile, images []session.Image) (<-chan session.Event, error) {
	a.text = text
	a.files = append([]remote.WireFile(nil), files...)
	a.images = append([]session.Image(nil), images...)
	return a.fakeAgent.Submit(ctx, text)
}

// parkedKeeperLab puts one running conversation with a waiting queue into the
// keeper and leaves an unrelated conversation in front. Tests can then deliver
// the landing edge directly, which is the same contentless nudge the watcher
// sends after it drains a real stream.
func parkedKeeperLab(t *testing.T, parks []parked) (*app, *switchAgent, *fakeAgent, string, *kept) {
	t.Helper()
	heldAgent := &switchAgent{fakeAgent: &fakeAgent{model: "m"}, running: true}
	a := newTestApp(heldAgent)
	a.file = "/tmp/lab/held.jsonl"
	a.stirs = make(chan behindStirMsg, stirDepth)
	a.state = stateWorking
	a.parks = parks

	conv, side := a.front(), a.detachConversation()
	drain(t, a, a.stow(conv, side))
	front := &fakeAgent{model: "m"}
	drain(t, a, a.attachConversation(Conversation{Agent: front, SessionFile: "/tmp/lab/front.jsonl"}, nil))
	key := convKey("/tmp/lab/held.jsonl")
	held := a.behind[key]
	if held == nil {
		t.Fatal("the conversation was not kept")
	}
	t.Cleanup(held.watch.stop)
	return a, heldAgent, front, key, held
}

// landHeld drives the surface half of the edge a watcher raises when its
// current stream closes. The agent's running answer is updated first because
// Attach is the authority behind follow-up priority.
func landHeld(t *testing.T, a *app, agent *switchAgent, key string, held *kept, running bool) {
	t.Helper()
	agent.running = running
	held.watch.landed.Store(true)
	held.watch.armed.Store(true)
	drive(t, a, runCmd(a.behindStir(behindStirMsg{key: key}))...)
}

func awaitWatch(t *testing.T, what string, yes func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !yes() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !yes() {
		t.Fatal(what)
	}
}

// C2: the message is submitted to the held agent, with its picture bytes, and
// the stream returned by that send becomes the watcher's next turn. Nothing is
// cross-routed into the conversation in front.
func TestAWaitingPictureSendsInItsHeldConversationAndTheWatcherAdoptsItsTurn(t *testing.T) {
	dir := t.TempDir()
	picture := filepath.Join(dir, "red.png")
	if err := os.WriteFile(picture, []byte("red square"), 0o600); err != nil {
		t.Fatal(err)
	}
	p := parked{text: "what colour is the square", chips: []chip{{path: picture}}}
	a, agent, front, key, held := parkedKeeperLab(t, []parked{p})

	landHeld(t, a, agent, key, held, false)

	if len(agent.imageText) != 1 || !strings.Contains(agent.imageText[0], "what colour is the square") {
		t.Fatalf("the held picture message was submitted as %q", agent.imageText)
	}
	if len(agent.images) != 1 || len(agent.images[0]) != 1 || string(agent.images[0][0].Bytes) != "red square" {
		t.Fatalf("SubmitImage received %+v", agent.images)
	}
	if len(front.sent) != 0 {
		t.Fatalf("the front conversation received %q", front.sent)
	}
	if len(held.side.parks) != 0 {
		t.Fatalf("the successful message is still waiting: %+v", held.side.parks)
	}
	awaitWatch(t, "the watcher never adopted the submitted turn", held.watch.turning.Load)

	agent.finish()
	awaitWatch(t, "closing the adopted stream produced no next landing edge", held.watch.landed.Load)
}

// The factored start keeps every front-send door available behind the screen:
// standing messages keep their mark, paste chips unfold, and a hosted ordinary
// file crosses the same fileSubmitter seam as the attachment tray.
func TestHeldWaitingMessagesUseStandingPasteAndRemoteFileDoors(t *testing.T) {
	standing := &fakeAgent{model: "m"}
	_, _, start := parkedStart(standing, t.Context(), false, parked{
		text:     "keep [paste 1 · 3 lines] true",
		pastes:   []pasteChip{{n: 1, text: "one\ntwo\nthree"}},
		standing: true,
	})
	if _, err := start(); err != nil {
		t.Fatal(err)
	}
	if len(standing.marked) != 1 || !strings.Contains(standing.marked[0], "one\ntwo\nthree") {
		t.Fatalf("the marked paste went through as %q", standing.marked)
	}

	document := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.WriteFile(document, []byte("remote document"), 0o600); err != nil {
		t.Fatal(err)
	}
	far := &keptFileAgent{fakeAgent: &fakeAgent{model: "m"}}
	_, _, start = parkedStart(far, t.Context(), true, parked{text: "read this", chips: []chip{{path: document, file: true}}})
	if _, err := start(); err != nil {
		t.Fatal(err)
	}
	if len(far.files) != 1 || far.files[0].Name != "notes.txt" || string(far.files[0].Bytes) != "remote document" {
		t.Fatalf("the hosted file road received %+v", far.files)
	}
	if len(far.images) != 0 || far.text != "read this" {
		t.Fatalf("the hosted file road sent text=%q images=%+v", far.text, far.images)
	}
}

// C3: every landing spends one queue head. A session follow-up that is already
// running is adopted first, and an in-flight held submit prevents a front close
// from sending the same head again.
func TestHeldWaitingMessagesGoOnePerLandingAfterSessionFollowUpsAndNeverTwice(t *testing.T) {
	a, agent, _, key, held := parkedKeeperLab(t, []parked{{text: "one"}, {text: "two"}})

	// The first landing has already opened the session's ctrl+q follow-up.
	landHeld(t, a, agent, key, held, true)
	if len(agent.sent) != 0 || len(held.side.parks) != 2 {
		t.Fatalf("a parked message jumped the follow-up: sent=%q parks=%+v", agent.sent, held.side.parks)
	}
	awaitWatch(t, "the watcher did not adopt the session follow-up", held.watch.turning.Load)

	// Its close offers exactly the oldest parked message.
	landHeld(t, a, agent, key, held, false)
	if got := strings.Join(agent.sent, ","); got != "one" {
		t.Fatalf("the first landing sent %q", got)
	}
	if len(held.side.parks) != 1 || held.side.parks[0].text != "two" {
		t.Fatalf("the first landing left %+v", held.side.parks)
	}
	awaitWatch(t, "the watcher did not adopt the first parked turn", held.watch.turning.Load)

	// The turn that message started closes, and only then does the second go.
	agent.finish()
	awaitWatch(t, "the first parked turn produced no landing edge", held.watch.landed.Load)
	landHeld(t, a, agent, key, held, false)
	if got := strings.Join(agent.sent, ","); got != "one,two" {
		t.Fatalf("two landing edges sent %q", got)
	}
	if len(held.side.parks) != 0 {
		t.Fatalf("the queue still holds %+v", held.side.parks)
	}

	// Returning during the crossing carries the marker with the queue. The
	// ordinary front close must stand down until that one result spends it.
	a.parks = []parked{{text: "do not duplicate", sending: true}}
	a.parkSending = true
	if cmd := a.sendParked(); cmd != nil {
		t.Fatal("the front tried to send a held submit a second time")
	}
}

func TestComingBackDuringAHeldSendNeverSendsItTwice(t *testing.T) {
	a, agent, _, key, held := parkedKeeperLab(t, []parked{{text: "only once"}})
	agent.running = false
	cmd := a.sendBehindParked(key, held)
	if cmd == nil || !held.side.parkSending || !held.side.parks[0].sending {
		t.Fatal("the held send did not mark its queue head before crossing")
	}

	// Come forward before the off-loop Submit returns. The marker travels with
	// the sidecar, so neither a close nor an explicit send can spend it again.
	forward, ours := a.bringForward("/tmp/lab/held.jsonl")
	if !ours {
		t.Fatal("the sending conversation could not come forward")
	}
	drain(t, a, forward)
	if !a.parkSending || len(a.parks) != 1 || !a.parks[0].sending {
		t.Fatalf("the crossing came forward as sending=%v parks=%+v", a.parkSending, a.parks)
	}
	if again := a.sendParked(); again != nil {
		t.Fatal("the front started the crossing message again")
	}

	drive(t, a, cmd())
	if got := strings.Join(agent.sent, ","); got != "only once" {
		t.Fatalf("the message was submitted as %q", got)
	}
	if a.parkSending || len(a.parks) != 0 {
		t.Fatalf("the successful crossing left sending=%v parks=%+v", a.parkSending, a.parks)
	}
}

// C4: if the answer has already ended by the time stow asks, there is no future
// close edge to wait for. The command returned by stow starts the queue head at
// once.
func TestAnAlreadyIdleTurnSendsItsWaitingMessageAtStow(t *testing.T) {
	agent := &switchAgent{fakeAgent: &fakeAgent{model: "m"}}
	a := newTestApp(agent)
	a.file = "/tmp/lab/idle.jsonl"
	a.stirs = make(chan behindStirMsg, stirDepth)
	a.parks = []parked{{text: "send me now"}}
	conv, side := a.front(), a.detachConversation()
	cmd := a.stow(conv, side)
	if cmd == nil {
		t.Fatal("stow stranded a queue whose turn was already idle")
	}
	drive(t, a, cmd())
	if got := strings.Join(agent.sent, ","); got != "send me now" {
		t.Fatalf("the idle stow sent %q", got)
	}
	if held := a.behind[convKey(conv.SessionFile)]; held == nil || len(held.side.parks) != 0 {
		t.Fatalf("the idle stow left %+v", held)
	} else {
		t.Cleanup(held.watch.stop)
	}
}

// C6: held conversations still write every waiting word into the plain crash
// draft, while the structured keep contains only the actual box.
func TestStowDraftsFoldsWaitingWordsIntoCrashInsurance(t *testing.T) {
	dir := t.TempDir()
	draft := filepath.Join(dir, "draft.txt")
	a := newTestApp(&fakeAgent{model: "m"})
	side := &aside{draft: "half typed", parks: []parked{{text: "first waiting"}, {text: "second waiting"}}}
	a.stowDrafts(Conversation{DraftFile: draft, Workspace: dir, SessionFile: filepath.Join(dir, "chat.jsonl")}, side)
	if got := readDraft(draft); got != "half typed\nfirst waiting\nsecond waiting" {
		t.Fatalf("the held crash draft is %q", got)
	}
}

// C8: the queue head is not spent until Submit succeeds. A refusal keeps all
// of its structured data and its note waits for the conversation it belongs to.
func TestAFailedHeldSendStaysWaitingWithItsAttachmentsAndNotesTheFailure(t *testing.T) {
	picture := filepath.Join(t.TempDir(), "still-here.png")
	if err := os.WriteFile(picture, []byte("picture"), 0o600); err != nil {
		t.Fatal(err)
	}
	p := parked{
		text:   "try this file",
		chips:  []chip{{path: picture}},
		pastes: []pasteChip{{n: 1, text: "one\ntwo\nthree"}},
	}
	a, agent, _, key, held := parkedKeeperLab(t, []parked{p})
	agent.failing = errors.New("agent closed")
	landHeld(t, a, agent, key, held, false)

	if len(held.side.parks) != 1 || held.side.parks[0].text != p.text || len(held.side.parks[0].chips) != 1 || len(held.side.parks[0].pastes) != 1 || held.side.parks[0].sending {
		t.Fatalf("the failed message changed to %+v", held.side.parks)
	}
	if len(held.side.parkNotes) != 1 || held.side.parkNotes[0] != "submit failed: agent closed" {
		t.Fatalf("the held failure notes are %q", held.side.parkNotes)
	}

	cmd, ours := a.bringForward("/tmp/lab/held.jsonl")
	if !ours {
		t.Fatal("the failed conversation could not come forward")
	}
	drain(t, a, cmd)
	if len(a.parks) != 1 || len(a.parks[0].chips) != 1 || len(a.parks[0].pastes) != 1 {
		t.Fatalf("the failed queue came forward as %+v", a.parks)
	}
	if !strings.Contains(plain(lastNote(t, a)), "submit failed: agent closed") {
		t.Fatalf("the conversation did not receive its submit failure: %q", plain(lastNote(t, a)))
	}
}
