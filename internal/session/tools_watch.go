package session

// The watch tool: the harness polls so the context does not.
//
// Watching a log today costs a turn per look. The model runs `tail -50 app.log`,
// reads fifty lines, waits, runs it again, reads forty-eight of the SAME fifty
// lines, and the transcript fills with near-identical chunks whose information
// content is two lines. Background jobs do not fix this — a job reports once,
// when it exits, and a log file never exits. Polling is the missing primitive's
// shadow: the model keeps asking because nothing can tell it.
//
// So the harness runs the command on a timer and speaks only when there is news.
// Three ideas hold the whole thing up:
//
//   - A WATCH IS A JOB. It gets an id from the same registry, a log file in the
//     same directory, a row in the same list, and it dies to the same kill and
//     the same Close (jobs.go). What differs from a background process is only
//     what happens in the middle — a timer instead of a wait — which is why it
//     is a jobKind and a stop function rather than a second machine with its own
//     vocabulary for "running".
//
//   - THE NOTE IS THE DELTA, NOT THE OUTPUT. Every mode answers the same
//     question — what is NEW — and answers it in at most forty lines. on=change
//     diffs against the previous tick and says only what appeared; on=match says
//     only the new lines the pattern picked; on=always is the deliberate
//     exception, for a number that is supposed to be watched climbing. The first
//     tick of a change or match watch is SILENT: it is the baseline, and a
//     baseline delivered as news would be exactly the fifty-line chunk this tool
//     exists to stop sending.
//
//   - IT RIDES THE STEERING LANE. The note is appended by the same registry
//     notify a job's exit note uses (jobs.go), drained into the transcript at
//     the next step boundary, read as plain user text. No push, no new event, no
//     change to any surface: news that arrives while the model is busy already
//     has a lane, and a second one would be a second ordering rule and a second
//     way to land inside a tool batch, which every provider rejects.
//
//     AND IT WAKES AN IDLE SESSION, which is the half a watch cannot do without.
//     The tool exists because the model STOPPED polling, so on an idle session
//     there is nothing left that will ever come and look: a delta that only
//     queued would wait for the person to type, which is the poll this replaced,
//     moved onto them. The wake and its coalescing are agent.go's
//     ([Agent.enqueueSteering]) — a watch ticking every ten seconds into a
//     running turn adds lines to it and starts nothing.
//
// Two governors, because a timer that never stops is a way to burn a session
// down. THREE WATCHES AT A TIME, so a model that discovers the tool cannot turn
// the transcript into a ticker tape. And THREE CONSECUTIVE FAILED TICKS WITH THE
// SAME ERROR ends the watch with one note — a watch spinning on a command that
// cannot run is the loop detector's cousin (looped.go), and the honest response
// to a repetition that carries no information is to say so once and stop.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/exec/bare"
)

const (
	// watchDefaultEvery, watchMinEvery and watchMaxEvery bound the timer. The
	// floor is what keeps a watch from being a busy loop with a nicer name; the
	// ceiling is an hour, past which the model should be asking rather than
	// watching.
	watchDefaultEvery = 10
	watchMinEvery     = 2
	watchMaxEvery     = 3600

	// watchMaxTickTimeout caps how long ONE run of the command may take,
	// alongside the interval itself: a tick that outlives its own period would
	// stall every tick behind it, and a watch that has stopped ticking is worse
	// than no watch because it looks like silence, and silence means "no news".
	watchMaxTickTimeout = 60 * time.Second

	// watchMaxConcurrent is how many watches one session may run at once.
	watchMaxConcurrent = 3

	// watchNoteLines caps a note's body, and watchTailLines is how much of the
	// output an on=always tick — or a change that removed lines rather than
	// adding them — is worth. Forty lines is a delta somebody can read; past
	// that the answer is `jobs output`, which is where the rest already is.
	watchNoteLines = 40
	watchTailLines = 10

	// watchNoteLineLimit clips ONE line of a note. A watch on a command that
	// prints minified JSON must not put a screenful in the transcript on the
	// strength of a single newline.
	watchNoteLineLimit = 200

	// watchFailLimit is how many consecutive identical failures end the watch.
	watchFailLimit = 3

	// watchLabelLimit keeps a derived name short enough to read inside a note.
	watchLabelLimit = 32

	// watchDefaultQuietTicks, watchMinQuietTicks and watchMaxQuietTicks bound
	// on=quiet's patience. Six ticks is a minute on the default interval, which
	// is the honest smallest span in which "it has stopped printing" means
	// anything: a build between two noisy phases goes quiet for seconds at a
	// time, and a watch that called that finished would be wrong more often
	// than right. The floor is two because one unchanged tick is not a run of
	// them, and the ceiling is the same "past this, ask rather than watch"
	// judgement watchMaxEvery makes.
	watchDefaultQuietTicks = 6
	watchMinQuietTicks     = 2
	watchMaxQuietTicks     = 100
)

