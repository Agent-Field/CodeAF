package session

// EVERY COMMAND IS A STREAM, AND THE MODEL ALWAYS SEES WHERE ITS STREAMS ARE.
//
// These tests are written from one measured run. A scoring script took three to
// five minutes; the foreground bound was two; every call was adopted as a job;
// the script block-buffered its output so the job's log was zero bytes until it
// exited; and the model answered `sleep 30 && tail jobs/1.log` nine times to
// `(no output)` before killing work that was about to finish. Sixty-eight per
// cent of a ten-hour worker's wall clock went into that loop.
//
// Four things had to become true, and there is one test here for each.

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// ── 1. bash waits ───────────────────────────────────────────────────────────

// A CALL IS NEVER SILENTLY CONVERTED INTO A JOB BEFORE THE MODEL'S OWN TIMEOUT.
// A command that takes longer than the old two-minute default and shorter than
// the ceiling is simply WAITED FOR, and the model gets the output it asked for
// rather than an id it then has to chase.
//
// The command here is short — the suite is not going to sit through two minutes
// to prove a constant — so the fact under test is the constant itself: what a
// call with no timeout of its own is bounded by.
func TestAForegroundCallWaitsForTheCommandRatherThanAdoptingIt(t *testing.T) {
	t.Parallel()
	agent, _ := jobsAgent(t)

	if bound := BashTimeoutSeconds(json.RawMessage(`{"command":"python3 /opt/score_golden.py"}`)); bound != BashCeilingSeconds {
		t.Fatalf("a call that named no timeout is bounded at %v seconds, want the %d second ceiling — "+
			"anything shorter turns a four-minute script into a job nobody asked for", bound, BashCeilingSeconds)
	}

	answer, isError := runBash(t, context.Background(), agent, map[string]any{
		"command": "sleep 0.2; echo the-score-is-0.48",
	})
	if isError {
		t.Fatalf("the call failed: %q", answer)
	}
	if !strings.Contains(answer, "the-score-is-0.48") {
		t.Fatalf("the call did not return the command's output: %q", answer)
	}
	if strings.Contains(answer, "still running as job") {
		t.Fatalf("a call the model did not bound was adopted anyway: %q", answer)
	}
	if jobs := agent.jobs.all(); len(jobs) != 0 {
		t.Fatalf("a waited-for call left %d jobs behind", len(jobs))
	}
}

// ── 2. the output exists before the command ends ────────────────────────────

// THE LOG IS NOT EMPTY WHILE THE WORK RUNS. Python buffers its output in 4KB
// blocks when it is not writing to a terminal, so a script that prints one line
// per case and takes four minutes wrote NOTHING anybody could read until the
// last second of it — which is what made a running job indistinguishable from a
// dead one.
//
// The fix is [bare.StreamingEnv] and [bare.StreamingShell] (internal/exec/bare's
// streaming.go), and this is the test that says it worked: a Python print loop's
// first lines are readable while the loop is still running.
func TestAPythonPrintLoopIsReadableBeforeItExits(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("no python3 on this machine")
	}
	agent, _ := jobsAgent(t)

	// No flush() anywhere: a script that flushes was never the problem, and one
	// that does not is the whole problem.
	script := "import time\n" +
		"for case in range(40):\n" +
		"    print(\"scored case %d\" % case)\n" +
		"    time.sleep(0.1)\n"
	id := startJob(t, agent, "python3 -c "+shellQuote(script))
	running := agent.jobs.find(id)
	if running == nil {
		t.Fatalf("job %d is not in the registry", id)
	}

	waitFor(t, "the first line of a still-running python loop", func() bool {
		return strings.Contains(running.sink.text(), "scored case 0")
	})
	if !running.running() {
		t.Fatal("the loop had already exited, so this proves nothing about buffering")
	}
	// And the footer the model reads says the same thing, which is the point of
	// having partial output at all.
	if footer := agent.jobs.runningFooter(); !strings.Contains(footer, "· last: scored case ") {
		t.Fatalf("the job's footer quotes nothing: %q", footer)
	}
	if _, isError := agent.jobs.kill(id); isError {
		t.Fatal("could not stop the loop")
	}
}

