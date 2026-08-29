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
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/calllog"
)

// defaultLogTail is how many calls `aforge logs` shows when nobody says. Forty
// is a few minutes of a busy run — enough to hold the call that went wrong and
// the ones around it, short enough to read in a terminal without scrolling.
const defaultLogTail = 40

// followInterval is how often --follow looks for more. A log is appended to by
// another process, so there is nothing to subscribe to; a quarter of a second
// is faster than a person can read and cheap enough to leave running.
const followInterval = 250 * time.Millisecond

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
	if err := flags.Parse(reorder(args, map[string]bool{"tail": true})); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("usage: aforge logs [--tail N] [--follow] [--path]")
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
	if _, err := fmt.Fprintln(output, path); err != nil {
		return err
	}
	records, err := readCallLog(path)
	if err != nil {
		return err
	}
	lines := renderCallLog(records, *tail, now())
	for _, line := range lines {
		if _, err := fmt.Fprintln(output, line); err != nil {
			return err
		}
	}
	if !*follow {
		return nil
	}
	return followCallLog(output, path, now)
}

// readCallLog reads every record in the file. A file that does not exist yet is
// no records and no error — nothing has called a model on this machine, which
// is a true thing to have found out rather than a fault.
//
// A line that will not parse is skipped rather than fatal: the last line of a
// log being written to at this instant may be half a line.
func readCallLog(path string) ([]calllog.Record, error) {
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

func decodeCallLog(source io.Reader) ([]calllog.Record, error) {
	var records []calllog.Record
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
		records = append(records, record)
	}
	if err := scanner.Err(); err != nil {
		return records, err
	}
	return records, nil
}

// renderCallLog turns records into the lines a person reads, newest last.
//
// THE PAIRING IS THE POINT. Every attempt writes a row as it goes out and a row
// as it comes back, sharing an id. A start whose end has arrived says nothing
// the end does not say better, so it is dropped; a start with no end is a call
// that is STILL RUNNING, and it is the one line this whole command exists for.
func renderCallLog(records []calllog.Record, tail int, now time.Time) []string {
	ended := make(map[string]bool, len(records))
	for _, record := range records {
		if record.Phase != calllog.PhaseStart && record.ID != "" {
			ended[record.ID] = true
		}
	}
	var lines []string
	for _, record := range records {
		if record.Phase == calllog.PhaseStart && (record.ID == "" || ended[record.ID]) {
			continue
		}
		lines = append(lines, callLogLine(record, now))
	}
	if tail > 0 && len(lines) > tail {
		lines = lines[len(lines)-tail:]
	}
	return lines
}

// callLogLine is one call on one line. Every segment is dropped when it is
// empty rather than printed as a zero or a dash: a call that never reached an
// endpoint has no duration and no finish reason, and a column of "0" would be a
// measurement nobody made.
func callLogLine(record calllog.Record, now time.Time) string {
	fields := []string{clockOf(record.Time), record.Tag, record.Model}
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
		return strings.Join(compactFields(append(fields, inFlightFor(record.Time, now))), "  ")
	}
	fields = append(fields, arrowOf(record))
	if record.Millis > 0 {
		fields = append(fields, shortDuration(time.Duration(record.Millis)*time.Millisecond))
	}
	fields = append(fields, record.Finish)
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
	if len(record.Learned) > 0 {
		fields = append(fields, "learned "+strings.Join(record.Learned, " "))
	}
	return strings.Join(compactFields(fields), "  ")
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

// followCallLog prints calls as they land, until the terminal is closed. It
// reopens rather than holding one descriptor, because the log rotates
// underneath it and a follower holding the old inode would go quiet forever
// without saying why.
func followCallLog(output io.Writer, path string, now func() time.Time) error {
	seen := 0
	if records, err := readCallLog(path); err == nil {
		seen = len(records)
	}
	for {
		time.Sleep(followInterval)
		records, err := readCallLog(path)
		if err != nil {
			return err
		}
		if len(records) < seen {
			// The file rotated or was truncated; start again from the top of
			// what is there rather than printing nothing for the rest of the run.
			seen = 0
		}
		fresh := records[seen:]
		seen = len(records)
		for _, line := range renderCallLog(fresh, 0, now()) {
			if _, err := fmt.Fprintln(output, line); err != nil {
				return err
			}
		}
	}
}
