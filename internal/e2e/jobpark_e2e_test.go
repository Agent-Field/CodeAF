//go:build e2e

// A TASK WORKER WAITS OUT THE COMMAND IT STARTED (#568).
//
// WHAT THIS LANE PROVES, AND WHAT IT COST NOT TO HAVE IT. A worker ran a suite
// in the foreground, the command crossed the session's background-after clock,
// and [session.BashPromotedLead] answered the call — `still running as job 3;
// log at …`. That is a true sentence and, before the fix, a terrible question:
// the loop asked the model what to do next while the thing the whole node was
// waiting on still had minutes to run. There is nothing to do next, so a real
// model manufactured something — `sleep 28; tail <log>` — the loop detector read
// the repetition exactly right, and the node killed a healthy run and ended
// saying `this turn is going in circles`.
//
// So the four things asserted below are the four halves of that story: the
// promotion happened, NOTHING was asked of the worker between the promotion and
// the ending, the ending arrived WHOLE, and the node came to rest having
// reported what the command printed.
//
// WHY IT IS AN END-TO-END LANE AND NOT A UNIT TEST. internal/session pins the
// park against a scripted model, which is the right place to pin the mechanism.
// The defect was never the mechanism: it was what a REAL model does when it is
// handed a running job and asked for a step. Only a real model can invent the
// polling this file exists to find the absence of.
//
// WHAT IT COSTS. One task, one worker, one minute of shell, on
// deepseek/deepseek-v4-flash with every text row pinned to it
// ([pinEveryTextModel]) — cents, and the figure is read back off the machine's
// own usage ledger ([ledgerSince]) rather than guessed.
//
//	go test -tags e2e -count=1 -timeout 20m -v -run TestATaskWaitsOutItsOwnLongCommand ./internal/e2e/
package e2e

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/approval"
	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/session"
)

const (
	// jobParkBackgroundAfter is the handoff clock this scenario runs on, in
	// seconds. It is deliberately far below the command's own length so the
	// promotion is the FIRST thing that happens to the call rather than a race:
	// the shape under test is what the worker does for the remaining minute, and
	// a clock that fired near the end would leave almost no window to look at.
	jobParkBackgroundAfter = 5
	// jobParkScriptSeconds is how long the stand-in suite runs. A minute is
	// comfortably past the clock above and is long enough that a model asked for
	// a next step has time to invent several — which is all the duration has to
	// prove. The defect's own suite ran eight minutes; paying for eight here
	// would measure patience rather than behaviour.
	jobParkScriptSeconds = 60
	// jobParkLastLine is the distinctive sentence the script signs off with. It
	// is the needle for the whole chain: it has to survive the job's own tail,
	// the ending note, and the worker's report.
	jobParkLastLine = "SUITE PASSED"
	// jobParkWall is how long the whole scenario may take from the moment the
	// task is admitted. A minute of shell plus a handful of cheap turns is two
	// or three; twelve is the far edge of that and still an answer rather than a
	// hang.
	jobParkWall = 12 * time.Minute
	// jobParkCap is what this scenario may spend before the run itself is the
	// finding. One worker doing one command measures in cents.
	jobParkCap = 0.50
)

// jobParkAsk is the work, in a person's own words. Three things are said flatly
// because a model that reads them otherwise is not testing the park: run it in
// the FOREGROUND (a background:true call is a job from the first instant and is
// never promoted at all — promote.go), run it ONCE, and quote the last line back
// (the ending is the only place that line can have come from).
//
// IT DOES NOT SAY "DO NOT POLL". That would be the test asking the model for the
// behaviour the test claims to measure. The whole finding is that a worker with
// the ending in front of it has nothing left to poll FOR.
var jobParkAsk = fmt.Sprintf(`This repository holds one script, suite.sh, and it takes about a minute to run.

Run it once with bash, in the foreground: ./suite.sh — and let it finish.
Do not start it in the background and do not run it a second time.

It does not change any file and neither should you: this errand is only to run it
and tell me how it went. When it is over, quote its LAST line back to me in your
report, exactly as it printed it.`)

// ── the person's repository ─────────────────────────────────────────────────

