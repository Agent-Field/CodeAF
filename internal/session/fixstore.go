package session

// The error→fix sidecar: what went wrong here before, and what made it go away.
//
// A harness watches every tool call fail and then throws the failure away. The
// next session meets the same error — the same regex flavour ugrep will not
// take, the same build tag, the same binary macOS kills on sight — and pays the
// same three calls to rediscover the same one-line answer. Nothing about that
// needs a model to fix it: the error text is a string, the command that worked
// afterwards is a string, and the pairing is a lookup.
//
// So this file is a lookup, and deliberately nothing more. There is NO MODEL
// CALL ANYWHERE ON THE WRITE PATH — a fix is recorded because a call failed and
// the next call on the same tool succeeded, which is an observation and not a
// judgement — and no tool on the belt (tools.go) reaches any of it. The model is
// never given a verb for this; it is handed ONE LINE appended to a failed tool
// result, and the appending happens in the harness at [Agent.executeTool]'s
// chokepoint (fixrecall.go). Retrieval happens ONLY ON AN ERROR. A store
// consulted before a call would be a store shaping work that was going fine.
//
// ── THE NORMALIZATION LAW ──
//
// OVER-NORMALIZE AND DIFFERENT ERRORS COLLIDE; UNDER-NORMALIZE AND RECURRENCE IS
// INVISIBLE. Both failures are silent, and they fail in opposite directions: a
// signature stripped down to "command exited with code N" matches every failure
// this machine will ever have and offers the wrong patch to all of them, while a
// signature that keeps the byte offset in a regex, or the line number in a build
// error, or the temp directory a test ran in, never matches itself twice and the
// whole sidecar is dead weight. [fixSignature] therefore strips exactly the
// things that vary between two occurrences of ONE error — numbers, quoted
// literals, paths, file:line prefixes, hexadecimal — and keeps every word.
//
// ── WHAT IS KEPT ON DISK ──
//
// One JSON file per scope, holding the entries and the counters that say whether
// any of this is working. The counters live in the store rather than in a metrics
// system so that the product question — is the sidecar earning its place — has an
// answer from the first day, readable with `cat`.
//
// Every write is best-effort in consent.go's sense: temp file, rename, and a
// failure is dropped in silence. AN UNWRITABLE DISK MUST NOT BREAK A SESSION —
// the caller is a person's tool call that has already failed once, and the news
// that a lookup file could not be saved is the least useful thing that could
// happen to them next.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/home"
)

const (
	// fixesFileName is the store's name in both scopes: the project bucket
	// beside tasks.jsonl, and the v3 state root for the machine-wide one.
	fixesFileName = "fixes.json"

	// fixesFileVersion stamps the document. A file from a future version is read
	// as far as it parses rather than refused: this is a cache of hints, and the
	// worst a misread entry can do is fail the ratio gate.
	fixesFileVersion = 1

	// fixDecayInterval is how long counts stand before they are halved. It is a
	// week for sweepTTL's reason and states the same judgement about a person's
	// working memory: a fix confirmed once, months ago, on a toolchain since
	// replaced, must not outrank one confirmed twice this morning. Halving rather
	// than expiring is what keeps a fix that KEEPS working — one that earns a new
	// confirmation faster than the halving takes them away — at the top forever.
	fixDecayInterval = 7 * 24 * time.Hour

	// fixMinSuccessRatio is the floor under saying anything at all. A patch that
	// has failed as often as it has worked is a coin toss dressed as advice, and
	// the silence is worth more than the line.
	fixMinSuccessRatio = 0.6

	// fixMinConfirmations is how many times a patch must have worked before it is
	// ever offered. One is deliberate: a fix seen once is thin evidence, and it is
	// still infinitely better than the nothing the model would otherwise have.
	fixMinConfirmations = 1

	// fixAdviceLimit is how many patches may ride on one failed result. ONE. The
	// line is read by a model that has just failed and is deciding what to do
	// next; a menu of three would be a second decision handed to it at the worst
	// possible moment, and the second-best patch is by definition the one that
	// worked less often.
	fixAdviceLimit = 1

	// fixCandidateLimit bounds how many distinct patches one signature may keep.
	// Past it the weakest is dropped, so a signature that a hundred different
	// commands have "fixed" once cannot grow without bound.
	fixCandidateLimit = 8

	// fixSignatureLimit and fixPatchLimit bound the two model-authored strings
	// this file keeps. A signature past the limit is clipped, which is safe —
	// the discriminating words in an error are at the front — and a patch past
	// it is clipped for the same reason and marked, so a clipped command is
	// never offered as though it were whole.
	fixSignatureLimit = 200
	fixPatchLimit     = 300

	// fixSignatureFloor is the shortest normalized error worth keying on.
	// Anything shorter carries no diagnostic words and would collide with
	// everything (see the normalization law above).
	fixSignatureFloor = 12

	// fixErrorScanLines bounds how far into a failed result the diagnostic line
	// is looked for. A tool that has printed forty lines without saying what went
	// wrong is not going to say it on line four hundred.
	fixErrorScanLines = 40
)

