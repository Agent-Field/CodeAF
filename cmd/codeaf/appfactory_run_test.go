package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// ── the list: only this engine's own row is ever forgotten ─────────────────

// A SOLO ROW IS NOT THIS ENGINE'S BECAUSE IT SAYS SOLO. Only the row marked
// local is stale or ended by this engine; every address a person attached,
// solo or not, and the session held for it, survives a start and a quit.
func TestLocalFactoryForgetsOnlyItsOwnMarkedRows(t *testing.T) {
	useLocalHome(t)
	hub := testFactoryHub(t)
	remoteSolo := appFactoryRow{ID: appFactoryID("https://solo.example"), URL: "https://solo.example", Name: "Solo elsewhere", Mode: "solo"}
	remoteTeam := appFactoryRow{ID: appFactoryID("https://team.example"), URL: "https://team.example", Name: "Team", Mode: "team"}
	loopbackSolo := appFactoryRow{ID: appFactoryID("http://127.0.0.1:9"), URL: "http://127.0.0.1:9", Name: "Attached by hand", Mode: "solo"}
	stale := appFactoryRow{ID: appFactoryID("http://127.0.0.1:7"), URL: "http://127.0.0.1:7", Name: appLocalName, Mode: "solo", Local: true}
	hub.mu.Lock()
	hub.rows = []appFactoryRow{remoteSolo, remoteTeam, loopbackSolo, stale}
	hub.loaded = true
	err := hub.save()
	hub.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	hub.hold(remoteSolo.ID, "solo-session", 0)
	hub.hold(loopbackSolo.ID, "loopback-session", 0)
	hub.hold(stale.ID, "stale-session", 0)

	w := newFactoryWire(t, hub)
	offlineLocal(w.backend, &setupGitHub{}, t)
	var st appwire.FactoryLocalStatus
	w.must("factory.local_start", map[string]any{"caller": "w1"}, &st)

	rows := func() map[string]appFactoryRow {
		hub.mu.Lock()
		defer hub.mu.Unlock()
		out := map[string]appFactoryRow{}
		for _, r := range hub.rows {
			out[r.ID] = r
		}
		return out
	}
	got := rows()
	for _, kept := range []appFactoryRow{remoteSolo, remoteTeam, loopbackSolo} {
		if got[kept.ID] != kept {
			t.Errorf("start changed the row a person attached: %+v -> %+v", kept, got[kept.ID])
		}
	}
	if _, ok := got[stale.ID]; ok {
		t.Error("start kept the stale row this engine marked itself")
	}
	if own := got[st.Factory]; !own.Local || own.Mode != "solo" {
		t.Errorf("the local row is not marked: %+v", own)
	}
	hub.mu.Lock()
	solo, loop, gone := hub.tokenLocked(remoteSolo.ID), hub.tokenLocked(loopbackSolo.ID), hub.tokenLocked(stale.ID)
	hub.mu.Unlock()
	if solo != "solo-session" || loop != "loopback-session" {
		t.Error("start dropped a session held for an attached factory")
	}
	if gone != "" {
		t.Error("start kept the session of a stale local row")
	}

	w.backend.closeLocalFactory()
	got = rows()
	for _, kept := range []appFactoryRow{remoteSolo, remoteTeam, loopbackSolo} {
		if got[kept.ID] != kept {
			t.Errorf("quit changed the row a person attached: %+v -> %+v", kept, got[kept.ID])
		}
	}
	// The row this engine made for a service the person started stays, as the
	// record of that choice, without the bearer it held.
	if own, ok := got[st.Factory]; !ok || len(got) != 4 || !own.Local || !own.Resume {
		t.Errorf("quit left %d rows, own among them: %v", len(got), ok)
	}
	if hub.tokenFor(st.Factory) != "" {
		t.Error("quit kept the bearer of the local service")
	}
	hub.mu.Lock()
	solo = hub.tokenLocked(remoteSolo.ID)
	hub.mu.Unlock()
	if solo != "solo-session" {
		t.Error("quit dropped the session of an attached factory")
	}
}

