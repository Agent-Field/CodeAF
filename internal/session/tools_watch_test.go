package session

// Watch tests.
//
// Every timing assertion here rides a REAL two-second timer — the tool's own
// floor — because the thing under test is a loop whose whole subject is the
// passage of time, and a fake clock would test the fake. The cost is paid once:
// the slow cases run in parallel with each other, so the file's wall time is
// about one interval plus change rather than the sum of them, and every wait is
// a polled condition with a deadline rather than a fixed sleep.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ── harness ─────────────────────────────────────────────────────────────────

// startWatchTool calls the belt's watch tool and returns its one line.
func startWatchTool(t *testing.T, agent *Agent, arguments map[string]any) (string, bool) {
	t.Helper()
	encoded, err := json.Marshal(arguments)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return runTool(t, agent, "watch", string(encoded))
}

// feed writes the file a watch is pointed at. Writes are whole-file and atomic
// enough for a `cat`: a tick that read a half-written file would be a flake
// nobody could reproduce.
func feed(t *testing.T, workspace, name string, lines ...string) {
	t.Helper()
	path := filepath.Join(workspace, name)
	body := ""
	if len(lines) > 0 {
		body = strings.Join(lines, "\n") + "\n"
	}
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, []byte(body), 0o644); err != nil {
		t.Fatalf("write feed: %v", err)
	}
	if err := os.Rename(temporary, path); err != nil {
		t.Fatalf("rename feed: %v", err)
	}
}

// waitTicks waits until a watch has completed at least n runs of its command.
func waitTicks(t *testing.T, agent *Agent, id, n int) {
	t.Helper()
	waitFor(t, fmt.Sprintf("job %d to reach %d ticks", id, n), func() bool {
		target := agent.jobs.find(id)
		return target != nil && target.tickCount() >= n
	})
}

// watchID finds the id of the only watch in the registry.
func watchID(t *testing.T, agent *Agent) int {
	t.Helper()
	for _, candidate := range agent.jobs.all() {
		if candidate.kind == jobKindWatch {
			return candidate.id
		}
	}
	t.Fatal("no watch in the registry")
	return 0
}

// ── the wire ────────────────────────────────────────────────────────────────

// The tool is on the belt, its schema builds, and starting one returns the
// TERMS — one line, never the output.
func TestWatchStartLineIsOneLineOfTerms(t *testing.T) {
	agent, workspace := jobsAgent(t)
	feed(t, workspace, "app.log", "hello from the log")

	text, isError := startWatchTool(t, agent, map[string]any{"command": "cat app.log"})
	if isError {
		t.Fatalf("watch failed to start: %s", text)
	}
	if want := "watch cat-app.log started · every 10s · on change — kill with jobs"; text != want {
		t.Fatalf("start line is %q, want %q", text, want)
	}
	if strings.Contains(text, "hello from the log") {
		t.Fatalf("the start line carried the output: %q", text)
	}
	if _, err := toolDefinitions(agent.tools); err != nil {
		t.Fatalf("belt with watch does not build: %v", err)
	}
	if _, present := schemaProperties(t, beltTool(t, agent, "watch").Schema)["every_seconds"]; !present {
		t.Fatal("watch schema has no every_seconds")
	}

	// The interval is clamped, not refused, at both ends.
	text, _ = startWatchTool(t, agent, map[string]any{"command": "cat app.log", "every_seconds": 0, "name": "floor"})
	if !strings.Contains(text, fmt.Sprintf("every %ds", watchMinEvery)) {
		t.Fatalf("0 seconds was not clamped to the floor: %q", text)
	}
	text, _ = startWatchTool(t, agent, map[string]any{"command": "cat app.log", "every_seconds": 99999, "name": "ceiling"})
	if !strings.Contains(text, fmt.Sprintf("every %ds", watchMaxEvery)) {
		t.Fatalf("99999 seconds was not clamped to the ceiling: %q", text)
	}
}

