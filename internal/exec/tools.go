package exec

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/Agent-Field/aforge-v2/internal/ctxbudget"
	"github.com/Agent-Field/aforge-v2/internal/guard"
	"github.com/Agent-Field/aforge-v2/internal/rtk"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// The base tool set is five tools, and the count is the design. Pull-only
// recall and configured media capabilities are appended at the leaf boundary.
//
// Every definition is re-sent on every turn, and every result stays in context
// for every turn after it arrives, so the question is not "what would be
// convenient" but "what earns its place in a prompt paid for repeatedly".
//
//	sh     one definition buys read, list, search, find, move, curl, and
//	       everything nobody has thought of yet. The shell already composes, so
//	       batching needs no schema help.
//	job    keeps long-lived shell work from blocking the linear loop while
//	       preserving shell composition for logs and readiness monitors.
//	write  because content must not pass through a shell. Emitting a document
//	       through a heredoc means quoting prose, which corrupts it and burns
//	       tokens on escaping.
//	edit   because changing a paragraph should cost a paragraph, not a whole
//	       file re-typed.
//	web    the only capability a shell genuinely lacks. urls is plural because
//	       research is search-once-then-read-several, and batching that turns
//	       five round-trips into one.
const maxCommandSeconds = 120

// The four bounds on what one tool result may cost, stated as the fractions of
// the leaf's observation window they have always secretly been.
//
// Each number below was tuned against a 32KB window — the one an unrecognised
// model still gets, defaultObservationBudget — and each was then written down as
// an absolute, which is the mistake the fill law exists to undo: a model holding
// a million tokens was handed the same twelve kilobytes as one holding thirty-two
// thousand. So the literals stay exactly as they are and change job. They are the
// numerator of a share of the window (see toolBudgetsFor), and they are the named
// fallback for the case where the window is unknown, which is what keeps offline
// behaviour byte-for-byte what it was.
//
// The ratios between them are the part that must not drift, and expressing all
// four against one reference window is what holds them: preview stays below
// spill, spill stays below the result cap, and recall stays the tightest of the
// three that bound a whole result.
const (
	// maxToolResultBytes is the largest a single result may be before its middle
	// is elided — see clamp, which keeps the head and the verdict at the end.
	maxToolResultBytes = 12 << 10

	// spillBytes is where a result stops being worth carrying. Anything larger
	// is written to a file and represented in context by a preview and a path.
	//
	// This is the single highest-leverage bound in the system. A turn resends
	// every earlier observation, so a k-turn loop pays for its results k times
	// over — one 25-turn leaf was 54% of a whole run's input tokens. Spilling
	// caps what any observation can cost, and the model loses nothing it cannot
	// get back: reading part of a file is one `sh` call away.
	//
	// The threshold sits where a typical single document — a source file, a
	// fetched page's article body — still fits whole. Set at 4KB it truncated
	// most of the very files an agent was asked to change, and the agent spent
	// its run re-fetching slices of things it had already read. Aging is the
	// decay pass's job; spill only has to stop the genuinely huge result.
	spillBytes   = 10 << 10
	previewBytes = 4 << 10
	// maxRecallResultBytes bounds persistent memory more tightly than ordinary
	// observations: recall is a map used to choose what to read, not a second
	// copy of the territory.
	maxRecallResultBytes = 8 << 10

	// toolBudgetReference is the window the four numbers above were measured
	// against, and therefore the denominator that turns each of them into a
	// fraction. It is defaultObservationBudget itself rather than a copy of its
	// value: the reference window and the unknown-model window are the same
	// fact, and a second spelling of it would drift.
	toolBudgetReference = defaultObservationBudget
)

// maxResultLines is the other half of every result bound, and the half no byte
// count can express.
//
// A byte cap cuts where the byte falls: mid-line, mid-path, mid-JSON. What the
// model gets back is a half-line it cannot use and cannot address, so it
// re-runs the command to see the whole of it — the exact duplication the caps
// exist to prevent. Lines are the unit the shell speaks in and the unit sed,
// head and tail address, so a cut on a line boundary is a cut the continuation
// command in the notice can name exactly: `sed -n '2001,4000p' <file>` is only
// a true statement about where the reader got to if the reader got to the end
// of a line.
//
// The two caps bind together, whichever is reached first, because output comes
// in two shapes and each defeats the other cap alone. A build log is thin and
// long — two thousand lines of it is well inside any byte budget, and without a
// line cap the model is handed a wall it will not read. A minified bundle or a
// one-line JSON dump is the opposite — a single line of megabytes, inside any
// line cap, which the byte cap is the only thing standing in front of.
//
// Two thousand is the number pi settled on independently, and it is where a
// listing stops being read and starts being searched: past it the right move is
// never "read more", it is grep, and the notice says where to grep.
const maxResultLines = 2000

// resultShape says which END of an over-long result is the end worth keeping.
// It travels on the Result because only the tool that produced it knows, and
// getting it wrong is expensive in a way truncation normally is not: keeping
// the head of a failed build discards the error, and keeping the tail of a
// document discards the part that was asked for.
type resultShape uint8

const (
	// shapeRead is material read forward — a fetched page, a parsed document, a
	// recall map. The beginning is what was asked for, and the rest continues
	// from an offset the notice states.
	shapeRead resultShape = iota
	// shapeCommand is what a command printed. The verdict is at the end: the
	// compiler's error count, the test summary, the traceback, the exit reason.
	// Head-truncating command output is the single most common way a tool loop
	// throws away the only line that mattered.
	shapeCommand
)

// spillRef names the file the whole result was preserved in, so a truncation
// notice can hand the model a command that works instead of an apology. The
// zero value means nothing was saved, which is a real case — the workspace may
// be read-only — and the notice says so rather than naming a path that is not
// there.
type spillRef struct {
	path    string
	bytes   int
	partial bool // the file holds only the beginning of the output
}

// truncation is one over-long result decomposed into everything a notice has to
// state. It is built from the whole text (boundResult) or from the tail of a
// stream (cappedOutput.String), and both render through the same method, which
// is what keeps a streamed command's notice identical to a held one's.
type truncation struct {
	kept      string
	keptLines int // 0 when not one whole line fit and a line had to be cut
	// totalLines and totalBytes describe the whole output, which the streaming
	// collector knows from counters rather than from the bytes it still holds.
	totalLines int
	totalBytes int
	// byteWindow and lineWindow are the two caps that were applied, and they
	// are in the struct because the continuation command is written from them:
	// the next window is the same size as this one.
	byteWindow int
	lineWindow int
	tail       bool
	spill      spillRef
}

// countLines counts lines the way a line-addressing tool counts them: a
// trailing newline terminates the last line rather than starting an empty one,
// so `wc -l`, `sed -n '$p'` and this function agree about which line is last.
func countLines(text string) int {
	if text == "" {
		return 0
	}
	lines := strings.Count(text, "\n")
	if !strings.HasSuffix(text, "\n") {
		lines++
	}
	return lines
}

// truncateHead keeps the first whole lines that fit both caps. When not even
// the first line fits — a minified file, a one-line JSON dump — it keeps the
// first byteLimit bytes of that line and says which line it cut.
func truncateHead(text string, byteLimit, lineLimit int) truncation {
	cut := truncation{totalBytes: len(text), totalLines: countLines(text)}
	end, lines := 0, 0
	for lines < lineLimit {
		next := strings.IndexByte(text[end:], '\n')
		if next < 0 || end+next+1 > byteLimit {
			break
		}
		end += next + 1
		lines++
	}
	if lines == 0 {
		cut.kept = wholeRunesHead(text[:min(byteLimit, len(text))])
		return cut
	}
	cut.kept, cut.keptLines = text[:end], lines
	return cut
}

// truncateTail keeps the last whole lines that fit both caps — the tail rule,
// for output whose verdict is at the end. When the final line alone is over the
// byte cap it keeps that line's last bytes, because on a single-line output the
// end is still where the answer is.
func truncateTail(text string, byteLimit, lineLimit int) truncation {
	cut := truncation{totalBytes: len(text), totalLines: countLines(text)}
	search := len(text)
	if strings.HasSuffix(text, "\n") {
		// The trailing newline belongs to the last line; the line before it is
		// where the search for a boundary starts.
		search--
	}
	start, lines := len(text), 0
	for lines < lineLimit && search >= 0 {
		boundary := strings.LastIndexByte(text[:search], '\n')
		begin := boundary + 1
		if len(text)-begin > byteLimit {
			break
		}
		start, lines = begin, lines+1
		if boundary < 0 {
			break
		}
		search = boundary
	}
	if lines == 0 {
		cut.kept = wholeRunesTail(text[max(0, len(text)-byteLimit):])
		return cut
	}
	cut.kept, cut.keptLines = text[start:], lines
	return cut
}

// render puts the notice where the cut is: in front of a tail, because that is
// where the missing material was, and behind a head for the same reason. A
// model reading downwards meets the explanation at the seam either way.
func (c truncation) render() string {
	if c.tail {
		return c.notice() + "\n" + c.kept
	}
	return strings.TrimRight(c.kept, "\n") + "\n\n" + c.notice()
}