// ONE ROW PER CONNECTED FACTORY. Attaching the address the app itself is
// serving, through the real door, is the local connection and nothing new: the
// id, the Resume choice, the engine's bearer and every remote row are as they
// were, and the quit keeps the row.
func TestAttachingTheLocalAddressIsTheLocalConnectionNotASecondOne(t *testing.T) {
	useLocalHome(t)
	hub := testFactoryHub(t)
	w := newFactoryWire(t, hub)
	offlineLocal(w.backend, &setupGitHub{}, t)
	remote := appFactoryRow{ID: appFactoryID("https://team.example"), URL: "https://team.example", Name: "Team", Mode: "team"}
	attached(t, hub, remote)
	var st appwire.FactoryLocalStatus
	w.must("factory.local_start", map[string]any{"caller": "w1"}, &st)
	if st.Factory != appLocalID {
		t.Fatalf("local id %q", st.Factory)
	}
	engineToken := hub.tokenFor(appLocalID)
	if engineToken == "" {
		t.Fatal("no bearer held for the local connection")
	}
	before, _ := hub.snapshot()

	var conn appwire.FactoryConnection
	w.must("factory.attach", map[string]any{"url": st.URL, "token": "someone-elses-token"}, &conn)
	if conn.ID != appLocalID {
		t.Fatalf("attach made connection %q, want %q", conn.ID, appLocalID)
	}
	after, _ := hub.snapshot()
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("attach changed the list:\n%+v\n%+v", before, after)
	}
	if hub.tokenFor(appLocalID) != engineToken {
		t.Fatal("attach replaced the engine's own bearer")
	}

	// The access-link and adoption doors resolve the address the same way.
	hub.mu.Lock()
	id := hub.idForLocked(st.URL)
	hub.mu.Unlock()
	if id != appLocalID {
		t.Fatalf("an address resolved to %q", id)
	}

	w.backend.closeLocalFactory()
	rows, _ := hub.snapshot()
	var own int
	for _, r := range rows {
		if r.ID == appLocalID && r.Local && r.Resume {
			own++
		}
	}
	if len(rows) != 2 || own != 1 || !reflect.DeepEqual(rows, after) {
		t.Fatalf("rows after quit: %+v", rows)
	}
	if hub.tokenFor(appLocalID) != "" {
		t.Fatal("the bearer outlived the service")
	}
}

// A person's explicit address is never a way to revoke hosting they chose, nor
// to adopt a service they did not start: a row that joined another codeaf, or
// that an earlier release left, stays Local and unmarked, so it resumes nothing.
func TestAttachingDoesNotMakeAnUnmarkedLocalRowResumeOrVanishOnQuit(t *testing.T) {
	hub := testFactoryHub(t)
	hub.mu.Lock()
	hub.rows, hub.loaded = []appFactoryRow{{ID: appLocalID, URL: "http://127.0.0.1:7", Name: appLocalName, Mode: "solo", Local: true}}, true
	hub.mu.Unlock()
	if err := hub.saveAttached(appFactoryRow{ID: appLocalID, URL: "http://127.0.0.1:7", Name: "x"}, true); err != nil {
		t.Fatal(err)
	}
	rows, _ := hub.snapshot()
	if len(rows) != 1 || !rows[0].Local || rows[0].Resume || rows[0].Name != appLocalName {
		t.Fatalf("rows: %+v", rows)
	}
}

// ── the runner: only an explicit run starts one ────────────────────────────

func runnerHeld(s *localSetup) bool {
	release, ok := s.backend.local.host.floor.TryWorker()
	if ok {
		release()
	}
	return !ok
}

func eventually(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("never: %s", what)
}