// watchMode is what counts as news.
type watchMode string

const (
	watchOnChange watchMode = "change"
	watchOnMatch  watchMode = "match"
	watchOnAlways watchMode = "always"
	// watchOnQuiet is on=change's inverse, and it is the shape the other three
	// modes cannot make: news is the output NOT moving. It is how a watch
	// answers "tell me when this has finished" for the very large class of
	// commands that finish without saying so — a build whose log stops growing,
	// a download whose byte count stops climbing, a server that has stopped
	// logging its startup.
	//
	// It ENDS the watch, the way `until` does, because a run of unchanged ticks
	// is an answer and not a symptom: a watch that reported it and kept going
	// would report it again every tick from then on.
	watchOnQuiet watchMode = "quiet"
)

// watchSpec is one watch's settled terms: what to run, how often, and what to
// say something about. It is built once by the tool, validated there, and read
// without a lock by the goroutine that owns the timer.
type watchSpec struct {
	command string
	name    string
	every   time.Duration
	on      watchMode
	pattern *regexp.Regexp
	until   *regexp.Regexp
	// quietTicks is how many consecutive unchanged ticks end an on=quiet watch.
	// It is meaningless in every other mode and is refused there rather than
	// ignored (see [parseWatchArguments]).
	quietTicks int
}

// quietSpan is how long the quiet run this watch is waiting for actually lasts,
// which is the figure its final note names. It is derived from the interval and
// the tick count rather than measured, because those two are what the person
// asked for and the answer must be the terms they set.
func (s watchSpec) quietSpan() string {
	return fmt.Sprintf("%d ticks (%ds)", s.quietTicks, s.quietTicks*int(s.every/time.Second))
}

// detail is the terms in the words the list row and the start line both use.
func (s watchSpec) detail() string {
	detail := fmt.Sprintf("every %ds · on %s", int(s.every/time.Second), s.on)
	if s.on == watchOnMatch {
		detail += fmt.Sprintf(" /%s/", s.pattern)
	}
	if s.on == watchOnQuiet {
		detail += fmt.Sprintf(" · %s", s.quietSpan())
	}
	if s.until != nil {
		detail += fmt.Sprintf(" · until /%s/", s.until)
	}
	return detail
}

// watchState is what one watch remembers between ticks. It belongs to a single
// goroutine — the one running the timer — which is why nothing here is locked.
type watchState struct {
	// seeded marks that a baseline exists. Until it does, a change or match
	// watch says nothing: there is no "new" without a "before".
	seeded bool
	hash   uint64
	lines  []string
	// failText is the last tick's failure, verbatim, and failCount how many
	// ticks in a row have failed with exactly it. Identity is the whole tick
	// text on purpose: a command failing with a DIFFERENT message each time is
	// producing news, however unhappily, and is not the stuck case.
	failText  string
	failCount int
	// quietCount is how many ticks in a row have produced exactly the previous
	// tick's text, for on=quiet. Identity is the whole tick text, which is the
	// same identity the delta logic and the failure streak already use: a
	// command whose output differs by one byte has produced news, and news is
	// not quiet.
	quietCount int
}

// ── the tool ────────────────────────────────────────────────────────────────

// WRITTEN FOR DENSITY, BECAUSE THIS STRING IS BILLED ON EVERY REQUEST OF EVERY
// TURN. The belt's schemas ride in front of each request the model makes, so a
// sentence here is paid dozens of times in one task while the prose above it is
// free. Every rule the long version stated survives; what went is the worked
// examples and the second telling of what the `on` field already says. The
// boundary with `stand` stays in full, because that one is a defect a real
// model made (standing_boundary_test.go pins the words).
//
// The concurrency limit is INTERPOLATED, never typed: [watchMaxConcurrent] is
// what claimWatch actually enforces and a digit here would be the second copy
// that drifts.
var watchDescription = "Run a command on a timer and hear only when there is news, instead of polling it every turn. A background job (jobs lists and kills it) whose notes arrive at the next step boundary. At most " + strconv.Itoa(watchMaxConcurrent) + " at once. A WATCH DIES WITH THIS CONVERSATION; what must keep looking AFTER this window is closed is `stand`'s."

