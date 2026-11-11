package session

// ── A TASK'S WORK, AS FACTS ────────────────────────────────────────────────
//
// A task's page used to be handed its work as [AppTaskView.WorkLog]: a list of
// already-worded lines ("· bash go test ./…", "✗ bash: exit status 1"). Every
// fact a reader needs about a step — which tool ran, what it was asked, how long
// it took, whether it failed and with what code — was spent composing that
// sentence and then unrecoverable, so the one surface that groups and folds
// steps by kind received prose and drew a dim notice per line. A real task that
// ran sixty-two tests over seven steps arrived as seven notices and a page that
// said nothing about any of them (cmd/codeaf's appwork.go).
//
// So the engine answers with the facts and nothing else. This file is the one
// place those facts are gathered, from the three records that genuinely hold
// them, and NOTHING HERE INVENTS ONE. A duration is present only where a clock
// was recorded; an exit code only where a command reported one; a tool kind only
// where the record names the tool. A reader distinguishes "unknown" from "zero"
// because an absent fact is absent here rather than defaulted.
//
// THE THREE RECORDS, and what each of them knows:
//
//   - the LIVE ring ([taskLive]), written from the child's own event stream, is
//     the only record of a step while it is still running. It knows the tool, the
//     arguments, the gloss, the clock, the exit code the result reported, and
//     whether a door refused the call.
//   - the node JOURNAL, the durable session file a restored node still has, knows
//     everything the live ring did except who refused what: the call's id, its
//     tool, its arguments, its result and — since the `took` line — its duration.
//   - a handed-over run's TRAJECTORY (internal/run's Step), which knows the step
//     number, the tool, the command as the model spelled it, the head of what came
//     back, the recorded exit code, and whether the step ran at all.
//
// A LINE WITH NO TOOL PAYLOAD STAYS A LINE. The oldest journals and the oldest
// trajectories recorded a command and nothing else, and a step read out of one is
// marked [AppTaskStep.Legacy] and carries its words in Text. It is shown as the
// line it is. Classifying it would mean guessing a kind from a string, which is
// how a page comes to assert a fact its record never held.

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// appTaskStepsMax bounds every projection in this file. It is [taskLiveRing]
	// said again on purpose: "the steps of one task" is one idea and must not
	// have two budgets, and a page that asked for more would be asking the live
	// ring for lines it already dropped.
	appTaskStepsMax = taskLiveRing
	// appStepTailMax is how many lines of one step's output a step carries. The
	// whole output stays where it was recorded; this is the tail a row shows.
	appStepTailMax = 12
	// appStepTextMax clips ONE recorded string — a command, a gloss, a legacy
	// line. A step whose command is a minified payload must not put a screenful
	// into a page on the strength of being one step.
	appStepTextMax = 2000
	// appStepFilesKept bounds how many fingerprinted records this process keeps
	// a parsed projection for. A task page polls, and re-reading a long journal
	// on every poll is the cost this cache exists to avoid.
	appStepFilesKept = 64
)

// AppStepSource is WHICH RECORD a step was read out of. It is carried rather
// than flattened away because the three records do not know the same things: a
// surface that is told a duration is absent can say nothing about it, but a
// surface that is told WHY it is absent can say whether asking again would help.
type AppStepSource string

const (
	// AppStepLive is the running node's own event stream.
	AppStepLive AppStepSource = "live"
	// AppStepJournal is the node's durable session file.
	AppStepJournal AppStepSource = "journal"
	// AppStepTrajectory is a handed-over run's recorded trajectory.
	AppStepTrajectory AppStepSource = "trajectory"
)

