//go:build e2e

package e2e

// The local-files journey for ongoing work, driven entirely through the
// shipped binary's own doors: `aforge collections` and `aforge standing`, with
// `aforge standing check` running the same pass the background timer runs.
//
// THE MODEL IS A SCRIPT, AND THAT IS WHAT THIS TEST CAN AND CANNOT PROVE. It
// answers from what the engine actually sent it — the brief, the change list,
// the rules in the system prompt, the file a tool read — so every assertion
// about causes, versions, rules reaching work, pause/stop, retries and published
// reports is about aforge and not about a model's judgment. It is NOT live-model
// acceptance: nothing here shows a real model writes a useful report or obeys a
// rule. `scripts/demo-local-work.sh` is the live run.
//
// No API key, no network beyond loopback, no connector. It FAILS rather than
// skips without bin/aforge, because a skip here was never evidence of anything.

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/config"
)

// The two rule markers. Each rule's words carry one, so the script can say
// which rules actually reached the prompt it was sent.
const (
	ruleLaunch    = "RULE-LAUNCH-7"
	ruleMarketing = "RULE-MKT-9"
	reportMarker  = "REPORT: your FINAL REPLY"
)

func TestLocalWorkJourney(t *testing.T) {
	binary := filepath.Join(repoRoot(t), "bin", "aforge")
	if _, err := os.Stat(binary); err != nil {
		t.Fatal("bin/aforge is missing: run make build first (this journey drives the shipped binary)")
	}
	model := newScriptedModel(t)
	home, project := keptDir(t, "home"), keptDir(t, "project")
	profile, _ := json.Marshal(map[string]string{config.KeyMemoryEnabled: "off", config.KeyStandingBackground: "off"})
	must(t, os.WriteFile(filepath.Join(home, "config.json"), profile, 0o600))
	for _, dir := range []string{"inbox", "product", "marketing"} {
		must(t, os.MkdirAll(filepath.Join(project, dir), 0o755))
	}
	must(t, os.WriteFile(filepath.Join(project, "product", "spec.md"), []byte("Offline support: supported.\n"), 0o644))
	copyText := "Launch copy: works offline, anywhere.\n"
	must(t, os.WriteFile(filepath.Join(project, "marketing", "launch-copy.md"), []byte(copyText), 0o644))

	j := &journey{t: t, binary: binary, home: home, project: project, env: scriptedEnv(home, model.url)}

	// ── folders and the rules placed on them ─────────────────────────────
	launch := j.folder("Launch")
	marketing := j.folder("Marketing")
	j.ok("standing", "add", "--hold", "--words", ruleLaunch+": inbox reports never quote email addresses; write [redacted] instead.", "--scope", launch, "--workspace", project)
	j.ok("standing", "add", "--hold", "--words", ruleMarketing+": marketing notes must not promise offline support unless product/spec.md says it is supported.", "--scope", marketing, "--workspace", project)

	// A report inside its own watch is refused before anything stands.
	if out, err := j.run("standing", "add", "--words", "loop", "--instructions", "x", "--watch", "reports/*", "--report", "reports/x.md", "--workspace", project); err == nil || !strings.Contains(out, "would wake it again") {
		t.Fatalf("a self-waking report was not refused: err=%v\n%s", err, out)
	}

	// ── the ongoing work, set up whole at the terminal ───────────────────
	inbox := j.addWork("Keep an eye on the inbox folder and keep reports/inbox-report.md current.",
		"Read the changed files in inbox/ and write a short report of new decisions and requests. FOCUS: decisions",
		"inbox/*", "reports/inbox-report.md", launch)

	// The first reading is the baseline: nothing runs.
	j.check()
	j.expectRuns(inbox, 0)

	// A new file wakes it; the run consumed version 1 and published the report.
	j.write("inbox/a.md", "Decision: ship Friday. Ask alice@example.com to confirm the venue.\n")
	j.check()
	first := j.expectRuns(inbox, 1)[0]
	j.expectRun(first, runExpect{spec: 1, attempt: 1, outcome: "landed", changes: []string{"added inbox/a.md"}})
	report := j.read("reports/inbox-report.md")
	for _, want := range []string{"FOCUS: decisions", "added inbox/a.md", "rules seen: " + ruleLaunch, "read: Decision: ship Friday"} {
		if !strings.Contains(report, want) {
			t.Fatalf("report lacks %q:\n%s", want, report)
		}
	}
	if strings.Contains(report, ruleMarketing) {
		t.Fatalf("the Marketing rule reached Launch work:\n%s", report)
	}
	j.expectPublished(first, "reports/inbox-report.md")
	j.expectCause(first, []string{ruleLaunch}, []string{ruleMarketing})
	// THE RULE IS KEPT IN WHAT IS PUBLISHED, not only shown to the run. The
	// script's first draft quotes the address, as the live model's did; the
	// check before publication found it, the run was sent back once with the
	// quote, and only the corrected report was published.
	if strings.Contains(report, "alice@example.com") || !strings.Contains(report, "[redacted]") {
		t.Fatalf("a raw contact detail survived the Launch rule:\n%s", report)
	}
	if c := first.RuleCheck; c == nil || c.Verdict != "kept" || !c.Rewrote || !strings.Contains(c.First, "alice@example.com") {
		t.Fatalf("the first report's rule check: %+v", first.RuleCheck)
	}

	// Nothing changed: another pass must not run or publish again.
	calls := model.firings.Load()
	j.check()
	j.expectRuns(inbox, 1)
	if model.firings.Load() != calls {
		t.Fatal("an unchanged folder cost another model call")
	}

	// ── an edit reaches the NEXT occurrence, and history keeps the old one ─
	j.ok("standing", "edit", inbox, "--instructions", "Read the changed files in inbox/ and list who owns each open request. FOCUS: owners")
	j.append("inbox/a.md", "Request: Bob owns the press release.\n")
	j.check()
	runs := j.expectRuns(inbox, 2)
	j.expectRun(runs[0], runExpect{spec: 2, attempt: 1, outcome: "landed", changes: []string{"modified inbox/a.md"}})
	if !strings.Contains(runs[1].Brief, "FOCUS: decisions") || !strings.Contains(runs[0].Brief, "FOCUS: owners") {
		t.Fatalf("occurrence briefs do not keep their own versions: %q / %q", runs[1].Brief, runs[0].Brief)
	}
	if report = j.read("reports/inbox-report.md"); !strings.Contains(report, "FOCUS: owners") {
		t.Fatalf("the edit did not reach the next report:\n%s", report)
	}

	// ── pause holds new work back without losing the change ───────────────
	j.ok("standing", "pause", inbox)
	j.write("inbox/b.md", "Decision: budget capped at 5k.\n")
	j.check()
	j.expectRuns(inbox, 2)
	j.ok("standing", "resume", inbox)
	j.check()
	runs = j.expectRuns(inbox, 3)
	j.expectRun(runs[0], runExpect{spec: 2, attempt: 1, outcome: "landed", changes: []string{"added inbox/b.md"}})

	// ── a process killed mid-run: the retry finishes the SAME occurrence ─
	model.hang.Store(true)
	j.write("inbox/c.md", "Request: legal review by Tuesday.\n")
	killed := j.start("standing", "check")
	select {
	case <-model.hanging:
	case <-time.After(90 * time.Second):
		t.Fatal("the firing never reached the model")
	}
	must(t, killed.Process.Signal(syscall.SIGKILL))
	_ = killed.Wait()
	model.hang.Store(false)
	runs = j.expectRuns(inbox, 4)
	if runs[0].Phase != "admitted" || runs[0].Published != nil {
		t.Fatalf("the killed run should be admitted and unpublished: %+v", runs[0].occurrenceView)
	}
	before := j.read("reports/inbox-report.md")
	j.check()
	runs = j.expectRuns(inbox, 5)
	j.expectRun(runs[0], runExpect{spec: 2, attempt: 2, outcome: "landed", changes: []string{"added inbox/c.md"}})
	if len(runs[0].Supersedes) != 1 || filepath.Base(runs[0].Supersedes[0]) != filepath.Base(runs[1].RunDir) {
		t.Fatalf("the retry does not name the interrupted attempt: %+v", runs[0].Supersedes)
	}
	if runs[1].Phase != "interrupted" || runs[1].SupersededBy != runs[0].ID {
		t.Fatalf("the killed attempt is not marked interrupted: %+v", runs[1].occurrenceView)
	}
	after := j.read("reports/inbox-report.md")
	if after == before || !strings.Contains(after, "added inbox/c.md") {
		t.Fatalf("the retry did not publish the interrupted occurrence:\n%s", after)
	}
	j.check()
	j.expectRuns(inbox, 5)

	// ── a report that still breaks the rule is held back, and says so ─────
	// The script will not correct a note marked KEEP-RAW, so the check finds
	// the address again after the one correction. Nothing is published, the
	// pass exits unanswered rather than 0, and the next run is told about the
	// file the held run never reported.
	kept := j.read("reports/inbox-report.md")
	j.write("inbox/f.md", "Request: KEEP-RAW ask bob@example.com for the keys.\n")
	out, code := j.checkExit()
	if code != 4 || !strings.Contains(out, "1 need you") || !strings.Contains(out, "not finished: aforge standing show "+inbox) {
		t.Fatalf("a held report did not make the check exit unanswered (4): code %d\n%s", code, out)
	}
	runs = j.expectRuns(inbox, 6)
	if o := runs[0]; o.Phase != "finished" || o.Outcome != "needs-you" || o.Published != nil || o.RuleCheck == nil || o.RuleCheck.Verdict != "broken" || o.RuleCheck.Held == "" || !o.RuleCheck.Rewrote {
		t.Fatalf("the held run: %+v checked %+v", o.occurrenceView, o.RuleCheck)
	}
	if got := j.read("reports/inbox-report.md"); got != kept || strings.Contains(got, "bob@example.com") {
		t.Fatalf("a held report replaced the last good one:\n%s", got)
	}
	if show := j.ok("standing", "show", inbox); !strings.Contains(show, "held back, not published") || !strings.Contains(show, "waiting on you:") {
		t.Fatalf("show does not say the report was held:\n%s", show)
	}
	// The person fixes the note; a new note arrives. The run lists both.
	j.write("inbox/f.md", "Request: ask Bob for the keys.\n")
	j.write("inbox/g.md", "Decision: keys handed over Monday.\n")
	j.check()
	runs = j.expectRuns(inbox, 7)
	j.expectRun(runs[0], runExpect{spec: 2, attempt: 1, outcome: "landed", changes: []string{"added inbox/f.md", "added inbox/g.md"}})
	if got := j.read("reports/inbox-report.md"); strings.Contains(got, "bob@example.com") || !strings.Contains(got, "added inbox/f.md") {
		t.Fatalf("the run after the held one:\n%s", got)
	}

	// ── Product → Marketing on the same primitives ────────────────────────
	review := j.addWork("When the product spec changes, review the launch copy against it.",
		"Compare marketing/launch-copy.md with the changed product files and list claims they no longer support. Do not edit the copy. FOCUS: claims",
		"product/*", "marketing/review-notes.md", marketing)
	// A REFERENCE files the inbox work under Marketing too; it must not
	// acquire Marketing's rule, because only a placement governs.
	j.ok("collections", "add", marketing, "standing", inbox)
	j.check() // baseline for the review
	j.expectRuns(review, 0)
	j.write("product/spec.md", "Offline support: removed in this release.\n")
	j.write("inbox/d.md", "Decision: keep the Friday date.\n")
	j.check()
	reviews := j.expectRuns(review, 1)
	j.expectRun(reviews[0], runExpect{spec: 1, attempt: 1, outcome: "landed", changes: []string{"modified product/spec.md"}})
	notes := j.read("marketing/review-notes.md")
	if !strings.Contains(notes, "rules seen: "+ruleMarketing) || strings.Contains(notes, ruleLaunch) {
		t.Fatalf("review notes did not get exactly the Marketing rule:\n%s", notes)
	}
	j.expectCause(reviews[0], []string{ruleMarketing}, []string{ruleLaunch})
	runs = j.expectRuns(inbox, 8)
	j.expectCause(runs[0], []string{ruleLaunch}, []string{ruleMarketing})
	if got := j.read("marketing/launch-copy.md"); got != copyText {
		t.Fatalf("the review changed the copy it was only asked to review:\n%s", got)
	}

	// ── stop is final ─────────────────────────────────────────────────────
	j.ok("standing", "stop", inbox)
	j.write("inbox/e.md", "Decision: nothing after stop.\n")
	j.check()
	j.expectRuns(inbox, 8)
	if out, err := j.run("standing", "resume", inbox); err == nil || !strings.Contains(out, "set up afresh") {
		t.Fatalf("a stopped item resumed: %v\n%s", err, out)
	}

	// A LAST PASS WITH NOTHING NEW RUNS NOTHING AND PUBLISHES NOTHING TWICE.
	before = j.read("reports/inbox-report.md")
	notesBefore := j.read("marketing/review-notes.md")
	if out := j.ok("standing", "check"); !strings.Contains(out, "nothing was due") && !strings.Contains(out, "checked") {
		t.Fatalf("the idle pass: %s", out)
	}
	j.expectRuns(inbox, 8)
	j.expectRuns(review, 1)
	if j.read("reports/inbox-report.md") != before || j.read("marketing/review-notes.md") != notesBefore {
		t.Fatal("an idle pass rewrote a report")
	}

	// News for work with no conversation behind it waits in the project inbox.
	if inboxNotes := j.projectInbox(); !strings.Contains(inboxNotes, "report updated: reports/inbox-report.md") {
		t.Fatalf("no project inbox note for the terminal-made work:\n%s", inboxNotes)
	}
	t.Logf("SHOW inbox work:\n%s", j.ok("standing", "show", inbox))
	t.Logf("SHOW review work:\n%s", j.ok("standing", "show", review))
	t.Logf("LOCALWORK RECEIPT scripted model firings=%d rule checks=%d (scripted model, not live-model acceptance)", model.firings.Load(), model.checks.Load())
}