// THIS MAC IS THE RUNNER OF ITS OWN FACTORY FROM THE START, AND IT STARTS
// NOTHING. A factory that knows whose GitHub it is brings its runner on with no
// switch to find (one that does not is TestLocalRunNeedsGitHubConnected…), and
// setup all the way to done still starts no job.
func TestTheLocalRunnerIsOnFromTheStartAndStartsNothing(t *testing.T) {
	s := newLocalSetup(t)
	// Before start there is nothing to run.
	s.mustFail("factory.local_run", map[string]any{"caller": "w1"}, appwire.CodeNotFound)
	s.mustFail("factory.local_run", map[string]any{}, appwire.CodeBadParams)

	st := s.start()
	if !st.Runner {
		t.Fatalf("the start did not bring the runner on: %+v", st)
	}
	l := &s.backend.local
	eventually(t, "the runner holds the worker lock", func() bool { return runnerHeld(s) })
	s.setup("factory.setup_connect", map[string]any{"source": "token", "token": setupSecret})
	s.setup("factory.setup_select", map[string]any{"repos": []string{"acme/api"}})
	s.until("checkouts", func(v appwire.FactorySetupView) bool { return v.State == "checkouts" })
	s.setup("factory.setup_skip", map[string]any{"repo": "acme/api"})
	if v := s.setup("factory.setup_done", map[string]any{}); v.State != "done" {
		t.Fatalf("done: %+v", v)
	}
	// Give a job every chance to start that nobody pressed Run on.
	time.Sleep(1500 * time.Millisecond)
	var q api.Queue
	s.must("factory.queue", map[string]any{"factory": st.Factory}, &q)
	for _, j := range q.Jobs {
		if j.State != "backlog" {
			t.Fatalf("job %d is %q with nobody pressing Run", j.ID, j.State)
		}
	}

	var run appwire.FactoryLocalStatus
	s.must("factory.local_run", map[string]any{"caller": "w1"}, &run)
	if !run.Runner || run.State != appwire.LocalRunning || run.Factory != st.Factory {
		t.Fatalf("run: %+v", run)
	}
	// Run again is the same runner.
	l.mu.Lock()
	done := l.runnerDone
	l.mu.Unlock()
	var again appwire.FactoryLocalStatus
	s.must("factory.local_run", map[string]any{"caller": "w2"}, &again)
	l.mu.Lock()
	same := l.runnerDone == done
	l.mu.Unlock()
	if !again.Runner || !same {
		t.Fatalf("second run started another: %+v", again)
	}
	var status appwire.FactoryLocalStatus
	s.must("factory.local_status", map[string]any{"caller": "w1"}, &status)
	if !status.Runner {
		t.Fatalf("status lost the runner: %+v", status)
	}

	// Quitting stops it, bounded, and the lock goes with it.
	host := l.host
	started2 := time.Now()
	s.backend.closeLocalFactory()
	if took := time.Since(started2); took > appLocalStopWait {
		t.Fatalf("quit took %s", took)
	}
	select {
	case <-done:
	default:
		t.Fatal("quit left the runner's loop running")
	}
	if release, ok := host.floor.TryWorker(); !ok {
		t.Fatal("quit left the worker lock held")
	} else {
		release()
	}
	s.mustFail("factory.local_run", map[string]any{"caller": "w1"}, appwire.CodeClosed)
}

// A RUNNER THE FACTORY WOULD REFUSE IS NOT STARTED. With no GitHub login the
// factory takes no worker (it needs a handle), so the start brings none on, and
// a run asked for says so and starts nothing, instead of retrying in silence
// for as long as the engine lives.
func TestLocalRunNeedsGitHubConnectedAndStartsNothingWithout(t *testing.T) {
	useLocalHome(t)
	w := newFactoryWire(t, testFactoryHub(t))
	w.must("factory.local_start", map[string]any{"caller": "w1"}, &appwire.FactoryLocalStatus{})
	msg := w.mustFail("factory.local_run", map[string]any{"caller": "w1"}, appwire.CodeConflict)
	if !strings.Contains(msg, "connect GitHub") {
		t.Fatalf("the refusal does not say what to do: %q", msg)
	}
	l := &w.backend.local
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.runner || l.runnerDone != nil {
		t.Fatal("a refused run left a runner")
	}
}

// ANOTHER CODEAF THAT SERVES THE FACTORY KEEPS ITS OWN RUNNER: this engine
// opens no second authority and is refused a runner of its own.
func TestLocalRunIsRefusedWhenAnotherCodeafServes(t *testing.T) {
	useLocalHome(t)
	a := newFactoryWire(t, testFactoryHub(t))
	offlineLocal(a.backend, &setupGitHub{}, t)
	a.must("factory.local_start", map[string]any{"caller": "a"}, &appwire.FactoryLocalStatus{})
	b := newFactoryWire(t, testFactoryHub(t))
	var second appwire.FactoryLocalStatus
	b.must("factory.local_start", map[string]any{"caller": "b"}, &second)
	if second.Authority != appwire.LocalAuthorityOther {
		t.Fatalf("second: %+v", second)
	}
	b.mustFail("factory.local_run", map[string]any{"caller": "b"}, appwire.CodeConflict)
	if b.backend.local.runner || b.backend.local.runnerDone != nil {
		t.Fatal("the joiner started a runner")
	}
}