// Every bound in the schema is INTERPOLATED from the constant the parser clamps
// against ([parseWatchArguments]), for the one-source-of-truth law's reason: a
// model reasons from the figure it is shown, and a hand-typed digit is the copy
// that goes stale the day the constant moves.
var watchSchemaJSON = `{"type":"object","properties":{` +
	`"command":{"type":"string","description":"Command run each tick in the workspace."},` +
	`"every_seconds":{"type":"number","description":"Seconds per tick (default: ` + strconv.Itoa(watchDefaultEvery) + `, min: ` + strconv.Itoa(watchMinEvery) + `, max: ` + strconv.Itoa(watchMaxEvery) + `)"},` +
	`"on":{"type":"string","description":"News, one mode only. change: new lines when output differs. match: new lines matching pattern. always: the last ` + strconv.Itoa(watchTailLines) + ` lines each tick. quiet: unchanged for quiet_ticks ticks running, which catches a silent finish and ends the watch. Tick one is a silent baseline, except always.","enum":["change","match","always","quiet"]},` +
	`"quiet_ticks":{"type":"number","description":"Ticks for on=quiet (default: ` + strconv.Itoa(watchDefaultQuietTicks) + `, min: ` + strconv.Itoa(watchMinQuietTicks) + `, max: ` + strconv.Itoa(watchMaxQuietTicks) + `); refused in other modes."},` +
	`"pattern":{"type":"string","description":"Lines to report, as a regex; required when on is match."},` +
	`"until":{"type":"string","description":"Regex ending the watch; its first match is the last note."},` +
	`"name":{"type":"string","description":"Label for notes and the jobs row (default: from the command)."}` +
	`},"required":["command"],"additionalProperties":false}`

// watchArguments is the wire form. everySeconds is a pointer so an absent
// interval and an explicit 0 stay different answers: absent takes the default,
// while 0 is a number the model chose and gets clamped to the floor.
type watchArguments struct {
	Command      string `json:"command"`
	EverySeconds *int   `json:"every_seconds"`
	On           string `json:"on"`
	// QuietTicks is a pointer for EverySeconds' reason and one more: an absent
	// figure takes the default, while a figure the model spelled out in a mode
	// that has no use for it is a misunderstanding worth saying out loud.
	QuietTicks *int   `json:"quiet_ticks"`
	Pattern    string `json:"pattern"`
	Until      string `json:"until"`
	Name       string `json:"name"`
}

// watchTool is the belt entry. It validates, starts, and returns ONE LINE — the
// terms of the watch and how to end it, never the command's output. Returning
// the first tick here would reintroduce the chunk the tool exists to remove,
// and it would arrive before the watch had anything to compare it to.
func (a *Agent) watchTool() bare.Tool {
	return bare.Tool{
		Name:        "watch",
		Description: watchDescription,
		Schema:      json.RawMessage(watchSchemaJSON),
		Execute: func(ctx context.Context, args json.RawMessage) (string, bool, error) {
			spec, problem := parseWatchArguments(args)
			if problem != "" {
				return problem, true, nil
			}
			started, err := a.jobs.startWatch(spec)
			if err != nil {
				return err.Error(), true, nil
			}
			return fmt.Sprintf("watch %s started · %s — kill with jobs",
				started.label, started.detail), false, nil
		},
	}
}

