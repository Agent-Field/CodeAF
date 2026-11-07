//go:build e2e

package e2e

// THE MANAGERS, LIVE (audit A2). One job on factory-demo, driven through the
// `codeaf app` stdio protocol the desk speaks:
//
//  1. While Write runs, its manager is asked to add a security step. The job
//     must keep running (never Backlog, never Plan again) and run the step.
//  2. At the pull request's decision, the manager is asked "merge it?". Its
//     answer is kept; a merge it presses is undone at once, so factory-demo's
//     main is never changed.
//  3. The factory-wide manager is opened with factory.foreman_open and asked
//     "what's running?".
//
// It is the general-factory world ([newFLEWorld]) with its own prefix, AUDA2,
// and its own cleanup: every AUDA2 issue and pull request is closed with
// "factory audit, closing" and every branch this run pushed is deleted.
//
//	FLE2E=1 FLE2E_GH_TOKEN_FILE=… FLE2E_LOG_DIR=… go test -tags e2e -run TestFactoryManagersE2E -count=1 -timeout 60m -v ./internal/e2e/

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

const (
	auda2Prefix = "AUDA2"
	auda2Close  = "factory audit, closing"
)

type auda2Entry struct {
	Kind   string    `json:"kind"`
	Author string    `json:"author"`
	At     time.Time `json:"at"`
	Text   string    `json:"text"`
}

func TestFactoryManagersE2E(t *testing.T) {
	w := newFLEWorld(t)
	// Only this run's job may start: the world's rules pick up labelled
	// issues by themselves, which this audit does not want.
	w.must("factory.rules_set", map[string]any{"factory": w.factory, "rules": []string{}, "ask_first": []string{}, "never": []string{}, "start_on_its_own": false, "start_labels": []string{"agent-ready"}})
	t.Cleanup(func() { w.auda2Cleanup() })

	var acted struct {
		Job int `json:"job"`
	}
	w.decode("factory.job_create", map[string]any{"factory": w.factory, "repo": fleRepo, "title": auda2Prefix + ": add Ledger.Smallest() returning the smallest entry",
		"text": "Add a method Smallest() to Ledger that returns the entry with the smallest amount (the first one on a tie) and false for an empty ledger, with a table test. Keep it small."}, &acted)
	id := acted.Job
	if id == 0 {
		t.Fatal("factory.job_create named no job")
	}
	w.started[id] = true
	j := w.job(id)
	w.must("factory.job_control", map[string]any{"factory": w.factory, "job": id, "mode": "auto", "expect": map[string]any{"version": j.Version}})
	j = w.job(id)
	w.must("factory.job_start", map[string]any{"factory": w.factory, "job": id, "expect": map[string]any{"version": j.Version}})
	t.Logf("job %d started in Auto", id)

	// ── 1. the step edit during Write ──
	fleEventually(t, "Write to be running", 15*time.Minute, 3*time.Second, func() bool {
		return w.currentStep(id) == "write" && w.item(id).State == factory.StateRunning
	})
	planRounds := w.phase(id, "plan").Round
	var sent struct {
		Agent string `json:"agent"`
	}
	w.decode("factory.job_send", map[string]any{"factory": w.factory, "job": id, "text": "add a security review step after review"}, &sent)
	manager := sent.Agent
	edit := w.awaitAnswer(t, manager, time.Now().Add(-time.Second), 5*time.Minute)
	t.Logf("manager on the edit: %s", edit)
	sawNew := false
	fleEventually(t, "the edited job to carry on and run security", 25*time.Minute, 3*time.Second, func() bool {
		it := w.item(id)
		if it.State == factory.StateNew || factory.StoppedByPerson(it) {
			sawNew = true
		}
		ph := w.phase(id, "security")
		return ph.State == factory.PhaseDone || ph.State == factory.PhaseRunning && it.State == factory.StateRunning
	})
	if sawNew {
		t.Errorf("the job fell back to Backlog after the step edit")
	}
	if p := w.phase(id, "plan"); p.Round != planRounds || p.State != factory.PhaseDone {
		t.Errorf("plan ran again after the edit: round %d → %d, state %s", planRounds, p.Round, p.State)
	}
	t.Logf("RESULT edit: job carried on, security %s, plan round %d", w.phase(id, "security").State, w.phase(id, "plan").Round)

	// ── 2. "merge it?" at the pull request's decision ──
	fleEventually(t, "the pull request and its decision", 30*time.Minute, 5*time.Second, func() bool {
		it := w.item(id)
		if it.PullRequest == nil || it.PullRequest.Number == 0 {
			return false
		}
		return factory.OffersMarkReady(it) || factory.MergeGate(it, factory.MergeAsk{By: "person"}).OK
	})
	it := w.item(id)
	t.Logf("PR #%d draft=%v merge gate: %+v", it.PullRequest.Number, it.PullRequest.Draft, factory.MergeGate(it, factory.MergeAsk{By: "person"}))
	asked := time.Now().Add(-time.Second)
	if _, err := w.eng.call("agents.send", map[string]any{"id": manager, "text": "merge it?"}, time.Minute); err != nil {
		t.Fatal(err)
	}
	stopUndo := w.undoAnyMerge(t, id)
	merge := w.awaitAnswer(t, manager, asked, 5*time.Minute)
	stopUndo()
	t.Logf("manager on merge it?: %s", merge)
	if strings.Contains(strings.ToLower(merge), "nothing waiting") || strings.Contains(strings.ToLower(merge), "nothing is waiting") {
		t.Errorf("the manager said nothing waits while the decision was offered: %s", merge)
	}

	// ── 3. the foreman ──
	var fm struct {
		Factory string `json:"factory"`
		Agent   string `json:"agent"`
	}
	w.decode("factory.foreman_open", map[string]any{"factory": w.factory}, &fm)
	if fm.Agent == "" {
		t.Fatal("factory.foreman_open named no agent")
	}
	var again struct {
		Agent string `json:"agent"`
	}
	w.decode("factory.foreman_open", map[string]any{"factory": w.factory}, &again)
	if again.Agent != fm.Agent {
		t.Errorf("a second foreman_open answered %s, want %s", again.Agent, fm.Agent)
	}
	_, _ = w.eng.call("agents.open", map[string]any{"id": fm.Agent}, time.Minute)
	asked = time.Now().Add(-time.Second)
	if _, err := w.eng.call("agents.send", map[string]any{"id": fm.Agent, "text": "what's running?"}, time.Minute); err != nil {
		t.Fatal(err)
	}
	running := w.awaitAnswer(t, fm.Agent, asked, 5*time.Minute)
	t.Logf("foreman on what's running?: %s", running)
	if !strings.Contains(running, "#") {
		t.Errorf("the foreman named no job: %s", running)
	}
	w.saveChat(t, "manager", manager)
	w.saveChat(t, "foreman", fm.Agent)
}

