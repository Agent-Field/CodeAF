package main

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/service"
	"github.com/Agent-Field/codeaf/internal/service/servicetest"
	"github.com/Agent-Field/codeaf/internal/session"
)

// svcWorld is the app's backend over a real supervisor on a machine with no
// processes, its clock moved by the test. No model is called.
type svcWorld struct {
	backend *appBackend
	rec     *appRecorder
	agent   appwire.Agent
	sup     *service.Supervisor
	rt      *servicetest.Runtime
	clock   *servicetest.Clock
	dir     string
	work    string
}

func newSvcWorld(t *testing.T) *svcWorld {
	t.Helper()
	backend, rec, agent := workBackend(t, &fakeConversation{})
	w := &svcWorld{backend: backend, rec: rec, agent: agent, rt: servicetest.New(), clock: servicetest.NewClock(time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)), dir: t.TempDir(), work: t.TempDir()}
	w.open(t)
	return w
}

// relaunch is the same machine, supervisor folder and app files opened by a
// new backend, the way the app is opened again after a quit.
func (w *svcWorld) relaunch(t *testing.T, emit func(appwire.Emitter) appwire.Emitter) *svcWorld {
	t.Helper()
	w.backend.closeServices()
	rec := newAppRecorder()
	var into appwire.Emitter = rec
	if emit != nil {
		into = emit(rec)
	}
	var mu sync.Mutex
	var opens, releases int
	backend := newFakeBackend(into, fakeOpener(&fakeConversation{}, &opens, &releases, &mu))
	t.Cleanup(func() { backend.Close() })
	backend.state = w.backend.state
	agent, err := mustCreate(t, backend, appwire.CreateAsk{Workspace: t.TempDir()})
	if err != nil || agent.ID != w.agent.ID {
		t.Fatalf("the relaunched agent %+v %v", agent, err)
	}
	next := &svcWorld{backend: backend, rec: rec, agent: agent, rt: w.rt, clock: w.clock, dir: w.dir, work: w.work}
	next.open(t)
	return next
}

// open opens the supervisor over the world's folder and hands it to the
// backend, the way runApp does.
func (w *svcWorld) open(t *testing.T) {
	t.Helper()
	sup, err := service.Open(service.Options{Dir: w.dir, Runtime: w.rt, Clock: w.clock})
	if err != nil {
		t.Fatal(err)
	}
	w.sup = sup
	w.backend.attachServices(sup)
	t.Cleanup(w.backend.closeServices)
}

func (w *svcWorld) keep(t *testing.T, owner string, policy service.Policy) service.Row {
	t.Helper()
	row, err := w.sup.Keep(owner, service.Spec{Name: "web dev server", Command: "npm run dev", Dir: w.work, Health: service.Health{Port: 5173}}, policy, service.Provenance{Label: "start the web dev server"})
	if err != nil {
		t.Fatal(err)
	}
	return row
}

// changed waits for a process.changed of id that match accepts.
func (w *svcWorld) changed(t *testing.T, id string, match func(appwire.Process) bool) appwire.Process {
	t.Helper()
	i := w.rec.waitFor(t, 5*time.Second, func(_ string, data any) bool {
		c, ok := data.(appwire.ProcessChanged)
		return ok && c.Process.ID == id && c.Process.Service != nil && match(c.Process)
	})
	w.rec.mu.Lock()
	defer w.rec.mu.Unlock()
	return w.rec.data[i].(appwire.ProcessChanged).Process
}

// check moves the clock one check on and waits for its answer to be said.
func (w *svcWorld) check(t *testing.T, id string, fails int) appwire.Process {
	t.Helper()
	w.clock.Advance(service.CheckEvery)
	return w.changed(t, id, func(p appwire.Process) bool { return p.Service.Check != nil && p.Service.Check.Fails == fails })
}

// all is every event of type T the recorder heard.
func svcAll[T any](rec *appRecorder) []T {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	var out []T
	for _, data := range rec.data {
		if v, ok := data.(T); ok {
			out = append(out, v)
		}
	}
	return out
}

