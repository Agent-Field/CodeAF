package lane

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/filelock"
)

// ── WHY THE FILE GREW A SECOND HALF ─────────────────────────────────────────
//
// The belief file used to be the whole story: write the set, merge last-writer
// -wins per lane, done. THE HIERARCHY MAKES THAT ACTIVELY WRONG. Two processes
// each hold μ, a[lane] and b[model] for the SAME subjects and each has folded
// different evidence into them, so "newer wins" discards one process's
// afternoon because the other happened to save second — and the shared levels
// are exactly the ones that make a cold start work.
//
// Observations, unlike beliefs, MERGE BY BEING REPLAYED. So the store becomes
// two files:
//
//	lanes.json   the compacted state — the chains, the beliefs, the moment
//	lanes.log    one line of NDJSON per observation since that moment
//
// APPEND is one write under O_APPEND with a SHARED advisory lock, and it IS on
// the send path — an answer is folded in the moment it finishes. Shared,
// because two appenders do not conflict — O_APPEND is what keeps their bytes
// apart — and the lock is there only to keep them out of a compaction, which is
// why it is asked for once and never waited on.
//
// LOAD reads the state and replays the whole journal into it in time order.
// Observations commute in this filter to first order and exactly in time order,
// so two processes reading the same two files converge on the same belief.
//
// COMPACT takes the EXCLUSIVE lock, which no appender can be inside, replays,
// writes the state through a temporary file and a rename, and truncates the
// journal to zero. It happens ON THE WRITER GOROUTINE ALONE ([ledger.Persist])
// and never in front of somebody's first token — which is issue #264, and used
// to be untrue of the first observation of every process.
//
// A MISSING JOURNAL IS AN EMPTY ONE. A LINE THAT WILL NOT PARSE IS SKIPPED AND
// COUNTED — a half-written record from a machine that lost power is one
// observation lost, and refusing to read the file around it would be an
// afternoon lost.

// journalLimit is how many records may pile up before a compaction is worth
// doing. It bounds the replay a cold process pays for: five hundred records is
// a few milliseconds of arithmetic and about a hundred kilobytes.
const journalLimit = 512

// thought is one whole thinking phase, timed. It is keyed on the model and the
// effort rung it was asked at because that is what the duration is a property
// of; a lane can only make the same thought arrive faster.
type thought struct {
	Model string        `json:"model"`
	Rung  string        `json:"rung,omitempty"`
	Took  time.Duration `json:"took"`
	At    time.Time     `json:"at,omitzero"`
}

// record is one observation as it is appended.
//
// EXACTLY ONE PAYLOAD IS SET, and the shapes are the package's own rather than
// a flattened copy of them: a journal with its own spelling of a sighting is a
// journal that disagrees with the ledger the first time a field is added.
type record struct {
	At     time.Time `json:"at"`
	Sight  *Sighting `json:"sight,omitempty"`
	Out    *Outcome  `json:"out,omitempty"`
	Row    *Row      `json:"row,omitempty"`
	Weight float64   `json:"k,omitempty"`
	Think  *thought  `json:"think,omitempty"`
	Work   *Workload `json:"work,omitempty"`
}

// ok reports whether the record says one thing. A line naming no observation,
// or two, is a line this build did not write and cannot act on.
func (r record) ok() bool {
	said := 0
	for _, set := range []bool{r.Sight != nil, r.Out != nil, r.Row != nil, r.Think != nil, r.Work != nil} {
		if set {
			said++
		}
	}
	return said == 1
}

// journalPath is the observation log beside a state file: `lanes.json` and
// `lanes.log` in one directory, so that the pair is obvious in a listing and a
// person clearing one knows what the other is.
func journalPath(state string) string {
	if state == "" {
		return ""
	}
	trimmed := strings.TrimSuffix(state, filepath.Ext(state))
	return trimmed + ".log"
}

// journal is the append-only observation log.
//
// It holds no handle between calls: an append is one open, one write and one
// close, which is what makes it safe for a process to be killed at any point
// and what keeps a long-running session from pinning an inode a compaction has
// replaced.
type journal struct{ path string }

// add appends one observation.
//
// A SHARED LOCK, ASKED FOR ONCE AND NEVER WAITED ON. This is the one thing in
// this package that a send path really does reach — a sighting is folded in the
// moment an answer finishes — so it may not wait on anything (store.go,
// "nothing here waits on a lock"). The lock is only here to keep the write out
// of a compaction's truncation; without it the write still happens, which is
// what a lone process has always done and what a person on a network home is
// entitled to, and the worst it can cost is this one observation.
func (j journal) add(entry record) error {
	if j.path == "" || !entry.ok() {
		return nil
	}
	line, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(j.path), 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(j.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	if filelock.Lock(file, false, true) == nil {
		defer filelock.Unlock(file)
	}
	_, err = file.Write(append(line, '\n'))
	return err
}

// read is every observation the journal holds, in time order, with the number
// of lines that would not parse.
func (j journal) read() ([]record, int) {
	if j.path == "" {
		return nil, 0
	}
	file, err := os.Open(j.path)
	if err != nil {
		return nil, 0
	}
	defer file.Close()
	if filelock.Lock(file, false, true) == nil {
		defer filelock.Unlock(file)
	}
	return decodeRecords(file)
}

// decodeRecords parses NDJSON into observations in time order, counting what it
// had to skip.
//
// THE ORDER IS THE WHOLE POINT. A filter folds observations commutatively only
// to first order; in time order it is exact, so replaying a journal in the
// order it was written is what makes two processes agree rather than nearly
// agree. [sort.SliceStable] keeps two records of one moment in the order they
// were appended, which is the order they happened in.
func decodeRecords(from *os.File) ([]record, int) {
	var kept []record
	skipped := 0
	lines := bufio.NewScanner(from)
	lines.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for lines.Scan() {
		line := bytes.TrimSpace(lines.Bytes())
		if len(line) == 0 {
			continue
		}
		var entry record
		if err := json.Unmarshal(line, &entry); err != nil || !entry.ok() {
			skipped++
			continue
		}
		kept = append(kept, entry)
	}
	if lines.Err() != nil {
		skipped++
	}
	sort.SliceStable(kept, func(i, j int) bool { return kept[i].At.Before(kept[j].At) })
	return kept, skipped
}

// drain reads the journal under the EXCLUSIVE lock — which no appender can be
// inside — hands the records to fn, and empties the file only when fn says the
// state they were folded into has been written.
//
// The truncation is [os.File.Truncate] on the handle the lock is held through,
// so an appender that opens the file the instant the lock is released sees a
// file of zero bytes rather than a file that is about to become one. The state
// is written by fn through a rename, which is why its own lock is a third file:
// a lock on an inode the next write replaces is a lock two processes each think
// they hold.
//
// A BUSY LOCK ANSWERS [errLockBusy] AND TRUNCATES NOTHING. This is the one lock
// in the pair that may not be skipped: emptying the file while an appender is
// inside it is how an observation disappears, and a compaction that did not
// happen is worth nothing at all next to that.
func (j journal) drain(fold func([]record, int) bool) error {
	if j.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(j.path), 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(j.path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	switch err := take(file, true); {
	case err == nil:
		defer filelock.Unlock(file)
	case errors.Is(err, errLockBusy):
		return errLockBusy
	}
	records, skipped := decodeRecords(file)
	if fold(records, skipped) {
		_ = file.Truncate(0)
	}
	return nil
}