// shellQuote wraps a script in single quotes for `bash -c`, which is enough for
// a test whose scripts contain no quotes of their own.
func shellQuote(script string) string { return "'" + script + "'" }

// ── 3. a promoted call answers with what it has ─────────────────────────────

// A CALL THAT OUTLIVES THE CEILING HANDS BACK THE OUTPUT SO FAR. The id and the
// path used to be the whole answer, which taught the model nothing about the
// work and made its next move a call to go and read the log.
func TestAPromotedCallCarriesTheOutputSoFar(t *testing.T) {
	t.Parallel()
	agent, _ := jobsAgent(t)

	answer, isError := runBash(t, context.Background(), agent, map[string]any{
		"command": "echo plan: 120 cases; sleep 5",
		"timeout": 0.4,
	})
	if isError {
		t.Fatalf("a promoted call answered as an error: %q", answer)
	}
	id := promotedJobID(t, answer)
	if !strings.Contains(answer, "plan: 120 cases") {
		t.Fatalf("the promotion sentence withheld the output the command had already printed: %q", answer)
	}
	if _, isError := agent.jobs.kill(id); isError {
		t.Fatal("could not stop the promoted job")
	}
}

// ── 4. the state of every stream rides on every result ──────────────────────

// A SHELL PRINTS ITS BACKGROUND JOBS UNDER THE PROMPT, AND SO DOES THIS. One
// line per outstanding job at the foot of every tool result — alive, this long,
// last said this — so the question the model used to spend a call on is answered
// before it can ask.
func TestEveryToolResultCarriesTheStateOfEveryRunningJob(t *testing.T) {
	t.Parallel()
	agent, _ := jobsAgent(t)
	hub := newEventHub()
	defer hub.close()

	id := startJob(t, agent, "echo case 41 of 120; sleep 5")
	waitFor(t, "the job's first line", func() bool {
		return strings.Contains(agent.jobs.find(id).sink.text(), "case 41 of 120")
	})

	// A call about something else entirely: the footer is the SESSION's state,
	// not this call's.
	results := agent.runToolsWarm(context.Background(), agent.newEpisode(), []ai.ToolCall{
		fixBash("echo unrelated"),
	}, hub, nil)
	if len(results) != 1 {
		t.Fatalf("want one result, got %d", len(results))
	}
	want := fmt.Sprintf("[job %d] running ", id)
	if !strings.Contains(results[0].text, want) {
		t.Fatalf("the result does not carry the running job:\n%q", results[0].text)
	}
	if !strings.Contains(results[0].text, "· last: case 41 of 120") {
		t.Fatalf("the footer does not say what the job last said:\n%q", results[0].text)
	}
	if !strings.HasPrefix(results[0].text, "unrelated") {
		t.Fatalf("the footer displaced the tool's own answer:\n%q", results[0].text)
	}

	// AND IT GOES AWAY WHEN THE WORK DOES. A footer that outlived its job would
	// be the model told about a stream that no longer exists.
	if _, isError := agent.jobs.kill(id); isError {
		t.Fatal("could not stop the job")
	}
	after := agent.runToolsWarm(context.Background(), agent.newEpisode(), []ai.ToolCall{
		fixBash("echo done"),
	}, hub, nil)
	if strings.Contains(after[0].text, "[job ") {
		t.Fatalf("a finished job is still on the footer:\n%q", after[0].text)
	}
}

// ── the strip, which is what keeps the footer out of every comparison ───────

