package session

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/plandb"
)

// The reader alone, over the spellings a worker actually writes. The ground is a
// folder under the person's home, which is where a canvas's work folder lives in
// the app, so the home and its parent are both outside it.
func TestAWideSearchIsReadFromTheCommandItIsSpelledIn(t *testing.T) {
	home := "/home/person"
	ground := "/home/person/canvases/cv_1/work"
	refused := map[string]string{
		// The measured command, verbatim from qloop run32 FIRE t1.
		`ls /x 2>/dev/null | head; echo ---; find / -iname '*reports*research*' -not -path '*/.git/*' 2>/dev/null | head`: "find /",
		`find ~ -name '*.md'`:                   "find /home/person",
		`find $HOME -type d -name skills`:       "find /home/person",
		`find "${HOME}" -name x`:                "find /home/person",
		`find /home -name x`:                    "find /home",
		`find /usr /opt -name libfoo.so`:        "find /usr",
		`cd / && find . -name x`:                "find /",
		`grep -rn "Reports and research" /`:     "grep /",
		`grep -Rl skill ~ 2>/dev/null`:          "grep /home/person",
		`rg -l docLayout ~`:                     "rg /home/person",
		`fd -H SKILL.md /`:                      "fd /",
		`du -sh /* 2>/dev/null`:                 "du /*",
		`du -sh /`:                              "du /",
		`ls -R / | head`:                        "ls /",
		`tree ~`:                                "tree /home/person",
		`sudo find / -name x`:                   "find /",
		`find -L / -name x`:                     "find /",
		`find / -maxdepth 9 -name x`:            "find /",
		`echo start; find /tmp -name '*.json'`:  "find /tmp",
		`rg --max-depth 12 pattern /`:           "rg /",
		`grep -r --include=*.md pattern /home/`: "grep /home",
	}
	for command, want := range refused {
		if got := wideSearchAim(ground, command, home); got != want {
			t.Errorf("%q: aimed at %q, want %q", command, got, want)
		}
	}
	allowed := []string{
		`find . -name '*.png'`,
		`find "` + ground + `" -name x`,
		`find / -maxdepth 2 -name bin`,
		`rg --max-depth 3 pattern /`,
		`du -d 1 -h ~`,
		`tree -L 2 /`,
		`grep -n x /etc/hosts`,
		`grep pattern ~/notes.md`,
		`cat /etc/os-release`,
		`ls ~`,
		`ls -la /`,
		`grep -rn TODO ~/src/project`,
		`find ~/src/project -name '*.go'`,
		`rg docLayout`,
		`rg -n pattern ../sibling`,
		`canvas show cv_1`,
		`find $REPO -name x`,
	}
	for _, command := range allowed {
		if got := wideSearchAim(ground, command, home); got != "" {
			t.Errorf("%q was read as a wide search of %q", command, got)
		}
	}
}

// A root the task is standing in is never wide, even when it is the home itself.
func TestATaskStandingInAWideRootMaySearchItsOwnFolder(t *testing.T) {
	if got := wideSearchAim("/home/person", "find . -name x", "/home/person"); got != "" {
		t.Fatalf("a task in the home was refused its own folder: %q", got)
	}
	if got := wideSearchAim("/home/person", "find / -name x", "/home/person"); got != "find /" {
		t.Fatalf("a task in the home searched the disk: %q", got)
	}
}

func TestOnlyATreeWalkPutsTheQuietClockOnACommand(t *testing.T) {
	for command, want := range map[string]bool{
		`find . -name x`:          true,
		`cd src && rg pattern`:    true,
		`grep -rn x .`:            true,
		`grep x file.txt`:         false,
		`sleep 40`:                false,
		`go test ./...`:           false,
		`ls -la`:                  false,
		`python3 make_chart.py`:   false,
		`echo a | grep -c a`:      false,
		`du -sh node_modules`:     true,
		`canvas show cv_1 | head`: false,
	} {
		if got := commandSearches(command); got != want {
			t.Errorf("%q: searches=%v, want %v", command, got, want)
		}
	}
}

// ── through a real worker ───────────────────────────────────────────────────