func TestWatchRejectsBadArguments(t *testing.T) {
	agent, _ := jobsAgent(t)

	cases := []struct {
		name      string
		arguments map[string]any
		wanted    string
	}{
		{"no command", map[string]any{"every_seconds": 5}, "command is required"},
		{"unknown mode", map[string]any{"command": "date", "on": "sometimes"}, "on must be change, match, always, or quiet"},
		// quiet is a MODE, not a modifier: asking for its tick count while
		// asking for another mode is a misunderstanding, and it is said out
		// loud rather than ignored.
		{"quiet_ticks in another mode", map[string]any{"command": "date", "quiet_ticks": 4}, "quiet_ticks only applies when on is quiet (got on=change)"},
		{"quiet_ticks with always", map[string]any{"command": "date", "on": "always", "quiet_ticks": 4}, "quiet_ticks only applies when on is quiet (got on=always)"},
		{"match without a pattern", map[string]any{"command": "date", "on": "match"}, "pattern is required"},
		{"broken pattern", map[string]any{"command": "date", "on": "match", "pattern": "("}, "not a valid regular expression"},
		{"broken until", map[string]any{"command": "date", "until": "["}, "not a valid regular expression"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			text, isError := startWatchTool(t, agent, testCase.arguments)
			if !isError || !strings.Contains(text, testCase.wanted) {
				t.Fatalf("got %q, want an error containing %q", text, testCase.wanted)
			}
		})
	}
	// And nothing was started by any of them.
	if got := agent.jobs.list(); got != "No background jobs." {
		t.Fatalf("a rejected watch registered a job: %q", got)
	}
}

// ── change ──────────────────────────────────────────────────────────────────

// The heart of the tool, in one test because the three facts are one story: the
// first tick is a SILENT baseline, an unchanged tick says nothing at all, and a
// changed one delivers the NEW lines only — not the file.
func TestWatchChangeIsBaselineSilenceThenDeltaOnly(t *testing.T) {
	t.Parallel()
	agent, workspace := jobsAgent(t)
	feed(t, workspace, "app.log", "old one", "old two")

	text, isError := startWatchTool(t, agent, map[string]any{
		"command": "cat app.log", "every_seconds": watchMinEvery, "name": "app",
	})
	if isError {
		t.Fatalf("watch failed to start: %s", text)
	}
	id := watchID(t, agent)

	// Two ticks over an unchanged file: the first is the baseline, the second
	// has nothing to say. Neither may speak.
	waitTicks(t, agent, id, 2)
	if queued := sessionNotes(agent); len(queued) != 0 {
		t.Fatalf("an unchanged watch spoke: %v", queued)
	}

	feed(t, workspace, "app.log", "old one", "old two", "new three", "new four")
	waitFor(t, "the delta note", func() bool { return len(sessionNotes(agent)) > 0 })

	queued := sessionNotes(agent)
	if len(queued) != 1 {
		t.Fatalf("want exactly one note, got %v", queued)
	}
	note := queued[0]
	if !strings.HasPrefix(note, "2 lines new") {
		t.Fatalf("note header is wrong: %q", note)
	}
	// The boundary queue keeps the newest evidence only. Every line, including
	// new three, remains in this watch's `jobs output`.
	if !strings.Contains(note, "new four") {
		t.Fatalf("note is missing the newest line: %q", note)
	}
	if strings.Contains(note, "new three") {
		t.Fatalf("note repeated detail that belongs behind jobs output: %q", note)
	}
	if strings.Contains(note, "old one") || strings.Contains(note, "old two") {
		t.Fatalf("note repeated the lines the model already has: %q", note)
	}
}

// A tail that SCROLLS is the command this tool was built for: the previous
// window and the current one overlap, and only the lines past the overlap are
// news.
func TestWatchChangeReportsOnlyThePastTheOverlap(t *testing.T) {
	previous := []string{"line1", "line2", "line3"}
	current := []string{"line2", "line3", "line4"}
	if got := deltaLines(previous, current); len(got) != 1 || got[0] != "line4" {
		t.Fatalf("scrolled tail delta = %v, want [line4]", got)
	}
	if got := deltaLines(previous, []string{"line1", "line2", "line3", "line4"}); len(got) != 1 || got[0] != "line4" {
		t.Fatalf("appended delta = %v, want [line4]", got)
	}
	if got := deltaLines(previous, previous); len(got) != 0 {
		t.Fatalf("unchanged delta = %v, want nothing", got)
	}
	if got := deltaLines(previous, []string{"unrelated"}); len(got) != 1 {
		t.Fatalf("unrelated delta = %v, want the whole output", got)
	}
}