// ── the signature ───────────────────────────────────────────────────────────

// The substitutions, in the order they are applied. ORDER IS MEANING here: the
// file:line rule must run before the digit rule, or the line number is already
// an N and the path around it is not recognisable as one; the quote rule must
// run before the path rule, or a quoted path is stripped twice into a shape
// neither rule produces.
var (
	// fixQuoted matches a quoted literal, and it is deliberately fussy about
	// what may sit either side of the quotes. A naive `'[^']*'` matches the
	// span between the apostrophe in "doesn't" and the next quote in the same
	// sentence, which silently eats the diagnostic words in between — so an
	// opening quote must follow the start of the line or an opening delimiter,
	// and a closing quote must be followed by the end or a closing one.
	fixQuoted = regexp.MustCompile("(^|[\\s=:(\\[,])['\"`]([^'\"`\\n]{1,120})['\"`]([\\s.,:;)\\]]|$)")

	// fixFileLine matches `some/file.go:412:2` and `file.py:12` — the shape every
	// compiler, linter and stack trace prints. The path AND the numbers go: the
	// same error on line 9 and on line 412 of two different files is one error.
	fixFileLine = regexp.MustCompile(`[\w./\\+@-]*[\w+-]\.[A-Za-z0-9_]+:\d+(:\d+)?`)

	// fixAbsPath matches a rooted path of at least two segments — absolute, or
	// explicitly relative with `./` or `../`.
	//
	// A path must be ROOTED to be matched, and that is the deliberate line. A
	// bare `a/b` is matched by nothing here, because the errno string "Input/output
	// error" is a bare `a/b` and turning it into "<path> error" would collide it
	// with every other slash-word an error can contain. Two segments minimum for
	// the same kind of reason: a lone `/tmp` in prose is a word, and the temp
	// directories that actually vary between two runs of one test are deeper.
	fixAbsPath = regexp.MustCompile(`(\.{1,2})?/(?:[\w.+@-]+/)+[\w.+@-]*`)

	// fixHex catches the two shapes a number wears when it is not decimal: an
	// 0x-prefixed address, and a bare git-sized hash. Both vary between two
	// occurrences of one error and neither survives the digit rule intact.
	fixHex = regexp.MustCompile(`\b(0[xX][0-9a-fA-F]+|[0-9a-f]{7,40})\b`)

	// fixDigits is the last rule and the broadest: every remaining run of digits
	// is a quantity that varied, not a word that discriminated.
	fixDigits = regexp.MustCompile(`\d+`)

	// fixSpaces collapses the whitespace the rules above leave behind.
	fixSpaces = regexp.MustCompile(`\s+`)
)

// fixWrapperLines are the lines a failed result carries that say only THAT it
// failed. They are skipped when the diagnostic line is chosen, because keying on
// one of them is exactly the collision the normalization law warns about: every
// failing command in the world exits with a code.
var fixWrapperLines = []string{
	"command exited with code",
	"command timed out after",
	"command aborted",
	"operation aborted",
	"exit status",
	"error: process completed with exit code",
	"(no output)",
	"tool panicked:",
}

