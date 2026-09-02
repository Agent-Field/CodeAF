// Package trace is the record of everything one run did, kept only when
// somebody asked for it.
//
// It exists because of what debugging a bad turn costs today. The model-call
// log beside it (internal/calllog) is the INDEX: one line per call, always on,
// small enough to grep, and deliberately holding none of the person's own
// words. That is the right shape for "which call was slow" and the wrong shape
// for "what exactly did we send, and what exactly came back" — the question
// somebody has after a reply that made no sense, a tool that refused, or a
// route that went somewhere they did not expect. Answering that needs bodies,
// and bodies do not belong in a file that is always on and rotates at 32 MB,
// where one long turn evicts the failure the person came for.
//
// So: ONE SWITCH, ONE FOLDER PER RUN, AND NOTHING WRITTEN WHEN IT IS OFF. The
// switch has three doors — the environment pin AFORGE_DEBUG, a --debug flag on
// chat, do and exec, and /debug inside a conversation — and when none of them
// was used, [For] returns nil after two atomic loads and every method on that
// nil recorder is a no-op. A feeder site therefore costs one call and one nil
// check on the runs nobody is debugging, which is what lets the feeders sit on
// the hot path at all.
//
// THE THREE DOORS MEAN THE SAME RECORD AND NOT THE SAME SCOPE, and the run id
// on the context is what draws the line. The pin and the flag were handed to
// THIS PROCESS on purpose, so they turn the record on process-wide ([Enable]):
// every run the process opens is recorded, each into its own folder. /debug was
// typed inside ONE conversation, and a process can hold several — an engine
// host holds one per person sitting in front of it — so it turns the record on
// for that run and no other ([EnableRun]). A switch that could not tell them
// apart would land one person's prompts, files and replies in a folder somebody
// else asked for, which is the one thing a record of a person's own data may
// never do.
//
// THE RECORD IS A PERSON'S OWN DATA. It holds their prompts, their files and
// the model's whole reply, so it lives under the state root and nowhere else,
// its folder is 0700 and its files 0600, and no authorization header or key
// value is ever written into it ([Scrub]). A run keeps its own folder, named by
// a run id that every record in it carries, and old folders are pruned WHOLE
// (see [KeepRuns]) — never a rotation inside a run, because a rotation inside a
// run is exactly how the failure being investigated gets deleted mid-run.
//
// A WRITE FAILURE IS NEVER A FAILED RUN. Everything here follows calllog's
// discipline: a full disk, a read-only home or a path that is a directory
// silences this run's recorder after ONE line on stderr naming the path, and
// the run carries on exactly as it would have with the switch off.
package trace

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/Agent-Field/aforge-v2/internal/home"
)

const (
	// EnvVar is the switch's spelling in a shell. It is exported so the manual,
	// the settings footer and the doors all say the word the code reads.
	EnvVar = "AFORGE_DEBUG"
	// BodiesEnvVar is the switch's OLD name — the pin that used to put request
	// and response bodies on every line of the model-call log. It means the
	// same thing as EnvVar for one release, so a person with the old word in a
	// shell history gets the record rather than silence.
	BodiesEnvVar = "AFORGE_CALL_LOG_BODIES"
	// MaxMBEnvVar and KeepEnvVar move the two ceilings below. They are pins
	// rather than settings rows for the reason the bodies pin was: they are
	// turned for one investigation, in a shell, on purpose.
	MaxMBEnvVar = "AFORGE_TRACE_MAX_MB"
	KeepEnvVar  = "AFORGE_TRACE_KEEP"

	// DirName and TraceDirName put the record beside the model-call log rather
	// than under it, because "where does aforge keep what it wrote down" has
	// one answer and this is the second thing in it.
	DirName      = "logs"
	TraceDirName = "trace"
	// EventsFileName is the run's one appended file: tool calls and decisions,
	// JSON Lines, in the order they happened. Call bodies are files of their
	// own beside it, under CallsDirName, because a body is megabytes and a
	// reader wants exactly one of them.
	EventsFileName = "events.jsonl"
	CallsDirName   = "calls"
	// RunFileName is the run's own header, written by the door that opened it:
	// which door, which model was asked for, which build, which folder, when.
	// It is the one file a switched-on run always has, so a folder is never a
	// pile of bodies with nothing saying what the run was.
	RunFileName = "run.json"

	// MaxRunBytes is what ONE run's folder may hold. A quarter of a gigabyte is
	// a very long agentic run with every body kept whole, and it is a ceiling
	// rather than a rotation on purpose: when a run reaches it the record says
	// so on its last line and stops, keeping everything it already had. The
	// alternative — evicting the oldest records to make room — throws away the
	// beginning of the run, which is where the decision that went wrong nearly
	// always is.
	MaxRunBytes = 256 << 20
	// KeepRuns is how many run folders survive. Twenty is a few days of
	// debugging, pruned oldest-first and WHOLE, so a run that is kept is
	// complete and a run that is not is simply gone.
	KeepRuns = 20
)

