// The model-call log from the command line.
//
// `aforge logs` is the reading end of internal/calllog: the file every model
// call in the process writes a line to, always, without a proxy in front of
// anything. The file is JSON Lines because a machine reads it too; this command
// exists because a person does not want to.
//
// One line per call. A call that is still in flight shows as one — that is the
// whole reason the log writes a row on the way OUT as well as on the way back:
// a planning call four minutes into a 65,536-token ceiling used to be
// indistinguishable from a process doing nothing at all.
//
// THE READER SHOWS EVERYTHING THE RECORD HOLDS. A field that is on the row and
// not on the line is a field a person has to leave the tool to read, and the
// half of the row that used to be invisible — the lane asked for, the lane that
// answered, what was done about a silence and what it cost — is exactly the
// half somebody opens this command to see. Anything the row does not carry is
// absent rather than zero.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/calllog"
	"github.com/Agent-Field/aforge-v2/internal/home"
)

// defaultLogTail is how many calls `aforge logs` shows when nobody says. Forty
// is a few minutes of a busy run — enough to hold the call that went wrong and
// the ones around it, short enough to read in a terminal without scrolling.
const defaultLogTail = 40

// followInterval is how often --follow looks for more. A log is appended to by
// another process, so there is nothing to subscribe to; a quarter of a second
// is faster than a person can read and cheap enough to leave running.
const followInterval = 250 * time.Millisecond

// traceDirName and failuresDirName are the two places a call's body can be,
// both beside the ledger rather than under it: the trace is what the debug
// switch writes for a whole run, and the failures folder is the always-on
// keeping of the one call that went wrong. Neither is written yet — the
// foundation that fills them is #339 and its children — so --body reads them
// and says plainly when there is nothing there, which is the honest answer
// today and the right one afterwards without a change.
const (
	traceDirName    = "trace"
	failuresDirName = "failures"
)

func runLogs(args []string) error {
	// Resolved from the profile directory the same way every other durable
	// aforge file is, rather than from the running log's own singleton: this
	// process has not loaded a config and has opened nothing.
	return runLogsWith(args, os.Stdout,
		calllog.PathFor(strings.TrimSpace(os.Getenv("AFORGE_PROFILE_DIR"))), time.Now)
}

// runLogsWith is the command with its two outside readings injectable: where
// the log is, and what time it is — the second because an in-flight call's age
// is measured against now, and a test cannot wait three minutes to see it.
func runLogsWith(args []string, output io.Writer, path string, now func() time.Time) error {
	flags := flag.NewFlagSet("logs", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	tail := flags.Int("tail", defaultLogTail, "how many calls to show")
	follow := flags.Bool("follow", false, "keep printing calls as they happen")
	pathOnly := flags.Bool("path", false, "print where the log is and nothing else")
	asJSON := flags.Bool("json", false, "print the matching rows exactly as they are on disk")
	body := flags.String("body", "", "print the recorded body of one call id")
	filter := logFilter{}
	flags.StringVar(&filter.run, "run", "", "only calls belonging to this run")
	flags.StringVar(&filter.tag, "tag", "", "only calls with this tag")
	flags.StringVar(&filter.model, "model", "", "only calls asking for this model")
	flags.StringVar(&filter.node, "node", "", "only calls belonging to this node")
	flags.StringVar(&filter.id, "id", "", "only this call id, both of its rows")
	if err := flags.Parse(reorder(flags, args)); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("usage: aforge logs [--tail N] [--follow] [--path] [--json] " +
			"[--run id] [--id id] [--tag t] [--model m] [--node n] [--body id]")
	}
	// --body is answered before the ledger's own switch is consulted, because
	// the two are different records: a person who turned the line-per-call log
	// off can still have kept the body of the call that failed.
	if strings.TrimSpace(*body) != "" {
		return printCallBody(output, recordDirFor(path), strings.TrimSpace(*body))
	}
	if path == "" {
		// Switched off is a state, not a failure: the person turned it off, and
		// saying so is more use than an empty listing they would read as a
		// broken log.
		_, err := fmt.Fprintf(output, "the model-call log is off (%s=%s)\n", calllog.EnvVar, calllog.OffValue)
		return err
	}
	if *pathOnly {
		_, err := fmt.Fprintln(output, path)
		return err
	}
	// --json is a passthrough and not a rendering, so it prints the rows and
	// nothing else — no path header in front of them. The whole point of the
	// flag is that another program reads what comes out.
	if !*asJSON {
		if _, err := fmt.Fprintln(output, path); err != nil {
			return err
		}
	}
	calls, err := readCallLog(path)
	if err != nil {
		return err
	}
	if err := writeCallLog(output, filter.keep(calls), *tail, *asJSON, filter, now()); err != nil {
		return err
	}
	if !*follow {
		return nil
	}
	return followCallLog(output, path, *asJSON, filter, now)
}

