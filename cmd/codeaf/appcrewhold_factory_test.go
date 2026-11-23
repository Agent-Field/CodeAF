package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/modelsource"
	"github.com/Agent-Field/codeaf/internal/modelsource/sourcestub"
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
	teamstore "github.com/Agent-Field/codeaf/internal/teams"
)

// lockedJournalCrew is a job crew whose one working member's journal is held
// by a session agent in this process. The agent is not registered as a factory
// stage, so the crew hold is not lent the conversation: it loses the journal
// lock, which is what Give back looks like while the runner drives the step.
type lockedJournalCrew struct {
	handle       string
	name         string
	factoryStage bool
}

// journalLockCrew is that crew, looked at by the app, with the runner still
// holding the member's journal until letGo.
type journalLockCrew struct {
	backend *appBackend
	rec     *appRecorder
	run     *appCrewRun
	crew    string
	lead    string
	member  string
	handle  string
	runner  *session.Agent
	holds   atomic.Bool
	opens   atomic.Int32
	door    atomic.Value // string, the opener's error while the runner holds
	warned  *[]string
	warnMu  *sync.Mutex
}

// A FACTORY STEP THIS PROCESS'S RUNNER HOLDS IS NOT ANOTHER WINDOW. After Give
// back the runner drives the step again and keeps its journal locked. The crew
// hold loses that lock. The step's questions reach the person through the
// factory, so the hold is not said, the row does not read as unheld, and when
// the runner lets go the next try holds the step without saying it is open
// here again.
func TestAFactoryStepTheRunnerHoldsIsNotAnotherWindow(t *testing.T) {
	f := newJournalLockCrew(t, lockedJournalCrew{
		handle: "write2", name: "#21 · add the word hello", factoryStage: true,
	})
	f.waitForTheLock(t)
	f.look(t)
	if said := f.windowWords(); said != "" {
		t.Fatalf("a step this process's runner holds was called another window: %s", said)
	}
	f.letGo(t)
	f.holdOnceTheJournalIsFree(t)
	f.look(t)
	if said := f.windowWords(); said != "" {
		t.Fatalf("letting the runner go was said: %s", said)
	}
}

// AN ORDINARY MEMBER LOCKED BY ANOTHER WINDOW KEEPS TODAY'S WORDS. The same
// journal lock on a member that is not a factory step is still said once, the
// row reads unheld, and the recovery is said once the lock lets go.
func TestAnOrdinaryCrewMemberLockedByAnotherWindowIsStillSaid(t *testing.T) {
	f := newJournalLockCrew(t, lockedJournalCrew{
		handle: "verifier", name: "Trudeau Watch", factoryStage: false,
	})
	f.waitForTheLock(t)
	f.look(t)
	said := f.windowWords()
	who := "@" + f.handle
	if !strings.Contains(said, who+" could not be opened here") || !strings.Contains(said, appWindowConflict) || !strings.Contains(said, appUnheldLead) {
		t.Fatalf("the other window was not said: %s", said)
	}
	if strings.Count(said, "could not be opened") != 2 {
		t.Fatalf("the failure was not said once as a notice and a warn: %s", said)
	}
	f.letGo(t)
	f.holdOnceTheJournalIsFree(t)
	f.look(t)
	said = f.windowWords()
	if !strings.Contains(said, who+" is open here again") {
		t.Fatalf("the recovery was not said: %s", said)
	}
}