// on is the switch, and reading it is the whole cost of the record on a run
// that did not ask for one. It is set at init from the environment and by
// [Enable] from a flag or /debug, and it is never turned off again: a person
// who asked for the record mid-session gets it for the rest of the session.
var on atomic.Bool

func init() {
	if envEnabled(os.Getenv) {
		on.Store(true)
	}
}

// envEnabled reads the switch's two spellings. It takes its own getenv so the
// parsing is testable without the process's environment, which init has already
// read by the time any test runs.
func envEnabled(getenv func(string) string) bool {
	return pinOn(getenv(EnvVar)) || pinOn(getenv(BodiesEnvVar))
}

// pinOn is what "set" means for a switch: anything but empty, and not one of
// the three words a person writes when they mean no.
func pinOn(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	return !strings.EqualFold(value, "0") &&
		!strings.EqualFold(value, "false") &&
		!strings.EqualFold(value, "off")
}

// Enabled reports whether this process is keeping the record of EVERY run it
// opens. It is what a door asks before saying so; a single conversation asks
// [EnabledRun], because the answer for one run is not the answer for the
// process.
func Enabled() bool { return on.Load() }

// Enable turns the record on for the rest of the process, and it is what the
// environment pin and the --debug flag call — those two were given to THIS
// PROCESS, so every run it opens is recorded, each into its own folder. There
// is deliberately no way to turn it off again, because the only reason to ask
// for the record is that something already went wrong and half a record is
// worse than none.
func Enable() { on.Store(true) }

// enabled is the set of runs somebody turned the record on for one at a time.
// A mutex-guarded set rather than a second atomic because it is written once
// per /debug — a person typing — and read only after the two atomics below have
// already said there is something in it.
var enabled struct {
	mutex sync.Mutex
	by    map[string]bool
}

// anyRun says the set above is not empty, and it exists purely so that a
// process where nobody has typed /debug pays an atomic load rather than a mutex
// on every feeder call. It is set and never cleared, for the same reason [on]
// is.
var anyRun atomic.Bool

// EnableRun turns the record on for ONE run — the run whose id is on the given
// context — and returns that id, or the empty string where the context belongs
// to no run and there is therefore nothing to record. It is what /debug calls.
//
// THE RUN ID ON THE CONTEXT IS THE SCOPE OF THE SWITCH. One process can hold
// several conversations, so a /debug typed in one of them must not start
// writing another's prompts and replies into a folder its person never asked
// for. The process-wide flip is [Enable], and only the pin and the flag reach
// it.
func EnableRun(ctx context.Context) string {
	run := runOnContext(ctx)
	if run == "" {
		return ""
	}
	enabled.mutex.Lock()
	if enabled.by == nil {
		enabled.by = make(map[string]bool)
	}
	enabled.by[run] = true
	enabled.mutex.Unlock()
	anyRun.Store(true)
	return run
}

// EnabledRun reports whether THIS run is being recorded — because the process
// is recording all of them, or because somebody typed /debug in this one. It is
// what a conversation asks before saying "the record is already on".
func EnabledRun(ctx context.Context) bool {
	if on.Load() {
		return true
	}
	return runIsEnabled(runOnContext(ctx))
}

// runIsEnabled is the set lookup, behind the atomic that makes it free when the
// set is empty.
func runIsEnabled(run string) bool {
	if run == "" || !anyRun.Load() {
		return false
	}
	enabled.mutex.Lock()
	defer enabled.mutex.Unlock()
	return enabled.by[run]
}

