package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/config"
	homepkg "github.com/Agent-Field/aforge-v2/internal/home"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/router"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

// The whole point of `do` in one test: a headless run is not the static
// pipeline. A deliverable that a reviewer rejects — with the reviewer quoting
// the user's own words for what is missing — does not ship as written. The job
// grows the work that closes the gap, that work runs, and what the caller
// finally reads on stdout is the repaired answer.
//
// Every mechanism here is the resident's: the compiler that turns the ask into
// a goal, the working method written for a task-scale job, the executor, the
// delivery gate, the revision pass, the citation invariant, and the replan that
// splices new work into a live graph. None of it is reachable from a graph that
// was planned once and written to a file, which is exactly why `do` exists.
func TestDoRepairsARejectedDeliverableThroughTheGate(t *testing.T) {
	script := newScriptedBrain(t)
	defer script.close()

	var stdout, stderr strings.Builder
	err := doErrand(doRequest{
		task:      "write the release note and include the migration steps",
		timeout:   60 * time.Second,
		stdout:    &stdout,
		stderr:    &stderr,
		newClient: script.client,
	})
	if err != nil {
		t.Fatalf("the errand did not settle cleanly: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
	}

	// The gate ran on the first draft, again on the revision, and once more on
	// the work the gap bought. Two is the old ceiling; the third is the whole
	// mechanism under test.
	if got := script.count("gate"); got < 3 {
		t.Fatalf("delivery gate ran %d times, want at least 3 (draft, revision, repair)", got)
	}
	if got := script.count("extension"); got != 1 {
		t.Fatalf("the extension leaf ran %d times, want exactly 1", got)
	}
	if got := script.count("contract"); got == 0 {
		t.Fatal("no working method was written — a task-scale job must still get its contract")
	}
	if got := script.count("replan"); got == 0 {
		t.Fatal("the gap never reached the remainder planner")
	}
	if !strings.Contains(stdout.String(), repairedAnswer) {
		t.Fatalf("stdout does not carry the repaired deliverable:\n%s", stdout.String())
	}
	if strings.Contains(stdout.String(), firstDraftAnswer) {
		t.Fatalf("stdout shipped the rejected first draft:\n%s", stdout.String())
	}
}

// The private store is the default because isolation is the point, and a
// default that leaves a database behind on every invocation is a mess nobody
// asked for. --keep is the way to look at what happened.
func TestDoDeletesItsPrivateStoreUnlessKept(t *testing.T) {
	for _, keep := range []bool{false, true} {
		t.Run(fmt.Sprintf("keep=%v", keep), func(t *testing.T) {
			script := newScriptedBrain(t)
			defer script.close()
			var stdout, stderr strings.Builder
			if err := doErrand(doRequest{
				task: "write the release note and include the migration steps", keep: keep,
				timeout: 60 * time.Second, stdout: &stdout, stderr: &stderr, newClient: script.client,
			}); err != nil {
				t.Fatalf("errand: %v\n%s", err, stderr.String())
			}
			home := keptHome(stderr.String())
			if !keep {
				if home != "" {
					t.Fatalf("an ephemeral run announced a kept store: %q", stderr.String())
				}
				return
			}
			if home == "" {
				t.Fatalf("--keep never said where the store is:\n%s", stderr.String())
			}
			defer os.RemoveAll(home)
			if _, err := os.Stat(filepath.Join(home, "graph.db")); err != nil {
				t.Fatalf("--keep did not keep the store: %v", err)
			}
		})
	}
}

// --json is the machine shape and it is the whole of stdout: a caller piping
// this into jq must not have to strip a footer off the front of it.
func TestDoJSONCarriesTheWholeOutcome(t *testing.T) {
	script := newScriptedBrain(t)
	defer script.close()
	var stdout, stderr strings.Builder
	if err := doErrand(doRequest{
		task: "write the release note and include the migration steps", asJSON: true,
		timeout: 60 * time.Second, stdout: &stdout, stderr: &stderr, newClient: script.client,
	}); err != nil {
		t.Fatalf("errand: %v\n%s", err, stderr.String())
	}
	var outcome struct {
		Deliverable string   `json:"deliverable"`
		Artifacts   []string `json:"artifacts"`
		Spend       float64  `json:"spend"`
		Nodes       int      `json:"nodes"`
		Seconds     float64  `json:"seconds"`
		Settled     bool     `json:"settled"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &outcome); err != nil {
		t.Fatalf("stdout is not one JSON object: %v\n%s", err, stdout.String())
	}
	if !outcome.Settled {
		t.Fatal("a run that finished reported itself unsettled")
	}
	if !strings.Contains(outcome.Deliverable, repairedAnswer) {
		t.Fatalf("json deliverable is not the repaired one: %q", outcome.Deliverable)
	}
	if outcome.Nodes < 2 {
		t.Fatalf("json node count = %d, want the original job and its extension", outcome.Nodes)
	}
	if outcome.Seconds <= 0 {
		t.Fatal("json reported no elapsed time")
	}
}

// A wall that arrives first is not a failure and not a success: what exists is
// printed, and the exit code says it is a partial.
func TestDoTimesOutWithAPartialAndCodeTwo(t *testing.T) {
	script := newScriptedBrain(t)
	script.stall = true
	defer script.close()
	var stdout, stderr strings.Builder
	err := doErrand(doRequest{
		task: "write the release note and include the migration steps",
		// Long enough for the compile to land and the leaf to start, short
		// enough that the leaf is still in the model call when the wall comes.
		timeout: 2 * time.Second, stdout: &stdout, stderr: &stderr, newClient: script.client,
	})
	var status exitStatus
	if !asExitStatus(err, &status) || status != exitTimeout {
		t.Fatalf("timeout exit = %v, want exit status 2", err)
	}
	if strings.TrimSpace(stdout.String()) == "" {
		t.Fatal("a timeout printed nothing at all")
	}
}

// The price is quoted before the money moves, and a desk with nobody standing
// at it may not answer for the person. Without --yes-spend the run stops and
// says what it would have cost.
func TestDoRefusesToBuyAPlanOverTheConsentThreshold(t *testing.T) {
	script := newScriptedBrain(t)
	defer script.close()
	// A cent of consent threshold and a measured journal cost puts every plan
	// over the line, which is the condition under test.
	t.Setenv("AFORGE_PLAN_CONSENT", "0.01")
	script.leafCost = 1.0

	var stdout, stderr strings.Builder
	err := doErrand(doRequest{
		task:    "write the release note and include the migration steps",
		timeout: 20 * time.Second, stdout: &stdout, stderr: &stderr, newClient: script.client,
	})
	var status exitStatus
	if !asExitStatus(err, &status) || status != exitFailed {
		t.Fatalf("refused spend exit = %v, want exit status 1", err)
	}
	if !strings.Contains(stderr.String(), "--yes-spend") {
		t.Fatalf("the refusal never named the way to approve it:\n%s", stderr.String())
	}
	if !strings.Contains(stderr.String(), "$") {
		t.Fatalf("the refusal never quoted a price:\n%s", stderr.String())
	}
}

// -w is where the files go, and it has to survive the store: the whole reason
// to name a directory is that the private database is about to be deleted.
func TestDoLeavesArtifactsUnderTheNamedWorkspace(t *testing.T) {
	script := newScriptedBrain(t)
	script.writeFile = true
	defer script.close()
	workspace := t.TempDir()
	var stdout, stderr strings.Builder
	if err := doErrand(doRequest{
		task: "write the release note and include the migration steps", workspace: workspace,
		timeout: 60 * time.Second, stdout: &stdout, stderr: &stderr, newClient: script.client,
	}); err != nil {
		t.Fatalf("errand: %v\n%s", err, stderr.String())
	}
	var found string
	_ = filepath.Walk(workspace, func(path string, info os.FileInfo, err error) error {
		if err == nil && info != nil && !info.IsDir() && filepath.Base(path) == "notes.md" {
			found = path
		}
		return nil
	})
	if found == "" {
		t.Fatalf("nothing the job wrote survived under %s", workspace)
	}
	if !strings.Contains(stdout.String(), found) {
		t.Fatalf("the footer never named the file it left behind:\n%s", stdout.String())
	}
}

// The defect this fixes cost a benchmark run its whole point. Sent at a
// project with `-w`, the errand worked in a freshly created empty subdirectory
// of it: the file it was told to fix was not there to read, so it invented a
// module from nothing, tested its invention, and reported success while the
// person's file sat byte-identical beside it.
//
// The directory a person names IS the working directory. The proof is the edit
// tool, which replaces an exact string in an existing file: it can only succeed
// if the real file was visible from where the leaf ran.
func TestDoEditsTheNamedDirectoryInPlace(t *testing.T) {
	script := newScriptedBrain(t)
	script.editPath = "intervals.py"
	defer script.close()

	workspace := t.TempDir()
	target := filepath.Join(workspace, script.editPath)
	if err := os.WriteFile(target, []byte(originalSource), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr strings.Builder
	if err := doErrand(doRequest{
		task: "fix the failing test in intervals.py", workspace: workspace,
		timeout: 60 * time.Second, stdout: &stdout, stderr: &stderr, newClient: script.client,
	}); err != nil {
		t.Fatalf("errand: %v\n%s", err, stderr.String())
	}
	if script.count("edited") == 0 {
		t.Fatalf("the leaf never reached the file it was sent to fix:\n%s", stderr.String())
	}

	after, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(after), fixedLine) {
		t.Fatalf("%s was not edited in place:\n%s", target, string(after))
	}
	if strings.Contains(string(after), brokenLine) {
		t.Fatalf("the broken line survived the edit:\n%s", string(after))
	}

	// Nothing of the engine's may remain in someone's project. The scratch
	// directories are the second-order half of the same defect: leftover
	// task-2/ folders broke the user's own pytest run with a duplicate module
	// basename collection error.
	entries, err := os.ReadDir(workspace)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			t.Fatalf("the run left %s/ inside the person's directory", entry.Name())
		}
		if entry.Name() != script.editPath {
			t.Fatalf("the run left %s beside the person's files", entry.Name())
		}
	}
}

// Saying nothing means here, which is what every other agent a person runs
// from a terminal means by it.
func TestErrandWorkspaceDefaultsToTheCurrentDirectory(t *testing.T) {
	here, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	got, err := errandWorkspace("")
	if err != nil {
		t.Fatal(err)
	}
	if got != here {
		t.Fatalf("default workspace = %q, want the process directory %q", got, here)
	}
	// A named one is resolved against the same place rather than left relative,
	// because the leaf that will use it does not run from here.
	named, err := errandWorkspace("sub/dir")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(here, "sub", "dir"); named != want {
		t.Fatalf("named workspace = %q, want %q", named, want)
	}
}

// A chat window is the other half of the same seam and must not have moved.
// One thread hosts many unrelated jobs, so each still gets its own directory
// under the store's workspace; only an errand shares one.
func TestChatKeepsItsPerJobWorkspaceLayout(t *testing.T) {
	script := newScriptedBrain(t)
	defer script.close()
	root := t.TempDir()
	window := testWindow(t, root)
	brain, err := buildBrain(window, "s1", brainOptions{newClient: script.client})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(brain.closeAll)
	if brain.workspaceRoot != filepath.Join(root, "workspace") {
		t.Fatalf("chat workspace root = %q, want the store's own", brain.workspaceRoot)
	}
	// The commander resolves a node to its own job directory beneath that root,
	// which is the layout the whole chat surface reads through.
	if err := window.graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "job", Brief: "produce the artifact", Stage: 0},
	}}, store.Provenance{Origin: store.OriginUser, Intent: "produce the artifact"}); err != nil {
		t.Fatal(err)
	}
	jobDir := filepath.Join(brain.workspaceRoot, "job")
	if err := os.MkdirAll(jobDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if got, ok := brain.commander.WorkspacePath("job"); !ok || got != jobDir {
		t.Fatalf("chat job workspace = (%q, %v), want (%q, true)", got, ok, jobDir)
	}
}

// stderr is the only window a person has into a headless run, and it was
// shut. Every line it printed hung off a node changing status, a node's first
// status is Pending, and Pending is skipped — so a run whose leaf was never
// claimed printed nothing at all for the whole of its life and then exited 2.
// The ask becoming work is said out loud now, before any leaf moves.
func TestDoReportsItsProgressOnStderr(t *testing.T) {
	script := newScriptedBrain(t)
	defer script.close()
	var stdout, stderr strings.Builder
	if err := doErrand(doRequest{
		task: "write the release note and include the migration steps", workspace: t.TempDir(),
		timeout: 60 * time.Second, stdout: &stdout, stderr: &stderr, newClient: script.client,
	}); err != nil {
		t.Fatalf("errand: %v\n%s", err, stderr.String())
	}
	said := stderr.String()
	understood := strings.Index(said, "understood")
	if understood < 0 {
		t.Fatalf("stderr never said the ask became work:\n%s", said)
	}
	landed := strings.Index(said, statusMark(store.Done))
	if landed < 0 {
		t.Fatalf("stderr never reported a node landing:\n%s", said)
	}
	if understood > landed {
		t.Fatalf("the structure was announced after the work finished:\n%s", said)
	}
}

// A run that has stopped producing evidence has to account for itself. This is
// the trace that prompted it: fifteen minutes of a completely empty terminal
// behind a leaf that was never claimed, then exit 2. One structural read — no
// model call, no flag — turns an invisible hang into a diagnosable one.
func TestDoSaysWhatItIsWaitingOnWhenNothingMoves(t *testing.T) {
	root := t.TempDir()
	graph, err := store.Open(filepath.Join(root, "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer graph.Close()
	session := "headless-quiet"
	command, err := graph.RequestCommand(store.Command{
		SessionID: session, Kind: store.CommandSplice, Instruction: "fix the failing test",
	})
	if err != nil {
		t.Fatal(err)
	}
	// One task, admitted to this errand and never claimed by anyone — the
	// shape of the wedged run exactly.
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "task-1", Brief: "fix the failing test", Stage: 0},
	}}, store.Provenance{Origin: store.OriginUser, SessionID: session, Intent: "fix the failing test"}); err != nil {
		t.Fatal(err)
	}

	var progress strings.Builder
	watcher := &settlementWatch{
		graph: graph, session: session, commandSeq: command.Seq,
		refused: make(chan planEstimate, 1), progress: &progress,
		started: time.Now(), quiet: 50 * time.Millisecond,
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := watcher.wait(ctx); err != nil {
		t.Fatal(err)
	}
	said := progress.String()
	for _, phrase := range []string{"still waiting", "1 task pending", "none running"} {
		if !strings.Contains(said, phrase) {
			t.Fatalf("the quiet line never said %q:\n%s", phrase, said)
		}
	}
}

// The factoring itself: one construction, two shapes. A chat window still gets
// every piece it ever had, and headless differs by exactly the conversational
// half — no head, no commander, no stream, no arrival brief — over an
// identically wired reconciler, runner and consent desk.
func TestBuildBrainSeparatesTheConversationFromTheWork(t *testing.T) {
	script := newScriptedBrain(t)
	defer script.close()

	for _, shape := range []struct {
		name     string
		headless bool
	}{{"chat", false}, {"headless", true}} {
		t.Run(shape.name, func(t *testing.T) {
			window := testWindow(t, t.TempDir())
			brain, err := buildBrain(window, "s1", brainOptions{
				headless: shape.headless, ephemeral: shape.headless, newClient: script.client,
			})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(brain.closeAll)
			// The work half is the same object either way. This is the whole
			// claim the headless mode rests on.
			if brain.reconciler == nil || brain.runner == nil || brain.consent == nil {
				t.Fatal("the working half is incomplete")
			}
			if shape.headless {
				if brain.serveHead != nil || brain.commander != nil ||
					brain.streamEvents != nil || brain.deliverBrief != nil {
					t.Fatal("a headless brain carries a conversation it cannot have")
				}
				return
			}
			if brain.serveHead == nil || brain.commander == nil || brain.streamEvents == nil {
				t.Fatal("a chat brain lost part of its conversation to the factoring")
			}
		})
	}
}

// AFORGE_HOME moves the whole home in one word — the seam a harness runs a
// fleet of isolated aforges through.
func TestAforgeHomeMovesTheDefaultStore(t *testing.T) {
	home := t.TempDir()
	t.Setenv(homepkg.EnvVar, home)
	if got, want := defaultChatDB(), filepath.Join(home, "graph.db"); got != want {
		t.Fatalf("default store = %q, want %q", got, want)
	}
}

// ---------------------------------------------------------------------------
// The scripted brain: one HTTP endpoint standing in for every model call the
// run makes, dispatching on the prompt that arrived. It is deliberately not a
// stub of aforge's own seams — the real compiler, planner, executor, gate and
// replan all run, and this only decides what the model says back to them.

const (
	firstDraftAnswer = "RELEASE NOTE DRAFT: the parser is faster."
	repairedAnswer   = "RELEASE NOTE FINAL: the parser is faster, and here are the migration steps."
	// citedGap quotes the user's own words, which is the one thing that lets a
	// gap commission new work. A gap that invented a requirement would be
	// refused before a planning call was made.
	citedQuote = "include the migration steps"
	// artifactName is what the worker writes when the test asks it to leave
	// something on disk.
	artifactName = "notes.md"
	// The in-place edit: a file that already exists in the person's directory,
	// with one line the worker is scripted to replace. The edit tool requires
	// the old text to be found, so a successful edit is proof the real file was
	// where the leaf was standing.
	brokenLine     = "return start <= other.end and other.start < end"
	fixedLine      = "return start <= other.end and other.start <= end"
	originalSource = "def overlaps(start, end, other):\n    " + brokenLine + "\n"
)

type scriptedBrain struct {
	t      *testing.T
	server *httptest.Server
	dir    string

	// stall makes every leaf call hang, so a wall can be proved.
	stall bool
	// writeFile makes the first leaf write a real artifact.
	writeFile bool
	// editPath names a file already in the workspace that the first leaf edits
	// in place, which is what a coding errand actually does.
	editPath string
	// leafCost is what each call reports spending, which is what the consent
	// desk's estimate is built from.
	leafCost float64

	mu     sync.Mutex
	counts map[string]int
}

func newScriptedBrain(t *testing.T) *scriptedBrain {
	t.Helper()
	script := &scriptedBrain{t: t, dir: t.TempDir(), counts: map[string]int{}}
	script.server = httptest.NewServer(http.HandlerFunc(script.serve))
	// The catalog, the media clients and anything else that reaches for an
	// endpoint find this one; none of them are what is under test, and all of
	// them degrade cleanly against a server that has no answers for them.
	t.Setenv("AFORGE_BASE_URL", script.server.URL)
	t.Setenv("OPENROUTER_API_KEY", "test-key")
	t.Setenv("AFORGE_PROFILE_DIR", script.dir)
	t.Setenv("AFORGE_DAILY_BUDGET", "0")
	t.Setenv("AFORGE_PRACTICE_BUDGET", "0")
	if os.Getenv("AFORGE_PLAN_CONSENT") == "" {
		t.Setenv("AFORGE_PLAN_CONSENT", "0")
	}
	return script
}

func (s *scriptedBrain) close() { s.server.Close() }

func (s *scriptedBrain) count(name string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.counts[name]
}

func (s *scriptedBrain) tally(name string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.counts[name]++
	return s.counts[name]
}

// client is the injection seam buildBrain takes: every slot in the brain gets a
// one-rung panel pointed at the scripted endpoint. One rung matters — it is
// what keeps the executor from escalating a leaf to a second model and doubling
// every count this test reads.
func (s *scriptedBrain) client(settings config.Config, model string) (*liveClient, error) {
	panel, err := router.New(router.Panel{Models: []router.Spec{{Slug: model, Price: 0.01}}},
		provider.Config{APIKey: "test-key", BaseURL: s.server.URL}, s.dir)
	if err != nil {
		return nil, err
	}
	settings.Model = model
	return &liveClient{settings: settings, model: model, client: panel}, nil
}

func (s *scriptedBrain) serve(writer http.ResponseWriter, request *http.Request) {
	if !strings.HasSuffix(request.URL.Path, "/chat/completions") {
		http.Error(writer, `{"error":"no"}`, http.StatusNotFound)
		return
	}
	raw, err := io.ReadAll(request.Body)
	if err != nil {
		http.Error(writer, `{"error":"unreadable"}`, http.StatusBadRequest)
		return
	}
	body := string(raw)
	if s.stall && strings.Contains(body, "You complete one piece of work, alone, using tools") {
		s.tally("stalled")
		// Wedged, but not wedged past the test: the client's own context ends
		// this the moment the wall arrives, and the handler lets go with it so
		// the server can close.
		select {
		case <-request.Context().Done():
		case <-time.After(30 * time.Second):
		}
		return
	}
	writer.Header().Set("Content-Type", "application/json")
	fmt.Fprint(writer, s.reply(body))
}

// reply is the whole script, in the order the run reaches it.
func (s *scriptedBrain) reply(body string) string {
	switch {
	case strings.Contains(body, "You are the intent compiler"):
		s.tally("compile")
		// Task scale: one worker end to end, which is the shape that still
		// earns a written working method and still faces the gate.
		return s.say(`{"goal":"Write the release note for the parser work, including the migration steps.",` +
			`"scale":"task","builds_on":[],"assumptions":[],"question":"","trial_of":0}`)

	case strings.Contains(body, "You write the working method for one agent"):
		s.tally("contract")
		return s.say(`{"contract":"Read the changelog first. Done means the note names every migration step a reader has to take."}`)

	case strings.Contains(body, "You name jobs for a narrow task list"):
		s.tally("title")
		return s.say("Release note and migration")

	case strings.Contains(body, "You break a goal into its ordered stages"),
		strings.Contains(body, "settled points"):
		// The remainder planner's opening pass. Refusing it here proves the
		// documented fallback — one fresh worker on the remainder — rather than
		// leaving the replan untested when a planner is unavailable.
		s.tally("replan")
		return s.say("no plan today")

	case strings.Contains(body, "You are the final gate"):
		round := s.tally("gate")
		if round <= 2 {
			// The first draft and the revision of it are both judged short of
			// the ask, and the gap quotes the ask itself — the one thing that
			// buys another round of real work.
			return s.say(fmt.Sprintf(
				`{"pass":false,"gaps":"the migration steps are missing","quote":%q,"exercised":false}`, citedQuote))
		}
		return s.say(`{"pass":true,"gaps":"","quote":"","exercised":true}`)

	case strings.Contains(body, "You judge whether a finished job taught"):
		s.tally("distill")
		return s.say(`{"facts":[]}`)

	case strings.Contains(body, "You complete one piece of work, alone, using tools"):
		return s.leaf(body)
	}
	// Anything else the resident asks about itself gets a shrug it can absorb.
	s.tally("other")
	return s.say("{}")
}

// leaf answers as the worker. Which worker it is reads off the inputs it was
// given, which is how the product itself distinguishes the three: a first
// draft, the revision the gate's critique bought, and the work the cited gap
// commissioned.
func (s *scriptedBrain) leaf(body string) string {
	switch {
	case strings.Contains(body, "Finish work a previous agent started"):
		s.tally("extension")
		return s.say(repairedAnswer)
	case strings.Contains(body, "A reviewer compared the previous attempt"):
		s.tally("revision")
		return s.say(firstDraftAnswer + " (revised, still nothing about migrating)")
	// The first leaf turn edits; the task itself names the file, so the guard
	// counts turns rather than looking for the path in the transcript.
	case s.editPath != "" && s.count("edited") == 0:
		s.tally("edited")
		return s.tool("edit", fmt.Sprintf(`{"path":%q,"old":%q,"new":%q}`, s.editPath, brokenLine, fixedLine))
	case s.writeFile && !strings.Contains(body, artifactName):
		// The honest way to leave a file behind is the tool the product gives
		// the worker for it, so the artifact reaches the outcome the way every
		// real artifact does rather than by being asserted into existence.
		s.tally("wrote")
		return s.tool("write", fmt.Sprintf(`{"path":%q,"text":"migration steps go here"}`, artifactName))
	default:
		s.tally("draft")
		return s.say(firstDraftAnswer)
	}
}

func (s *scriptedBrain) say(content string) string {
	encoded, _ := json.Marshal(content)
	return fmt.Sprintf(`{"model":"scripted","choices":[{"index":0,"finish_reason":"stop",`+
		`"message":{"role":"assistant","content":%s}}],`+
		`"usage":{"prompt_tokens":10,"completion_tokens":10,"total_tokens":20,"cost":%f}}`,
		string(encoded), s.leafCost)
}

func (s *scriptedBrain) tool(name, arguments string) string {
	encoded, _ := json.Marshal(arguments)
	return fmt.Sprintf(`{"model":"scripted","choices":[{"index":0,"finish_reason":"tool_calls",`+
		`"message":{"role":"assistant","content":"","tool_calls":[{"id":"call-1","type":"function",`+
		`"function":{"name":%q,"arguments":%s}}]}}],`+
		`"usage":{"prompt_tokens":10,"completion_tokens":10,"total_tokens":20,"cost":%f}}`,
		name, string(encoded), s.leafCost)
}

func asExitStatus(err error, status *exitStatus) bool {
	coded, ok := err.(exitStatus)
	if !ok {
		return false
	}
	*status = coded
	return true
}

func keptHome(stderr string) string {
	for _, line := range strings.Split(stderr, "\n") {
		if strings.HasPrefix(line, "store kept at ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "store kept at "))
		}
	}
	return ""
}
