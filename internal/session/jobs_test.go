package session

// Background-job tests. Nothing here starts a real long-lived process: the
// longest-lived thing is a `sleep` that exists only to be killed, and every
// wait is a polled condition with a deadline rather than a fixed pause — a
// sleep long enough to be reliable on a loaded machine is a sleep that makes
// the suite slow on every other one.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/exec/bare"
)

// ── harness ─────────────────────────────────────────────────────────────────

// jobsAgent builds an agent with no scripted provider steps: these tests drive
// the belt directly, because the point under test is the tool and the registry,
// not the loop that calls them.
func jobsAgent(t *testing.T) (*Agent, string) {
	t.Helper()
	return newTestAgent(t, &scriptedCompleter{}, nil)
}

func beltTool(t *testing.T, agent *Agent, name string) bare.Tool {
	t.Helper()
	for _, tool := range agent.tools {
		if tool.Name == name {
			return tool
		}
	}
	t.Fatalf("belt has no %q tool", name)
	return bare.Tool{}
}

// runTool calls one belt tool and fails on a harness-level error, which no tool
// in this slice is supposed to return.
func runTool(t *testing.T, agent *Agent, name, arguments string) (string, bool) {
	t.Helper()
	tool := beltTool(t, agent, name)
	text, isError, err := tool.Execute(context.Background(), json.RawMessage(arguments))
	if err != nil {
		t.Fatalf("%s returned a harness error: %v", name, err)
	}
	return text, isError
}

