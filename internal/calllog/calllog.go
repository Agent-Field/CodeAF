// Package calllog is aforge's always-on record of the model calls it makes.
//
// It exists because of what debugging one used to cost. A headless run that
// sits on "still waiting" for fifteen minutes writes nothing anywhere that says
// which call is in flight, what shape it had, or how it ended, and the last
// three wire bugs — a thinking pass eating the answer ceiling on GLM 5.3,
// encrypted reasoning replayed to a model that did not produce it after a
// /model switch, a bare leaf that never filed its artifacts — were each found
// by standing a logging proxy in front of OpenRouter. A proxy is not something
// a person running aforge on their own laptop can be asked to build, so the
// record is built in.
//
// ONE LINE PER CALL, JSON Lines, appended under a mutex. The provider adapter
// writes it, because every outbound call in the process passes through that one
// door — the chat's turn, `aforge do`, plan briefs and contracts, the delivery
// gate, reflexes, the document route.
//
// WHAT IS NEVER IN IT: the prompts. A transcript is the person's own data and
// their own files, and a debug log that quietly accumulates it is a liability
// rather than a tool. The record carries the SHAPE of a request — how many
// messages, how many tools, which knobs, which ceiling — and the bodies only
// when someone deliberately asks for them with AFORGE_CALL_LOG_BODIES.
//
// A WRITE FAILURE IS NEVER A FAILED CALL. Anything that goes wrong here — a
// read-only home, a full disk, a path that is a directory — silences the log
// for the rest of the process and prints one line naming the path it could not
// write. A model call that failed because its log could not be written would be
// the worst possible trade for a debugging convenience.
//
// AND A ROW IS NEVER LOST OVER A VALUE. A number JSON has no spelling for — the
// wait controller's +Inf when it holds no alternative lane — is taken off the
// row and named in words on it rather than costing the row, because a row that
// vanishes is a call that reads as in flight forever (finite.go).
package calllog

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/home"
)

const (
	// EnvVar switches the log off or moves it. It is exported so the manual,
	// the settings footer and `aforge logs` can all say the same word the code
	// reads.
	EnvVar = "AFORGE_CALL_LOG"
	// BodiesEnvVar adds the request and response bodies to every record. It is
	// a separate pin and not a value of EnvVar because the two answer different
	// questions — where the log goes, and how much of the person's own data it
	// is allowed to hold.
	BodiesEnvVar = "AFORGE_CALL_LOG_BODIES"
	// OffValue is what EnvVar is set to to write nothing at all.
	OffValue = "off"

	// DirName is the folder the log lives in, beside the quirks memo rather
	// than under it: both are things this process learned about its provider,
	// and "where does aforge keep what it wrote down" has one answer.
	DirName = "logs"
	// FileName is the live log; PreviousFileName is the one predecessor kept
	// across a rotation.
	FileName         = "calls.jsonl"
	PreviousFileName = "calls.1.jsonl"

	// MaxBytes is where the live file rotates when the bodies pin is off.
	// Thirty-two megabytes is a few hundred thousand shape-only records —
	// weeks of ordinary use, and still small enough that a person can grep
	// the whole thing — and the predecessor doubles the history without
	// letting the pair grow without bound.
	MaxBytes = 32 << 20

	// BodiesMaxBytes is where the live file rotates when BodiesEnvVar is on.
	// A body-bearing record is tens to hundreds of kilobytes, and MaxBytes
	// then turns over after a few dozen calls — too soon for the session
	// that asked for the bodies. Two hundred and fifty-six megabytes is the
	// same figure the debug record keeps per run, and it is hours of a real
	// debug session rather than minutes, while still bounding the pair.
	BodiesMaxBytes = 256 << 20
)

