package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
)

// appSlowOpen is an opener that holds every open until release is closed, the
// way a fresh home's first open of a large workspace holds it for many
// seconds, and counts the opens it was asked for.
type appSlowOpen struct {
	conv    *fakeConversation
	release chan struct{}
	asked   atomic.Int64
}

func newAppSlowOpen(conv *fakeConversation) *appSlowOpen {
	return &appSlowOpen{conv: conv, release: make(chan struct{})}
}

func (o *appSlowOpen) open(ask appOpenAsk) (*appConn, error) {
	o.asked.Add(1)
	<-o.release
	return &appConn{Conv: o.conv, Workspace: ask.Workspace, Transcript: appFakeTranscript, Release: func() {}}, nil
}

// waitOpening waits until the backend has asked for the open, so a call made
// next lands while the open holds the agent's gate.
func (o *appSlowOpen) waitOpening(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for o.asked.Load() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the open was never asked for")
		}
		time.Sleep(time.Millisecond)
	}
}

// answersWithin runs call and fails the test if it has not returned in d: the calls
// tested here must answer while an open is still held.
func answersWithin(t *testing.T, d time.Duration, what string, call func()) {
	t.Helper()
	done := make(chan struct{})
	go func() { defer close(done); call() }()
	select {
	case <-done:
	case <-time.After(d):
		t.Fatalf("%s did not answer within %v while the conversation was still opening", what, d)
	}
}

func (f *fakeConversation) submitted() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.submits
}

func (f *fakeConversation) steeredWords() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.steered...)
}

// AGENTS.OPEN DOES NOT WAIT FOR THE BOOT. A created agent whose conversation is
// still opening answers agents.open at once with its view, which reads
// starting; the turn's start says working.
func TestAppOpenAnswersAStartingAgentAtOnce(t *testing.T) {
	conv := &fakeConversation{script: []session.Event{{Kind: session.EventTurnDone}}}
	slow := newAppSlowOpen(conv)
	rec := newAppRecorder()
	backend := newFakeBackend(rec, slow.open)
	defer backend.Close()
	agent, err := backend.CreateAgent(appwire.CreateAsk{Workspace: t.TempDir(), Text: "fix the parser", ClientID: "c-1"})
	if err != nil {
		t.Fatal(err)
	}
	if agent.State != appwire.StateStarting {
		t.Fatalf("a created agent with words reads %q, want %q", agent.State, appwire.StateStarting)
	}
	slow.waitOpening(t)
	var opened appwire.Agent
	answersWithin(t, 2*time.Second, "agents.open", func() { opened, err = backend.OpenAgent(agent.ID) })
	if err != nil || opened.ID != agent.ID || opened.State != appwire.StateStarting {
		t.Fatalf("agents.open while starting = %+v, %v", opened, err)
	}
	// A disk read in the meantime does not say it gave up.
	if _, err := backend.find(agent.ID); err != nil {
		t.Fatal(err)
	}
	var state string
	backend.locked(func() { state = backend.agents[agent.ID].view.State })
	if state != appwire.StateStarting {
		t.Fatalf("after a disk read the starting agent reads %q", state)
	}
	close(slow.release)
	rec.waitFor(t, 5*time.Second, func(event string, data any) bool {
		c, ok := data.(appwire.AgentChanged)
		return ok && c.Agent.ID == agent.ID && c.Agent.State == appwire.StateWorking
	})
	rec.waitFor(t, 5*time.Second, appTurnDone)
}