// AppTaskStep is ONE step of a task's work as the record holds it.
//
// Every optional field is a pointer or a zero that means UNKNOWN, never a
// default that means a figure. The engine's own vocabulary is used throughout —
// the registered tool name, the action family from actioncategory.go — and no
// surface's labels appear here: which icon or which group word a kind draws is
// the surface's business, and an engine that spelled them would own a decision it
// cannot see the consequences of.
type AppTaskStep struct {
	// ID is this step's identity, stable across reads of the same record so a
	// page redrawn mid-poll does not grow a second copy of a row it already has.
	// It is the provider's call id where the record kept one, and the record's
	// own address for the step otherwise ("journal:4", "trajectory:7").
	ID string
	// Call is the provider's id for the call, and "" where the record never had
	// one. A live end pairs on it.
	Call string
	// Tool is the engine's REGISTERED tool name ("bash", "read", "edit"), and ""
	// where the record does not name one. A surface reads the kind off this
	// through the engine's own family and never off the words of a gloss.
	Tool string
	// Category is the family of work the engine sorts this tool into
	// ([ActionCategoryForTool]). It is [ActionWork] — the zero — for a tool the
	// engine has no family for and for every step whose tool is unknown.
	Category ActionCategory
	// Command is what the step was ASKED to do, as it was spelled: a shell
	// command as the model wrote it, a path, a query. Target is the call's gloss
	// as the tool cluster rendered it, which is what a row says when there is no
	// command to say instead.
	Command string
	Target  string
	// Args is the call's arguments exactly as the record holds them — one line,
	// capped, parseable whenever the wire arguments were ([Event.Args]). It is
	// what lets a reader name the files an edit touched without this file
	// guessing at them. "" where the record kept none.
	Args string
	// Detail is the head of what came back, for display only and never the
	// result the model read.
	Detail string
	// Tail is the last recorded lines of the step's output, oldest first, and
	// Lines how many it wrote in all where that was counted. Lines is nil when
	// nothing counted them, which is not the same as a step that printed nothing.
	Tail  []string
	Lines *int
	// StartedAt and EndedAt are the step's own clock, and they are ZERO WHERE NO
	// CLOCK WAS RECORDED. Took is the duration the record kept, which is not
	// always EndedAt minus StartedAt: a journal keeps the duration of the call's
	// own work without keeping either instant. Zero means unknown.
	StartedAt time.Time
	EndedAt   time.Time
	Took      time.Duration
	// ExitCode is the code the command itself reported, nil where none was. A
	// nil here is NOT a zero: a step that was never run and a step that exited
	// cleanly are different facts and a page speaks about them differently.
	ExitCode *int
	// OK is whether the step succeeded, and nil while it is still running or
	// where the record never settled it.
	OK *bool
	// Running is a step this process can still see in flight. It is false for
	// every step read out of a durable record, including one a killed session
	// left unfinished — that step is unsettled, not running.
	Running bool
	// Batch is the same on every step the model asked for together, so a surface
	// can draw parallel calls as one row. "" where the record kept none.
	Batch string
	// Service and ServiceName are the account a connector step acted through:
	// its id, and the word a person reads.
	Service, ServiceName string
	// Refused says a DOOR said no to something the step ATTEMPTED, and
	// HarnessMade that the harness wrote the answer rather than the world the
	// model reached for ([Event.Refused], [Event.HarnessMade]). They are the
	// fields and not the words of a failure: a reader must not sort a refusal by
	// reading the sentence it was refused with.
	Refused, HarnessMade bool
	// Step is the step's number within its task where the record counted one
	// (a trajectory does), and 0 where none did.
	Step int
	// Source is which record this came out of.
	Source AppStepSource
	// Legacy marks a step the record holds with NO TOOL PAYLOAD: a command or a
	// line and nothing else. Text is then its words. A legacy step is shown as
	// the line it is and is never classified, because its record cannot support a
	// claim about which hands were used.
	Legacy bool
	Text   string
}

// Settled is whether the record says how this step came out. An unsettled step
// is one still running, or one a session's death left with no result.
func (s AppTaskStep) Settled() bool { return s.OK != nil }

// Failed is whether the record says this step did not succeed. It reads the
// recorded outcome and never the words of a result.
func (s AppTaskStep) Failed() bool { return s.OK != nil && !*s.OK }

// appStepText clips one recorded string to [appStepTextMax] and trims it.
func appStepText(text string) string { return clip(strings.TrimSpace(text), appStepTextMax) }

// appStepTail keeps at most [appStepTailMax] lines, newest kept.
func appStepTail(lines []string) []string {
	if len(lines) == 0 {
		return nil
	}
	if len(lines) > appStepTailMax {
		lines = lines[len(lines)-appStepTailMax:]
	}
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		out = append(out, clip(line, taskLiveLineLimit))
	}
	return out
}