// beltWorkerRunning builds the worker the app's hand-off actually builds — the
// bash-belt seat, NewBeltWorker — scripted to make one bash call and then say it
// is done, and answers the worker and the failure the call came back as (if it
// failed).
func beltWorkerRunning(t *testing.T, command string, mutate func(*Agent)) (*Agent, Event, bool, []Event) {
	t.Helper()
	beltWorkerEnv(t)
	store, err := plandb.Open(filepath.Join(t.TempDir(), planStoreFilename), "the work", planRootID, "", "")
	if err != nil {
		t.Fatal(err)
	}
	workspace := t.TempDir()
	arguments := fmt.Sprintf(`{"command":%q}`, command)
	script := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("look", "bash", arguments), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("done"), nil },
	}}
	worker, err := NewBeltWorker(Config{Workspace: workspace, Model: "test/model", System: "SYSTEM"}, script, store.Task(planRootID), store.Path(), store.RootID())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = worker.Close() })
	if mutate != nil {
		mutate(worker)
	}
	events := collect(t, mustSubmit(t, worker, "find the rules doc"))
	failed, ok := firstOfKind(events, EventToolFailed)
	return worker, failed, ok, events
}

// beltWorkerEnv is the environment [TestBeltWorkerKeepsProfileReadFailure]
// builds a belt worker in: its own home, the bash belt asked for, and a plan
// CLI that is never run.
func beltWorkerEnv(t *testing.T) {
	t.Helper()
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("CODEAF_TASK_BELT", "bash")
	t.Setenv(planCLIBinEnv, filepath.Join(t.TempDir(), "stub-codeaf"))
}

// THE MEASURED RUN, END TO END: the worker's `find /` comes back refused before
// anything runs, and the refusal names the worker's own folder.
func TestAWorkersSearchOfTheWholeDiskIsRefusedNamingItsFolder(t *testing.T) {
	began := time.Now()
	worker, failed, ok, _ := beltWorkerRunning(t,
		`find / -iname '*reports*research*' -not -path '*/.git/*' 2>/dev/null | head`, nil)
	if !ok {
		t.Fatal("the worker's search of the whole disk was run")
	}
	for _, want := range []string{"find /", "was not run", worker.config.Workspace, "search there"} {
		if !strings.Contains(failed.Output, want) {
			t.Fatalf("the refusal does not say %q:\n%s", want, failed.Output)
		}
	}
	if elapsed := time.Since(began); elapsed > 20*time.Second {
		t.Fatalf("the refusal took %s", elapsed)
	}
}

// A search that prints nothing is stopped at the quiet limit with a sentence
// naming the worker's folder, instead of holding the worker to the ceiling.
func TestAWorkersSilentSearchIsStoppedAtItsQuietLimit(t *testing.T) {
	gate := filepath.Join(t.TempDir(), "never")
	// The search finds nothing, and the spin after it is the test's own hold:
	// nothing releases it, so only the quiet clock can end this call.
	command := fmt.Sprintf(`find . -name 'zz-nothing-here'; until [ -f '%s' ]; do sleep 0.05; done`, gate)
	began := time.Now()
	worker, failed, ok, _ := beltWorkerRunning(t, command, func(worker *Agent) {
		worker.config.searchQuiet = 300 * time.Millisecond
	})
	if !ok {
		t.Fatal("the silent search came back as a success")
	}
	for _, want := range []string{"Search stopped", "printed nothing", worker.config.Workspace} {
		if !strings.Contains(failed.Output, want) {
			t.Fatalf("the stopped search does not say %q:\n%s", want, failed.Output)
		}
	}
	if elapsed := time.Since(began); elapsed > 60*time.Second {
		t.Fatalf("the quiet stop took %s", elapsed)
	}
	if strings.Contains(failed.Output, BashPromotedLead) {
		t.Fatalf("the silent search was handed to a job instead of stopped:\n%s", failed.Output)
	}
}

// A search that keeps finding things keeps printing, and printing resets the
// clock: a long search that is working is never cut by it.
func TestAWorkersSearchThatKeepsFindingIsNotStopped(t *testing.T) {
	command := `for i in $(seq 1 20); do echo "hit $i"; sleep 0.05; done; find . -name 'zz-nothing-here'`
	_, failed, ok, events := beltWorkerRunning(t, command, func(worker *Agent) {
		worker.config.searchQuiet = 2 * time.Second
	})
	if ok {
		t.Fatalf("a search that kept printing was stopped:\n%s", failed.Output)
	}
	end, found := firstOfKind(events, EventToolEnd)
	if !found || !strings.Contains(end.Output, "hit 20") {
		t.Fatalf("the search did not finish with its output: %+v", end)
	}
}