// notice is the whole of what truncation costs the model to recover from: what
// it is holding, of what, and the exact command that reads the rest.
//
// Exactness beats byte-stability here. These lines land in the transcript once,
// at the moment the result arrives, and are then re-sent as cached prefix like
// everything else — so a notice that differs per call costs nothing after that
// call, while a generic example ("for example: sed -n '1,80p' file") costs a
// wasted turn every time a model has to work out the real one.
func (c truncation) notice() string {
	var head string
	switch {
	case c.keptLines == 0 && c.tail:
		head = fmt.Sprintf("[Line %d of %d is longer than the %d bytes that fit; showing its last %d, of %d bytes in all.",
			c.totalLines, c.totalLines, c.byteWindow, len(c.kept), c.totalBytes)
	case c.keptLines == 0:
		head = fmt.Sprintf("[Line 1 of %d is longer than the %d bytes that fit; showing its first %d, of %d bytes in all.",
			c.totalLines, c.byteWindow, len(c.kept), c.totalBytes)
	case c.tail:
		head = fmt.Sprintf("[Showing lines %d-%d of %d, the last %d of %d bytes.",
			c.totalLines-c.keptLines+1, c.totalLines, c.totalLines, len(c.kept), c.totalBytes)
	default:
		head = fmt.Sprintf("[Showing lines 1-%d of %d, the first %d of %d bytes.",
			c.keptLines, c.totalLines, len(c.kept), c.totalBytes)
	}
	if c.spill.path == "" {
		return head + " The rest was not saved — re-run narrowed (grep, head, tail) if you need it.]"
	}
	whole := "Whole output: " + c.spill.path
	if c.spill.partial {
		whole = fmt.Sprintf("First %d bytes: %s", c.spill.bytes, c.spill.path)
	}
	return head + " " + whole + " — " + c.command() + "]"
}

// command is the exact next call, against the file that exists, with the
// numbers this cut actually produced. The window it reads is the same size as
// the window that was kept, so a model that keeps issuing it walks the file at
// a pace its context can hold.
func (c truncation) command() string {
	switch {
	case c.keptLines == 0 && c.tail:
		return fmt.Sprintf("read that line from the start with sh: sed -n '%dp' %s | head -c %d",
			c.totalLines, c.spill.path, c.byteWindow)
	case c.keptLines == 0:
		return fmt.Sprintf("read on with sh: sed -n '1p' %s | cut -b %d-%d",
			c.spill.path, len(c.kept)+1, len(c.kept)+c.byteWindow)
	case c.tail:
		first := c.totalLines - c.keptLines + 1
		window := min(c.lineWindow, first-1)
		return fmt.Sprintf("read the start with sh: sed -n '1,%dp' %s", window, c.spill.path)
	default:
		return fmt.Sprintf("continue with sh: sed -n '%d,%dp' %s",
			c.keptLines+1, c.keptLines+c.lineWindow, c.spill.path)
	}
}

// boundResult renders one whole result under both caps, and reports whether
// anything was cut. It is the held-in-memory twin of cappedOutput, which does
// the same arithmetic on a stream; the two must agree byte for byte, and a test
// holds them to it.
func boundResult(text string, carry, keep int, shape resultShape, spill spillRef) (string, bool) {
	if len(text) <= carry && countLines(text) <= maxResultLines {
		return text, false
	}
	if keep <= 0 || keep >= carry {
		keep = carry / 2
	}
	var cut truncation
	if shape == shapeCommand {
		cut = truncateTail(text, keep, maxResultLines)
		cut.tail = true
	} else {
		cut = truncateHead(text, keep, maxResultLines)
	}
	cut.byteWindow, cut.lineWindow, cut.spill = keep, maxResultLines, spill
	return cut.render(), true
}

// toolBudgets is one leaf's four result bounds in bytes, resolved once.
//
// Once is the whole point and it is a cache property rather than a performance
// one. These numbers decide how long a result is, results are the bulk of the
// transcript, and a bound that moved mid-run would rewrite messages the provider
// is otherwise serving warm. They are settled when the toolbox is built, from a
// window that cannot change for the life of the leaf, and read from there.
type toolBudgets struct {
	result  int // one whole result, before clamp elides its middle
	spill   int // past this a result goes to a file instead
	preview int // what stays in context when it does
	recall  int // the recall map, which is bounded tighter than the territory
}

// toolBudgetsFor turns the leaf's context window into its four bounds. A window
// nobody could name yields exactly the old literals — see ctxbudget: unknown is
// never treated as small, it is treated as unmeasured, and every consumer names
// what it used to do.
//
// The share is taken of the WORKING SET rather than of the raw window, because
// the memory that has to HOLD these results is taken of the working set — see
// observationWindow, working-set clamped since the fill law arrived. Sizing the
// result off one pot and the memory off a smaller one was a mismatch with a
// measured cost: on a 200k model a single sh result was permitted 75,696 bytes
// against a 105,856-byte observation window — 71% of the leaf's whole memory in
// one call — and on a 1M model the same call was permitted 795,696 bytes
// against that identical window, 7.5x more than could ever be held. Two results
// put the window over budget on arrival, so the decay pass fired every turn,
// rewrote the middle of the transcript every turn, and nothing behind the fade
// line was ever served warm again. That is a structural contributor to the
// 5.76x duplication this leaf was measured at.
//
// The convergence this introduces is the honest part rather than the cost: past
// the working set a result cap stops growing because the memory holding it
// stopped growing. It is a dial and not a belief — AFORGE_WORKING_SET raises
// the window and these four bounds together, which is the only coherent way to
// raise either.
func toolBudgetsFor(contextTokens int) toolBudgets {
	budget := ctxbudget.For(contextTokens).WithinWorkingSet().WithFloor(observationFixedFloorTokens)
	return toolBudgets{
		result:  budget.Share(maxToolResultBytes, toolBudgetReference, maxToolResultBytes),
		spill:   budget.Share(spillBytes, toolBudgetReference, spillBytes),
		preview: budget.Share(previewBytes, toolBudgetReference, previewBytes),
		recall:  budget.Share(maxRecallResultBytes, toolBudgetReference, maxRecallResultBytes),
	}
}

// Result is one tool's answer. A failure is a Result, never a Go error: the
// model has to see what went wrong to fix it, and aborting the loop over a
// mistyped path throws away every turn that came before.
type Result struct {
	Content string
	IsError bool
	// reportedJobs prevents a status-bearing result from being memoised and
	// replayed later as if its transient background state were still current.
	reportedJobs bool
	// shape is which end of this result survives if it has to be cut. The
	// default is shapeRead because most results are read forward; the tools
	// whose output ends in a verdict say so. See resultShape.
	shape resultShape
	// bounded says this result already went through the caps and carries its
	// own truncation notice — a streamed command, which had to be bounded as it
	// arrived because nothing could hold the whole of it. Bounding it a second
	// time at the turn boundary would cut the notice off the end of itself.
	bounded bool
	// Followup carries multimodal content that must reach the next model turn.
	// The ordinary text result is still emitted first so tool-call pairing
	// remains valid on every OpenAI-compatible backend.
	Followup []ai.ContentPart
	Usage    Usage
}

func errorf(format string, args ...any) Result {
	return Result{Content: fmt.Sprintf(format, args...), IsError: true}
}

// Toolbox executes tool calls against one workspace on behalf of one node.
type Toolbox struct {
	workspace *Workspace
	// leaf is who this toolbox works for, and it names every file the leaf's
	// machinery writes: the artifact bucket, the spilled observations, the
	// background job logs. It is a string because the identity a caller holds is
	// not always a number — see Task.NodeKey.
	leaf    string
	web     *Web
	history *store.Store
	media   *MediaTools
	jobs    *jobRegistry
	// budgets are what this leaf's model can afford to carry, fixed at
	// construction and never recomputed. See toolBudgets.
	budgets toolBudgets
	// spills is atomic because a turn's tool calls execute concurrently, and
	// two large results spilling at once must not race the counter into the
	// same file name.
	spills atomic.Int64

	// armed names the optional capability families whose schemas this leaf is
	// currently carrying. It is guarded because a turn's tool calls run
	// concurrently and Definitions is read between turns.
	armedMu sync.Mutex
	armed   map[string]bool
	// armedOrder is the same set in the order it was armed, and it exists for
	// the prompt cache rather than for bookkeeping. Emitting the armed schemas
	// in a fixed order would insert a newly armed family in FRONT of one already
	// in hand — a rewrite of the middle of the tool block, which re-bills every
	// byte behind it — where emitting them in arrival order can only ever append
	// at the tail. See Definitions.
	armedOrder []string

	// share is the worker's one-line channel to the rest of its job. Nil for a
	// job with no siblings — the schema is only carried where somebody is
	// listening. Installed from Task.Share at run start.
	share func(line string) error
}