// Record is one model call as it happened: what was asked, what came back, and
// what the answer taught. Every field is omitempty, because the emptiness law
// applies to files as much as to screens — a record of a call that never
// reached an endpoint says nothing about tokens, and a zero in that place would
// be a figure somebody could read as a measurement.
type Record struct {
	// Time is when this row was written, RFC3339 with milliseconds: the moment
	// an attempt went out on a start row, the moment it came back on an end row.
	Time string `json:"ts"`
	// ID pairs the two rows one attempt writes. It is short and random rather
	// than a counter because several agents in one process append to one file,
	// and a counter would need a lock that says nothing a random token does not.
	ID string `json:"id,omitempty"`
	// Run is the invocation this call belongs to — the id internal/trace mints
	// at every door, and the same id that names the debug record's folder.
	//
	// IT IS WHAT MAKES THIS FILE JOINABLE. A developer who wanted the call
	// count and the round count of one `aforge do` came here to reconstruct
	// them, and found rows carrying a tag, a node and a timestamp and nothing
	// at all naming the run — so attribution was by clock alone, in a file
	// several runs on one machine append to. The run id is on the row and in
	// the `--json` envelope the same run printed, and the two are joined by
	// looking at them.
	//
	// It is on BOTH rows of a pair rather than only the start: the end row is
	// the one that carries the cost and the finish reason, and a reader
	// filtering the file to one run must not have to pair every row first to
	// keep the halves that matter.
	Run string `json:"run,omitempty"`
	// Phase is "start" on the row written the moment a call goes out, and
	// ABSENT on the row written when it comes back. One word rather than two,
	// because the pair is what a reader is looking for: a start with no end
	// beside it is a call that is still in flight, and that is exactly the state
	// that used to be invisible — a planning call four minutes into a
	// 65,536-token ceiling looked identical to an idle process.
	Phase string `json:"phase,omitempty"`
	// Tag is what the call was for — "turn", "leaf", "compile", "brief" — set
	// by whoever made it (provider.WithCallTag). Empty for a call this lane did
	// not locate, which is honest: an untagged row is still a row.
	Tag string `json:"tag,omitempty"`
	// Node is the work the call belongs to, where the caller knows one: a plan
	// node's key or a task's id.
	Node string `json:"node,omitempty"`

	// Model is what was asked for; Served is who the router says answered,
	// which is the difference between "GLM is slow" and "one endpoint serving
	// GLM is slow".
	Model  string `json:"model,omitempty"`
	Served string `json:"served,omitempty"`
	// Effort is the reasoning word or budget that actually travelled, in the
	// shape the wire carried it: a word, "NNN tokens" for a budget, or "off"
	// when the request asked for no thinking pass at all. It is what was SENT
	// and not what the caller wanted, because the two differ on every model
	// whose endpoint refuses a disable.
	Effort string `json:"effort,omitempty"`
	// MaxTokens is the ceiling that TRAVELLED — the caller's answer plus the
	// room the thinking pass in front of it is allocated (the provider's
	// ceilingFor) — and not the figure the caller started from. The gap between
	// the two is exactly the bug a thinking pass eating an answer produces.
	MaxTokens int `json:"max_tokens,omitempty"`
	// Messages and Tools are the request's shape. Counts and not content: see
	// the package comment on why the transcript is not in here.
	Messages int  `json:"messages,omitempty"`
	Tools    int  `json:"tools,omitempty"`
	Stream   bool `json:"stream,omitempty"`
	// Attempt is 1-based over the transport's retry loop, so a call that was
	// paced four times leaves four rows that can be told apart.
	Attempt int `json:"attempt,omitempty"`
	// Relaxed is which rungs of the endpoint-refusal ladder this body had
	// already climbed — "reasoning", "max_tokens", "tools" — so a degraded
	// request is never mistaken for the one the caller wrote.
	Relaxed []string `json:"relaxed,omitempty"`

	Status int    `json:"status,omitempty"`
	Millis int64  `json:"ms,omitempty"`
	Finish string `json:"finish,omitempty"`

	// ── the lane, and the wait it made (docs/ARCHITECTURE.md, Decision 10)
	//
	// Millis above is the whole call, which is the figure a bill is computed
	// from and the wrong one to debug a slow afternoon with: it is a queue and
	// a generation added together, and the two go wrong for different reasons.
	// These four separate them and say who the wait belonged to.
	//
	// Lane is who the routing preference ASKED FOR and Served, above, is who
	// answered. Keeping both is the whole point: a log in which they differ is
	// a log of the router overriding a choice, and that is the single most
	// useful line in the file when a lane a person pinned is not the one that
	// wrote their answer.

	// TTFTms is the wait before the first token, in milliseconds, on a streamed
	// call. It is absent on a call that was not streamed — where the first
	// token and the last arrive together and no endpoint's queue is separable
	// from its writing — and absent is honest there rather than instant.
	TTFTms int64 `json:"ttft_ms,omitempty"`
	// DeadlineMs is when the watch was going to start thinking about a second
	// request, derived at send time from the belief about the lane expected to
	// serve rather than from any constant. It is on the row because a hedge
	// that fired is only half a story: the rows where the deadline was set and
	// NOT reached are what say the deadline was set in the right place.
	DeadlineMs int64 `json:"deadline_ms,omitempty"`
	// Lane is the machine the preference named — the first entry of the
	// `provider.order` this request carried, or the pin it carried instead.
	// Empty for a call to an endpoint that is not a router, and for one sent
	// with no preference at all.
	Lane string `json:"lane,omitempty"`
	// Hedged marks the row of a call that was rescued by a second request to
	// another lane. Both halves of the pair leave their own rows; this is what
	// says they were a pair.
	Hedged bool `json:"hedged,omitempty"`

	// ── why it waited, and what was done about it
	//
	// The four fields above say what was BELIEVED before the request went out.
	// These say what happened to the wait itself, and they are what makes an
	// autopsy possible in one line rather than in a correlation of two: when
	// the silence was acted on, what the act was, how many requests one
	// question became, and what the arms that did not answer cost.
	//
	// A row with none of them is a call nothing had to be done about, which is
	// almost every row and is exactly the state the emptiness law leaves blank.

	// SilenceMs is how long the stream had been silent — nothing visible, and
	// for a first token nothing at all — at the moment something was done about
	// it.
	SilenceMs int64 `json:"silence_ms,omitempty"`
	// Action is what was done: "hedge", "ask", "report", "escalate" or
	// "commit", in the controller's own words. Absent on a call that was never
	// acted on.
	Action string `json:"action,omitempty"`
	// Reason is the controller's own machine word for what it decided on. It is
	// absent when nothing was acted on.
	Reason string `json:"reason,omitempty"`
	// Refused is why a hedge the controller called for never reached the wire:
	// "budget", "no alt" or "no room". It is absent when nothing was refused.
	Refused string `json:"refused,omitempty"`
	// Arms is how many requests this one question put on the wire, counting the
	// original. One is the ordinary case and is left off the row.
	Arms int `json:"arms,omitempty"`
	// WasteUSD is what the arms that did not answer cost, by the router's own
	// figure where one arrived and by the frontier's estimate where the arm was
	// cancelled before its usage frame.
	WasteUSD float64 `json:"waste_usd,omitempty"`
	// WaitS and CostS are the two numbers the decision was actually made on:
	// the expected remaining wait, and what acting was expected to cost, both
	// in seconds at the moment of the act. A row that recorded the action
	// without them could only ever confirm what somebody already suspected.
	WaitS float64 `json:"wait_s,omitempty"`
	CostS float64 `json:"cost_s,omitempty"`
	// Note is one sentence, in words, about something this call decided that no
	// other field can say — "pinned lane coreweave was silent for 10s —
	// borrowing auto for this answer". It is empty on almost every row, and it
	// is where a decision taken with nobody watching leaves its account.
	Note string `json:"note,omitempty"`

	PromptTokens     int     `json:"prompt_tokens,omitempty"`
	CompletionTokens int     `json:"completion_tokens,omitempty"`
	ReasoningTokens  int     `json:"reasoning_tokens,omitempty"`
	CachedTokens     int     `json:"cached_tokens,omitempty"`
	Cost             float64 `json:"cost,omitempty"`

	// Error is the provider's own sentence, clipped. Clipped rather than whole
	// because a provider that answers with a stack trace or an HTML error page
	// would otherwise put a screenful into every line of the log.
	Error string `json:"error,omitempty"`
	// Learned is the quirk this answer taught the adapter, by the memo's own
	// names: reasoning_mandatory, reasoning_disable_ignored,
	// cache_control_refused, reasoning_budget_refused, reasoning_replay_refused.
	// It is a list because one 400 can name more than one refused field.
	Learned []string `json:"learned,omitempty"`
	// EmptyAtCeiling is the thinking-ate-the-answer signature: no text, a
	// "length" finish, and the whole ceiling spent.
	EmptyAtCeiling bool `json:"empty_at_ceiling,omitempty"`

	// RequestBody and ResponseBody are present ONLY under BodiesEnvVar. They
	// are whole and unclipped, because the reason to turn them on is that
	// something in the exact bytes is what is wrong.
	RequestBody  string `json:"request_body,omitempty"`
	ResponseBody string `json:"response_body,omitempty"`
}