// fixSignature reduces one tool name and one failed result to the key this
// store is indexed by, or reports that the failure carries nothing worth keying
// on.
//
// The key is the TOOL plus the normalized diagnostic line, joined by a byte that
// cannot appear in either. The tool is part of the key because the same words
// mean different things from different hands — "no such file or directory" from
// `read` is a path the model guessed at, and from `bash` it is a command that
// ran somewhere unexpected — and a patch for one is not a patch for the other.
func fixSignature(tool, text string) (string, bool) {
	line, found := fixDiagnosticLine(text)
	if !found {
		return "", false
	}
	normalized := fixNormalize(line)
	if !fixUsableSignature(normalized) {
		return "", false
	}
	return strings.TrimSpace(tool) + "\x00" + normalized, true
}

// fixDiagnosticLine picks the line a signature is made of: the FIRST non-empty
// line that is neither a wrapper nor a header.
//
// First rather than last, and it is a real choice. Compilers, interpreters and
// the shell put the diagnosis at the top and the summary at the bottom, and a
// summary ("make: *** [build] Error 2") is the same three words for every
// failure a build can have — which is the collision end of the normalization
// law. The cost of choosing first is a command whose real error is buried under
// its own progress output; that failure is invisible rather than wrong, which is
// the direction to fail in.
func fixDiagnosticLine(text string) (string, bool) {
	scanned := 0
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(scrubbed(raw))
		if line == "" {
			continue
		}
		scanned++
		if scanned > fixErrorScanLines {
			return "", false
		}
		// Go prints the package a build error belongs to on its own line as
		// `# github.com/owner/repo/pkg`. It is stable across every different
		// error in that package, which makes it the perfect wrong key.
		if strings.HasPrefix(line, "#") {
			continue
		}
		if fixIsWrapper(line) {
			continue
		}
		return line, true
	}
	return "", false
}

// fixIsWrapper reports whether a line says only that something failed.
func fixIsWrapper(line string) bool {
	lowered := strings.ToLower(line)
	for _, wrapper := range fixWrapperLines {
		if strings.HasPrefix(lowered, wrapper) {
			return true
		}
	}
	return false
}

// fixNormalize applies the substitutions in their stated order and lowercases
// the result. Lowercasing is the one rule with no counter-argument: "Error" and
// "error" are one error, and no tool in the world distinguishes two failures by
// the case of the same word.
func fixNormalize(line string) string {
	line = scrubbed(line)
	line = fixQuoted.ReplaceAllString(line, "${1}<q>${3}")
	line = fixFileLine.ReplaceAllString(line, "<path>:N")
	line = fixAbsPath.ReplaceAllString(line, "<path>")
	line = fixHex.ReplaceAllString(line, "<hex>")
	line = fixDigits.ReplaceAllString(line, "N")
	line = fixSpaces.ReplaceAllString(line, " ")
	return clip(strings.ToLower(strings.TrimSpace(line)), fixSignatureLimit)
}

// fixUsableSignature is the floor under keying on anything. A normalized line
// with no letters left in it is punctuation and placeholders; one shorter than
// [fixSignatureFloor] has been stripped past the point of discriminating.
func fixUsableSignature(normalized string) bool {
	if len(normalized) < fixSignatureFloor {
		return false
	}
	for _, r := range normalized {
		if r >= 'a' && r <= 'z' {
			return true
		}
	}
	return false
}

// fixToolOf reads the tool name back out of a key, for the store's own records.
func fixToolOf(signature string) string {
	if index := strings.IndexByte(signature, 0); index >= 0 {
		return signature[:index]
	}
	return ""
}

// fixErrorOf reads the normalized error back out of a key.
func fixErrorOf(signature string) string {
	if index := strings.IndexByte(signature, 0); index >= 0 {
		return signature[index+1:]
	}
	return signature
}

// ── the file ────────────────────────────────────────────────────────────────