// TestAServiceIsAProcessAndTheProcessVerbsReachIt: one list, the service's
// own facts on it, and processes.stop / rerun / output acting on it.
func TestAServiceIsAProcessAndTheProcessVerbsReachIt(t *testing.T) {
	w := newSvcWorld(t)
	row := w.keep(t, w.agent.ID, service.PolicyOnFailure)
	id := w.agent.ID + ":" + row.ID
	list, err := w.backend.ListProcesses(w.agent.ID)
	if err != nil {
		t.Fatal(err)
	}
	var got *appwire.Process
	for i := range list {
		if list[i].ID == id {
			got = &list[i]
		}
	}
	if got == nil || got.State != appwire.ProcessRunning || got.Service == nil || !got.Service.Running ||
		got.Service.Timing.CheckEverySeconds != 10 || got.Service.Timing.RestartWindowSeconds != 600 || got.Service.Health.Words != "port 5173" {
		t.Fatalf("the service in the list: %+v", got)
	}
	if err := os.WriteFile(row.LogPath, []byte("ready on 5173\nGET /\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	lines, next, err := w.backend.ProcessOutput(id, 0)
	if err != nil || len(lines) != 2 || lines[0] != "ready on 5173" || next != 2 {
		t.Fatalf("output %q %d %v", lines, next, err)
	}
	if err := w.backend.StopProcess(id); err != nil {
		t.Fatal(err)
	}
	w.changed(t, id, func(p appwire.Process) bool { return p.State == appwire.ProcessStopped && !p.Service.Running })
	again, err := w.backend.RerunProcess(id)
	if err != nil || again.ID != id || again.State != appwire.ProcessRunning {
		t.Fatalf("rerun %+v %v", again, err)
	}
	w.changed(t, id, func(p appwire.Process) bool { return p.Service.Running && p.Service.StartedHow == "person" })
	// The record of the identity comes before the command is let run.
	for deadline := time.Now().Add(5 * time.Second); w.rt.Starts() != 2; time.Sleep(time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("started %d", w.rt.Starts())
		}
	}
}

// TestOneEpisodeIsOneInboxLineAndOneKeptNotice: three failed checks open one
// line and leave one notice; later checks update the same line; healthy again
// moves it to done at the moment the supervisor says, and the chat's history
// shows the notice after a relaunch without saying it twice.
func TestOneEpisodeIsOneInboxLineAndOneKeptNotice(t *testing.T) {
	w := newSvcWorld(t)
	row := w.keep(t, w.agent.ID, service.PolicyNever)
	id := w.agent.ID + ":" + row.ID
	w.rt.Failing(true)
	w.check(t, id, 1)
	w.check(t, id, 2)
	w.check(t, id, 3)
	w.changed(t, id, func(p appwire.Process) bool {
		return p.Service.Lifecycle == appwire.ServiceUnhealthy && p.Service.EpisodeOpen
	})
	w.check(t, id, 4)
	w.check(t, id, 5)

	items := map[string]int{}
	for _, added := range svcAll[appwire.InboxAddedEvent](w.rec) {
		if added.Item.Ask.Subkind == appwire.SubkindServiceHealth {
			items[added.Item.ID]++
		}
	}
	if len(items) != 1 {
		t.Fatalf("one episode is %d Inbox lines: %v", len(items), items)
	}
	var notices []appwire.Entry
	for _, e := range svcAll[appwire.EntryAdded](w.rec) {
		if e.Entry.Footer == appwire.FooterServiceTrouble {
			notices = append(notices, e.Entry)
		}
	}
	if len(notices) != 1 || notices[0].Ref != id {
		t.Fatalf("one episode left %d notices: %+v", len(notices), notices)
	}

	w.rt.Failing(false)
	healthy := w.check(t, id, 0)
	healthy = w.changed(t, id, func(p appwire.Process) bool { return !p.Service.EpisodeOpen && p.Service.EpisodeResolvedAt != nil })
	w.rec.waitFor(t, 5*time.Second, func(_ string, data any) bool {
		r, ok := data.(appwire.InboxResolvedEvent)
		return ok && r.By == appwire.ResolvedRecovered
	})
	// The clock moves on before anyone reads the line: its time is the
	// supervisor's, not the moment it was heard.
	w.clock.Advance(time.Hour)
	done, _, err := w.backend.ListInbox(appwire.InboxDone, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(done) != 1 || done[0].Answered == nil || !done[0].Answered.At.Equal(*healthy.Service.EpisodeResolvedAt) {
		t.Fatalf("done: %+v", done)
	}

	// A relaunch: the supervisor and the backend's half are opened again over
	// the same files, and nothing is said twice.
	w = w.relaunch(t, nil)
	entries, _, err := w.backend.Entries(w.agent.ID, "", 200)
	if err != nil {
		t.Fatal(err)
	}
	kept := 0
	for _, e := range entries {
		if e.Footer == appwire.FooterServiceTrouble && e.ID == notices[0].ID {
			kept++
		}
	}
	if kept != 1 {
		t.Fatalf("the history holds the notice %d times: %+v", kept, entries)
	}
	for _, e := range svcAll[appwire.EntryAdded](w.rec) {
		if e.Entry.Footer == appwire.FooterServiceTrouble {
			t.Fatalf("the relaunch said the notice again: %+v", e.Entry)
		}
	}
}

// TestWhatIsInTroubleAtStartIsInTheInboxAtOnce: a backend opened over an
// episode already open has its line before the next check.
func TestWhatIsInTroubleAtStartIsInTheInboxAtOnce(t *testing.T) {
	w := newSvcWorld(t)
	row := w.keep(t, w.agent.ID, service.PolicyNever)
	id := w.agent.ID + ":" + row.ID
	w.rt.Failing(true)
	w.check(t, id, 1)
	w.check(t, id, 2)
	w.check(t, id, 3)
	w.changed(t, id, func(p appwire.Process) bool { return p.Service.EpisodeOpen })
	w = w.relaunch(t, nil)
	waiting, _, _ := w.backend.ListInbox(appwire.InboxWaiting, "")
	found := 0
	for _, item := range waiting {
		if item.Ask.Subkind == appwire.SubkindServiceHealth && item.Ask.Subject != nil && item.Ask.Subject.Ref == id {
			found++
		}
	}
	if found != 1 {
		t.Fatalf("the open episode is %d lines at start: %+v", found, waiting)
	}
}

// TestClosingAnAgentWaitsForItsProcessesAndKeepsItWhenOneWillNotStop.
func TestClosingAnAgentWaitsForItsProcessesAndKeepsItWhenOneWillNotStop(t *testing.T) {
	w := newSvcWorld(t)
	row := w.keep(t, w.agent.ID, service.PolicyAlways)
	other, err := w.sup.Keep("another-agent", service.Spec{Name: "api", Command: "node api.js", Dir: w.work, Health: service.Health{Port: 4011}}, service.PolicyNever, service.Provenance{})
	if err != nil {
		t.Fatal(err)
	}
	w.rt.Refusing(true)
	closed := make(chan error, 1)
	go func() { closed <- w.backend.CloseAgent(w.agent.ID) }()
	// The stop's grace and its kill are moved through on the test's clock.
	var err2 error
	for done := false; !done; {
		select {
		case err2 = <-closed:
			done = true
		default:
			w.clock.Advance(time.Second)
			time.Sleep(time.Millisecond)
		}
	}
	if err2 == nil || !strings.Contains(err2.Error(), row.ID) {
		t.Fatalf("a close that could not stop its process said %v", err2)
	}
	a, _ := w.backend.find(w.agent.ID)
	var state string
	w.backend.locked(func() { state = a.view.State })
	if state == appwire.StateClosed {
		t.Fatal("the agent was closed over a process still running")
	}
	if got, _ := w.sup.Get(w.agent.ID, row.ID); !got.Running {
		t.Fatalf("the process is hidden: %+v", got)
	}
	if got, _ := w.sup.Get("another-agent", other.ID); !got.Running {
		t.Fatal("another agent's process was stopped")
	}

	w.rt.Refusing(false)
	if err := w.backend.CloseAgent(w.agent.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := w.sup.Get(w.agent.ID, row.ID); got.Running || got.StoppedBy != appServiceClosedBy {
		t.Fatalf("after the close: %+v", got)
	}
	w.backend.locked(func() { state = a.view.State })
	if state != appwire.StateClosed {
		t.Fatalf("state %s", state)
	}
}

// TestAFollowThatMeetsTheCloseIsRefused: a follow that found the supervisor
// before the quit drained the follows is refused after it, not started.
func TestAFollowThatMeetsTheCloseIsRefused(t *testing.T) {
	w := newSvcWorld(t)
	row := w.keep(t, w.agent.ID, service.PolicyNever)
	id := w.agent.ID + ":" + row.ID
	held, release := make(chan struct{}), make(chan struct{})
	appServiceFollowGate = func() { close(held); <-release }
	t.Cleanup(func() { appServiceFollowGate = nil })
	result := make(chan error, 1)
	go func() { result <- w.backend.FollowProcess(id, true) }()
	<-held
	svc, _ := w.backend.servicesOf()
	w.backend.closeServices()
	release <- struct{}{}
	if err := <-result; err == nil || !strings.Contains(err.Error(), "closing") {
		t.Fatalf("a follow after the close said %v", err)
	}
	svc.mu.Lock()
	n := len(svc.follows)
	svc.mu.Unlock()
	if n != 0 {
		t.Fatalf("%d follows outlived the close", n)
	}
}

// svcBlockedEmitter holds every process.output until it is let go.
type svcBlockedEmitter struct {
	next    appwire.Emitter
	blocked chan struct{}
	release chan struct{}
	once    sync.Once
}

func (e *svcBlockedEmitter) Emit(event string, data any) {
	if event == appwire.EventProcessOutput {
		e.once.Do(func() { close(e.blocked) })
		<-e.release
	}
	e.next.Emit(event, data)
}

// TestUnfollowAndCloseDoNotWaitOnAWireThatIsNotTaking: a tail stuck in a send
// the window is not reading neither holds an unfollow nor a quit.
func TestUnfollowAndCloseDoNotWaitOnAWireThatIsNotTaking(t *testing.T) {
	first := newSvcWorld(t)
	row := first.keep(t, first.agent.ID, service.PolicyNever)
	blocked := &svcBlockedEmitter{blocked: make(chan struct{}), release: make(chan struct{})}
	w := first.relaunch(t, func(rec appwire.Emitter) appwire.Emitter { blocked.next = rec; return blocked })
	id := w.agent.ID + ":" + row.ID
	if err := w.backend.FollowProcess(id, true); err != nil {
		t.Fatal(err)
	}
	// Two views follow; one leaving keeps the tail for the other.
	if err := w.backend.FollowProcess(id, true); err != nil {
		t.Fatal(err)
	}
	svc, _ := w.backend.servicesOf()
	svc.mu.Lock()
	f := svc.follows[id]
	svc.mu.Unlock()
	if err := os.WriteFile(row.LogPath, []byte("one\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	select {
	case <-blocked.blocked:
	case <-time.After(5 * time.Second):
		t.Fatal("the tail never sent")
	}
	quick := func(what string, fn func()) {
		t.Helper()
		done := make(chan struct{})
		go func() { fn(); close(done) }()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatalf("%s waited on the stuck send", what)
		}
	}
	quick("the first unfollow", func() { _ = w.backend.FollowProcess(id, false) })
	svc.mu.Lock()
	still := svc.follows[id] == f && f.consumers == 1
	svc.mu.Unlock()
	if !still {
		t.Fatal("one view's unfollow ended the other's")
	}
	quick("the quit", w.backend.closeServices)
	close(blocked.release)
	select {
	case <-f.done:
	case <-time.After(5 * time.Second):
		t.Fatal("the tail did not go once its send returned")
	}
}

// TestARemovedServiceStillOpensItsOutput: removal keeps the output and says
// whether earlier output had been cut.
func TestARemovedServiceStillOpensItsOutput(t *testing.T) {
	w := newSvcWorld(t)
	row := w.keep(t, w.agent.ID, service.PolicyNever)
	id := w.agent.ID + ":" + row.ID
	if err := os.WriteFile(row.LogPath, []byte("listening\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := w.backend.StopService(id); err != nil {
		t.Fatal(err)
	}
	// The first event of a kept service is its launch, which also says not
	// running; only the stopped state with the grace over means the process is
	// proven gone and removal is allowed.
	w.changed(t, id, func(p appwire.Process) bool {
		return p.State == appwire.ProcessStopped && !p.Service.Running && !p.Service.Stopping
	})
	if err := w.backend.RemoveService(id); err != nil {
		t.Fatal(err)
	}
	list, _ := w.backend.ListServices(w.agent.ID)
	if len(list) != 0 {
		t.Fatalf("still listed: %+v", list)
	}
	got, err := w.backend.GetService(id)
	if err != nil || got.Service == nil || !got.Service.Removed {
		t.Fatalf("the removed service: %+v %v", got, err)
	}
	lines, _, err := w.backend.ProcessOutput(id, 0)
	if err != nil || len(lines) != 1 || lines[0] != "listening" {
		t.Fatalf("its output %q %v", lines, err)
	}
	if _, err := os.Stat(filepath.Clean(row.LogPath)); err != nil {
		t.Fatal(err)
	}
}

// TestTheKeepRunningCardOnTheWire: its three answers in the app's names over
// the engine's keys, and its proposal typed from the card.
func TestTheKeepRunningCardOnTheWire(t *testing.T) {
	notice := session.KeepNotice{ID: 7, CallID: "k1", Name: "web dev server", Command: "npm run dev", Dir: "/work/site ", Health: service.Health{Kind: service.HealthPort, Port: 5173}, HealthWords: "port 5173", Policy: service.PolicyAlways, PolicyWords: "restart always"}
	q := session.KeepQuestion(notice)
	ask := appAskOf("a1", q)
	if ask.Kind != appwire.AskApproval || ask.Subkind != appwire.SubkindKeepRunning {
		t.Fatalf("%+v", ask)
	}
	want := []string{appwire.OptionAllow, appwire.OptionAllowNever, appwire.OptionDeny}
	keys := []string{session.KeepAllowKey, session.KeepAllowNeverKey, session.KeepDenyKey}
	options := appOptionsOf(q)
	if len(options) != 3 {
		t.Fatalf("%+v", options)
	}
	for i, option := range options {
		if option.id != want[i] || option.key != keys[i] {
			t.Fatalf("option %d = %+v", i, option)
		}
	}
	facts := ask.KeepRunning
	if facts == nil || facts.Command != "npm run dev" || facts.Dir != "/work/site " || facts.Health.Kind != "port" || facts.Health.Port != 5173 ||
		facts.Policy != "always" || facts.CallID != "k1" || facts.Timing.CheckEverySeconds != 10 || facts.Timing.StopGraceSeconds != 5 {
		t.Fatalf("the proposal: %+v", facts)
	}
	if got := appReceipt("", appwire.OptionAllowNever, nil, time.Now()); !strings.HasPrefix(got, "Kept running, never restarted") {
		t.Fatal(got)
	}
}

// TestALiveServiceKeepsItsAgentThroughTheIdleSweep.
func TestALiveServiceKeepsItsAgentThroughTheIdleSweep(t *testing.T) {
	w := newSvcWorld(t)
	if w.backend.serviceKeeps(w.agent.ID) {
		t.Fatal("an agent with nothing kept running is kept")
	}
	w.keep(t, w.agent.ID, service.PolicyNever)
	if !w.backend.serviceKeeps(w.agent.ID) {
		t.Fatal("an agent with a live service is let go by the sweep")
	}
}

// TestWithNoSupervisorTheServicesAreAbsent: every door says unknown_method.
func TestWithNoSupervisorTheServicesAreAbsent(t *testing.T) {
	backend, _, agent := workBackend(t, &fakeConversation{})
	_, err := backend.ListServices(agent.ID)
	var wire *appwire.Error
	if !errors.As(err, &wire) || wire.Code != appwire.CodeUnknownMethod {
		t.Fatalf("%v", err)
	}
	if err := backend.CloseAgent(agent.ID); err != nil {
		t.Fatalf("closing with no supervisor: %v", err)
	}
}

// TestNothingStartsForAnAgentWhileItCloses: a start asked for while the close
// is waiting out its grace is refused, and the close still succeeds.
func TestNothingStartsForAnAgentWhileItCloses(t *testing.T) {
	w := newSvcWorld(t)
	row := w.keep(t, w.agent.ID, service.PolicyNever)
	id := w.agent.ID + ":" + row.ID
	w.rt.Refusing(true)
	closed := make(chan error, 1)
	go func() { closed <- w.backend.CloseAgent(w.agent.ID) }()
	w.changed(t, id, func(p appwire.Process) bool { return p.Service.Stopping })
	if _, err := w.backend.StartService(id); err == nil || !strings.Contains(err.Error(), "closing") {
		t.Fatalf("a start during the close said %v", err)
	}
	if _, err := w.sup.Keep(w.agent.ID, service.Spec{Name: "api", Command: "node api.js", Dir: w.work, Health: service.Health{Port: 4013}}, service.PolicyNever, service.Provenance{}); !errors.Is(err, service.ErrClosing) {
		t.Fatalf("a keep during the close said %v", err)
	}
	w.rt.Refusing(false)
	for done := false; !done; {
		select {
		case err := <-closed:
			if err == nil {
				// The grace ended on SIGKILL, which now works.
				done = true
				break
			}
			// Refused while the test was still refusing: close again.
			go func() { closed <- w.backend.CloseAgent(w.agent.ID) }()
		default:
			w.clock.Advance(time.Second)
			time.Sleep(time.Millisecond)
		}
	}
	if got, _ := w.sup.Get(w.agent.ID, row.ID); got.Running {
		t.Fatalf("%+v", got)
	}
	// Reopened later, the agent may start things again.
	if _, err := w.backend.StartService(id); err != nil {
		t.Fatalf("after the close: %v", err)
	}
}

// TestEveryNoticeIsKeptAndPagedPastFiveHundred: six hundred episodes leave six
// hundred notices on disk; the history pages back from the newest to the
// oldest a page at a time, after a relaunch too, and a replayed episode is
// not written twice.
func TestEveryNoticeIsKeptAndPagedPastFiveHundred(t *testing.T) {
	w := newSvcWorld(t)
	log := w.backend.noticeLog()
	base := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	ref := w.agent.ID + ":s1"
	const episodes = 600
	for i := 1; i <= episodes; i++ {
		n := appServiceNotice{Agent: w.agent.ID, Ref: ref, Episode: "s1." + itoa(i), Text: "web is not answering", At: base.Add(time.Duration(i) * time.Minute)}
		if err := log.Append(n); err != nil {
			t.Fatal(err)
		}
		// The same episode heard again — a replay — writes nothing.
		if err := log.Append(n); err != nil {
			t.Fatal(err)
		}
	}
	record, _, _ := log.paths(w.agent.ID)
	raw, err := os.ReadFile(record)
	if err != nil {
		t.Fatal(err)
	}
	if lines := strings.Count(string(raw), "\n"); lines != episodes {
		t.Fatalf("the record holds %d lines for %d episodes", lines, episodes)
	}
	// A new log over the same folder, as after a relaunch: its index is read
	// back, and a replay still writes nothing.
	again := newAppNoticeLog(log.dir)
	if !again.Noticed(appServiceNotice{Agent: w.agent.ID, Ref: ref, Episode: "s1.1"}) {
		t.Fatal("the oldest episode is not known as noticed after a relaunch")
	}
	w = w.relaunch(t, nil)
	seen := map[string]bool{}
	var oldest, newest string
	before, pages := "", 0
	for {
		page, more, err := w.backend.Entries(w.agent.ID, before, 100)
		if err != nil {
			t.Fatal(err)
		}
		if len(page) > 100 {
			t.Fatalf("a page of %d", len(page))
		}
		pages++
		for _, e := range page {
			if e.Footer != appwire.FooterServiceTrouble {
				continue
			}
			if seen[e.ID] {
				t.Fatalf("%s given twice", e.ID)
			}
			seen[e.ID] = true
		}
		if pages == 1 && len(page) > 0 {
			newest = page[len(page)-1].ID
		}
		if len(page) > 0 {
			oldest = page[0].ID
			before = page[0].ID
		}
		if !more {
			break
		}
		if pages > 20 {
			t.Fatal("the pages never end")
		}
	}
	if len(seen) != episodes || newest != "svc"+ref+".s1."+itoa(episodes) || oldest != "svc"+ref+".s1.1" {
		t.Fatalf("paged %d notices over %d pages, newest %s, oldest %s", len(seen), pages, newest, oldest)
	}
}

func itoa(n int) string { return strconv.Itoa(n) }

// TestNoticesLieBetweenTheEntriesTheyCameBetween: paged back in small pages,
// a conversation and its notices come out as one sequence in time order, each
// entry once, whichever kind a page's cursor is.
func TestNoticesLieBetweenTheEntriesTheyCameBetween(t *testing.T) {
	base := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	var items []session.DisplayEntry
	for i := 0; i < 20; i++ {
		items = append(items, session.DisplayEntry{Role: "user", Text: "q" + itoa(i), At: base.Add(time.Duration(i) * time.Minute)})
	}
	record := session.Record{Entries: items}
	log := newAppNoticeLog(t.TempDir())
	// Notices after every third message, and two after the last.
	var want []string
	for i := 0; i < 20; i++ {
		want = append(want, appUserEntryID(i))
		if i%3 == 0 || i == 19 {
			n := appServiceNotice{Agent: "a1", Ref: "a1:s1", Episode: "s1." + itoa(i+1), Text: "down", At: base.Add(time.Duration(i)*time.Minute + 30*time.Second)}
			if err := log.Append(n); err != nil {
				t.Fatal(err)
			}
			want = append(want, n.id())
		}
	}
	tail := appServiceNotice{Agent: "a1", Ref: "a1:s1", Episode: "s1.99", Text: "down", At: base.Add(time.Hour)}
	if err := log.Append(tail); err != nil {
		t.Fatal(err)
	}
	want = append(want, tail.id())
	notes := func() (*appNoticeReader, error) { return log.Reader("a1") }
	facts := func(session.DisplayEntry) appwire.ToolFacts { return appwire.ToolFacts{} }
	var got []string
	before := ""
	for pages := 0; ; pages++ {
		page, more, err := appEntryPageWith("a1", record, base, nil, notes, before, 4, facts)
		if err != nil {
			t.Fatal(err)
		}
		ids := make([]string, len(page))
		for i, e := range page {
			ids[i] = e.ID
		}
		got = append(ids, got...)
		if !more || len(page) == 0 {
			break
		}
		before = page[0].ID
		if pages > 50 {
			t.Fatal("the pages never end")
		}
	}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("paged\n %v\nwant\n %v", got, want)
	}
}
