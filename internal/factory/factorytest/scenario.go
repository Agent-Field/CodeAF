package factorytest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/client"
	factorygithub "github.com/Agent-Field/codeaf/internal/factory/github"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/run"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/factory/worker"
)

// A SCENARIO IS ONE JOB'S LIFE ON A WHOLE FACTORY, IN ONE PROCESS. The service
// is the real one (the store, the lease desk, the HTTP handler and its settle
// pass), the runner is the real worker over loopback with a real Runner, the
// checkout is a real git repository with a bare remote, and the forge is read
// by the real poller. Three things are scripted: the delegate (the stage
// executor, [Delegate]), the forge's answers ([FakeGitHub]) and the person,
// who presses the service's own doors over HTTP.
//
// A scenario is a list of events. After each one the job is left to come to
// rest and [Scenario.Check] holds the rule every replay and random run is
// for: a job is never idle without a named owner and an action that will work.

// The scenario's person, machine and repository.
const (
	scenarioPerson  = "me"
	scenarioMachine = "laptop"
	scenarioOwner   = "acme"
	scenarioRepo    = "api"
)

// Scenario is one job on one factory.
type Scenario struct {
	T        testing.TB
	Mode     string
	Store    *store.Store
	Desk     *lease.Desk
	Record   *record.Record
	GitHub   *FakeGitHub
	Delegate *Delegate
	// Job is the job's id.
	Job int

	// Quiet stops the scenario's own requests from settling the floor, so a
	// replay can hold a job where only a timer would move it.
	Quiet bool

	clock    *Clock
	seam     factory.Seam
	handler  *serve.Handler
	server   *httptest.Server
	source   *factorygithub.Source
	root     string
	mirror   *store.Store
	workdirs *run.Workdirs
	// computers keeps each computer's own mirror and worktrees by its name.
	computers map[string]computer
	checkout  string
	bare      string
	scratch   string

	// done gives the scenario's slot back, once.
	done sync.Once

	mu     sync.Mutex
	runner *run.Runner
	quit   func()
	// mainAgent is the scripted main agent ([Scenario.MainAgent]).
	mainAgent func(q factory.Asked) (answer, why string)
	// machine is the name the worker attaches as; a scenario changes it to
	// stand for another computer.
	machine string

	// Events is every event so far, oldest first, as a failure prints them.
	Events []string
	seed   string
	edits  int
	// clockFaults are the writes that moved a wait's clock without moving the
	// wait, seen by the store's observer.
	clockFaults []string
	// refused is what the service refused this computer's runner.
	refused []string
}

// computer is one runner's own disk: the mirror its jobs run over and the
// folder its worktrees are made in.
type computer struct {
	mirror   *store.Store
	workdirs *run.Workdirs
}

// Clock is the service's clock: the real one, moved forward when a scenario
// waits out an undo window or a lease.
type Clock struct {
	mu     sync.Mutex
	offset time.Duration
}

// Now is the scenario's time.
func (c *Clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return time.Now().Add(c.offset)
}

// Advance moves the clock forward.
func (c *Clock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.offset += d
}

// NewScenario builds a factory with one job in its backlog, in mode
// ([factory.AutonomyAuto] or [factory.AutonomyManual]), and a runner online.
// The job has the shipped steps named, or plan, approve, write, test, review
// and proof when none is.
func NewScenario(t testing.TB, mode string, steps ...string) *Scenario {
	t.Helper()
	s := newScenario(t, t.TempDir(), mode, steps)
	t.Cleanup(s.close)
	return s
}

