package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/calllog"
)

// row is one line of the log, decoded.
//
// IT EMBEDS THE RECORD THE WRITER WRITES rather than restating its fields, so
// that a field added to internal/calllog is readable here the day it lands and
// the two can never disagree about a name. What is added beside it is the one
// thing an embedding cannot carry: a field this wave RENAMED, which ten days of
// already-written rows still spell the old way.
type row struct {
	calllog.Record
	// HazardCeilingLegacy is `deadline_ms` as builds before this wave wrote it.
	// It was never a deadline — it is the moment the wait controller was going
	// to think about a second request — and it is read here under its old name
	// so a census run over a mixed log does not silently lose ten days of it.
	HazardCeilingLegacy int64 `json:"deadline_ms,omitempty"`

	// at is Time parsed, and zero when the row carried no readable stamp.
	at time.Time
	// raw is the line as it arrived, kept only long enough for the checks that
	// are about the bytes rather than about the values — a float JSON has no
	// spelling for reaches this program as a decode failure, not as a number.
	raw string
}

// hazardCeiling is how long this attempt had before the wait controller would
// have acted on its silence, whichever build wrote the row.
func (r row) hazardCeiling() int64 {
	if r.HazardCeilingMs > 0 {
		return r.HazardCeilingMs
	}
	return r.HazardCeilingLegacy
}

// lost is a line that was written and could not be read back: a float JSON has
// no spelling for took the whole object with it. It is kept as a row so the
// checks can count it, and it is not an attempt — nothing about the call it was
// about survived.
func (r row) lost() bool { return r.raw != "" && r.Time == "" }

// finished reports whether this row is the end of an attempt rather than its
// beginning. A start row omits nothing and says so in one word; everything else
// is an outcome (internal/calllog's PhaseStart).
func (r row) finished() bool { return !r.lost() && r.Phase != calllog.PhaseStart }

// failed reports whether this attempt produced no usable answer — which is NOT
// the same as a status outside the 200s, and that is the whole point of the
// census. A refusal delivered inside an opened 200 stream is a failure the
// status column cannot see.
func (r row) failed() bool {
	return strings.TrimSpace(r.Error) != "" || (r.Status != 0 && r.Status != 200)
}

// readLog reads every line of a log into rows, skipping the ones that are not
// JSON at all.
//
// A LINE THAT WILL NOT DECODE IS COUNTED AND NEVER FATAL. A log is appended to
// by a live process; the last line of a file read while a call is landing can
// be half a line, and a census that refused to run over it would be an
// instrument nobody could use while the thing it measures is running.
func readLog(path string) ([]row, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("read the call log: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	// A row carrying bodies (AFORGE_CALL_LOG_BODIES) is hundreds of kilobytes,
	// and bufio's default 64 KiB would stop the scan at the first one.
	scanner.Buffer(make([]byte, 0, 64<<10), 8<<20)
	var rows []row
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var decoded row
		if err := json.Unmarshal([]byte(line), &decoded); err != nil {
			// The one decode failure that is a FINDING rather than a torn line
			// is a float JSON cannot spell, which is exactly what the belief
			// file has been choking on. It is kept as a row of its own so the
			// surprising-checks section can count it.
			if unspellable(line) {
				rows = append(rows, row{raw: line})
			}
			continue
		}
		decoded.raw = line
		if stamp, err := time.Parse(timeLayout, decoded.Time); err == nil {
			decoded.at = stamp
		}
		rows = append(rows, decoded)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read the call log: %w", err)
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].at.Before(rows[j].at) })
	return rows, nil
}

// timeLayout is how internal/calllog spells Record.Time.
const timeLayout = "2006-01-02T15:04:05.000Z07:00"

// unspellable reports whether a line that would not decode carried a float JSON
// has no spelling for. Go's decoder refuses the whole object on one of these,
// so the row is lost — which is the defect internal/calllog's finite.go exists
// to prevent and this census exists to notice if it ever comes back.
func unspellable(line string) bool {
	for _, token := range []string{":NaN", ":+Inf", ":-Inf", ":Inf", ": NaN", ": +Inf", ": -Inf"} {
		if strings.Contains(line, token) {
			return true
		}
	}
	return false
}

// ── WHAT A FINISH WAS ───────────────────────────────────────────────────────

// statusClass is the six-way reading of a finished attempt the design's §1
// table is built on. It is six and not two because the interesting distinction
// is inside the 200s: a refusal delivered after the headers landed is a 200 in
// every dashboard and a failure to the person waiting.
type statusClass string

const (
	classClean     statusClass = "200 clean"
	classInStream  statusClass = "200 + error"
	classPaced     statusClass = "429"
	classTransport statusClass = "transport"
	classRouting   statusClass = "404"
	classMalformed statusClass = "400"
	classOther     statusClass = "other status"
	classNote      statusClass = "note only"
)

