package tui3

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/directory"
)

type r5cOfflineTaker struct {
	fakeTaker
	opens int
	err   error
	seen  *directory.LeaseExpectation
	on    func()
	taken Taken
}

func (f *r5cOfflineTaker) OpenAvailable(_ context.Context, _ string, seen *directory.LeaseExpectation) (Taken, error) {
	f.opens++
	f.seen = seen
	if f.on != nil {
		f.on()
	}
	return f.taken, f.err
}

func TestIdleOpen_R5c_OwnReleasedChatOpensLocallyWhenSyncDown(t *testing.T) {
	for _, mode := range []string{"down", "reading error", "take unreachable", "take timeout", "local copy"} {
		t.Run(mode, func(t *testing.T) {
			row := chatlist.Row{Cell: heldCell, Status: chatlist.Idle, Mine: mode != "local copy", Released: true, AutoOpen: true, DeviceID: "A", Fence: 7}
			source := &r5bHomeSource{display: chatlist.Static{row}, read: func(context.Context) ([]chatlist.Row, uint64, error) {
				return nil, 0, directory.ErrUnreachable
			}}
			a, file, _ := r5bLocalHome(t, source, false)
			taker := &r5cOfflineTaker{err: directory.ErrUnreachable}
			if mode == "take timeout" {
				taker.err = context.DeadlineExceeded
			}
			a.taker = taker
			if mode == "down" || mode == "local copy" {
				a.machineRead.down = true
			} else if mode == "reading error" {
				a.machineRead.err = directory.ErrUnreachable
			}
			opens := 0
			open := a.open
			a.open = func(workspace, transcript string) (Conversation, error) {
				opens++
				return open(workspace, transcript)
			}
			r5bSelectChat(t, a, file)
			drive(t, a, key("enter"))
			wantTakes := 0
			if strings.HasPrefix(mode, "take ") {
				wantTakes = 1
			}
			if opens != 1 || a.at(pageHome) || a.file != file || taker.opens != wantTakes || len(taker.cells) != 0 || source.reads != 0 || a.homeTaking != "" {
				t.Fatalf("offline local open: local=%d home=%v file=%q free=%d forced=%v reads=%d pending=%q", opens, a.at(pageHome), a.file, taker.opens, taker.cells, source.reads, a.homeTaking)
			}
		})
	}
}

func TestIdleOpen_R5c_RemoteRowOfflineSaysWhy(t *testing.T) {
	for _, mode := range []string{"down", "reading error", "take unreachable", "take timeout"} {
		t.Run(mode, func(t *testing.T) {
			row := chatlist.Row{Cell: "remote-only", Status: chatlist.Idle, AutoOpen: true, DeviceID: "B", Fence: 3}
			source := &r5bHomeSource{display: chatlist.Static{row}}
			a, _, _ := r5bLocalHome(t, source, false)
			taker := &r5cOfflineTaker{err: directory.ErrUnreachable}
			if mode == "take timeout" {
				taker.err = context.DeadlineExceeded
			}
			a.taker = taker
			if mode == "down" {
				a.machineRead.down = true
			} else if mode == "reading error" {
				a.machineRead.err = directory.ErrUnreachable
			}
			standOn(t, a, row.Cell)
			wantTakes := 0
			if strings.HasPrefix(mode, "take ") {
				wantTakes = 1
			}
			if !a.at(pageHome) || !strings.Contains(a.home.msg, chatlist.Unreachable) || a.homeTaking != "" || len(taker.cells) != 0 || taker.opens != wantTakes {
				t.Fatalf("offline remote refusal: home=%v message=%q pending=%q forced=%v", a.at(pageHome), a.home.msg, a.homeTaking, taker.cells)
			}
		})
	}
}

func TestIdleOpen_R5c_OwnReleasedChatOnlineTakesNewFence(t *testing.T) {
	row := chatlist.Row{Cell: heldCell, Status: chatlist.Idle, Mine: true, Released: true, AutoOpen: true, DeviceID: "A", Fence: 7}
	source := &r5bHomeSource{display: chatlist.Static{row}, read: func(context.Context) ([]chatlist.Row, uint64, error) {
		return nil, 0, errors.New("answered display must not add a read")
	}}
	a, file, _ := r5bLocalHome(t, source, false)
	taker := &r5cOfflineTaker{taken: Taken{Transcript: file, Fence: 8}, on: func() {
		source.display[0].Status, source.display[0].Fence = chatlist.Here, 8
		source.display[0].Released, source.display[0].AutoOpen = false, false
	}}
	a.taker = taker
	r5bSelectChat(t, a, file)
	drive(t, a, key("enter"))
	if a.at(pageHome) || a.file != file || taker.opens != 1 || len(taker.cells) != 0 || source.reads != 0 || taker.seen == nil || taker.seen.Device != "A" || taker.seen.Fence != 7 || source.display[0].Fence != 8 {
		t.Fatalf("own released chat bypassed conditional free acquisition: free=%d forced=%v seen=%+v", taker.opens, taker.cells, taker.seen)
	}
}

func TestIdleOpen_R5c_LocalCopyKeepsTakeRaceRefusal(t *testing.T) {
	row := chatlist.Row{Cell: heldCell, Status: chatlist.Idle, Mine: true, AutoOpen: true, DeviceID: "A", Fence: 7}
	source := &r5bHomeSource{display: chatlist.Static{row}, read: func(context.Context) ([]chatlist.Row, uint64, error) {
		return nil, 0, errors.New("unexpected ownership read")
	}}
	a, file, _ := r5bLocalHome(t, source, false)
	a.taker = &r5cOfflineTaker{err: directory.ErrConditionalTake}
	r5bSelectChat(t, a, file)
	drive(t, a, key("enter"))
	if !a.at(pageHome) || !strings.Contains(a.home.msg, directory.ErrConditionalTake.Error()) {
		t.Fatal("a conditional-acquire race incorrectly opened the local copy")
	}
}
