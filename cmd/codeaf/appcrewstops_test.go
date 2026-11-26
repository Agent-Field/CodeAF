package main

import (
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
	teamstore "github.com/Agent-Field/codeaf/internal/teams"
)

// dooredConversation remembers the door each stop came through, as a
// conversation reached across a connection is told it.
type dooredConversation struct {
	*fakeConversation
	doorMu sync.Mutex
	doors  []session.StopDoor
}

func (d *dooredConversation) Interrupt() { d.InterruptFor(session.StopByPerson) }

func (d *dooredConversation) InterruptFor(door session.StopDoor) {
	d.doorMu.Lock()
	defer d.doorMu.Unlock()
	d.doors = append(d.doors, door)
}

func (d *dooredConversation) stops() []session.StopDoor {
	d.doorMu.Lock()
	defer d.doorMu.Unlock()
	return slices.Clone(d.doors)
}

// boundedDooredConversation is a [dooredConversation] whose stop through a
// named door can carry its bound, as one reached across a connection can.
type boundedDooredConversation struct {
	*dooredConversation
	graces []time.Duration
}

func (b *boundedDooredConversation) InterruptWithinFor(door session.StopDoor, grace time.Duration) {
	b.InterruptFor(door)
	b.doorMu.Lock()
	defer b.doorMu.Unlock()
	b.graces = append(b.graces, grace)
}