// A note is a sentence and forty lines, whatever the command printed.
func TestWatchNoteCapsAtFortyLines(t *testing.T) {
	var lines []string
	for index := 1; index <= 100; index++ {
		lines = append(lines, fmt.Sprintf("line%d", index))
	}
	note := watchNote("big", "100 lines new", lines)
	body := strings.Split(note, "\n")
	// One header, forty lines, one overflow row.
	if len(body) != watchNoteLines+2 {
		t.Fatalf("note has %d rows, want %d", len(body), watchNoteLines+2)
	}
	if body[len(body)-1] != fmt.Sprintf("… %d more", 100-watchNoteLines) {
		t.Fatalf("overflow row is %q", body[len(body)-1])
	}
	if body[1] != "line1" || body[watchNoteLines] != fmt.Sprintf("line%d", watchNoteLines) {
		t.Fatalf("the kept lines are not the first %d: %q … %q", watchNoteLines, body[1], body[watchNoteLines])
	}
}

// ── match ───────────────────────────────────────────────────────────────────

// on=match delivers the matching lines and NOTHING else: new lines that do not
// match are as quiet as no lines at all.
func TestWatchMatchDeliversOnlyMatchingLines(t *testing.T) {
	t.Parallel()
	agent, workspace := jobsAgent(t)
	feed(t, workspace, "app.log", "INFO starting")

	text, isError := startWatchTool(t, agent, map[string]any{
		"command": "cat app.log", "every_seconds": watchMinEvery,
		"on": "match", "pattern": "ERROR", "name": "errors",
	})
	if isError {
		t.Fatalf("watch failed to start: %s", text)
	}
	id := watchID(t, agent)
	waitTicks(t, agent, id, 1)

	// New lines, none of them matching: silence.
	feed(t, workspace, "app.log", "INFO starting", "INFO listening")
	waitTicks(t, agent, id, 3)
	if queued := sessionNotes(agent); len(queued) != 0 {
		t.Fatalf("a non-matching change spoke: %v", queued)
	}

	feed(t, workspace, "app.log", "INFO starting", "INFO listening", "ERROR disk full", "INFO retrying")
	waitFor(t, "the match note", func() bool { return len(sessionNotes(agent)) > 0 })

	note := sessionNotes(agent)[0]
	if !strings.HasPrefix(note, "1 line matching /ERROR/") {
		t.Fatalf("note header is wrong: %q", note)
	}
	if !strings.Contains(note, "ERROR disk full") {
		t.Fatalf("note is missing the matching line: %q", note)
	}
	if strings.Contains(note, "INFO") {
		t.Fatalf("note carried lines the pattern did not select: %q", note)
	}
}

// ── always ──────────────────────────────────────────────────────────────────

// on=always is the deliberate exception to baseline silence: the first tick
// reports, because "show me the number" means this number, now.
func TestWatchAlwaysReportsEveryTickIncludingTheFirst(t *testing.T) {
	t.Parallel()
	agent, _ := jobsAgent(t)

	text, isError := startWatchTool(t, agent, map[string]any{
		"command": "echo 42", "every_seconds": watchMinEvery, "on": "always", "name": "counter",
	})
	if isError {
		t.Fatalf("watch failed to start: %s", text)
	}
	waitFor(t, "the first tick's note", func() bool { return len(sessionNotes(agent)) > 0 })

	note := sessionNotes(agent)[0]
	if !strings.HasPrefix(note, "tick 1") || !strings.Contains(note, "42") {
		t.Fatalf("first always-note is wrong: %q", note)
	}
	// And it keeps going: the second tick reports the same value again, because
	// that is what always means.
	waitFor(t, "the second tick's note", func() bool { return len(sessionNotes(agent)) > 1 })
	if second := sessionNotes(agent)[1]; !strings.HasPrefix(second, "tick 2") {
		t.Fatalf("second always-note is wrong: %q", second)
	}
}

// ── until ───────────────────────────────────────────────────────────────────

