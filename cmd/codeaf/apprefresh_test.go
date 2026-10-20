package main

import (
	"github.com/Agent-Field/codeaf/internal/modelsource"
	"github.com/Agent-Field/codeaf/internal/service"
	"github.com/Agent-Field/codeaf/internal/service/servicetest"
	"github.com/Agent-Field/codeaf/internal/session"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/automation"
	teamstore "github.com/Agent-Field/codeaf/internal/teams"
)

func TestAppCrewRefreshNoticesChangedInputs(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	backend, rec, _ := workBackend(t, &fakeConversation{})
	backend.profileDir = t.TempDir()
	const id = "aaaaaaaaaaaa"
	if err := teamstore.Save(backend.profileDir, []teamstore.Team{{ID: id, Name: "before"}}); err != nil {
		t.Fatal(err)
	}
	run := &appCrewRun{prints: map[string]string{}, members: map[string]string{}, cursors: map[string]string{}, acted: map[string]bool{}, packets: map[string]teamstore.Packet{}, asks: map[string]appwire.Ask{}, held: map[string][]string{}, opening: map[string]bool{}}
	backend.crewTick(run, true)
	backend.crewRefresh(run)
	if backend.crewRefresh(run) {
		t.Fatal("quiet crew rebuilt")
	}
	if err := teamstore.Save(backend.profileDir, []teamstore.Team{{ID: id, Name: "after"}}); err != nil {
		t.Fatal(err)
	}
	if !backend.crewRefresh(run) {
		t.Fatal("changed teams file was missed")
	}
	if backend.crewRefresh(run) {
		t.Fatal("changed view did not settle")
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	found := false
	for i, event := range rec.events {
		if event == appwire.EventCrewChanged {
			view := rec.data[i].(appwire.CrewChanged)
			found = found || view.Crew.Name == "after"
		}
	}
	if !found {
		t.Fatal("updated crew was not emitted")
	}
}

func TestAppOrdersRefreshOnlyReadsChangedStore(t *testing.T) {
	backend, rec, store := ordersBackend(t)
	a := proposedOrder(t, store)
	stamp := ""
	seen, open := map[string]string{}, map[int64]appwire.Ask{}
	cursor, changed := backend.ordersRefresh(store, &stamp, 0, seen, open)
	if !changed {
		t.Fatal("first input was skipped")
	}
	if _, changed = backend.ordersRefresh(store, &stamp, cursor, seen, open); changed {
		t.Fatal("unchanged store was read")
	}
	other, err := automation.Open(store.Root())
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	a.Title = "changed from another connection"
	if _, err := other.Update(a); err != nil {
		t.Fatal(err)
	}
	cursor, changed = backend.ordersRefresh(store, &stamp, cursor, seen, open)
	if !changed {
		t.Fatal("external WAL write was missed")
	}
	// A revision with no visible difference is still a changed input, but must
	// not send another serialized view to the client.
	rec.mu.Lock()
	count := len(rec.events)
	rec.mu.Unlock()
	if _, err := other.Update(a); err != nil {
		t.Fatal(err)
	}
	cursor, _ = backend.ordersRefresh(store, &stamp, cursor, seen, open)
	rec.mu.Lock()
	if len(rec.events) != count {
		t.Error("identical serialized order emitted again")
	}
	rec.mu.Unlock()
	if _, changed := backend.ordersRefresh(store, &stamp, cursor, seen, open); changed {
		t.Fatal("quiet database was read again")
	}
}

func TestAppCrewLayerReloadsExternalReplacement(t *testing.T) {
	path := filepath.Join(t.TempDir(), "crews.json")
	layer := &appCrewLayer{path: path}
	if err := layer.read(func(*appCrewLayerFile) {}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"seq":42}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := layer.read(func(file *appCrewLayerFile) {
		if file.Seq != 42 {
			t.Errorf("external layer read as %+v", file)
		}
	}); err != nil {
		t.Fatal(err)
	}
}

func TestAppCrewRefreshAdmitsANewLiveStageWithoutViewChanges(t *testing.T) {
	b, rec, _, opens := lazyBackend(t, 1)
	dir := filepath.Join(t.TempDir(), "90be26ee38c80e56")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	transcript := filepath.Join(dir, "transcript.jsonl")
	sources := modelsource.NewSet(modelsource.Connected{Source: modelsource.DefaultSource("http://127.0.0.1:1"), Key: "test-key", Address: "http://127.0.0.1:1"})
	stage, err := session.New(session.Config{Workspace: t.TempDir(), SessionFile: transcript, Model: "openai/gpt-4.1-mini", Sources: sources, System: "SYSTEM"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = stage.Close() })
	id := appAgentID(transcript)
	b.remember(session.SessionRow{ID: id, Dir: filepath.Dir(transcript), Transcript: transcript})
	run := &appCrewRun{prints: map[string]string{}, members: map[string]string{}, cursors: map[string]string{}, acted: map[string]bool{}, packets: map[string]teamstore.Packet{}, asks: map[string]appwire.Ask{}, held: map[string][]string{}, opening: map[string]bool{}, wakeStamps: map[string]string{}}
	b.crewTick(run, true)
	b.crewRefresh(run)
	if opens.Load() != 0 || b.crewRefresh(run) {
		t.Fatal("quiet indexed records did not settle")
	}
	before := b.crewInputs(run)
	rememberStageAgent(stage)
	defer forgetStageAgent(stage)
	if err := os.WriteFile(filepath.Join(filepath.Dir(transcript), "presence.json"), []byte(`{"schema":1,"state":"waiting on you","pid":98765}`), 0600); err != nil {
		t.Fatal(err)
	}
	if b.crewInputs(run) != before {
		t.Fatal("fixture changed a crew-view input")
	}
	b.crewRefresh(run)
	opening := false
	run.locked(func() { opening = run.opening[id] })
	if !opening && !b.isHeld(id) {
		t.Fatal("unchanged view skipped live-stage admission")
	}
	rec.waitFor(t, 5*time.Second, func(_ string, data any) bool {
		changed, ok := data.(appwire.AgentChanged)
		return ok && changed.Agent.ID == id
	})
	live := b.liveOf(mustFind(t, b, id))
	conv := live.conn.Conv.(*fakeConversation)
	conv.raise(session.Event{Kind: session.EventQuestion, Question: &session.Question{Kind: session.QuestionConsent, ID: 77, Head: "May I continue?", Options: session.AnswerOptions(session.QuestionConsent)}})
	rec.waitFor(t, 5*time.Second, func(_ string, data any) bool {
		opened, ok := data.(appwire.AskOpened)
		return ok && opened.Ask.Agent == id
	})
	if got := opens.Load(); got != 1 {
		t.Fatalf("live stage opened %d agents, want one", got)
	}
}

func TestAppCrewRefreshAdmitsAKeptServiceWithoutViewChanges(t *testing.T) {
	b, rec, paths, opens := lazyBackend(t, 1)
	w := &svcWorld{backend: b, rt: servicetest.New(), clock: servicetest.NewClock(time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)), dir: t.TempDir(), work: t.TempDir()}
	w.open(t)
	run := &appCrewRun{prints: map[string]string{}, members: map[string]string{}, cursors: map[string]string{}, acted: map[string]bool{}, packets: map[string]teamstore.Packet{}, asks: map[string]appwire.Ask{}, held: map[string][]string{}, opening: map[string]bool{}, wakeStamps: map[string]string{}}
	b.crewTick(run, true)
	b.crewRefresh(run)
	if opens.Load() != 0 || b.crewRefresh(run) {
		t.Fatal("quiet crew did not settle")
	}
	before := b.crewInputs(run)
	id := appAgentID(paths[0])
	w.keep(t, id, service.PolicyNever)
	if b.crewInputs(run) != before {
		t.Fatal("fixture changed a crew-view input")
	}
	b.crewRefresh(run)
	opening := false
	run.locked(func() { opening = run.opening[id] })
	if !opening && !b.isHeld(id) {
		t.Fatal("unchanged view skipped kept-service admission")
	}
	rec.waitFor(t, 5*time.Second, func(_ string, data any) bool {
		changed, ok := data.(appwire.AgentChanged)
		return ok && changed.Agent.ID == id
	})
	if got := opens.Load(); got != 1 {
		t.Fatalf("service opened %d agents, want one", got)
	}
}