// ── the journey's hands ─────────────────────────────────────────────────────

type journey struct {
	t                     *testing.T
	binary, home, project string
	env                   []string
}

type runRecord struct {
	occurrenceView
	RunDir  string
	Journal string
}

type occurrenceView struct {
	ID           string   `json:"id"`
	Spec         uint64   `json:"spec"`
	Brief        string   `json:"brief"`
	Attempt      int      `json:"attempt"`
	Supersedes   []string `json:"supersedes"`
	Phase        string   `json:"phase"`
	Outcome      string   `json:"outcome"`
	SupersededBy string   `json:"supersededBy"`
	Changes      []struct {
		Path string `json:"path"`
		Kind string `json:"kind"`
	} `json:"changes"`
	Published *struct {
		Path   string `json:"path"`
		SHA256 string `json:"sha256"`
	} `json:"published"`
	RuleCheck *struct {
		Verdict string `json:"verdict"`
		Quote   string `json:"quote"`
		First   string `json:"first"`
		Rewrote bool   `json:"rewrote"`
		Held    string `json:"held"`
	} `json:"ruleCheck"`
}

type showRecord struct {
	Occurrences []json.RawMessage `json:"occurrences"`
}

func (j *journey) run(args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, j.binary, args...)
	cmd.Env, cmd.Dir = j.env, j.project
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func (j *journey) ok(args ...string) string {
	j.t.Helper()
	out, err := j.run(args...)
	if err != nil {
		j.t.Fatalf("aforge %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return out
}

func (j *journey) start(args ...string) *exec.Cmd {
	cmd := exec.Command(j.binary, args...)
	cmd.Env, cmd.Dir = j.env, j.project
	must(j.t, cmd.Start())
	return cmd
}

func (j *journey) folder(name string) string {
	var made struct {
		ID string `json:"id"`
	}
	must(j.t, json.Unmarshal([]byte(j.ok("collections", "create", name, "--json")), &made))
	return made.ID
}

func (j *journey) addWork(words, brief, watch, report, folder string) string {
	var made struct {
		ID string `json:"id"`
	}
	out := j.ok("standing", "add", "--json", "--words", words, "--instructions", brief, "--watch", watch, "--report", report, "--place", folder, "--workspace", j.project, "--per-run-usd", "0.5")
	must(j.t, json.Unmarshal([]byte(strings.SplitN(out, "\n", 2)[0]), &made))
	return made.ID
}

func (j *journey) check() {
	j.t.Helper()
	j.t.Logf("check: %s", strings.TrimSpace(j.ok("standing", "check")))
}

// checkExit runs one pass and answers its output and exit status, for the
// passes that are meant to end unfinished.
func (j *journey) checkExit() (string, int) {
	j.t.Helper()
	out, err := j.run("standing", "check")
	var exit *exec.ExitError
	switch {
	case err == nil:
		return out, 0
	case errors.As(err, &exit):
		return out, exit.ExitCode()
	}
	j.t.Fatalf("aforge standing check: %v\n%s", err, out)
	return out, -1
}

func (j *journey) runs(id string) []runRecord {
	j.t.Helper()
	var show showRecord
	must(j.t, json.Unmarshal([]byte(j.ok("standing", "show", id, "--json", "--runs", "50")), &show))
	out := make([]runRecord, 0, len(show.Occurrences))
	for _, raw := range show.Occurrences {
		var record runRecord
		must(j.t, json.Unmarshal(raw, &record.occurrenceView))
		var paths struct {
			RunDir  string `json:"runDir"`
			Journal string `json:"journal"`
		}
		must(j.t, json.Unmarshal(raw, &paths))
		record.RunDir, record.Journal = paths.RunDir, paths.Journal
		out = append(out, record)
	}
	return out
}

func (j *journey) expectRuns(id string, n int) []runRecord {
	j.t.Helper()
	runs := j.runs(id)
	if len(runs) != n {
		j.t.Fatalf("item %s has %d runs, want %d", id, len(runs), n)
	}
	return runs
}

type runExpect struct {
	spec    uint64
	attempt int
	outcome string
	changes []string
}

func (j *journey) expectRun(run runRecord, want runExpect) {
	j.t.Helper()
	o := run
	var changes []string
	for _, change := range o.Changes {
		changes = append(changes, change.Kind+" "+change.Path)
	}
	if o.Phase != "finished" || o.Spec != want.spec || o.Attempt != want.attempt || o.Outcome != want.outcome || strings.Join(changes, ",") != strings.Join(want.changes, ",") {
		j.t.Fatalf("run %s = phase %s spec %d attempt %d outcome %s changes %v; want %+v", o.ID, o.Phase, o.Spec, o.Attempt, o.Outcome, changes, want)
	}
}

func (j *journey) expectPublished(run runRecord, path string) {
	j.t.Helper()
	p := run.Published
	if p == nil || p.Path != path {
		j.t.Fatalf("run %s published %+v, want %s", run.ID, p, path)
	}
	sum := sha256.Sum256([]byte(j.read(path)))
	if hex.EncodeToString(sum[:]) != p.SHA256 {
		j.t.Fatalf("published receipt does not match %s on disk", path)
	}
}

// expectCause reads the run's own journal: the first selection record must
// name this occurrence as its parent cause, and carry exactly the rules that
// govern this work.
func (j *journey) expectCause(run runRecord, rules, not []string) {
	j.t.Helper()
	if run.Journal == "" {
		j.t.Fatalf("run %s has no journal", run.ID)
	}
	file, err := os.Open(run.Journal)
	must(j.t, err)
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		var entry struct {
			Type     string `json:"type"`
			Exposure *struct {
				ParentCause string `json:"parent_cause"`
				Cause       *struct {
					Kind string `json:"kind"`
					ID   string `json:"id"`
					Spec uint64 `json:"spec"`
				} `json:"cause"`
				Standing []struct {
					Prompt string `json:"prompt"`
				} `json:"standing"`
			} `json:"context_exposure"`
		}
		if json.Unmarshal(scanner.Bytes(), &entry) != nil || entry.Type != "context_exposure" || entry.Exposure == nil || entry.Exposure.ParentCause == "" {
			continue
		}
		e := entry.Exposure
		if e.ParentCause != "standing_occurrence" || e.Cause == nil || e.Cause.ID != run.ID || e.Cause.Spec != run.Spec {
			j.t.Fatalf("journal cause %+v does not name occurrence %s v%d", e.Cause, run.ID, run.Spec)
		}
		var prompts []string
		for _, s := range e.Standing {
			prompts = append(prompts, s.Prompt)
		}
		joined := strings.Join(prompts, "\n")
		for _, rule := range rules {
			if !strings.Contains(joined, rule) {
				j.t.Fatalf("journal selection lacks %s: %s", rule, joined)
			}
		}
		for _, rule := range not {
			if strings.Contains(joined, rule) {
				j.t.Fatalf("journal selection carries %s: %s", rule, joined)
			}
		}
		return
	}
	j.t.Fatalf("no selection record in %s", run.Journal)
}

func (j *journey) write(rel, text string) {
	j.t.Helper()
	// A second apart at least, so a size-equal rewrite still moves the mtime
	// the reading compares.
	time.Sleep(20 * time.Millisecond)
	must(j.t, os.WriteFile(filepath.Join(j.project, rel), []byte(text), 0o644))
}

func (j *journey) append(rel, text string) {
	j.t.Helper()
	file, err := os.OpenFile(filepath.Join(j.project, rel), os.O_APPEND|os.O_WRONLY, 0o644)
	must(j.t, err)
	_, err = file.WriteString(text)
	must(j.t, err)
	must(j.t, file.Close())
}

func (j *journey) read(rel string) string {
	j.t.Helper()
	raw, err := os.ReadFile(filepath.Join(j.project, rel))
	must(j.t, err)
	return string(raw)
}

func (j *journey) projectInbox() string {
	var out strings.Builder
	_ = filepath.Walk(filepath.Join(j.home, "v3", "standing"), func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && filepath.Base(path) == "inbox.jsonl" {
			raw, _ := os.ReadFile(path)
			out.Write(raw)
		}
		return nil
	})
	return out.String()
}

