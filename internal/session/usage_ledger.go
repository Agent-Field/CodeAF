package session

// THE USAGE LEDGER: WHAT THIS MACHINE SPENT, BY DAY, BY MODEL, ON WHAT.
//
// Every figure a spend page wants already existed and none of it was reachable.
// One `journalUsage` line is written per model call (sessionfile.go) and it
// carries the model, the role, the calls, the four token counts, the cost and a
// timestamp — model, cost and time coexisting on exactly one record in the whole
// program. But that record lives INSIDE one conversation's transcript, and the
// replay arm that reads it sums the numbers into a flat [Usage] and throws the
// model, the role and the timestamp away. So "what did opus cost me this month"
// could only be answered by opening every transcript on the machine — the exact
// read `Meta.SpentUSD` was invented to avoid — and "what did I spend on Tuesday"
// could not be answered at all, because a conversation's own spend is a lifetime
// scalar with no day in it.
//
// This is the second write of the same fact, into one machine-wide append-only
// file, keyed by day. It is GLOBAL where a transcript is per-conversation, for
// artifacts.jsonl's reason: "what has this machine been spending on" is a
// cross-project question, and a per-project answer to it is not an answer.
//
// ── FOUR RULES ──
//
//   - IT IS WRITTEN WHERE THE CALL WAS MADE, AND A FOLD IS NOT A CALL. A task
//     node journals its own turns and then its whole tally is folded into the
//     conversation that spawned it (task_run.go's [Agent.foldTaskUsage]), which
//     is right for a session's own books and would be double counting here. The
//     fold has its own door ([Agent.addFoldedUsage]) that writes no ledger line,
//     and this file's totals are therefore each call once.
//   - IT NEVER BLOCKS A TURN. One `O_APPEND` write of a ~200-byte line, no
//     flush, no fsync, every error dropped — the same bargain [RecordArtifact]
//     makes, and the same one the turn is already paying one line earlier when
//     it appends the identical fact to its own transcript.
//   - A LINE THAT SPENT NOTHING IS NOT WRITTEN. The emptiness law applied to a
//     file: an instantly-cancelled turn and a zero-token seal leave no row, so a
//     day with no line in it is a day nothing was spent, rather than a day whose
//     rows all say zero.
//   - IT IS READ THROUGH A CACHE THAT READS THE TAIL. Home's clock beats every
//     three seconds and this file grows by a line per call, so a reader that
//     re-parsed the whole file whenever it changed would re-parse it after every
//     turn forever. [UsageCache] keeps what it has parsed and reads only what
//     was appended since ([UsageCache.Read]).
//
// WHAT IT DOES NOT HOLD, said plainly, because a page must not imply otherwise:
// the four-way token split (input and output are kept, the cache share is not —
// the journal this line's session id names has it), which of the five router
// slots a call ran under (nothing records that anywhere; `Role` here is
// internal/roles' auxiliary name, and only three auxiliary calls in the whole
// program name themselves at all), and any title for a task or a standing item —
// only their ids, which a page joins against the task index and the standing
// store it is already reading.

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/home"
)

// UsageLedgerName is the file, under the v3 home directory
// (~/.aforge/v3/usage.jsonl). It is a name beside a path function rather than a
// literal at every call site, for [ArtifactsIndexName]'s reason: two spellings
// of one path are two ledgers with half a person's spending in each.
const UsageLedgerName = "usage.jsonl"

// usageDayLayout is the LOCAL calendar day a line belongs to, spelled exactly
// as internal/standing's daily ledger spells its own file names
// (standing.go's LedgerPath). A day is the unit a person asks about — "what did
// Tuesday cost" — and it is local because their Tuesday is, so a line carries
// the day it was made in beside the instant it was made at, and no reader has
// to re-derive a calendar from a timestamp in some other zone.
const usageDayLayout = "2006-01-02"

// UsageLedgerPath is this machine's ledger, resolved through internal/home so
// AFORGE_HOME moves it with everything else (Decision 26 — one home, one seam).
// It is the fallback the engine writes to; a caller with a path of its own —
// a test, a second brain on one laptop — hands one to [RecordUsage] instead.
func UsageLedgerPath() string { return home.Join("v3", UsageLedgerName) }

