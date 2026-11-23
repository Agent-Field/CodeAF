package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
	teamstore "github.com/Agent-Field/codeaf/internal/teams"
)

func lazyBackend(t *testing.T, count int) (*appBackend, *appRecorder, []string, *atomic.Int32) {
	t.Helper()
	t.Setenv("CODEAF_HOME", filepath.Join(t.TempDir(), "state"))
	bucket, err := v3ProjectDir(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for i := 0; i < count; i++ {
		dir := writeV3Session(t, bucket, fmt.Sprintf("%016x", i+1), "hello", time.Now())
		paths = append(paths, session.Place{Dir: dir}.Transcript())
	}
	opens := new(atomic.Int32)
	rec := newAppRecorder()
	b := newFakeBackend(rec, func(ask appOpenAsk) (*appConn, error) {
		opens.Add(1)
		return &appConn{Conv: &fakeConversation{}, Workspace: ask.Workspace, Transcript: ask.Transcript, UserMessages: 1, Release: func() {}}, nil
	})
	b.profileDir = t.TempDir()
	t.Cleanup(func() { b.Close() })
	if err := teamstore.Update(b.profileDir, func(f *teamstore.File) error {
		id := teamstore.NewID()
		f.Teams = append(f.Teams, teamstore.Team{ID: id, Name: "lazy", Made: time.Now()})
		for i, path := range paths {
			if err := f.AddMember(id, teamstore.Member{Key: path, File: path, Handle: fmt.Sprintf("member%d", i), Where: "/w"}); err != nil {
				return err
			}
		}
		return f.SetManager(id, paths[0])
	}); err != nil {
		t.Fatal(err)
	}
	return b, rec, paths, opens
}

func TestAppLazyIdleStartupAndConcurrentFirstView(t *testing.T) {
	b, _, paths, opens := lazyBackend(t, 24)
	run := b.crews()
	b.crewTick(run, false)
	if got := opens.Load(); got != 0 {
		t.Fatalf("idle startup opened %d agents", got)
	}
	agents, err := b.ListAgents("")
	if err != nil || len(agents) != 24 {
		t.Fatalf("light records: %d, %v", len(agents), err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	opener := b.open
	b.open = func(ask appOpenAsk) (*appConn, error) { close(entered); <-release; return opener(ask) }
	var wg sync.WaitGroup
	errs := make(chan error, 24)
	for range 24 {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := b.OpenAgent(appAgentID(paths[1])); errs <- err }()
	}
	<-entered
	close(release)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if got := opens.Load(); got != 1 {
		t.Fatalf("concurrent first view assembled %d times", got)
	}
	if _, err := b.OpenAgent(appAgentID(paths[1])); err != nil {
		t.Fatal(err)
	}
	if got := opens.Load(); got != 1 {
		t.Fatalf("repeat view assembled %d times", got)
	}
}

func TestAppLazyStartupOpensOnlyRecoveryWork(t *testing.T) {
	b, rec, paths, opens := lazyBackend(t, 12)
	if err := os.WriteFile(filepath.Join(filepath.Dir(paths[3]), "tasks.json"), []byte(`{"type":"tasks","version":1,"seq":1,"nodes":[{"id":1,"title":"parked work","brief":"carry on","kind":"quick","state":"queued"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	// This agent is outside a crew; its pending question still needs a holder.
	if err := teamstore.Update(b.profileDir, func(f *teamstore.File) error { f.Teams = nil; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(paths[7]), "presence.json"), []byte(`{"schema":1,"state":"waiting on you","pid":98765}`), 0o600); err != nil {
		t.Fatal(err)
	}
	b.crews()
	for _, path := range []string{paths[3], paths[7]} {
		id := appAgentID(path)
		rec.waitFor(t, 5*time.Second, func(_ string, data any) bool {
			changed, ok := data.(appwire.AgentChanged)
			return ok && changed.Agent.ID == id
		})
	}
	if got := opens.Load(); got != 2 {
		t.Fatalf("startup opened %d agents, want only two with work", got)
	}
}

func TestAppLazyTrafficOpensAnUnopenedMember(t *testing.T) {
	b, rec, paths, opens := lazyBackend(t, 3)
	var boundary time.Time
	opener := b.open
	b.open = func(ask appOpenAsk) (*appConn, error) { boundary = ask.TeamListeningSince; return opener(ask) }
	run := b.crews()
	if opens.Load() != 0 {
		t.Fatal("crew membership opened an idle agent")
	}
	file, err := teamstore.Load(b.profileDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := teamstore.AppendTraffic(b.profileDir, file.Teams[0].ID, teamstore.Entry{Kind: teamstore.KindDirective, From: teamstore.FromManager, To: "member1", Text: "read the new work"}); err != nil {
		t.Fatal(err)
	}
	// No explicit tick: the subscription itself must observe the disk write.
	rec.waitFor(t, 5*time.Second, func(_ string, data any) bool {
		changed, ok := data.(appwire.AgentChanged)
		return ok && changed.Agent.ID == appAgentID(paths[1])
	})
	if boundary.IsZero() || !boundary.Equal(run.since) {
		t.Fatalf("open lost subscription boundary: %v, want %v", boundary, run.since)
	}
	if got := opens.Load(); got != 1 {
		t.Fatalf("traffic opened %d agents", got)
	}
	if err := teamstore.AppendTraffic(b.profileDir, file.Teams[0].ID, teamstore.Entry{Kind: teamstore.KindNote, From: teamstore.FromManager, To: "member2", Text: "information for later"}); err != nil {
		t.Fatal(err)
	}
	rec.waitFor(t, 5*time.Second, func(_ string, data any) bool {
		changed, ok := data.(appwire.AgentChanged)
		return ok && changed.Agent.ID == appAgentID(paths[2])
	})
	if got := opens.Load(); got != 2 {
		t.Fatalf("addressed note opened %d agents, want two in total", got)
	}

}

func TestAppLazyStartupLeavesCrashAdmissionToRecovery(t *testing.T) {
	r := newCloseRig(t, session.PresenceWorking, session.PresenceWindow+time.Second)
	r.b.snapshotCloseClaims()
	run := r.b.crews()
	opening := false
	run.locked(func() { opening = run.opening[r.id] })
	if opening || r.b.isHeld(r.id) {
		t.Fatal("automatic open bypassed crash admission")
	}
	r.b.startCloseWatch()
	r.clock.advance(t, 0)
	resumed, _ := r.conv.counts()
	if resumed != 1 {
		t.Fatalf("recovery resumed %d times, want 1", resumed)
	}
	run.locked(func() { opening = run.recovering[r.id] })
	if opening {
		t.Fatal("completed recovery still blocks future traffic")
	}
}

func TestAppLazyDoesNotReopenWorkRecoveryRefused(t *testing.T) {
	r := newCloseRig(t, session.PresenceWorking, session.PresenceWindow+time.Second)
	_ = r.b.state.write(func(f *appStateFile) error { f.CloseResumes = map[string]int{r.id: 2}; return nil })
	r.b.snapshotCloseClaims()
	run := r.b.crews()
	r.b.startCloseWatch()
	r.clock.advance(t, 0)
	r.b.crewTick(run, false)
	opening := false
	run.locked(func() { opening = run.opening[r.id] })
	if opening || r.b.isHeld(r.id) {
		t.Fatal("a lazy open undid the crash recovery refusal")
	}
}

// A bad identity must be refused before opening can wake any saved work.
func TestAppLazyInvalidClientIDDoesNotOpen(t *testing.T) {
	for _, door := range []string{"create", "send", "message", "queue", "steer"} {
		t.Run(door, func(t *testing.T) {
			b, _, paths, opens := lazyBackend(t, 1)
			id := appAgentID(paths[0])
			clientID := strings.Repeat("x", session.MaxClientIDBytes+1)
			var err error
			switch door {
			case "create":
				_, err = b.CreateAgent(appwire.CreateAsk{Workspace: t.TempDir(), Text: "words", ClientID: clientID})
			case "send":
				_, err = b.Send(id, "words", clientID)
			case "message":
				_, err = b.SendMessage(id, appwire.AgentMessage{Text: "words", ClientID: clientID})
			case "queue":
				_, err = b.Queue(id, "words", clientID)
			case "steer":
				err = b.Steer(id, "words", clientID)
			}
			if wireCode(err) != appwire.CodeBadParams || opens.Load() != 0 {
				t.Fatalf("refused with %v after %d opens", err, opens.Load())
			}
		})
	}
}

func TestAppLazyCrewReholdsAnOpenedMemberWithoutAWorkStampChange(t *testing.T) {
	b, rec, paths, opens := lazyBackend(t, 2)
	run := b.crews()
	id := appAgentID(paths[0])
	if _, err := b.OpenAgent(id); err != nil {
		t.Fatal(err)
	}
	// A dead engine leaves no new checkpoint or presence in this fixture.
	// Its established crew hold must still reopen it on the next look.
	a := mustFind(t, b, id)
	live := b.liveOf(a)
	b.died(a, live)
	b.crewTick(run, false)
	rec.waitFor(t, 5*time.Second, func(_ string, data any) bool {
		changed, ok := data.(appwire.AgentChanged)
		return ok && changed.Agent.ID == id && b.isHeld(id)
	})
	if got := opens.Load(); got != 2 {
		t.Fatalf("opened %d agents, want the same member twice", got)
	}
}

type lazyReadEngine struct {
	*renderConversation
}

func (e *lazyReadEngine) ProcessPage(ctx context.Context, ask session.ProcessPageAsk) (session.ProcessPage, error) {
	return crewEnginePager{e.fakeConversation}.ProcessPage(ctx, ask)
}

func (e *lazyReadEngine) SpendRail() (float64, error) { return 7, nil }
func (e *lazyReadEngine) ApplicableMemory(context.Context) (session.MemoryScopeSnapshot, error) {
	return session.MemoryScopeSnapshot{Available: true, Basis: "configured_live"}, nil
}

func TestAppLazyCrewReadsKeepTheOwnersAnswers(t *testing.T) {
	for _, door := range []string{"budget", "render.list", "render.get", "render.cancel", "memory.applies", "process.page", "crew.process.page"} {
		t.Run(door, func(t *testing.T) {
			b, _, paths, opens := lazyBackend(t, 1)
			b.crews()
			render := session.RenderRecord{ID: "rnd_live", Kind: "video", State: session.RenderRunning}
			engine := &lazyReadEngine{renderConversation: &renderConversation{fakeConversation: &fakeConversation{}, page: session.RenderPage{Renders: []session.RenderRecord{render}}, records: map[string]session.RenderRecord{render.ID: render}}}
			opener := b.open
			b.open = func(ask appOpenAsk) (*appConn, error) {
				conn, err := opener(ask)
				if err == nil {
					conn.Conv = engine
				}
				return conn, err
			}
			id := appAgentID(paths[0])
			switch door {
			case "budget":
				got, err := b.AgentBudget(id)
				if err != nil || got.Engine == nil || got.USD == nil || *got.USD != 7 {
					t.Fatalf("owner budget: %+v %v", got, err)
				}
			case "render.list":
				got, err := b.ListRenders(id, "", 10)
				if err != nil || len(got.Renders) != 1 || got.Renders[0].ID != render.ID {
					t.Fatalf("owner renders: %+v %v", got, err)
				}
			case "render.get":
				got, err := b.GetRender(id, render.ID)
				if err != nil || got.ID != render.ID {
					t.Fatalf("owner render: %+v %v", got, err)
				}
			case "render.cancel":
				got, err := b.CancelRender(id, render.ID)
				if err != nil || got.Outcome != "cancelled" || len(engine.cancelled) != 1 {
					t.Fatalf("owner cancel: %+v %v", got, err)
				}
			case "process.page", "crew.process.page":
				var got *appwire.ProcessPageResult
				var err error
				if door == "process.page" {
					got, err = b.PageProcesses(t.Context(), appwire.ProcessPageAsk{Agent: id})
				} else {
					bucket := filepath.Base(filepath.Dir(filepath.Dir(paths[0])))
					got, err = b.crewOwnerPage(t.Context(), crewOwner{id: id, bucket: bucket, file: paths[0]}, 10)
				}
				if err != nil || len(got.Processes) != 1 || got.Processes[0].State != appwire.ProcessRunning {
					t.Fatalf("owner process page: %+v %v", got, err)
				}
			case "memory.applies":
				got, err := b.ApplicableMemory(t.Context(), id)
				if err != nil || !got.Available || got.Basis != "configured_live" {
					t.Fatalf("owner memory scope: %+v %v", got, err)
				}
			}
			if got := opens.Load(); got != 1 {
				t.Fatalf("read opened %d owners, want one", got)
			}
		})
	}
}