// newSuiteGround is the person's checkout with the long command in it: one
// commit, one script, an identity of their own — [newRepositoryGround]'s shape,
// with material this scenario can wait on.
func newSuiteGround(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("make the person's repository: %v", err)
	}
	gitAt(t, dir, "init", "--quiet")
	gitAt(t, dir, "config", "user.name", "the person")
	gitAt(t, dir, "config", "user.email", "person@localhost")
	gitAt(t, dir, "config", "commit.gpgsign", "false")
	// THE SCRIPT IS BUILT FROM THE CONSTANTS AND NEVER SPELLED TWICE. Its length
	// and its closing line are what the assertions below wait for, so a figure
	// written into the shell as well as into Go would be a second place either
	// could drift.
	script := fmt.Sprintf(`#!/bin/sh
# A stand-in for the long suite a worker waits out. It prints a line a second so
# that the promotion has something to hand back, and signs off with the one
# sentence the ending, the note and the report all have to carry.
step=1
while [ "$step" -le %d ]; do
	echo "checking case $step of %d"
	sleep 1
	step=$((step + 1))
done
echo "%s"
`, jobParkScriptSeconds, jobParkScriptSeconds, jobParkLastLine)
	if err := os.WriteFile(filepath.Join(dir, "suite.sh"), []byte(script), 0o755); err != nil {
		t.Fatalf("seed the person's repository: %v", err)
	}
	gitAt(t, dir, "add", "suite.sh")
	gitAt(t, dir, "commit", "--quiet", "-m", "the suite")
	return dir
}

// jobParkConfig is what the v3 door wires for work, with this scenario's one
// row moved: everything here is read the way cmd/aforge reads it
// (chatv3.go's applyV3Governance).
func jobParkConfig(w *world) func(*session.Config) {
	profile := w.settings.ProfileDir
	return func(cfg *session.Config) {
		cfg.TaskModel = config.TaskModelAt(profile)
		cfg.TaskParallel = config.TaskParallelAt(profile)
		cfg.TaskSettle = config.TaskSettleAt(profile)
		cfg.ModelFallbacks = config.ParseModelFallbacks(config.ModelFallbacksAt(profile))
		// THE CLOCK THIS SCENARIO IS ABOUT, read off the person's own row rather
		// than written in here, so the value the engine runs on is the value a
		// person setting `bash.background_after_seconds` would get.
		cfg.BashBackgroundAfterSeconds = config.BashBackgroundAfterAt(profile)
		// NOBODY IS AT A KEYBOARD, so a question a node asks would wait for a card
		// nobody can press until its turn's context dies (task.go's askTask).
		cfg.AskConsent = false
		cfg.TaskAutoApproveSeconds = 0
		// THE CHECK AND THE REPAIR ROUNDS ARE OFF, and it is a scope ruling: what
		// this lane measures is what one worker does between a promotion and an
		// ending, and a cheap checker's opinion between the worker and rest would
		// put turns in the transcript that are about something else.
		cfg.TaskAudit = false
		cfg.TaskRepairRounds = 0
		// AND THE DIVISION ROAD STAYS OFF. Nil here is no divide_work on the
		// belt, which is what makes the root node the worker — the one node whose
		// transcript this file reads.
		cfg.Divide = false
		// THE WORKER MAY RUN THINGS. A node's own consent question reaches no
		// drain loop out here, and running the script is the whole errand.
		policy, err := approval.Load(map[string]any{"default": "allow"})
		if err != nil {
			panic("approval.Load: " + err.Error())
		}
		cfg.ApprovalPolicy = &policy
	}
}

// ── the scenario ────────────────────────────────────────────────────────────