// UsageLine is one model call as the ledger remembers it.
//
// Every field is a fact somebody asked for by name on the spend page, and there
// is nothing here that is not: which day, which model, what for, how many calls,
// how many tokens, how much money, and the three ids that say what the money was
// spent ON — a conversation, a piece of work, a standing promise.
type UsageLine struct {
	// At is the instant the call was journaled, RFC3339 with nanoseconds.
	At time.Time `json:"at"`
	// Day is At's LOCAL calendar day, "2006-01-02". It is written down rather
	// than derived on read because the reader may be a different process in a
	// different zone, and a day that moves depending on who is asking is not a
	// day (see [usageDayLayout]).
	Day string `json:"day"`
	// Model is what answered, and empty where nobody said. It is the id the
	// provider knows, not a pretty name: a page that wants "opus 4.1" makes that
	// word itself, from one place, the way every other surface does.
	Model string `json:"model,omitempty"`
	// Role is WHAT the call was for — "title", "taskname", "intake" — on the
	// three auxiliary calls in the program that name themselves, and empty on
	// every other line including every turn's own seal. It is internal/roles'
	// vocabulary and NOT the five router slots (execution, conversation,
	// verification, naming, planning): nothing in the program records which slot
	// a call ran under, and a page that labelled this column with those words
	// would be inventing the join.
	Role string `json:"role,omitempty"`
	// Calls is how many provider requests this line covers — one for an
	// ordinary call, and a whole turn's worth for a turn's seal.
	Calls int `json:"calls,omitempty"`
	// Input and Output are the tokens. The cache split is deliberately not here:
	// this file answers "how much and on what", and the four-way breakdown with
	// the cached share in it is in the journal the session id names.
	Input  int `json:"in,omitempty"`
	Output int `json:"out,omitempty"`
	// USD is what it cost, and zero means nobody could price it rather than that
	// it was free — the same reading [TaskIndexEntry.Cost] has.
	USD float64 `json:"usd,omitempty"`
	// Session is the 16-hex id of the conversation the call was made in. For a
	// piece of work it is the NODE's own journal id and not the conversation
	// that asked for it, which is why Task sits beside it: the pair is what
	// identifies where the money went.
	Session string `json:"session,omitempty"`
	// Task is the id of the node this call was made inside, and empty in a
	// conversation. It is the node's id within its session, exactly as
	// [TaskIndexEntry.ID] is, so the two join.
	Task string `json:"task,omitempty"`
	// Standing is the id of the standing item whose firing made this call, and
	// empty everywhere else. A firing is InTask and may also carry a Task id;
	// [UsageBySubject] prefers this one, because a person recognises the promise
	// they made long before they recognise the run it spawned.
	Standing string `json:"standing,omitempty"`
	// Workspace is the project root the call was made against — what a page
	// groups by, and empty for a conversation held nowhere in particular.
	Workspace string `json:"workspace,omitempty"`
}

// usageMu serializes this process's appends; two processes are serialized by
// O_APPEND, which is what makes an append-only file the right shape here
// ([RecordArtifact] says the same).
var usageMu sync.Mutex

// RecordUsage writes one line. Every failure is silence, for
// [RecordArtifact]'s reason: the caller has just finished a piece of a person's
// turn, and there is nothing it could usefully do with the news that a spending
// record could not be written — least of all tell them about it mid-sentence.
//
// A LINE THAT SPENT NOTHING IS NOT WRITTEN, which is [sessionFile.appendUsage]'s
// own guard kept here as well rather than trusted: this file is appended to from
// more than one door over its life, and a zero row in a spending ledger is worse
// than no row — it is a day that looks measured and was not.
func RecordUsage(path string, line UsageLine) {
	if strings.TrimSpace(path) == "" {
		return
	}
	if line.Input == 0 && line.Output == 0 && line.USD == 0 {
		return
	}
	if line.At.IsZero() {
		line.At = time.Now()
	}
	if strings.TrimSpace(line.Day) == "" {
		line.Day = line.At.Local().Format(usageDayLayout)
	}
	payload, err := json.Marshal(line)
	if err != nil {
		return
	}
	usageMu.Lock()
	defer usageMu.Unlock()
	if directory := filepath.Dir(path); directory != "" && directory != "." {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			return
		}
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	defer file.Close()
	// ONE write, so O_APPEND's atomic offset covers the whole row — which is
	// also what lets [UsageCache] trust that the bytes before the last newline
	// are whole lines.
	_, _ = file.Write(append(payload, '\n'))
}

// ReadUsage reads the ledger, OLDEST FIRST, keeping only lines at or after
// `since`. A zero `since` keeps everything.
//
// The order is the file's own and not reversed, because every caller of this is
// an aggregation over a window rather than a list somebody scrolls: a series
// wants its days in the order days happen.
//
// IT TOLERATES EVERYTHING A LEDGER CAN BE. A file that is not there is a machine
// that has spent nothing and answers nil with no error — the first run of a new
// install must not be an error path. A line that does not parse is skipped, for
// the task index's reason: two processes appending can in the limit interleave,
// and one bad line must cost one call's record and not the page. A real failure
// to OPEN a file that exists is returned, because that a caller can say
// something about.
func ReadUsage(path string, since time.Time) ([]UsageLine, error) {
	if strings.TrimSpace(path) == "" {
		return nil, nil
	}
	file, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()
	lines, _, err := scanUsage(file, since)
	return lines, err
}