// loggedCall is one row as it was read: the record the reader renders, and the
// exact bytes it came from, because --json hands those bytes back untouched and
// a re-encoding would quietly drop every field this build does not know about.
type loggedCall struct {
	record calllog.Record
	raw    string
	// run is the run this call belonged to, read out of the row's own JSON
	// rather than off calllog.Record, which has no such field yet — the run id
	// lands with the foundation (#339). Reading it generically means the filter
	// works the day the writer starts writing it and matches nothing until
	// then, which is the truthful behaviour in both states.
	run string
}

// logFilter is every --run, --id, --tag, --model and --node in one predicate,
// built once and asked once per row. Each is an exact match and they combine by
// AND: a person narrowing a log is adding conditions, never widening.
type logFilter struct {
	run   string
	tag   string
	model string
	node  string
	id    string
}

// matches answers whether one row survives every condition that was named. A
// condition nobody gave is not a condition, so an empty filter keeps the log
// exactly as it reads today.
func (f logFilter) matches(call loggedCall) bool {
	switch {
	case f.tag != "" && call.record.Tag != f.tag:
		return false
	case f.model != "" && call.record.Model != f.model:
		return false
	case f.node != "" && call.record.Node != f.node:
		return false
	case f.id != "" && call.record.ID != f.id:
		return false
	case f.run != "" && call.run != f.run:
		return false
	}
	return true
}

func (f logFilter) keep(calls []loggedCall) []loggedCall {
	if f == (logFilter{}) {
		return calls
	}
	kept := make([]loggedCall, 0, len(calls))
	for _, call := range calls {
		if f.matches(call) {
			kept = append(kept, call)
		}
	}
	return kept
}

// bothRows reports whether the start row of a finished attempt should be kept.
//
// It is asked for by --id and by nothing else. Naming one call is asking for
// that call's whole story, and the story is two rows: what went out, and what
// came back. Every other reading of the log wants the answer only, because a
// screen of paired rows is twice the log to read for nothing.
func (f logFilter) bothRows() bool { return f.id != "" }

// readCallLog reads every record in the file. A file that does not exist yet is
// no records and no error — nothing has called a model on this machine, which
// is a true thing to have found out rather than a fault.
//
// A line that will not parse is skipped rather than fatal: the last line of a
// log being written to at this instant may be half a line.
func readCallLog(path string) ([]loggedCall, error) {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer file.Close()
	return decodeCallLog(file)
}

func decodeCallLog(source io.Reader) ([]loggedCall, error) {
	var calls []loggedCall
	scanner := bufio.NewScanner(source)
	// A record with bodies switched on can be as long as a whole transcript, so
	// the scanner is given room a default 64K buffer does not have.
	scanner.Buffer(make([]byte, 0, 64<<10), 16<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var record calllog.Record
		if json.Unmarshal([]byte(line), &record) != nil {
			continue
		}
		var loose struct {
			Run string `json:"run"`
		}
		_ = json.Unmarshal([]byte(line), &loose)
		calls = append(calls, loggedCall{record: record, raw: line, run: strings.TrimSpace(loose.Run)})
	}
	if err := scanner.Err(); err != nil {
		return calls, err
	}
	return calls, nil
}