// `until` ends the watch: one final note carrying the line that matched, the
// job settles as stopped, and nothing is said afterwards.
func TestWatchUntilDeliversFinalNoteAndStops(t *testing.T) {
	t.Parallel()
	agent, workspace := jobsAgent(t)
	feed(t, workspace, "build.log", "compiling")

	text, isError := startWatchTool(t, agent, map[string]any{
		"command": "cat build.log", "every_seconds": watchMinEvery,
		"until": "BUILD (OK|FAILED)", "name": "build",
	})
	if isError {
		t.Fatalf("watch failed to start: %s", text)
	}
	id := watchID(t, agent)
	waitTicks(t, agent, id, 1)

	feed(t, workspace, "build.log", "compiling", "BUILD OK in 4s")
	waitFor(t, "the until note", func() bool { return len(sessionNotes(agent)) > 0 })

	queued := sessionNotes(agent)
	if len(queued) != 1 {
		t.Fatalf("want exactly one note, got %v", queued)
	}
	if !strings.HasPrefix(queued[0], "until matched") {
		t.Fatalf("final note is wrong: %q", queued[0])
	}
	if !strings.Contains(queued[0], "BUILD OK in 4s") {
		t.Fatalf("final note does not carry the matching line: %q", queued[0])
	}

	waitFor(t, "the watch to stop", func() bool { return !agent.jobs.find(id).running() })
	if list := agent.jobs.list(); !strings.Contains(list, "stopped") {
		t.Fatalf("list does not show the stop: %q", list)
	}
	// A stopped watch ticks no more, and says no more.
	before := agent.jobs.find(id).tickCount()
	time.Sleep(time.Duration(watchMinEvery)*time.Second + 500*time.Millisecond)
	if after := agent.jobs.find(id).tickCount(); after != before {
		t.Fatalf("a stopped watch ticked again: %d → %d", before, after)
	}
	if queued := sessionNotes(agent); len(queued) != 1 {
		t.Fatalf("a stopped watch kept talking: %v", queued)
	}
	// Its slot went back: a watch that ended is not still holding one.
	if _, isError := startWatchTool(t, agent, map[string]any{"command": "date", "name": "after"}); isError {
		t.Fatal("the stopped watch did not release its slot")
	}
}

// ── the broken command ──────────────────────────────────────────────────────

// A watch spinning on a command that cannot run says so ONCE and stops.
func TestWatchStopsAfterThreeIdenticalFailures(t *testing.T) {
	t.Parallel()
	agent, _ := jobsAgent(t)

	text, isError := startWatchTool(t, agent, map[string]any{
		"command": "exit 7", "every_seconds": watchMinEvery, "name": "broken",
	})
	if isError {
		t.Fatalf("watch failed to start: %s", text)
	}
	id := watchID(t, agent)

	waitFor(t, "the failure note", func() bool { return len(sessionNotes(agent)) > 0 })
	queued := sessionNotes(agent)
	if len(queued) != 1 {
		t.Fatalf("a broken watch reported more than once: %v", queued)
	}
	note := queued[0]
	if !strings.HasPrefix(note, fmt.Sprintf("stopped: the command failed %d ticks in a row", watchFailLimit)) {
		t.Fatalf("failure note is wrong: %q", note)
	}
	if !strings.Contains(note, "exit status 7") {
		t.Fatalf("failure note does not name the error: %q", note)
	}
	waitFor(t, "the watch to stop", func() bool { return !agent.jobs.find(id).running() })
	if ticks := agent.jobs.find(id).tickCount(); ticks != watchFailLimit {
		t.Fatalf("the watch ran %d ticks before stopping, want %d", ticks, watchFailLimit)
	}
}

// ── the limit ───────────────────────────────────────────────────────────────

