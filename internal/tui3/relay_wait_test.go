package tui3

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/directory"
)

type slowRelayListing struct {
	entered chan context.Context
	answer  chan error
	rows    []chatlist.Row
}

func (s *slowRelayListing) RelayHost() string { return "relay.example" }
func (s *slowRelayListing) Rows(ctx context.Context) ([]chatlist.Row, error) {
	s.entered <- ctx
	select {
	case err := <-s.answer:
		return s.rows, err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
func TestASlowRelayKeepsCheckingAndNamesTheRelay(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEAF_TELEMETRY", "off")
	a := newTestApp(&fakeAgent{model: "m"})
	drain(t, a, a.openHome())
	rows := []chatlist.Row{leftOffRow("kept", "tower", chatlist.Off, time.Minute)}
	src := &slowRelayListing{entered: make(chan context.Context, 1), answer: make(chan error, 1), rows: rows}
	a.machines = src
	a.machineRead.rows = rows
	batch, ok := a.askChats()().(tea.BatchMsg)
	if !ok || len(batch) != 2 {
		t.Fatal("listing has no progress threshold")
	}
	result := make(chan tea.Msg, 1)
	go func() { result <- batch[0]() }()
	ctx := <-src.entered
	deadline, ok := ctx.Deadline()
	if !ok || time.Until(deadline) < 24*time.Second || time.Until(deadline) > 25*time.Second {
		t.Fatal("listing bound is not 25s")
	}
	drive(t, a, waitOut(batch[1]))
	if !strings.Contains(homeText(a), "waiting for relay.example; still checking") || a.machineRead.down {
		t.Fatal(homeText(a))
	}
	select {
	case <-ctx.Done():
		t.Fatal("progress abandoned the listing")
	default:
	}
	if !reflect.DeepEqual(a.machineRead.rows, rows) {
		t.Fatal("slow read dropped rows")
	}
	src.answer <- nil
	drive(t, a, <-result)
	if strings.Contains(homeText(a), "still checking") {
		t.Fatal("progress survived success")
	}
	for _, err := range []error{directory.ErrUnreachable, context.DeadlineExceeded} {
		a.machinesAsking = false
		commands := a.askChats()().(tea.BatchMsg)
		go func() { result <- commands[0]() }()
		<-src.entered
		src.answer <- err
		drive(t, a, <-result)
		want := "relay.example is unreachable; still trying"
		if errors.Is(err, context.DeadlineExceeded) {
			want = "relay.example did not answer in 25s; still trying"
		}
		if !strings.Contains(homeText(a), want) || !a.machineRead.down || !reflect.DeepEqual(a.machineRead.rows, rows) {
			t.Fatal(homeText(a))
		}
	}
}

type startingSendAgent struct {
	*fakeAgent
	ready    chan struct{}
	observed chan struct{}
	held     atomic.Bool
}

func (s *startingSendAgent) RenewalStatus() (time.Time, time.Duration, bool) {
	return time.Time{}, 0, s.held.Load()
}
func (s *startingSendAgent) WriterChanges() <-chan struct{} {
	select {
	case s.observed <- struct{}{}:
	default:
	}
	return s.ready
}

type localWriterListing struct {
	chatlist.Static
	failure error
}

func (s localWriterListing) RelayHost() string                        { return "relay.example" }
func (s localWriterListing) SendLease(string) (string, uint64, error) { return heldCell, 4, nil }
func (s localWriterListing) RowsForChatSend(_ context.Context, _ string, _ bool, holding bool) ([]chatlist.Row, uint64, bool, error) {
	if !holding {
		return nil, 0, false, directory.ErrUnreachable
	}
	return s.Static, 0, true, s.failure
}
func TestTheFirstMessageAfterAMoveWaitsForItsWriter(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEAF_TELEMETRY", "off")
	src := &continuationListing{row: leftOffRow(heldCell, "tower", chatlist.Running, time.Minute)}
	taker := &fakeTaker{}
	a, _ := heldLab(t, src, taker)
	taker.taken = Taken{Fence: 4, Transcript: a.file}
	taker.on = func(string) {
		src.change(func(row *chatlist.Row) { row.Mine, row.Status, row.Fence = true, chatlist.Here, 4 })
	}
	standOn(t, a, heldCell)
	confirm(t, a)
	writer := &startingSendAgent{fakeAgent: &fakeAgent{model: "m"}, ready: make(chan struct{}), observed: make(chan struct{}, 1)}
	a.agent = writer
	a.machines = localWriterListing{Static: chatlist.Static{src.row}}
	a.input.setText(" keep this exact message ")
	cmd := stepKeyNoDrain(t, a, "enter")
	result := make(chan []tea.Msg, 1)
	go func() { result <- collectBatchMsgs(cmd) }()
	select {
	case <-writer.observed:
	case <-result:
		t.Fatal("send returned before waiting for its writer")
	}
	if len(writer.sent) != 0 || a.input.String() != " keep this exact message " {
		t.Fatal("send was spent before writer startup")
	}
	writer.held.Store(true)
	close(writer.ready)
	for _, msg := range <-result {
		drive(t, a, msg)
	}
	if len(writer.sent) != 1 || writer.sent[0] != "keep this exact message" {
		t.Fatalf("sent %v", writer.sent)
	}
	if strings.Contains(strings.Join(noteTexts(a), "\n"), chatlist.ReadTimedOut) {
		t.Fatal("Home listing sentence appeared in chat")
	}
}
func TestAnUnsentMessageSaysItWasKept(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEAF_TELEMETRY", "off")
	src := &continuationListing{row: leftOffRow(heldCell, "tower", chatlist.Running, time.Minute)}
	a, agent := heldLab(t, src, &fakeTaker{})
	a.closeHome()
	a.machines = hostedFailedListing{}
	a.input.setText(" keep " + pasteToken(1, 2) + " these words")
	a.pastes = []pasteChip{{n: 1, text: "whole\npaste"}}
	a.chips = []chip{{path: "/tmp/kept", file: true}}
	want := a.mainComposer()
	drive(t, a, key("enter"))
	if !reflect.DeepEqual(a.mainComposer(), want) || len(agent.sent) != 0 {
		t.Fatal("unsent composer changed")
	}
	if got := plain(lastNote(t, a)); got != "relay.example has not answered; your message is still in the box, press enter to send it" {
		t.Fatal(got)
	}
}

type hostedFailedListing struct{}

func (hostedFailedListing) RelayHost() string { return "relay.example" }
func (hostedFailedListing) Rows(context.Context) ([]chatlist.Row, error) {
	return nil, context.DeadlineExceeded
}