func newScenario(t testing.TB, root, mode string, steps []string) *Scenario {
	t.Helper()
	s := scenarioAt(t, root, mode)
	s.git("", "init", "-q", "-b", "main", s.checkout)
	s.write(filepath.Join(s.checkout, "ledger.go"), "package ledger\n")
	s.git(s.checkout, "add", "ledger.go")
	s.git(s.checkout, "commit", "-q", "-m", "first")
	s.git(s.checkout, "config", "user.name", "t")
	s.git(s.checkout, "config", "user.email", "t@t")
	s.git(s.checkout, "config", "commit.gpgsign", "false")
	// Git's own flushes to disk protect a repository from a power cut. These
	// live for one test.
	s.git(s.checkout, "config", "core.fsync", "none")
	s.git("", "init", "-q", "--bare", "-b", "main", s.bare)
	s.git(s.bare, "config", "core.fsync", "none")
	s.git(s.checkout, "remote", "add", "origin", s.bare)
	s.git(s.checkout, "push", "-q", "origin", "main")
	s.openStores()
	if err := s.Store.SetRepos([]string{scenarioOwner + "/" + scenarioRepo}); err != nil {
		t.Fatal(err)
	}
	if len(steps) == 0 {
		steps = []string{"plan", factory.ApproveName, "write", "test", "review", "proof"}
	}
	stages := []factory.Stage{}
	for _, name := range steps {
		st, ok := factory.ShippedStage(name)
		if !ok {
			t.Fatalf("no shipped step is named %s", name)
		}
		stages = append(stages, st)
	}
	id, err := s.Store.Add(context.Background(), factory.Item{
		Autonomy: mode, Title: "Keep the ledger balanced", Repo: scenarioRepo, Product: scenarioOwner, Num: 12,
		Kind: factory.KindIssue, Tier: factory.TierOwner, Starter: scenarioPerson, Cap: 10, Stages: stages,
	})
	if err != nil {
		t.Fatal(err)
	}
	s.Job = id
	s.open()
	s.StartRunner()
	return s
}

// realGit puts the git binary itself first on this process's PATH, once. On
// macOS /usr/bin/git is a shim that finds the developer tools on every call,
// which triples the cost of the hundreds of git commands a scenario runs.
var realGit sync.Once

func useRealGit() {
	if runtime.GOOS != "darwin" {
		return
	}
	found, err := exec.LookPath("git")
	if err != nil || found != "/usr/bin/git" {
		return
	}
	out, err := exec.Command("xcrun", "-f", "git").Output()
	if dir := filepath.Dir(strings.TrimSpace(string(out))); err == nil && filepath.IsAbs(dir) {
		_ = os.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	}
}

// scenarioSlots is how many scenarios run at once. Each is a whole factory
// that flushes every write to disk, and the disk takes those flushes one at a
// time: past a handful, more scenarios at once finish no sooner and only
// starve the timing tests of the packages that run beside this one.
var scenarioSlots = make(chan struct{}, 6)

// scenarioAt is a scenario's names under root, with nothing made yet. It
// waits for a slot, which the scenario gives back when it closes.
func scenarioAt(t testing.TB, root, mode string) *Scenario {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git on this machine")
	}
	realGit.Do(useRealGit)
	scenarioSlots <- struct{}{}
	s := &Scenario{T: t, Mode: mode, root: root, clock: &Clock{}, GitHub: NewFakeGitHub(scenarioOwner, scenarioRepo), Delegate: &Delegate{}, machine: scenarioMachine}
	t.Cleanup(s.release)
	s.Delegate.s = s
	s.checkout = filepath.Join(root, "checkout", scenarioRepo)
	s.bare = filepath.Join(root, "remote.git")
	s.scratch = filepath.Join(root, "scratch")
	if err := os.MkdirAll(s.scratch, 0o700); err != nil {
		t.Fatal(err)
	}
	s.GitHub.headOf = s.headOf
	return s
}