// A silent command that is not a search — a build, a sleep, a test suite that
// prints at the end — is never on the quiet clock.
func TestAWorkersSilentNonSearchCommandIsNotStopped(t *testing.T) {
	_, failed, ok, _ := beltWorkerRunning(t, `sleep 1; echo built`, func(worker *Agent) {
		worker.config.searchQuiet = 200 * time.Millisecond
	})
	if ok {
		t.Fatalf("a silent build was stopped:\n%s", failed.Output)
	}
}

// A conversation is the person's own, and its searches are neither refused nor
// put on the quiet clock.
func TestAConversationsSearchIsThePersonsOwn(t *testing.T) {
	session, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.searchQuiet = 200 * time.Millisecond
	})
	call := ai.ToolCall{ID: "c1", Type: "function"}
	call.Function.Name = "bash"
	call.Function.Arguments = `{"command":"find / -name x"}`
	if _, _, ok := (wideSearchGuard{agent: session}).PreAction(context.Background(), nil, nil, call); !ok {
		t.Fatal("a conversation's own search was refused")
	}
	text, isError := runTool(t, session, "bash", `{"command":"find . -name zz-nothing; sleep 0.6; echo ok"}`)
	if isError || strings.Contains(text, "Search stopped") {
		t.Fatalf("a conversation's search was stopped: %s", text)
	}
}

// The belt's own find and grep walk from their path argument, and are held to
// the same refusal.
func TestAWorkersNativeSearchOfTheDiskIsRefused(t *testing.T) {
	beltWorkerEnv(t)
	store, err := plandb.Open(filepath.Join(t.TempDir(), planStoreFilename), "the work", planRootID, "", "")
	if err != nil {
		t.Fatal(err)
	}
	worker, err := NewBeltWorker(Config{Workspace: t.TempDir(), Model: "test/model", System: "SYSTEM"}, &scriptedCompleter{}, store.Task(planRootID), store.Path(), store.RootID())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = worker.Close() })
	home, _ := os.UserHomeDir()
	for name, arguments := range map[string]string{
		"find": `{"pattern":"*research*","path":"/"}`,
		"grep": `{"pattern":"docLayout","path":"~"}`,
	} {
		if name == "grep" && home == "" {
			continue
		}
		call := ai.ToolCall{ID: "c-" + name, Type: "function"}
		call.Function.Name = name
		call.Function.Arguments = arguments
		_, result, ok := (wideSearchGuard{agent: worker}).PreAction(context.Background(), nil, nil, call)
		if ok || !strings.Contains(result.text, worker.config.Workspace) {
			t.Fatalf("%s %s was not refused naming the folder: %q", name, arguments, result.text)
		}
	}
	call := ai.ToolCall{ID: "c-ok", Type: "function"}
	call.Function.Name = "find"
	call.Function.Arguments = `{"pattern":"*.png","path":"."}`
	if _, _, ok := (wideSearchGuard{agent: worker}).PreAction(context.Background(), nil, nil, call); !ok {
		t.Fatal("a search of the worker's own folder was refused")
	}
}

// A node worker carries the conversation's handoff clock, and a worker waits on
// a command the clock adopted (task_job_park.go). So a task's search is left off
// that clock and ended by its quiet instead: handed to a job, it would hold the
// worker exactly as long as it held the call.
func TestANodeWorkersSilentSearchIsStoppedNotHandedToAJob(t *testing.T) {
	session, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.BashBackgroundAfterSeconds = 1
	})
	worker, _ := workerFor(t, session, taskSpec{title: "the rules doc", brief: "b", acceptance: "a", depth: 1})
	worker.config.searchQuiet = 1500 * time.Millisecond
	gate := filepath.Join(t.TempDir(), "never")
	command := fmt.Sprintf(`find . -name 'zz-nothing-here'; until [ -f '%s' ]; do sleep 0.05; done`, gate)
	text, isError := runTool(t, worker, "bash", fmt.Sprintf(`{"command":%q}`, command))
	if !isError || !strings.Contains(text, "Search stopped") || strings.Contains(text, BashPromotedLead) {
		t.Fatalf("the node worker's silent search was not stopped by its quiet:\n%s", text)
	}
}
