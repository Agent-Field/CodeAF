package session

// Promote-to-background tests.
//
// The subject is a handoff between two packages and four racing endings, so
// every case here runs a REAL process against a REAL timer — a fake clock would
// test the fake, and the thing that used to be broken was the ordering, not the
// arithmetic. The timers are the smallest ones the law allows a caller to spell,
// and every wait is a polled condition with a deadline (jobs_test.go's
// [waitFor]) rather than a sleep long enough to be reliable on a loaded machine.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/exec/bare"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// bashAnswer is one bash call's result carried whole, so a call made from a
// goroutine reports back rather than failing from somewhere the testing package
// cannot hear it.
type bashAnswer struct {
	text    string
	isError bool
	err     error
}

// callBash runs the belt's bash under a context of the caller's choosing, which
// is what these tests need and [runTool] deliberately does not offer.
func callBash(ctx context.Context, agent *Agent, arguments map[string]any) bashAnswer {
	encoded, err := json.Marshal(arguments)
	if err != nil {
		return bashAnswer{err: err}
	}
	for _, tool := range agent.tools {
		if tool.Name != "bash" {
			continue
		}
		text, isError, err := tool.Execute(ctx, json.RawMessage(encoded))
		return bashAnswer{text: text, isError: isError, err: err}
	}
	return bashAnswer{err: errors.New("the belt has no bash tool")}
}

func runBash(t *testing.T, ctx context.Context, agent *Agent, arguments map[string]any) (string, bool) {
	t.Helper()
	got := callBash(ctx, agent, arguments)
	if got.err != nil {
		t.Fatalf("bash returned a harness error: %v", got.err)
	}
	return got.text, got.isError
}

// bareBashTool is pi's own bash, unwrapped. It is the only way to have a
// foreground call with nobody behind it to take the process, which is exactly
// what a subharness leaf runs.
func bareBashTool(t *testing.T, agent *Agent) bare.Tool {
	t.Helper()
	for _, tool := range bare.AllTools(agent.config.Workspace) {
		if tool.Name == "bash" {
			return tool
		}
	}
	t.Fatal("bare has no bash tool")
	return bare.Tool{}
}

// promotedJobID reads the id out of the sentence a promoted call answers with.
func promotedJobID(t *testing.T, line string) int {
	t.Helper()
	var id int
	if _, err := fmt.Sscanf(line, "still running as job %d", &id); err != nil {
		t.Fatalf("not a promotion sentence: %q", line)
	}
	return id
}

// ── the timeout ─────────────────────────────────────────────────────────────

// THE WHOLE POINT: A COMMAND THAT RUNS OUT OF TIME IS ADOPTED, NOT KILLED.
//
// The call answers with the sentence a background start already speaks, the
// process is still running under the registry, and its exit reaches the session
// through the lane every other job's exit rides.
func TestAForegroundCommandThatRunsOutOfTimeBecomesAJob(t *testing.T) {
	t.Parallel()
	agent, _ := jobsAgent(t)

	answer, isError := runBash(t, context.Background(), agent, map[string]any{
		"command": "sleep 1; echo the-build-finished",
		"timeout": 0.4,
	})
	// A NORMAL RESULT, because the transcript has to stay legal: the turn
	// carries on from here and nothing about this call went wrong.
	if isError {
		t.Fatalf("a promoted call answered as an error: %q", answer)
	}
	id := promotedJobID(t, answer)
	if !strings.Contains(answer, "; log at ") {
		t.Fatalf("the promotion sentence does not name the log: %q", answer)
	}

	// It is a job, and it is still running — which is the work that used to be
	// thrown away.
	promoted := agent.jobs.find(id)
	if promoted == nil {
		t.Fatalf("job %d is not in the registry", id)
	}
	if !promoted.running() {
		t.Fatal("the adopted process was killed rather than kept")
	}

	// And its exit rides the lane, like any other job's.
	waitFor(t, "the exit note", func() bool {
		return notesContain(agent, fmt.Sprintf("job %d exited 0", id))
	})
	if !notesContain(agent, "the-build-finished") {
		t.Fatalf("the exit note does not quote the last line: %v", sessionNotes(agent))
	}

	// The output the command produced AFTER the promotion is in the job's log,
	// which is where the sentence said it would be.
	logged, err := os.ReadFile(promoted.logPath)
	if err != nil {
		t.Fatalf("read the job log: %v", err)
	}
	if !strings.Contains(string(logged), "the-build-finished") {
		t.Fatalf("the job log is missing the command's output: %q", logged)
	}
}