// headOf is the head of branch on the scenario's remote, "" when it has none.
func (s *Scenario) headOf(branch string) string {
	out, err := exec.Command("git", "-C", s.bare, "rev-parse", "--verify", "-q", branch).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// openStores opens the factory's store and this computer's disk.
func (s *Scenario) openStores() {
	s.T.Helper()
	var err error
	if s.Store, err = store.Open(filepath.Join(s.root, "factory")); err != nil {
		s.T.Fatal(err)
	}
	s.useComputer(scenarioMachine)
	s.Store.ObserveWrites(s.observe)
	s.source = factorygithub.New(s.GitHub, []string{scenarioOwner + "/" + scenarioRepo}, s.clock.Now)
}

// A SCENARIO THAT STARTS AT AN OPEN PULL REQUEST IS COPIED FROM ONE THAT GOT
// THERE. Most replays and every random run begin where the pull request has
// just been opened, and running the pipeline to that point costs each of them
// a few seconds of the same work. The first [Opened] in a mode runs it once,
// through the real doors with the properties held at every rest, and keeps
// the whole factory's folder; each later one copies the folder and goes on
// from there. The stores keep no path of their own, so only git's three files
// that name the folder are rewritten.
type opened struct {
	once   sync.Once
	root   string
	failed string
	job    int
	edits  int
	offset time.Duration
	events []string
	github *FakeGitHub
}

var (
	openedMu   sync.Mutex
	openedByID = map[string]*opened{}
)

// Opened is a scenario whose job has opened its pull request: a draft, its
// checks not yet reported, Follow PR watching it. The job has the steps write
// and proof. A test binary that calls it calls [CleanScenarios] when it ends.
func Opened(t testing.TB, mode string) *Scenario {
	t.Helper()
	openedMu.Lock()
	tmpl := openedByID[mode]
	if tmpl == nil {
		tmpl = &opened{}
		openedByID[mode] = tmpl
	}
	openedMu.Unlock()
	tmpl.once.Do(func() {
		tmpl.failed = "the scenario every other is copied from did not open its pull request"
		root, err := os.MkdirTemp("", "factory-scenario-")
		if err != nil {
			t.Fatal(err)
		}
		tmpl.root = root
		first := newScenario(t, root, mode, []string{"write", "proof"})
		t.Cleanup(first.close)
		first.Open()
		first.close()
		tmpl.job, tmpl.edits, tmpl.offset = first.Job, first.edits, first.clock.Now().Sub(time.Now())
		tmpl.events = append([]string(nil), first.Events...)
		tmpl.github = first.GitHub.clone()
		tmpl.failed = ""
	})
	if tmpl.failed != "" {
		t.Fatal(tmpl.failed)
	}
	root := t.TempDir()
	if err := copyDir(tmpl.root, root); err != nil {
		t.Fatalf("copy the opened scenario: %v", err)
	}
	s := scenarioAt(t, root, mode)
	named, _ := filepath.Glob(filepath.Join(s.checkout, ".git", "worktrees", "*", "gitdir"))
	work, _ := filepath.Glob(filepath.Join(root, "*", "work", "*", ".git"))
	for _, path := range append(append(named, work...), filepath.Join(s.checkout, ".git", "config")) {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		s.write(path, strings.ReplaceAll(string(data), tmpl.root, root))
	}
	s.GitHub = tmpl.github.clone()
	s.GitHub.headOf = s.headOf
	s.Job, s.edits = tmpl.job, tmpl.edits
	s.clock.Advance(tmpl.offset)
	s.Events = append([]string(nil), tmpl.events...)
	s.openStores()
	s.open()
	t.Cleanup(s.close)
	s.StartRunner()
	return s
}

// CleanScenarios removes the folders [Opened] keeps for the life of a test
// binary. A TestMain calls it after the tests have run.
func CleanScenarios() {
	openedMu.Lock()
	defer openedMu.Unlock()
	for mode, tmpl := range openedByID {
		if tmpl.root != "" {
			_ = os.RemoveAll(tmpl.root)
		}
		delete(openedByID, mode)
	}
}

// open stands the service up over the store: the lease desk, the doors, the
// record and the HTTP handler. A restart closes it and opens it again.
func (s *Scenario) open() {
	s.T.Helper()
	var err error
	if s.Desk, err = lease.Open(s.Store, lease.Options{Now: s.clock.Now}); err != nil {
		s.T.Fatal(err)
	}
	s.seam = factory.LocalSeam(s.Store, time.Now(), factory.WithRunner(lease.NewDoors(s.Desk)))
	s.seam.GitHub = func(context.Context) (factory.GitHubLink, error) {
		return factory.GitHubLink{Login: scenarioPerson}, nil
	}
	if s.Record, err = record.Open(s.Store, record.Options{}); err != nil {
		s.T.Fatal(err)
	}
	s.handler = serve.New(s.seam, serve.Config{
		Address: "http://test", Store: s.Store, Record: s.Record, Now: s.clock.Now,
		PullWait: 300 * time.Millisecond, LeaseOptions: lease.Options{Now: s.clock.Now},
	})
	s.server = httptest.NewServer(s.listen(s.handler))
}

// listen is the service as the runner reaches it. What the service refuses a
// runner is kept for the story: a refused report is how a job loses results.
func (s *Scenario) listen(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := &statusWriter{ResponseWriter: w}
		handler.ServeHTTP(rec, r)
		if rec.status >= 400 && rec.status != http.StatusUnauthorized {
			s.mu.Lock()
			if len(s.refused) < 12 {
				s.refused = append(s.refused, fmt.Sprintf("%s %s -> %d %s", r.Method, lastOf(r.URL.Path), rec.status, strings.TrimSpace(string(rec.body))))
			}
			s.mu.Unlock()
		}
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
	body   []byte
}

func (w *statusWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status >= 400 && len(w.body) < 300 {
		w.body = append(w.body, b...)
	}
	return w.ResponseWriter.Write(b)
}

func (w *statusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func lastOf(path string) string {
	if i := strings.LastIndex(path, "/"); i >= 0 {
		return path[i:]
	}
	return path
}

// close ends the scenario: its runner and service stop and its slot is free.
func (s *Scenario) close() {
	if os.Getenv("SCENARIO_STORY") != "" && s.handler != nil {
		s.T.Log("\n" + s.Story())
	}
	s.shut()
	s.release()
}

// release gives the scenario's slot back, once.
func (s *Scenario) release() { s.done.Do(func() { <-scenarioSlots }) }

// shut stops the runner and the service and keeps everything on disk.
func (s *Scenario) shut() {
	s.StopRunner()
	s.mu.Lock()
	server, handler := s.server, s.handler
	s.server, s.handler = nil, nil
	s.mu.Unlock()
	if server != nil {
		server.Close()
		handler.Close()
	}
}

// RestartService closes the service and opens it again over the same store,
// as quitting and reopening the app does. The runner goes with it.
func (s *Scenario) RestartService() {
	s.T.Helper()
	s.shut()
	s.open()
	s.StartRunner()
}

// useComputer makes name the computer the scenario's runner runs on, with a
// disk of its own the first time it is named.
func (s *Scenario) useComputer(name string) {
	s.T.Helper()
	if s.computers == nil {
		s.computers = map[string]computer{}
	}
	c, ok := s.computers[name]
	if !ok {
		mirror, err := store.Open(filepath.Join(s.root, name, "mirror"))
		if err != nil {
			s.T.Fatal(err)
		}
		c = computer{mirror: mirror, workdirs: run.NewWorkdirs(filepath.Join(s.root, name, "work"), run.ExecGit{})}
		s.computers[name] = c
	}
	s.machine, s.mirror, s.workdirs = name, c.mirror, c.workdirs
}

// SwitchComputer takes this computer's runner offline and brings another
// computer's online in its place: one with the same person and none of the
// first one's worktrees.
func (s *Scenario) SwitchComputer(name string) {
	s.T.Helper()
	s.StopRunner()
	s.useComputer(name)
	s.StartRunner()
}

// StartRunner brings this computer's runner online.
func (s *Scenario) StartRunner() {
	s.T.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.quit != nil {
		return
	}
	push := run.GitPush(run.ExecGit{})
	workdirs := s.workdirs
	r := run.New(run.Options{
		Store:   s.mirror,
		Exec:    map[factory.StageKind]run.Executor{factory.StageChat: s.Delegate},
		RepoDir: func(string) string { return s.checkout },
		Workdir: func(it factory.Item) (string, error) { return workdirs.For(it, s.checkout) },
		Source:  func(string) factory.Source { return s.source },
		Inbox: func(_ context.Context, _ factory.Item, q factory.Asked) (run.InboxReply, error) {
			s.mu.Lock()
			agent := s.mainAgent
			s.mu.Unlock()
			if agent == nil {
				return run.InboxReply{Why: "there is no main agent here"}, nil
			}
			answer, why := agent(q)
			return run.InboxReply{Answer: answer, Why: why}, nil
		},
		Push: func(dir, branch string, before ...func() error) error {
			if err := push(dir, branch, before...); err != nil {
				return err
			}
			// The runner pushes a refspec, `<commit>:refs/heads/<branch>`.
			if _, to, ok := strings.Cut(branch, ":"); ok {
				branch = strings.TrimPrefix(to, "refs/heads/")
			}
			s.GitHub.pushed(branch, s.GitHub.headOf(branch))
			return nil
		},
	})
	s.runner = r
	base, machine, mirror := s.server.URL, s.machine, s.mirror
	var mu sync.Mutex
	creds := map[int]string{}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		worker.Run(ctx, worker.Options{
			Dial: func(slot int) *client.WorkerClient {
				mu.Lock()
				defer mu.Unlock()
				return client.Open(base, "").Worker(creds[slot])
			},
			Keep: func(slot int, w *client.WorkerClient) {
				mu.Lock()
				defer mu.Unlock()
				creds[slot] = w.Credential()
			},
			Attach:      api.WorkerAttach{Machine: machine, Name: machine + " (pid 1)", Handle: scenarioPerson, TakesUnassigned: map[string]bool{lease.EveryRepo: true}},
			Runner:      r,
			Mirror:      mirror,
			Now:         s.clock.Now,
			Slots:       1,
			ReportEvery: 15 * time.Millisecond,
			BeatEvery:   200 * time.Millisecond,
		})
	}()
	s.quit = func() { cancel(); <-done }
}

// MainAgent scripts the main agent: its one turn on a step's question. turn
// answers the step, or says why the question is the person's. With none
// scripted every question goes on to the person.
func (s *Scenario) MainAgent(turn func(q factory.Asked) (answer, why string)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.mainAgent = turn
}

// StopRunner quits this computer's runner, as quitting the app does: a job
// it holds is handed back.
func (s *Scenario) StopRunner() {
	s.mu.Lock()
	quit, runner := s.quit, s.runner
	s.quit, s.runner = nil, nil
	s.mu.Unlock()
	if quit == nil {
		return
	}
	quit()
	// The round the runner let go of ends a moment after the runner does;
	// nothing of it may still be writing when the test's folders are removed.
	for deadline := time.Now().Add(5 * time.Second); runner.Running(s.Job) && time.Now().Before(deadline); {
		time.Sleep(2 * time.Millisecond)
	}
}

// KillRunnerMidRound is this computer's runner dying in the middle of a
// Follow PR round that start sets off: the round is under way, the runner's
// process is gone without a word to the service, and its hold on the job
// runs out. The runner comes back afterwards, as a restarted app's does. It
// says false, having changed nothing else, when start set no round off.
func (s *Scenario) KillRunnerMidRound(start func()) bool {
	s.T.Helper()
	entered, hold := make(chan struct{}), make(chan struct{})
	s.Delegate.Next(factory.FollowPRStep, Round{entered: entered, hold: hold})
	start()
	deadline := time.After(30 * time.Second)
	for began := false; !began; {
		select {
		case <-entered:
			began = true
		case <-deadline:
			s.Delegate.Forget(factory.FollowPRStep)
			close(hold)
			return false
		case <-time.After(tickEvery):
			s.Tick()
		}
	}
	// A process that dies says nothing: the service is out of its reach
	// before it is stopped, so it cannot hand the job back.
	s.mu.Lock()
	server, handler := s.server, s.handler
	s.mu.Unlock()
	server.CloseClientConnections()
	server.Close()
	s.StopRunner()
	close(hold)
	s.mu.Lock()
	s.server = httptest.NewServer(s.listen(handler))
	s.mu.Unlock()
	s.Advance(s.Desk.TTL() + time.Minute)
	s.Tick()
	s.StartRunner()
	return true
}

// RunnerOnline says whether this computer's runner is attached.
func (s *Scenario) RunnerOnline() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.quit != nil
}