// appStepsFrom is the tail of a projection, bounded, so every reader of this
// file is bounded by construction rather than by remembering to be.
func appStepsFrom(steps []AppTaskStep, limit int) []AppTaskStep {
	if limit <= 0 || limit > appTaskStepsMax {
		limit = appTaskStepsMax
	}
	if len(steps) > limit {
		steps = steps[len(steps)-limit:]
	}
	return steps
}

// ── the live ring ──────────────────────────────────────────────────────────

// liveStep is one step as the recorder holds it while the node runs. It is the
// typed record beside the ring of worded lines, written from the same events: a
// surface that groups by kind needs the facts, and a model asking "what is it
// doing" needs the sentence, and composing one from the other in either
// direction loses something.
type liveStep struct {
	step  AppTaskStep
	began time.Time
}

// recordStep folds one tool lifecycle event into the typed ring. It is called
// from [taskLive.record] with l.mu held, so it never takes a lock of its own.
func (l *taskLive) recordStep(event Event, at time.Time) {
	switch event.Kind {
	case EventToolBegin:
		l.openStep(event, at)
	case EventToolFinished:
		// The call's own work is over and the engine knows how long it took,
		// which it says BEFORE the batch around the call ends.
		if s := l.findStep(event.CallID, event.Tool); s != nil && event.Took > 0 {
			s.step.Took = event.Took
		}
	case EventToolEnd, EventToolFailed:
		l.closeStep(event, at)
	}
}

// openStep starts a step's typed record. A call whose begin was never seen —
// an event stream joined late — is opened by its end instead (see closeStep),
// so a step is recorded once however the recorder came by it.
func (l *taskLive) openStep(event Event, at time.Time) {
	if l.stepAt == nil {
		l.stepAt = map[string]int{}
	}
	step := AppTaskStep{
		ID:          appLiveStepID(event, len(l.calls)),
		Call:        event.CallID,
		Tool:        strings.TrimSpace(event.Tool),
		Category:    ActionCategoryForTool(event.Tool),
		Target:      appStepText(event.Hint),
		Args:        event.Args,
		Running:     true,
		Batch:       event.Batch,
		Service:     event.Service,
		ServiceName: event.ServiceName,
		StartedAt:   at,
		Source:      AppStepLive,
	}
	step.Command = appStepCommand(event.Tool, event.Args, event.Hint)
	l.calls = append(l.calls, liveStep{step: step, began: at})
	if event.CallID != "" {
		l.stepAt[event.CallID] = len(l.calls) - 1
	}
	l.trimSteps()
}

// findStep is the open record for one call: by its provider id where it has
// one, and otherwise the newest unfinished step of the same tool, which is the
// best a stream with no ids can pair on. Callers hold l.mu.
func (l *taskLive) findStep(call, tool string) *liveStep {
	if call != "" {
		if at, known := l.stepAt[call]; known && at >= 0 && at < len(l.calls) {
			return &l.calls[at]
		}
	}
	tool = strings.TrimSpace(tool)
	for i := len(l.calls) - 1; i >= 0; i-- {
		if l.calls[i].step.Running && (tool == "" || l.calls[i].step.Tool == tool) {
			return &l.calls[i]
		}
	}
	return nil
}

// closeStep settles a step with what its end reported.
func (l *taskLive) closeStep(event Event, at time.Time) {
	s := l.findStep(event.CallID, event.Tool)
	if s == nil {
		// An end with no begin: the step is still a step, and the only thing
		// unknown about it is when it started, which stays zero.
		l.openStep(event, time.Time{})
		s = l.findStep(event.CallID, event.Tool)
		if s == nil {
			return
		}
		s.step.StartedAt = time.Time{}
		s.began = time.Time{}
	}
	ok := event.Kind == EventToolEnd
	s.step.OK = &ok
	s.step.Running = false
	s.step.EndedAt = at
	if s.step.Took == 0 && !s.began.IsZero() {
		s.step.Took = at.Sub(s.began)
	}
	if event.Exit != nil {
		code := *event.Exit
		s.step.ExitCode = &code
	}
	s.step.Refused, s.step.HarnessMade = event.Refused, event.HarnessMade
	if detail := appStepText(event.Output); detail != "" {
		s.step.Detail = detail
		s.step.Tail = appStepTail(strings.Split(strings.TrimRight(detail, "\n"), "\n"))
	}
	if hint := appStepText(event.Hint); hint != "" && event.Kind == EventToolFailed {
		// A FAILURE'S HINT IS THE FAILURE, not a gloss of the call (loop.go puts
		// the result's first line there), so it is kept as the detail rather than
		// written over the target the begin recorded.
		if s.step.Detail == "" {
			s.step.Detail = hint
		}
	}
}