// fixEntry is one patch for one signature, and how it has done.
//
// OK and Failed are a CONFIDENCE, not a census. They are halved on the decay
// interval, so an entry that reads 3/4 today may have been 7/8 a fortnight ago;
// what they rank correctly is which of two patches is more likely to work now.
//
// OK AND WORKED ARE NOT THE SAME OBSERVATION, and the line between them is what
// the model is allowed to be told (fixrecall.go's [fixAnnotate]). OK counts the
// times this command RAN AFTER this failure and the failure did not come back —
// a pairing, which is all the store ever sees the first time. Worked counts the
// times this command was OFFERED as the answer to this failure, the offer was
// taken, and the failure went away — which is the only evidence that says the
// command is a cure rather than a coincidence. Worked is therefore always a
// subset of OK, and an entry may sit at OK 4, Worked 0 forever if nobody ever
// takes the line.
type fixEntry struct {
	Tool   string    `json:"tool"`
	Error  string    `json:"error"`
	Fix    string    `json:"fix"`
	OK     int       `json:"ok"`
	Worked int       `json:"worked,omitempty"`
	Failed int       `json:"failed,omitempty"`
	Seen   time.Time `json:"seen"`

	// The three deltas are THIS session's own increments since the file was last
	// read, and they are never serialized. They exist because the merge on save
	// re-reads the file: adding a delta to whatever is on disk keeps two sessions
	// working the same project from each overwriting the other's count with a
	// stale one (see [fixStore.saveLocked]).
	okDelta, workedDelta, failedDelta int
}

// attempts and ratio are how one entry is ranked and gated.
func (e *fixEntry) attempts() int { return e.ok() + e.failed() }
func (e *fixEntry) ok() int       { return e.OK + e.okDelta }
func (e *fixEntry) worked() int   { return e.Worked + e.workedDelta }
func (e *fixEntry) failed() int   { return e.Failed + e.failedDelta }

func (e *fixEntry) ratio() float64 {
	attempts := e.attempts()
	if attempts <= 0 {
		return 0
	}
	return float64(e.ok()) / float64(attempts)
}

// worthSaying is the gate the whole sidecar's silence hangs on.
func (e *fixEntry) worthSaying() bool {
	return strings.TrimSpace(e.Fix) != "" &&
		e.ok() >= fixMinConfirmations &&
		e.ratio() >= fixMinSuccessRatio
}

// fixDocument is the file on disk. The four counters are the product metric —
// how often the store was asked, how often it had something, and how the
// something did — and they live here rather than anywhere else so that the
// question can be answered by reading the file.
type fixDocument struct {
	Type    string      `json:"type"`
	Version int         `json:"version"`
	Decayed time.Time   `json:"decayed"`
	Asked   int         `json:"asked"`
	Found   int         `json:"found"`
	Worked  int         `json:"worked"`
	Failed  int         `json:"failed"`
	Entries []*fixEntry `json:"entries"`
}

// ── the store ───────────────────────────────────────────────────────────────

// fixStore is one scope's file, loaded once and written through.
//
// The lock is not decoration: a tool batch runs its calls from a goroutine each
// (loop.go's runToolsWarm), so two failures in one batch reach this at the same
// instant.
type fixStore struct {
	mu     sync.Mutex
	path   string
	loaded bool

	document fixDocument
	index    map[string][]*fixEntry

	// The counter deltas are this session's own, for the reason an entry's are:
	// the merge on save adds them to what is on disk rather than replacing it.
	askedDelta, foundDelta, workedDelta, failedDelta int

	// now is the clock, injectable so the decay can be tested without waiting a
	// week. It is nil everywhere but a test.
	now func() time.Time
}

func newFixStore(path string) *fixStore { return &fixStore{path: path} }

func (s *fixStore) clock() time.Time {
	if s.now != nil {
		return s.now()
	}
	return time.Now()
}

// loadLocked reads the file once. Everything is tolerated: a missing file is the
// ordinary case (a project that has never failed at anything), and a file that
// does not parse is a file some other version or some interrupted write left
// behind — starting empty loses hints, which is survivable, where refusing to
// start would not be.
func (s *fixStore) loadLocked() {
	if s.loaded {
		return
	}
	s.loaded = true
	s.document = readFixDocument(s.path)
	s.reindexLocked()
	// The decay is applied to what this session READS but not written back here:
	// a load is not a write, and [fixStore.saveLocked] decides independently from
	// what it finds on disk, so the halving lands on the file exactly once no
	// matter how many sessions opened it.
	if s.decayDueLocked() {
		s.decayLocked(s.clock())
	}
}

func (s *fixStore) decayDueLocked() bool {
	if s.document.Decayed.IsZero() {
		return false
	}
	return s.clock().Sub(s.document.Decayed) >= fixDecayInterval
}