// classesInOrder is the order the table prints, worst-understood last.
var classesInOrder = []statusClass{
	classClean, classInStream, classPaced, classTransport,
	classRouting, classMalformed, classOther, classNote,
}

func (r row) statusClass() statusClass {
	failing := strings.TrimSpace(r.Error) != ""
	switch {
	case r.Status == 200 && !failing:
		return classClean
	case r.Status == 200:
		return classInStream
	case r.Status == 429:
		return classPaced
	case r.Status == 404:
		return classRouting
	case r.Status == 400:
		return classMalformed
	case r.Status == 0 && failing:
		return classTransport
	case r.Status == 0:
		return classNote
	default:
		return classOther
	}
}

// ── WHY IT FAILED ───────────────────────────────────────────────────────────

// causeFamily is the reading that matters most in the design: whose fault the
// failure was. It is the difference between a provider problem and a budget
// this build set for itself and then enforced against a healthy stream.
type causeFamily string

const (
	causeCanceled  causeFamily = "self-inflicted: canceled"
	causeDeadline  causeFamily = "self-inflicted: deadline"
	causeWall      causeFamily = "self-inflicted: walls"
	causePaced     causeFamily = "provider: 429"
	causeNetwork   causeFamily = "network / this laptop"
	causeRouting   causeFamily = "account / routing 404"
	causeUpstream  causeFamily = "provider 5xx in a 200 stream"
	causeMalformed causeFamily = "provider 400"
	causeUnread    causeFamily = "unclassified"
)

var familiesInOrder = []causeFamily{
	causeCanceled, causePaced, causeDeadline, causeNetwork,
	causeRouting, causeWall, causeUpstream, causeMalformed, causeUnread,
}

// networkPhrases are what a failure on this laptop's own wire says. They are
// matched on the sentence rather than on the status because the commonest of
// them — a connection reset halfway through a reply — arrives INSIDE an opened
// 200 and has no status of its own to be read from.
var networkPhrases = []string{
	"no such host", "no route to host", "connection reset by peer",
	"can't assign requested address", "broken pipe", "connection refused",
	"network is unreachable", "i/o timeout", "operation timed out", "tls handshake",
	"http2:", "cannot connect to host", "eof",
}

// wallPhrases are this build's own bounds, spelled the way the guard spells
// them to a person (internal/provider's streamguard.go).
var wallPhrases = []string{
	"was cut", "went quiet", "nothing came back", "ran past",
}

// cause is the family this failure belongs to, and false for a row that did not
// fail.
//
// THE ORDER OF THE READINGS IS THE FINDING. A cancellation names itself even
// when it arrives wrapped in a decode error, and a refusal names its own status
// inside a 200 — so the sentence is read before the status column, always, for
// the reason internal/provider's velocity.go states as a law: a refusal is
// acted on from what it says and never from where it was read.
func (r row) cause() (causeFamily, bool) {
	if !r.failed() {
		return "", false
	}
	said := strings.ToLower(r.Error)
	switch {
	case strings.Contains(said, "context canceled"):
		return causeCanceled, true
	case strings.Contains(said, "context deadline exceeded"):
		return causeDeadline, true
	case r.Status == 429 || apiErrorStatus(said) == 429:
		return causePaced, true
	case r.Status == 404 || apiErrorStatus(said) == 404:
		return causeRouting, true
	case r.Status == 400 || apiErrorStatus(said) == 400:
		return causeMalformed, true
	case containsAny(said, networkPhrases):
		return causeNetwork, true
	case apiErrorStatus(said) >= 500:
		return causeUpstream, true
	case containsAny(said, wallPhrases):
		return causeWall, true
	case r.Status == 0:
		return causeNetwork, true
	default:
		return causeUnread, true
	}
}

// apiErrorStatus is the status a router's own sentence carries — `API error
// (429): …` — and zero when it carries none. It is read from the sentence
// because that is the only place a 502 ever appears: no upstream 5xx in ten
// days of this log reached the status column.
func apiErrorStatus(said string) int {
	const marker = "api error ("
	at := strings.Index(said, marker)
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

func containsAny(said string, phrases []string) bool {
	for _, phrase := range phrases {
		if strings.Contains(said, phrase) {
			return true
		}
	}
	return false
}

// nonFinite reports whether a decoded float is a figure no second can hold. The
// writer takes +Inf and NaN off a row before it is written (internal/calllog's
// finite.go), so a decoded row can only ever carry a FINITE absurdity — which
// is the shape the 1.99e+146 in the first census had, and is why the check is
// a magnitude rather than math.IsInf alone.
func nonFinite(value float64) bool {
	return math.IsInf(value, 0) || math.IsNaN(value)
}

// absurdSeconds is where a figure in seconds stops being a measurement. A wait
// or a cost of more than a million seconds — eleven days — is arithmetic that
// went wrong, not a call somebody sat through.
const absurdSeconds = 1e6