// runKey is the context key the run id travels on. It is a private type so
// nothing else in the tree can collide with it.
type runKey struct{}

// NewRunID mints the token every record in one run carries. EIGHT BYTES, which
// is sixteen hex characters, because the id names a FOLDER: two runs that
// minted the same id would open the same folder and [os.MkdirAll] would merge
// them silently, and a record of two runs read as one is worse than no record.
// Four bytes — calllog's width, which this began as — is a collision every few
// tens of thousands of runs on one machine; eight makes it unlikely enough to
// stop reasoning about, and is still short enough to sit in a folder name a
// person is typing.
func NewRunID() string {
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err != nil {
		// A machine with no entropy is not a reason to lose the record. The
		// caller falls back to the process's run, or to nothing.
		return ""
	}
	return hex.EncodeToString(raw[:])
}

// WithRun puts a run id on a context, where every feeder reads it from.
func WithRun(ctx context.Context, id string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, runKey{}, id)
}

// nodeKey is the context key the node travels on, and it is this package's own
// so that internal/trace imports nothing to read it.
type nodeKey struct{}

// WithNode names the piece of work whose records these are — a plan node, a
// leaf, a named errand — for every record written under the returned context.
//
// THE RECORD ANSWERS "WHICH WORK WAS THIS?" AND NOT ONLY "WHICH RUN?". A long
// agentic run is dozens of calls across a plan, and a folder in which they are
// distinguishable only by their timestamps is a folder somebody has to
// reconstruct the plan from. The node is carried rather than passed because the
// feeder sites are deep — a provider retrying a call knows nothing about plans
// — and because it is exactly how the model-call log already carries the same
// fact (provider's WithCallNode, which will call this too, so that one context
// value is set in one place and both records name the same work).
func WithNode(ctx context.Context, node string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	node = strings.TrimSpace(node)
	if node == "" {
		// Setting an empty node would HIDE an outer one, and a record that
		// silently lost the name of its work is the defect this exists to stop.
		return ctx
	}
	return context.WithValue(ctx, nodeKey{}, node)
}

// NodeFrom is the work a record belongs to, or the empty string where nobody
// named one. The emptiness law applies to the file as much as to the screen: a
// record with no node says nothing rather than naming a node called "".
func NodeFrom(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	node, _ := ctx.Value(nodeKey{}).(string)
	return node
}

// processRun is the run this invocation belongs to, set by [Begin].
//
// IT IS THE FALLBACK AND NOT THE SOURCE. The run id belongs on the context and
// every feeder reads it from there; this exists because a door mints the id at
// the top of a process whose deeper layers still start from
// context.Background(), and a record written under a different id — or under no
// id at all — is a record nothing can be joined to. One process is one run for
// every door aforge has, so the fallback cannot be wrong.
var processRun atomic.Value

// Begin mints this invocation's run id at the door, remembers it as the
// process's own, and returns the context carrying it. Every door calls it once,
// switch on or off: minting an id costs four bytes of entropy, and a door that
// only minted one when the switch was already on could not answer /debug.
func Begin(ctx context.Context) context.Context {
	id := NewRunID()
	processRun.Store(id)
	return WithRun(ctx, id)
}

// RunFrom is the run a record belongs to: the id the caller carried, and this
// process's own where a caller could not carry one.
func RunFrom(ctx context.Context) string {
	if id := runOnContext(ctx); id != "" {
		return id
	}
	id, _ := processRun.Load().(string)
	return id
}

// runOnContext is the run the caller actually carried, with NO fallback. It is
// the reading the scope of /debug is decided by: the process's run is a good
// enough answer to "what should this record be named?" and never to "did this
// context's person ask for a record?".
func runOnContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	id, _ := ctx.Value(runKey{}).(string)
	return id
}

// Dir names one run's folder, whether or not anything has been written into it.
// It is what /debug prints when it turns the record on, before there is
// anything to print about.
func Dir(run string) string {
	if run == "" {
		return ""
	}
	return home.Join(DirName, TraceDirName, run)
}