// keptDir is a fresh folder, kept after the test when AFORGE_LOCALWORK_KEEP
// names where — so a receipt can point at the run folders it describes.
func keptDir(t *testing.T, name string) string {
	t.Helper()
	root := os.Getenv("AFORGE_LOCALWORK_KEEP")
	if root == "" {
		return t.TempDir()
	}
	dir := filepath.Join(root, name)
	must(t, os.RemoveAll(dir))
	must(t, os.MkdirAll(dir, 0o755))
	return dir
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// scriptedEnv is the child's whole environment: this process's, minus every
// aforge and provider variable, plus a disposable home and the loopback model.
// HOME itself is left alone; only AFORGE_HOME moves.
func scriptedEnv(home, url string) []string {
	var env []string
	for _, kv := range os.Environ() {
		name := strings.SplitN(kv, "=", 2)[0]
		if strings.HasPrefix(name, "AFORGE_") || name == "OPENROUTER_API_KEY" || name == "OPENAI_API_KEY" {
			continue
		}
		env = append(env, kv)
	}
	return append(env,
		"AFORGE_HOME="+home,
		"AFORGE_BASE_URL="+url+"/api/v1",
		"AFORGE_MODEL=stub/scripted",
		"OPENROUTER_API_KEY=scripted-model-no-network",
	)
}

// ── the scripted model ──────────────────────────────────────────────────────

type scriptedModel struct {
	url     string
	firings atomic.Int64
	checks  atomic.Int64
	hang    atomic.Bool
	hanging chan struct{}
}

var changeLine = regexp.MustCompile(`(?m)^(added|modified|removed)  (\S+)$`)
var focusLine = regexp.MustCompile(`FOCUS: (\w+)`)

// The script's reading of the one rule it knows how to check. It is the TEST'S
// stand-in for a model's judgment, keyed to this fixture on purpose; aforge's
// own check has no pattern in it and reads the rule's words with a model.
var emailAddress = regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}`)

// rulesCheckLead is the start of aforge's instruction to its report check.
const rulesCheckLead = "You check one report against the rules"

func newScriptedModel(t *testing.T) *scriptedModel {
	m := &scriptedModel{hanging: make(chan struct{}, 4)}
	server := httptest.NewServer(http.HandlerFunc(m.serve))
	t.Cleanup(server.Close)
	m.url = server.URL
	return m
}

func (m *scriptedModel) serve(w http.ResponseWriter, r *http.Request) {
	switch {
	case strings.HasSuffix(r.URL.Path, "/models"):
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"data":[{"id":"stub/scripted","canonical_slug":"stub/scripted","name":"Scripted","context_length":200000,"architecture":{"input_modalities":["text"],"output_modalities":["text"]},"pricing":{"prompt":"0","completion":"0","request":"0"},"supported_parameters":["tools","tool_choice","max_tokens"]}]}`)
		return
	case !strings.HasSuffix(r.URL.Path, "/chat/completions"):
		http.NotFound(w, r)
		return
	}
	var body struct {
		Stream   bool `json:"stream"`
		Messages []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
		Tools []json.RawMessage `json:"tools"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	text := func(raw json.RawMessage) string {
		var plain string
		if json.Unmarshal(raw, &plain) == nil {
			return plain
		}
		var parts []struct {
			Text string `json:"text"`
		}
		_ = json.Unmarshal(raw, &parts)
		var out strings.Builder
		for _, p := range parts {
			out.WriteString(p.Text)
		}
		return out.String()
	}
	var all strings.Builder
	ask, answered, last := "", "", -1
	for i, message := range body.Messages {
		content := text(message.Content)
		all.WriteString(content + "\n")
		if message.Role == "user" {
			ask, answered, last = content, "", i
		}
		if message.Role == "tool" && i > last {
			answered = content
		}
	}
	// THE CHECK BEFORE PUBLICATION: the script finds an address in the report
	// when the Launch rule is among the rules it was given.
	if len(body.Messages) > 0 && strings.HasPrefix(text(body.Messages[0].Content), rulesCheckLead) {
		m.checks.Add(1)
		draft := ask
		if at := strings.Index(draft, "<<<\n"); at >= 0 {
			draft = draft[at+4:]
		}
		if at := strings.Index(draft, "\n>>>"); at >= 0 {
			draft = draft[:at]
		}
		if found := emailAddress.FindString(draft); found != "" && strings.Contains(ask, ruleLaunch) {
			verdict, _ := json.Marshal(map[string]any{"kept": false, "rule": ruleLaunch, "quote": found, "why": "it quotes an email address"})
			m.reply(w, body.Stream, "", "", string(verdict))
			return
		}
		m.reply(w, body.Stream, "", "", `{"kept": true}`)
		return
	}
	// THE ONE CORRECTION: the script redacts the quoted words from its last
	// report — unless that report carries KEEP-RAW, which it will not change.
	if len(body.Tools) > 0 && strings.HasPrefix(ask, "RULE CHECK:") {
		previous := ""
		for _, message := range body.Messages {
			if message.Role == "assistant" {
				if said := text(message.Content); strings.TrimSpace(said) != "" {
					previous = said
				}
			}
		}
		quote := ""
		for _, line := range strings.Split(ask, "\n") {
			if strings.HasPrefix(line, "- the report says: ") {
				quote = strings.TrimPrefix(line, "- the report says: ")
			}
		}
		if quote != "" && !strings.Contains(previous, "KEEP-RAW") {
			previous = strings.ReplaceAll(previous, quote, "[redacted]")
		}
		m.reply(w, body.Stream, "", "", previous)
		return
	}
	// Only the firing's own turn is scripted: it carries tools and the report
	// instruction. Everything else a session asks for gets a plain answer.
	if len(body.Tools) == 0 || !strings.Contains(ask, reportMarker) {
		m.reply(w, body.Stream, "", "", "ok")
		return
	}
	if answered == "" {
		m.firings.Add(1)
		if m.hang.Load() {
			m.hanging <- struct{}{}
			<-r.Context().Done()
			return
		}
		first := ""
		if match := changeLine.FindStringSubmatch(ask); match != nil && match[1] != "removed" {
			first = match[2]
		}
		if first != "" {
			args, _ := json.Marshal(map[string]string{"path": first})
			m.reply(w, body.Stream, "read", string(args), "")
			return
		}
	}
	var report strings.Builder
	report.WriteString("# Report\n\n")
	if focus := focusLine.FindStringSubmatch(ask); focus != nil {
		report.WriteString("FOCUS: " + focus[1] + "\n")
	}
	for _, match := range changeLine.FindAllStringSubmatch(ask, -1) {
		report.WriteString("- " + match[1] + " " + match[2] + "\n")
	}
	var seen []string
	for _, rule := range []string{ruleLaunch, ruleMarketing} {
		if strings.Contains(all.String(), rule) {
			seen = append(seen, rule)
		}
	}
	report.WriteString("rules seen: " + strings.Join(seen, ", ") + "\n")
	if answered != "" {
		read := strings.TrimSpace(answered)
		for _, line := range strings.Split(read, "\n") {
			// The read tool numbers its lines; the script quotes the text.
			if at := strings.Index(line, "Decision"); at >= 0 {
				read = line[at:]
				break
			}
			if at := strings.Index(line, "Request"); at >= 0 {
				read = line[at:]
				break
			}
			if at := strings.Index(line, "Offline"); at >= 0 {
				read = line[at:]
				break
			}
		}
		report.WriteString("read: " + read + "\n")
	}
	m.reply(w, body.Stream, "", "", report.String())
}

func (m *scriptedModel) reply(w http.ResponseWriter, stream bool, tool, args, text string) {
	usage := `"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2,"cost":0}`
	if !stream {
		w.Header().Set("Content-Type", "application/json")
		if tool != "" {
			fmt.Fprintf(w, `{"id":"s","object":"chat.completion","model":"stub/scripted","choices":[{"index":0,"message":{"role":"assistant","content":"","tool_calls":[{"id":"call_1","type":"function","function":{"name":%q,"arguments":%q}}]},"finish_reason":"tool_calls"}],%s}`, tool, args, usage)
			return
		}
		fmt.Fprintf(w, `{"id":"s","object":"chat.completion","model":"stub/scripted","choices":[{"index":0,"message":{"role":"assistant","content":%q},"finish_reason":"stop"}],%s}`, text, usage)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	flusher, _ := w.(http.Flusher)
	send := func(payload string) {
		fmt.Fprintf(w, "data: %s\n\n", payload)
		if flusher != nil {
			flusher.Flush()
		}
	}
	chunk := func(delta, reason string) string {
		finish := "null"
		extra := ""
		if reason != "" {
			finish, extra = fmt.Sprintf("%q", reason), ","+usage
		}
		return fmt.Sprintf(`{"id":"s","object":"chat.completion.chunk","model":"stub/scripted","choices":[{"index":0,"delta":%s,"finish_reason":%s}]%s}`, delta, finish, extra)
	}
	send(chunk(`{"role":"assistant","content":""}`, ""))
	if tool != "" {
		send(chunk(fmt.Sprintf(`{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":%q,"arguments":%q}}]}`, tool, args), ""))
		send(chunk(`{}`, "tool_calls"))
	} else {
		send(chunk(fmt.Sprintf(`{"content":%q}`, text), ""))
		send(chunk(`{}`, "stop"))
	}
	send("[DONE]")
}