// PhaseStart is the value Record.Phase carries on the row written as a call
// goes out. There is deliberately no PhaseEnd: an end row omits the field, so
// the common case costs nothing and "no phase" has exactly one meaning.
const PhaseStart = "start"

// NewID mints the token that pairs one attempt's two rows. Four bytes is eight
// hex characters — enough that two live calls in one file will not collide, and
// short enough to sit on a line a person is reading.
func NewID() string {
	var raw [4]byte
	if _, err := rand.Read(raw[:]); err != nil {
		// A machine with no entropy is not a reason to lose the row. An empty
		// id costs the pairing and nothing else.
		return ""
	}
	return hex.EncodeToString(raw[:])
}

// MaxErrorChars bounds Record.Error. It is a sentence beside a status code, not
// a report: the provider's own words fit, and an upstream that answered with a
// page does not get to own a line of the log.
const MaxErrorChars = 400

// ClipError is how a provider's message becomes a record's Error field. It is
// here rather than at the call site so that every writer clips the same way and
// a reader can trust what a trailing ellipsis means.
func ClipError(message string) string {
	message = strings.TrimSpace(message)
	if len(message) <= MaxErrorChars {
		return message
	}
	return strings.TrimSpace(message[:MaxErrorChars]) + "…"
}

// PathFor names the log for one profile directory, and returns "" when the log
// is switched off.
//
// It resolves exactly the way the quirks memo beside it does — the profile
// directory when there is one, the state root otherwise — with the environment
// pin on top, so a person debugging one run can put its log somewhere they can
// watch without moving anything else aforge owns.
//
// UNDER `go test` it answers "" for anything that would land inside the state
// root the environment named, so that a test binary cannot write into the
// ledger of the person who started it (undertest.go says why, and what a test
// that wants a log does instead).
func PathFor(dir string) string {
	if pinned := strings.TrimSpace(os.Getenv(EnvVar)); pinned != "" {
		if strings.EqualFold(pinned, OffValue) {
			return ""
		}
		return pinned
	}
	if dir = strings.TrimSpace(dir); dir != "" {
		return chosenPath(filepath.Join(dir, DirName, FileName))
	}
	return chosenPath(homeJoin(DirName, FileName))
}

