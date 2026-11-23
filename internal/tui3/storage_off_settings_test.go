package tui3

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cell/celltest"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	cellstoretest "github.com/Agent-Field/codeaf/internal/cellstore/celltest"
	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/executor"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

type storageOffTaker struct {
	continuer *syncsetup.Continuer
	on        func(syncsetup.Continued)
}

func (f storageOffTaker) Take(ctx context.Context, id string) (Taken, error) {
	got, err := f.continuer.Take(ctx, id)
	if err == nil && f.on != nil {
		f.on(got)
	}
	// Carry outcome flags without requiring new declarations in the before-fix tree.
	raw, _ := json.Marshal(got)
	var taken Taken
	_ = json.Unmarshal(raw, &taken)
	taken.Fence, taken.Head = got.Taken.Driving.Fence, got.Taken.Driving.Head
	taken.Transcript = filepath.Join(got.Taken.Cell.Root, cell.TranscriptPath)
	return taken, err
}

type storageOffReadOnlyStore struct {
	blobstore.Store
	writes int
}

func (s *storageOffReadOnlyStore) PutFrame(context.Context, []byte) (blobstore.FrameID, error) {
	s.writes++
	return "", errors.New("unexpected store write")
}

// B5 uses the real continuation card, reopened agent and Enter road to save the next turn.
func TestContinueCardSendsAndSavesWhileStorageOff(t *testing.T) {
	celltest.RequireEngine(t)
	lab := newHomeLab(t)
	work := lab.workspace("project")
	at, err := cell.CreateIn(lab.project("project"), cell.Options{Class: cell.HostBound})
	if err != nil {
		t.Fatal(err)
	}
	file := lab.session("project", at.ID, "Local chat", work, time.Now())
	canonical := session.Place{Dir: at.Root}.Transcript()
	if err := os.Rename(file, canonical); err != nil {
		t.Fatal(err)
	}
	file = canonical
	appendFile := func(words string) {
		f, err := os.OpenFile(file, os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			t.Fatal(err)
		}
		_, err = f.WriteString(`{"type":"message","role":"user","content":"` + words + `"}` + "\n")
		if err != nil {
			t.Fatal(err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
	}
	eng := cellstore.EngineFor(work)
	eng.DataRoot = cellstoretest.EngineDir(t)
	for _, words := range []string{"preserved one", "preserved two"} {
		appendFile(words)
		if _, err := eng.Seal(t.Context(), at, cellstore.TurnInfo{}); err != nil {
			t.Fatal(err)
		}
	}
	if err := cellstore.NotePublished(at, ""); err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	id, err := identity.Ensure(home)
	if err != nil {
		t.Fatal(err)
	}
	dev, err := identity.Device(home)
	if err != nil {
		t.Fatal(err)
	}
	dir := directory.NewMemory(time.Now)
	defer dir.Close()
	view, err := dir.For("other-computer").Create(t.Context(), at.ID, directory.CellInit{})
	if err != nil {
		t.Fatal(err)
	}
	store := &storageOffReadOnlyStore{Store: blobstore.NewMemory()}
	s := &syncsetup.Sync{Home: home, Relay: syncsetup.ProductionRelayURL, Identity: id, Device: dev, Dir: dir.For(dev.ID()), Store: store}
	a := lab.app(file)
	t.Cleanup(a.leaveEverything)
	a.width, a.height = 200, 70
	a.open = func(workspace, transcript string) (Conversation, error) {
		seat, err := cellstore.SeatOver(executor.HostBound, at, workspace, nil, nil, func(cellstore.Engine) cellstore.Store { return eng })
		if err != nil {
			return Conversation{}, err
		}
		agent, err := session.New(session.Config{Workspace: workspace, Place: session.Place{Dir: at.Root, Workspace: workspace}, SessionFile: transcript, Seat: seat, Model: "stub/chat", APIKey: "stub", BaseURL: "http://127.0.0.1:1"})
		return Conversation{Agent: agent, Workspace: workspace, SessionFile: transcript, Resumed: true}, err
	}
	drain(t, a, a.openHome())
	src := &continuationListing{row: chatlist.Row{Cell: at.ID, Title: "Local chat", Device: "other-computer", DeviceID: "other-computer", Fence: view.Cell.Lease.Fence, Status: chatlist.Running}}
	a.machines = src
	a.taker = storageOffTaker{
		continuer: s.Continuer(eng, syncsetup.TakeOptions{RootFor: func(string) string { return at.Root }}),
		on: func(got syncsetup.Continued) {
			src.change(func(row *chatlist.Row) {
				row.Mine, row.Status, row.DeviceID, row.Fence = true, chatlist.Here, dev.ID(), got.Taken.Driving.Fence
			})
		},
	}
	// A previous displaced writer must not make the local continuation wait again.
	a.machineRead.sendHolds = map[string]sendHold{at.ID: {fence: view.Cell.Lease.Fence, failed: true}}
	drain(t, a, a.askMachines())
	standOn(t, a, at.ID)
	confirm(t, a)
	if a.at(pageHome) || a.file != file {
		t.Fatalf("continuation did not reopen the conversation: %s", plain(frame(a)))
	}
	if a.machineRead.sendHolds[at.ID].awaitingWriter {
		t.Fatal("local continuation waits for an uploading writer")
	}
	if strings.Contains(plain(frame(a)), "continue here") || strings.Contains(plain(frame(a)), "held on other-computer") {
		t.Fatal("continuation card remained")
	}
	typeLine(t, a, "! printf next-local-turn")
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	for _, words := range []string{"preserved one", "preserved two", "next-local-turn"} {
		if !strings.Contains(string(raw), words) {
			t.Fatalf("saved conversation lacks %q: %s", words, raw)
		}
	}
	turns, err := cellstore.Turns(at)
	if err != nil || len(turns) < 3 || store.writes != 0 {
		t.Fatalf("turns=%d writes=%d err=%v", len(turns), store.writes, err)
	}
}

type storageOffSampleDoor struct {
	*storageApprovals
	reads   atomic.Int32
	entered chan struct{}
	release chan struct{}
}

func (d *storageOffSampleDoor) RelayStorage() (string, string) {
	d.reads.Add(1)
	if d.entered != nil {
		d.entered <- struct{}{}
		<-d.release
	}
	return d.storageApprovals.RelayStorage()
}

// Item 7 bounds account reads to visible consumers, with one in flight and ordered answers.
func TestSettingsSamplesOnlyVisibleStorage(t *testing.T) {
	door := &storageOffSampleDoor{storageApprovals: &storageApprovals{fakeApprovals: devicesFake(), code: "id_account", state: "off"}}
	a := newTestApp(&fakeAgent{model: "m"})
	a.approvals = door
	a.openSettings()
	for i := 0; i < 4; i++ {
		_, cmd := (placeSettings{}).tick(a, time.Now())
		if cmd != nil {
			a.Update(cmd())
		}
	}
	if got := door.reads.Load(); got != 0 {
		t.Fatalf("unrelated Settings tab read the account %d times", got)
	}
	for i, tab := range settingTabs {
		if tab == tabDevices {
			a.sheet.tab = i
		}
	}
	door.entered, door.release = make(chan struct{}, 1), make(chan struct{})
	_, cmd := (placeSettings{}).tick(a, time.Now())
	if cmd == nil {
		t.Fatal("Devices did not read storage")
	}
	answer := make(chan any, 1)
	go func() { answer <- cmd() }()
	<-door.entered
	for i := 0; i < 4; i++ {
		if _, cmd := (placeSettings{}).tick(a, time.Now()); cmd != nil {
			t.Fatal("overlapping account read")
		}
	}
	close(door.release)
	old := <-answer
	a.Update(old)
	door.entered, door.release = nil, nil
	door.state = "on"
	_, cmd = (placeSettings{}).tick(a, time.Now())
	if cmd == nil {
		t.Fatal("next beat did not refresh")
	}
	a.Update(cmd())
	a.Update(old)
	if a.sheet.storageState != "on" || door.reads.Load() != 2 {
		t.Fatalf("state=%s reads=%d", a.sheet.storageState, door.reads.Load())
	}
	a.sheet.tab = 0
	a.sheet.query.insert("sync storage")
	_, cmd = (placeSettings{}).tick(a, time.Now())
	if cmd == nil {
		t.Fatal("Settings search did not refresh")
	}
	a.Update(cmd())
}

// Item 8 crosses a real remote client with an older saving reply that omits StorageOff.
func TestCloseOlderRemoteSavingReceipt(t *testing.T) {
	for _, receipt := range []bool{true, false} {
		t.Run(map[bool]string{true: "positive local receipt", false: "no durability receipt"}[receipt], func(t *testing.T) {
			front, back := remote.Pipe()
			done := make(chan struct{})
			go func() {
				defer close(done)
				defer back.Close()
				dec, enc := json.NewDecoder(back), json.NewEncoder(back)
				var hello remote.Frame
				if dec.Decode(&hello) != nil {
					return
				}
				welcome, _ := json.Marshal(remote.Welcome{Version: remote.Version, LocalSavingReceipts: receipt, SessionFile: "/chat/transcript.jsonl"})
				if enc.Encode(remote.Frame{Kind: "welcome", Payload: welcome}) != nil {
					return
				}
				for {
					var call remote.Frame
					if dec.Decode(&call) != nil {
						return
					}
					if call.Method != remote.MethodClose {
						if enc.Encode(remote.Frame{Kind: "result", ID: call.ID, Payload: json.RawMessage("null")}) != nil {
							return
						}
						continue
					}
					// This map deliberately has no storageOff key, as on the compatible peer.
					_ = enc.Encode(map[string]any{"kind": "result", "id": call.ID, "error": "upload pending", "localSaved": receipt, "syncPending": receipt})
					return
				}
			}()
			client, err := remote.Dial(front, "older-peer", remote.Hello{})
			if err != nil {
				t.Fatal(err)
			}
			defer func() { front.Close(); <-done }()
			a := newTestApp(client.Agent())
			t.Cleanup(a.leaveEverything)
			a.file = "/chat/transcript.jsonl"
			a.syncService = "http://127.0.0.1:8901"
			emptyMachine(a)
			a.stow(Conversation{Agent: &fakeAgent{model: "m"}, SessionFile: "/remaining/transcript.jsonl"}, &aside{})
			cmd, more := a.closeFront()
			if !more {
				t.Fatal("remaining chat missing")
			}
			spend(t, a, cmd)
			shown := strings.Join(strings.Fields(plain(frame(a))), " ")
			want := closeSaveFailedWord
			if receipt {
				want = "saved here; it reaches your other devices when the sync service at 127.0.0.1:8901 answers"
			}
			if !strings.Contains(shown, want) {
				t.Fatalf("receipt=%v frame=%s", receipt, shown)
			}
		})
	}
}