// ── GitHub custody: nothing a connect writes reaches a profile ─────────────

// snapshotTree is every file under dir with its bytes, for a before and after.
func snapshotTree(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err == nil && info.Mode().IsRegular() {
			data, _ := os.ReadFile(path)
			out[path] = string(data)
		}
		return nil
	})
	return out
}

// THE APP HOLDS THE TOKEN IN MEMORY AND NO PROFILE KEEPS IT. A connect leaves
// the ambient profile byte-for-byte as it was, writes neither profile field the
// terminal's connect writes (github_token, github_via) anywhere under the app
// home, the person's home or the profile, and the credential memory holds it.
func TestLocalConnectWritesNoProfileAndFillsTheCredentialMemory(t *testing.T) {
	appHome, userHome, decoy := useLocalHome(t)
	if err := os.WriteFile(filepath.Join(decoy, "config.json"), []byte(`{"model":"keep/me"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	before := snapshotTree(t, decoy)
	w := newFactoryWire(t, testFactoryHub(t))
	gh := &setupGitHub{}
	offlineLocal(w.backend, gh, t)
	s := &localSetup{factoryWire: w, home: appHome, userHm: userHome, github: gh}
	s.start()
	for _, source := range []string{"token", "gh"} {
		params := map[string]any{"source": source}
		if source == "token" {
			params["token"] = setupSecret
		}
		// "gh" has no gh on this PATH and is refused; neither road may write a profile.
		if source == "gh" {
			params["caller"] = "w1"
			s.mustFail("factory.setup_connect", params, appwire.CodeEngine)
			continue
		}
		s.setup("factory.setup_connect", params)
	}
	if key, ok := config.AppCredentialSource().Key("github"); !ok || key != setupSecret {
		t.Fatal("the credential memory does not hold the token")
	}
	after := snapshotTree(t, decoy)
	// The credential store's own lock file (config.json.lock) is its bookkeeping
	// in the process profile, unchanged behaviour; it holds no bytes of a secret.
	delete(after, filepath.Join(decoy, "config.json.lock"))
	if len(after) != len(before) {
		t.Fatalf("the ambient profile gained or lost files: %v -> %v", baseNames(before), baseNames(after))
	}
	// What the credential store's own bookkeeping may add to config.json: the
	// day's budget defaults it writes whenever ANY key is handed to it (the app's
	// OpenRouter key does the same). Every earlier row stays as it was, and no
	// field about GitHub is among them. E5: d05770111c (Wormhole) also stamps
	// that budget preference in the same profile transaction.
	allowed := map[string]bool{"daily_budget_authority": true, "daily_budget_usd": true, "sync.preference_times": true}
	path := filepath.Join(decoy, "config.json")
	var was, now map[string]any
	if json.Unmarshal([]byte(before[path]), &was) != nil || json.Unmarshal([]byte(after[path]), &now) != nil {
		t.Fatalf("config.json unreadable: %q -> %q", before[path], after[path])
	}
	for key, value := range was {
		if !reflect.DeepEqual(now[key], value) {
			t.Fatalf("the ambient profile's %s changed: %v -> %v", key, value, now[key])
		}
	}
	for key := range now {
		if _, old := was[key]; !old && !allowed[key] {
			t.Fatalf("the ambient profile gained %q", key)
		}
	}
	stamps, ok := now["sync.preference_times"].(map[string]any)
	stamp, numeric := stamps["daily_budget_usd"].(float64)
	if !ok || len(stamps) != 1 || !numeric || stamp <= 0 {
		t.Fatalf("only the budget's preference clock may be added: %+v", now["sync.preference_times"])
	}
	for _, dir := range []string{appHome, userHome, decoy} {
		for path, data := range snapshotTree(t, dir) {
			for _, field := range []string{`"github_token"`, `"github_via"`, setupSecret} {
				if strings.Contains(data, field) {
					t.Fatalf("%s holds %s", path, field)
				}
			}
		}
	}
}

func baseNames(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, filepath.Base(k))
	}
	return out
}
