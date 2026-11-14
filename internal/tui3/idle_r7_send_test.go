package tui3

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/session"
)

type r7SendSource struct {
	walkSendSource
	err    error
	reads  int
	within time.Duration
}

func (*r7SendSource) RelayHost() string { return "fixture-relay" }

func (s *r7SendSource) RowsForChatSend(ctx context.Context, _ string, offline, holding bool) ([]chatlist.Row, uint64, bool, error) {
	if holding {
		rows, err := s.Rows(ctx)
		return rows, 0, true, err
	}
	if offline {
		return nil, 0, false, directory.ErrUnreachable
	}
	s.reads++
	deadline, ok := ctx.Deadline()
	if ok {
		s.within = time.Until(deadline)
	}
	if s.err != nil {
		return nil, 0, false, s.err
	}
	rows, err := s.Rows(ctx)
	return append(rows, chatlist.Row{Cell: "another-chat", Title: "Keep this row"}), 7, false, err
}

func r7ReleasedWindow(t *testing.T) (*app, *fakeAgent, *r7SendSource) {
	t.Helper()
	t.Setenv("CODEAF_IDLE_HANDOFF", "on")
	source := &r7SendSource{walkSendSource: walkSendSource{&continuationListing{row: chatlist.Row{Cell: heldCell, Status: chatlist.Idle, Mine: true, Released: true, AutoOpen: true, Fence: 2, Head: "saved", DeviceID: "A"}}}}
	a, old := heldLab(t, source, &fakeTaker{})
	keepSurfaceWorkersInTest(t, a)
	a.closeHome()
	root := session.DirOf(a.file)
	if err := os.MkdirAll(filepath.Join(root, ".cell"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".cell", "adopted.json"), []byte(`{"V":1,"head":"saved","turns_size":0}`), 0600); err != nil {
		t.Fatal(err)
	}
	a.agent = &walkSendAgent{fakeAgent: old, source: source.walkSendSource}
	a.machineRead.down, a.machineRead.err = true, syscall.ECONNREFUSED
	return a, old, source
}

func TestIdleOpen_R7_FirstEnterAfterRecoverySends(t *testing.T) {
	a, old, source := r7ReleasedWindow(t)
	a.input.setText("send on the first Enter")
	drive(t, a, key("enter"))
	rows, _ := source.Rows(t.Context())
	if len(old.sent) != 1 || old.sent[0] != "send on the first Enter" || rows[0].Fence != 3 {
		t.Fatalf("first Enter did not send at a new fence: sent=%v rows=%+v notes=%v", old.sent, rows, noteTexts(a))
	}
	if source.reads != 1 || source.within <= 0 || source.within > 3*time.Second {
		t.Fatalf("fresh directory attempts=%d budget=%v; want one bounded by 3s", source.reads, source.within)
	}
	if a.machineRead.down || a.machineRead.err != nil || a.machineRead.version != 7 {
		t.Fatalf("successful send retained Home's outage: %+v", a.machineRead)
	}
	if len(a.machineRead.rows) != 2 || a.machineRead.rows[1].Cell != "another-chat" {
		t.Fatalf("recovery lost the rest of the directory reading: %+v", a.machineRead.rows)
	}
	if notes := strings.Join(noteTexts(a), "\n"); strings.Contains(notes, "unreachable") {
		t.Fatalf("Enter replayed the cached refusal despite recovery: %s", notes)
	}
}

func TestIdleOpen_R7_StillDownKeepsWordsWithHonestLine(t *testing.T) {
	for _, cause := range []error{syscall.ECONNREFUSED, context.DeadlineExceeded} {
		t.Run(cause.Error(), func(t *testing.T) {
			a, old, source := r7ReleasedWindow(t)
			source.err = &url.Error{Op: "Get", URL: "http://127.0.0.1:1", Err: cause}
			before, err := os.ReadFile(a.file)
			if err != nil {
				t.Fatal(err)
			}
			const words = "keep these words until recovery"
			a.input.setText(words)
			drive(t, a, key("enter"))
			after, err := os.ReadFile(a.file)
			if err != nil || string(after) != string(before) || len(old.sent) != 0 || a.input.String() != words {
				t.Fatalf("failed probe spent words or wrote the chat: sent=%v draft=%q err=%v", old.sent, a.input.String(), err)
			}
			notes := strings.Join(noteTexts(a), "\n")
			if source.reads != 1 || strings.Count(notes, chatlist.TakeBackUnreachable) != 1 || strings.Contains(notes, chatlist.Available) {
				t.Fatalf("failed probe did not explain take-back: reads=%d notes=%s", source.reads, notes)
			}
			source.err = nil
			drive(t, a, key("enter"))
			if source.reads != 2 || len(old.sent) != 1 || old.sent[0] != words || a.machineRead.down {
				t.Fatalf("first retry after recovery did not send: reads=%d sent=%v notes=%v", source.reads, old.sent, noteTexts(a))
			}
		})
	}
}

func TestIdleOpen_R7_LiveHeldOfflineSendKeepsLocalAdmission(t *testing.T) {
	a, old, source := r7ReleasedWindow(t)
	source.change(func(row *chatlist.Row) {
		row.Status, row.Released, row.AutoOpen = chatlist.Here, false, false
	})
	source.err = directory.ErrUnreachable
	a.input.setText("held work stays local")
	drive(t, a, key("enter"))
	rows, _ := source.Rows(t.Context())
	if source.reads != 0 || len(old.sent) != 1 || rows[0].Fence != 2 || !a.machineRead.down {
		t.Fatalf("live-held offline admission changed: reads=%d sent=%v rows=%+v down=%v", source.reads, old.sent, rows, a.machineRead.down)
	}
}

func TestIdleOpen_R7_LapsedWindowProbesBeforeConditionalReturn(t *testing.T) {
	a, old, source := r7ReleasedWindow(t)
	source.change(func(row *chatlist.Row) {
		row.Status, row.Released, row.AutoOpen = chatlist.Off, false, false
	})
	taker := &returningTaker{}
	taker.taken = Taken{Transcript: a.file, Fence: 3, Head: "saved"}
	taker.on = func(string) {
		source.change(func(row *chatlist.Row) { row.Status, row.Fence = chatlist.Here, 3 })
	}
	a.taker = taker
	a.input.setText("first Enter after lapse")
	drive(t, a, key("enter"))
	if source.reads != 1 || source.within <= 0 || source.within > 3*time.Second {
		t.Fatalf("lapsed window did not make one bounded probe: reads=%d budget=%v", source.reads, source.within)
	}
	if taker.seen == nil || taker.seen.Device != "A" || taker.seen.Fence != 2 {
		t.Fatalf("lapsed return lost its observed holder and fence: %+v", taker.seen)
	}
	arrived, ok := a.agent.(*switchAgent)
	if len(old.sent) != 0 || !ok || len(arrived.sent) != 1 || arrived.sent[0] != "first Enter after lapse" || a.machineRead.down {
		t.Fatalf("first Enter did not send through the replacement: old=%v new=%T notes=%v", old.sent, a.agent, noteTexts(a))
	}
}