// currentStep is the name of the step the job's run is on.
func (w *fleWorld) currentStep(id int) string {
	it := w.item(id)
	if it.Stream == nil || it.Stream.Cur < 0 || it.Stream.Cur >= len(it.Stream.Phases) {
		return ""
	}
	return it.Stream.Phases[it.Stream.Cur].Name
}

// phase is the job's run's phase named name, zero when it has none.
func (w *fleWorld) phase(id int, name string) factory.Phase {
	it := w.item(id)
	if it.Stream != nil {
		for _, ph := range it.Stream.Phases {
			if ph.Name == name {
				return ph
			}
		}
	}
	return factory.Phase{}
}

// entries is an agent's conversation as the desk reads it.
func (w *fleWorld) entries(agent string) []auda2Entry {
	raw, err := w.eng.call("agents.entries", map[string]any{"id": agent, "limit": 400}, time.Minute)
	if err != nil {
		_, _ = w.eng.call("agents.open", map[string]any{"id": agent}, time.Minute)
		raw, err = w.eng.call("agents.entries", map[string]any{"id": agent, "limit": 400}, time.Minute)
	}
	if err != nil {
		return nil
	}
	var e struct {
		Entries []auda2Entry `json:"entries"`
	}
	_ = json.Unmarshal(raw, &e)
	return e.Entries
}

// awaitAnswer waits for the model's answer after since: model-authored
// answer entries that then stop changing for twenty seconds.
func (w *fleWorld) awaitAnswer(t *testing.T, agent string, since time.Time, within time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(within)
	last, stable := "", time.Time{}
	for time.Now().Before(deadline) {
		var said []string
		for _, e := range w.entries(agent) {
			if e.Kind == "answer" && e.Author != "engine" && e.At.After(since) && strings.TrimSpace(e.Text) != "" {
				said = append(said, strings.TrimSpace(e.Text))
			}
		}
		now := strings.Join(said, "\n")
		switch {
		case now != last:
			last, stable = now, time.Now()
		case now != "" && time.Since(stable) > 20*time.Second:
			return now
		}
		time.Sleep(3 * time.Second)
	}
	t.Errorf("no settled answer from %s within %s (last: %q)", agent, within, last)
	return last
}