// The optional capability families, and the whole of why they are optional.
//
// Every definition is re-sent on every turn of every leaf. Measured on a
// six-cell benchmark, the media and document schemas were ~1,112 tokens of
// each leaf turn's ~3,778-token fixed floor — 18% of every input token the run
// spent — and not one of those cells could have used a single one of them. A
// bugfix cannot generate a video. A release note cannot read a PDF that does
// not exist.
//
// The answer is not to remove the tools; the resident's charter, watch and
// media journeys genuinely need them. It is to stop paying for them by
// default. A leaf carries the core loop plus one small tool that says what
// else exists; asking for a family arms it for the next turn and every turn
// after. The judgement of whether the work needs a camera stays with the model
// doing the work, made at the moment it knows — not predicted for it at
// compile time by a call that has never seen the workspace.
//
// The cost is honest and worth naming: a job that does need media pays one
// extra turn and one prefix-cache invalidation at the moment it arms. A job
// that does not — the overwhelming majority — pays nothing at all, ever.
const (
	FamilyMedia    = "media"
	FamilyDocument = "documents"
)

// The tools each family carries. Arming is per tool rather than per family so
// a structural signal can admit exactly what it justifies: an attached
// screenshot is a reason to be able to look at an image, not a reason to be
// able to score a film.
var familyTools = map[string][]string{
	FamilyMedia:    {"generate_image", "generate_music", "generate_video", "speak", "view_image"},
	FamilyDocument: {"read_document"},
}

// Arm admits named capability families or individual tools for the rest of
// this leaf's life. It is how a structural fact about the assignment — an
// attached image, an attached document — buys back exactly the schema it
// justifies before the first turn, and how the discovery tool answers a
// worker that asked.
func (t *Toolbox) Arm(names ...string) {
	t.armedMu.Lock()
	defer t.armedMu.Unlock()
	if t.armed == nil {
		t.armed = map[string]bool{}
	}
	for _, name := range names {
		if tools, ok := familyTools[name]; ok {
			for _, tool := range tools {
				t.armOne(tool)
			}
			continue
		}
		t.armOne(name)
	}
}

// armOne records one newly armed tool once, keeping the arrival order the tool
// block is emitted in. Called with armedMu held.
func (t *Toolbox) armOne(name string) {
	if t.armed[name] {
		return
	}
	t.armed[name] = true
	t.armedOrder = append(t.armedOrder, name)
}

func (t *Toolbox) isArmed(name string) bool {
	t.armedMu.Lock()
	defer t.armedMu.Unlock()
	return t.armed[name]
}

// armedInOrder is a snapshot of what this leaf holds, oldest first.
func (t *Toolbox) armedInOrder() []string {
	t.armedMu.Lock()
	defer t.armedMu.Unlock()
	order := make([]string, len(t.armedOrder))
	copy(order, t.armedOrder)
	return order
}

// offered names the families this leaf could still arm — configured on the
// brain, and not already in hand. It is prose only: it phrases the error a
// worker reads when it calls a tool it has not got. It decides nothing about
// the tool list, because a tool list that moves when a family is armed is the
// defect this file spent a whole commit removing.
func (t *Toolbox) offered() []string {
	var families []string
	if t.media != nil && t.media.Provider != nil && !t.isArmed("generate_image") {
		families = append(families, FamilyMedia)
	}
	if t.media != nil && t.media.DocumentClient != nil && !t.isArmed("read_document") {
		families = append(families, FamilyDocument)
	}
	return families
}

// configuredFamilies says whether this machine has anything to arm at all. It
// reads the brain's wiring and never the armed set, which is exactly what makes
// it constant for the whole life of a leaf — and identical across every leaf of
// a run, since they all share one brain.
func (t *Toolbox) configuredFamilies() bool {
	return t.media != nil && (t.media.Provider != nil || t.media.DocumentClient != nil)
}

// capabilitiesDefinition is the whole discovery surface: one tool, one
// argument, and an enum the code owns because the families are the code's own
// grouping of its own tools. The model reads its own work and decides; nothing
// here matches a phrase against the brief.
//
// Every byte of it is frozen, and that is the fix rather than the style.
//
// The description used to be assembled from whichever families were still
// unarmed, and the enum with it. Tool definitions ride at the front of every
// request, ahead of the entire transcript, so the moment a worker armed media
// the sentence describing the families changed, the prefix diverged at the tool
// block, and the whole prompt behind it was re-billed cold. Arming is supposed
// to cost exactly one invalidation — the new schemas appended at the end of the
// tool list, which is the price the design already accepted and named. This was
// a second, larger one nobody had costed, paid at the front instead of the back.
//
// So the enum stays full width too, including a family this machine may not
// have configured. Asking for one that is missing is answered by capabilities
// itself, in a sentence that tells the worker to do the job without it and say
// so — one wasted call in the rare case, against a definition block that never
// moves for any leaf on any turn.
//
// It no longer retires, either, and that was the last moving part. Retiring it
// once everything on offer was armed removed a definition from the MIDDLE of
// the tool list — every schema behind it shifted, so the turn that finished
// arming paid a second full-prompt invalidation on top of the one arming
// already costs. The tool is ~90 tokens; the block it was displacing is the
// whole prompt. So it stays for the life of any leaf whose machine has a family
// configured, and a worker that asks for something it already holds is answered
// in one cheap line — see capabilities.
func capabilitiesDefinition() ai.ToolDefinition {
	return define("capabilities",
		"Load tools you do not have yet; they arrive on your next turn. Ask once, only if the work needs one. "+
			"media: generate images, music, video, speech; look at an image. "+
			"documents: read a PDF, DOCX or PPTX into text.",
		map[string]any{
			"need": map[string]any{"type": "string", "enum": []string{FamilyMedia, FamilyDocument}},
		}, "need")
}

// capabilities arms what was asked for and says what arrived. The reply names
// the tools rather than the family, because the next turn's schema list is
// what the worker will actually be holding.
func (t *Toolbox) capabilities(args map[string]any) Result {
	need := strings.TrimSpace(stringArg(args, "need"))
	tools, known := familyTools[need]
	if !known {
		// The families, not what is left to arm: the enum is the same on every
		// turn now, so the correction has to name the same thing the enum does
		// or the two disagree in front of the model.
		return errorf("no capability family named %q. Available: %s, %s", need, FamilyMedia, FamilyDocument)
	}
	switch need {
	case FamilyMedia:
		if t.media == nil || t.media.Provider == nil {
			return errorf("media generation is not configured on this machine — do what the assignment needs without it and say plainly in your answer that it could not be done")
		}
	case FamilyDocument:
		if t.media == nil || t.media.DocumentClient == nil {
			return errorf("document parsing is not configured on this machine — do what the assignment needs without it and say plainly in your answer that it could not be done")
		}
	}
	// Already in hand: the cheap no-op that lets the definition stay in the
	// prompt forever. A worker that asks twice costs one short tool result at
	// the end of the transcript, which is appended and therefore free of any
	// prefix invalidation; retiring the tool to prevent the second ask would
	// rewrite the tool block instead, and that is paid for by every remaining
	// turn of the leaf.
	if t.holdsAll(tools) {
		return Result{Content: "Already loaded — " + strings.Join(tools, ", ") +
			" are in your tool list now. Use them; do not ask again."}
	}
	t.Arm(need)
	return Result{Content: "Loaded for your next turn and every turn after: " + strings.Join(tools, ", ") +
		". Their full descriptions are in your tool list from here on."}
}

// holdsAll reports that every named tool is already armed.
func (t *Toolbox) holdsAll(tools []string) bool {
	for _, tool := range tools {
		if !t.isArmed(tool) {
			return false
		}
	}
	return len(tools) > 0
}

// shareLine hands one line to the rest of the job. The write itself is the
// caller's closure — the toolbox knows nothing about where notes live, which
// is what keeps this package free of the store's job topology.
func (t *Toolbox) shareLine(args map[string]any) Result {
	if t.share == nil {
		return errorf("this work has no other workers to tell")
	}
	line := strings.TrimSpace(stringArg(args, "line"))
	if line == "" {
		return errorf("share needs the one line the others should read")
	}
	// One line means one line: a paragraph shared to every sibling is paid for
	// in every sibling's every remaining turn.
	if len(line) > shareLineBytes {
		clipped := line[:shareLineBytes]
		for len(clipped) > 0 && !utf8.ValidString(clipped) {
			clipped = clipped[:len(clipped)-1]
		}
		line = clipped + "…"
	}
	if err := t.share(line); err != nil {
		return errorf("could not pass that along: %v", err)
	}
	return Result{Content: "Passed along. The other workers read it between turns."}
}

// shareLineBytes bounds one shared line. It is a bound on cost, not on
// content: every byte here is re-read by every sibling on every remaining
// turn, so a note pays rent everywhere at once.
const shareLineBytes = 300

// NewToolbox builds a toolbox for a caller that cannot say what its model
// holds. That is not a guess about a small model — it is the honest unknown, and
// it resolves to the bounds this package has always used.
func NewToolbox(workspace *Workspace, leaf string, web *Web) *Toolbox {
	return newToolbox(workspace, leaf, web, nil, nil, 0)
}

// NewToolboxWithStore adds persistent recall to the generic toolbox. A nil
// store deliberately collapses to NewToolbox so one-shot leaves retain the
// base-definition prompt.
func NewToolboxWithStore(workspace *Workspace, leaf string, web *Web, history *store.Store) *Toolbox {
	return newToolbox(workspace, leaf, web, history, nil, 0)
}