// decayLocked halves every count and drops what reaches nothing.
//
// Floor division rather than rounding, and that is the point: a patch confirmed
// once and never again is gone after one interval, which is exactly the stale
// entry the decay exists to remove. A patch that keeps being confirmed gains
// faster than the halving takes away.
func (s *fixStore) decayLocked(at time.Time) {
	kept := s.document.Entries[:0]
	for _, entry := range s.document.Entries {
		entry.OK /= 2
		entry.Worked /= 2
		entry.Failed /= 2
		if entry.OK <= 0 && entry.Failed <= 0 {
			continue
		}
		kept = append(kept, entry)
	}
	s.document.Entries = kept
	s.document.Decayed = at
	s.reindexLocked()
}

func (s *fixStore) reindexLocked() {
	s.index = make(map[string][]*fixEntry, len(s.document.Entries))
	for _, entry := range s.document.Entries {
		key := entry.Tool + "\x00" + entry.Error
		s.index[key] = append(s.index[key], entry)
	}
}

// consult answers one failed call: the patches worth offering, at most
// [fixAdviceLimit] of them, best first. It is the ONLY read path, and it counts
// itself — every consultation is an `asked`, and one that answers is a `found`.
func (s *fixStore) consult(signature string) []*fixEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadLocked()

	s.askedDelta++
	best := s.rankLocked(signature)
	if len(best) > 0 {
		s.foundDelta++
	}
	s.saveLocked()
	return best
}

// rankLocked is the argmax: the candidates that pass the gate, ordered by how
// often they have worked. Ratio first, then the raw number of confirmations —
// 5/5 beats 1/1, which is the whole reason the tiebreak exists — then recency.
func (s *fixStore) rankLocked(signature string) []*fixEntry {
	candidates := make([]*fixEntry, 0, len(s.index[signature]))
	for _, entry := range s.index[signature] {
		if entry.worthSaying() {
			candidates = append(candidates, entry)
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		left, right := candidates[i], candidates[j]
		if left.ratio() != right.ratio() {
			return left.ratio() > right.ratio()
		}
		if left.ok() != right.ok() {
			return left.ok() > right.ok()
		}
		return left.Seen.After(right.Seen)
	})
	if len(candidates) > fixAdviceLimit {
		candidates = candidates[:fixAdviceLimit]
	}
	// The copies are what the caller reads, so nothing outside this file holds a
	// pointer into an index another goroutine may be re-sorting.
	copies := make([]*fixEntry, len(candidates))
	for index, entry := range candidates {
		clone := *entry
		copies[index] = &clone
	}
	return copies
}

// confirm records that this patch ran after this error and the error did not
// come back. It is a PAIRING and nothing stronger — see [fixEntry].
func (s *fixStore) confirm(signature, patch string) {
	s.tally(signature, patch, 1, 0, 0)
}

// confirmAdvised records the same pairing when the patch that ran is the patch
// this store OFFERED. That is the one observation that says the command is a
// cure, so it is the only one [fixAnnotate] is allowed to call "what worked".
func (s *fixStore) confirmAdvised(signature, patch string) {
	s.tally(signature, patch, 1, 1, 0)
}

// blame records that this patch was offered, tried, and the same error came
// back. It is the half that keeps the counts honest: a store that only ever
// counted successes would rank a patch that has failed nineteen times out of
// twenty at the top of its own signature forever.
func (s *fixStore) blame(signature, patch string) {
	s.tally(signature, patch, 0, 0, 1)
}

func (s *fixStore) tally(signature, patch string, ok, worked, failed int) {
	patch = fixCleanPatch(patch)
	if signature == "" || patch == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadLocked()

	entry := s.findLocked(signature, patch)
	if entry == nil {
		entry = &fixEntry{
			Tool:  fixToolOf(signature),
			Error: fixErrorOf(signature),
			Fix:   patch,
		}
		s.document.Entries = append(s.document.Entries, entry)
		s.index[signature] = append(s.index[signature], entry)
	}
	entry.okDelta += ok
	entry.workedDelta += worked
	entry.failedDelta += failed
	entry.Seen = s.clock()
	s.trimLocked(signature)
	s.saveLocked()
}