// Bodies reports whether this process was asked to record the request and
// response bodies as well as the shape of a call.
func Bodies() bool {
	value := strings.TrimSpace(os.Getenv(BodiesEnvVar))
	return value != "" && value != "0" && !strings.EqualFold(value, "false") && !strings.EqualFold(value, OffValue)
}

// rotateAt is the live-file size that triggers a rotation. The ordinary cap
// stays MaxBytes so a shape-only log still turns over after weeks, not hours.
// Bodies raise it: a body-bearing record is tens to hundreds of kilobytes,
// and MaxBytes then turns over after a few dozen calls — too soon for the
// session that asked for the bodies.
func rotateAt() int64 {
	if Bodies() {
		return BodiesMaxBytes
	}
	return MaxBytes
}

// log is the process's one open file. It is a singleton for the reason the
// quirks memo is: the adapter underneath is shared by every agent in the
// process, and one appender with one mutex is the only shape in which their
// rows cannot interleave halfway through a line.
type log struct {
	mutex sync.Mutex
	// path is where the log is being written. Empty means nothing has been
	// opened yet, which is not the same as being switched off — see write,
	// where the first append resolves the default.
	path string
	file *os.File
	// size is what has been written to the open file, counted rather than
	// stat'd: a stat per append is a syscall per model call to learn something
	// this process already knows.
	size int64
	// silenced is set by the first write failure and never cleared. A log that
	// could not be written once will almost certainly fail again, and a line of
	// stderr per model call would be worse than the missing log.
	silenced bool
	// resolved says PathFor has already been consulted for this process, so an
	// unopened log does not re-read the environment on every call.
	resolved bool
}