// newJournalLockCrew puts one crew on disk and opens its member's journal in
// this process, then starts the crew hold. The opener is the app's own door
// while the runner holds the file, and a stand-in conversation once it lets go.
func newJournalLockCrew(t *testing.T, spec lockedJournalCrew) *journalLockCrew {
	t.Helper()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("CODEAF_HOME", filepath.Join(homeDir, "state"))
	profile := filepath.Join(homeDir, "profile")
	t.Setenv(config.ProfileDirEnv, profile)

	bucket, err := v3ProjectDir(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	dir := writeV3Session(t, bucket, "0000000000000212", "the step", time.Now())
	runner := openRunnerJournal(t, session.Place{Dir: dir}.Transcript())
	transcript := runner.SessionPath()
	if !session.InUse(transcript) {
		t.Fatal("the runner's journal was not in use")
	}
	tasks := session.Place{Dir: session.DirOf(transcript)}.Tasks()
	if err := os.MkdirAll(filepath.Dir(tasks), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tasks, []byte(`{"type":"tasks","version":1,"seq":1,"nodes":[{"id":1,"title":"parked work","brief":"carry on","kind":"quick","state":"queued"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}

	f := &journalLockCrew{member: appAgentID(transcript), handle: spec.handle, runner: runner}
	f.holds.Store(true)
	var warned []string
	var warnMu sync.Mutex
	f.warned, f.warnMu = &warned, &warnMu
	was := appCrewWarn
	appCrewWarn = func(format string, args ...any) {
		warnMu.Lock()
		defer warnMu.Unlock()
		warned = append(warned, fmt.Sprintf(format, args...))
	}
	t.Cleanup(func() { appCrewWarn = was })

	memberConv := &fakeConversation{}
	turns := make(chan remote.Following, 1)
	lead := &fakeConversation{}
	f.rec = newAppRecorder()
	f.backend = newFakeBackend(f.rec, func(ask appOpenAsk) (*appConn, error) {
		if appAgentID(ask.Transcript) != f.member {
			return &appConn{Conv: lead, Workspace: ask.Workspace, Transcript: appFakeTranscript, Release: func() {}}, nil
		}
		f.opens.Add(1)
		if f.holds.Load() {
			_, err := appOpenInProcess(ask)
			if err == nil {
				err = errors.New("the door opened a journal the runner holds")
			}
			f.door.Store(err.Error())
			return nil, err
		}
		return &appConn{
			Conv: memberConv, Workspace: ask.Workspace, Transcript: ask.Transcript, Release: func() {},
			Follow: func() <-chan remote.Following { return turns },
		}, nil
	})
	t.Cleanup(func() { f.backend.Close() })
	f.backend.state = &appState{path: filepath.Join(t.TempDir(), appStateName)}
	f.backend.profileDir = profile

	manager, err := mustCreate(t, f.backend, appwire.CreateAsk{Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	f.lead = manager.ID
	f.crew = teamstore.NewID()
	err = teamstore.Update(profile, func(file *teamstore.File) error {
		file.Teams = append(file.Teams, teamstore.Team{ID: f.crew, Name: spec.name, Made: time.Now()})
		if err := file.AddMember(f.crew, teamstore.Member{Key: appFakeTranscript, File: appFakeTranscript, Where: "/w", Word: "lead", Handle: "lead"}); err != nil {
			return err
		}
		step := teamstore.Member{Key: transcript, File: transcript, Where: "/w", Word: "Write the change", Handle: spec.handle, Started: true, FactoryStage: spec.factoryStage}
		if err := file.AddMember(f.crew, step); err != nil {
			return err
		}
		return file.SetManager(f.crew, appFakeTranscript)
	})
	if err != nil {
		t.Fatal(err)
	}
	f.run = f.backend.crews()
	return f
}

// openRunnerJournal is the factory runner: a real agent holding the step's
// journal. It is not remembered as a stage, so the crew cannot be lent it.
func openRunnerJournal(t *testing.T, transcript string) *session.Agent {
	t.Helper()
	server := sourcestub.New("deepseek/deepseek-v4-flash")
	t.Cleanup(server.Close)
	sources := modelsource.NewSet(modelsource.Connected{
		Source: modelsource.DefaultSource(server.URL()), Key: "test-key", Address: server.URL(),
	})
	cfg := session.Config{
		Workspace: t.TempDir(), Model: "deepseek/deepseek-v4-flash", Sources: sources, SessionFile: transcript,
	}
	agent, err := session.New(cfg)
	if err != nil {
		t.Fatalf("the runner could not open the step: %v", err)
	}
	t.Cleanup(func() { _ = agent.Close() })
	if _, err := session.New(cfg); !errors.Is(err, session.ErrSessionLocked) {
		t.Fatalf("a second open of the runner's journal = %v, want the session lock", err)
	}
	return agent
}

// waitForTheLock waits until the crew hold has lost the journal to the runner
// and the door has called that another window.
func (f *journalLockCrew) waitForTheLock(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for f.opens.Load() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the crew never tried to open the member")
		}
		time.Sleep(10 * time.Millisecond)
	}
	// The hold records the failure just after the door returns.
	time.Sleep(100 * time.Millisecond)
	got, _ := f.door.Load().(string)
	if !strings.Contains(got, appWindowConflict) {
		t.Fatalf("the door's refusal = %q, want the other-window conflict", got)
	}
}

// look is the crew's next look, the one that says a hold the first look only recorded.
func (f *journalLockCrew) look(t *testing.T) {
	t.Helper()
	f.backend.crewTick(f.run, false)
}

// letGo is the runner leaving the journal, as a step finishing does.
func (f *journalLockCrew) letGo(t *testing.T) {
	t.Helper()
	f.holds.Store(false)
	if err := f.runner.Close(); err != nil {
		t.Fatal(err)
	}
}

// holdOnceTheJournalIsFree is the person opening the crew, which tries the
// member again now, and waits until that try holds it.
func (f *journalLockCrew) holdOnceTheJournalIsFree(t *testing.T) {
	t.Helper()
	if _, _, err := f.backend.CrewLog(f.crew, "", 0); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for !f.backend.isHeld(f.member) {
		if time.Now().After(deadline) {
			t.Fatal("the member was not held once its journal was free")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// windowWords is everything the crew said about this member's hold: the
// manager's notice, the warn log, stderr, and the row's unheld line.
func (f *journalLockCrew) windowWords() string {
	var b strings.Builder
	f.rec.mu.Lock()
	for _, data := range f.rec.data {
		switch v := data.(type) {
		case appwire.EntryAdded:
			if v.Entry.Kind == appwire.EntryNotice && (strings.Contains(v.Entry.Text, "could not be opened") || strings.Contains(v.Entry.Text, "is open here again")) {
				b.WriteString(v.Entry.Text)
				b.WriteByte('\n')
			}
		case appwire.Log:
			if v.Level == "warn" && strings.Contains(v.Message, "@"+f.handle) {
				b.WriteString(v.Message)
				b.WriteByte('\n')
			}
		case appwire.MemberChanged:
			if v.Member.Agent == f.member && strings.Contains(v.Member.Line, appUnheldLead) {
				b.WriteString(v.Member.Line)
				b.WriteByte('\n')
			}
		}
	}
	f.rec.mu.Unlock()
	f.warnMu.Lock()
	for _, line := range *f.warned {
		if strings.Contains(line, f.member) {
			b.WriteString(line)
			b.WriteByte('\n')
		}
	}
	f.warnMu.Unlock()
	return b.String()
}
