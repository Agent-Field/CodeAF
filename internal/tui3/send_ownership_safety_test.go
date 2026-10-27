package tui3

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/session"
)

func TestSendRequiresTheWindowsCurrentHold(t *testing.T) {
	for _, state := range []struct {
		name   string
		status chatlist.Status
		mine   bool
		fence  uint64
	}{
		{"free newer copy", chatlist.Idle, true, 2},
		{"expired own copy", chatlist.Off, true, 2},
		{"returned with another hold", chatlist.Here, true, 3},
	} {
		t.Run(state.name, func(t *testing.T) {
			src := &continuationListing{row: chatlist.Row{Cell: heldCell, Status: chatlist.Here, Mine: true, Fence: 2}}
			a, agent := heldLab(t, src, &fakeTaker{})
			keepSurfaceWorkersInTest(t, a)
			a.closeHome()
			src.change(func(r *chatlist.Row) { r.Status, r.Mine, r.Fence = state.status, state.mine, state.fence })
			src.change(func(r *chatlist.Row) { r.Head = "newer" })
			a.input.setText("keep this unsent")
			drive(t, a, key("enter"))
			if len(agent.sent) != 0 {
				t.Errorf("unheld copy ran a turn: %v", agent.sent)
			}
			if _, card := a.homeAsking(); !card && !strings.Contains(plain(lastNote(t, a)), "only shows it") {
				t.Error("unheld copy has neither a continuation card nor a viewing explanation")
			}
			if got := string(a.mainComposer().box.value); got != "keep this unsent" {
				t.Errorf("draft = %q", got)
			}
		})
	}
}

type windowProofSource struct{ *continuationListing }

func (*windowProofSource) SendLease(string) (string, uint64, error) { return heldCell, 1, nil }

func TestFreshReadRejectsAnotherWindowsHold(t *testing.T) {
	src := &windowProofSource{&continuationListing{row: chatlist.Row{Cell: heldCell, Status: chatlist.Here, Mine: true, Fence: 2}}}
	a, agent := heldLab(t, src, &fakeTaker{})
	keepSurfaceWorkersInTest(t, a)
	a.closeHome()
	a.input.setText("still unsent")
	drive(t, a, key("enter"))
	if len(agent.sent) != 0 {
		t.Fatal("a first reading mistook another window's hold for its own")
	}
	if _, card := a.homeAsking(); !card {
		t.Fatal("a mismatched window did not offer continuation")
	}
}

func TestHeldSendUsesOneFreshReading(t *testing.T) {
	src := &continuationListing{row: chatlist.Row{Cell: heldCell, Status: chatlist.Here, Mine: true, Fence: 2}}
	a, agent := heldLab(t, src, &fakeTaker{})
	keepSurfaceWorkersInTest(t, a)
	a.closeHome()
	before := src.reads
	a.input.setText("send this")
	drive(t, a, key("enter"))
	if len(agent.sent) != 1 || src.reads-before != 1 {
		t.Fatalf("sends=%v, ownership reads=%d", agent.sent, src.reads-before)
	}
}

type returningTaker struct {
	fakeTaker
	seen *directory.LeaseExpectation
}

func (t *returningTaker) TakeSeen(ctx context.Context, id string, seen *directory.LeaseExpectation) (Taken, error) {
	t.seen = seen
	return t.Take(ctx, id)
}

func TestLapsedCurrentChatSendsAfterAConditionalReturn(t *testing.T) {
	src := &continuationListing{row: chatlist.Row{Cell: heldCell, Status: chatlist.Off, Mine: true, DeviceID: "dev_here", Fence: 2, Head: "saved"}}
	taker := &returningTaker{}
	a, old := heldLab(t, src, taker)
	keepSurfaceWorkersInTest(t, a)
	a.closeHome()
	root := session.DirOf(a.file)
	if err := os.MkdirAll(filepath.Join(root, ".cell"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".cell", "adopted.json"), []byte(`{"V":1,"head":"saved","turns_size":0}`), 0600); err != nil {
		t.Fatal(err)
	}
	taker.taken = Taken{Transcript: a.file, Fence: 3, Head: "saved"}
	taker.on = func(string) { src.change(func(r *chatlist.Row) { r.Status, r.Fence = chatlist.Here, 3 }) }
	a.input.setText("send after waking")
	drive(t, a, key("enter"))
	if taker.seen == nil || taker.seen.Device != "dev_here" || taker.seen.Fence != 2 {
		t.Errorf("return did not condition on the observed hold: %+v", taker.seen)
	}
	if len(old.sent) != 0 {
		t.Error("the retired session sent")
	}
	arrived, ok := a.agent.(*switchAgent)
	if !ok || len(arrived.sent) != 1 {
		t.Fatalf("conditional return did not send through the reopened chat: %T", a.agent)
	}
}

func TestFailedRestoreCannotSendOnTheReleasedCopy(t *testing.T) {
	src := &continuationListing{row: leftOffRow(heldCell, "other", chatlist.Running, time.Minute)}
	taker := &fakeTaker{err: errors.New("handoff: engine materialize: rewind cancelled before mutation: workspace path changed during rewind: .git")}
	a, agent := heldLab(t, src, taker)
	keepSurfaceWorkersInTest(t, a)
	a.closeHome()
	a.input.setText("keep this unsent")
	drive(t, a, key("enter"))
	taker.on = func(string) { src.change(func(r *chatlist.Row) { r.Status, r.Mine = chatlist.Idle, true }) }
	confirm(t, a)
	said := homeText(a)
	if !strings.Contains(said, "folder changed") || strings.Contains(said, "engine materialize") {
		t.Errorf("failure did not give a plain reason: %s", said)
	}
	a.closeHome()
	a.input.setText("try again")
	drive(t, a, key("enter"))
	if len(agent.sent) != 0 {
		t.Errorf("failed take's released copy ran a turn: %v", agent.sent)
	}
}

func TestHeldAndUnpublishedChatsStillSend(t *testing.T) {
	for _, published := range []bool{false, true} {
		var rows chatlist.Static
		if published {
			rows = chatlist.Static{{Cell: heldCell, Status: chatlist.Here, Mine: true, Fence: 2}}
		}
		a, agent := heldLab(t, rows, &fakeTaker{})
		keepSurfaceWorkersInTest(t, a)
		a.closeHome()
		a.input.setText("send this")
		drive(t, a, key("enter"))
		if len(agent.sent) != 1 {
			t.Errorf("published=%v: sent %v", published, agent.sent)
		}
	}
}