// A STOP WHILE STARTING TAKES EFFECT. The first turn never starts, the agent
// ends stopped, the turn it would have been ends stopped, and its words are
// kept for agents.retry because they were never sent.
func TestAppStopWhileStartingKeepsTheFirstTurnFromRunning(t *testing.T) {
	conv := &fakeConversation{script: []session.Event{{Kind: session.EventTurnDone}}}
	slow := newAppSlowOpen(conv)
	rec := newAppRecorder()
	backend := newFakeBackend(rec, slow.open)
	defer backend.Close()
	agent, err := backend.CreateAgent(appwire.CreateAsk{Workspace: t.TempDir(), Text: "rewrite everything", ClientID: "c-2"})
	if err != nil {
		t.Fatal(err)
	}
	slow.waitOpening(t)
	answersWithin(t, 2*time.Second, "agents.stop", func() { err = backend.Stop(agent.ID) })
	if err != nil {
		t.Fatalf("agents.stop while starting = %v", err)
	}
	close(slow.release)
	i := rec.waitFor(t, 5*time.Second, func(event string, data any) bool {
		d, ok := data.(appwire.TurnDone)
		return ok && d.Agent == agent.ID
	})
	rec.mu.Lock()
	done := rec.data[i].(appwire.TurnDone)
	rec.mu.Unlock()
	if done.OK || done.ErrorKind != appwire.ErrorStopped {
		t.Fatalf("the stopped start ended %+v", done)
	}
	rec.waitFor(t, 5*time.Second, func(event string, data any) bool {
		c, ok := data.(appwire.AgentChanged)
		return ok && c.Agent.ID == agent.ID && c.Agent.State == appwire.StateStopped && c.Agent.Retryable
	})
	// Nothing reached the model, now or a moment later.
	time.Sleep(50 * time.Millisecond)
	if n := conv.submitted(); n != 0 {
		t.Fatalf("the first turn was sent %d times after a stop", n)
	}
	// The words were never sent, so a retry sends them.
	if _, err := backend.Retry(agent.ID); err != nil {
		t.Fatalf("retry after a stopped start = %v", err)
	}
	rec.waitFor(t, 5*time.Second, func(event string, data any) bool {
		d, ok := data.(appwire.TurnDone)
		return ok && d.Agent == agent.ID && d.OK
	})
	if n := conv.submitted(); n != 1 {
		t.Fatalf("the retry sent %d turns", n)
	}
}

// A STOP THAT LANDS BETWEEN THE OPEN AND THE TURN'S START STILL STOPS IT: the
// turn is interrupted the moment it runs.
func TestAppStopRightAsTheFirstTurnStartsInterruptsIt(t *testing.T) {
	hold := make(chan struct{})
	defer close(hold)
	conv := &fakeConversation{hold: hold}
	rec := newAppRecorder()
	var backend *appBackend
	backend = newFakeBackend(rec, func(ask appOpenAsk) (*appConn, error) {
		return &appConn{Conv: conv, Workspace: ask.Workspace, Transcript: appFakeTranscript, Release: func() {}}, nil
	})
	defer backend.Close()
	agent, err := backend.CreateAgent(appwire.CreateAsk{Workspace: t.TempDir(), Text: "go"})
	if err != nil {
		t.Fatal(err)
	}
	// The stop is kept whichever side of the turn's start it lands on.
	a, _ := backend.find(agent.ID)
	backend.locked(func() {
		if a.starting {
			a.stopAsked = true
		}
	})
	rec.waitFor(t, 5*time.Second, func(event string, data any) bool {
		c, ok := data.(appwire.AgentChanged)
		return ok && c.Agent.ID == agent.ID && (c.Agent.State == appwire.StateStopped || c.Agent.State == appwire.StateWorking)
	})
	deadline := time.Now().Add(5 * time.Second)
	for {
		conv.mu.Lock()
		submits, interrupted := conv.submits, conv.interrupted
		conv.mu.Unlock()
		if submits == 0 || interrupted > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the first turn ran (%d sends) and was never interrupted", submits)
		}
		time.Sleep(time.Millisecond)
	}
}