// outcome records what happened AFTER a line was injected: worked when the
// offered patch is the one that then succeeded, failed when the same error came
// back. A retry that succeeded with something else entirely is neither — the
// advice was read and set aside, which is not a verdict on it.
func (s *fixStore) outcome(worked bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadLocked()
	if worked {
		s.workedDelta++
	} else {
		s.failedDelta++
	}
	s.saveLocked()
}

func (s *fixStore) findLocked(signature, patch string) *fixEntry {
	for _, entry := range s.index[signature] {
		if entry.Fix == patch {
			return entry
		}
	}
	return nil
}

// trimLocked bounds one signature's candidates, dropping the weakest.
func (s *fixStore) trimLocked(signature string) {
	entries := s.index[signature]
	if len(entries) <= fixCandidateLimit {
		return
	}
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].ratio() != entries[j].ratio() {
			return entries[i].ratio() > entries[j].ratio()
		}
		return entries[i].ok() > entries[j].ok()
	})
	doomed := map[*fixEntry]bool{}
	for _, entry := range entries[fixCandidateLimit:] {
		doomed[entry] = true
	}
	s.index[signature] = entries[:fixCandidateLimit]
	kept := s.document.Entries[:0]
	for _, entry := range s.document.Entries {
		if doomed[entry] {
			continue
		}
		kept = append(kept, entry)
	}
	s.document.Entries = kept
}

// ── the write ───────────────────────────────────────────────────────────────

// saveLocked merges this session's deltas onto whatever is on disk and writes
// the whole file through a temp and a rename.
//
// ── WHY A MERGE AND NOT A LAST-WRITER-WINS ──
//
// Two terminals open on one project are two processes on one file. A whole-file
// write from the one that loaded first would silently undo everything the other
// learned in between — which for a store whose entire value is accumulated
// counts is not a rounding error, it is the feature turning itself off. The
// merge is cheap because the deltas make it exact: this session knows how many
// confirmations IT added, so it adds them to the disk's number rather than
// replacing it, and two sessions confirming the same fix at the same moment end
// at two rather than one. New entries from either side are kept.
//
// What is still last-writer-wins, and is left that way deliberately: the patch
// TEXT of an entry two sessions created simultaneously with the same key, and
// the decay timestamp. Both are one string, both converge on the next write, and
// neither is a count.
//
// A DELTA IS SPENT ONLY WHEN THE WRITE LANDS. A store on an unwritable disk
// keeps every count it has made this session in memory, so the session still
// gets its own advice; it simply does not outlive the process.
//
// Every failure here is silence. See this file's header for the law.
func (s *fixStore) saveLocked() {
	if strings.TrimSpace(s.path) == "" {
		return
	}
	// The read-modify-write is serialized across this process, exactly as the
	// task index's append is (task_index.go's taskIndexMu). Two windows on one
	// project are two PROCESSES and are not covered by it: for them the rename
	// still makes every write whole, and the loss a collision can cause is one
	// confirmation in a confidence count — which is the direction to lose in.
	fixWriteMu.Lock()
	defer fixWriteMu.Unlock()

	merged := s.mergeLocked(readFixDocument(s.path))
	if !writeFixDocument(s.path, merged) {
		return
	}
	// The merged document becomes this session's base — so a store written here
	// immediately sees what the other terminal had learned — and the deltas,
	// which are now folded into what is on disk, go with the entries that
	// carried them.
	s.document = merged
	s.reindexLocked()
	s.askedDelta, s.foundDelta, s.workedDelta, s.failedDelta = 0, 0, 0, 0
}

// fixWriteMu serializes this process's store writes. It is one lock for every
// path rather than one per path because a write here is a few kilobytes of JSON
// that happens only when something has already failed — the contention it can
// cause is smaller than the map of mutexes that would avoid it.
var fixWriteMu sync.Mutex