func TestATaskWaitsOutItsOwnLongCommand(t *testing.T) {
	w := newWorld(t)
	pinEveryTextModel(t)
	setBackgroundAfter(t, jobParkBackgroundAfter)

	ground := newSuiteGround(t)
	desk := filepath.Join(t.TempDir(), "desk")
	if err := os.MkdirAll(desk, 0o755); err != nil {
		t.Fatalf("make the conversation's own folder: %v", err)
	}
	place := w.place(w.projectBucket(desk), desk)
	agent := w.openAt(desk, place, jobParkConfig(w))
	// THE GROUND IS SAID RATHER THAN GUESSED, through the door a surface calls
	// when a person names a folder ([session.Agent.ReferPlace]), so the work is
	// about the person's repository rather than about the directory this
	// conversation stands in.
	if _, err := agent.ReferPlace(ground, session.PlaceSaid); err != nil {
		t.Fatalf("refer %s: %v", ground, err)
	}

	started := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), jobParkWall)
	defer cancel()

	// THE TASK IS STARTED AT THE PERSON'S OWN DOOR ([session.Agent.StartTask],
	// which is what `/task` calls) rather than through a chat turn that hopes the
	// model reaches for propose_task: the subject here is what happens AFTER a
	// task starts, and the typed door takes one model's whim out of getting there.
	id, title, _, err := agent.StartTask(ctx, jobParkAsk)
	if err != nil {
		t.Fatalf("start the task: %v", err)
	}
	t.Logf("TASK %d started: %q  (ground %s, background after %ds)", id, title, ground, jobParkBackgroundAfter)

	settled := waitForTaskRest(ctx, t, place)
	wall := time.Since(started)
	rows := readTaskRows(t, place.Tasks())
	usd, models := ledgerSince(t, started)
	t.Logf("SCENARIO wall %s · cost $%.6f · models %s · nodes %d",
		wall.Round(time.Second), usd, strings.Join(models, ","), len(rows))
	for _, row := range rows {
		t.Logf("  #%d parent=%d state=%s ending=%q cost=$%.4f report=%q",
			row.ID, row.Parent, row.State, row.Ending, row.CostUSD, shorten(row.Report, 400))
	}
	if !settled {
		t.Fatalf("the task did not come to rest inside %s", jobParkWall)
	}
	if usd > jobParkCap {
		t.Errorf("this scenario spent $%.4f, past the $%.2f one worker and one command cost on %s",
			usd, jobParkCap, e2eModel)
	}
	for _, model := range models {
		if model != e2eModel {
			t.Errorf("a call rode %q; every model row in this run is pinned at %q", model, e2eModel)
		}
	}

	journal := agent.TaskJournal(id)
	if strings.TrimSpace(journal) == "" {
		t.Fatalf("node %d wrote no transcript, so there is nothing to read the run out of", id)
	}
	record := session.ReadTranscript(journal)
	if record.Unreadable != "" {
		t.Fatalf("node %d's transcript at %s: %s", id, journal, record.Unreadable)
	}
	entries := record.Entries
	t.Logf("the worker's transcript is %s (%d entries)", journal, len(entries))
	for at, entry := range entries {
		if entry.Role == "tool" {
			t.Logf("  [%d] %s %s\n      → %s", at, entry.Tool, shorten(entry.Args, 300), shorten(entry.Output, 400))
			continue
		}
		t.Logf("  [%d] %s: %s", at, entry.Role, shorten(entry.Text, 400))
	}

	// ── 1. THE COMMAND WAS PROMOTED ────────────────────────────────────────
	//
	// Without this the rest of the file is asserting about a run that never got
	// into the bind: a command that finished inside the clock, or one the model
	// started in the background, is not the scenario.
	promotedAt, jobID, logPath := findPromotion(t, entries)
	t.Logf("PROMOTED at entry %d → job %d, log at %s", promotedAt, jobID, logPath)

	// ── 3. THE ENDING ARRIVED WHOLE ────────────────────────────────────────
	//
	// Located before the polling window is read, because the window is the span
	// BETWEEN the two and there is no window without both ends.
	endingAt := findJobEnding(t, entries, promotedAt, jobID)
	ending := entries[endingAt].Text
	for _, want := range []string{
		jobEndingLead,
		fmt.Sprintf("job %d exited 0", jobID),
		jobParkLastLine,
		"full log:",
	} {
		if !strings.Contains(ending, want) {
			t.Errorf("the ending the worker read is missing %q; it said:\n%s", want, ending)
		}
	}
	t.Logf("THE ENDING at entry %d (%s):\n%s", endingAt, entries[endingAt].Role, ending)

	// ── 2. NOTHING WAS ASKED OF THE WORKER IN BETWEEN ──────────────────────
	//
	// THIS IS THE ASSERTION THAT WAS FALSE. The worker used to be asked for its
	// next step the instant the promotion answered, and what a real model
	// answered with was `sleep N; tail <log>` — again and again, until the loop
	// detector stopped the node. A parked worker makes no call at all, so every
	// call in this window is a step somebody was asked for while the command it
	// was waiting on still ran.
	var window, polling []string
	for at := promotedAt + 1; at < endingAt; at++ {
		entry := entries[at]
		if entry.Role != "tool" {
			continue
		}
		// A ROW WITH NO NAME ON IT IS NOT A STEP ANYBODY TOOK. The shaping makes
		// one row per tool call the assistant message NAMED, and this provider
		// sometimes leaves a nameless, argument-less, unanswered one riding the
		// same message as the real call — so it sits inside this window by
		// accident of batching rather than by anything the worker did. It could
		// never be a poll (there is nothing in it to poll with), and listing it
		// would put a call in the log that a reader would go looking for.
		if strings.TrimSpace(entry.Tool) == "" && strings.TrimSpace(entry.Args) == "" {
			continue
		}
		one := fmt.Sprintf("[%d] %s %s", at, entry.Tool, shorten(entry.Args, 200))
		window = append(window, one)
		if isJobPolling(entry.Tool, entry.Args, logPath) {
			polling = append(polling, one)
		}
	}
	if len(window) > 0 {
		t.Logf("calls made while the command still ran: %v", window)
	}
	if len(polling) > 0 {
		t.Errorf("the worker polled its own running command %d time(s) between the promotion and the ending — the park did not hold:\n    %s",
			len(polling), strings.Join(polling, "\n    "))
	}

	// ── 4. AND THE NODE CAME TO REST HAVING REPORTED WHAT IT SAW ───────────
	//
	// The circling sentence is looked for in the WHOLE journal and not only in
	// the row: the loop's own nudge lands in the transcript before it ever
	// becomes an ending, so a run that was nudged and recovered is still a run
	// this fix was supposed to make impossible.
	whole, err := os.ReadFile(journal)
	if err != nil {
		t.Fatalf("read %s: %v", journal, err)
	}
	if strings.Contains(string(whole), loopCirclingSentence) {
		t.Errorf("%q appears in node %d's transcript: waiting was read as repetition", loopCirclingSentence, id)
	}
	root, found := taskRowFor(rows, id)
	if !found {
		t.Fatalf("the checkpoint holds no node %d", id)
	}
	if root.Ending == string(session.TaskEndingCircling) {
		t.Errorf("node %d ended %q", id, root.Ending)
	}
	if root.State != string(session.TaskDone) {
		t.Errorf("node %d settled %q, not %q; it said: %s", id, root.State, session.TaskDone, root.Report)
	}
	// THE ONE PLACE THIS FILE READS THE MODEL'S PROSE, deliberately: the last
	// line exists nowhere the worker could have got it except the ending, so a
	// closing word carrying it is the end-to-end proof that the ending was not
	// merely queued but READ. The brief asks for it in as many words.
	//
	// IT IS THE WORKER'S OWN LAST WORD AND NOT THE ROW'S `report`. The two are
	// not the same artifact: the report is composed on the way out and clipped,
	// and a run of this scenario was watched to quote the line perfectly in the
	// transcript and land a report cut off at the opening of the fence it had
	// put around it. That clipping is the reporting path's business; what this
	// lane is about is whether the ending reached the model at all, and the
	// transcript is where that is true or false.
	closing := closingWords(entries, endingAt)
	if !strings.Contains(closing, jobParkLastLine) {
		t.Errorf("node %d never quotes %q after reading the ending, so nothing here says the ending reached the model; its last word was:\n%s",
			id, jobParkLastLine, closing)
	}
}