// undoAnyMerge watches the job while the manager answers "merge it?" and
// undoes a merge it pressed inside its Undo window, so the audit never
// merges into factory-demo. It answers the function that stops the watch.
func (w *fleWorld) undoAnyMerge(t *testing.T, id int) func() {
	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-done:
				return
			case <-time.After(300 * time.Millisecond):
			}
			it, err := w.floor.Get(id)
			if err != nil || it.Merging == nil || it.Merging.State != factory.MergeAsked {
				continue
			}
			raw, err := w.eng.call("factory.job", map[string]any{"factory": w.factory, "job": id}, 10*time.Second)
			if err != nil {
				continue
			}
			var j fleJob
			_ = json.Unmarshal(raw, &j)
			_, err = w.eng.call("factory.job_undo", map[string]any{"factory": w.factory, "job": id, "expect": map[string]any{"version": j.Version}}, 10*time.Second)
			t.Logf("RESULT merge: the manager pressed Merge as asked; undone by the audit: %v", err)
			return
		}
	}()
	return func() { close(done) }
}

// saveChat writes one agent's whole conversation beside the logs.
func (w *fleWorld) saveChat(t *testing.T, who, agent string) {
	var doc strings.Builder
	es := w.entries(agent)
	fmt.Fprintf(&doc, "%s · agent %s · %d entries\n\n", who, agent, len(es))
	for _, e := range es {
		fmt.Fprintf(&doc, "── %s · %s · %s\n%s\n\n", e.At.Format(time.RFC3339), e.Author, e.Kind, strings.TrimSpace(e.Text))
	}
	path := filepath.Join(w.logDir, "auda2-"+who+".txt")
	if err := os.WriteFile(path, []byte(doc.String()), 0o644); err != nil {
		t.Errorf("save %s: %v", path, err)
	}
}

// auda2Cleanup stops this run's jobs and closes every AUDA2 issue and pull
// request, and every pull request on a branch this run pushed, with the one
// sentence, deleting the branches.
func (w *fleWorld) auda2Cleanup() {
	t := w.t
	ours := map[string]bool{}
	for id := range w.started {
		w.stopQuiet(id)
		if it, err := w.floor.Get(id); err == nil {
			if it.PullRequest != nil && it.PullRequest.HeadBranch != "" {
				ours[it.PullRequest.HeadBranch] = true
			}
			if it.Change != nil && it.Change.Branch != "" {
				ours[it.Change.Branch] = true
			}
		}
	}
	if out, err := w.gh("pr", "list", "-R", fleRepo, "--state", "open", "--json", "number,title,headRefName", "--limit", "200"); err == nil {
		var prs []struct {
			Number int    `json:"number"`
			Title  string `json:"title"`
			Head   string `json:"headRefName"`
		}
		_ = json.Unmarshal([]byte(out), &prs)
		for _, p := range prs {
			if !strings.Contains(p.Title, auda2Prefix) && !ours[p.Head] {
				continue
			}
			ours[p.Head] = true
			o, err := w.gh("pr", "close", strconv.Itoa(p.Number), "-R", fleRepo, "--comment", auda2Close, "--delete-branch")
			t.Logf("closed PR #%d (%s): %v %s", p.Number, p.Head, err, o)
		}
	}
	if out, err := w.gh("issue", "list", "-R", fleRepo, "--state", "open", "--search", auda2Prefix+" in:title", "--json", "number,title", "--limit", "100"); err == nil {
		var issues []struct {
			Number int    `json:"number"`
			Title  string `json:"title"`
		}
		_ = json.Unmarshal([]byte(out), &issues)
		for _, is := range issues {
			if strings.Contains(is.Title, auda2Prefix) {
				o, err := w.gh("issue", "close", strconv.Itoa(is.Number), "-R", fleRepo, "--comment", auda2Close)
				t.Logf("closed issue #%d: %v %s", is.Number, err, o)
			}
		}
	}
	if out, err := w.gh("api", "repos/"+fleRepo+"/branches", "--paginate", "-q", ".[].name"); err == nil {
		for _, b := range strings.Fields(out) {
			if w.branchesBefore[b] || !ours[b] && !strings.Contains(strings.ToLower(b), "auda2") {
				continue
			}
			o, err := w.gh("api", "-X", "DELETE", "repos/"+fleRepo+"/git/refs/heads/"+b)
			t.Logf("deleted branch %s: %v %s", b, err, o)
		}
	}
}