// mergeLocked folds this session's deltas onto a document read from disk.
func (s *fixStore) mergeLocked(disk fixDocument) fixDocument {
	byKey := make(map[string]*fixEntry, len(disk.Entries))
	for _, entry := range disk.Entries {
		byKey[entry.Tool+"\x00"+entry.Error+"\x00"+entry.Fix] = entry
	}
	for _, mine := range s.document.Entries {
		key := mine.Tool + "\x00" + mine.Error + "\x00" + mine.Fix
		theirs, known := byKey[key]
		if !known {
			// An entry only this session has: its deltas ARE its counts, because
			// its base was zero when it was created.
			fresh := *mine
			fresh.OK, fresh.Worked, fresh.Failed = mine.ok(), mine.worked(), mine.failed()
			fresh.okDelta, fresh.workedDelta, fresh.failedDelta = 0, 0, 0
			disk.Entries = append(disk.Entries, &fresh)
			byKey[key] = &fresh
			continue
		}
		theirs.OK += mine.okDelta
		theirs.Worked += mine.workedDelta
		theirs.Failed += mine.failedDelta
		if mine.Seen.After(theirs.Seen) {
			theirs.Seen = mine.Seen
		}
	}

	disk.Type, disk.Version = "fixes", fixesFileVersion
	disk.Asked += s.askedDelta
	disk.Found += s.foundDelta
	disk.Worked += s.workedDelta
	disk.Failed += s.failedDelta
	if disk.Decayed.IsZero() {
		disk.Decayed = s.clock()
	}

	merged := &fixStore{path: s.path, loaded: true, document: disk, now: s.now}
	merged.reindexLocked()
	if merged.decayDueLocked() {
		merged.decayLocked(merged.clock())
	}
	// Every signature is trimmed on the way out rather than only the one just
	// written: the merge can put another session's candidates beside this one's,
	// and the cap is a property of the FILE, not of one session's view of it.
	for signature := range merged.index {
		merged.trimLocked(signature)
	}
	return merged.document
}

// readFixDocument reads one store, tolerating everything (see loadLocked).
func readFixDocument(path string) fixDocument {
	if strings.TrimSpace(path) == "" {
		return fixDocument{}
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return fixDocument{}
	}
	var document fixDocument
	if err := json.Unmarshal(raw, &document); err != nil {
		return fixDocument{}
	}
	kept := document.Entries[:0]
	for _, entry := range document.Entries {
		if entry == nil || entry.Tool == "" || entry.Error == "" || entry.Fix == "" {
			continue
		}
		entry.okDelta, entry.workedDelta, entry.failedDelta = 0, 0, 0
		kept = append(kept, entry)
	}
	document.Entries = kept
	return document
}

// writeFixDocument writes the file atomically and reports whether it landed.
//
// The temp file is minted in the destination directory with a random name
// rather than being a fixed `path + ".tmp"`: two sessions writing at the same
// instant would otherwise be two writers on one temp file, and the rename that
// is supposed to be the one write that cannot half-happen would publish a
// half-written mixture of both.
func writeFixDocument(path string, document fixDocument) bool {
	encoded, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return false
	}
	directory := filepath.Dir(path)
	if directory != "" && directory != "." {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			return false
		}
	}
	temporary, err := os.CreateTemp(directory, ".fixes-*.json")
	if err != nil {
		return false
	}
	name := temporary.Name()
	if _, err := temporary.Write(append(encoded, '\n')); err != nil {
		temporary.Close()
		_ = os.Remove(name)
		return false
	}
	if err := temporary.Close(); err != nil {
		_ = os.Remove(name)
		return false
	}
	if err := os.Chmod(name, 0o600); err != nil {
		_ = os.Remove(name)
		return false
	}
	if err := os.Rename(name, path); err != nil {
		_ = os.Remove(name)
		return false
	}
	return true
}

// fixCleanPatch bounds and cleans a command on its way into the store. It is
// model-authored text that will be read back out into a person's transcript, so
// it loses the bytes a terminal takes as instructions (loop.go's `scrubbed`
// states why) and is clipped to one readable line.
func fixCleanPatch(patch string) string {
	patch = strings.TrimSpace(scrubbed(strings.ReplaceAll(patch, "\n", " ")))
	patch = fixSpaces.ReplaceAllString(patch, " ")
	return clip(patch, fixPatchLimit)
}

// ── the two scopes ──────────────────────────────────────────────────────────

