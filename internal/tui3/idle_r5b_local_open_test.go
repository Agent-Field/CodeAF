package tui3

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/chatlist"
)

type r5bHomeSource struct {
	display chatlist.Static
	reads   int
	read    func(context.Context) ([]chatlist.Row, uint64, error)
}

func (s *r5bHomeSource) Rows(ctx context.Context) ([]chatlist.Row, error) {
	return s.display.Rows(ctx)
}

func (s *r5bHomeSource) RowsForSend(ctx context.Context) ([]chatlist.Row, uint64, error) {
	s.reads++
	return s.read(ctx)
}

func r5bLocalHome(t *testing.T, source *r5bHomeSource, alreadyOpen bool) (*app, string, *walkOpenTaker) {
	t.Helper()
	t.Setenv("CODEAF_IDLE_HANDOFF", "on")
	lab := newHomeLab(t)
	file := lab.session("project", heldCell, "Mine", lab.workspace("project"), time.Now())
	standing := ""
	if alreadyOpen {
		standing = file
	}
	a := lab.app(standing)
	keepSurfaceWorkersInTest(t, a)
	a.width, a.height = 200, 70
	a.teamsPaired = func() bool { return true }
	journal, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	fresh := &continuationListing{row: chatlist.Row{Cell: heldCell, Status: chatlist.Idle, Released: true, AutoOpen: true, DeviceID: "B", Fence: 3, Head: "newest"}}
	taker := &walkOpenTaker{source: fresh, file: file, journal: string(journal)}
	a.machines, a.taker = source, taker
	drain(t, a, a.openHome())
	return a, file, taker
}

func r5bSelectChat(t *testing.T, a *app, file string) homeLine {
	t.Helper()
	for i, line := range a.home.lines {
		if line.kind == homeSession && line.row.Transcript == file || line.remote != nil && line.remote.Cell == heldCell {
			a.home.cursor = i
			return line
		}
	}
	t.Fatal("missing chat on Home")
	return homeLine{}
}

func TestIdleOpen_R5b_LocalOpenWithUnreachableSyncOpensLocal(t *testing.T) {
	for _, mode := range []string{"error", "timeout", "stale own hold", "unrelated display"} {
		t.Run(mode, func(t *testing.T) {
			source := &r5bHomeSource{}
			if mode == "stale own hold" {
				source.display = chatlist.Static{{Cell: heldCell, Status: chatlist.Here, Mine: true, DeviceID: "A", Fence: 1}}
			} else if mode == "unrelated display" {
				source.display = chatlist.Static{{Cell: "another-chat", Status: chatlist.Here, Mine: true}}
			}
			source.read = func(ctx context.Context) ([]chatlist.Row, uint64, error) {
				if mode != "timeout" {
					return nil, 0, errors.New("fixture sync service unreachable")
				}
				deadline, ok := ctx.Deadline()
				if !ok || time.Until(deadline) > 2500*time.Millisecond {
					t.Error("local open ownership read must be bounded to about two seconds")
					return nil, 0, context.DeadlineExceeded
				}
				<-ctx.Done()
				return nil, 0, ctx.Err()
			}
			a, file, taker := r5bLocalHome(t, source, false)
			opens := 0
			open := a.open
			a.open = func(workspace, transcript string) (Conversation, error) {
				opens++
				return open(workspace, transcript)
			}
			r5bSelectChat(t, a, file)
			drive(t, a, key("enter"))
			if source.reads != 1 || opens != 1 || a.at(pageHome) || a.file != file || taker.opens != 0 || len(taker.cells) != 0 {
				t.Fatalf("local open failed: reads=%d local opens=%d file=%q home=%v automatic=%d forced=%v", source.reads, opens, a.file, a.at(pageHome), taker.opens, taker.cells)
			}
			if strings.Contains(a.home.msg, "unreachable") || strings.Contains(a.home.msg, "answer") || strings.Contains(frame(a), chatlist.Unreachable) || strings.Contains(frame(a), chatlist.ReadTimedOut) {
				t.Fatalf("local open showed an unavailable refusal: %q", a.home.msg)
			}
		})
	}
}

func TestIdleOpen_R5b_LocalOpenUsesDisplayWhenItAnswers(t *testing.T) {
	for _, mode := range []string{"this window", "this window without display", "this window with unrelated display", "free", "stale viewer free", "held elsewhere"} {
		t.Run(mode, func(t *testing.T) {
			row := chatlist.Row{Cell: heldCell, Status: chatlist.Here, Mine: true, DeviceID: "A", Fence: 1, Head: "old"}
			if mode == "free" || mode == "stale viewer free" {
				row = chatlist.Row{Cell: heldCell, Status: chatlist.Idle, Released: true, AutoOpen: true, DeviceID: "B", Fence: 3, Head: "newest"}
			} else if mode == "held elsewhere" {
				row = chatlist.Row{Cell: heldCell, Status: chatlist.Running, Device: "laptop", DeviceID: "B", Fence: 3, Head: "newest"}
			}
			source := &r5bHomeSource{display: chatlist.Static{row}, read: func(context.Context) ([]chatlist.Row, uint64, error) {
				return nil, 0, errors.New("display already answers; ownership read forbidden")
			}}
			if mode == "this window without display" {
				source.display = nil
			} else if mode == "this window with unrelated display" {
				source.display = chatlist.Static{{Cell: "another-chat", Status: chatlist.Here, Mine: true}}
			}
			a, file, taker := r5bLocalHome(t, source, strings.HasPrefix(mode, "this window") || mode == "stale viewer free")
			if mode == "stale viewer free" {
				a.blockDisplacedSend(chatlist.Superseded("laptop"))
			}
			r5bSelectChat(t, a, file)
			drive(t, a, key("enter"))
			if source.reads != 0 {
				t.Errorf("display answer made %d ownership reads; want zero", source.reads)
			}
			if len(taker.cells) != 0 {
				t.Fatalf("forced takes=%v", taker.cells)
			}
			switch mode {
			case "free", "stale viewer free":
				if taker.opens != 1 || a.at(pageHome) || a.file != file {
					t.Fatalf("free row did not open directly: opens=%d file=%q", taker.opens, a.file)
				}
			case "held elsewhere":
				if taker.opens != 0 || !a.at(pageHome) || !strings.Contains(homeText(a), continueAsk) {
					t.Fatal("live hold did not keep the existing choice")
				}
			default:
				if taker.opens != 0 || a.at(pageHome) || a.file != file {
					t.Fatal("this window's open chat did not reopen immediately")
				}
			}
		})
	}
}

func TestIdleOpen_R5b_UnpairedNeverReads(t *testing.T) {
	for _, mode := range []string{"unpaired", "sync off", "single device"} {
		t.Run(mode, func(t *testing.T) {
			source := &r5bHomeSource{read: func(context.Context) ([]chatlist.Row, uint64, error) {
				return nil, 0, errors.New("ownership read forbidden without paired computers")
			}}
			a, file, taker := r5bLocalHome(t, source, false)
			if mode == "single device" {
				a.addMachine.known, a.addMachine.size = true, 1
			} else {
				a.teamsPaired = func() bool { return false }
			}
			r5bSelectChat(t, a, file)
			drive(t, a, key("enter"))
			if source.reads != 0 || a.at(pageHome) || a.file != file || taker.opens != 0 || len(taker.cells) != 0 {
				t.Fatalf("local-only opening changed: reads=%d home=%v file=%q automatic=%d forced=%v", source.reads, a.at(pageHome), a.file, taker.opens, taker.cells)
			}
		})
	}
}