// waitFor polls a condition to a deadline. Every job assertion is about
// something another goroutine will do shortly, and this is how the tests say
// "shortly" without saying "in exactly 40ms".
func waitFor(t *testing.T, what string, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// steeringQueue copies the queue under the agent's own lock — the same lock the
// loop drains it with, which is what makes these tests race-clean.
func steeringQueue(agent *Agent) []string {
	agent.mu.Lock()
	defer agent.mu.Unlock()
	queued := make([]string, 0, len(agent.steering))
	for _, message := range agent.steering {
		queued = append(queued, message.text())
	}
	return queued
}

// ambientQueue copies the boundary-held notes under the same lock the turn
// takes them with. It stays separate from steeringQueue so a test cannot pass
// while a watch update has regressed onto the mid-turn lane.
func ambientQueue(agent *Agent) []string {
	agent.mu.Lock()
	defer agent.mu.Unlock()
	queued := make([]string, 0, len(agent.ambient))
	for _, message := range agent.ambient {
		queued = append(queued, message.text())
	}
	return queued
}

func steeringContains(agent *Agent, substring string) bool {
	for _, line := range steeringQueue(agent) {
		if strings.Contains(line, substring) {
			return true
		}
	}
	return false
}

// sessionNotes is every line the SESSION has been told, in arrival order: what a
// wake has already drained into the transcript, then whatever is still queued
// behind it.
//
// It exists because a job's exit can wake and drain immediately while a watch's
// delta waits on the ambient queue. A test that read either queue alone would
// confuse delivery with loss. The transcript plus both queues are ONE fact —
// the session was told — and every test in this file and tools_watch_test.go is
// about that fact rather than which legal place currently holds it.
func sessionNotes(agent *Agent) []string {
	agent.mu.Lock()
	defer agent.mu.Unlock()
	notes := make([]string, 0, len(agent.steering)+len(agent.ambient)+2)
	for _, message := range agent.messages {
		if message.Role == "user" {
			notes = append(notes, messageText(message))
		}
	}
	for _, message := range agent.steering {
		notes = append(notes, message.text())
	}
	for _, message := range agent.ambient {
		notes = append(notes, message.text())
	}
	return notes
}

func notesContain(agent *Agent, substring string) bool {
	for _, line := range sessionNotes(agent) {
		if strings.Contains(line, substring) {
			return true
		}
	}
	return false
}

// startJob runs one background bash call and returns the job's id.
func startJob(t *testing.T, agent *Agent, command string) int {
	t.Helper()
	arguments, err := json.Marshal(map[string]any{"command": command, "background": true})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	text, isError := runTool(t, agent, "bash", string(arguments))
	if isError {
		t.Fatalf("background bash failed: %s", text)
	}
	var id int
	if _, err := fmt.Sscanf(text, "job %d started", &id); err != nil {
		t.Fatalf("background bash did not report a job id: %q", text)
	}
	return id
}

// tailLines splits an output result into its log lines, dropping the footer.
func tailLines(t *testing.T, output string) []string {
	t.Helper()
	index := strings.LastIndex(output, "\n\n[job ")
	if index < 0 {
		t.Fatalf("output has no footer: %q", output)
	}
	body := output[:index]
	if body == "" {
		return nil
	}
	return strings.Split(body, "\n")
}

func waitExited(t *testing.T, agent *Agent, id int) {
	t.Helper()
	waitFor(t, fmt.Sprintf("job %d to exit", id), func() bool {
		target := agent.jobs.find(id)
		return target != nil && !target.running()
	})
}

// ── the wire ────────────────────────────────────────────────────────────────

// The wrapper must be pi's bash plus one argument — not a rewrite of it.
func TestBackgroundBashSchemaExtendsBare(t *testing.T) {
	agent, _ := jobsAgent(t)

	tool := beltTool(t, agent, "bash")
	var schema struct {
		Properties map[string]json.RawMessage `json:"properties"`
		Required   []string                   `json:"required"`
	}
	if err := json.Unmarshal(tool.Schema, &schema); err != nil {
		t.Fatalf("bash schema does not parse: %v", err)
	}
	for _, property := range []string{"command", "timeout", "background"} {
		if _, present := schema.Properties[property]; !present {
			t.Fatalf("bash schema lost %q: %s", property, tool.Schema)
		}
	}
	if len(schema.Required) != 1 || schema.Required[0] != "command" {
		t.Fatalf("bash required changed: %v", schema.Required)
	}
	if !strings.Contains(tool.Description, "background:true") {
		t.Fatal("bash description does not mention background")
	}
	// The whole belt must still be buildable on the wire, jobs included.
	if _, err := toolDefinitions(agent.tools); err != nil {
		t.Fatalf("belt with jobs does not build: %v", err)
	}
	if _, present := schemaProperties(t, beltTool(t, agent, "jobs").Schema)["action"]; !present {
		t.Fatal("jobs schema has no action")
	}
}

func schemaProperties(t *testing.T, schema json.RawMessage) map[string]json.RawMessage {
	t.Helper()
	var decoded struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(schema, &decoded); err != nil {
		t.Fatalf("schema does not parse: %v", err)
	}
	return decoded.Properties
}

// Foreground bash is unchanged: it waits, and it returns the output.
func TestForegroundBashUnchanged(t *testing.T) {
	agent, _ := jobsAgent(t)

	text, isError := runTool(t, agent, "bash", `{"command":"echo foreground"}`)
	if isError {
		t.Fatalf("foreground bash failed: %s", text)
	}
	if !strings.Contains(text, "foreground") {
		t.Fatalf("foreground bash did not return its output: %q", text)
	}
	if strings.Contains(text, "job ") {
		t.Fatalf("foreground bash reported a job: %q", text)
	}
	// background:false is the same path, and the extra field must not confuse
	// bare's parser.
	text, isError = runTool(t, agent, "bash", `{"command":"echo plain","background":false}`)
	if isError || !strings.Contains(text, "plain") {
		t.Fatalf("background:false did not run in the foreground: %q", text)
	}
	if got := agent.jobs.list(); got != "No background jobs." {
		t.Fatalf("foreground calls registered a job: %q", got)
	}
}

// ── starting ────────────────────────────────────────────────────────────────

// The call returns while the process is still running, with an id and a log
// file that exists — the two things the model needs to follow up.
func TestBackgroundBashReturnsImmediately(t *testing.T) {
	agent, workspace := jobsAgent(t)

	started := time.Now()
	text, isError := runTool(t, agent, "bash", `{"command":"sleep 30","background":true}`)
	elapsed := time.Since(started)
	if isError {
		t.Fatalf("background bash failed: %s", text)
	}
	if elapsed > 3*time.Second {
		t.Fatalf("background bash waited %s for a 30s command", elapsed)
	}

	logPath := filepath.Join(workspace, ".aforge-v3", "jobs", "1.log")
	if !strings.Contains(text, "job 1 started") || !strings.Contains(text, logPath) {
		t.Fatalf("unexpected start line: %q (wanted job 1 and %s)", text, logPath)
	}
	if _, err := os.Stat(logPath); err != nil {
		t.Fatalf("log file was not created: %v", err)
	}
	if !agent.jobs.find(1).running() {
		t.Fatal("the sleeper is not running")
	}
	// The agent's Close (t.Cleanup) is what ends it.
}

// ── output ──────────────────────────────────────────────────────────────────

// output returns the LAST n lines, and never more than the cap however large
// the request.
func TestJobsOutputTailAndCap(t *testing.T) {
	agent, _ := jobsAgent(t)

	id := startJob(t, agent, "for i in $(seq 1 300); do echo line$i; done")
	waitExited(t, agent, id)

	text, isError := runTool(t, agent, "jobs", fmt.Sprintf(`{"action":"output","id":%d,"tail":3}`, id))
	if isError {
		t.Fatalf("jobs output failed: %s", text)
	}
	if got := tailLines(t, text); len(got) != 3 || got[0] != "line298" || got[2] != "line300" {
		t.Fatalf("tail 3 returned %v", got)
	}

	// The default is a screenful.
	text, _ = runTool(t, agent, "jobs", fmt.Sprintf(`{"action":"output","id":%d}`, id))
	if got := tailLines(t, text); len(got) != jobsDefaultTail {
		t.Fatalf("default tail returned %d lines, want %d", len(got), jobsDefaultTail)
	}

	// And the cap holds against a request for everything.
	text, _ = runTool(t, agent, "jobs", fmt.Sprintf(`{"action":"output","id":%d,"tail":5000}`, id))
	got := tailLines(t, text)
	if len(got) != jobsMaxTail {
		t.Fatalf("tail 5000 returned %d lines, want the %d cap", len(got), jobsMaxTail)
	}
	if got[len(got)-1] != "line300" {
		t.Fatalf("capped tail is not the END of the log: %q", got[len(got)-1])
	}
	// The footer points at the file, which is where the other 100 lines are.
	if !strings.Contains(text, "full log:") {
		t.Fatalf("output footer does not name the log: %q", text)
	}
}

func TestJobsOutputUnknownJob(t *testing.T) {
	agent, _ := jobsAgent(t)
	text, isError := runTool(t, agent, "jobs", `{"action":"output","id":9}`)
	if !isError || !strings.Contains(text, "No job 9") {
		t.Fatalf("unknown job did not report as an error: %q", text)
	}
}

// ── kill ────────────────────────────────────────────────────────────────────

// An explicit kill ends the process, marks it killed, and — the guard — does
// NOT put a completion note on the steering queue: the caller just did it.
func TestJobsKillEndsSleeperWithoutSelfReport(t *testing.T) {
	agent, _ := jobsAgent(t)

	id := startJob(t, agent, "sleep 30")
	text, isError := runTool(t, agent, "jobs", fmt.Sprintf(`{"action":"kill","id":%d}`, id))
	if isError {
		t.Fatalf("kill failed: %s", text)
	}
	if !strings.Contains(text, fmt.Sprintf("job %d killed", id)) {
		t.Fatalf("unexpected kill line: %q", text)
	}

	killed := agent.jobs.find(id)
	if killed.running() {
		t.Fatal("the sleeper is still running after kill")
	}
	if state := killed.info().state; state != jobKilled {
		t.Fatalf("state after kill is %v, want jobKilled", state)
	}
	if list := agent.jobs.list(); !strings.Contains(list, "killed") {
		t.Fatalf("list does not show the kill: %q", list)
	}

	// The note would be written by the watcher, which has already run — done is
	// closed by the time kill returns. A short settle covers the ordering
	// anyway, then the session must still have been told nothing.
	time.Sleep(50 * time.Millisecond)
	if queued := sessionNotes(agent); len(queued) != 0 {
		t.Fatalf("a killed job reported itself: %v", queued)
	}

	// Killing it twice is not an error the model should chase.
	text, isError = runTool(t, agent, "jobs", fmt.Sprintf(`{"action":"kill","id":%d}`, id))
	if !isError || !strings.Contains(text, "already killed") {
		t.Fatalf("second kill said %q", text)
	}
}

// ── completion ──────────────────────────────────────────────────────────────

// A job that ends on its own lands its note on the steering lane — asserted
// here as the LINE, not as a turn: what the note says is this file's contract,
// and when the model reads it is the loop's law (wake_test.go owns that half).
func TestExitingJobLandsSteeringNote(t *testing.T) {
	agent, _ := jobsAgent(t)
	// Hold the session before its public opening so this remains an assertion
	// about the steering note itself rather than the boundary batch around it.
	agent.mu.Lock()
	agent.opened = false
	agent.mu.Unlock()

	id := startJob(t, agent, "echo build finished; exit 3")
	waitFor(t, "the completion note", func() bool {
		return notesContain(agent, fmt.Sprintf("job %d exited 3", id))
	})

	queued := sessionNotes(agent)
	if len(queued) != 1 {
		t.Fatalf("want exactly one note, got %v", queued)
	}
	// The HEADLINE quotes the last non-empty log line, which is the one thing a
	// person (or a model) reads a completion for.
	headline, _, _ := strings.Cut(queued[0], "\n")
	if !strings.HasSuffix(headline, ": build finished") {
		t.Fatalf("note does not quote the last line: %q", queued[0])
	}
	if length := len(headline); length > 160 {
		t.Fatalf("headline is %d bytes; it is a sentence, not the log", length)
	}
	// THE NOTE CARRIES THE OUTPUT UNDER ITS HEADLINE and names the whole log, so
	// the ending itself never costs another tool call.
	if !strings.Contains(queued[0], "\n\nbuild finished") {
		t.Fatalf("completion note lost the output tail: %q", queued[0])
	}
	footer := fmt.Sprintf("[job %d · last %d lines · full log: ", id, jobExitTailLines)
	if !strings.Contains(queued[0], footer) {
		t.Fatalf("completion note does not name the full log: %q", queued[0])
	}
	output, isError := runTool(t, agent, "jobs", fmt.Sprintf(`{"action":"output","id":%d}`, id))
	if isError || !strings.Contains(output, "build finished") || !strings.Contains(output, "full log: ") {
		t.Fatalf("jobs output lost the completion detail: %q", output)
	}
}

// An ordinary background job's ending reaches both the steering lane and the
// model's boundary batch whole: headline, output tail and path to the full log.
func TestAnOrdinaryJobsEndingArrivesWhole(t *testing.T) {
	agent, _ := jobsAgent(t)
	// Hold the session before its public opening so the test can inspect the
	// steering lane before taking the same boundary the model reads.
	agent.mu.Lock()
	agent.opened = false
	agent.mu.Unlock()

	id := startJob(t, agent, "printf 'a\nb\nc\n'")
	waitFor(t, "the completion note", func() bool {
		return notesContain(agent, fmt.Sprintf("job %d exited 0", id))
	})

	queued := steeringQueue(agent)
	if len(queued) != 1 {
		t.Fatalf("want exactly one queued note, got %v", queued)
	}
	headline, _, _ := strings.Cut(queued[0], "\n")
	if want := fmt.Sprintf("job %d exited 0: c", id); headline != want {
		t.Fatalf("completion headline is %q, want %q", headline, want)
	}
	tail := "\n\na\nb\nc"
	if !strings.Contains(queued[0], tail) {
		t.Fatalf("completion note lost its whole output tail: %q", queued[0])
	}
	job := agent.jobs.find(id)
	footer := fmt.Sprintf("[job %d · last %d lines · full log: %s]", id, jobExitTailLines, job.logPath)
	if !strings.Contains(queued[0], footer) {
		t.Fatalf("completion note lost its log path: %q", queued[0])
	}

	agent.drainSteering(nil)
	var users []string
	for _, message := range agent.snapshot() {
		if message.Role == "user" {
			users = append(users, messageText(message))
		}
	}
	if len(users) != 1 || !strings.Contains(users[0], "while you worked:") ||
		!strings.Contains(users[0], tail) || !strings.Contains(users[0], footer) {
		t.Fatalf("the whole ending did not survive its boundary batch: %v", users)
	}
}

// A silent job still reports: the exit code alone is the news.
func TestSilentExitingJobReportsCodeOnly(t *testing.T) {
	agent, _ := jobsAgent(t)

	id := startJob(t, agent, "false")
	waitFor(t, "the completion note", func() bool {
		return notesContain(agent, fmt.Sprintf("job %d exited 1", id))
	})
	if queued := sessionNotes(agent); strings.Contains(queued[0], fmt.Sprintf("job %d exited 1:", id)) {
		t.Fatalf("a silent job quoted something: %q", queued[0])
	}
}

// An owed job note takes boundary-held ambient news with it, but the pair is one
// authored message rather than two synthetic user rows.
func TestJobNoteCoalescesWithAmbientNewsAtItsStepBoundary(t *testing.T) {
	agent, _ := jobsAgent(t)
	// Hold the session before its public opening so the test can inspect both
	// queues without racing the owed note's wake.
	agent.mu.Lock()
	agent.opened = false
	agent.mu.Unlock()

	agent.enqueueWatchNote("lint", "watch lint · 1 line new\nalso check the linter", false)
	id := startJob(t, agent, "echo done")
	waitFor(t, "the completion note", func() bool {
		return len(steeringQueue(agent)) == 1
	})

	if queued := ambientQueue(agent); len(queued) != 1 || !strings.Contains(queued[0], "also check the linter") {
		t.Fatalf("the ambient note is not held at the boundary: %v", queued)
	}
	if queued := steeringQueue(agent); len(queued) != 1 ||
		!strings.HasPrefix(queued[0], fmt.Sprintf("job %d exited 0", id)) {
		t.Fatalf("the owed job note is not on the step lane: %v", queued)
	}

	// One legal boundary takes both queues as one authored batch.
	agent.drainSteering(nil)
	var users []string
	for _, message := range agent.snapshot() {
		if message.Role == "user" {
			users = append(users, messageText(message))
		}
	}
	if len(users) != 1 || !strings.Contains(users[0], "while you worked:") ||
		!strings.Contains(users[0], "also check the linter") ||
		!strings.Contains(users[0], "lint watch: 1 update") ||
		!strings.Contains(users[0], fmt.Sprintf("job %d exited 0", id)) {
		t.Fatalf("the pair did not reach the transcript as one batch: %v", users)
	}
}

// ── list ────────────────────────────────────────────────────────────────────

// list is the status view: running while it runs, exited(N) after, with the
// command and an elapsed time on every row.
func TestJobsListShowsStatusTransitions(t *testing.T) {
	agent, _ := jobsAgent(t)

	sleeper := startJob(t, agent, "sleep 30")
	text, isError := runTool(t, agent, "jobs", `{"action":"list"}`)
	if isError {
		t.Fatalf("list failed: %s", text)
	}
	if !strings.Contains(text, fmt.Sprintf("job %d · running", sleeper)) {
		t.Fatalf("list does not show the sleeper running: %q", text)
	}
	if !strings.Contains(text, "sleep 30") {
		t.Fatalf("list does not show the command: %q", text)
	}

	quick := startJob(t, agent, "exit 7")
	waitExited(t, agent, quick)

	text, _ = runTool(t, agent, "jobs", `{"action":"list"}`)
	if !strings.Contains(text, fmt.Sprintf("job %d · exited(7)", quick)) {
		t.Fatalf("list does not show the exit code: %q", text)
	}
	if !strings.Contains(text, fmt.Sprintf("job %d · running", sleeper)) {
		t.Fatalf("the sleeper stopped being running: %q", text)
	}
	if lines := strings.Split(text, "\n"); len(lines) != 2 {
		t.Fatalf("list rendered %d rows for 2 jobs: %q", len(lines), text)
	}
}

func TestJobsUnknownAction(t *testing.T) {
	agent, _ := jobsAgent(t)

	text, isError := runTool(t, agent, "jobs", `{"action":"restart","id":1}`)
	if !isError || !strings.Contains(text, "Unknown action") {
		t.Fatalf("unknown action said %q", text)
	}
	text, isError = runTool(t, agent, "jobs", `{"action":"kill"}`)
	if !isError || !strings.Contains(text, "id is required") {
		t.Fatalf("kill without an id said %q", text)
	}
}

// ── close ───────────────────────────────────────────────────────────────────

// Close terminates what is still running, and does it before the session file
// is closed — a job outlives turns, not the session.
func TestCloseTerminatesRunningJobs(t *testing.T) {
	// A session file, because the ordering under test is jobs-then-file: the
	// kills must land while the journal is still open.
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.SessionFile = filepath.Join(t.TempDir(), "session.jsonl")
	})

	first := startJob(t, agent, "sleep 30")
	second := startJob(t, agent, "sleep 30")

	started := time.Now()
	if err := agent.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	elapsed := time.Since(started)

	for _, id := range []int{first, second} {
		target := agent.jobs.find(id)
		if target.running() {
			t.Fatalf("job %d survived Close", id)
		}
		if state := target.info().state; state != jobKilled {
			t.Fatalf("job %d is %v after Close, want jobKilled", id, state)
		}
	}
	// One shared grace, not one per job: two sleepers must not cost four
	// seconds. A SIGTERM'd `sleep` dies at once, so this is well under it.
	if elapsed > jobShutdownGrace+closeGrace {
		t.Fatalf("Close took %s for two sleepers", elapsed)
	}
	// And no kill of ours reported itself onto a queue nothing will drain.
	if queued := sessionNotes(agent); len(queued) != 0 {
		t.Fatalf("Close's kills self-reported: %v", queued)
	}
	// Close is idempotent, jobs and all.
	if err := agent.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

// ── the ring ────────────────────────────────────────────────────────────────

// The ring keeps the TAIL and stays bounded: a job that prints a lot must not
// be able to grow the session's memory without limit, and the lines it keeps
// must be the most recent ones.
func TestJobSinkRingIsBoundedToTheTail(t *testing.T) {
	sink := &jobSink{}
	for index := 0; index < 20000; index++ {
		if _, err := sink.Write([]byte(fmt.Sprintf("line%d\n", index))); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	sink.mu.Lock()
	size := len(sink.ring)
	sink.mu.Unlock()
	if size > jobRingBytes*2 {
		t.Fatalf("ring grew to %d bytes, cap is %d", size, jobRingBytes*2)
	}
	if last := sink.lastNonEmptyLine(); last != "line19999" {
		t.Fatalf("ring lost the tail: %q", last)
	}
	if got := sink.tail(2); got != "line19998\nline19999" {
		t.Fatalf("tail(2) = %q", got)
	}
}