// A STEER WHILE STARTING IS NOT REFUSED: it is kept, and given to the first
// turn the moment it runs.
func TestAppSteerWhileStartingReachesTheFirstTurn(t *testing.T) {
	hold := make(chan struct{})
	conv := &fakeConversation{hold: hold, steer: make(chan session.Event)}
	slow := newAppSlowOpen(conv)
	rec := newAppRecorder()
	backend := newFakeBackend(rec, slow.open)
	defer backend.Close()
	agent, err := backend.CreateAgent(appwire.CreateAsk{Workspace: t.TempDir(), Text: "build it", ClientID: "c-3"})
	if err != nil {
		t.Fatal(err)
	}
	slow.waitOpening(t)
	answersWithin(t, 2*time.Second, "agents.steer", func() { err = backend.Steer(agent.ID, "go", "s-1") })
	if err != nil {
		t.Fatalf("a steer while starting was refused: %v", err)
	}
	close(slow.release)
	i := rec.waitFor(t, 5*time.Second, func(event string, data any) bool {
		e, ok := data.(appwire.EntryAdded)
		return ok && e.Entry.Agent == agent.ID && e.Entry.Kind == appwire.EntrySteer
	})
	rec.mu.Lock()
	steer := rec.data[i].(appwire.EntryAdded).Entry
	rec.mu.Unlock()
	if steer.Text != "go" || steer.ClientID != "s-1" || steer.Turn == 0 {
		t.Fatalf("the kept steer's entry = %+v", steer)
	}
	if got := conv.steeredWords(); len(got) != 1 || got[0] != "go" {
		t.Fatalf("the first turn was steered with %q", got)
	}
	close(hold)
	rec.waitFor(t, 5*time.Second, appTurnDone)
}

// A steer kept while starting that the first turn ends before reading is not
// lost: it is the next message.
func TestAppSteerWhileStartingFallsThroughToTheNextMessage(t *testing.T) {
	conv := &fakeConversation{script: []session.Event{{Kind: session.EventTurnDone}}}
	slow := newAppSlowOpen(conv)
	rec := newAppRecorder()
	backend := newFakeBackend(rec, slow.open)
	defer backend.Close()
	agent, err := backend.CreateAgent(appwire.CreateAsk{Workspace: t.TempDir(), Text: "build it"})
	if err != nil {
		t.Fatal(err)
	}
	slow.waitOpening(t)
	if err := backend.Steer(agent.ID, "and test it", ""); err != nil {
		t.Fatalf("a steer while starting was refused: %v", err)
	}
	close(slow.release)
	deadline := time.Now().Add(5 * time.Second)
	for {
		conv.mu.Lock()
		texts := append([]string(nil), conv.texts...)
		conv.mu.Unlock()
		if len(texts) == 2 && texts[1] == "and test it" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the conversation was sent %q; the kept steer never arrived", texts)
		}
		time.Sleep(time.Millisecond)
	}
}