func parseWatchArguments(args json.RawMessage) (watchSpec, string) {
	var parsed watchArguments
	if err := json.Unmarshal(args, &parsed); err != nil {
		return watchSpec{}, "Invalid arguments: " + err.Error()
	}
	spec := watchSpec{command: strings.TrimSpace(parsed.Command)}
	if spec.command == "" {
		return spec, "Invalid arguments: command is required"
	}

	seconds := watchDefaultEvery
	if parsed.EverySeconds != nil {
		seconds = *parsed.EverySeconds
	}
	// Clamped rather than refused: an out-of-range interval is a model reaching
	// for "as often as possible" or "hardly ever", and both of those have a
	// nearest legal answer that does what was meant.
	if seconds < watchMinEvery {
		seconds = watchMinEvery
	}
	if seconds > watchMaxEvery {
		seconds = watchMaxEvery
	}
	spec.every = time.Duration(seconds) * time.Second

	spec.on = watchMode(strings.TrimSpace(parsed.On))
	if spec.on == "" {
		spec.on = watchOnChange
	}
	switch spec.on {
	case watchOnChange, watchOnMatch, watchOnAlways, watchOnQuiet:
	default:
		return spec, fmt.Sprintf("Invalid arguments: on must be change, match, always, or quiet (got %q)", parsed.On)
	}

	// quiet_ticks belongs to one mode, and a call that sets it in another is
	// REFUSED rather than quietly ignored: a model that wrote it meant something
	// by it, and the something it meant — "tell me when this goes quiet" — is
	// not what on=change is about to do. The refusal is the shape every other
	// bad argument here gets, because a wrong-combination error the model has
	// never seen is a wrong-combination error it cannot learn from.
	if parsed.QuietTicks != nil && spec.on != watchOnQuiet {
		return spec, fmt.Sprintf("Invalid arguments: quiet_ticks only applies when on is quiet (got on=%s)", spec.on)
	}
	if spec.on == watchOnQuiet {
		ticks := watchDefaultQuietTicks
		if parsed.QuietTicks != nil {
			ticks = *parsed.QuietTicks
		}
		// Clamped rather than refused, exactly as the interval is, and for the
		// same reason: an out-of-range count is a model reaching for "as soon as
		// possible" or "only when it is really over".
		if ticks < watchMinQuietTicks {
			ticks = watchMinQuietTicks
		}
		if ticks > watchMaxQuietTicks {
			ticks = watchMaxQuietTicks
		}
		spec.quietTicks = ticks
	}

	if pattern := strings.TrimSpace(parsed.Pattern); pattern != "" {
		compiled, err := regexp.Compile(pattern)
		if err != nil {
			return spec, "Invalid arguments: pattern is not a valid regular expression: " + err.Error()
		}
		spec.pattern = compiled
	}
	if spec.on == watchOnMatch && spec.pattern == nil {
		return spec, "Invalid arguments: pattern is required when on is match"
	}
	if until := strings.TrimSpace(parsed.Until); until != "" {
		compiled, err := regexp.Compile(until)
		if err != nil {
			return spec, "Invalid arguments: until is not a valid regular expression: " + err.Error()
		}
		spec.until = compiled
	}

	spec.name = watchLabel(parsed.Name, spec.command)
	return spec, ""
}

// watchLabel is the watch's handle: what the model asked for, or something
// derived from the command it gave.
//
// The derivation is the verb plus the last thing that is not a flag —
// `tail -50 app.log` becomes `tail-app.log` — because a label is read inside a
// note ("watch tail-app.log · 2 new lines") where "watch 3" would make the
// reader go and look up what 3 was.
func watchLabel(asked, command string) string {
	if given := strings.TrimSpace(asked); given != "" {
		return truncateLabel(given)
	}
	fields := strings.Fields(firstLine(command))
	if len(fields) == 0 {
		return "watch"
	}
	label := filepath.Base(strings.Trim(fields[0], `"'`))
	for index := len(fields) - 1; index > 0; index-- {
		candidate := strings.Trim(fields[index], `"'`)
		if candidate == "" || strings.HasPrefix(candidate, "-") {
			continue
		}
		label += "-" + filepath.Base(candidate)
		break
	}
	return truncateLabel(label)
}

func truncateLabel(label string) string {
	runes := []rune(label)
	if len(runes) <= watchLabelLimit {
		return label
	}
	return string(runes[:watchLabelLimit])
}

// ── starting and stopping ───────────────────────────────────────────────────