// M1: the background-after clock returns the existing promotion result and
// roster row promptly, then carries the real exit into the next model boundary.
func TestTheBackgroundAfterClockKeepsTheCommandAndTheConversationMovesOn(t *testing.T) {
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("clock-call", "bash", `{"command":"sleep 5; echo late"}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("I moved on while it finishes."), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("The late command finished."), nil
		},
	}}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.BashBackgroundAfterSeconds = 2
	})
	lane := rosterLane(t, agent)

	started := time.Now()
	turn := mustSubmit(t, agent, "run the slow command and continue")
	row := awaitJobRow(t, lane, TaskRunning)
	collect(t, turn)
	elapsed := time.Since(started)
	if elapsed < 1500*time.Millisecond || elapsed > 4*time.Second {
		t.Fatalf("the foreground turn returned after %v, want the two-second clock", elapsed)
	}

	request := completer.request(1)
	result := roleText(request, "tool")
	const resultLead = "still running as job 1; log at "
	if !strings.Contains(result, resultLead) || !strings.Contains(result, ".log") {
		t.Fatalf("the tool result did not carry the existing promotion sentence and log: %q", result)
	}
	firstLine := strings.SplitN(result, "\n", 2)[0]
	logPath := strings.TrimPrefix(firstLine, resultLead)
	if row.Report != jobRowLead(1, logPath) {
		t.Fatalf("the running roster row is %q, want %q", row.Report, jobRowLead(1, logPath))
	}
	waitFor(t, "the promoted call to leave the in-flight set", func() bool {
		return agent.inFlightBash.find("clock-call") == nil
	})
	waitFor(t, "the exit to reach the next model boundary", func() bool {
		return completer.requests() >= 3
	})
	if got := strings.Join(userLines(completer.request(2)), "\n"); !strings.Contains(got, "job 1 exited 0: late") {
		t.Fatalf("the next boundary did not receive the exit note: %q", got)
	}
}

// M2: a command that finishes before the background-after clock remains an
// ordinary foreground result and never enters the job registry.
func TestAQuickForegroundCommandFinishesNormallyBeforeTheClock(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.BashBackgroundAfterSeconds = 1
	})
	answer, isError := runBash(t, context.Background(), agent, map[string]any{
		"command": "sleep 0.2; echo quick",
	})
	if isError || strings.TrimSpace(answer) != "quick" {
		t.Fatalf("quick foreground bash answered %q (error=%v)", answer, isError)
	}
	if jobs := agent.jobs.all(); len(jobs) != 0 {
		t.Fatalf("a quick command left %d jobs", len(jobs))
	}
}

// The output a command produced BEFORE its promotion is not lost: bare's tail is
// replayed into the job's log, so the file reads as one command from the top.
func TestAPromotedCommandKeepsWhatItPrintedBeforeThePromotion(t *testing.T) {
	t.Parallel()
	agent, _ := jobsAgent(t)

	answer, isError := runBash(t, context.Background(), agent, map[string]any{
		"command": "echo before-the-bound; sleep 1; echo after-the-bound",
		"timeout": 0.4,
	})
	if isError {
		t.Fatalf("a promoted call answered as an error: %q", answer)
	}
	id := promotedJobID(t, answer)
	waitExited(t, agent, id)

	logged, err := os.ReadFile(agent.jobs.find(id).logPath)
	if err != nil {
		t.Fatalf("read the job log: %v", err)
	}
	for _, wanted := range []string{"before-the-bound", "after-the-bound"} {
		if !strings.Contains(string(logged), wanted) {
			t.Fatalf("the job log lost %q: %q", wanted, logged)
		}
	}
}

// With nothing there to take the process, the law is exactly what it always was.
// This is the path a tool taken straight off bare's belt runs on.
func TestWithNobodyToPromoteItATimeoutStillKills(t *testing.T) {
	t.Parallel()
	agent, _ := jobsAgent(t)

	// The registry is reached through the wrapper, so the way to have no
	// promoter is to call the tool bare's belt carries rather than the session's.
	tool := bareBashTool(t, agent)
	answer, isError, err := tool.Execute(context.Background(),
		json.RawMessage(`{"command":"sleep 5","timeout":0.3}`))
	if err != nil {
		t.Fatalf("bash returned a harness error: %v", err)
	}
	if !isError || !strings.Contains(answer, "Command timed out after") {
		t.Fatalf("got %q, want the timeout wording", answer)
	}
	if list := agent.jobs.list(); list != "No background jobs." {
		t.Fatalf("a killed call left a job behind: %q", list)
	}
}

// ── the interrupt ───────────────────────────────────────────────────────────

// A KILL THE PERSON ASKED FOR IS STILL A KILL. esc cancels the turn's context,
// and a cancelled call may not be promoted at any price: the process dies and
// no job row is left behind for work somebody just said they did not want.
func TestAnInterruptedCommandIsNeverPromoted(t *testing.T) {
	t.Parallel()
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.BashBackgroundAfterSeconds = 1
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan bashAnswer, 1)
	go func() {
		done <- callBash(ctx, agent, map[string]any{
			"command": "sleep 5",
			// A bound the interrupt will beat to it — the timeout must not be
			// able to promote a call that is already cancelled.
			"timeout": 5,
		})
	}()

	// Interrupt WHILE it runs and BEFORE the bound.
	time.Sleep(100 * time.Millisecond)
	cancel()

	var got bashAnswer
	select {
	case got = <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the interrupted call never returned")
	}
	if got.err != nil {
		t.Fatalf("bash returned a harness error: %v", got.err)
	}
	if !got.isError || !strings.Contains(got.text, "Command aborted") {
		t.Fatalf("got %q, want the abort wording", got.text)
	}
	if list := agent.jobs.list(); list != "No background jobs." {
		t.Fatalf("an interrupt left a job behind: %q", list)
	}
	// And the promotion door refuses the call even now.
	if _, promoted := agent.PromoteCall("nothing-by-that-name"); promoted {
		t.Fatal("the door promoted a call that does not exist")
	}
}

// M4: once the clock has adopted a call, the key door cannot adopt it again;
// the one process has exactly one job id and one tool result.
func TestTheClockWinsOnceAndAKeyCannotAdoptTheCallAgain(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.BashBackgroundAfterSeconds = 1
	})
	const callID = "clock-then-key"
	answers := make(chan bashAnswer, 1)
	go func() {
		answers <- callBash(withCallID(context.Background(), callID), agent, map[string]any{
			"command": "sleep 2; echo once",
		})
	}()
	waitFor(t, "the clock to adopt the call", func() bool { return len(agent.jobs.all()) == 1 })
	if _, promoted := agent.PromoteCall(callID); promoted {
		t.Fatal("the key adopted a call the clock had already taken")
	}
	select {
	case answer := <-answers:
		if answer.err != nil || answer.isError || !strings.Contains(answer.text, "still running as job 1") {
			t.Fatalf("the clock's tool result was %+v", answer)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the clock-promoted call never answered")
	}
	if jobs := agent.jobs.all(); len(jobs) != 1 || jobs[0].id != 1 {
		t.Fatalf("the one command became %+v", jobs)
	}
}

// ── the door the surface presses ────────────────────────────────────────────

// A RUNNING CALL CAN BE PROMOTED ON DEMAND, not only when its clock runs out.
// This is the seam internal/tui3's ctrl+g presses, and it is the same adoption:
// one registry, one job, no kill and no restart.
func TestARunningForegroundCommandCanBeSentToTheBackgroundOnDemand(t *testing.T) {
	t.Parallel()
	agent, _ := jobsAgent(t)

	const callID = "call-the-surface-can-see"
	answers := make(chan bashAnswer, 1)
	go func() {
		answers <- callBash(withCallID(context.Background(), callID), agent, map[string]any{
			"command": "sleep 1; echo let-it-run",
			// Far beyond the moment the key is pressed, so nothing here can be
			// the timeout doing the work.
			"timeout": 30,
		})
	}()

	waitFor(t, "the call to become promotable", func() bool {
		return agent.inFlightBash.find(callID) != nil
	})
	line, promoted := agent.PromoteCall(callID)
	if !promoted {
		t.Fatal("the door refused a call that was running")
	}
	id := promotedJobID(t, line)

	// The tool's own result is the same sentence, so the transcript stays legal
	// and the turn carries on.
	select {
	case answer := <-answers:
		if answer.err != nil {
			t.Fatalf("bash returned a harness error: %v", answer.err)
		}
		if answer.text != line {
			t.Fatalf("the call answered %q, the door said %q", answer.text, line)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the promoted call never returned")
	}

	if target := agent.jobs.find(id); target == nil {
		t.Fatalf("job %d is not in the registry", id)
	}
	waitFor(t, "the exit note", func() bool {
		return notesContain(agent, fmt.Sprintf("job %d exited 0", id))
	})

	// The call is gone from the promotable set the moment it answered: a second
	// press has nothing to find.
	waitFor(t, "the call to be forgotten", func() bool {
		return agent.inFlightBash.find(callID) == nil
	})
	if _, again := agent.PromoteCall(callID); again {
		t.Fatal("a finished call was promoted a second time")
	}
}

// A background call was a job from the first instant and has no foreground
// process to promote — the door never sees it, because the wrapper branched
// before the promoter was fitted.
func TestABackgroundCallIsNeverPromotable(t *testing.T) {
	t.Parallel()
	agent, _ := jobsAgent(t)

	const callID = "call-that-asked-for-background"
	answer, isError := runBash(t, withCallID(context.Background(), callID), agent, map[string]any{
		"command": "sleep 5", "background": true,
	})
	if isError {
		t.Fatalf("background bash failed: %s", answer)
	}
	if !strings.HasPrefix(answer, "job ") {
		t.Fatalf("got %q, want a background start line", answer)
	}
	if agent.inFlightBash.find(callID) != nil {
		t.Fatal("a background call registered itself as promotable")
	}
	if _, promoted := agent.PromoteCall(callID); promoted {
		t.Fatal("a background call was promoted")
	}
}