// writeCallLog prints what survived the filter, either as the rows themselves
// or as the lines a person reads.
func writeCallLog(output io.Writer, calls []loggedCall, tail int, asJSON bool, filter logFilter, now time.Time) error {
	if asJSON {
		for _, call := range tailOf(rawRows(calls), tail) {
			if _, err := fmt.Fprintln(output, call); err != nil {
				return err
			}
		}
		return nil
	}
	for _, line := range renderCallLog(calls, tail, filter.bothRows(), now) {
		if _, err := fmt.Fprintln(output, line); err != nil {
			return err
		}
	}
	return nil
}

func rawRows(calls []loggedCall) []string {
	rows := make([]string, 0, len(calls))
	for _, call := range calls {
		rows = append(rows, call.raw)
	}
	return rows
}

func tailOf(lines []string, tail int) []string {
	if tail > 0 && len(lines) > tail {
		return lines[len(lines)-tail:]
	}
	return lines
}

// renderCallLog turns records into the lines a person reads, newest last.
//
// THE PAIRING IS THE POINT. Every attempt writes a row as it goes out and a row
// as it comes back, sharing an id. A start whose end has arrived says nothing
// the end does not say better, so it is dropped; a start with no end is a call
// that is STILL RUNNING, and it is the one line this whole command exists for.
// The exception is a person who named one call id, who asked for that attempt's
// two rows and gets them.
func renderCallLog(calls []loggedCall, tail int, bothRows bool, now time.Time) []string {
	ended := make(map[string]bool, len(calls))
	for _, call := range calls {
		if call.record.Phase != calllog.PhaseStart && call.record.ID != "" {
			ended[call.record.ID] = true
		}
	}
	var lines []string
	for _, call := range calls {
		record := call.record
		answered := record.ID != "" && ended[record.ID]
		if !bothRows && record.Phase == calllog.PhaseStart && (record.ID == "" || answered) {
			continue
		}
		lines = append(lines, callLogLine(record, answered, now))
	}
	return tailOf(lines, tail)
}

// callLogLine is one call on one line. Every segment is dropped when it is
// empty rather than printed as a zero or a dash: a call that never reached an
// endpoint has no duration and no finish reason, and a column of "0" would be a
// measurement nobody made.
//
// answered says whether this row's other half has already arrived. It only ever
// matters on a start row, and only under --id, which is the one reading that
// shows both halves: a request whose reply is on the next line was SENT, and
// calling it still in flight would be the line telling a person the opposite of
// what the line under it says.
func callLogLine(record calllog.Record, answered bool, now time.Time) string {
	fields := []string{clockOf(record.Time), record.Tag, record.Model, laneOf(record)}
	if record.Node != "" {
		fields = append(fields, "#"+record.Node)
	}
	fields = append(fields, record.Effort)
	if record.MaxTokens > 0 {
		fields = append(fields, fmt.Sprintf("max %d", record.MaxTokens))
	}
	if record.Attempt > 1 {
		fields = append(fields, fmt.Sprintf("try %d", record.Attempt))
	}
	if len(record.Relaxed) > 0 {
		fields = append(fields, "without "+strings.Join(record.Relaxed, ","))
	}
	if record.Phase == calllog.PhaseStart {
		outcome := inFlightFor(record.Time, now)
		if answered {
			outcome = "sent"
		}
		fields = append(fields, deadlineOf(record), outcome)
		return strings.Join(compactFields(fields), "  ")
	}
	fields = append(fields, arrowOf(record))
	if record.Millis > 0 {
		fields = append(fields, shortDuration(time.Duration(record.Millis)*time.Millisecond))
	}
	// The wait before the first token, kept next to the whole duration because
	// the pair is the reading: a slow call whose first token was instant was
	// slow writing, and one whose first token took nine seconds was queued.
	if record.TTFTms > 0 {
		fields = append(fields, "first token "+shortDuration(time.Duration(record.TTFTms)*time.Millisecond))
	}
	fields = append(fields, deadlineOf(record), record.Finish)
	if record.Error != "" {
		fields = append(fields, record.Error)
	}
	if record.EmptyAtCeiling {
		fields = append(fields, "empty at the ceiling")
	}
	if record.CompletionTokens > 0 {
		tokens := fmt.Sprintf("%d tok", record.CompletionTokens)
		if record.ReasoningTokens > 0 {
			tokens += fmt.Sprintf(" (%d thinking)", record.ReasoningTokens)
		}
		fields = append(fields, tokens)
	}
	if record.CachedTokens > 0 {
		fields = append(fields, fmt.Sprintf("%d cached", record.CachedTokens))
	}
	if record.Cost > 0 {
		fields = append(fields, fmt.Sprintf("$%.4f", record.Cost))
	}
	fields = append(fields, hedgeFields(record)...)
	if len(record.Learned) > 0 {
		fields = append(fields, "learned "+strings.Join(record.Learned, " "))
	}
	return strings.Join(compactFields(fields), "  ")
}