// startWatch registers a watch as a job and starts its timer.
//
// The slot is claimed BEFORE the job exists, and released by the goroutine that
// ends the watch, so the count is of watches that are actually ticking rather
// than of rows in a list that also holds every watch this session has ever run.
func (r *jobRegistry) startWatch(spec watchSpec) (*job, error) {
	name, claimed := r.claimWatch(spec.name)
	if !claimed {
		return nil, fmt.Errorf("this session already has %d watches running, which is the limit — stop one with jobs kill first, or use bash background:true for a command that ends on its own", watchMaxConcurrent)
	}
	spec.name = name

	started, err := r.newJob(spec.command, jobKindWatch)
	if err != nil {
		r.releaseWatch()
		return nil, fmt.Errorf("could not start the watch: %w", err)
	}
	started.label = spec.name
	started.detail = spec.detail()

	// Background, not the caller's context: a watch outlives the turn that
	// started it, for the same reason a background job does. Its end is this
	// cancel, reached through job.signal by jobs kill and by Close.
	ctx, cancel := context.WithCancel(context.Background())
	started.stop = cancel
	r.add(started)

	go r.runWatch(ctx, cancel, started, spec)
	return started, nil
}

// claimWatch takes one of the session's watch slots and settles the watch's
// name, both under the registry's lock: two starts in one tool batch run
// concurrently, so a limit checked outside the lock is a limit that lets three
// through, and a name checked outside it is a name two watches can share.
func (r *jobRegistry) claimWatch(label string) (string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.watches >= watchMaxConcurrent {
		return "", false
	}
	r.watches++

	unique := label
	for suffix := 2; r.labelTakenLocked(unique); suffix++ {
		unique = fmt.Sprintf("%s-%d", label, suffix)
	}
	return unique, true
}

// labelTakenLocked reports whether any watch this session ever started already
// answers to this name — dead ones included, because the transcript still holds
// their notes, and a name that meant two different commands in one conversation
// is worse than an ugly one.
func (r *jobRegistry) labelTakenLocked(label string) bool {
	for _, existing := range r.jobs {
		if existing.kind == jobKindWatch && existing.label == label {
			return true
		}
	}
	return false
}

func (r *jobRegistry) releaseWatch() {
	r.mu.Lock()
	if r.watches > 0 {
		r.watches--
	}
	r.mu.Unlock()
}

// runWatch is the timer loop: tick, decide, maybe speak, wait.
//
// The first tick runs IMMEDIATELY rather than after one interval. A watch on a
// ten-minute timer whose baseline is ten minutes old is a watch that reports
// ten minutes of accumulated change as its first news, and a person who starts
// a watch means "from now".
func (r *jobRegistry) runWatch(ctx context.Context, cancel context.CancelFunc, watched *job, spec watchSpec) {
	defer cancel()
	defer r.releaseWatch()

	ticker := time.NewTicker(spec.every)
	defer ticker.Stop()

	state := &watchState{}
	for ctx.Err() == nil {
		note, final := r.watchTick(ctx, watched, spec, state)
		// A cancel that landed during the tick is a kill, and a kill is not
		// news: the caller who asked for it already knows, exactly as a killed
		// job does not report its own death (jobs.go).
		if ctx.Err() != nil {
			break
		}
		if note != "" && r.notify != nil {
			r.notify(note)
		}
		if final {
			break
		}
		select {
		case <-ctx.Done():
		case <-ticker.C:
		}
	}
	// Code 0: a watch has no exit status of its own to report. Its ending was
	// either asked for (settle reads that and says "killed") or already said in
	// a note, and statusText renders the rest as "stopped".
	r.settled(watched, 0)
}