// trimSteps drops the oldest typed steps once the ring is full, keeping the
// index of what remains correct. Callers hold l.mu.
func (l *taskLive) trimSteps() {
	if len(l.calls) <= appTaskStepsMax {
		return
	}
	// Copied down rather than resliced, for [taskLive.push]'s own reason: a
	// slice that only ever moves its start keeps the whole backing array alive
	// for the life of the node.
	drop := len(l.calls) - appTaskStepsMax
	copy(l.calls, l.calls[drop:])
	l.calls = l.calls[:appTaskStepsMax]
	for call, at := range l.stepAt {
		if at-drop < 0 {
			delete(l.stepAt, call)
			continue
		}
		l.stepAt[call] = at - drop
	}
}

// appLiveStepID is a live step's stable identity: the provider's call id where
// there is one, and the ring's own position otherwise.
func appLiveStepID(event Event, at int) string {
	if call := strings.TrimSpace(event.CallID); call != "" {
		return call
	}
	return AppStepLive.prefix() + strconv.Itoa(at)
}

func (s AppStepSource) prefix() string { return string(s) + ":" }

// liveSteps copies the typed ring out, bounded. Callers hold l.mu.
func (l *taskLive) liveSteps(limit int) []AppTaskStep {
	out := make([]AppTaskStep, 0, len(l.calls))
	for _, held := range l.calls {
		step := held.step
		step.Tail = append([]string(nil), held.step.Tail...)
		out = append(out, step)
	}
	return appStepsFrom(out, limit)
}

// ── the durable records ────────────────────────────────────────────────────

// appStepCommand is what a call was ASKED to do, read out of the arguments the
// record kept: the command field where the call has one, and otherwise nothing.
//
// IT READS ONE NAMED FIELD AND GUESSES AT NOTHING. A gloss is already carried in
// Target, and a reader that wants the path an edit touched has the arguments
// themselves. Inventing a command out of a sentence is the move that puts words
// in a record's mouth.
func appStepCommand(tool, args, hint string) string {
	var fields map[string]json.RawMessage
	if json.Unmarshal([]byte(args), &fields) != nil {
		return ""
	}
	raw, ok := fields["command"]
	if !ok {
		return ""
	}
	var command string
	if json.Unmarshal(raw, &command) != nil {
		return ""
	}
	return appStepText(command)
}

// appJournalSteps is a node's steps as its durable session file holds them.
//
// IT IS THE REASON A REOPENED TASK READS AS ITSELF. A node restored after a
// restart has no live ring at all, and before this its page was handed the
// journal's lines glued together ("read internal/parse/lex.go the file is …")
// with every fact about every call already spent on the gluing.
func appJournalSteps(path string, limit int) []AppTaskStep {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	return appStepsFrom(appStepCache.read(path, func() []AppTaskStep {
		return appStepsOfRecord(ReadTranscript(path))
	}), limit)
}

// appStepsOfRecord is the shaping, over a record already read. It is separate so
// a reader holding a record's BYTES — a hosted page's journal tail — shapes it
// the same way, and so the shaping is testable without a file.
func appStepsOfRecord(record Record) []AppTaskStep {
	var out []AppTaskStep
	for _, items := range [][]DisplayEntry{record.Earlier, record.Entries} {
		for _, item := range items {
			if item.Role != "tool" {
				continue
			}
			// A TOOL'S RESULT MESSAGE IS NOT A SECOND CALL. The shaping keeps a
			// row for the result with no id and no name; the call it answers is
			// the entry above it, carrying that result as its output. Taken as a
			// step of its own it is a blank the record never named — which is
			// exactly the rule cmd/codeaf's own transcript shaping already
			// applies, said here so one reading of a record cannot disagree
			// with the other about how many steps it holds.
			if item.CallID == "" && strings.TrimSpace(item.Tool) == "" {
				continue
			}
			out = append(out, appStepOfEntry(item, len(out)))
		}
	}
	return out
}