// Three at a time, and the fourth is an error that NAMES the limit and the way
// out. The same test reads the jobs list, because a watch that is a job has to
// look like one.
func TestWatchLimitIsThreeAndTheListShowsThem(t *testing.T) {
	agent, _ := jobsAgent(t)

	// An hour apart, so these three sit still while the test looks at them.
	for index := 1; index <= watchMaxConcurrent; index++ {
		text, isError := startWatchTool(t, agent, map[string]any{
			"command": "echo watching", "every_seconds": watchMaxEvery, "name": fmt.Sprintf("w%d", index),
		})
		if isError {
			t.Fatalf("watch %d failed to start: %s", index, text)
		}
	}

	text, isError := startWatchTool(t, agent, map[string]any{"command": "echo one too many"})
	if !isError {
		t.Fatalf("the fourth watch started: %q", text)
	}
	if !strings.Contains(text, fmt.Sprintf("%d watches", watchMaxConcurrent)) || !strings.Contains(text, "jobs kill") {
		t.Fatalf("the limit error does not name the limit and the way out: %q", text)
	}

	// The list is the jobs list: same rows, same ids, with the kind, the name,
	// the terms and the tick count on a watch's row.
	list, _ := runTool(t, agent, "jobs", `{"action":"list"}`)
	rows := strings.Split(list, "\n")
	if len(rows) != watchMaxConcurrent {
		t.Fatalf("list has %d rows for %d watches: %q", len(rows), watchMaxConcurrent, list)
	}
	if !strings.Contains(rows[0], "job 1 · watch w1 · running") {
		t.Fatalf("watch row is wrong: %q", rows[0])
	}
	if !strings.Contains(rows[0], fmt.Sprintf("every %ds · on change", watchMaxEvery)) {
		t.Fatalf("watch row does not carry the terms: %q", rows[0])
	}
	if !strings.Contains(rows[0], "echo watching") {
		t.Fatalf("watch row does not carry the command: %q", rows[0])
	}

	// Killing one gives the slot back, through the same tool that kills a
	// process — and the killed watch does not report its own death.
	text, isError = runTool(t, agent, "jobs", `{"action":"kill","id":1}`)
	if isError || !strings.Contains(text, "watch w1 (job 1) stopped") {
		t.Fatalf("killing a watch said %q", text)
	}
	waitFor(t, "the slot to come back", func() bool {
		_, failed := startWatchTool(t, agent, map[string]any{"command": "echo replacement", "name": "replacement"})
		return !failed
	})
	if queued := sessionNotes(agent); len(queued) != 0 {
		t.Fatalf("a killed watch reported itself: %v", queued)
	}
}

// A watch's ticks land in the job's log and its ring, so `jobs output` answers
// "what did it actually print" the way it does for any other job.
func TestWatchOutputReachesTheJobLog(t *testing.T) {
	agent, workspace := jobsAgent(t)
	feed(t, workspace, "app.log", "first line")

	if _, isError := startWatchTool(t, agent, map[string]any{
		"command": "cat app.log", "every_seconds": watchMaxEvery, "name": "logged",
	}); isError {
		t.Fatal("watch failed to start")
	}
	id := watchID(t, agent)
	waitTicks(t, agent, id, 1)

	text, isError := runTool(t, agent, "jobs", fmt.Sprintf(`{"action":"output","id":%d}`, id))
	if isError {
		t.Fatalf("jobs output failed: %s", text)
	}
	if !strings.Contains(text, "── tick 1 ──") || !strings.Contains(text, "first line") {
		t.Fatalf("the tick did not reach the job's output: %q", text)
	}
	logPath := filepath.Join(workspace, ".aforge-v3", "jobs", fmt.Sprintf("%d.log", id))
	body, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("the watch has no log file: %v", err)
	}
	if !strings.Contains(string(body), "first line") {
		t.Fatalf("the log file is missing the tick: %q", body)
	}
}

// Close ends a watch the way it ends a process, and says nothing about it.
func TestCloseStopsWatches(t *testing.T) {
	agent, _ := jobsAgent(t)

	if _, isError := startWatchTool(t, agent, map[string]any{
		"command": "echo alive", "every_seconds": watchMaxEvery, "name": "survivor",
	}); isError {
		t.Fatal("watch failed to start")
	}
	id := watchID(t, agent)

	started := time.Now()
	if err := agent.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if elapsed := time.Since(started); elapsed > jobShutdownGrace+closeGrace {
		t.Fatalf("Close took %s to stop one watch", elapsed)
	}
	if target := agent.jobs.find(id); target.running() {
		t.Fatal("the watch survived Close")
	}
	if queued := sessionNotes(agent); len(queued) != 0 {
		t.Fatalf("Close's stop self-reported: %v", queued)
	}
}

// ── quiet ───────────────────────────────────────────────────────────────────

