package tui3

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/executor"
	"github.com/Agent-Field/codeaf/internal/session"
)

type walkSendSource struct{ *continuationListing }

func (s walkSendSource) SendLease(string) (string, uint64, error) {
	rows, _ := s.Rows(context.Background())
	if len(rows) == 1 && rows[0].Status == chatlist.Here {
		return heldCell, rows[0].Fence, nil
	}
	return "", 0, os.ErrNotExist
}

type walkSendAgent struct {
	*fakeAgent
	source walkSendSource
	checks int
	moved  bool
}

func (a *walkSendAgent) RenewalStatus() (time.Time, time.Duration, bool) {
	rows, _ := a.source.Rows(context.Background())
	return time.Time{}, time.Second, rows[0].Status == chatlist.Here
}
func (a *walkSendAgent) CheckWork() error {
	a.checks++
	if a.moved {
		return &executor.WorkRefusal{Line: chatlist.Superseded("laptop"), HolderID: "B", HolderName: "laptop"}
	}
	a.source.change(func(r *chatlist.Row) { r.Status, r.Fence = chatlist.Here, r.Fence+1 })
	return nil
}

func TestIdleRelease_C3_D1_TerminalModelSendKeepsTheOpenWorkspace(t *testing.T) {
	for _, moved := range []bool{false, true} {
		t.Run(map[bool]string{false: "own release", true: "another computer wins"}[moved], func(t *testing.T) {
			source := walkSendSource{&continuationListing{row: chatlist.Row{Cell: heldCell, Status: chatlist.Idle, Mine: true, Released: true, AutoOpen: true, Fence: 2, Head: "saved", DeviceID: "A"}}}
			taker := &fakeTaker{}
			a, old := heldLab(t, source, taker)
			keepSurfaceWorkersInTest(t, a)
			a.closeHome()
			root := session.DirOf(a.file)
			if err := os.MkdirAll(filepath.Join(root, ".cell"), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, ".cell", "adopted.json"), []byte(`{"V":1,"head":"saved","turns_size":0}`), 0600); err != nil {
				t.Fatal(err)
			}
			ignored := filepath.Join(a.workspace, "node_modules", "x", "index.js")
			if err := os.MkdirAll(filepath.Dir(ignored), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(ignored, []byte("keep this local install"), 0600); err != nil {
				t.Fatal(err)
			}
			taker.taken = Taken{Transcript: a.file, Fence: 3, Head: "saved"}
			taker.on = func(string) {
				_ = os.RemoveAll(filepath.Join(a.workspace, "node_modules"))
				source.change(func(r *chatlist.Row) { r.Status, r.Fence = chatlist.Here, 3 })
			}
			agent := &walkSendAgent{fakeAgent: old, source: source, moved: moved}
			a.agent = agent
			a.input.setText("ordinary model message")
			drive(t, a, key("enter"))
			if len(taker.cells) != 0 {
				t.Errorf("D1 same-window send restored through takeover: %v", taker.cells)
			}
			if raw, err := os.ReadFile(ignored); err != nil || string(raw) != "keep this local install" {
				t.Errorf("D1 ignored local file changed: %q, %v", raw, err)
			}
			if agent.checks != 1 {
				t.Errorf("D1 ownership admissions=%d", agent.checks)
			}
			if moved {
				if len(old.sent) != 0 || string(a.mainComposer().box.value) != "ordinary model message" {
					t.Fatal("D1 losing send spent the draft")
				}
			} else if len(old.sent) != 1 || a.agent != agent {
				t.Fatalf("D1 did not send through the same session: sent=%v agent=%T", old.sent, a.agent)
			}
			var notes strings.Builder
			for _, entry := range a.entries {
				if entry.kind == entryNote {
					notes.WriteString(plain(entry.text))
				}
			}
			head, card := a.questionHead()
			if card && head.question.Kind == resumeKind || strings.Contains(notes.String(), "Moved here") {
				t.Fatal("D1 raised a move/setup notice")
			}
		})
	}
}

type walkOpenTaker struct {
	fakeTaker
	opens   int
	source  *continuationListing
	file    string
	journal string
}

