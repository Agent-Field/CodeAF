package tui3

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/syncwords"
	"github.com/Agent-Field/codeaf/internal/wireauth"
)

// B1 requires the first status frame after pairing to use the cached permission.
func TestFirstStatusAfterPairing(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	door := &storageApprovals{fakeApprovals: devicesFake(), state: "off"}
	a.approvals = door
	requests := 0
	a.syncStats = func(context.Context) (blobstore.Counts, error) { requests++; return blobstore.Counts{}, nil }
	door.code = "id_account"
	cmd := a.runStatusCommand("")
	shown := plain(frame(a))
	if !strings.Contains(strings.Join(strings.Fields(shown), " "), "sync storage off") || requests != 0 || cmd == nil {
		t.Fatalf("first status frame, requests=%d: %s", requests, shown)
	}
}

// B2 offers the request action in search only while storage is off.
func TestStorageSearchActionMatchesPermission(t *testing.T) {
	for _, state := range []string{"off", "on"} {
		t.Run(state, func(t *testing.T) {
			door := &storageApprovals{fakeApprovals: devicesFake(), code: "id_account", state: state}
			a, r := openSettingsDevices(t, door, time.Now())
			r.press("ctrl+u")
			r.typeText("sync storage")
			found := false
			for i, item := range a.sheet.items {
				if item.dev != nil && item.dev.storage {
					a.sheet.cursor = i
					found = true
					break
				}
			}
			if !found {
				t.Fatal("storage search result missing")
			}
			shown := plain(frame(a))
			if strings.Contains(shown, "enter request access") != (state == "off") {
				t.Fatalf("search footer: %s", shown)
			}
			if state == "on" {
				old := processOpener
				processOpener = func(string) error { t.Error("approved row opened a request"); return nil }
				defer func() { processOpener = old }()
				r.press("enter")
				if a.sheet.storageRequest {
					t.Fatal("approved row requested storage")
				}
			}
		})
	}
}

type storageOffCloseAgent struct {
	*fakeAgent
	err error
}

func (a *storageOffCloseAgent) Close() error { return a.err }

// B6 distinguishes a durable local save from a failed save when another chat remains open.
func TestCloseStorageOffReceipt(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{"storage off", syncwords.UploadFailure(wireauth.ErrStorageOff), "saved here; sync storage is off for this account, so it stays on this computer"},
		{"local failure", errors.New("disk full"), closeSaveFailedWord},
		{"mixed failure", errors.Join(errors.New("disk full"), syncwords.UploadFailure(wireauth.ErrStorageOff)), closeSaveFailedWord},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := newTestApp(&storageOffCloseAgent{fakeAgent: &fakeAgent{model: "m"}, err: tc.err})
			t.Cleanup(a.leaveEverything)
			a.file = "/tmp/closing/transcript.jsonl"
			emptyMachine(a)
			a.stow(Conversation{Agent: &fakeAgent{model: "m"}, SessionFile: "/tmp/remaining/transcript.jsonl"}, &aside{})
			if a.openCount() != 2 {
				t.Fatal("fixture must have exactly two chats")
			}
			cmd, more := a.closeFront()
			if !more {
				t.Fatal("no remaining chat")
			}
			spend(t, a, cmd)
			shown := strings.Join(strings.Fields(plain(frame(a))), " ")
			if !strings.Contains(shown, tc.want) || tc.name == "storage off" && strings.Contains(shown, closeSaveFailedWord) {
				t.Fatalf("close frame: %s", shown)
			}
		})
	}
}

// B3 keeps an already-open Settings frame current after the background answer arrives.
func TestOpenSettingsNoticesPairingAnswer(t *testing.T) {
	door := &storageApprovals{fakeApprovals: devicesFake()}
	a, _ := openSettingsDevices(t, door, time.Now())
	if strings.Contains(plain(frame(a)), "sync storage") {
		t.Fatal("unpaired row shown")
	}
	door.code, door.state = "id_account", "off"
	_, cmd := (placeSettings{}).tick(a, time.Now())
	if cmd == nil {
		t.Fatal("Settings never sampled the account answer")
	}
	a.Update(cmd())
	shown := plain(frame(a))
	if !strings.Contains(shown, "sync storage") || a.sheet.storageState != "off" {
		t.Fatalf("paired row missing: %s", shown)
	}
}

type storageOffLateCloseAgent struct{ *blockedCloseAgent }

func (a *storageOffLateCloseAgent) Close() error {
	_ = a.blockedCloseAgent.Close()
	return syncwords.UploadFailure(wireauth.ErrStorageOff)
}

// B6 reports the same saved-here receipt when an individual close finishes after the wait.
func TestLateCloseStorageOffReceipt(t *testing.T) {
	agent := &storageOffLateCloseAgent{&blockedCloseAgent{fakeAgent: &fakeAgent{model: "m"}, release: make(chan struct{})}}
	a := newTestApp(agent)
	t.Cleanup(a.leaveEverything)
	a.file, a.title = "/tmp/closing/transcript.jsonl", "saved local chat"
	emptyMachine(a)
	a.stow(Conversation{Agent: &fakeAgent{model: "m"}, SessionFile: "/tmp/remaining/transcript.jsonl"}, &aside{})
	if a.openCount() != 2 {
		t.Fatal("fixture must have exactly two chats")
	}
	finished := make(chan struct{})
	a.closingStarted = func(done <-chan struct{}) { go func() { <-done; close(finished) }() }
	cmd, more := a.closeFront()
	if !more {
		t.Fatal("no remaining chat")
	}
	spend(t, a, cmd)
	close(agent.release)
	<-finished
	a.Update(noticesMsg{})
	shown := strings.Join(strings.Fields(plain(frame(a))), " ")
	if !strings.Contains(shown, "saved here; sync storage is off for this account, so it stays on this computer") || strings.Contains(shown, closeSaveFailedWord) {
		t.Fatalf("late receipt: %s", shown)
	}
}