// laneOf is the machine the routing preference asked for and the machine that
// answered, in the one shape that says which question a difference between them
// answers: `auto→coreweave` when the router overrode the choice, the served
// name alone when it did not, and nothing at all on a call to an endpoint that
// is not a router. A row that named only one of the two would leave a reader
// unable to tell an override from an ordinary answer.
func laneOf(record calllog.Record) string {
	lane, served := strings.TrimSpace(record.Lane), strings.TrimSpace(record.Served)
	switch {
	case lane == "" && served == "":
		return ""
	case lane == "" || lane == served:
		return served
	case served == "":
		return lane
	default:
		return lane + "→" + served
	}
}

// deadlineOf is when a second request was going to be considered, from the
// belief held at send time. It is on the line because a hedge that fired is
// only half the story: the calls whose deadline was set and never reached are
// what say the deadline was set in the right place.
func deadlineOf(record calllog.Record) string {
	if record.DeadlineMs <= 0 {
		return ""
	}
	return "deadline " + shortDuration(time.Duration(record.DeadlineMs)*time.Millisecond)
}

// hedgeFields is what was done about a silence, and what it cost. Almost every
// row has none of it, which is why it sits at the end and prints nothing at all
// when nothing had to be done.
func hedgeFields(record calllog.Record) []string {
	var fields []string
	if action := strings.TrimSpace(record.Action); action != "" {
		fields = append(fields, "acted "+action)
	}
	if record.Arms > 1 {
		fields = append(fields, fmt.Sprintf("%d arms", record.Arms))
	}
	if record.Hedged {
		fields = append(fields, "hedged")
	}
	if record.WasteUSD > 0 {
		fields = append(fields, fmt.Sprintf("waste $%.4f", record.WasteUSD))
	}
	return fields
}

// arrowOf is how the call came back: the status when there was one, and the
// arrow alone when the request never reached an endpoint at all.
func arrowOf(record calllog.Record) string {
	if record.Status == 0 {
		return "→ no answer"
	}
	return fmt.Sprintf("→ %d", record.Status)
}

// inFlightFor is the one line a person watching a stuck run wants: this call
// went out and has not come back.
func inFlightFor(stamp string, now time.Time) string {
	began, err := time.Parse("2006-01-02T15:04:05.000Z07:00", stamp)
	if err != nil {
		return "⋯ in flight"
	}
	return "⋯ in flight " + shortDuration(now.Sub(began))
}