// runs is the process's open recorders, one per run id. A map rather than a
// single recorder because one process can hold several conversations, and two
// runs interleaving their records into one folder is exactly the confusion the
// run id exists to end.
var runs struct {
	mutex sync.Mutex
	by    map[string]*Recorder
}

// runToRecord answers the only question [For] has: which run, if any, do this
// context's records belong to?
//
// A CONTEXT WITH NO RUN OF ITS OWN IS RECORDED ONLY BY THE PROCESS-WIDE SWITCH.
// The fallback to the process's run (see [processRun]) is what lets the deeper
// layers of a headless errand — which still start from context.Background() —
// write into the run their door opened, and under the pin or the flag that is
// exactly right, because those turned every run of this process on. Letting it
// stand for a /debug instead would take a switch somebody threw inside ONE
// conversation and apply it to work that belongs to another, which is the leak
// the run id exists to close.
func runToRecord(ctx context.Context) string {
	if run := runOnContext(ctx); run != "" {
		if on.Load() || runIsEnabled(run) {
			return run
		}
		return ""
	}
	if !on.Load() {
		return ""
	}
	run, _ := processRun.Load().(string)
	return run
}

// For is the recorder every feeder site calls. It returns nil when nobody asked
// for a record — two atomic loads and a nil return — and nil when there is no
// run to belong to, because a record nothing can be joined to is worse than no
// record.
func For(ctx context.Context) *Recorder {
	run := runToRecord(ctx)
	if run == "" {
		return nil
	}
	runs.mutex.Lock()
	defer runs.mutex.Unlock()
	if runs.by == nil {
		runs.by = make(map[string]*Recorder)
	}
	if recorder, ok := runs.by[run]; ok {
		return recorder
	}
	recorder := &Recorder{run: run, dir: Dir(run), max: maxRunBytes(os.Getenv), keep: keepRuns(os.Getenv)}
	runs.by[run] = recorder
	return recorder
}

// Announce prints the one line a door leaves behind: where the record went. It
// prints NOTHING when the run wrote nothing, because a path to a folder that
// does not exist is a door telling somebody to go and look at an empty room —
// and a run nobody switched on has no recorder to have written anything, which
// is why this asks the recorder rather than the switch.
func Announce(ctx context.Context, w io.Writer) {
	runs.mutex.Lock()
	recorder := runs.by[RunFrom(ctx)]
	runs.mutex.Unlock()
	if recorder == nil || !recorder.Wrote() {
		return
	}
	fmt.Fprintf(w, "debug record: %s\n", recorder.dir)
}

// maxRunBytes and keepRuns read the two ceilings, taking their own getenv for
// the reason envEnabled does. A pin that is not a positive number is ignored
// rather than refused: this is a debugging record, and a typo in a shell must
// not be the thing that stops a run.
func maxRunBytes(getenv func(string) string) int64 {
	if mb, err := strconv.ParseInt(strings.TrimSpace(getenv(MaxMBEnvVar)), 10, 64); err == nil && mb > 0 {
		return mb << 20
	}
	return MaxRunBytes
}

func keepRuns(getenv func(string) string) int {
	if keep, err := strconv.Atoi(strings.TrimSpace(getenv(KeepEnvVar))); err == nil && keep > 0 {
		return keep
	}
	return KeepRuns
}

// stderr is seamed so the one failure line is assertable without a test having
// to capture the process's own file descriptor. Same seam, same reason, as
// calllog's.
var stderr io.Writer = os.Stderr

// prune keeps the newest keep folders under the trace root and removes the rest
// whole. It runs when a run folder is opened, which is once per run: the cost is
// one directory listing on the first record of a run, and the alternative — a
// sweep on a timer, or none at all — is either a goroutine nobody asked for or a
// folder that grows without bound.
func prune(root string, keep int) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	type folder struct {
		path string
		age  int64
	}
	var folders []folder
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		folders = append(folders, folder{path: filepath.Join(root, entry.Name()), age: info.ModTime().UnixNano()})
	}
	for len(folders) > keep {
		oldest := 0
		for i, f := range folders {
			if f.age < folders[oldest].age {
				oldest = i
			}
		}
		os.RemoveAll(folders[oldest].path)
		folders = append(folders[:oldest], folders[oldest+1:]...)
	}
}