var shared = &log{}

// stderr is seamed so the one failure line is assertable without a test having
// to capture the process's own file descriptor.
var stderr io.Writer = os.Stderr

// homeJoin is the state-root default, seamed so this package's own tests can
// exercise the fallback without an AFORGE_HOME.
var homeJoin = defaultHomeJoin

// Open points the log at a profile directory and is called once at startup,
// beside the quirks memo it lives next to. It opens nothing: the file is opened
// by the first record, so a process that makes no model call leaves no file and
// no empty logs directory behind.
func Open(dir string) {
	shared.mutex.Lock()
	defer shared.mutex.Unlock()
	shared.close()
	shared.path = PathFor(dir)
	shared.resolved = true
	shared.silenced = false
}

// Close releases the file. It is called on the way out of a process that opened
// one; a process that forgets loses nothing, because every record is written
// and flushed as it happens.
func Close() {
	shared.mutex.Lock()
	defer shared.mutex.Unlock()
	shared.close()
	// The path is kept. A late record after a Close — a goroutine finishing its
	// call while the surface is tearing down — reopens rather than vanishing.
	shared.file = nil
}

// Path is where records are going, or "" when the log is off. It is what
// `aforge logs --path` and `aforge doctor` read.
func Path() string {
	shared.mutex.Lock()
	defer shared.mutex.Unlock()
	if !shared.resolved {
		shared.path = PathFor("")
		shared.resolved = true
	}
	return shared.path
}

// Append writes one record. It never returns an error and never blocks on
// anything but the mutex and the write itself: its callers are model calls, and
// nothing about a model call may depend on a disk.
func Append(record Record) {
	noteLast(record)
	countCall(record)
	shared.write(record)
}

// calls is how many model calls each run this process opened has started. It is
// kept in memory beside [last] and for the same reason: the figure a headless
// verb publishes in its `--json` envelope may not depend on whether anybody
// turned the file on, and AFORGE_CALL_LOG=off must not change what a run
// reports about itself.
//
// COUNTED ON THE START ROW, so a call that is still in flight when the run is
// priced is counted — it was made, whatever came back — and a retried attempt
// counts once per attempt, which is what a person reading this file counts too.
//
// One entry per run, minted once per door ([trace.Begin]), so the map is a
// handful of entries in the longest-lived process and never grows per call.
var calls struct {
	mutex sync.Mutex
	by    map[string]int
}

func countCall(record Record) {
	if record.Phase != PhaseStart || record.Run == "" {
		return
	}
	calls.mutex.Lock()
	defer calls.mutex.Unlock()
	if calls.by == nil {
		calls.by = make(map[string]int, 2)
	}
	calls.by[record.Run]++
}

// CallsFor is how many model calls one run has made. Zero for a run that has
// made none, and for a run this process never opened — which is the same
// answer, and the caller that publishes it is the one that knows whether the
// run is its own.
func CallsFor(run string) int {
	calls.mutex.Lock()
	defer calls.mutex.Unlock()
	return calls.by[strings.TrimSpace(run)]
}

// LastCall is the newest call this process has heard back from: the model that
// answered and the moment it did. It is the in-memory half of the log, kept
// whether or not the file is being written.
type LastCall struct {
	Model string
	Tag   string
	Node  string
	At    time.Time
}

// last is the one fact the log keeps in memory as well as on disk. The headless
// waiting line used to read it from the journal's usage rows, and a bare leaf
// writes its usage row when it FINISHES — so a leaf ten minutes into its work
// reported "last call … 10m ago" while calls were landing every second, which
// is the opposite of what the line exists to say. Every end row passes through
// Append; this is the same record, read a moment sooner.
var last struct {
	mutex sync.Mutex
	call  LastCall
	found bool
}