// NOTHING THAT ONLY READS OR STOPS WAITS ON AN OPEN. A crew keeps this agent,
// and a send is opening its conversation again behind a slow open: a stop and
// a process list both answer at once.
func TestAppStopAndProcessListDoNotWaitOnAnOpen(t *testing.T) {
	conv := &fakeConversation{script: []session.Event{{Kind: session.EventTurnDone}}}
	var slowMu sync.Mutex
	var slow *appSlowOpen
	rec := newAppRecorder()
	backend := newFakeBackend(rec, func(ask appOpenAsk) (*appConn, error) {
		slowMu.Lock()
		s := slow
		slowMu.Unlock()
		if s != nil {
			return s.open(ask)
		}
		return &appConn{Conv: conv, Workspace: ask.Workspace, Transcript: appFakeTranscript, Release: func() {}}, nil
	})
	profile := t.TempDir()
	backend.profileDir = profile
	defer backend.Close()
	agent, err := mustCreate(t, backend, appwire.CreateAsk{Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	a, _ := backend.find(agent.ID)
	backend.letGo(a)
	run := backend.crews()
	run.locked(func() { run.held[agent.ID] = []string{"crew1"} })
	slowMu.Lock()
	slow = newAppSlowOpen(conv)
	s := slow
	slowMu.Unlock()
	defer close(s.release)
	go func() { _, _ = backend.Send(agent.ID, "work", "") }()
	s.waitOpening(t)
	answersWithin(t, 2*time.Second, "agents.stop", func() { err = backend.Stop(agent.ID) })
	if err != nil {
		t.Fatalf("agents.stop = %v", err)
	}
	answersWithin(t, 2*time.Second, "processes.list", func() { _, err = backend.ListProcesses(agent.ID) })
	if err != nil {
		t.Fatalf("processes.list = %v", err)
	}
}

// appStuckJobs is a conversation whose engine does not answer for its
// processes, as one mid-way through a long seal or a quit does.
type appStuckJobs struct {
	*fakeConversation
	stuck chan struct{}
}

func (s *appStuckJobs) JobNotices() ([]session.JobNotice, error) {
	<-s.stuck
	return nil, nil
}

// processes.list answers from the record when the engine holding the
// conversation does not answer in time.
func TestAppProcessListAnswersWhenItsEngineIsBusy(t *testing.T) {
	stuck := &appStuckJobs{fakeConversation: &fakeConversation{}, stuck: make(chan struct{})}
	defer close(stuck.stuck)
	rec := newAppRecorder()
	backend := newFakeBackend(rec, func(ask appOpenAsk) (*appConn, error) {
		return &appConn{Conv: stuck, Workspace: ask.Workspace, Transcript: filepath.Join(t.TempDir(), "transcript.jsonl"), Release: func() {}}, nil
	})
	defer backend.Close()
	agent, err := mustCreate(t, backend, appwire.CreateAsk{Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	var list []appwire.Process
	answersWithin(t, appProcessListWait+time.Second, "processes.list", func() { list, err = backend.ListProcesses(agent.ID) })
	if err != nil || len(list) != 0 {
		t.Fatalf("processes.list on a busy engine = %+v, %v", list, err)
	}
}

// CREWS.CREATE FROM A PROPOSAL ANSWERS WHILE THE MANAGER IS MID-TURN AND THE
// HELPERS' CONVERSATIONS ARE STILL OPENING. Every helper is seated before it
// answers; their conversations open behind it.
func TestAppCrewFromAProposalAnswersBeforeItsHelpersOpen(t *testing.T) {
	hold := make(chan struct{})
	defer close(hold)
	conv := &proposingConversation{fakeConversation: &fakeConversation{hold: hold}, answer: helpersProposal}
	backend, rec, manager := proposingBackend(t, conv)
	if err := backend.CrewPropose(manager.ID); err != nil {
		t.Fatal(err)
	}
	at := rec.waitFor(t, 5*time.Second, appAskEvent(appwire.EventAskOpened))
	rec.mu.Lock()
	proposal := rec.data[at].(appwire.AskOpened).Ask.Proposal.ID
	rec.mu.Unlock()
	// The manager is mid-turn: its first message is still being answered.
	if _, err := backend.Send(manager.ID, "build the thing", ""); err != nil {
		t.Fatal(err)
	}
	helperRoot := t.TempDir()
	var minted atomic.Int64
	backend.mint = func(string) (string, error) {
		return filepath.Join(helperRoot, fmt.Sprint(minted.Add(1)), "transcript.jsonl"), nil
	}
	release := make(chan struct{})
	defer close(release)
	backend.open = func(ask appOpenAsk) (*appConn, error) {
		<-release
		return &appConn{Conv: &fakeConversation{}, Workspace: ask.Workspace, Transcript: ask.Transcript, Release: func() {}}, nil
	}
	var helpers []appwire.ProposalHelper
	for _, handle := range []string{"pipelines", "statsgen"} {
		helpers = append(helpers, appwire.ProposalHelper{Handle: handle, Brief: "Own the " + handle + " assignment", ReportsTo: "manager"})
	}
	var crew appwire.Crew
	var err error
	answersWithin(t, 3*time.Second, "crews.create", func() {
		crew, err = backend.CrewCreate(appwire.CrewCreate{From: "proposal", Proposal: proposal, Edits: &appwire.Proposal{Helpers: helpers}})
	})
	if err != nil {
		t.Fatal(err)
	}
	seated := map[string]bool{}
	for _, member := range crew.Members {
		seated[member.Handle] = member.Agent != manager.ID
	}
	if !seated["pipelines"] || !seated["statsgen"] || crew.Manager != manager.ID {
		t.Fatalf("crews.create answered %+v; want both helpers seated under the manager", crew.Members)
	}
	if !strings.Contains(fmt.Sprint(crew.Members), "statsgen") {
		t.Fatalf("members = %+v", crew.Members)
	}
}