// closingWords is what the worker said after the ending landed in front of it:
// its last assistant message, which is the turn the ending was the answer to.
func closingWords(entries []session.DisplayEntry, after int) string {
	var last string
	for at := after + 1; at < len(entries); at++ {
		if entries[at].Role == "assistant" && strings.TrimSpace(entries[at].Text) != "" {
			last = entries[at].Text
		}
	}
	return last
}

// ── the readers ─────────────────────────────────────────────────────────────

const (
	// jobEndingLead is how a session note reaches a model that was busy while it
	// arrived (agent.go's [batchSessionNotes]). It is spelled here rather than
	// exported because a test that read it off the engine's own variable would
	// pass whatever the engine started saying instead.
	jobEndingLead = "while you worked:"
	// loopCirclingSentence is the front of the loop guard's own ending
	// (looped.go's loopLeftUndoneNote), which is what #568 ended with. Only the
	// front, because the tail of that sentence is about what was left undone and
	// is not the fact being looked for.
	loopCirclingSentence = "this turn is going in circles"
)

// setBackgroundAfter moves the handoff clock through the person's own settings
// row, so that everything downstream — the config read in [jobParkConfig], the
// clock [bashPromotion.Started] arms, the sentence the bash tool's description
// carries — is reading one value from one place.
func setBackgroundAfter(t *testing.T, seconds int) {
	t.Helper()
	registry := config.NewSettings(config.SettingsOptions{})
	row, found := registry.Row(config.KeyBashBackgroundAfter)
	if !found {
		t.Fatalf("the settings registry has no row %q", config.KeyBashBackgroundAfter)
	}
	if err := row.Apply(strconv.Itoa(seconds)); err != nil {
		t.Fatalf("write %s=%d: %v", config.KeyBashBackgroundAfter, seconds, err)
	}
}