// usageLineBytes bounds one line the reader will hold in memory. A usage row is
// a hundred-odd bytes of numbers and ids; anything past this is a file that has
// been concatenated with something else, and reading it into memory is not a
// service to anybody. Such a line is SKIPPED but still counted as consumed, so
// one absurd row costs its own record and never the offset.
const usageLineBytes = 64 * 1024

// scanUsage reads lines from r and answers them with the count of BYTES it
// consumed in whole lines — which is what makes the tail read in
// [UsageCache.Read] safe. A partial last line (a write caught mid-flight) is not
// counted, so the next read starts at its beginning and reads it whole.
//
// IT COUNTS BYTES AND NOT TOKENS, which is why it reads to the newline itself
// rather than through a bufio.Scanner. A scanner hands back the line with its
// terminator — and a stray carriage return — already stripped, so the caller can
// only GUESS at how much of the file it just consumed; guess wrong by one byte
// and the next tail read starts mid-row and silently loses everything after it.
// The one thing this offset must be is exact.
func scanUsage(reader io.Reader, since time.Time) ([]UsageLine, int64, error) {
	var lines []UsageLine
	var consumed int64
	buffered := bufio.NewReaderSize(reader, 32*1024)
	for {
		raw, err := buffered.ReadString('\n')
		if !strings.HasSuffix(raw, "\n") {
			// The tail of the file with no terminator on it: either the file ends
			// without one, or a writer is mid-append. Either way it is not a whole
			// line, so it is neither parsed nor counted.
			if err != nil && !errors.Is(err, io.EOF) {
				return lines, consumed, err
			}
			return lines, consumed, nil
		}
		consumed += int64(len(raw))
		if len(raw) > usageLineBytes {
			continue
		}
		var line UsageLine
		if json.Unmarshal([]byte(raw), &line) != nil {
			continue
		}
		if line.At.IsZero() {
			continue
		}
		if !since.IsZero() && line.At.Before(since) {
			continue
		}
		lines = append(lines, line)
	}
}

// recordUsageLine is the engine's one door onto the ledger: the figures a
// journal line already carries, plus the four ids that say whose they are.
//
// IT IS CALLED FROM THE TWO PLACES THAT JOURNAL A COST and from nowhere else —
// the turn's seal ([Agent.sealTurn]) and an auxiliary call
// ([Agent.addAuxiliaryUsageAs]) — so the ledger and the transcripts can never
// come to hold different money. A fold goes through a door of its own and does
// not reach here ([Agent.addFoldedUsage] says why).
//
// The write is outside a.mu for [Agent.sealTurn]'s reason: it is a file append,
// and holding the agent's lock across one would put every reader of the
// session's totals behind a disk.
func (a *Agent) recordUsageLine(used Usage, model, role string) {
	if used.Input == 0 && used.Output == 0 && used.CostUSD == 0 {
		return
	}
	path := strings.TrimSpace(a.config.usageLedger)
	if path == "" {
		path = UsageLedgerPath()
	}
	a.mu.Lock()
	session := a.sessionID()
	a.mu.Unlock()
	now := time.Now()
	RecordUsage(path, UsageLine{
		At:     now,
		Day:    now.Local().Format(usageDayLayout),
		Model:  strings.TrimSpace(model),
		Role:   strings.TrimSpace(role),
		Calls:  used.Calls,
		Input:  used.Input,
		Output: used.Output,
		USD:    used.CostUSD,

		Session: session,
		// The node this agent IS, and nothing for a conversation — the same
		// figure [TaskNotice.Parent] is registered under, spelled the way
		// [TaskIndexEntry.ID] spells it so the two join.
		Task:      usageTaskID(a.config.taskID),
		Standing:  strings.TrimSpace(a.config.standingItemID),
		Workspace: strings.TrimSpace(a.config.Workspace),
	})
}

// usageTaskID spells a node's id the way the task index spells it, and answers
// nothing at all for a conversation — where "0" would be a node that does not
// exist rather than the absence of one.
func usageTaskID(id uint64) string {
	if id == 0 {
		return ""
	}
	return strconv.FormatUint(id, 10)
}