// watchTick runs the command once and decides what, if anything, to say. The
// bool is "this was the last tick".
//
// The order of the decisions is the design. `until` is checked FIRST and on
// every tick including the baseline, because it is the answer to a question the
// person asked ("tell me when this appears") and a match that was already there
// is still the answer. The failure streak is checked SECOND, so a watch that
// has broken says it is broken rather than reporting its own error text as news
// three times. Everything else is the delta.
func (r *jobRegistry) watchTick(ctx context.Context, watched *job, spec watchSpec, state *watchState) (string, bool) {
	output, failure := r.runTick(ctx, spec)
	if ctx.Err() != nil {
		return "", true
	}
	count := watched.countTick()

	// The whole tick goes to the job's log and its ring, so `jobs output` and
	// the file on disk answer "what did it actually print" the way they do for
	// any other job. The note is a summary of this; it is not a replacement.
	text := clampTail(output, jobRingBytes)
	if failure != "" {
		if strings.TrimSpace(text) == "" {
			text = failure
		} else {
			text = strings.TrimRight(text, "\n") + "\n" + failure
		}
	}
	fmt.Fprintf(watched.sink, "── tick %d ──\n%s\n", count, strings.TrimRight(text, "\n"))

	lines := splitLines(text)

	if spec.until != nil {
		if hit, found := firstMatching(lines, spec.until); found {
			return fmt.Sprintf("watch %s: until matched\n%s", spec.name, clip(hit, watchNoteLineLimit)), true
		}
	}

	if failure != "" {
		if text == state.failText {
			state.failCount++
		} else {
			state.failText, state.failCount = text, 1
		}
		if state.failCount >= watchFailLimit {
			return watchNote(spec.name,
				fmt.Sprintf("stopped: the command failed %d ticks in a row", state.failCount),
				tailOf(lines, watchTailLines)), true
		}
	} else {
		state.failText, state.failCount = "", 0
	}

	hash := hashLines(text)
	fresh := deltaLines(state.lines, lines)
	baseline := !state.seeded
	state.seeded, state.lines = true, lines
	previousHash := state.hash
	state.hash = hash

	switch spec.on {
	case watchOnQuiet:
		// The baseline is a tick with nothing before it, so it cannot be part of
		// a run of unchanged ticks: the count starts at the FIRST tick that
		// matched its predecessor.
		if baseline || hash != previousHash {
			state.quietCount = 0
			return "", false
		}
		state.quietCount++
		if state.quietCount < spec.quietTicks {
			return "", false
		}
		// `until`'s shape, for `until`'s reason: this is the answer to a
		// question the caller asked, so it is one sentence naming the terms that
		// were met, then the single line of evidence — what the command was
		// still saying when it stopped saying anything new.
		return fmt.Sprintf("watch %s: quiet for %s\n%s",
			spec.name, spec.quietSpan(), clip(lastNonEmpty(lines), watchNoteLineLimit)), true

	case watchOnAlways:
		// The one mode with no baseline: "show me the number climbing" wants the
		// first number too.
		return watchNote(spec.name, fmt.Sprintf("tick %d", count), tailOf(lines, watchTailLines)), false

	case watchOnMatch:
		if baseline {
			return "", false
		}
		matched := matchingLines(fresh, spec.pattern)
		if len(matched) == 0 {
			return "", false
		}
		return watchNote(spec.name,
			fmt.Sprintf("%s matching /%s/", countedLines(len(matched)), spec.pattern), matched), false

	default:
		if baseline || hash == previousHash {
			return "", false
		}
		if len(fresh) == 0 {
			// The output changed by LOSING lines — a truncated file, a rotated
			// log, a command that now prints less. There is no delta to quote,
			// so the tail is the honest report of what it says now.
			return watchNote(spec.name, "output changed", tailOf(lines, watchTailLines)), false
		}
		return watchNote(spec.name, countedLines(len(fresh))+" new", fresh), false
	}
}

// runTick runs the command once, through the same shell, workspace, environment
// and process group a background job gets (jobs.go) — a watch must not be able
// to see or do anything bash could not.
//
// It returns the combined output and, when the run did not succeed, one line
// describing how. Both matter: the output is what the delta is computed from,
// and the failure line is both part of that output (a command that starts
// failing IS news) and the identity the failure streak counts.
func (r *jobRegistry) runTick(ctx context.Context, spec watchSpec) (string, string) {
	// Two bounds, and the tighter one wins: never longer than the interval, so
	// ticks cannot pile up on each other, and never longer than a minute, so a
	// watch on an hourly timer cannot sit inside one hung run for an hour.
	timeout := spec.every
	if timeout > watchMaxTickTimeout {
		timeout = watchMaxTickTimeout
	}
	tickCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	shell, shellArgs := jobShell()
	process := exec.CommandContext(tickCtx, shell, append(shellArgs, spec.command)...)
	process.Dir = r.workspace
	process.Env = os.Environ()
	process.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	// The timeout kills the whole GROUP, not just the shell: a tick that ran
	// `sleep 600 | grep x` leaves two processes, and killing the parent alone
	// would leak the rest of them once per tick, forever.
	process.Cancel = func() error {
		signalGroup(process, syscall.SIGKILL)
		return nil
	}
	// And the wait is bounded too, because output is copied from a pipe a
	// grandchild may still hold open after its parent died.
	process.WaitDelay = 2 * time.Second

	var captured bytes.Buffer
	process.Stdout = &captured
	process.Stderr = &captured

	err := process.Run()
	output := captured.String()
	switch {
	case tickCtx.Err() == context.DeadlineExceeded:
		return output, fmt.Sprintf("(the tick timed out after %s)", timeout)
	case err != nil:
		return output, fmt.Sprintf("(the command failed: %s)", err)
	}
	return output, ""
}