// newToolbox is the one constructor, and contextTokens is the fact every other
// bound in this file is derived from. It is passed in rather than looked up for
// the same reason Linear.WithContextLength exists: the surface owns the catalog
// and hands facts down, and a leaf must never fail to run because a metadata
// endpoint was dark.
func newToolbox(workspace *Workspace, leaf string, web *Web, history *store.Store,
	media *MediaTools, contextTokens int) *Toolbox {
	budgets := toolBudgetsFor(contextTokens)
	return &Toolbox{workspace: workspace, leaf: leaf, web: web, history: history, media: media,
		budgets: budgets, jobs: newJobRegistry(workspace, leaf, budgets.result)}
}

// mediaModelArgDescription teaches the model argument in one breath: the slot
// default is right for routine work, and "best" is for the times quality is
// the point. It is resent every turn, so it stays one sentence.
const mediaModelArgDescription = `optional model: omit for the default, "best" when the user asked for quality or this is the final deliverable, or a model name`

// toolGuidelines is the standing guidance each tool contributes to the system
// prompt, keyed by the tool that owns it — and it is owned by the tool rather
// than by the prompt for one reason: a leaf that is not holding the tool must
// not be reading the instruction.
//
// The monolithic prompt cannot express that. Every leaf pays for every sentence
// in it, including the paragraphs about capabilities this leaf does not have
// and will never arm, and the only way to add guidance for one tool is to widen
// the text every other leaf reads. Assembling it from the toolbox that was
// actually built inverts that: two tools, two lines.
//
// What lives here is deliberately narrow — the behaviour of a tool that a model
// cannot infer from its schema and that costs a wasted turn to learn by doing.
// The prompt's own paragraphs are not moved here and must not be: they are the
// shared warm prefix, a sibling owns their wording, and rewriting them to prove
// a mechanism would re-bill every leaf in flight to say the same thing
// differently. This is the mechanism, carrying the lines the recent tool
// changes actually created.
var toolGuidelines = map[string]string{
	"sh": "Long command output is cut to its LAST lines — where the error and the summary are — " +
		"and the whole of it is saved to a file the result names. Read the part you need from that " +
		"file with the command the notice gives you; do not re-run the command to see the beginning.",
	"edit": "To change several places in one file, pass them all in one edit call as edits: every old " +
		"is matched against the file as it is now, so you do not have to reason about how the earlier " +
		"edits in the same call moved the text.",
}

// Guidelines returns the guidance lines for the tools this leaf is actually
// holding, in the order the tools are defined. It is read once, when the system
// message is assembled: the set of tools a leaf holds is fixed at that point
// except for arming, and arming appends schemas rather than rewriting the
// standing text — a system message that changed mid-run would cost the whole
// prompt at full price on the turn it changed.
func (t *Toolbox) Guidelines() []string {
	var lines []string
	for _, definition := range t.Definitions() {
		if line, ok := toolGuidelines[definition.Function.Name]; ok {
			lines = append(lines, definition.Function.Name+": "+line)
		}
	}
	return lines
}

// Definitions are what the model sees. Descriptions are terse because they are
// resent every turn, but each one states the thing an agent gets wrong without
// being told.
//
// The ORDER is load-bearing and is the second half of the cache-shape fix. Tool
// definitions ride at the very front of every request, ahead of the whole
// transcript, so the first byte of this block that differs between two calls
// re-bills everything behind it at full price. The list is therefore built in
// three strata, widest agreement first:
//
//  1. the five tools every leaf on every machine always has, in a fixed order;
//  2. the discovery tool, present for the life of any leaf whose machine has an
//     optional family configured — a property of the brain, never of what this
//     leaf has armed, so it never appears or vanishes mid-run;
//  3. per-leaf conditionals (recall, share) and then the armed schemas.
//
// Only stratum 3 can move, and it can only ever grow at the tail: arming a
// family appends, so the invalidation is bounded by the schemas actually added
// rather than by everything that used to sit behind the thing that moved.
func (t *Toolbox) Definitions() []ai.ToolDefinition {
	definitions := []ai.ToolDefinition{
		define("sh", "Run a shell command in the workspace. Use it to read, list, search, and inspect. cmd is one command string (chain with && and pipes), or an array of commands run in order, stopping at the first failure. For INDEPENDENT commands, prefer separate sh calls in the same turn — they run at the same time. For servers, builds over a minute, or watch loops, set bg:true and use the job tool — do not block on them.", map[string]any{
			"cmd": prop("string", "shell command, or an array of commands run serially"),
			"t":   prop("integer", "timeout seconds; default 60, or 900 with bg"),
			"bg":  prop("boolean", "start as a background job"),
		}, "cmd"),
		define("job", "Check or wait on background jobs. Prefer one wait over repeated checks. Compose monitors from bg shell loops (e.g. bg: until curl -s :8080/health; do sleep 1; done — then wait on it). Kill servers when done testing; anything still running dies with the leaf. To keep a server running after the task, use keep — never nohup.", map[string]any{
			"id":   prop("integer", "job id; omit to list jobs"),
			"wait": prop("integer", "seconds to wait for exit, maximum 120"),
			"kill": prop("boolean", "terminate the job process group"),
			"keep": map[string]any{"type": "object", "description": "request promotion to a user-owned service", "properties": map[string]any{
				"name":   prop("string", "short service name"),
				"health": prop("string", "port:5173, url:http://..., or cmd:..."),
			}, "required": []string{"name", "health"}},
		}),
		define("write", "Write a file with exact content. Use this for any deliverable prose; never emit documents through sh.", map[string]any{
			"path": prop("string", "workspace-relative path"),
			"text": prop("string", "full file content"),
		}, "path", "text"),
		define("edit", "Replace exact text in a file. Pass edits to change several places in one call. Far cheaper than rewriting a long file to change part of it.", map[string]any{
			"path": prop("string", "workspace-relative path"),
			"edits": map[string]any{"type": "array", "description": "several replacements, each matched against the current file",
				"items": map[string]any{"type": "object", "properties": map[string]any{
					"old": prop("string", "exact text to replace, must appear once"),
					"new": prop("string", "replacement text"),
				}, "required": []string{"old", "new"}}},
			"old": prop("string", "exact text to replace, must appear once"),
			"new": prop("string", "replacement text"),
			// Only the path is required, because there are two legal shapes: one
			// old/new pair, or the edits array. A schema that demanded both would
			// make the batched form invalid on its face.
		}, "path"),
		define("web", "Search the web, or fetch pages as text. Pass q to search. Pass urls to fetch several pages in one call, which is much faster than one at a time.", map[string]any{
			"q":    prop("string", "search query"),
			"urls": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "page urls to fetch"},
			"n":    prop("integer", "max search results, default 6"),
		}),
	}
	// Stratum 2: the door to the optional families. Its presence follows the
	// machine's wiring and nothing else, so it is in the same slot on every turn
	// of every leaf of a run, or on none of them.
	if t.configuredFamilies() {
		definitions = append(definitions, capabilitiesDefinition())
	}
	// Stratum 3 begins here: everything below is per-leaf or per-arming, and is
	// only ever appended.
	if t.history != nil {
		definitions = append(definitions, define("recall", "Search folded work and the notebook. Recall gives the map, not the territory: use the returned digest to choose what matters, then read the returned pointer paths with sh for the verbatim details. terms are free text; scope_cues are optional workspace or file paths.", map[string]any{
			"terms":      prop("string", "words describing the prior work or lesson"),
			"scope_cues": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "optional workspace or file paths"},
			"limit":      prop("integer", "maximum fold and notebook hits, default 5, maximum 10"),
		}, "terms"))
	}
	if t.share != nil {
		definitions = append(definitions, define("share", "Tell the other workers on this job one line they need: a discovery about the material, a pitfall, a decision they must match. It reaches them between their turns. Only what changes how someone else acts — never progress reports, never your own status.", map[string]any{
			"line": prop("string", "one sentence the rest of the job needs"),
		}, "line"))
	}
	// The optional schemas ride the prompt only once this leaf has a reason to
	// carry them: a structural one it was armed with before turn 1, or the
	// worker's own request through the discovery tool.
	//
	// They are emitted in ARMING order, and that is the whole of what makes
	// arming an append. A fixed order looks tidier and is wrong: with the media
	// schemas written above read_document, a leaf that armed documents first and
	// media second would have five definitions inserted IN FRONT of the schema
	// it was already carrying, which is a rewrite of the middle of the block and
	// costs the entire transcript behind it. Emitting in the order they arrived
	// means the newest schema is always last, whatever the route in.
	//
	// Each optional schema is admitted on its own name, not on its family's,
	// so an attached screenshot buys view_image without also buying a video
	// generator it has no use for.
	catalog := t.optionalDefinitions()
	for _, name := range t.armedInOrder() {
		if definition, available := catalog[name]; available {
			definitions = append(definitions, definition)
		}
	}
	return definitions
}