// fixShelf is the pair of stores, consulted PROJECT FIRST.
//
// Project first because a fix is usually about this repository — its toolchain,
// its build tags, the one grep on this machine that does not take that regex —
// and a machine-wide answer that contradicts it is the more general and the less
// likely to be right. The global store is what makes the first failure in a
// brand new checkout cheap: it is where "this laptop's ugrep will not take
// `(sub)`" is remembered across every project on it.
//
// A confirmation is written to BOTH. That is the only way the global store ever
// learns anything, and the two are not redundant: the project's is precise, and
// the machine's is what survives the project being deleted.
type fixShelf struct {
	project *fixStore
	global  *fixStore
}

// fixAdvice is one answer: the patch, its record, and WHICH store said so — the
// last of which is needed because the outcome has to be recorded back where the
// advice came from.
type fixAdvice struct {
	patch string
	ok    int
	// worked is the count [fixAnnotate] is allowed to speak a number from: the
	// times this patch was offered here, taken, and the error went away.
	worked int
	failed int
	from   *fixStore
}

func newFixShelf(bucket string) *fixShelf {
	shelf := &fixShelf{global: newFixStore(home.Join("v3", fixesFileName))}
	if bucket = strings.TrimSpace(bucket); bucket != "" {
		shelf.project = newFixStore(filepath.Join(bucket, fixesFileName))
	}
	return shelf
}

// consult asks the project store and falls back to the machine's.
//
// BOTH are counted as asked when both are read, and only the one that answered
// is counted as found. A store that was never read is not a store that had
// nothing to say.
func (s *fixShelf) consult(signature string) []fixAdvice {
	if s == nil || signature == "" {
		return nil
	}
	for _, store := range []*fixStore{s.project, s.global} {
		if store == nil {
			continue
		}
		found := store.consult(signature)
		if len(found) == 0 {
			continue
		}
		advice := make([]fixAdvice, 0, len(found))
		for _, entry := range found {
			advice = append(advice, fixAdvice{
				patch:  entry.Fix,
				ok:     entry.ok(),
				worked: entry.worked(),
				failed: entry.failed(),
				from:   store,
			})
		}
		return advice
	}
	return nil
}

// confirm writes the pairing into both files, and says whether the patch that
// ran is the one that was OFFERED — because "this command was handed back and
// it worked" is a fact about the command, true in whichever file remembered it.
// Which store's own hit rate moves is a different question, settled by the
// caller (fixrecall.go's [episode.noteSuccess]).
func (s *fixShelf) confirm(signature, patch string, advised bool) {
	if s == nil {
		return
	}
	for _, store := range []*fixStore{s.project, s.global} {
		if store == nil {
			continue
		}
		if advised {
			store.confirmAdvised(signature, patch)
			continue
		}
		store.confirm(signature, patch)
	}
}

func (s *fixShelf) blame(signature, patch string) {
	if s == nil {
		return
	}
	for _, store := range []*fixStore{s.project, s.global} {
		if store != nil {
			store.blame(signature, patch)
		}
	}
}

// ── where the two files live ────────────────────────────────────────────────

// fixesBucket is this session's project directory: the bucket that holds every
// conversation about this workspace, which is where tasks.jsonl lives
// (task_index.go's own argument for climbing one level out of a session folder).
//
// A NODE IS HANDED ITS PARENT'S. A task node's session file is a journal inside
// the parent's place, not a place of its own, so deriving a bucket from it would
// give every node a private store nobody else ever reads — and a node failing at
// a build is the single richest source of error→fix pairs this product has. The
// executor threads it down at the spawn seam (task_run.go, orchestrate.go).
func (c Config) fixesBucket() string {
	if bucket := strings.TrimSpace(c.fixesDir); bucket != "" {
		return bucket
	}
	if dir := strings.TrimSpace(c.Place.Dir); dir != "" {
		bucket := filepath.Dir(dir)
		if bucket == "" || bucket == "." {
			return ""
		}
		return bucket
	}
	// The legacy flat layout: the session file's own directory IS the project's
	// directory, exactly as it is for the task index.
	file := strings.TrimSpace(c.SessionFile)
	if file == "" {
		return ""
	}
	directory := filepath.Dir(file)
	if directory == "" || directory == "." {
		return ""
	}
	return directory
}