// appStepOfEntry is one journaled call as a step.
func appStepOfEntry(item DisplayEntry, at int) AppTaskStep {
	step := AppTaskStep{
		ID:       item.CallID,
		Call:     item.CallID,
		Tool:     strings.TrimSpace(item.Tool),
		Category: ActionCategoryForTool(item.Tool),
		Target:   appStepText(item.Hint),
		Args:     item.Args,
		Took:     item.Took,
		Source:   AppStepJournal,
	}
	if step.ID == "" {
		step.ID = AppStepJournal.prefix() + strconv.Itoa(at)
	}
	step.Command = appStepCommand(item.Tool, item.Args, item.Hint)
	// A JOURNAL KEEPS A DURATION WITHOUT KEEPING EITHER INSTANT. The `took` line
	// is the call's own work from begin to end of its Execute; the message's
	// instant is when the ASKING was written. So the start is what the record
	// genuinely holds and the end is derived from it ONLY where both halves were
	// recorded — never from a duration alone, which would place a step at an
	// instant the file never claimed.
	if !item.At.IsZero() {
		step.StartedAt = item.At
		if item.Took > 0 {
			step.EndedAt = item.At.Add(item.Took)
		}
	}
	// AN UNANSWERED CALL IS UNSETTLED, NOT FAILED. The assistant message is
	// journaled before its batch runs, so a session read while a call was in
	// flight — or one killed mid-batch — names the asking and nothing else.
	if item.Answered {
		ok := true
		step.OK = &ok
		if detail := appStepText(item.Output); detail != "" {
			step.Detail = detail
			step.Tail = appStepTail(strings.Split(strings.TrimRight(detail, "\n"), "\n"))
		}
	}
	// A CALL WITH NO TOOL IS A LINE. Nothing in the record says which hands were
	// used, so nothing here claims to.
	if step.Tool == "" {
		step.Legacy = true
		step.Text = appStepText(strings.TrimSpace(item.Hint + " " + item.Text))
	}
	return step
}

// appTrajectorySteps is a handed-over run's steps as its trajectory holds them,
// with the step each of the task's notes was handed at.
//
// The run's recorder writes a line per step with the tool, the command as the
// model spelled it, the head of what came back, and the exit the command
// reported (internal/run's Step). Before this, the one field read back was the
// command — so a run's page lost which tool had been used, whether the step ran
// at all, and what it exited with, on every read.
func appTrajectorySteps(path string, limit int) ([]AppTaskStep, map[string]int) {
	file, err := openPlanTrajectoryFile(path)
	if err != nil {
		return nil, map[string]int{}
	}
	defer file.Close()
	return parseAppTrajectorySteps(file, limit)
}

func parseAppTrajectorySteps(reader io.Reader, limit int) ([]AppTaskStep, map[string]int) {
	reads := map[string]int{}
	var steps []AppTaskStep
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		var line struct {
			Kind        string   `json:"kind"`
			Step        int      `json:"step"`
			Tool        string   `json:"tool"`
			Command     string   `json:"command"`
			Observation string   `json:"observation"`
			ExitCode    *int     `json:"exit_code"`
			NotRun      bool     `json:"not_run"`
			Refused     bool     `json:"refused"`
			Notes       []string `json:"notes"`
		}
		if json.Unmarshal(scanner.Bytes(), &line) != nil {
			continue
		}
		switch line.Kind {
		case "step":
			steps = append(steps, appTrajectoryStep(line.Step, len(steps), line.Tool, line.Command,
				line.Observation, line.ExitCode, line.NotRun, line.Refused))
		case "notes":
			for _, id := range line.Notes {
				if _, known := reads[id]; !known {
					reads[id] = len(steps)
				}
			}
		}
	}
	return appStepsFrom(steps, limit), reads
}

