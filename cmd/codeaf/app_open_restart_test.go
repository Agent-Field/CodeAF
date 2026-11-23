package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cell/celltest"
	cellstoretest "github.com/Agent-Field/codeaf/internal/cellstore/celltest"
	"github.com/Agent-Field/codeaf/internal/devname"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

// #2781: OPENING A CHAT THIS COMPUTER ALREADY HOLDS IS THE RESTART OF ITS FILES.
// A chat moved here opens before its files arrive. When the fetch failed and the
// window that was fetching them closed, every workspace tool and every save
// refuses until the files are fetched again, and for a chat this computer holds
// there is nothing to press: Home offers plain open and the app lists it as not
// takeable. These tests drive the app door a person actually uses (agents.open),
// with two real app processes, a real relay and the real engine.

// restartSeed is what the sending computer's project holds, so a damaged copy
// has real bytes to be compared with.
var restartSeed = map[string][]byte{
	"README.md":     []byte("# project\n"),
	"src/main.go":   []byte("package main\n\nfunc main() {}\n"),
	"bin/run.sh":    []byte("#!/bin/sh\necho hi\n"),
	"data/blob.bin": bytes.Repeat([]byte{0, 1, 2, 250, 251, 252, 7}, 3000),
}

// restartWait waits for what a fetch does on a laptop that is busy with other
// work: the shared 30 seconds of appSyncWait is the ceiling of a quiet one.
func restartWait(t *testing.T, what string, check func() bool) {
	t.Helper()
	until := time.Now().Add(4 * time.Minute)
	for !check() {
		if time.Now().After(until) {
			t.Fatal("timed out waiting for " + what)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// restartFixture is a chat that moved from computer A to computer B, whose
// files arrived, and whose two computers are both closed. B is the computer that
// holds it (its lease is B's, released at the quit), so its next open is the
// open of a held chat.
type restartFixture struct {
	t         *testing.T
	dirA      string
	dirB      string
	chat      string // the chat's cell id
	agent     string // B's id for it
	root      string // B's folder for the chat
	workspace string // B's copy of the project
	fence     uint64 // the fence B held when it quit
}

func newRestartFixture(t *testing.T) *restartFixture {
	t.Helper()
	celltest.RequireEngine(t)
	_, workspace := wakeEnvironment(t, e2TempDir(t))
	for path, body := range restartSeed {
		full := filepath.Join(workspace, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, body, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	model := appTwoMessageSource(t, "echo codeaf-e2")
	t.Cleanup(model.Close)
	t.Setenv("CODEAF_BASE_URL", model.URL)
	relay := e2Relay(t)
	t.Setenv(syncsetup.URLVar, relay.URL)
	t.Setenv(syncsetup.IntervalVar, "50")
	dirA, dirB := home.Dir(), e2TempDir(t)
	cellstoretest.EngineDir(t)
	id, err := identity.Ensure(dirA)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := identity.Adopt(dirB, id, false); err != nil {
		t.Fatal(err)
	}
	for dir, name := range map[string]string{dirA: "A", dirB: "B"} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"tools.approvalMode":"allow"}`), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := devname.Set(dir, name); err != nil {
			t.Fatal(err)
		}
	}
	for _, dir := range []string{dirA, dirB} {
		s := appDeviceSync(t, dir)
		if err := syncsetup.PutOwnDevice(context.Background(), s.Dir, s.Identity, s.Device, devname.Name(dir)); err != nil {
			t.Fatal(err)
		}
	}
	a, b := e2StartApp(t, dirA), e2StartApp(t, dirB)
	created := machineCall[struct {
		Agent appwire.Agent `json:"agent"`
	}](t, a.w, "agents.create", map[string]any{"workspace": workspace}).Agent
	e2Turn(t, a, created.ID, "first")
	e2Quit(t, a)
	// The chat was saved by A leaving, and B sees it as a chat it may take.
	var row appwire.ChatRow
	appSyncWait(t, "the chat A left is listed on B", func() bool {
		for _, r := range machineCall[appwire.ChatsResult](t, b.w, "chats.list", struct{}{}).Chats {
			if r.Title != "" && r.CanTake {
				row = r
				return true
			}
		}
		return false
	})
	taken := machineCall[appwire.TakeResult](t, b.w, "agents.take", appwire.TakeParams{Cell: row.Cell, Seen: row.Seen}).Agent
	if taken.Location == nil || !taken.Location.Here {
		t.Fatalf("B did not take the chat: %+v", taken)
	}
	f := &restartFixture{t: t, dirA: dirA, dirB: dirB, chat: row.Cell, agent: taken.ID, workspace: taken.Workspace}
	f.root = takeRootForAt(dirB, f.chat)
	// The files arrive behind the open; wait for them before breaking anything.
	restartWait(t, "the moved files to arrive on B", func() bool {
		return !cell.WorkspaceArriving(f.root) && f.filesMatch() == ""
	})
	e2Quit(t, b)
	// B's quit released the hold, but the lease is still B's, which is what
	// makes the chat held here.
	view, err := appDeviceSync(t, dirB).Dir.Cell(context.Background(), f.chat)
	if err != nil {
		t.Fatal(err)
	}
	if view.Cell.Lease.Device != appDeviceSync(t, dirB).Device.ID() {
		t.Fatalf("after B quit the lease names %q, not B", view.Cell.Lease.Device)
	}
	f.fence = view.Cell.Lease.Fence
	return f
}

// filesMatch says what differs between B's copy and what A sent, or "".
func (f *restartFixture) filesMatch() string {
	for path, want := range restartSeed {
		got, err := os.ReadFile(filepath.Join(f.workspace, filepath.FromSlash(path)))
		if err != nil {
			return path + " is missing"
		}
		if !bytes.Equal(got, want) {
			return path + " differs"
		}
	}
	return ""
}

// breakFiles leaves B's copy as a fetch that failed would: a folder gone, a file
// gone and a file cut short.
func (f *restartFixture) breakFiles() {
	f.t.Helper()
	if err := os.RemoveAll(filepath.Join(f.workspace, "src")); err != nil {
		f.t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(f.workspace, "data", "blob.bin")); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.workspace, "README.md"), []byte("# pro"), 0o644); err != nil {
		f.t.Fatal(err)
	}
	if f.filesMatch() == "" {
		f.t.Fatal("the files were not broken")
	}
}

// repairFiles puts back what breakFiles took, as the arrival that was still in
// flight would have, so the app that leaves afterwards seals a whole tree.
func (f *restartFixture) repairFiles() {
	f.t.Helper()
	for path, body := range restartSeed {
		full := filepath.Join(f.workspace, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
			f.t.Fatal(err)
		}
		if err := os.WriteFile(full, body, 0o644); err != nil {
			f.t.Fatal(err)
		}
	}
}

// abandon is the failure the issue describes: the fetch failed and the window
// that was bringing the files is gone, so the arrival marker names no owner.
func (f *restartFixture) abandon() {
	f.t.Helper()
	cell.BeginWorkspaceArrival(f.root, f.workspace, nil, nil).Abandon("sync could not be reached")
	if !cell.WorkspaceArriving(f.root) {
		f.t.Fatal("the abandoned arrival does not hold the gate shut")
	}
}

// marker is where the arrival of this chat's files is written down.
func (f *restartFixture) marker() string { return cell.ArrivalMarkerPath(f.root) }

func (f *restartFixture) lease() (device string, fence uint64) {
	f.t.Helper()
	view, err := appDeviceSync(f.t, f.dirB).Dir.Cell(context.Background(), f.chat)
	if err != nil {
		f.t.Fatal(err)
	}
	return view.Cell.Lease.Device, view.Cell.Lease.Fence
}

// OPENING A HELD CHAT WHOSE FILES FAILED TO ARRIVE FETCHES THEM AGAIN. The
// person pressed open and nothing else: the files come back byte for byte, the
// gate opens, and the next tool call runs instead of being refused with a
// sentence that names an action this chat has no button for.
func TestAppOpenRestartsTheFilesOfAHeldChatWhoseArrivalWasAbandoned(t *testing.T) {
	f := newRestartFixture(t)
	f.breakFiles()
	f.abandon()
	heldBy, _ := f.lease()

	b := e2StartApp(t, f.dirB)
	opened := machineCall[appwire.TakeResult](t, b.w, "agents.open", appwire.IDParams{ID: f.agent}).Agent
	if opened.ID != f.agent {
		t.Fatalf("agents.open answered %+v, want the chat %s", opened, f.agent)
	}
	restartWait(t, "the abandoned files to be fetched again", func() bool {
		return !cell.WorkspaceArriving(f.root) && f.filesMatch() == ""
	})
	// A workspace tool call (the scripted model runs a shell command) used to be
	// refused with "the window that was fetching them has closed, so continue the
	// chat here again to fetch them".
	e2Turn(t, b, f.agent, "after the open")
	if got := f.filesMatch(); got != "" {
		t.Fatalf("after the turn: %s", got)
	}
	// Nothing was claimed or given up for it: the chat is still this computer's.
	if device, _ := f.lease(); device != heldBy {
		t.Fatalf("the lease moved from %q to %q", heldBy, device)
	}
	e2Quit(t, b)
}

// NOTHING IS RESTARTED UNDER A LIVE OWNER. While a running arrival's owner is
// still there the files are on their way, and a second fetch over the same
// folder would race the first.
func TestAppOpenStartsNoSecondArrivalWhileALiveOwnersIsInFlight(t *testing.T) {
	f := newRestartFixture(t)
	f.breakFiles()
	// This test process is the live owner of a running arrival over B's chat.
	owner := cell.BeginWorkspaceArrival(f.root, f.workspace, nil, nil)
	t.Cleanup(owner.Complete)
	before, err := os.ReadFile(f.marker())
	if err != nil || len(before) == 0 {
		t.Fatalf("the live owner left no marker: %v", err)
	}

	b := e2StartApp(t, f.dirB)
	opened := machineCall[appwire.TakeResult](t, b.w, "agents.open", appwire.IDParams{ID: f.agent}).Agent
	if opened.ID != f.agent {
		t.Fatalf("agents.open answered %+v, want the chat %s", opened, f.agent)
	}
	// A restart begins by replacing the marker with its own, and it does that
	// before the open answers, so a marker that is byte for byte the owner's is
	// the proof that no second arrival was started over it.
	if got, _ := os.ReadFile(f.marker()); !bytes.Equal(got, before) {
		t.Fatalf("the open replaced the live owner's marker: %q, was %q", got, before)
	}
	time.Sleep(2 * time.Second)
	if got := f.filesMatch(); got == "" {
		t.Fatal("the open fetched the files while another process was still bringing them")
	}
	if cell.LookupWorkspaceArrival(f.root) != owner || owner.Ready() {
		t.Fatal("the live owner's arrival was replaced")
	}
	// The app that leaves saves the chat, and a save waits behind an arrival that
	// is still in flight, so the owner finishes first, with the files whole.
	f.repairFiles()
	owner.Complete()
	e2Quit(t, b)
}

// A CHAT THAT ANOTHER COMPUTER NOW HOLDS IS NOT RESTARTED. A fetch here would set
// the local copy aside and seal its partial tree as edits, so the refusal stands
// until the chat is whole again: the open claims no hold and fetches nothing.
func TestAppOpenLeavesAnAbandonedChatAloneWhenAnotherComputerHoldsIt(t *testing.T) {
	f := newRestartFixture(t)
	// A takes the chat back while B is closed.
	a := e2StartApp(t, f.dirA)
	machineCall[appwire.TakeResult](t, a.w, "agents.take", appwire.TakeParams{Cell: f.chat, Seen: appwire.Seen{Device: appDeviceSync(t, f.dirB).Device.ID(), Fence: f.fence}})
	// A is still running, so its hold is live: the lease names A, and A's own
	// heartbeats may still move the fence, so only WHO holds it is compared below.
	aDevice := appDeviceSync(t, f.dirA).Device.ID()
	if heldBy, _ := f.lease(); heldBy != aDevice {
		t.Fatalf("A did not take the chat: the lease names %q, not A (%q)", heldBy, aDevice)
	}
	f.breakFiles()
	f.abandon()
	before, err := os.ReadFile(f.marker())
	if err != nil || len(before) == 0 {
		t.Fatalf("the abandoned arrival left no marker: %v", err)
	}

	b := e2StartApp(t, f.dirB)
	// The open may be answered or refused (A holds the chat); either way it must
	// not reach for the files or for a hold.
	_, _ = b.w.call("agents.open", appwire.IDParams{ID: f.agent})
	time.Sleep(2 * time.Second)
	if got, _ := os.ReadFile(f.marker()); !bytes.Equal(got, before) {
		t.Fatalf("the open replaced the failure of a chat it could not restart: %q, was %q", got, before)
	}
	if got := f.filesMatch(); got == "" {
		t.Fatal("the open fetched the files of a chat another computer holds")
	}
	if !cell.WorkspaceArriving(f.root) {
		t.Fatal("the open forgot the failure of a chat it could not restart")
	}
	if device, _ := f.lease(); device != aDevice {
		t.Fatalf("the open took the hold from A: the lease now names %q, A is %q", device, aDevice)
	}
	e2Quit(t, b)
	e2Quit(t, a)
}

// THE TERMINAL'S OPEN IS THE RESTART TOO. Every terminal open of a saved chat (the
// launch, Home's local open, the resume picker) ends in openV3Agent, so that is
// the door this drives: the conversation it would open never needs to exist for
// the files to be fetched again, because the restart runs in front of it.
func TestTerminalOpenRestartsTheFilesOfAHeldChatWhoseArrivalWasAbandoned(t *testing.T) {
	f := newRestartFixture(t)
	f.breakFiles()
	f.abandon()
	heldBy, _ := f.lease()
	// The terminal reads this computer's own home from the environment.
	t.Setenv(home.EnvVar, f.dirB)

	stop := errors.New("the conversation is not what this test is about")
	opened := false
	_, _, _, err := openV3Agent(session.Config{Place: session.Place{Dir: f.root}, Workspace: f.workspace}, f.workspace, func(session.Config) (*session.Agent, error) {
		opened = true
		return nil, stop
	})
	if err == nil {
		t.Fatal("the stubbed conversation opened")
	}
	if opened {
		// The restart is in front of the open: by the time the conversation is built
		// the failure has been replaced by an arrival that is on its way or landed.
		if cell.AbandonedArrival(f.root) != nil {
			t.Fatal("the conversation was built while the failure was still abandoned")
		}
	}
	restartWait(t, "the abandoned files to be fetched again", func() bool {
		return !cell.WorkspaceArriving(f.root) && f.filesMatch() == ""
	})
	if device, _ := f.lease(); device != heldBy {
		t.Fatalf("the lease moved from %q to %q", heldBy, device)
	}
}