// dooredBackend holds one conversation that records its stops.
func dooredBackend(t *testing.T, assembled bool) (*appBackend, *dooredConversation, appwire.Agent) {
	t.Helper()
	state, profile := t.TempDir(), t.TempDir()
	conv := &dooredConversation{fakeConversation: &fakeConversation{}}
	backend := newFakeBackend(newAppRecorder(), func(ask appOpenAsk) (*appConn, error) {
		return &appConn{Conv: conv, Workspace: ask.Workspace, Transcript: appFakeTranscript, Assembled: assembled, Release: func() {}}, nil
	})
	t.Cleanup(func() { backend.Close() })
	backend.state = &appState{path: filepath.Join(state, appStateName)}
	backend.profileDir = profile
	agent, err := mustCreate(t, backend, appwire.CreateAsk{Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	return backend, conv, agent
}

// wake hands the backend one finished turn it did not start.
func wake(backend *appBackend, id, said string) {
	a := backend.agent(id)
	events := make(chan session.Event, 1)
	events <- session.Event{Kind: session.EventTurnDone}
	close(events)
	backend.wokenTurn(a, backend.liveOf(a), appTurnElsewhere{said: said, events: events})
}

// A PAUSED MEMBER'S STRAY WAKE IS STOPPED AS THE APP'S OWN DOING, and a turn
// the person's words opened is left to run. Through the person's door the stop
// would be told to the member's manager as work the person had stopped.
func TestAppWokenTurnOfAPausedMemberStopsThroughTheAppsDoor(t *testing.T) {
	backend, conv, agent := dooredBackend(t, false)
	crew := teamstore.NewID()
	paused := func(on bool) {
		t.Helper()
		if err := teamstore.Update(backend.profileDir, func(f *teamstore.File) error {
			return f.SetPaused(crew, appFakeTranscript, on)
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := teamstore.Update(backend.profileDir, func(f *teamstore.File) error {
		f.Teams = append(f.Teams, teamstore.Team{ID: crew, Name: "crew"})
		lead := filepath.Join(t.TempDir(), "lead", "transcript.jsonl")
		if err := f.AddMember(crew, teamstore.Member{Key: lead, File: lead, Handle: "lead"}); err != nil {
			return err
		}
		if err := f.AddMember(crew, teamstore.Member{Key: appFakeTranscript, File: appFakeTranscript, Handle: "scout", Started: true}); err != nil {
			return err
		}
		return f.SetManager(crew, lead)
	}); err != nil {
		t.Fatal(err)
	}

	wake(backend, agent.ID, "")
	if got := conv.stops(); len(got) != 0 {
		t.Fatalf("a wake of a member that is not paused was stopped: %v", got)
	}
	paused(true)
	if !backend.crewPaused(agent.ID) {
		t.Fatal("the app does not read the member as paused")
	}
	wake(backend, agent.ID, "carry on with the header")
	if got := conv.stops(); len(got) != 0 {
		t.Fatalf("a turn the person's own words opened on a paused member was stopped: %v", got)
	}
	wake(backend, agent.ID, "")
	if got := conv.stops(); len(got) != 1 || got[0] != session.StopByWorkStopped {
		t.Fatalf("a paused member's stray wake was stopped through %v, want only %q", got, session.StopByWorkStopped)
	}
}

// QUITTING THE APP IS NOT THE PERSON'S STOP: a conversation the app assembled
// is stopped through the closing door, so a member's manager is not told the
// person stopped its work, and one that is another window's is left alone.
func TestAppCloseStopsThroughTheClosingDoor(t *testing.T) {
	backend, conv, _ := dooredBackend(t, true)
	backend.Close()
	if got := conv.stops(); len(got) != 1 || got[0] != session.StopByClosing {
		t.Fatalf("closing the app stopped its conversation through %v, want only %q", got, session.StopByClosing)
	}

	backend, conv, _ = dooredBackend(t, false)
	backend.Close()
	if got := conv.stops(); len(got) != 0 {
		t.Fatalf("closing the app stopped a conversation it only joined: %v", got)
	}
}

// AN EARLIER BUILD'S PAUSE IS CLEARED FROM THE APP'S FILE ONLY ONCE THE TEAM'S
// RECORD HAS IT. A store that could not be read keeps the flag for the next
// launch; a pause whose crew is gone has nowhere to go and is dropped.
func TestAppMovePausesToStoreKeepsAPauseItCouldNotCarry(t *testing.T) {
	backend, _, agent := dooredBackend(t, false)
	crew, gone := teamstore.NewID(), teamstore.NewID()
	if err := teamstore.Update(backend.profileDir, func(f *teamstore.File) error {
		f.Teams = append(f.Teams, teamstore.Team{ID: crew, Name: "crew"})
		lead := filepath.Join(t.TempDir(), "lead", "transcript.jsonl")
		if err := f.AddMember(crew, teamstore.Member{Key: lead, File: lead, Handle: "lead"}); err != nil {
			return err
		}
		if err := f.AddMember(crew, teamstore.Member{Key: appFakeTranscript, File: appFakeTranscript, Handle: "scout", Started: true}); err != nil {
			return err
		}
		return f.SetManager(crew, lead)
	}); err != nil {
		t.Fatal(err)
	}
	if err := backend.crewLayer().write(func(f *appCrewLayerFile) error {
		f.member(crew, agent.ID).Paused = true
		f.member(gone, agent.ID).Paused = true
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	flag := func(crew string) (paused bool) {
		t.Helper()
		if err := backend.crewLayer().read(func(f *appCrewLayerFile) { paused = f.member(crew, agent.ID).Paused }); err != nil {
			t.Fatal(err)
		}
		return paused
	}

	// The store cannot be read: nothing is carried and nothing is forgotten.
	path := teamstore.Path(backend.profileDir)
	if err := os.Rename(path, path+".aside"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	backend.movePausesToStore()
	if !flag(crew) || !flag(gone) {
		t.Fatalf("a pause that could not be carried was cleared: crew=%v gone=%v", flag(crew), flag(gone))
	}

	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path+".aside", path); err != nil {
		t.Fatal(err)
	}
	backend.movePausesToStore()
	if !backend.crewPaused(agent.ID) {
		t.Fatal("the earlier build's pause was not carried onto the team's record")
	}
	if flag(crew) || flag(gone) {
		t.Fatalf("a settled pause is still in the app's file: crew=%v gone=%v", flag(crew), flag(gone))
	}
}

// A PAUSE ENDS THE TURN THROUGH THE PAUSE'S OWN DOOR, never the person's Stop.
// Through the person's door the engine began the stop hold: a command's ending
// was kept as stopped work, the manager was told "stopped by the person" and
// not to hand the work out again, and Resume started nothing. The stop line the
// pause writes is read back the same way, and a real Stop still uses the
// person's door.
func TestAppPauseEndsTheTurnThroughThePausesDoor(t *testing.T) {
	backend, conv, agent := dooredBackend(t, false)
	crew := teamstore.NewID()
	if err := teamstore.Update(backend.profileDir, func(f *teamstore.File) error {
		f.Teams = append(f.Teams, teamstore.Team{ID: crew, Name: "crew"})
		lead := filepath.Join(t.TempDir(), "lead", "transcript.jsonl")
		if err := f.AddMember(crew, teamstore.Member{Key: lead, File: lead, Handle: "lead"}); err != nil {
			return err
		}
		if err := f.AddMember(crew, teamstore.Member{Key: appFakeTranscript, File: appFakeTranscript, Handle: "scout", Started: true}); err != nil {
			return err
		}
		return f.SetManager(crew, lead)
	}); err != nil {
		t.Fatal(err)
	}
	only := func(want session.StopDoor) {
		t.Helper()
		got := conv.stops()
		if len(got) == 0 {
			t.Fatalf("nothing was stopped, want %q", want)
		}
		for _, door := range got {
			if door != want {
				t.Fatalf("the member was stopped through %v, want only %q", got, want)
			}
		}
	}

	if _, err := backend.Member("pause", appwire.MemberAsk{Crew: crew, Agent: agent.ID}); err != nil {
		t.Fatal(err)
	}
	only(session.StopByPause)

	entries, err := teamstore.ReadTraffic(backend.profileDir, crew, "", 100)
	if err != nil {
		t.Fatal(err)
	}
	file, err := teamstore.Load(backend.profileDir)
	if err != nil {
		t.Fatal(err)
	}
	team, _ := file.Team(crew)
	conv.doorMu.Lock()
	conv.doors = nil
	conv.doorMu.Unlock()
	pauses := 0
	for _, entry := range entries {
		if entry.PauseStop() {
			pauses++
			backend.crewAct(&appCrewRun{acted: map[string]bool{}}, appCrewRead{file: file}, team, entry)
		}
	}
	if pauses != 1 {
		t.Fatalf("the pause wrote %d stop lines, want one", pauses)
	}
	only(session.StopByPause)

	if _, err := backend.Member("resume", appwire.MemberAsk{Crew: crew, Agent: agent.ID}); err != nil {
		t.Fatal(err)
	}
	conv.doorMu.Lock()
	conv.doors = nil
	conv.doorMu.Unlock()
	if _, err := backend.Member("stop", appwire.MemberAsk{Crew: crew, Agent: agent.ID}); err != nil {
		t.Fatal(err)
	}
	only(session.StopByPerson)
}

// A PAUSE CARRIES THE BOUND THE PERSON'S STOP CARRIES. Through an unbounded
// interrupt a turn parked on a wait that ignores its context stayed "working"
// after Pause; the conversation is given the stop's own grace with the pause's
// door.
func TestAppPauseIsBoundedLikeTheStop(t *testing.T) {
	state, profile := t.TempDir(), t.TempDir()
	conv := &boundedDooredConversation{dooredConversation: &dooredConversation{fakeConversation: &fakeConversation{}}}
	backend := newFakeBackend(newAppRecorder(), func(ask appOpenAsk) (*appConn, error) {
		return &appConn{Conv: conv, Workspace: ask.Workspace, Transcript: appFakeTranscript, Release: func() {}}, nil
	})
	t.Cleanup(func() { backend.Close() })
	backend.state = &appState{path: filepath.Join(state, appStateName)}
	backend.profileDir = profile
	agent, err := mustCreate(t, backend, appwire.CreateAsk{Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	crew := teamstore.NewID()
	if err := teamstore.Update(backend.profileDir, func(f *teamstore.File) error {
		f.Teams = append(f.Teams, teamstore.Team{ID: crew, Name: "crew"})
		lead := filepath.Join(t.TempDir(), "lead", "transcript.jsonl")
		if err := f.AddMember(crew, teamstore.Member{Key: lead, File: lead, Handle: "lead"}); err != nil {
			return err
		}
		if err := f.AddMember(crew, teamstore.Member{Key: appFakeTranscript, File: appFakeTranscript, Handle: "scout", Started: true}); err != nil {
			return err
		}
		return f.SetManager(crew, lead)
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := backend.Member("pause", appwire.MemberAsk{Crew: crew, Agent: agent.ID}); err != nil {
		t.Fatal(err)
	}
	conv.doorMu.Lock()
	defer conv.doorMu.Unlock()
	if len(conv.doors) == 0 || len(conv.graces) != len(conv.doors) {
		t.Fatalf("the pause stopped through %v with bounds %v, want each stop bounded", conv.doors, conv.graces)
	}
	for i, door := range conv.doors {
		if door != session.StopByPause || conv.graces[i] != session.StopGrace {
			t.Fatalf("the pause stopped through %v with bounds %v, want the pause's door with %v", conv.doors, conv.graces, session.StopGrace)
		}
	}
}