// optionalDefinitions is every schema this machine could arm, keyed by name.
// A family the brain has not wired produces no entries, so a leaf that armed a
// tool the machine cannot serve carries no schema for it — and is answered in
// prose by capabilities instead.
func (t *Toolbox) optionalDefinitions() map[string]ai.ToolDefinition {
	catalog := map[string]ai.ToolDefinition{}
	if t.media == nil {
		return catalog
	}
	if t.media.Provider != nil {
		for _, definition := range []ai.ToolDefinition{
			define("generate_image", "Generate one or more images into the workspace media directory. reference_paths may name existing workspace images for image-to-image work.", map[string]any{
				"prompt":          prop("string", "what to generate"),
				"n":               prop("integer", "number of images, default 1, maximum 10"),
				"size":            prop("string", "optional image size or aspect ratio"),
				"model":           prop("string", mediaModelArgDescription),
				"reference_paths": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "workspace image paths to use as references"},
			}, "prompt"),
			// No format and no duration: the composing endpoint takes neither,
			// and the format argument that used to be here promised a choice
			// the wire never offered (internal/provider/music.go).
			define("generate_music", "Compose music into the workspace media directory. The prompt describes the music — genre, instruments, tempo, mood — not lyrics to sing and not text to read out; use speak for a voiceover. There is no length argument: the model writes a piece of its own choosing, around a minute, and the call costs the same however long it turns out.", map[string]any{
				"prompt": prop("string", "a description of the music to compose"),
				"model":  prop("string", mediaModelArgDescription),
			}, "prompt"),
			define("generate_video", "Generate a video into the workspace media directory. This call waits for the asynchronous provider job to finish, for up to ten minutes. The first two reference_paths become first/last frames; any remaining images are style references.", map[string]any{
				"prompt":          prop("string", "what to generate"),
				"duration":        prop("integer", "optional duration in seconds"),
				"resolution":      prop("string", "optional resolution such as 480p, 720p, or 1080p"),
				"aspect_ratio":    prop("string", "optional aspect ratio such as 16:9"),
				"model":           prop("string", mediaModelArgDescription),
				"reference_paths": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "workspace image paths for first/last frames and style guidance"},
			}, "prompt"),
			define("speak", "Synthesize speech as an MP3 in the workspace media directory.", map[string]any{
				"text":  prop("string", "text to speak"),
				"voice": prop("string", "optional voice, default alloy"),
				"model": prop("string", mediaModelArgDescription),
			}, "text"),
			define("view_image", "Look at a workspace image. If the current model cannot see, a vision model looks and reports back — pass question for a targeted check.", map[string]any{
				"path":     prop("string", "workspace-relative image path"),
				"question": prop("string", "optional targeted question about the image"),
			}, "path"),
		} {
			catalog[definition.Function.Name] = definition
		}
	}
	if t.media.DocumentClient != nil {
		catalog["read_document"] = define("read_document", "Read a PDF into text. Free and local when possible; scanned documents escalate to OCR through the rail. Repeat reads are cached.", map[string]any{
			"path":     prop("string", "workspace-relative PDF, DOCX, or PPTX path"),
			"pages":    prop("string", "optional PDF page range such as 1-5"),
			"question": prop("string", "optional question the worker will answer from the extracted text"),
		}, "path")
	}
	return catalog
}

func reflexPromotionDefinition() ai.ToolDefinition {
	return define("promote", "Stop this reflex and hand the original request to a full job. Use immediately when the action is not one obvious reversible step. partial says what you learned or changed before stopping.", map[string]any{
		"partial": prop("string", "useful partial result or discovery for the full job to build on"),
	}, "partial")
}

// requestSplitDescription is the whole of the ownable-subject test, in the one
// place a model reads before deciding.
//
// The test is written as a thing to try rather than as a definition, because
// every definition of "independent" a prompt has offered was agreed with and
// then ignored: a model asked to divide will divide, and the cheapest division
// is the procedure it was about to follow, relabelled as parts. The
// knowing-nothing clause is what that relabelling cannot survive — "write the
// report" cannot be started by someone who has not seen "gather the data", and
// saying so out loud is what makes the model notice.
//
// The terminal sentence is not a warning, it is the contract. A leaf that
// requested a split and then kept working would produce a partial its own
// children were planned against, and the two would be the same work paid for
// twice.
const requestSplitDescription = "Say that this assignment is really several separate jobs, and hand them over. " +
	"Use it only when working on it has REVEALED that — a directory that turned out to hold twelve independent " +
	"cases, a question that turned out to be four unrelated questions. " +
	"The test for a part: could one agent take it from start to finished knowing nothing of what the other parts " +
	"produced? If a part needs another part's output, or reads as a stage of one procedure — gather, then analyse, " +
	"then write up — it is not a part, and this assignment is one job. Two parts minimum. " +
	"Calling this ENDS your run immediately: you do not continue afterwards, the parts do. " +
	"Everything you have already produced is handed to them, so say it in your reply first if it is not written down."

// requestSplitDefinition is the cooperative division tool. It is on the belt
// only in swarm mode, and it is added by Linear rather than by the toolbox for
// the same reason promote is: the call is intercepted and never executed, so a
// Toolbox entry for it would be a handler that can never run.
func requestSplitDefinition() ai.ToolDefinition {
	return define("request_split", requestSplitDescription, map[string]any{
		"parts": map[string]any{
			"type":        "array",
			"description": "the separate jobs this assignment turned out to hold — at least two",
			"items": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"title":   prop("string", "what this part is called, a few words"),
					"summary": prop("string", "one line saying what this part is for"),
					"brief":   prop("string", "the whole assignment for this part, written for an agent that will read nothing else"),
				},
				"required": []string{"title", "summary", "brief"},
			},
		},
		"evidence": prop("string", "what you read or found that revealed the division — the file, the count, the shape of the thing"),
	}, "parts", "evidence")
}

// requestedSplit reads the leaf's division out of a turn's calls. The
// request_split call is intercepted by Linear and never reaches the toolbox.
//
// Arguments that will not parse still end the run. The model has said the
// assignment is several jobs, and that finding is the expensive half; a request
// that arrives malformed is refused by Valid one layer up, which delivers the
// partial — the same ending a governor refusal gives it, and a far better one
// than handing the loop back to a worker that has just announced it is stopping.
func requestedSplit(calls []ai.ToolCall) (*SplitRequest, bool) {
	for _, call := range calls {
		if call.Function.Name != "request_split" {
			continue
		}
		var args struct {
			Parts []struct {
				Title   string `json:"title"`
				Summary string `json:"summary"`
				Brief   string `json:"brief"`
			} `json:"parts"`
			Evidence string `json:"evidence"`
		}
		if err := json.Unmarshal([]byte(call.Function.Arguments), &args); err != nil {
			return &SplitRequest{}, true
		}
		request := &SplitRequest{Evidence: strings.TrimSpace(args.Evidence)}
		for _, part := range args.Parts {
			request.Parts = append(request.Parts, SplitPart{
				Title:   strings.TrimSpace(part.Title),
				Summary: strings.TrimSpace(part.Summary),
				Brief:   strings.TrimSpace(part.Brief),
			})
		}
		return request, true
	}
	return nil, false
}

// reflexPromotion reads the executor's explicit larger-than-it-looked verdict.
// The promote call is intercepted by Linear and never reaches the toolbox.
func reflexPromotion(calls []ai.ToolCall) (string, bool) {
	for _, call := range calls {
		if call.Function.Name != "promote" {
			continue
		}
		var args struct {
			Partial string `json:"partial"`
		}
		if err := json.Unmarshal([]byte(call.Function.Arguments), &args); err != nil {
			return "", true
		}
		return strings.TrimSpace(args.Partial), true
	}
	return "", false
}

// Execute dispatches one call. An unknown name is answered with the valid list
// rather than refused, because a model that guessed a tool name can recover
// from being told the real ones and cannot recover from a dead loop.
func (t *Toolbox) Execute(ctx context.Context, name string, arguments string) (outcome Result) {
	// A tool that panics is one bad call, not a dead worker. The model reads
	// the fault the way it reads any other tool error and picks another move;
	// the stack goes to the log, where it is useful.
	defer func() {
		if recovered := recover(); recovered != nil {
			_ = guard.Note("exec/tool "+name, recovered)
			outcome = errorf("internal fault in this tool call — recorded to the log: %v. Try a different approach.", recovered)
		}
	}()
	var args map[string]any
	if strings.TrimSpace(arguments) != "" {
		if err := json.Unmarshal([]byte(arguments), &args); err != nil {
			return t.finishResult(errorf("arguments were not valid JSON: %v", err))
		}
	}
	var result Result
	switch name {
	case "sh":
		result = t.sh(ctx, args)
	case "job":
		result = t.job(args)
	case "write":
		result = t.write(args)
	case "edit":
		result = t.edit(args)
	case "web":
		result = t.webCall(ctx, args)
	case "recall":
		result = t.recall(args)
	case "generate_image":
		result = t.generateImage(ctx, args)
	case "generate_music":
		result = t.generateMusic(ctx, args)
	case "generate_video":
		result = t.generateVideo(ctx, args)
	case "speak":
		result = t.speak(ctx, args)
	case "view_image":
		result = t.viewImage(ctx, args)
	case "read_document":
		result = t.readDocument(ctx, args)
	case "capabilities":
		result = t.capabilities(args)
	case "share":
		result = t.shareLine(args)
	default:
		available := "sh, job, write, edit, web"
		if t.history != nil {
			available += ", recall"
		}
		// Only what this leaf is actually holding is named, plus the door to
		// the rest. A model told about a tool it does not have is a model that
		// will call it next turn and be told the same thing again.
		for _, family := range []string{FamilyMedia, FamilyDocument} {
			for _, tool := range familyTools[family] {
				if t.isArmed(tool) {
					available += ", " + tool
				}
			}
		}
		if families := t.offered(); len(families) > 0 {
			available += ", capabilities (loads: " + strings.Join(families, ", ") + ")"
		}
		result = errorf("no tool named %q. Available: %s", name, available)
	}
	return t.finishResult(result)
}