func noteLast(record Record) {
	// A start row is a call that has not answered yet, and a row with no model
	// cannot say who answered — neither is the fact a waiting line wants.
	if record.Phase != "" || record.Model == "" {
		return
	}
	at, err := time.Parse(timeLayout, record.Time)
	if err != nil {
		return
	}
	last.mutex.Lock()
	last.call = LastCall{Model: record.Model, Tag: record.Tag, Node: record.Node, At: at}
	last.found = true
	last.mutex.Unlock()
}

// Last reports the newest finished call this process made, and false when there
// has not been one — a process that has not reached a model and one that heard
// back a moment ago are different situations, and no zero is invented for the
// first.
func Last() (LastCall, bool) {
	last.mutex.Lock()
	defer last.mutex.Unlock()
	return last.call, last.found
}

// timeLayout is how Record.Time is spelled, on the way out and on the way back.
const timeLayout = "2006-01-02T15:04:05.000Z07:00"

func (l *log) write(record Record) {
	line, err := marshalRecord(record)
	if err != nil {
		// A record that will not serialize is a bug in the builder rather than
		// a broken disk, and it must not silence the log for the calls that
		// follow. It must not be quiet either: a row that vanishes is a call
		// that reads as in flight forever, which is the one failure a log may
		// not have (finite.go, and the numbers it rescues before this point).
		reportUnspellable(err)
		return
	}
	l.mutex.Lock()
	defer l.mutex.Unlock()
	if l.silenced {
		return
	}
	if !l.resolved {
		// Nobody called Open — a surface that never loaded a config, a tool
		// reaching a model before startup finished — and the log is still ON,
		// because always-on is the whole point. It resolves to the same place a
		// loaded process would put it, except in a test binary, where that
		// place belongs to the person who started the test (undertest.go).
		l.path = PathFor("")
		l.resolved = true
	}
	if l.path == "" {
		return
	}
	if err := l.ensure(); err != nil {
		l.silence(err)
		return
	}
	if l.size+int64(len(line))+1 > rotateAt() {
		if err := l.rotate(); err != nil {
			l.silence(err)
			return
		}
	}
	written, err := l.file.Write(append(line, '\n'))
	l.size += int64(written)
	if err != nil {
		l.silence(err)
	}
}

// ensure opens the file if it is not open, and learns how big it already is so
// the rotation below has something to count against.
func (l *log) ensure() error {
	if l.file != nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(l.path), 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(l.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	l.file = file
	l.size = 0
	if info, err := file.Stat(); err == nil {
		l.size = info.Size()
	}
	return nil
}

// rotate moves the full log aside and starts a new one, keeping exactly one
// predecessor. Two files rather than a dated series: this is a debugging record
// and not an archive, and a series is how a log quietly fills a disk nobody was
// watching.
func (l *log) rotate() error {
	l.close()
	previous := filepath.Join(filepath.Dir(l.path), PreviousFileName)
	if base := filepath.Base(l.path); base != FileName {
		// A redirected log keeps its own name for its predecessor, so two runs
		// pointed at two paths never rotate on top of each other.
		previous = l.path + ".1"
	}
	// The old predecessor goes without ceremony; keeping one means replacing
	// one. A rename that cannot happen is a real failure — the next write would
	// go straight back over the cap — so it is reported rather than swallowed.
	if err := os.Rename(l.path, previous); err != nil && !os.IsNotExist(err) {
		return err
	}
	return l.ensure()
}

// silence stops the log for the rest of the process, after ONE line naming what
// could not be written. The line is on stderr rather than in the surface
// because the surfaces that make model calls are drawing a conversation, and a
// disk problem is not a turn.
func (l *log) silence(err error) {
	l.close()
	l.silenced = true
	fmt.Fprintf(stderr, "aforge: cannot write the model-call log at %s (%v); it is off for this run\n", l.path, err)
}

func (l *log) close() {
	if l.file != nil {
		l.file.Close()
		l.file = nil
	}
	l.size = 0
}

// defaultHomeJoin names a file under aforge's state root. It is a function
// variable's default rather than a direct call so that this package's tests can
// exercise the fallback without moving the developer's own state root.
func defaultHomeJoin(elements ...string) string { return home.Join(elements...) }