// appTrajectoryStep is one recorded trajectory line as a step.
func appTrajectoryStep(number, at int, tool, command, observation string, exit *int, notRun, refused bool) AppTaskStep {
	step := AppTaskStep{
		ID:       AppStepTrajectory.prefix() + strconv.Itoa(number),
		Tool:     strings.TrimSpace(tool),
		Category: ActionCategoryForTool(tool),
		Command:  appStepText(command),
		Step:     number,
		Source:   AppStepTrajectory,
	}
	if number == 0 {
		step.ID = AppStepTrajectory.prefix() + strconv.Itoa(at)
	}
	if exit != nil {
		code := *exit
		step.ExitCode = &code
	}
	// A STEP THE HARNESS ANSWERED IS NOT A STEP THE WORLD ANSWERED. The recorder
	// establishes that at the event, where an attempted action is known; the
	// answer's own words are never read back for it.
	step.HarnessMade, step.Refused = notRun, refused
	switch {
	case refused:
		// A door refused something the worker tried: that is a settled failure.
		failed := false
		step.OK = &failed
	case notRun:
		// Nothing was attempted on the world, so there is no outcome to claim.
	case exit != nil:
		ok := *exit == 0
		step.OK = &ok
	default:
		// NO RECORDED EXIT IS NOT A PASS. A record written before exits were
		// recorded says the step happened and nothing about how it came out.
	}
	if detail := appStepText(observation); detail != "" {
		step.Detail = detail
		step.Tail = appStepTail(strings.Split(strings.TrimRight(detail, "\n"), "\n"))
	}
	// A trajectory written before the tool field existed holds a command and
	// nothing else: it stays the line it is.
	if step.Tool == "" {
		step.Legacy = true
		step.Text = step.Command
	}
	return step
}

// ── the fingerprint cache ──────────────────────────────────────────────────
//
// A TASK PAGE POLLS, AND A JOURNAL IS NOT SMALL. Reading and reshaping a whole
// session file once a second for a page that has not changed is the cost that
// turns a correct projection into an unusable one, so a parse is kept against
// the file's own fingerprint — its size and the instant it was last written —
// and reused until the file moves underneath it.
//
// THE FINGERPRINT IS WHAT THE FILESYSTEM SAYS, NOT WHAT WE HOPE. A file that
// cannot be stat-ed has no fingerprint, and is read every time rather than
// served from a parse that may be about a file that no longer exists.

type appStepFingerprint struct {
	size    int64
	modTime time.Time
}

type appStepParse struct {
	print appStepFingerprint
	steps []AppTaskStep
	used  time.Time
}

type appStepStore struct {
	mu    sync.Mutex
	parse map[string]appStepParse
	now   func() time.Time
}

var appStepCache = &appStepStore{parse: map[string]appStepParse{}}

func (s *appStepStore) clock() time.Time {
	if s.now != nil {
		return s.now()
	}
	return time.Now()
}

// read answers the parse of one file, reshaping it only when its fingerprint
// has moved. parse is called with no lock held: it reads a file, and holding a
// mutex across that would make every task page wait on the slowest journal.
func (s *appStepStore) read(path string, parse func() []AppTaskStep) []AppTaskStep {
	print, known := appStepPrintOf(path)
	if known {
		s.mu.Lock()
		held, cached := s.parse[path]
		if cached && held.print == print {
			held.used = s.clock()
			s.parse[path] = held
			steps := held.steps
			s.mu.Unlock()
			return steps
		}
		s.mu.Unlock()
	}
	steps := parse()
	if !known {
		return steps
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.parse[path] = appStepParse{print: print, steps: steps, used: s.clock()}
	s.forgetLocked()
	return steps
}

// forgetLocked keeps the store bounded, dropping the least recently used
// parses. Callers hold s.mu.
func (s *appStepStore) forgetLocked() {
	if len(s.parse) <= appStepFilesKept {
		return
	}
	paths := make([]string, 0, len(s.parse))
	for path := range s.parse {
		paths = append(paths, path)
	}
	sort.Slice(paths, func(i, j int) bool { return s.parse[paths[i]].used.Before(s.parse[paths[j]].used) })
	for _, path := range paths[:len(s.parse)-appStepFilesKept] {
		delete(s.parse, path)
	}
}

// appStepPrintOf is one file's fingerprint, and whether it has one at all.
func appStepPrintOf(path string) (appStepFingerprint, bool) {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return appStepFingerprint{}, false
	}
	return appStepFingerprint{size: info.Size(), modTime: info.ModTime()}, true
}