// on=quiet FIRES ON THE ABSENCE OF NEWS, which is the shape none of the other
// three modes can make: a run of unchanged ticks ends the watch with one final
// note naming the span and the last thing the command said.
//
// The whole story is one test because the three facts are one behaviour: it
// stays silent while the output moves, it counts only consecutive unchanged
// ticks, and the note that ends it carries the evidence.
func TestWatchQuietFiresWhenTheOutputStopsMoving(t *testing.T) {
	t.Parallel()
	agent, workspace := jobsAgent(t)
	feed(t, workspace, "build.log", "compiling one")

	text, isError := startWatchTool(t, agent, map[string]any{
		"command": "cat build.log", "every_seconds": watchMinEvery,
		"on": "quiet", "quiet_ticks": watchMinQuietTicks, "name": "build",
	})
	if isError {
		t.Fatalf("watch failed to start: %s", text)
	}
	// The terms name the span, because the answer it will give is in them.
	if !strings.Contains(text, "on quiet") || !strings.Contains(text, "2 ticks") {
		t.Fatalf("the start line does not state the quiet terms: %q", text)
	}
	id := watchID(t, agent)

	// WHILE THE OUTPUT MOVES, NOTHING IS SAID. Two changes across three ticks
	// keep resetting the run, and a quiet watch that spoke here would be
	// announcing the opposite of what it was asked to watch for.
	waitTicks(t, agent, id, 1)
	feed(t, workspace, "build.log", "compiling one", "compiling two")
	waitTicks(t, agent, id, 2)
	feed(t, workspace, "build.log", "compiling one", "compiling two", "linking")
	waitTicks(t, agent, id, 3)
	if queued := sessionNotes(agent); len(queued) != 0 {
		t.Fatalf("a quiet watch spoke while the output was moving: %v", queued)
	}

	// Now leave it alone. Two unchanged ticks in a row are the terms, and the
	// note that follows is the last one this watch ever sends.
	waitFor(t, "the quiet note", func() bool { return len(sessionNotes(agent)) > 0 })
	queued := sessionNotes(agent)
	if len(queued) != 1 {
		t.Fatalf("want exactly one note, got %v", queued)
	}
	if !strings.HasPrefix(queued[0], "quiet for 2 ticks (4s)") {
		t.Fatalf("the final note is wrong: %q", queued[0])
	}
	if !strings.Contains(queued[0], "linking") {
		t.Fatalf("the final note does not carry the last output line: %q", queued[0])
	}

	waitFor(t, "the watch to stop", func() bool { return !agent.jobs.find(id).running() })
	// It ends the way `until` ends: no more ticks, and no more notes.
	before := agent.jobs.find(id).tickCount()
	time.Sleep(time.Duration(watchMinEvery)*time.Second + 500*time.Millisecond)
	if after := agent.jobs.find(id).tickCount(); after != before {
		t.Fatalf("a quiet watch ticked on after it fired: %d → %d", before, after)
	}
	if queued := sessionNotes(agent); len(queued) != 1 {
		t.Fatalf("a quiet watch kept talking: %v", queued)
	}
}

// The tick count is CLAMPED and never refused, exactly as the interval is: an
// out-of-range figure is a model reaching for "as soon as possible" or "only
// when it is really over", and both have a nearest legal answer.
func TestWatchQuietTicksAreClamped(t *testing.T) {
	cases := []struct {
		asked  int
		wanted int
	}{
		{0, watchMinQuietTicks},
		{-4, watchMinQuietTicks},
		{watchMaxQuietTicks + 500, watchMaxQuietTicks},
	}
	for _, testCase := range cases {
		arguments, err := json.Marshal(map[string]any{
			"command": "date", "on": "quiet", "quiet_ticks": testCase.asked,
		})
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		spec, problem := parseWatchArguments(arguments)
		if problem != "" {
			t.Fatalf("quiet_ticks %d was refused: %s", testCase.asked, problem)
		}
		if spec.quietTicks != testCase.wanted {
			t.Fatalf("quiet_ticks %d became %d, want %d", testCase.asked, spec.quietTicks, testCase.wanted)
		}
	}
	// And an absent figure is the default, not a zero.
	spec, problem := parseWatchArguments(json.RawMessage(`{"command":"date","on":"quiet"}`))
	if problem != "" || spec.quietTicks != watchDefaultQuietTicks {
		t.Fatalf("the default quiet run is %d (%s), want %d", spec.quietTicks, problem, watchDefaultQuietTicks)
	}
}