// THE FOOTER IS NOT INFORMATION ABOUT THE WORK. It carries an elapsed time, so
// it differs on every single result, and anything comparing two results has to
// take it off first or every result looks new.
func TestTheJobFooterComesOffAgainExactly(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ name, text, want string }{
		{"a body and one job", "(no output)\n\n[job 1] running 3m12s · last: case 41", "(no output)"},
		{"a body and three jobs",
			"ok\n\n[job 1] running 3m12s · last: a\n[job 2] running 12s\n[job 7] running 1h04m · last: b", "ok"},
		{"a footer and nothing else", "[job 1] running 9s", ""},
		{"no footer at all", "ok\n\nall tests passed", "ok\n\nall tests passed"},
		{"a body that merely talks about jobs", "see [job 1] in the log", "see [job 1] in the log"},
		{"a body whose last line looks like one but is not", "[job one] running 3s", "[job one] running 3s"},
	} {
		if got := stripJobFooter(c.text); got != c.want {
			t.Fatalf("%s: stripped to %q, want %q", c.name, got, c.want)
		}
	}

	// The two halves are written against each other, so a rendered footer is a
	// stripped footer whatever the numbers in it are.
	body := "FAIL\tgithub.com/x [build failed]"
	for _, elapsed := range []time.Duration{9 * time.Second, 3*time.Minute + 12*time.Second, 64 * time.Minute} {
		grown := body + "\n\n[job 4] running " + formatJobAge(elapsed) + " · last: still compiling"
		if got := stripJobFooter(grown); got != body {
			t.Fatalf("a %v footer stripped to %q", elapsed, got)
		}
	}
}

// ── 5. progress is information, never activity ──────────────────────────────

// NINE DISTINCT POLLS WITH ONE ANSWER ARE NINE STEPS OF NOTHING.
//
// This is the defect exactly as it was measured: the model wrote
// `sleep 30 && tail`, `sleep 45 && tail`, `sleep 60 && tail` — every command
// string different, every answer `(no output)` — and the counter that exists to
// notice a node getting nowhere read each one as a discovery and reset itself.
func TestNineIdenticalAnswersToNineDistinctCommandsAreNoProgress(t *testing.T) {
	t.Parallel()
	seen := newProgressLedger()
	progress := 0
	for poll := 1; poll <= 9; poll++ {
		event := Event{
			Kind:   EventToolEnd,
			Tool:   "bash",
			Args:   fmt.Sprintf(`{"command":"sleep %d && tail jobs/1.log"}`, poll*15),
			Output: "(no output)",
		}
		// Each poll's result carries the job's state, which is a different
		// string every time — the exact trap the strip exists for.
		event.Output += fmt.Sprintf("\n\n[job 1] running %dm%02ds", poll, poll*3)
		if taughtSomething(event, seen) {
			progress++
		}
	}
	// The FIRST poll is a discovery: the node did not know the log was empty.
	// The other eight are the spin.
	if progress != 1 {
		t.Fatalf("%d of nine identical answers counted as progress, want exactly the first", progress)
	}
}

// AND THE COUNTER ACTUALLY KILLS IT. The unit above says the answer is not new;
// this one runs a real node through the real drain and says the node stops.
func TestANodeThatPollsItsWayNowhereIsStopped(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	// Nine distinct commands, every one of them silent: the shape of a poll
	// loop, with none of the sleeping.
	child := make([]step, 0, 10)
	for poll := 1; poll <= 9; poll++ {
		command := fmt.Sprintf("sleep 0.0%d && cat /dev/null", poll)
		child = append(child, func(context.Context, []ai.Message) (*ai.Response, error) {
			arguments, _ := json.Marshal(struct {
				Command string `json:"command"`
			}{Command: command})
			return toolResponse("call-poll", "bash", string(arguments)), nil
		})
	}
	completer := &routedCompleter{
		parent: []step{
			func(context.Context, []ai.Message) (*ai.Response, error) {
				arguments, _ := json.Marshal(taskArguments{
					Title: "Score", Summary: "s", Brief: "score it\n" + taskBriefMark,
					Deliverable: "d", Acceptance: "a", NoProgress: 6, MaxSteps: 30,
				})
				return toolResponse("call-task", "propose_task", string(arguments)), nil
			},
			finalText("handed off"),
		},
		child: append(child, finalText("gave up")),
	}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.AskConsent = false
		config.TaskAutoApproveSeconds = 0
		config.TaskAudit = false
	})
	graph := agent.graph()
	collect(t, mustSubmit(t, agent, "score it"))
	node := graph.node(1)
	waitDoneNode(t, node)

	notice := node.notice()
	if !strings.Contains(notice.Report, "stopped: 6 steps without progress") {
		t.Fatalf("nine silent polls were not read as a spin: %q", notice.Report)
	}
}