// waitForTaskRest polls the graph's own checkpoint until every node in it has
// settled. It reads THE FILE rather than the live graph, for [familyRun.waitForRest]'s
// reason: the file is what a surface, a resumed process and this test all have.
func waitForTaskRest(ctx context.Context, t *testing.T, place session.Place) bool {
	t.Helper()
	for {
		if rows := readTaskRows(t, place.Tasks()); len(rows) > 0 && allSettled(rows) {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(2 * time.Second):
		}
	}
}

// taskRowFor is one row of the checkpoint by id.
func taskRowFor(rows []taskRow, id uint64) (taskRow, bool) {
	for _, row := range rows {
		if row.ID == id {
			return row, true
		}
	}
	return taskRow{}, false
}

// findPromotion is the FIRST call in this transcript that came back as a job,
// with the job's id and log path read out of the sentence it answered with.
//
// The first and not the last: everything asserted after it is about the window
// that opens at the promotion, and a run that promoted twice has already broken
// the "run it once" the brief asked for — which the failure below would rather
// name than quietly measure the second half of.
func findPromotion(t *testing.T, entries []session.DisplayEntry) (int, int, string) {
	t.Helper()
	for at, entry := range entries {
		if entry.Role != "tool" || !strings.Contains(entry.Output, session.BashPromotedLead) {
			continue
		}
		id, logPath, ok := promotedJob(entry.Output)
		if !ok {
			t.Fatalf("entry %d answered with a promotion this test cannot read:\n%s", at, entry.Output)
		}
		return at, id, logPath
	}
	t.Fatalf("no call in the worker's transcript was promoted (%q never appears), so the command never crossed the %ds clock",
		session.BashPromotedLead, jobParkBackgroundAfter)
	return 0, 0, ""
}

// promotedJob reads the id and the log path out of `still running as job 3; log
// at /path/3.log`, which is [promotedSentence]'s first line and always its first
// line — the tail of the command's own output follows it.
func promotedJob(output string) (int, string, bool) {
	at := strings.Index(output, session.BashPromotedLead)
	if at < 0 {
		return 0, "", false
	}
	line := output[at+len(session.BashPromotedLead):]
	if cut := strings.IndexAny(line, "\r\n"); cut >= 0 {
		line = line[:cut]
	}
	number, path, found := strings.Cut(line, "; log at ")
	if !found {
		return 0, "", false
	}
	id, err := strconv.Atoi(strings.TrimSpace(number))
	if err != nil {
		return 0, "", false
	}
	return id, strings.TrimSpace(path), true
}

// findJobEnding is the message that put this job's ending in front of the
// worker: the first thing after the promotion that is not a tool result and
// carries the lead a session note travels under.
func findJobEnding(t *testing.T, entries []session.DisplayEntry, after, jobID int) int {
	t.Helper()
	for at := after + 1; at < len(entries); at++ {
		if entries[at].Role == "tool" {
			continue
		}
		if strings.Contains(entries[at].Text, jobEndingLead) {
			return at
		}
	}
	t.Fatalf("job %d's ending never reached the worker: nothing after entry %d carries %q",
		jobID, after, jobEndingLead)
	return 0
}

// isJobPolling answers whether one call is the worker going to look at the
// command it is already waiting for. The three shapes are the three #568
// recorded: the `jobs` tool, a shell command naming the job's own log, and the
// `sleep`/`tail` pair a model reaches for when it has been asked for a step it
// has no answer to.
func isJobPolling(tool, args, logPath string) bool {
	if tool == "jobs" {
		return true
	}
	if logPath != "" && strings.Contains(args, logPath) {
		return true
	}
	lower := strings.ToLower(args)
	for _, word := range []string{"sleep", "tail ", "tail -"} {
		if strings.Contains(lower, word) {
			return true
		}
	}
	return false
}
