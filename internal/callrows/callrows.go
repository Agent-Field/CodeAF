// Package callrows reads the model-call log this build always writes
// (`~/.aforge/logs/calls.jsonl`, internal/calllog) back into rows.
//
// IT IS ONE READER BECAUSE THERE IS ONE FILE. Two instruments now read it —
// cmd/aforge-census, which prints what went wrong, and cmd/aforge-replay, which
// asks what a different chooser would have done — and a second reader is a
// second answer to "was this attempt a failure", "which of these two rows is
// the same call" and "what does a torn last line mean". The census had all
// three in its own package and the first replay bench
// (`internal/lane/replay_bench_test.go`, deleted with this package's first
// caller) had its own copy of the first two, already disagreeing about whether
// a hedge's losing arm is a failure. So the reading lives here, once, and an
// instrument built next week starts from the same rows.
//
// WHAT IS NOT HERE is anything that turns a row into a JUDGEMENT. Which family
// a failure belongs to, which signature it shares with another, what a regret
// is — those are each instrument's own argument about the same bytes, and they
// stay where the argument is made.
package callrows

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/calllog"
)

// Row is one line of the log, decoded.
//
// IT EMBEDS THE RECORD THE WRITER WRITES rather than restating its fields, so
// that a field added to internal/calllog is readable here the day it lands and
// the two can never disagree about a name. What is added beside it is the one
// thing an embedding cannot carry: a field a wave RENAMED, which ten days of
// already-written rows still spell the old way.
type Row struct {
	calllog.Record
	// HazardCeilingLegacy is `deadline_ms` as builds before 2026-09-10 wrote it.
	// It was never a deadline — it is the moment the wait controller was going
	// to think about a second request — and it is read here under its old name
	// so a reading of a mixed log does not silently lose ten days of it.
	HazardCeilingLegacy int64 `json:"deadline_ms,omitempty"`

	// At is Time parsed, and zero when the row carried no readable stamp.
	At time.Time
	// Raw is the line as it arrived, kept only long enough for the checks that
	// are about the bytes rather than about the values — a float JSON has no
	// spelling for reaches a reader as a decode failure, not as a number.
	Raw string
}

// HazardCeiling is how long this attempt had before the wait controller would
// have acted on its silence, whichever build wrote the row.
func (r Row) HazardCeiling() int64 {
	if r.HazardCeilingMs > 0 {
		return r.HazardCeilingMs
	}
	return r.HazardCeilingLegacy
}

// Lost is a line that was written and could not be read back: a float JSON has
// no spelling for took the whole object with it. It is kept as a row so a
// reader can count it, and it is not an attempt — nothing about the call it was
// about survived.
func (r Row) Lost() bool { return r.Raw != "" && r.Time == "" }

// Finished reports whether this row is the end of an attempt rather than its
// beginning. A start row omits nothing and says so in one word; everything else
// is an outcome (internal/calllog's PhaseStart).
func (r Row) Finished() bool { return !r.Lost() && r.Phase != calllog.PhaseStart }

// Failed reports whether this attempt produced no usable answer — which is NOT
// the same as a status outside the 200s, and that is the whole point of reading
// the log at all. A refusal delivered inside an opened 200 stream is a 200 in
// every dashboard and a failure to the person waiting.
func (r Row) Failed() bool {
	return strings.TrimSpace(r.Error) != "" || (r.Status != 0 && r.Status != 200)
}

// Exhaust reports whether this row is the losing arm of a hedge this build won.
//
// IT IS NOT A FAILURE AND COUNTING IT AS ONE MEASURES OUR OWN HEDGING POLICY.
// The arm really was sent and really was cut off, so its row says `context
// canceled` and reads exactly like a caller walking away: 1,204 of 3,906 bad
// rows in the first census, the largest cause family in it, every one of them
// the price of a race that ended with an answer.
//
// The sentence is read as well as the field because the rows already in the log
// have only the sentence — internal/provider wrote the note for one wave while
// this field was landing, and a reading that could not see it would show the
// finding disappearing on the day of the rebuild rather than on the day the
// build changed.
func (r Row) Exhaust() bool {
	return r.Record.Exhaust || strings.Contains(strings.ToLower(r.Note), "lost the race")
}

// Read reads every line of a log into rows, in time order, skipping the ones
// that are not JSON at all.
//
// A LINE THAT WILL NOT DECODE IS COUNTED AND NEVER FATAL. A log is appended to
// by a live process; the last line of a file read while a call is landing can
// be half a line, and an instrument that refused to run over it would be an
// instrument nobody could use while the thing it measures is running.
func Read(path string) ([]Row, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("read the call log: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	// A row carrying bodies (AFORGE_CALL_LOG_BODIES) is hundreds of kilobytes,
	// and bufio's default 64 KiB would stop the scan at the first one.
	scanner.Buffer(make([]byte, 0, 64<<10), 8<<20)
	var rows []Row
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var decoded Row
		if err := json.Unmarshal([]byte(line), &decoded); err != nil {
			// The one decode failure that is a FINDING rather than a torn line
			// is a float JSON cannot spell, which is exactly what the belief
			// file has been choking on. It is kept as a row of its own so a
			// reader can count it.
			if Unspellable(line) {
				rows = append(rows, Row{Raw: line})
			}
			continue
		}
		decoded.Raw = line
		if stamp, err := time.Parse(TimeLayout, decoded.Time); err == nil {
			decoded.At = stamp
		}
		rows = append(rows, decoded)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read the call log: %w", err)
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].At.Before(rows[j].At) })
	return rows, nil
}

// TimeLayout is how internal/calllog spells Record.Time.
const TimeLayout = "2006-01-02T15:04:05.000Z07:00"

// Unspellable reports whether a line that would not decode carried a float JSON
// has no spelling for. Go's decoder refuses the whole object on one of these,
// so the row is lost — which is the defect internal/calllog's finite.go exists
// to prevent and an instrument exists to notice if it ever comes back.
func Unspellable(line string) bool {
	for _, token := range []string{":NaN", ":+Inf", ":-Inf", ":Inf", ": NaN", ": +Inf", ": -Inf"} {
		if strings.Contains(line, token) {
			return true
		}
	}
	return false
}

// SaidStatus is the status a router's own sentence carries — `API error (429):
// …` — and zero when it carries none.
//
// IT IS READ BECAUSE THE COLUMN CANNOT BE TRUSTED TO HOLD IT. A refusal
// delivered inside an opened 200 stream has a 200 in its status column and a 429
// in its words, and no upstream 5xx in ten days of this log ever reached the
// column at all. internal/provider's velocity.go states the same rule as a law
// for the live path: a refusal is acted on from what it SAYS and never from
// where it was read.
func SaidStatus(said string) int {
	const marker = "api error ("
	at := strings.Index(strings.ToLower(said), marker)
	if at < 0 {
		return 0
	}
	rest := said[at+len(marker):]
	end := strings.IndexByte(rest, ')')
	if end <= 0 || end > 3 {
		return 0
	}
	status := 0
	for _, digit := range rest[:end] {
		if digit < '0' || digit > '9' {
			return 0
		}
		status = status*10 + int(digit-'0')
	}
	return status
}