// finishResult is the single turn boundary for every tool, including optional
// recall and media tools. With no jobs it returns spill's value untouched.
func (t *Toolbox) finishResult(result Result) Result {
	result = t.spill(result)
	if report := t.jobs.report(); report != "" {
		result.Content = clamp(result.Content+"\n\n"+report, t.budgets.result)
		result.reportedJobs = true
	}
	return result
}

type recallToolFact struct {
	NodeID   string         `json:"node_id,omitempty"`
	Scope    string         `json:"scope"`
	Kind     store.FactKind `json:"kind"`
	Body     string         `json:"body"`
	Pointers []string       `json:"pointers"`
	Age      string         `json:"age"`
}

type recallToolResponse struct {
	Folds    []store.RecallHit `json:"folds"`
	Notebook []recallToolFact  `json:"notebook"`
}

func (t *Toolbox) recall(args map[string]any) Result {
	if t.history == nil {
		return errorf("recall is not available without an attached store")
	}
	terms := strings.TrimSpace(stringArg(args, "terms"))
	cues := stringsArg(args, "scope_cues")
	if terms == "" && len(cues) == 0 {
		return errorf("recall needs terms or scope_cues")
	}
	limit := intArg(args, "limit", 5)
	if limit <= 0 {
		limit = 5
	}
	if limit > 10 {
		limit = 10
	}
	folds, err := t.history.Recall(terms, cues, limit)
	if err != nil {
		return errorf("recall folds: %v", err)
	}
	facts, err := t.history.SearchFacts(store.FactQuery{Cues: recallFactCues(cues), Terms: terms, Limit: limit})
	if err != nil {
		return errorf("recall notebook: %v", err)
	}

	response := recallToolResponse{Folds: make([]store.RecallHit, 0), Notebook: make([]recallToolFact, 0)}
	for _, hit := range folds {
		hit.Intent = recallClip(hit.Intent, 512)
		hit.Digest = recallClip(hit.Digest, 2<<10)
		if len(hit.Pointers) > 8 {
			hit.Pointers = hit.Pointers[:8]
		}
		for index := range hit.Pointers {
			hit.Pointers[index] = recallClip(hit.Pointers[index], 512)
		}
		candidate := response
		candidate.Folds = append(append([]store.RecallHit(nil), response.Folds...), hit)
		if recallJSONFits(candidate, t.budgets.recall) {
			response = candidate
		}
	}
	now := time.Now().UTC()
	for _, fact := range facts {
		item := recallToolFact{NodeID: fact.NodeID, Scope: recallClip(fact.Scope, 256), Kind: fact.Kind,
			Body: recallClip(fact.Body, store.MaxFactBytes), Pointers: t.foldPointers(fact.NodeID),
			Age: store.AgeLabel(fact.Time, now)}
		candidate := response
		candidate.Notebook = append(append([]recallToolFact(nil), response.Notebook...), item)
		if recallJSONFits(candidate, t.budgets.recall) {
			response = candidate
		}
	}
	encoded, err := json.Marshal(response)
	if err != nil {
		return errorf("encode recall: %v", err)
	}
	return Result{Content: string(encoded)}
}

func (t *Toolbox) foldPointers(nodeID string) []string {
	for nodeID != "" && nodeID != store.RootID {
		node, ok, err := t.history.Node(nodeID)
		if err != nil || !ok {
			return nil
		}
		if node.FoldRoot {
			pointers := append([]string(nil), node.FoldPointers...)
			if len(pointers) > 8 {
				pointers = pointers[:8]
			}
			for index := range pointers {
				pointers[index] = recallClip(pointers[index], 512)
			}
			return pointers
		}
		nodeID = node.Parent
	}
	return nil
}

func recallFactCues(cues []string) []string {
	seen := make(map[string]bool)
	result := make([]string, 0, len(cues)*3)
	add := func(cue string) {
		cue = strings.TrimSpace(cue)
		if cue == "" || seen[cue] {
			return
		}
		seen[cue] = true
		result = append(result, cue)
	}
	for _, cue := range cues {
		add(cue)
		if strings.Contains(cue, ":") {
			continue
		}
		cleaned := filepath.ToSlash(filepath.Clean(cue))
		add("file:" + cleaned)
		add("repo:" + cleaned)
		if parent := filepath.ToSlash(filepath.Dir(cleaned)); parent != "." && parent != cleaned {
			add("repo:" + parent)
		}
	}
	return result
}

func recallJSONFits(response recallToolResponse, limit int) bool {
	encoded, err := json.Marshal(response)
	return err == nil && len(encoded) <= limit
}

func recallClip(value string, limit int) string {
	value = strings.TrimSpace(value)
	if len(value) <= limit {
		return value
	}
	return wholeRunesHead(value[:limit-len("...")]) + "..."
}

// wholeRunesHead and wholeRunesTail are the two halves of the same rule: a
// window cut at an arbitrary byte offset lands mid-character about half the
// time in any non-English text, and the replacement character it leaves behind
// rides every turn of the leaf that reads it. Every byte budget in this package
// is a budget, not a boundary, so both trim back to the nearest whole
// character rather than refusing to cut.
func wholeRunesHead(window string) string {
	// A head window can only close on an incomplete sequence, which is exactly
	// what DecodeLastRuneInString reports as a one-byte error. A genuine U+FFFD
	// in the text decodes at its true width and is left alone.
	for attempt := 0; attempt < utf8.UTFMax && len(window) > 0; attempt++ {
		if char, size := utf8.DecodeLastRuneInString(window); char != utf8.RuneError || size > 1 {
			break
		}
		window = window[:len(window)-1]
	}
	return window
}

func wholeRunesTail(window string) string {
	// A tail window can only open on a continuation byte, and a character is at
	// most UTFMax bytes long, so at most UTFMax-1 of them can precede the first
	// whole one.
	for attempt := 0; attempt < utf8.UTFMax && len(window) > 0; attempt++ {
		if window[0]&0xC0 != 0x80 {
			break
		}
		window = window[1:]
	}
	return window
}

// spill moves a large result out of context and leaves a pointer to it, cut on
// a line boundary at the end that matters for the tool that produced it, with
// the exact command that reads the rest.
//
// Errors are never spilled: they are usually short, and the whole value of an
// error is that the model reads it immediately rather than going to fetch it. A
// long error is the failed command's own output, and that one arrives already
// bounded — see runShell, where the same caps are applied to the stream.
func (t *Toolbox) spill(result Result) Result {
	if result.IsError || result.bounded {
		return result
	}
	if len(result.Content) <= t.budgets.spill && countLines(result.Content) <= maxResultLines {
		return result
	}
	// The file is written before the cut so the notice can name it, and it holds
	// exactly the bytes the line numbers in the notice were counted against.
	var ref spillRef
	if relative, ok := t.writeObs(t.spillName(), result.Content); ok {
		ref = spillRef{path: relative, bytes: len(result.Content)}
	}
	bounded, cut := boundResult(result.Content, t.budgets.spill, t.budgets.preview, result.shape, ref)
	// The result is amended rather than rebuilt: a spilled document read still
	// carries the tokens it cost, and a spilled image read still carries the
	// content part the next turn needs.
	result.Content, result.bounded = bounded, cut
	return result
}

// spillName is the next observation file name for this leaf. The counter is
// atomic because a turn's tool calls execute concurrently, and it is shared
// with the streaming collector so a teed command log and a spilled result can
// never land on the same name.
func (t *Toolbox) spillName() string {
	return fmt.Sprintf("%s-%d.txt", pathSlug(t.leaf), t.spills.Add(1))
}

// openSpill creates the file a running command's whole output is teed to. It is
// handed to the collector as a closure because opening it is a decision the
// collector makes — on the first byte past what it can carry, and never for the
// overwhelming majority of commands whose output fits.
func (t *Toolbox) openSpill() (io.WriteCloser, string, bool) {
	full, shown, err := t.workspace.ScratchPath(filepath.Join(obsDir, t.spillName()))
	if err != nil {
		return nil, "", false
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return nil, "", false
	}
	file, err := os.Create(full)
	if err != nil {
		return nil, "", false
	}
	return file, shown, true
}

// decaySpill preserves a decaying observation's full body under the
// workspace's observation directory. The file is named by tool call id, so
// writing is naturally idempotent — the decayer additionally guarantees it is
// invoked at most once per id.
func (t *Toolbox) decaySpill(toolCallID, body string) (string, bool) {
	return t.writeObs(fmt.Sprintf("%s-decay-%s.txt", pathSlug(t.leaf), safeName(toolCallID)), body)
}