// ── notes ───────────────────────────────────────────────────────────────────

// watchNote renders one note: a header naming the watch and what happened, then
// the lines, capped.
//
// The shape is the exit note's (jobs.go) — a sentence, then evidence — because
// they land in the same lane and a model reading the transcript should not have
// to learn two formats for "something happened while you were working".
func watchNote(name, summary string, lines []string) string {
	var rendered strings.Builder
	fmt.Fprintf(&rendered, "watch %s · %s", name, summary)
	if len(lines) == 0 {
		rendered.WriteString("\n(no output)")
		return rendered.String()
	}
	shown := lines
	if len(shown) > watchNoteLines {
		shown = shown[:watchNoteLines]
	}
	for _, line := range shown {
		rendered.WriteString("\n")
		rendered.WriteString(clip(line, watchNoteLineLimit))
	}
	if overflow := len(lines) - len(shown); overflow > 0 {
		fmt.Fprintf(&rendered, "\n… %d more", overflow)
	}
	return rendered.String()
}

func countedLines(n int) string {
	if n == 1 {
		return "1 line"
	}
	return fmt.Sprintf("%d lines", n)
}

// ── comparing ticks ─────────────────────────────────────────────────────────

// deltaLines is what is NEW in this tick, given the last one.
//
// It finds the longest suffix of the previous output that is also a prefix of
// this one, and calls everything past it new. That overlap is what makes the
// tool work on the command it was built for: `tail -50 app.log` scrolls, so the
// previous fifty lines and the current fifty share forty-eight in the middle,
// and the answer is the two at the end — not fifty, and not zero. A growing
// `cat` is the same rule with the overlap at full length, and output with
// nothing in common (a fresh `date`) is the same rule with the overlap at zero.
func deltaLines(previous, current []string) []string {
	if len(previous) == 0 {
		return current
	}
	for overlap := min(len(previous), len(current)); overlap > 0; overlap-- {
		if equalLines(previous[len(previous)-overlap:], current[:overlap]) {
			return current[overlap:]
		}
	}
	return current
}

func equalLines(left, right []string) bool {
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func matchingLines(lines []string, pattern *regexp.Regexp) []string {
	var matched []string
	for _, line := range lines {
		if pattern.MatchString(line) {
			matched = append(matched, line)
		}
	}
	return matched
}

func firstMatching(lines []string, pattern *regexp.Regexp) (string, bool) {
	for _, line := range lines {
		if pattern.MatchString(line) {
			return line, true
		}
	}
	return "", false
}

// lastNonEmpty is the one line an on=quiet note quotes: what the command was
// still saying when it stopped saying anything new. Blank lines are skipped for
// [jobSink.lastNonEmptyLine]'s reason — a command whose last write was a newline
// still has something to say about where it got to — and a command that printed
// nothing at all answers with the emptiness law's own answer, which is nothing.
func lastNonEmpty(lines []string) string {
	for index := len(lines) - 1; index >= 0; index-- {
		if trimmed := strings.TrimSpace(lines[index]); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func tailOf(lines []string, n int) []string {
	if len(lines) <= n {
		return lines
	}
	return lines[len(lines)-n:]
}

// splitLines is the tick's output as lines, with the trailing newline's empty
// last element dropped — otherwise every tick would show one blank "new" line.
func splitLines(text string) []string {
	text = strings.TrimRight(text, "\n")
	if text == "" {
		return nil
	}
	return strings.Split(text, "\n")
}

// clampTail bounds what one tick may carry, keeping the END of it: a command
// that prints a megabyte per tick must not be able to hold a megabyte per tick
// in memory, and the recent end is the part a delta can be computed against.
func clampTail(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	start := len(text) - limit
	for start < len(text) && !utf8RuneStart(text[start]) {
		start++
	}
	// Start at a line boundary when there is one nearby, so the first kept line
	// is a line rather than the back half of one.
	if newline := strings.IndexByte(text[start:], '\n'); newline >= 0 {
		start += newline + 1
	}
	return text[start:]
}

func hashLines(text string) uint64 {
	digest := fnv.New64a()
	_, _ = digest.Write([]byte(text))
	return digest.Sum64()
}