func (s *Scenario) running() bool {
	s.mu.Lock()
	r := s.runner
	s.mu.Unlock()
	return r != nil && r.Running(s.Job)
}

// Now is the scenario's time.
func (s *Scenario) Now() time.Time { return s.clock.Now() }

// Advance moves the scenario's clock forward.
func (s *Scenario) Advance(d time.Duration) { s.clock.Advance(d) }

// Item is the job as the service stores it.
func (s *Scenario) Item() factory.Item {
	s.T.Helper()
	it, err := s.Store.Get(s.Job)
	if err != nil {
		s.T.Fatalf("read the job: %v", err)
	}
	return it
}

// Change edits the stored job directly. It is for facts no door writes in a
// test (money spent by a model that does not exist here); a scenario that
// could press a door instead should.
func (s *Scenario) Change(change func(*factory.Item)) {
	s.T.Helper()
	if err := s.Store.Update(s.Job, func(it *factory.Item) error { change(it); return nil }); err != nil {
		s.T.Fatalf("change the job: %v", err)
	}
}

func (s *Scenario) git(dir string, args ...string) string {
	s.T.Helper()
	out, err := gitOut(dir, args...)
	if err != nil {
		s.T.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return out
}

func gitOut(dir string, args ...string) (string, error) {
	full := append([]string{"-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false"}, args...)
	if dir != "" {
		full = append([]string{"-C", dir}, full...)
	}
	cmd := exec.Command("git", full...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func (s *Scenario) write(path, text string) {
	s.T.Helper()
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		s.T.Fatal(err)
	}
}

// Pressed is the service's answer to one press of a door.
type Pressed struct {
	Status int
	// Error and Reason are the refusal's own words and its kind, "" on a yes.
	Error  string
	Reason string
	Body   string
}

// OK says the door took the press.
func (p Pressed) OK() bool { return p.Status >= 200 && p.Status < 300 }

func (p Pressed) String() string {
	if p.OK() {
		return strconv.Itoa(p.Status)
	}
	return fmt.Sprintf("%d %s", p.Status, firstOf(p.Error, p.Body))
}

func firstOf(words ...string) string {
	for _, w := range words {
		if strings.TrimSpace(w) != "" {
			return strings.TrimSpace(w)
		}
	}
	return ""
}

// press sends one request to handler as the job's person on this computer.
func press(handler http.Handler, method, path string, body any) Pressed {
	var reader io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		reader = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, path, reader)
	req.RemoteAddr = "127.0.0.1:1234"
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	out := Pressed{Status: rec.Code, Body: strings.TrimSpace(rec.Body.String())}
	var refusal struct {
		Error  string `json:"error"`
		Reason string `json:"reason"`
	}
	if json.Unmarshal(rec.Body.Bytes(), &refusal) == nil {
		out.Error, out.Reason = refusal.Error, refusal.Reason
	}
	return out
}

// Post presses a job door of the service: POST /v1/jobs/{id}/{verb}.
func (s *Scenario) Post(verb string, body any) Pressed {
	return press(s.handler, http.MethodPost, fmt.Sprintf("/v1/jobs/%d/%s", s.Job, verb), body)
}

// Press presses the door of an action the job offers, as its person, and
// answers what the door said.
func (s *Scenario) Press(action string) Pressed {
	s.T.Helper()
	got, known := pressDoor(s.handler, s.Store, s.Item(), action)
	if !known {
		s.T.Fatalf("the scenario knows no door for %s", action)
	}
	return got
}

// MustPress is Press for a press the scenario expects the door to take.
func (s *Scenario) MustPress(action string) {
	s.T.Helper()
	if got := s.Press(action); !got.OK() {
		s.T.Fatalf("%s was refused: %s\n%s", action, got, s.Story())
	}
}

// yesOrder is the order a person who says yes looks for a door in.
var yesOrder = []string{factory.DoApprove, factory.DoMerge, factory.DoAnswer, factory.DoRaiseBudget, factory.DoRetryPublish, factory.DoResume, factory.DoRerunStep, factory.DoResumePipeline, factory.DoStart}

// Yes is the person saying yes to what the job waits on them for: the first
// door the job offers them, in [yesOrder], is pressed. It answers the action.
func (s *Scenario) Yes() string {
	s.T.Helper()
	it := s.Item()
	for _, action := range yesOrder {
		if factory.Offered(it, factory.OfferByYou, action) {
			s.MustPress(action)
			return action
		}
	}
	s.T.Fatalf("the job waits on its person and offers nothing a yes presses: %v\n%s", it.Offers, s.Story())
	return ""
}

// Open starts the job and says yes until its pull request is open: a draft,
// its checks not yet reported, Follow PR watching it. Every rest on the way
// holds the properties.
func (s *Scenario) Open() factory.Item {
	s.T.Helper()
	it := s.Do("start", func() { s.MustPress(factory.DoStart) })
	for step := 0; step < 20; step++ {
		if it.PullRequest != nil && it.State == factory.StateShipped {
			return it
		}
		if it.Wait.Who != factory.WaitYou {
			break
		}
		it = s.Do("the person says yes", func() { s.Yes() })
	}
	s.T.Fatalf("the job did not open its pull request\n%s", s.Story())
	return it
}

// OutsidePush is someone else's commit on the pull request's branch, pushed
// to the remote: the forge's head moves to it. It answers the commit.
func (s *Scenario) OutsidePush() string {
	s.T.Helper()
	pull, ok := s.GitHub.PullRequest()
	if !ok {
		s.T.Fatal("there is no pull request to push to")
	}
	sha := s.commitOn(pull.Head, "other")
	s.GitHub.PushOutside(sha)
	return sha
}

// MoveBase is a commit landing on the base branch: the forge's base moves.
func (s *Scenario) MoveBase() string {
	s.T.Helper()
	sha := s.commitOn("main", "base")
	s.GitHub.MoveBase(sha)
	return sha
}

// commitOn pushes one new commit to branch on the remote from a clone that
// is nobody's worktree, and answers it.
func (s *Scenario) commitOn(branch, name string) string {
	s.T.Helper()
	s.mu.Lock()
	s.edits++
	n := s.edits
	s.mu.Unlock()
	dir := filepath.Join(s.scratch, fmt.Sprintf("clone-%d", n))
	s.git("", "clone", "-q", "--branch", branch, s.bare, dir)
	s.write(filepath.Join(dir, fmt.Sprintf("%s_%d.go", name, n)), "package ledger\n")
	s.git(dir, "add", ".")
	s.git(dir, "commit", "-q", "-m", fmt.Sprintf("%s %d", name, n))
	s.git(dir, "push", "-q", "origin", "HEAD:"+branch)
	sha := s.git(dir, "rev-parse", "HEAD")
	_ = os.RemoveAll(dir)
	return sha
}

// Tick is any request reaching the service, which settles the floor first:
// due approvals, due merges, Auto's own approvals, lost runners.
func (s *Scenario) Tick() {
	press(s.handler, http.MethodGet, "/v1/about", nil)
}

// Poll is one read of GitHub by the factory's poller: the pull request's
// facts are written on the job and a Follow PR round is queued when they ask
// for one.
func (s *Scenario) Poll() {
	s.T.Helper()
	if _, err := factorygithub.PollOnce(context.Background(), s.source, s.Store, "", s.clock.Now); err != nil {
		s.T.Fatalf("poll: %v", err)
	}
}

// held says whether a runner holds the job now.
func (s *Scenario) held() bool {
	live, err := s.Desk.Live()
	if err != nil {
		return false
	}
	for _, l := range live {
		if l.Job == s.Job {
			return true
		}
	}
	return false
}

// takes says this computer's runner is one the service offers the queued job
// to: the job's worktree is here or nowhere yet, and no retry of it is held
// back (lease's eligible).
func (s *Scenario) takes(it factory.Item) bool {
	if it.WorktreeMachine != "" && it.WorktreeMachine != s.machine {
		return false
	}
	if r := it.Recovery; r != nil && (r.State == "waiting" || r.State == "queued" && factory.RecoveryBlockReason(it) != "") {
		return false
	}
	return true
}

// How long a scenario waits for a job to come to rest, how long it must hold
// still to count, and how long a job may read running with nobody holding it
// before the scenario takes that as where it rests.
const (
	settleMost  = 45 * time.Second
	settleStill = 45 * time.Millisecond
	queuedMost  = 1500 * time.Millisecond
	tickEvery   = 30 * time.Millisecond
)

// Settle waits for the job to come to rest and answers it: nothing holds it
// and nothing is about to take it, or its runner is parked on a question.
// Undo windows are waited out by moving the clock, as a person who does not
// press Undo waits them out.
func (s *Scenario) Settle() factory.Item {
	s.T.Helper()
	began := time.Now()
	var stillSince, queuedSince, ticked time.Time
	stillAt := ""
	for {
		// The service settles on a request or on its own timer; the scenario
		// stands in for both with a request every so often, as an open job
		// page does.
		if !s.Quiet && time.Since(ticked) >= tickEvery {
			s.Tick()
			ticked = time.Now()
		}
		it := s.Item()
		held := s.held()
		now := s.clock.Now()
		if !s.Quiet {
			if a := it.Approval; a != nil && a.State == "approving" && now.Before(a.UndoUntil) {
				s.Advance(a.UndoUntil.Sub(now) + time.Millisecond)
				ticked = time.Time{}
				continue
			}
			if m := it.Merging; m != nil && m.State == factory.MergeAsked && now.Before(m.UndoUntil) {
				s.Advance(m.UndoUntil.Sub(now) + time.Millisecond)
				ticked = time.Time{}
				continue
			}
			// A retry the service has planned starts when its wait is over;
			// the scenario does not sit out the seconds.
			if r := it.Recovery; r != nil && r.State == "waiting" && now.Before(r.NextAt) && factory.RecoveryBlockReason(it) == "" {
				s.Advance(r.NextAt.Sub(now) + time.Millisecond)
				ticked = time.Time{}
				continue
			}
		}
		retrying := it.Recovery != nil && (it.Recovery.State == "waiting" || it.Recovery.State == "queued") && factory.RecoveryBlockReason(it) == ""
		rest := false
		switch {
		case held:
			// A held job rests only parked on its person: the runner's round
			// is waiting for an answer and the service has heard so, or the
			// service refused the runner's results and the runner keeps them
			// until it is told to send them again.
			rest = it.State == factory.StateNeedsYou && (s.running() || it.QKind == "results_not_saved")
		case !s.Quiet && it.Approval != nil && it.Approval.State == "approving":
		case !s.Quiet && it.Merging != nil && (it.Merging.State == factory.MergeAsked):
		case it.State == factory.StateQueued && s.RunnerOnline() && !run.StoredPaused(it):
			// A queued job this computer's runner may take is on its way to
			// it; one pinned to another computer rests in the queue.
			rest = !s.takes(it)
		case (it.State == factory.StateRunning || retrying && !s.Quiet) && s.RunnerOnline() && !run.StoredPaused(it):
			// Running with nobody holding it: a hold is being handed over,
			// or it is lost. The scenario gives the first a moment.
			if queuedSince.IsZero() {
				queuedSince = time.Now()
			}
			rest = time.Since(queuedSince) > queuedMost
		default:
			rest = !s.running()
		}
		if it.State != factory.StateRunning && !retrying {
			queuedSince = time.Time{}
		}
		mark := fmt.Sprintf("%v|%v|%s|%s|%d", rest, held, it.State, it.Question, len(logOf(it)))
		if mark != stillAt {
			stillAt, stillSince = mark, time.Now()
		}
		if rest && time.Since(stillSince) >= settleStill {
			return it
		}
		if time.Since(began) > settleMost {
			s.T.Fatalf("the job never came to rest: state %s, held %v, running %v, question %q\n%s", it.State, held, s.running(), it.Question, s.Story())
		}
		time.Sleep(8 * time.Millisecond)
	}
}

func logOf(it factory.Item) []factory.LogLine {
	if it.Stream == nil {
		return nil
	}
	return it.Stream.Log
}

// Story is the scenario so far: its seed, every event, and the job's log.
func (s *Scenario) Story() string {
	var b strings.Builder
	if s.seed != "" {
		fmt.Fprintf(&b, "seed: %s\n", s.seed)
	}
	fmt.Fprintf(&b, "mode: %s\nevents:\n", s.Mode)
	for i, e := range s.Events {
		fmt.Fprintf(&b, "  %2d. %s\n", i+1, e)
	}
	if it, err := s.Store.Get(s.Job); err == nil {
		fmt.Fprintf(&b, "job: state %s, wait %s, offers %v\n", it.State, waitKey(it), it.Offers)
		if f := it.FollowUp; f != nil {
			fmt.Fprintf(&b, "follow: %s (blocked by %q, attempt %d of %d) %s\n", f.State, f.BlockedBy, f.Attempt, f.Limit, f.Reason)
		}
		if it.Question != "" {
			fmt.Fprintf(&b, "question (%s): %s\n", it.QKind, it.Question)
		}
		fmt.Fprintf(&b, "github writes: %s\n", strings.Join(s.GitHub.Writes(), " | "))
		s.mu.Lock()
		for _, r := range s.refused {
			fmt.Fprintf(&b, "the service refused the runner: %s\n", r)
		}
		s.mu.Unlock()
		fmt.Fprintf(&b, "delegate rounds: %s\n", strings.Join(s.Delegate.ran(), " "))
		lines := logOf(it)
		if len(lines) > 25 {
			lines = lines[len(lines)-25:]
		}
		for _, l := range lines {
			fmt.Fprintf(&b, "  log %-4s %s\n", l.Tone, l.Text)
		}
	}
	return b.String()
}

// waitKey is the stored wait in a few words, for a failure to print.
func waitKey(it factory.Item) string {
	w := it.Wait
	if w == nil {
		return "none"
	}
	parts := []string{w.Who}
	for _, p := range []string{w.Action, w.What} {
		if p != "" {
			parts = append(parts, p)
		}
	}
	if w.PutAway {
		parts = append(parts, "put away")
	}
	return strings.Join(parts, ":")
}

// Do is one event of the scenario: what happens, then the job coming to rest,
// then the properties. It answers the job at rest.
func (s *Scenario) Do(name string, event func()) factory.Item {
	s.T.Helper()
	s.Events = append(s.Events, name)
	began := time.Now()
	if event != nil {
		event()
	}
	acted := time.Since(began)
	it := s.Settle()
	settled := time.Since(began)
	s.Events[len(s.Events)-1] = name + " -> " + waitKey(it)
	s.Check()
	if os.Getenv("SCENARIO_TIMES") != "" {
		s.T.Logf("%-40s act %4dms settle %4dms check %4dms", name, acted.Milliseconds(), (settled - acted).Milliseconds(), (time.Since(began) - settled).Milliseconds())
	}
	return it
}