func (t *walkOpenTaker) OpenAvailable(_ context.Context, _ string, seen *directory.LeaseExpectation) (Taken, error) {
	t.opens++
	if seen == nil || seen.Device != "B" || seen.Fence != 3 {
		return Taken{}, directory.ErrLeaseHeld
	}
	if err := os.WriteFile(t.file, []byte(t.journal), 0600); err != nil {
		return Taken{}, err
	}
	t.source.change(func(r *chatlist.Row) { r.Status, r.Mine, r.Fence = chatlist.Here, true, r.Fence+1 })
	return Taken{Transcript: t.file, Fence: 4, Head: "newest", Device: "laptop"}, nil
}
func TestIdleOpen_C6_D2_LocalViewerRowPullsTheReleasedHead(t *testing.T) {
	source := &continuationListing{row: chatlist.Row{Cell: heldCell, Status: chatlist.Running, Device: "laptop", DeviceID: "B", Fence: 3, Head: "newest"}}
	taker := &walkOpenTaker{source: source}
	a, old := heldLab(t, source, taker)
	keepSurfaceWorkersInTest(t, a)
	taker.file = a.file
	initial, err := os.ReadFile(a.file)
	if err != nil {
		t.Fatal(err)
	}
	taker.journal = string(initial) + `{"type":"message","role":"assistant","content":"newest journal from B"}` + "\n"
	if err := os.WriteFile(a.file, append(initial, []byte(`{"type":"message","role":"assistant","content":"stale journal on A"}`+"\n")...), 0600); err != nil {
		t.Fatal(err)
	}
	a.blockDisplacedSend(chatlist.Superseded("laptop"))
	source.change(func(r *chatlist.Row) { r.Status, r.Released, r.AutoOpen = chatlist.Idle, true, true })
	drain(t, a, a.askMachines())
	found := false
	for i, line := range a.home.lines {
		if line.kind == homeSession && line.row.Transcript == a.file {
			a.home.cursor = i
			found = true
			break
		}
	}
	if !found {
		t.Fatal("fixture did not expose the existing local row")
	}
	drive(t, a, key("enter"))
	if taker.opens != 1 {
		t.Fatalf("D2 existing viewer row opens=%d; want one non-forcing pull", taker.opens)
	}
	raw, err := os.ReadFile(taker.file)
	if err != nil || string(raw) != taker.journal {
		t.Fatalf("D2 stale journal retained: %q, %v", raw, err)
	}
	rows, _ := source.Rows(t.Context())
	if !rows[0].Mine || rows[0].Status != chatlist.Here || rows[0].Fence != 4 {
		t.Fatalf("D2 receiver did not hold the newest chat: %+v", rows)
	}
	if a.agent == old || a.machineRead.sendHolds[heldCell].blocked {
		t.Fatal("D2 retained the stale viewer")
	}
}

func TestIdleOpen_C7_D2_LocalViewerKeepsLiveHoldChoice(t *testing.T) {
	source := &continuationListing{row: chatlist.Row{Cell: heldCell, Status: chatlist.Idle, Released: true, AutoOpen: true, Device: "laptop", DeviceID: "B", Fence: 3, Head: "newest"}}
	taker := &walkOpenTaker{source: source}
	a, _ := heldLab(t, source, taker)
	keepSurfaceWorkersInTest(t, a)
	a.blockDisplacedSend(chatlist.Superseded("laptop"))
	var selected homeLine
	found := false
	for _, line := range a.home.lines {
		if line.kind == homeSession && line.row.Transcript == a.file {
			selected, found = line, true
			break
		}
	}
	if !found {
		t.Fatal("fixture did not expose the existing local row")
	}
	// The selected local row predates the news that B resumed work. Even
	// this retained handle must honor the new live hold before reusing a tab.
	source.change(func(r *chatlist.Row) { r.Status, r.Released, r.AutoOpen = chatlist.Running, false, false })
	drain(t, a, a.askMachines())
	drain(t, a, a.homeOpenLine(selected))
	if taker.opens != 0 || len(taker.cells) != 0 || !strings.Contains(homeText(a), continueAsk) {
		t.Fatal("D2 opening an existing live viewer bypassed the person's choice")
	}
}