// writeObs writes one observation file and returns the path to name it by. It
// is the one place spilled bytes land, shared by the size-triggered spill and
// the decay pass.
func (t *Toolbox) writeObs(name, content string) (string, bool) {
	full, shown, err := t.workspace.ScratchPath(filepath.Join(obsDir, name))
	if err != nil {
		return "", false
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return "", false
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		return "", false
	}
	return shown, true
}

var unsafeName = regexp.MustCompile(`[^A-Za-z0-9_-]+`)

// safeName makes a tool call id usable as a file name. Ids differ at the tail,
// so that is the part kept when one is too long.
func safeName(id string) string {
	cleaned := unsafeName.ReplaceAllString(id, "")
	if len(cleaned) > 40 {
		cleaned = cleaned[len(cleaned)-40:]
	}
	if cleaned == "" {
		cleaned = "x"
	}
	return cleaned
}

func (t *Toolbox) sh(ctx context.Context, args map[string]any) Result {
	command := stringArg(args, "cmd")
	if command == "" {
		// An array is an explicit serial script: each step runs only when
		// the one before it succeeded, exactly like hand-written a && b.
		if list, ok := args["cmd"].([]any); ok {
			steps := make([]string, 0, len(list))
			for _, step := range list {
				if text, ok := step.(string); ok && strings.TrimSpace(text) != "" {
					steps = append(steps, strings.TrimSpace(text))
				}
			}
			command = strings.Join(steps, " && ")
		}
	}
	if command == "" {
		return errorf("sh needs cmd")
	}
	if boolArg(args, "bg") {
		// A background job is deliberately never compressed. Its output is
		// watched rather than read — readiness loops grep the log, the job tool
		// tails it, a promoted service keeps writing to it long after the leaf
		// is gone — and all of that is programmatic, which is indistinguishable
		// from parsing. There is also no fallback available once a job has
		// started, and a compressor with no way back is not one we can offer.
		return t.startBackground(ctx, command, args)
	}
	seconds := intArg(args, "t", 60)
	if seconds <= 0 || seconds > maxCommandSeconds {
		seconds = maxCommandSeconds
	}
	// A command writes files, and until this line nothing in the product knew
	// it. The registry behind Outcome.Artifacts was populated by write and edit
	// alone, so a chart rendered by a script under this tool was invisible to
	// the files footer, to the delivery gate and to every later node. The mark
	// is taken here, before anything runs, and read back once the command is
	// done — see produced.go for why one sweep afterwards rather than two.
	mark := producedMark(time.Now())
	defer t.recordProduced(mark)
	// rtk compresses what the command said before the model has to pay for it,
	// on every later turn as well as this one. It only ever stands in for the
	// plain command when it can be trusted to have said the same thing: see
	// trustworthy, which sends anything doubtful back to the shell itself.
	if tool, ok := rtk.Available(); ok {
		if wrapped, class := tool.Wrap(ctx, command); class != rtk.ClassNone {
			run := t.runShell(ctx, wrapped, seconds, tool.Path)
			if run.trustworthy(class) {
				return run.result(seconds)
			}
			if rtk.Failed(run.exitCode, run.body) {
				tool.Ban(command)
			}
		}
	}
	return t.runShell(ctx, command, seconds, "").result(seconds)
}

// shellRun is one command's whole outcome, kept separate from the Result it
// becomes so a wrapped run can be weighed and discarded before it is spoken.
type shellRun struct {
	body     string
	err      error
	exitCode int
	timedOut bool
	detached bool
	// bounded says the body was cut as it streamed and already carries its own
	// truncation notice naming the file the whole output went to.
	bounded bool
}

// trustworthy asks whether a wrapped run may stand as the answer.
//
// A timeout stands as it is: it belongs to the command, not to the wrapping,
// and waiting for it a second time would spend the leaf's budget twice to learn
// nothing. rtk failing to run what it was handed never stands. Beyond that a
// check is believed whatever it exits with — a failing test suite is reporting,
// and its compressed failure is the most valuable output rtk produces — while a
// read that fails is asked again plain, because "no such file" has to reach the
// model as the shell's own sentence and asking twice costs nothing.
func (r shellRun) trustworthy(class rtk.Class) bool {
	if r.timedOut || r.detached {
		return true
	}
	if rtk.Failed(r.exitCode, r.body) {
		return false
	}
	return class == rtk.ClassCheck || r.exitCode == 0
}

// result turns a finished command into what the model reads. Every shape of it
// is shapeCommand: what a command has to say is at the END of what it printed —
// the error, the summary, the exit reason — so if this has to be cut, the cut
// comes off the front. bounded rides along so the turn boundary does not cut an
// already-cut body a second time.
func (r shellRun) result(seconds int) Result {
	if r.timedOut {
		out := errorf("command timed out after %ds. Partial output:\n%s", seconds, r.body)
		out.shape, out.bounded = shapeCommand, r.bounded
		return out
	}
	if r.detached {
		// The command itself finished; something it started in the background
		// kept the output pipe open until the grace ran out. That is a
		// completed command with a detached child, not a failure.
		return Result{Content: r.body + "\n(a background process the command started was left running detached)",
			shape: shapeCommand, bounded: r.bounded}
	}
	if r.err != nil {
		// The exit status matters less than the output; a build failure's value
		// is entirely in what it printed.
		return Result{Content: fmt.Sprintf("exit: %v\n%s", r.err, r.body), IsError: true,
			shape: shapeCommand, bounded: r.bounded}
	}
	if strings.TrimSpace(r.body) == "" {
		return Result{Content: "(no output)", shape: shapeCommand}
	}
	return Result{Content: r.body, shape: shapeCommand, bounded: r.bounded}
}

// runShell runs one command to completion in the workspace. rtkBin is the
// resolved rtk when this is a wrapped run and empty otherwise; a rewritten line
// calls rtk by bare name, so its shelf goes on PATH here.
func (t *Toolbox) runShell(ctx context.Context, command string, seconds int, rtkBin string) shellRun {
	runCtx, cancel := context.WithTimeout(ctx, time.Duration(seconds)*time.Second)
	defer cancel()
	var environment []string
	// The login shell may rewrite inherited PATH while reading its profile.
	// Export inside that shell so a shelf is added without changing the
	// benchmarked bare command path, which still runs with no Env set at all.
	if t.history != nil {
		if bin, err := store.SkillsBinDir(); err == nil {
			environment = os.Environ()
			environment = replaceEnv(environment, "AFORGE_SKILLS_BIN", bin)
			command = "export PATH=\"${AFORGE_SKILLS_BIN:?}:$PATH\"\n" + command
		}
	}
	if rtkBin != "" {
		if environment == nil {
			environment = os.Environ()
		}
		environment = replaceEnv(environment, "AFORGE_RTK_BIN", filepath.Dir(rtkBin))
		command = "export PATH=\"${AFORGE_RTK_BIN:?}:$PATH\"\n" + command
	}

	cmd := exec.CommandContext(runCtx, "bash", "-lc", command)
	cmd.Dir = t.workspace.Root()
	if environment != nil {
		cmd.Env = environment
	}
	// A command that leaves a background child sharing its stdout used to hang
	// the whole run: killing bash at the timeout is not enough, because Wait
	// blocks until every inherited pipe writer exits, and a scheduler goroutine
	// stuck there wedges the graph silently and forever. The process group
	// makes the timeout kill reach grandchildren, and WaitDelay force-closes
	// the pipes shortly after bash itself is gone for anything that survives —
	// a stuck tool call must cost its timeout, never the run.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	cmd.WaitDelay = 3 * time.Second
	// Collected rather than read whole. A command inside a fifteen-minute call
	// may print hundreds of megabytes and all but twelve kilobytes of them are
	// discarded a line later; the collector keeps only the part that survives,
	// including the nudge stripping, which it does a line at a time so that the
	// elided-byte count is counted on what a reader would have seen.
	var strip func([]byte) bool
	if rtkBin != "" {
		strip = func(start []byte) bool {
			line := string(start)
			return rtk.StripNudge(line) != line
		}
	}
	// The collector's two limits are the leaf's own: carry what a result may
	// cost the transcript, keep what survives when the command printed more
	// than that. Everything past the first is teed to a file as it arrives, so
	// the notice that ends up in context names a command that actually works.
	collected := newCappedOutput(strip, t.budgets.spill, t.budgets.preview, t.openSpill)
	cmd.Stdout, cmd.Stderr = collected, collected
	err := cmd.Run()
	run := shellRun{body: collected.String(), bounded: collected.truncated(),
		err: err, exitCode: exitCode(cmd, err)}
	run.timedOut = runCtx.Err() == context.DeadlineExceeded
	run.detached = errors.Is(err, exec.ErrWaitDelay)
	return run
}

func exitCode(cmd *exec.Cmd, err error) int {
	if err == nil {
		return 0
	}
	if cmd.ProcessState != nil {
		if code := cmd.ProcessState.ExitCode(); code >= 0 {
			return code
		}
	}
	return -1
}

