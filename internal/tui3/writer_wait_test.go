package tui3

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/Agent-Field/codeaf/internal/chatlist"
)

type stoppedWriter struct{ *fakeAgent }

func (stoppedWriter) RenewalStatus() (time.Time, time.Duration, bool) {
	return time.Time{}, time.Second, false
}
func TestAWriterWaitCannotReviveADisplacedWriter(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	pending := &startingSendAgent{ready: make(chan struct{}), observed: make(chan struct{}, 1)}
	if held, err := awaitSendWriter(ctx, pending); held || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled wait: %v %v", held, err)
	}
	ctx, deadline := context.WithDeadline(t.Context(), time.Unix(1, 0))
	defer deadline()
	if held, err := awaitSendWriter(ctx, pending); held || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expired wait: %v %v", held, err)
	}
	if held, err := awaitSendWriter(t.Context(), stoppedWriter{}); held || err != nil {
		t.Fatalf("displaced writer: %v %v", held, err)
	}
}
func TestAWriterWaitTimeoutKeepsTheWholeMessage(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEAF_TELEMETRY", "off")
	src := &continuationListing{row: leftOffRow(heldCell, "tower", chatlist.Running, time.Minute)}
	a, agent := heldLab(t, src, &fakeTaker{})
	a.closeHome()
	a.input.setText(" keep " + pasteToken(1, 2) + " more")
	a.pastes = []pasteChip{{n: 1, text: "whole\npaste"}}
	a.chips = []chip{{path: "/tmp/kept", file: true}}
	want := a.mainComposer()
	msg := ownershipReadMsg{file: a.file, draft: a.input.String(), chips: a.chips, pastes: a.pastes, writerWaitFailed: true, host: "sync.example", notHeld: true, proof: true, reading: homeMachinesMsg{err: context.DeadlineExceeded}}
	// The ownership result carries the unchanged submission through the same landing
	// as an expired writer-start wait; it must never spend the send callback.
	msg.resume = func() tea.Cmd { t.Fatal("expired writer sent"); return nil }
	drain(t, a, a.afterOwnershipRead(msg))
	if len(agent.sent) != 0 || !reflect.DeepEqual(a.mainComposer(), want) {
		t.Fatal("timeout changed the message")
	}
	if got := plain(lastNote(t, a)); got != "sync.example has not answered; your message is still in the box, press enter to send it" {
		t.Fatal(got)
	}
}
func TestAContinueExplainsThatTheMessageWasKept(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEAF_TELEMETRY", "off")
	src := &continuationListing{row: leftOffRow(heldCell, "tower", chatlist.Running, time.Minute)}
	taker := &fakeTaker{}
	a, old := heldLab(t, src, taker)
	a.closeHome()
	taker.taken = Taken{Fence: 4, Transcript: a.file}
	a.input.setText("message kept for the second enter")
	drive(t, a, key("enter"))
	taker.on = func(string) {
		src.change(func(row *chatlist.Row) { row.Mine, row.Status, row.Fence = true, chatlist.Here, 4 })
	}
	confirm(t, a)
	notes := strings.Join(noteTexts(a), "\n")
	if strings.Count(notes, "your message is still in the box; press enter to send it") != 1 || a.input.String() != "message kept for the second enter" || len(old.sent) != 0 {
		t.Fatalf("missing draft explanation: %s", notes)
	}
}

type otherWindowListing struct{ localWriterListing }

func (otherWindowListing) SendLease(string) (string, uint64, error) { return heldCell, 5, nil }
func (s otherWindowListing) RowsForChatSend(context.Context, string, bool, bool) ([]chatlist.Row, uint64, bool, error) {
	return s.Static, 0, false, nil
}
func TestAnotherWindowsHoldCannotFinishAWaitingSend(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEAF_TELEMETRY", "off")
	src := &continuationListing{row: leftOffRow(heldCell, "tower", chatlist.Running, time.Minute)}
	taker := &fakeTaker{}
	a, _ := heldLab(t, src, taker)
	taker.taken = Taken{Fence: 4, Transcript: a.file}
	taker.on = func(string) { src.change(func(r *chatlist.Row) { r.Mine, r.Status, r.Fence = true, chatlist.Here, 4 }) }
	standOn(t, a, heldCell)
	confirm(t, a)
	agent := &fakeAgent{model: "m"}
	a.agent = stoppedWriter{agent}
	other := src.row
	other.Fence = 5
	a.machines = otherWindowListing{localWriterListing{Static: chatlist.Static{other}}}
	a.input.setText("keep this for my window")
	drive(t, a, key("enter"))
	if len(agent.sent) != 0 || string(a.mainComposer().box.value) != "keep this for my window" {
		t.Fatal("another window's current receipt revived a stopped writer")
	}
	if _, ok := a.homeAsking(); !ok {
		t.Fatal("unheld window has no continue card")
	}
	taker.taken = Taken{Fence: 5, AlreadyHere: true, Transcript: a.file}
	confirm(t, a)
	if a.agent == agent || len(agent.sent) != 0 {
		t.Fatal("stopped writer was reused after confirmation")
	}
	arrived, ok := a.agent.(*switchAgent)
	if !ok || len(arrived.sent) != 0 || a.input.String() != "keep this for my window" {
		t.Fatal("confirmation did not reopen with the message kept")
	}
}

func TestAWriterWaitDoesNotSendAnEditedPaste(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEAF_TELEMETRY", "off")
	src := &continuationListing{row: leftOffRow(heldCell, "tower", chatlist.Running, time.Minute)}
	taker := &fakeTaker{}
	a, _ := heldLab(t, src, taker)
	taker.taken = Taken{Fence: 4, Transcript: a.file}
	taker.on = func(string) { src.change(func(r *chatlist.Row) { r.Mine, r.Status, r.Fence = true, chatlist.Here, 4 }) }
	standOn(t, a, heldCell)
	confirm(t, a)
	writer := &startingSendAgent{fakeAgent: &fakeAgent{model: "m"}, ready: make(chan struct{}), observed: make(chan struct{}, 1)}
	a.agent = writer
	a.machines = localWriterListing{Static: chatlist.Static{src.row}}
	a.input.setText("keep " + pasteToken(1, 2) + " these words")
	a.pastes = []pasteChip{{n: 1, text: "original\npaste"}}
	result := make(chan []tea.Msg, 1)
	cmd := stepKeyNoDrain(t, a, "enter")
	go func() { result <- collectBatchMsgs(cmd) }()
	select {
	case <-writer.observed:
	case <-result:
		t.Fatal("writer wait returned early")
	}
	a.pastes[0].text = "edited\npaste"
	writer.held.Store(true)
	close(writer.ready)
	for _, msg := range <-result {
		drive(t, a, msg)
	}
	if len(writer.sent) != 0 || a.pastes[0].text != "edited\npaste" {
		t.Fatal("an edited paste was sent or replaced")
	}
	if got := plain(lastNote(t, a)); got != "draft changed before sending; press enter to send your current words" {
		t.Fatal(got)
	}
}