// clockOf is the time of day out of a full stamp — a log read on the day it was
// written does not need the date on every line.
func clockOf(stamp string) string {
	parsed, err := time.Parse("2006-01-02T15:04:05.000Z07:00", stamp)
	if err != nil {
		return stamp
	}
	return parsed.Format("15:04:05")
}

// shortDuration is a wall clock a person reads at a glance: sub-minute in
// tenths of a second, past that in minutes and seconds.
func shortDuration(elapsed time.Duration) string {
	if elapsed < 0 {
		elapsed = 0
	}
	if elapsed < time.Minute {
		return fmt.Sprintf("%.1fs", elapsed.Seconds())
	}
	minutes := int(elapsed / time.Minute)
	seconds := int((elapsed % time.Minute) / time.Second)
	return fmt.Sprintf("%dm%02ds", minutes, seconds)
}

func compactFields(fields []string) []string {
	kept := fields[:0]
	for _, field := range fields {
		if strings.TrimSpace(field) != "" {
			kept = append(kept, field)
		}
	}
	return kept
}

// recordDirFor names the folder the bodies live in: the one the ledger itself
// is in, which is `<state root>/logs` wherever the ledger has not been moved by
// hand. Keying it to the ledger rather than to the state root means a person
// who pointed AFORGE_CALL_LOG somewhere they could watch finds the bodies in
// the same place they are watching.
func recordDirFor(path string) string {
	if strings.TrimSpace(path) == "" {
		return home.Join(calllog.DirName)
	}
	return filepath.Dir(path)
}

// printCallBody prints what was recorded of one call: the trace first, because
// that is the full record of a run somebody deliberately switched on, and the
// failures folder second, because that is the one call that was kept without
// being asked for.
//
// Neither folder is written yet. The switch and the trace are #339 and the
// always-on keeping of a failure is #343, so today this reliably says there is
// nothing — which is a truthful answer and not a broken one, and becomes the
// right answer with no change here the moment either writer lands.
func printCallBody(output io.Writer, dir, id string) error {
	if body, found := findCallBody(dir, id); found {
		if !strings.HasSuffix(body, "\n") {
			body += "\n"
		}
		_, err := io.WriteString(output, body)
		return err
	}
	_, err := fmt.Fprintf(output, "no body recorded for %s\n", id)
	return err
}

func findCallBody(dir, id string) (string, bool) {
	// A call id is one path element and never a path. Anything else is somebody
	// who mistyped or a string built from somewhere it should not have been,
	// and either way it does not get to name a file outside the record folder.
	if id != filepath.Base(id) || id == "." || id == ".." || strings.ContainsAny(id, `/\`) {
		return "", false
	}
	matches, _ := filepath.Glob(filepath.Join(dir, traceDirName, "*", "calls", id+".json"))
	for _, match := range matches {
		if body, err := os.ReadFile(match); err == nil {
			return string(body), true
		}
	}
	if body, err := os.ReadFile(filepath.Join(dir, failuresDirName, id+".json")); err == nil {
		return string(body), true
	}
	return "", false
}

// followCallLog prints calls as they land, until the terminal is closed. It
// reopens rather than holding one descriptor, because the log rotates
// underneath it and a follower holding the old inode would go quiet forever
// without saying why.
func followCallLog(output io.Writer, path string, asJSON bool, filter logFilter, now func() time.Time) error {
	seen := 0
	if calls, err := readCallLog(path); err == nil {
		seen = len(calls)
	}
	for {
		time.Sleep(followInterval)
		calls, err := readCallLog(path)
		if err != nil {
			return err
		}
		if len(calls) < seen {
			// The file rotated or was truncated; start again from the top of
			// what is there rather than printing nothing for the rest of the run.
			seen = 0
		}
		fresh := calls[seen:]
		seen = len(calls)
		// Filtered here and not before the count, so that rows a person is not
		// watching still advance the mark rather than being offered again on
		// every tick.
		if err := writeCallLog(output, filter.keep(fresh), 0, asJSON, filter, now()); err != nil {
			return err
		}
	}
}