func (t *Toolbox) write(args map[string]any) Result {
	path, text := stringArg(args, "path"), stringArg(args, "text")
	if path == "" {
		return errorf("write needs path")
	}
	full, err := t.workspace.Resolve(path)
	if err != nil {
		return errorf("%v", err)
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return errorf("could not create directory: %v", err)
	}
	if err := os.WriteFile(full, []byte(text), 0o644); err != nil {
		return errorf("could not write %s: %v", path, err)
	}
	t.workspace.Record(t.leaf, full)
	return Result{Content: fmt.Sprintf("wrote %s (%d bytes)", path, len(text))}
}

// replacement is one exact-text substitution. A call carries one of these or
// several; the several are the point.
type replacement struct {
	old string
	new string
}

// edit replaces exact text in a file, one block or several in a single call.
//
// Several is the shape the work actually has. A function rename touches five
// places in a file, and five separate edit calls are five round-trips, five
// tool definitions re-sent, five results in the transcript forever, and — the
// expensive part — five chances for the file to have moved under an offset the
// model was remembering. Batched, it is one call, one read, one write, and one
// sentence back.
//
// The semantics that make batching safe are the ones pi settled on and they are
// all consequences of a single rule: every old is matched against the ORIGINAL
// file, never against the file as the previous edits in this same call left it.
// A model writing five edits is looking at one file — the one it read — and
// matching against a moving target would mean the third edit had to be written
// against a file that has never existed anywhere. From that rule the other two
// follow: each old must appear exactly once in the original, because otherwise
// "the" match is a guess; and no two matched spans may overlap, because two
// edits to the same bytes have no defined result and applying them in call
// order would silently pick one.
//
// Nothing is written unless every edit resolves. A file half-edited by a call
// that then failed is the worst outcome available here — it is broken in a way
// the model cannot see from the error — so the failure is total and the message
// says which edit failed and how to fix it.
func (t *Toolbox) edit(args map[string]any) Result {
	path := stringArg(args, "path")
	edits, argErr := editList(args)
	if path == "" || argErr != "" {
		if argErr == "" {
			argErr = "edit needs path"
		}
		return errorf("%s", argErr)
	}
	full, err := t.workspace.Resolve(path)
	if err != nil {
		return errorf("%v", err)
	}
	data, err := os.ReadFile(full)
	if err != nil {
		return errorf("could not read %s: %v", path, err)
	}
	body := string(data)
	// Every span is located in the original before any of them is applied, which
	// is what makes the edits independent and what lets an overlap be a refusal
	// rather than a race between two Replace calls.
	type span struct{ start, end, index int }
	spans := make([]span, 0, len(edits))
	for index, one := range edits {
		switch matches := strings.Count(body, one.old); matches {
		case 1:
			at := strings.Index(body, one.old)
			spans = append(spans, span{start: at, end: at + len(one.old), index: index})
		case 0:
			return errorf("%s is not in %s. Read the file and match it byte for byte, whitespace included.",
				editNoun(index, len(edits)), path)
		default:
			return errorf("Found %d occurrences of %s in %s — an edit must match exactly one. Provide more surrounding context so it matches once.",
				matches, editNoun(index, len(edits)), path)
		}
	}
	sort.Slice(spans, func(a, b int) bool { return spans[a].start < spans[b].start })
	for i := 1; i < len(spans); i++ {
		if spans[i].start < spans[i-1].end {
			return errorf("edits %d and %d overlap the same text in %s — they cannot both apply. Combine them into one edit.",
				spans[i-1].index+1, spans[i].index+1, path)
		}
	}
	var updated strings.Builder
	updated.Grow(len(body))
	at := 0
	for _, s := range spans {
		updated.WriteString(body[at:s.start])
		updated.WriteString(edits[s.index].new)
		at = s.end
	}
	updated.WriteString(body[at:])
	if err := os.WriteFile(full, []byte(updated.String()), 0o644); err != nil {
		return errorf("could not write %s: %v", path, err)
	}
	t.workspace.Record(t.leaf, full)
	// One sentence, and deliberately not a diff.
	//
	// The diff is the most tempting thing to return here and the most expensive:
	// it is the size of the change, it lands in the transcript, and it is then
	// re-sent on every remaining turn of the leaf — to a model that already
	// knows what it asked for, because it wrote both sides of it a moment ago.
	// The human-facing surfaces still get the diff; they read it from the trace,
	// which is where per-call detail belongs. See account.go.
	return Result{Content: fmt.Sprintf("Successfully replaced %d block(s) in %s.", len(spans), path)}
}

// editList reads either shape of the call: the single old/new pair the tool has
// always taken, or an edits array. Both are accepted because the single form is
// most real calls and a schema that forced an array around one edit would spend
// tokens on brackets in every one of them.
//
// A call carrying both is taken at its word and applies both, in that order,
// rather than quietly dropping one. It is a shape a model does produce — the
// pair it started writing plus the batch it then decided on — and dropping
// either half would be an edit the model believes it made. Applying both is
// safe for exactly the reason the batch is safe: every old is still matched
// once against the original and overlapping spans are still refused, so a
// duplicate written twice is caught rather than applied twice.
func editList(args map[string]any) ([]replacement, string) {
	var edits []replacement
	if old := stringArg(args, "old"); old != "" {
		edits = append(edits, replacement{old: old, new: stringArg(args, "new")})
	}
	raw, _ := args["edits"].([]any)
	for index, item := range raw {
		fields, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Sprintf("edit %d is not an object with old and new", index+1)
		}
		old := stringArg(fields, "old")
		if old == "" {
			return nil, fmt.Sprintf("edit %d has no old text to match", index+1)
		}
		edits = append(edits, replacement{old: old, new: stringArg(fields, "new")})
	}
	if len(edits) == 0 {
		return nil, "edit needs old and new, or an edits array of {old, new} pairs"
	}
	return edits, ""
}

// editNoun names the edit an error is about, in the words the caller used: a
// single edit is "that exact text", one of several is numbered, because a model
// holding five edits has to know which one to fix.
func editNoun(index, total int) string {
	if total == 1 {
		return "that exact text"
	}
	return fmt.Sprintf("edit %d's text", index+1)
}

func (t *Toolbox) webCall(ctx context.Context, args map[string]any) Result {
	if t.web == nil {
		return errorf("web is not configured (set EXA_API_KEY)")
	}
	query := stringArg(args, "q")
	urls := stringsArg(args, "urls")
	if query == "" && len(urls) == 0 {
		return errorf("web needs q or urls")
	}
	var sections []string
	if query != "" {
		found, err := t.web.Search(ctx, query, intArg(args, "n", 6))
		if err != nil {
			return errorf("search failed: %v", err)
		}
		sections = append(sections, found)
	}
	if len(urls) > 0 {
		sections = append(sections, t.web.Fetch(ctx, urls))
	}
	// Whole, for the same reason a document is handed over whole: pages are read
	// forward, and the turn boundary cuts them on a line boundary with the rest
	// preserved and a command that reads on. See Toolbox.spill.
	return Result{Content: strings.Join(sections, "\n\n")}
}

// clamp bounds a COMPOSED report at both ends — a job listing, a status block,
// an upstream error body — at whatever the consuming leaf can afford. It is no
// longer what bounds a tool result: that is boundResult, which cuts on line
// boundaries at the end that matters and hands back the command that reads the
// rest. This one stays for the assembled strings, where there is no single
// underlying file to point at and the pieces are already short.
//
// Keeping only the head is the obvious implementation and the wrong one: a
// command's most valuable line is usually its last, because that is where the
// error is. Cutting the middle keeps the shape of the output and the verdict at
// the end, and says plainly how much went missing so the model can go looking
// for it if it matters.
func clamp(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	head := wholeRunesHead(text[:limit*2/3])
	tail := wholeRunesTail(text[len(text)-(limit-limit*2/3):])
	return head +
		fmt.Sprintf("\n\n... [%d bytes elided] ...\n\n", len(text)-len(head)-len(tail)) +
		tail
}

func define(name, description string, properties map[string]any, required ...string) ai.ToolDefinition {
	parameters := map[string]any{"type": "object", "properties": properties}
	if len(required) > 0 {
		parameters["required"] = required
	}
	return ai.ToolDefinition{Type: "function", Function: ai.ToolFunction{
		Name: name, Description: description, Parameters: parameters,
	}}
}

func prop(kind, description string) map[string]any {
	return map[string]any{"type": kind, "description": description}
}

func stringArg(args map[string]any, key string) string {
	if value, ok := args[key].(string); ok {
		return value
	}
	return ""
}

func boolArg(args map[string]any, key string) bool {
	value, _ := args[key].(bool)
	return value
}

func replaceEnv(environment []string, key, value string) []string {
	prefix := key + "="
	replaced := make([]string, 0, len(environment)+1)
	for _, entry := range environment {
		if !strings.HasPrefix(entry, prefix) {
			replaced = append(replaced, entry)
		}
	}
	return append(replaced, prefix+value)
}

func intArg(args map[string]any, key string, fallback int) int {
	switch value := args[key].(type) {
	case float64:
		return int(value)
	case int:
		return value
	}
	return fallback
}

func stringsArg(args map[string]any, key string) []string {
	raw, ok := args[key].([]any)
	if !ok {
		return nil
	}
	values := make([]string, 0, len(raw))
	for _, item := range raw {
		if text, ok := item.(string); ok && strings.TrimSpace(text) != "" {
			values = append(values, text)
		}
	}
	return values
}