// UsageCache is the ledger read the way home may call it: as often as it likes.
//
// THE PROBLEM IT SOLVES IS NOT THE FIRST READ, IT IS THE THOUSANDTH. Home's
// clock beats every three seconds, and this file grows by a line on every model
// call — so a cache keyed on "has the file changed" would find that it HAS,
// after every single turn, and re-parse a year of spending to learn about one
// new line. So the cache is keyed on how far it has already read: an unchanged
// file answers from memory, a GROWN file is read from where the last read
// stopped, and only a file that shrank — truncated, rotated, replaced — is read
// again from the beginning.
//
// It holds every line it has ever parsed, which is the one thing that makes the
// tail read possible. That is bounded by [usageCacheLines]: past it the oldest
// are dropped and the cache says so ([UsageCache.Full]), because a page drawing
// a fortnight must not be the reason a long-lived window grows without end.
//
// A zero UsageCache is ready to use. It is NOT safe for concurrent use: it is
// held by one surface and read on that surface's own goroutine, which is where
// every reader of it lives.
type UsageCache struct {
	// Path is the ledger this cache is over. Empty means [UsageLedgerPath].
	Path string

	lines []UsageLine
	// read is how many bytes of whole lines have been parsed, and size/mod are
	// the file as it stood when that was true.
	read int64
	size int64
	mod  time.Time
	// loaded says a first read has happened, so that a genuinely empty ledger is
	// distinguishable from one nobody has looked at yet.
	loaded bool
	full   bool
}

// usageCacheLines bounds what one cache holds in memory. Two hundred thousand
// lines is several years of heavy use at a few hundred bytes each — tens of
// megabytes at the very top, and a bound that exists so an always-open window
// cannot grow without one.
const usageCacheLines = 200_000

// Read answers every line at or after `since`, re-reading the file only where
// it has actually changed.
//
// Errors are answered BESIDE the lines and never instead of them, for
// [scanUsage]'s reason. A caller that only wants the figures may ignore the
// error entirely; a caller that wants to say "some of this could not be read"
// has it.
func (c *UsageCache) Read(since time.Time) ([]UsageLine, error) {
	path := strings.TrimSpace(c.Path)
	if path == "" {
		path = UsageLedgerPath()
	}
	info, err := os.Stat(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		// A machine that has spent nothing. The cache remembers that it looked,
		// so a ledger that appears later is picked up on the next beat.
		c.lines, c.read, c.size, c.mod, c.loaded = nil, 0, 0, time.Time{}, true
		return nil, nil
	case err != nil:
		return c.since(since), err
	}
	if c.loaded && info.Size() == c.size && info.ModTime().Equal(c.mod) {
		return c.since(since), nil
	}
	if !c.loaded || info.Size() < c.read {
		// Shorter than what we have already parsed: this is a different file
		// wearing the same name, and nothing we hold is about it.
		c.lines, c.read = nil, 0
	}
	file, err := os.Open(path)
	if err != nil {
		return c.since(since), err
	}
	defer file.Close()
	if c.read > 0 {
		if _, err := file.Seek(c.read, io.SeekStart); err != nil {
			// A seek that fails leaves the cache exactly as it was rather than
			// half-advanced; the next beat tries again from the same offset.
			return c.since(since), err
		}
	}
	// The TAIL is read with no floor: a cache that filtered on the way in could
	// never answer a question about a window older than the first one it was
	// asked. The floor is applied on the way out, over what is held.
	fresh, consumed, scanErr := scanUsage(file, time.Time{})
	c.lines = append(c.lines, fresh...)
	c.read += consumed
	c.size, c.mod, c.loaded = info.Size(), info.ModTime(), true
	if len(c.lines) > usageCacheLines {
		c.lines = c.lines[len(c.lines)-usageCacheLines:]
		c.full = true
	}
	return c.since(since), scanErr
}

// Full says the cache has dropped its oldest lines to stay inside its bound, so
// a total taken from it is a total over what it still holds. A page quoting an
// all-time figure has to say so; a page drawing a fortnight never has to care.
func (c *UsageCache) Full() bool { return c.full }

// since is the held lines from a floor, sharing the backing array where the
// whole slice is wanted — the ordinary case, since the file is already ordered.
func (c *UsageCache) since(floor time.Time) []UsageLine {
	if floor.IsZero() {
		return c.lines
	}
	// The file is written in time order, so the floor is a prefix cut rather
	// than a filter — except for the pathological case of a clock that moved
	// backwards, which costs one line's inclusion and nothing else.
	for i, line := range c.lines {
		if !line.At.Before(floor) {
			return c.lines[i:]
		}
	}
	return nil
}
