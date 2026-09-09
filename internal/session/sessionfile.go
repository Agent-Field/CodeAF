package session

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/filelock"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/roles"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// The session file is JSONL: one header line, then one line per COMPLETED
// message and per compaction pass. Append-only and line-oriented, so a crash
// mid-write costs the last line and nothing before it, and a resume is a
// forward read with no rewrite.
//
// Content is flattened to text. A session's messages are text — the tools
// return text and the person types text — and keeping the part array would
// buy fidelity for a shape that does not occur while making every line
// unreadable to the person the transcript is for.
//
// The one part that is not text is an image, and it is journaled as a REFERENCE
// rather than as content: path, digest, media type. A 4MB photo base64'd into a
// JSONL line is how a session file dies — it becomes unreadable to a person, it
// is re-read into memory on every resume, and it grows the file by more than the
// whole conversation around it. The bytes are already on disk at a path this
// machine can read, so the journal writes where they are and what they were, and
// the replay checks the second before trusting the first (see [journalPart]).
//
// The file is locked while it is open. Two aforge processes resuming the same
// path would both replay it and both append, and their lines interleave into
// one transcript that belongs to neither — the last writer's resume reads the
// other's messages as its own. A resume picks the newest file by mtime, so the
// two windows converge on the same path by default rather than by accident.
// openSessionFile therefore takes a non-blocking exclusive flock and the loser
// gets ErrSessionLocked, which names the file so the surface can offer "open it
// where it is" or "start a new one".

const sessionFileVersion = 1

// ErrSessionLocked is what a second open of a live session file returns. Match
// it with errors.Is; the *SessionLockedError it wraps carries the path.
var ErrSessionLocked = errors.New("session file is open in another aforge")

// errNewerFormat is the ONE refusal a reading of a session file can carry that
// is about the file's FORMAT rather than about this machine's luck with it — a
// header declaring a version above [sessionFileVersion].
//
// It is a sentinel because a caller has to be able to tell it apart. "This was
// written by a newer aforge" is a true and useful thing to say to somebody, and
// saying it about a disk that went away, or a read that was cut off, would be a
// confident wrong answer sending them to upgrade a build that is already fine.
// Match it with errors.Is; the *newerFormatError it wraps carries the path and
// both versions, and prints the sentence people are shown and the manual quotes
// (internal/manual/chat/sessions-and-rewind.md).
var errNewerFormat = errors.New("session file was written by a newer aforge")

// newerFormatError names the file and the two format versions.
type newerFormatError struct {
	Path    string
	Version int
	Reads   int
}

func (e *newerFormatError) Error() string {
	return fmt.Sprintf("session file: %s was written by a newer aforge (format version %d; this build reads %d)",
		e.Path, e.Version, e.Reads)
}

func (e *newerFormatError) Unwrap() error { return errNewerFormat }

// SessionLockedError names the file another process holds.
type SessionLockedError struct{ Path string }

func (e *SessionLockedError) Error() string {
	return fmt.Sprintf("session file: %s is open in another aforge", e.Path)
}

func (e *SessionLockedError) Unwrap() error { return ErrSessionLocked }

type sessionHeader struct {
	Type      string `json:"type"`
	Version   int    `json:"version"`
	ID        string `json:"id"`
	Cwd       string `json:"cwd"`
	Model     string `json:"model"`
	Timestamp string `json:"timestamp"`
}

type sessionEntry struct {
	Type       string        `json:"type"`
	Role       string        `json:"role,omitempty"`
	Content    string        `json:"content,omitempty"`
	ToolCalls  []ai.ToolCall `json:"toolCalls,omitempty"`
	ToolCallID string        `json:"toolCallId,omitempty"`
	// Reasoning fields are the assistant continuation exactly as it arrived.
	// They stay beside the message rather than inside Content so a resumed tool
	// loop preserves both the wire contract and what the person actually saw.
	ReasoningField   string          `json:"reasoningField,omitempty"`
	Reasoning        string          `json:"reasoning,omitempty"`
	ReasoningDetails json.RawMessage `json:"reasoningDetails,omitempty"`
	// ReasoningModel is the slug the working above was produced under, so a
	// resumed conversation that has since switched models does not replay
	// one endpoint's encrypted thinking to another. Absent in older files.
	ReasoningModel string `json:"reasoningModel,omitempty"`

	// Parts are the message's non-text content parts as durable references, in
	// the order they sit in the message AFTER its text. Absent on every message
	// that is only words, which is nearly all of them — a reader of an old file
	// and a reader of a new one see the same lines for the same conversation.
	Parts []journalPart `json:"parts,omitempty"`

	// Note marks a user-role line the SESSION wrote rather than the person: a
	// task's completion note and the other news that rides the steering queue
	// (agent.go's [Agent.enqueueNote]). The message is user-role because that is
	// what the model must read it as, and this is the one bit that says who
	// actually said it.
	//
	// It exists for the replay. Without it a resumed conversation draws the
	// harness's own line with the person's "›" in front of it — words in their
	// mouth they never typed, and the opposite of what the live surface does with
	// the same note ([Agent.wakeLocked], tui3's startFollow). It is absent from
	// every file written before it existed, and those lines replay exactly as
	// they always did.
	Note      bool           `json:"note,omitempty"`
	ReplyTags []TaskReplyTag `json:"replyTags,omitempty"`

	// Caption is a `caption` line: the sentence the cheap narrator wrote about
	// one BATCH of tool calls while it ran, and the family of work it named
	// (caption.go, actioncategory.go).
	//
	// IT IS ITS OWN LINE BECAUSE IT ARRIVES AFTER THE MESSAGE IT IS ABOUT. The
	// assistant message carrying a batch is journaled BEFORE the batch runs; the
	// narration lands half a second later at the earliest, so there is no line
	// open to write it into. It is anchored instead — [journalCaption.CallID] is
	// the batch's first call — which is the same identity a live surface pairs
	// on, so the record and the stream name the step the same way.
	//
	// WITHOUT IT A REOPENED CONVERSATION LOSES THE SENTENCE AND THE MARK. The
	// deterministic composite recomposes something from the tool names, so the
	// row is not blank — but `test` becomes `run` and "starting the local
	// server" becomes "running 1 command", which is a conversation that reads
	// differently on Tuesday than it did on Monday. Absent from every line that
	// is not one, and from every file written before it existed.
	Caption *journalCaption `json:"caption,omitempty"`

	// Deliveries names the durable deliveries this line is the record of
	// ([durableDelivery]): a landing's news, identified by session, task,
	// attempt and ending. It is what lets a resumed session tell a landing it
	// already recorded from one it still owes, without trusting a checkpoint
	// that may not have been written. Absent from every line that is not one,
	// and from every file written before it existed.
	Deliveries []string `json:"deliveries,omitempty"`

	// Steer marks the two lines that belong to STEERING — a sentence the person
	// typed into a turn that was already running (steer.go).
	//
	// On a `message` line it says that this user message did not open the turn it
	// sits in: it was spliced into it, and the model read it as part of the same
	// question. On a `steer` line — a line that is not a message at all — it says
	// the opposite: those words were sent at that turn, no request of it ever
	// carried them, and they went on to ask their own question a moment later.
	//
	// Absent from every line written before it existed, and from every line that
	// is not one of those two, so a file this build reads and a file it writes
	// tell the same conversation.
	Steer *journalSteer `json:"steer,omitempty"`

	// Compaction fields.
	//
	// Summary is LEGACY ONLY: it is the prose a summarizer wrote for every
	// marker up to the pass that stopped calling one, and it is still read so a
	// session compacted last week resumes as itself. Nothing writes it now.
	Summary      string `json:"summary,omitempty"`
	TokensBefore int    `json:"tokensBefore,omitempty"`

	// Stubbed and Folded are what the current pass did (loop.go): how many tool
	// results became pointers to their own bytes, and how many assistant
	// messages went into one marker line.
	//
	// They are the RECORD and never the instruction. A modern marker rebuilds
	// nothing by itself — the pass re-journals the whole rebuilt window behind
	// it, so replay reads the stubs and the fold marker as ordinary message
	// lines and reconstructs the transcript verbatim rather than from counts.
	Stubbed int `json:"stubbed,omitempty"`
	Folded  int `json:"folded,omitempty"`

	// Window is HOW MANY MESSAGE LINES THE PASS RE-JOURNALED BEHIND THIS MARKER
	// — the length of the rebuilt window [sessionFile.appendCompaction] writes
	// out after it.
	//
	// It is the one thing a reader cannot work out for itself, and it is what
	// makes the conversation above a marker readable. The lines above are the
	// original ones and the lines below are the pass's rewritten copy OF THE SAME
	// CONVERSATION, so a reader that showed both would draw the whole session
	// twice; it needs to know where the copy ends and the conversation carries on.
	// Nothing in the file says that but this number.
	//
	// ABSENT MEANS UNKNOWN, not zero. Every marker written before this field
	// existed — and every legacy marker, whose kept tail was re-journaled with no
	// count either — leaves the region above it unplaceable, and a reader must
	// then decline to offer it rather than guess (see [compactionOverlap]). It is
	// deliberately NOT derived from Stubbed and Folded: those are the RECORD of
	// what the pass did, nothing rebuilds from them, and a length derived from a
	// count is a length that drifts the day the pass changes.
	Window int `json:"window,omitempty"`

	// Dropped is how many messages a rewind removed (rewind.go). It is a COUNT
	// rather than a cut position because the file is append-only and positions
	// in it are not positions in the replayed transcript: a compaction marker
	// earlier in the file collapses everything before it into one message. A
	// count is applied to whatever the replay is holding when it reaches the
	// line, which is exactly the list the rewind was taken against.
	Dropped int `json:"dropped,omitempty"`

	// Title is the session's name (title.go). It is its own line rather than a
	// header field because the header is written ONCE, when the file is
	// created, and the name is not known until the first turn has been
	// answered. A line is also how a name can be rewritten later without any
	// reader having to rewrite the file: the replay takes the LAST title line.
	Title      string `json:"title,omitempty"`
	ShortTitle string `json:"shortTitle,omitempty"`

	// Usage is what one COMPLETED turn — or one auxiliary call beside it — cost,
	// and it is on its own line rather than on the assistant message that ended
	// the turn: a turn is several requests and several messages, and hanging the
	// bill on one of them would be a number that is true of the line above it and
	// of nothing else. Absent from every line that is not a seal, and from every
	// file written before it existed.
	Usage *journalUsage `json:"usage,omitempty"`

	// Call is ONE request's accounting, beside the seal rather than inside it.
	// Absent from every line that is not a call line, and from every file
	// written before it existed.
	Call *journalCall `json:"call,omitempty"`

	// Error is ONE CALL THAT FAILED (see [journalError]). It is the call line's
	// opposite number and it exists for the same reason: a turn that died on a
	// provider refusal left this file saying only that it had ended.
	Error *journalError `json:"error,omitempty"`

	// Mark is ONE reading taken at a checkpoint mark, and Ceiling is what the
	// last mark then did with the turn (checkpoint.go). Absent from every line
	// that is not one of those, and from every file written before they existed.
	Mark    *journalMark    `json:"mark,omitempty"`
	Ceiling *journalCeiling `json:"ceiling,omitempty"`

	// Carry is ONE RUNG of the ladder that decides what a handed-over worker
	// opens on (see [journalCarry]). Absent from every line that is not one, and
	// from every file written before it existed.
	Carry *journalCarry `json:"carry,omitempty"`

	// Failure is ONE CLASSIFICATION made at the response boundary (see
	// [journalFailure]). Absent from every line that is not one, and from every
	// file written before it existed.
	Failure *journalFailure `json:"failure,omitempty"`

	// Division is ONE division put to the road, whoever asked for it
	// (task_divide.go). Absent from every line that is not one, and from every
	// file written before it existed.
	Division *journalDivision `json:"division,omitempty"`

	// Principal is ONE MOMENT THE SESSION'S GOAL OWNER DECIDED SOMETHING
	// (see [journalPrincipal]). Absent from every line that is not one, and from
	// every file written before it existed — which is every attended session,
	// because a person decides these things in their own head.
	Principal *journalPrincipal `json:"principal,omitempty"`

	// Rule keeps a process-rule row written by an older build readable (see
	// [journalRule]). Current builds do not write this field.
	Rule *journalRule `json:"rule,omitempty"`

	// Abandoned is ONE TURN THIS SESSION LET GO OF (see [journalAbandoned]).
	// Absent from every line that is not one, and from every file written before
	// it existed.
	Abandoned *journalAbandoned `json:"abandoned,omitempty"`

	// Created is ONE FILE THIS SESSION MADE THAT WAS NOT THERE BEFORE (see
	// [journalCreated]). It is a line of its own rather than a field on the
	// message that wrote it because the fact it carries — DID THIS EXIST BEFORE
	// — can only be measured at the moment of the call and can never be
	// recovered from the transcript afterwards.
	Created *journalCreated `json:"created,omitempty"`

	Timestamp string `json:"timestamp"`
}

// journalPrincipal is ONE MOMENT THIS SESSION'S GOAL OWNER DECIDED SOMETHING
// (principal.go).
//
// IT EXISTS BECAUSE THE DECISIONS ARE THE FEATURE. An unattended run that
// carried itself on for four hours and one that stopped after eighteen minutes
// read IDENTICALLY in this file before it: the acceptance the whole ask was
// measured against existed nowhere, the moment the session decided the work was
// finished existed nowhere, and what it deleted on the way out existed nowhere.
// Every one of those is a thing a person would want to argue with afterwards.
//
// Event is what the moment was: `acceptance` when the done-condition for the
// whole ask was written and frozen, `delivery` when a retained result made its
// requested destination relevant, `decided` for the end of a turn that
// stopped, `checked` for one run of the session's declared checks from clean,
// and `reconciled` for the sweep that puts back what the session left lying
// about. Decision is the verb a `decided` line carries — carry on, done or stop
// — and Reason is why, in the words a person reads.
//
// IT IS EVIDENCE AND NEVER SPEND, for [journalCall]'s reason: what the
// acceptance call cost is already on its own call line.
type journalPrincipal struct {
	Delivery   *deliveryContract `json:"delivery,omitempty"`
	Who        string            `json:"who,omitempty"`
	Event      string            `json:"event,omitempty"`
	Acceptance string            `json:"acceptance,omitempty"`
	Decision   string            `json:"decision,omitempty"`
	Reason     string            `json:"reason,omitempty"`
	Brief      string            `json:"brief,omitempty"`
	Checks     []string          `json:"checks,omitempty"`
	Failed     []string          `json:"failed,omitempty"`
	Removed    []string          `json:"removed,omitempty"`
	Kept       []string          `json:"kept,omitempty"`
	// Stashed is how many entries `git stash list` named at the terminal
	// reading, and it rides the `checked` row: work the session took out of the
	// tree and never put back is part of what that reading found, and a run
	// that finished over a stashed fix used to be unreadable afterwards.
	Stashed int     `json:"stashed,omitempty"`
	WallMS  int64   `json:"wallMs,omitempty"`
	CostUSD float64 `json:"costUsd,omitempty"`
}

// journalRule keeps old session-file rule rows readable. Current builds do not
// emit them; the shape remains so reopening a conversation written by the
// retired progress-note gate does not discard unknown JSON.
type journalRule struct {
	Rule  string `json:"rule,omitempty"`
	Event string `json:"event,omitempty"`
	Count int    `json:"count,omitempty"`
}

// journalAbandoned is ONE TURN NOBODY WAITED FOR THE END OF.
//
// IT IS THE ONE LINE THAT COULD NOT BE RECONSTRUCTED. Every other record of what
// a turn did is written when the turn ENDS — the seal carries its cost, the
// principal line carries its decision — and the whole definition of an abandoned
// turn is that its ending never came. Before this line a turn let go of at a
// bound was indistinguishable in this file from a turn that simply stopped
// talking, which is exactly the reading that made issue #265's four minutes
// unaccountable: the surface was freed, the person moved on, and the file said a
// turn had been stopped and nothing about the fact that something may still have
// been running under it.
//
// Reason is why the turn was let go of ([AbandonReason]); today the only one is
// the stop bound. The token and money figures are THE TURN'S LAST KNOWN SPEND —
// what [Agent.bank] had moved by the moment the door was opened — and they are
// EVIDENCE AND NEVER SPEND, for [journalCall]'s reason exactly: every one of
// those calls already wrote its own line and moved the machine's ledger, so a
// replay that summed this one too would bill the abandoned turn twice.
//
// A turn abandoned before it had spent anything writes the line with no figures
// on it, which is the emptiness law: the fact worth recording is that the turn
// was let go of, and zeroes would read as a measurement.
//
// THERE IS NO DURATION ON IT, deliberately. A seal carries how long its turn
// took because a turn that ends knows when it ended; nobody waited for this one,
// so the only honest answer would be "how long until somebody stopped waiting",
// which is [tui3.stopGrace] plus whenever the person happened to press the key —
// a fact about the surface and not about the turn. The line's own timestamp says
// when it was let go of, which is the question that can be answered.
type journalAbandoned struct {
	Reason     string  `json:"reason,omitempty"`
	Input      int     `json:"input,omitempty"`
	Output     int     `json:"output,omitempty"`
	CacheRead  int     `json:"cacheRead,omitempty"`
	CacheWrite int     `json:"cacheWrite,omitempty"`
	CostUSD    float64 `json:"costUsd,omitempty"`
	Calls      int     `json:"calls,omitempty"`
}

// journalCreated is ONE FILE THIS SESSION MADE.
//
// Path is absolute — what a sweep is actually given — and Shown is the same
// path as a person reads it, relative to the workspace when it is under one.
// Both are kept for [fileChange]'s reason: the answer a person reads names
// files the way they asked for them, and the answer a machine acts on cannot
// depend on where a process happened to be standing.
//
// A MODIFIED FILE NEVER WRITES ONE. The whole value of the line is the word
// CREATED: nothing in this build may remove a file that was there before the
// session started, and a line that could not tell the two apart would be a line
// that cannot be acted on.
type journalCreated struct {
	Path  string `json:"path,omitempty"`
	Shown string `json:"shown,omitempty"`
}

// journalCall is what ONE provider response reported, on its own line.
//
// IT IS EVIDENCE AND NEVER SPEND. The seal above already carries every one of
// these numbers, summed; a replay that added these lines too would bill the
// session twice for the same calls. Nothing reads them back into the session's
// totals, and [replaySessionFile] says so where it drops them.
//
// It exists because a turn is sixty-odd requests with wildly different shapes —
// a cold first call, then fifty that are almost all cache read — and the sum of
// them cannot answer what a call with THIS many cached tokens actually cost.
// That question had to be reconstructed from transcript byte counts once, in a
// cost autopsy that found this surface paying 3.5× its models' list prices; the
// line is so the next one is a read rather than a reconstruction.
//
// Endpoint is who served it, exactly as the router spelled it, and it is the
// field the summed seal could never carry: a turn routed across three endpoints
// has one bill and three tariffs.
//
// EVERY REQUEST THIS SESSION MAKES WRITES ONE, the errands included
// (auxiliary.go's [Agent.callRole]). It did not always: the line was written
// from the turn's own accounting alone, so a measured run's call lines summed to
// $0.123 while the real bill was $0.739 — the difference being three side-calls
// to a mastermind that left `usage` lines and no shape at all. A record that
// covers most of the money is a record that answers cost questions wrongly, so
// the sum of these lines IS the bill.
//
// Role is which errand made the call, spelled as the role registry spells it
// (internal/roles). It is ABSENT on the conversation's own requests rather than
// spelled "chat", because absent is what the whole file means by "this is the
// session itself" — [journalUsage] already writes its own Role the same way —
// and a name invented for the default case is a name that has to be kept in step
// with a registry it is not in.
type journalCall struct {
	Model      string  `json:"model,omitempty"`
	Endpoint   string  `json:"endpoint,omitempty"`
	Role       string  `json:"role,omitempty"`
	Input      int     `json:"input,omitempty"`
	CacheRead  int     `json:"cacheRead,omitempty"`
	CacheWrite int     `json:"cacheWrite,omitempty"`
	Output     int     `json:"output,omitempty"`
	CostUSD    float64 `json:"costUsd,omitempty"`
}

// journalError is ONE CALL THAT FAILED, written down where the calls that
// succeeded already are.
//
// ── THE MEASURED FAILURE ────────────────────────────────────────────────────
//
// SWE-Marathon run s2, 22:45 UTC. A turn ended with `error: after 3 retries: API
// error (400): Provider returned error`, the session went idle, and the
// benchmark cell settled with five hours of budget unspent. NOTHING ABOUT THE
// 400 REACHED THIS FILE — no row, no status, no endpoint, no provider name, no
// upstream body — so the autopsy could say that a turn had died and nothing
// whatever about why. A journal that records every call that worked and nothing
// about the ones that did not is a journal that answers the easy question.
//
// So: EVERY FAILED CALL WRITES ONE, and it carries what an autopsy has to ask
// for otherwise. Status, Provider and Raw come off the refusal itself
// (internal/provider's APIError); Endpoint is who the router said was serving;
// Attempt is which rung of the retry ladder this was, so three rows for one step
// read as one ladder rather than three steps; and Input is THE ESTIMATE the
// session made of the request it was about to send, which is the only token
// figure a failed call has — the provider counted none.
//
// Output and DurationMS are the exception to "the provider counted none", and
// they exist for ONE class of failure: a guard cut (internal/provider's
// StreamCut). A cut stream ran for a measurable time and delivered a measurable
// amount of answer before it was ended, and those two figures are what tell a
// silent endpoint apart from one that wrote for eighteen minutes and never
// finished. Every other failure leaves both empty, which is the emptiness law:
// a zero here would read as "it produced nothing", and only a cut can say that
// honestly.
//
// IT IS EVIDENCE AND NEVER SPEND, for [journalCall]'s reason and one more: a
// failed call was not billed, so there is nothing here to sum.
type journalError struct {
	Model      string `json:"model,omitempty"`
	Endpoint   string `json:"endpoint,omitempty"`
	Role       string `json:"role,omitempty"`
	Status     int    `json:"status,omitempty"`
	Provider   string `json:"provider,omitempty"`
	Message    string `json:"message,omitempty"`
	Raw        string `json:"raw,omitempty"`
	Attempt    int    `json:"attempt,omitempty"`
	Input      int    `json:"input,omitempty"`
	Output     int    `json:"output,omitempty"`
	DurationMS int64  `json:"durationMs,omitempty"`
}

// journalFailure is ONE CLASSIFICATION made at the response boundary: what a bad
// response was taken to be, and what the harness did about it
// (internal/taxonomy).
//
// IT IS THE ROW A BENCH COUNTS. [journalError] already says that a call failed
// and what the provider said; what it cannot say is the only question the money
// turns on — whether the harness read that failure as the WIRE, as the MODEL, or
// as the WORK. Three runs of a measured comparison spent 57–82% of their bill on
// a stronger model bought because four bad responses in a row were read as the
// model being unable, and nothing in the file distinguished that from a model
// that had genuinely failed the work. One line per classification makes the two
// countable and the ratio between them readable.
//
// Class, Reason and Action are always written; everything else is the evidence
// that happened to be there, absent when it was not, which is the emptiness law
// as [journalError] applies it.
type journalFailure struct {
	Class    string  `json:"class"`
	Reason   string  `json:"reason,omitempty"`
	Action   string  `json:"action"`
	Model    string  `json:"model,omitempty"`
	Role     string  `json:"role,omitempty"`
	Attempt  int     `json:"attempt,omitempty"`
	Status   int     `json:"status,omitempty"`
	Provider string  `json:"provider,omitempty"`
	Refuted  int     `json:"refuted,omitempty"`
	SpentUSD float64 `json:"spentUsd,omitempty"`
}

// journalMark is ONE reading taken at a checkpoint mark: what the sidecar was
// asked to draw mid-turn, what it drew, what the harness did about it, and what
// the call itself cost (checkpoint.go's [Agent.readMark]).
//
// IT EXISTS BECAUSE A DECISION NOBODY WROTE DOWN CANNOT BE MEASURED. Three of
// these reads were made on one measured run and cost sixty-two cents between
// them — five times the whole of what the work they were judging cost — and
// every one of them answered "carry on". None of that was in the file: the
// spend showed up as three anonymous auxiliary lines, and what was asked, what
// came back and what it decided existed nowhere at all. So the reading is
// journaled where the money already is, and a bench can join the two.
//
// N is which rung of the ladder this was and Rounds is where the turn stood when
// it fired, which together say whether the ladder is landing where the policy
// says it does. Sketch is THE SHAPE LINE ALONE — the legend is a sentence for a
// worker and not evidence for a reader of the file — and Decision is what the
// harness took off it. Kept is the part of that drawing THAT DID NOT TRAVEL — the
// parts about work the conversation was still holding, which stay with it
// (checkpoint.go's [checkpointRead] and checkpoint_custody.go). It is absent on
// every mark that withheld nothing, which is nearly all of them, and it is what
// tells a reader of the file that a worker opened on less than was drawn and
// exactly how much less. Decision is what the harness took off the drawing:
// `split` when the turn was handed over on account of the
// parts, `continue` when nothing happened, `failed` when no reading came back at
// all. The ceiling's own read is a `continue` too: it decides nothing, and the
// ceiling line that follows it says what actually happened.
type journalMark struct {
	N          int     `json:"n,omitempty"`
	Rounds     int     `json:"rounds,omitempty"`
	Model      string  `json:"model,omitempty"`
	CostUSD    float64 `json:"costUsd,omitempty"`
	Sketch     string  `json:"sketch,omitempty"`
	Kept       string  `json:"kept,omitempty"`
	Decision   string  `json:"decision,omitempty"`
	DurationMS int64   `json:"durationMs,omitempty"`
}

// journalCeiling is what a HANDOVER did with the turn: moved the remaining work
// onto the one road, or ended it where it stood.
//
// IT IS WRITTEN ONCE PER HANDOVER ROAD AND NOT ONLY AT THE CEILING. Every door
// into checkpoint.go's [Agent.handOverRunningTurn] writes one — a mark whose
// drawing had parts in it, a turn past its write allowance, and the ceiling that
// gave this record its name — because the ending is decided there and nowhere
// else. Seam below says which door it was.
//
// It is a line of its own rather than a field on the mark above it because the
// two are different facts about different moments — the mark is a reading and
// this is an act — and because a handover can fire with no reading behind it at
// all (a sidecar nobody could reach still meets the ceiling).
//
// Decision is `moved`, `dropped:nothing-left` — the running model declared the
// work finished AND the mark's own reader agreed nothing remained — or one of the
// two ways a handover ends with no task of its own: `dropped:no-brief`, when
// nothing could be written down for anybody, and `dropped:work-already-out`, when
// everything there was to write down was about pieces this conversation is still
// holding and therefore nobody else's to take (checkpoint.go's custody road). On a
// run with a goal owner two more are possible, and both mean the turn was sealed
// at the handover with no task started: `dropped:stopped`, the owner read the
// ending and stopped the run with a reason, and `dropped:done`, the owner read it
// and said the ask was met (checkpoint.go's [Agent.endTurnUnderSteward]). And the
// write seam writes one more of its own, `dropped:delivering-own-result`: the turn
// is finishing the delivery of a result this conversation already owns, so the
// counter stood down and the work stayed here (writeseam.go's
// [Agent.writeSeamFires]). TaskID names the node when one was ADMITTED, and is
// absent otherwise by the emptiness law the rest of the line keeps — the delivery
// row admits nothing and names its result in Reason instead, so a bench counting
// tasks started off that field still counts only tasks.
//
// Carry NAMES THE RUNG THAT SUPPLIED THE BRIEF the task actually opened on
// (checkpoint.go's [Agent.handOverRunningTurn]): `handoff`, `draft` or `ask`.
// Two ceilings that both read `moved` are not the same event — one started a
// worker on a document written out of the turn's findings, the other started it
// on the person's bare sentence — and until this field the file could not tell
// them apart. The [journalCarry] lines directly above say WHY it was that rung;
// this is the one-word answer a bench can count.
//
// ── AND SEAM IS WHICH DOOR TOOK THE ENDING ──
//
// THE NAME OF THIS RECORD IS OLDER THAN WHAT IT RECORDS. It was the CEILING's
// line, because the ceiling was the only door that wrote one — so a handover the
// MARK road or the WRITE SEAM declined left no decision word in the file at all,
// and the real-model runs behind #567 all ended with an empty list of these while
// the refusal had plainly happened. The row now belongs to the ending rather than
// to the clock that noticed, and Seam says which of the three it was: `mark` for a
// drawing with parts in it, `write` for a turn past its write allowance, `ceiling`
// for the last rung of the ladder.
//
// A ROW WITH NO SEAM ON IT WAS WRITTEN BEFORE THIS EXISTED, and it is a ceiling by
// construction, because the ceiling was the only writer. An old file still reads.
//
// Reason is the reason WHERE THERE IS ONE and is empty everywhere else, which is
// the emptiness law and is most of the time: `dropped:no-brief` has the whole
// [journalCarry] ladder above it saying why each rung produced nothing, and
// `dropped:nothing-left` has no reason to give — nothing contradicted the work being
// done. `dropped:work-already-out` carries one, because the decision word alone
// does not say what was already out, and an autopsy grepping the word should get
// the why in the same line (checkpoint.go's carryHeldWork).
type journalCeiling struct {
	Rounds   int    `json:"rounds,omitempty"`
	Seam     string `json:"seam,omitempty"`
	Decision string `json:"decision,omitempty"`
	Reason   string `json:"reason,omitempty"`
	TaskID   uint64 `json:"taskId,omitempty"`
	Carry    string `json:"carry,omitempty"`
}

// journalCarry is ONE RUNG of the ladder that decides what a worker taken off a
// running turn OPENS ON (checkpoint.go's [Agent.handOverRunningTurn]).
//
// ── THE MEASURED FAILURE ────────────────────────────────────────────────────
//
// SWE-Marathon run s4, 00:01:54Z. A turn hit the round-40 ceiling and the ladder
// ran: the running model's draft came back as seven tokens nobody could work
// from, and the mastermind that writes the real brief was then asked and never
// answered — the call was cut by [checkpointHandoffWindow] ninety seconds later,
// to the millisecond. Both upper rungs returned the empty string, the task opened
// on the person's raw request, and the worker spent twelve minutes and seventy
// calls re-deriving what the chat had already found out.
//
// NONE OF THAT REACHED THIS FILE. The journal held a ceiling line saying `moved`
// and nothing else, which is the same line s2 wrote when the handoff worked and
// the worker opened on a 3.5 KB document. A fallback that changes what a worker
// is started on is an EVENT, not a default, and an event nobody wrote down is a
// difference no autopsy can see.
//
// So EVERY RUNG WRITES ONE. Rung is `handoff`, `draft` or `ask`, in ladder order.
// Outcome is what that rung did — `written`, `degenerate` (words that had stopped
// saying new things, or no words at all), `failed` with Reason carrying the
// provider's own sentence, `skipped` with Reason saying why it was never asked,
// `nothing-left` when the remains contract was answered instead, or `empty` when
// there was nothing there to carry. Chars is the size of what it produced, which
// is the one number that says a 3.5 KB dowry apart from a bare sentence. Used
// marks THE ONE rung that supplied the brief, so a reader of these lines alone —
// at the ceiling and at a mark's split, which writes no ceiling line — can see
// which of them the worker actually opened on.
//
// IT IS EVIDENCE AND NEVER SPEND, for [journalCall]'s reason: the money these
// rungs cost is already on their own call lines.
type journalCarry struct {
	Rung    string `json:"rung,omitempty"`
	Outcome string `json:"outcome,omitempty"`
	Reason  string `json:"reason,omitempty"`
	Chars   int    `json:"chars,omitempty"`
	Used    bool   `json:"used,omitempty"`
}

// journalDivision is ONE piece of work being put to the division road: who asked,
// how many parts they asked for, how many exist afterwards, and what answered
// (task_divide.go).
//
// IT EXISTS BECAUSE THREE COMPLETELY DIFFERENT OUTCOMES USED TO READ THE SAME.
// A task that ran with one worker had NEVER ASKED to divide, had asked and been
// refused by a free gate, or had asked and been refused by the reviewer — and the
// only trace of any of it was the absence of child nodes. Over three measured
// cells whose work a mastermind had already read as four jobs, every one landed
// `parts=0`, and nothing in any file said which of the three had happened. So one
// line, written wherever the road is asked.
//
// Source is `worker` for a division a worker reached for with the verb and
// `sketch` for one the harness submitted on its behalf out of a mark's drawing
// (task_divide_sketch.go). Requested is what was put; Admitted is how many parts
// exist, which differs when the reviewer merges. Decision is `admitted` or
// `refused:` and the gate that said no, so a bench can tell a floor refusal from
// a busy machine from a reviewer that read the parts as one job.
type journalDivision struct {
	TaskID    uint64 `json:"taskId,omitempty"`
	Source    string `json:"source,omitempty"`
	Requested int    `json:"requested,omitempty"`
	Admitted  int    `json:"admitted,omitempty"`
	Decision  string `json:"decision,omitempty"`
	// Error is why a review came to nothing, on the one decision where that is
	// not the same fact as the counter's refusal ([divisionRefusedUnreviewed]).
	Error string `json:"error,omitempty"`
	// Parts is what was asked for, by title, so a refusal reads against
	// something and not against a count.
	Parts []string `json:"parts,omitempty"`
	// Lifted names the parts the worker graded as ordinary work that were minted
	// on the careful tier anyway, because the ratings store said work of that
	// kind keeps being turned down on this model (task_divide.go, taskgrade.go).
	// It is empty on every division nothing was learned about, which is every
	// division until the store has seen the same kind of work settle twice — and
	// it is the one place an autopsy can tell a part that was CALLED careful from
	// one that EARNED it.
	Lifted []string `json:"lifted,omitempty"`
	// Shared names the check commands that stood in the done-condition of more
	// than one part, on the one decision where that is why the division was
	// turned down ([divisionRefusedShared]). It is empty on every other line.
	//
	// IT IS HERE SO THAT AN AUTOPSY CAN PROVE WHICH RUN WAS THE FAMILY'S rather
	// than infer it: the alternative is counting shell calls across four
	// worktrees that no longer exist, and the fact worth keeping is the one
	// command that would have been bought once per part.
	Shared []string `json:"shared,omitempty"`
	// Frozen is the commit the family tree stood at when this division was
	// admitted — the one world every part of it starts from — and Checkpoint is
	// the commit this division WROTE to get there, when the parent had work on
	// disk that nobody had committed yet (task_divide_wip.go).
	//
	// THEY ARE TWO FIELDS BECAUSE THEY ARE TWO FACTS. Every family with a tree of
	// its own has a Frozen; only a family whose parent had written something has
	// a Checkpoint. One field would leave an autopsy unable to tell a parent that
	// had written nothing from a family that was never frozen at all — which are
	// the ordinary case and the degradation, and telling them apart is most of
	// what this line is for.
	Frozen     string `json:"frozen,omitempty"`
	Checkpoint string `json:"checkpoint,omitempty"`
}

// journalUsage is one turn's accounting as the journal holds it.
//
// Duration is milliseconds and not a time.Duration because a time.Duration
// marshals as bare nanoseconds, and this is a file a person reads.
//
// Aux marks a line that was NOT a step of the conversation: the title call, a
// memory reflex, a rendered picture, a folded task node. The distinction is
// what lets a replay rebuild both counters the live session keeps — Calls
// counts every request to the provider, Turns only the ones a turn of the
// person's made (see [Agent.addAuxiliaryUsage]).
type journalUsage struct {
	Model      string  `json:"model,omitempty"`
	Input      int     `json:"input,omitempty"`
	Output     int     `json:"output,omitempty"`
	CacheRead  int     `json:"cacheRead,omitempty"`
	CacheWrite int     `json:"cacheWrite,omitempty"`
	CostUSD    float64 `json:"costUsd,omitempty"`
	Calls      int     `json:"calls,omitempty"`
	DurationMS int64   `json:"durationMs,omitempty"`
	Aux        bool    `json:"aux,omitempty"`
	// Empty marks a paid auxiliary call that reached its output ceiling without
	// returning any answer. Role says which errand it was; today only a reflex
	// writes the mark.
	Empty bool `json:"empty,omitempty"`

	// Role names WHAT the auxiliary call was for — "title", "taskname" — on the
	// lines where knowing it changes what a person can do with the record. Aux
	// says a turn did not ask for the call and Model says which model answered
	// it; neither says what was being asked, so a name that came back wrong
	// could not be traced to the model that gave it. Absent from a turn's own
	// seal and from every auxiliary call that does not name itself, by the same
	// emptiness law the rest of the line keeps.
	Role string `json:"role,omitempty"`
}

// journalSteer is one steer as the journal holds it: the instant the person
// pressed enter, and whether the turn they aimed it at actually carried it.
//
// The instant is the SEND's and not the line's. A steer typed while a long tool
// batch was running is journaled at the boundary that took it, seconds or
// minutes later, and the file's own Timestamp says that — which is the right
// answer to "when was this recorded" and the wrong one to "when did they say
// it". Both facts are worth keeping and they are kept separately.
//
// Consumed carries NO omitempty, deliberately. False is the meaningful answer
// here — the steer fell through — and a field that vanished when it was false
// would leave a reader unable to tell "it did not land" from "this build did not
// say". Every steer line states its outcome outright.
type journalSteer struct {
	At       string `json:"at,omitempty"`
	Consumed bool   `json:"consumed"`
	Landing  string `json:"landing,omitempty"`
}

// journalCaption is one step's narration as the file keeps it: which BATCH it is
// about, what was said, and which family of work that was.
//
// CallID IS THE ANCHOR AND IT IS THE BATCH'S FIRST CALL. A caption is about a
// run of calls rather than about any one of them, and the run's own identity is
// the identity of the call that opened it — which is a provider id the model
// minted, so it is stable across the journal, the wire and the surface, and it
// cannot be confused with the call that opened the batch AFTER it. Anchoring on
// "the newest tool row" instead is what let a late answer retitle a step it was
// never about (caption.go states the race).
//
// Category carries omitempty because a narrator that named no family is the
// ordinary case and a file should not spend bytes saying so; an absent one
// replays as the empty string, which a surface reads as "ask the tools".
type journalCaption struct {
	CallID   string         `json:"callId"`
	Text     string         `json:"text"`
	Category ActionCategory `json:"category,omitempty"`
}

// journalPartImage names the one non-text part a person's message can carry
// today. It is a field rather than an implied shape so a file written now stays
// readable when there is a second kind.
const journalPartImage = "image"

// journalPart is one non-text content part as the journal holds it: WHERE the
// bytes are and WHAT they were, never the bytes themselves.
//
// The digest is what makes the reference honest. A path alone says where a
// picture used to be; a build that trusted it would happily send a resumed
// session whatever now sits at that path — a different screenshot, a file the
// person overwrote an hour later — as the image they attached, and the model
// would answer about it as if the conversation had always been about that. So a
// replay re-reads the file ONLY when its digest still matches, and otherwise
// puts a placeholder in the transcript saying so. A transcript that admits it
// lost a picture is worth more than one that quietly substitutes another.
type journalPart struct {
	Type   string `json:"type"`
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	MIME   string `json:"mime,omitempty"`
}

// contentPart turns one reference back into content for the live transcript.
//
// This is the ONE rule for rebuilding a journaled part, and every rebuild goes
// through it: the resume replay below, and any later pass that rebuilds context
// from the file. Unchanged file, matching digest — the real image, byte-identical
// to what was sent the first time, so a resumed turn and the original turn put
// the same bytes on the wire. Anything else — moved, deleted, edited, unreadable,
// grown past the limit — is a text part that says which picture is missing.
func (p journalPart) contentPart() ai.ContentPart {
	if p.Type != journalPartImage {
		return p.placeholder()
	}
	info, err := os.Stat(p.Path)
	if err != nil || info.IsDir() || info.Size() > maxImageBytes {
		return p.placeholder()
	}
	data, err := os.ReadFile(p.Path)
	if err != nil || len(data) > maxImageBytes {
		return p.placeholder()
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != p.SHA256 {
		return p.placeholder()
	}
	mediaType := strings.TrimSpace(p.MIME)
	if mediaType == "" {
		mediaType = imageMediaTypes[strings.ToLower(filepath.Ext(p.Path))]
	}
	if mediaType == "" {
		return p.placeholder()
	}
	return ai.ContentPart{Type: "image_url", ImageURL: &ai.ImageURLData{
		URL: "data:" + mediaType + ";base64," + base64.StdEncoding.EncodeToString(data),
	}}
}

// reference is one journaled part as a REFERENCE AND NOTHING MORE: where the
// picture was, never its bytes.
//
// IT IS WHAT A READING DOOR REBUILDS, and the difference from [contentPart] is
// the whole of it. That one opens the file, hashes it and base64s it, because
// the messages it builds are about to be SENT and the model has to see the
// picture. A reading door builds messages nobody sends — they are shaped for
// display and thrown away ([ReadTranscript]) — so opening the file would be
// three syscalls and a hash per picture per read, on every room opened, every
// hosted record fetched and every beat of a run page's four-times-a-second poll.
//
// AND ON A HOSTED PAGE THE REBUILD WOULD ALSO BE FALSE. The paths in a record
// carried over a wire are the OTHER machine's, so every one of them fails to
// open here and [journalPart.placeholder] writes `[image /their/path — file
// changed or gone]` into the person's own line, about a file sitting untouched
// on the machine that made it.
//
// THE PATH IS CARRIED IN THE PART so that two pictures in one message stay two
// parts: [partKey] fingerprints a part by its body, and two empty parts would be
// one key — which is what lets [sessionFile.imageRefs] hand each of them its own
// name back. It is not a "text" part, so it puts no words in the person's line
// ([messageContentText] takes text parts and nothing else).
func (p journalPart) reference() ai.ContentPart {
	return ai.ContentPart{Type: p.Type, Text: p.Path}
}

// placeholder is what the model reads where a picture used to be. It names the
// path, because the person can often put the file back. The reason clause is
// the caller's, because the two callers know two different truths: a replay
// whose file will not open says so, and the transcript guard swapping for a
// blind model says THAT ([placeholderBecause]) — one sentence shape, the
// honest reason in it, never "changed or gone" about a file sitting untouched
// on disk.
func (p journalPart) placeholder() ai.ContentPart {
	return p.placeholderBecause("file changed or gone")
}

func (p journalPart) placeholderBecause(reason string) ai.ContentPart {
	if path := strings.TrimSpace(p.Path); path != "" {
		return ai.ContentPart{Type: "text", Text: "[image " + path + " — " + reason + "]"}
	}
	return ai.ContentPart{Type: "text", Text: "[image — " + reason + "]"}
}

// sessionFile is the open journal. Its own mutex keeps a line whole: the agent
// lock orders the writes, this one keeps a write from being interleaved by
// anything that reaches the file another way.
type sessionFile struct {
	mu     sync.Mutex
	file   *os.File
	locked bool
	closed bool
	// title is the name replayed from the file at open, so a resumed session
	// keeps the one it was given instead of paying to be named again.
	title      string
	shortTitle string
	// id is the header's session id — generated when the file is created and
	// replayed unchanged on every resume after it. It is what makes a session
	// one identity across days rather than one per process, which is what the
	// prompt-cache lineage is keyed on (see [Agent.cacheKey]).
	id string

	// images is WHERE the pictures in this conversation came from: one journaled
	// path per non-text content part, under a fingerprint of the part itself
	// (see [partKey]).
	//
	// It lives on the file because the file is the only thing that knows. A
	// rebuilt image part is a data URL — bytes with no provenance, which is the
	// same reason the journal had to write a reference in the first place — so by
	// the time a surface asks "what was this a picture of", the answer exists
	// nowhere in the transcript. Both places a picture enters the file put it here
	// too: the replay at open, and every appended message that carries refs.
	//
	// A fingerprint rather than the URL itself, because the URL is the whole
	// base64'd photo: an index keyed on it would hold every image of the session
	// alive for as long as the file is open, including the ones a compaction
	// already dropped.
	images map[string]string

	// notes is WHICH user-role messages the session wrote itself, under the same
	// kind of fingerprint the pictures use ([noteKey]).
	//
	// It lives here for the same reason images does: the transcript cannot answer
	// the question. A wake note is user-role text and nothing about the message
	// distinguishes it from a line somebody typed — the mark is on the journal's
	// line, so the journal is what a surface asks (see [sessionEntry.Note] and
	// [shapeEntries]).
	notes     map[string]bool
	replyTags map[string][]TaskReplyTag
	// delivered is the set of durable delivery ids this file has recorded, from
	// the replay at open and from every note appended since. It answers one
	// question — has this conversation already been told this landing — for a
	// resume deciding what it still owes (task_store.go). noteDeliveries is the
	// same ids under their line's own fingerprint, so a compaction re-journals a
	// note with what it was the record of.
	delivered      map[string]bool
	noteDeliveries map[string][]string

	// steers is WHICH user-role messages were spliced into a turn that was
	// already running (steer.go), under the same fingerprint the notes use.
	//
	// It lives here for the notes' own reason, one turn of the argument further
	// along: a steer is an ordinary user message in the transcript — it has to
	// be, because that is what the model reads it as — so nothing about the
	// message says it did not open the turn it sits in. The mark is on the
	// journal's line, so the journal is what a surface asks
	// ([sessionFile.steerMark], read by [shapeEntries]).
	steers map[string]SteerMark

	// captions is WHAT THE NARRATOR SAID ABOUT EACH BATCH, keyed by the batch's
	// first call id ([journalCaption]).
	//
	// It lives here for the steers' and the notes' reason: the transcript cannot
	// answer the question. A caption is not a message — the model never reads
	// one, nothing is sent in it — so there is no line in the conversation for it
	// to be recovered from, and a replay without this index falls back to
	// recomposing a sentence out of tool names.
	captions map[string]journalCaption

	// restored is what this conversation had already spent when the file was
	// opened: the SUM of its usage lines, replayed once and never updated after.
	// It is the file's answer to "what did this cost before today", and the agent
	// seeds its own running total from it at construction (agent.go). Nothing
	// stores a second copy of the total — the lines are the record, and this is
	// the one pass that adds them up.
	restored Usage
}

// RestoredUsage is what the conversation in this file had spent before it was
// opened, and the zero Usage for a file that is new or holds no usage lines.
func (s *sessionFile) RestoredUsage() Usage {
	if s == nil {
		return Usage{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.restored
}

// Title is the name this file was opened holding, empty when it has none.
func (s *sessionFile) Title() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.title
}

func (s *sessionFile) ShortTitle() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.shortTitle
}

// ID is the session id this file was opened holding, empty when the header
// carried none (a file written before the id was recorded, or a replay that
// stopped before reaching the header).
func (s *sessionFile) ID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.id
}

// imageRefs is the journaled path of every picture in one message, in the order
// the parts sit in it, and nil for the messages — nearly all of them — that
// carry none.
//
// The NIL RECEIVER answers nil, which is not defensiveness: a session with no
// file has no journal to have written a path, and making that caller test for a
// file before asking a question about pictures would put the same nil check at
// every call site instead of at the one place that can answer it.
func (s *sessionFile) imageRefs(message ai.Message) []string {
	if s == nil || len(message.Content) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.images) == 0 {
		return nil
	}
	var refs []string
	for _, part := range message.Content {
		if path, known := s.images[partKey(part)]; known {
			refs = append(refs, path)
		}
	}
	return refs
}

// imagePath is where ONE part's picture came from, "" when the journal never
// recorded it — a memory-only session, or a part this file did not write.
//
// It is separate from [sessionFile.imageRefs] rather than its inner half because
// the two ask different questions: that one walks a whole message under one lock
// and skips what it does not know, and this one asks about a single part and has
// to be able to say "not known" for it. The transcript guard is the caller, and
// it must replace EVERY image part whether or not the journal can name it.
//
// The NIL RECEIVER answers "", for the reason [sessionFile.imageRefs] answers
// nil: a session with no file wrote no path down.
func (s *sessionFile) imagePath(part ai.ContentPart) string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.images[partKey(part)]
}

// rememberImagePath indexes one part under a path this file already knows, which
// is what keeps a picture's NAME on a message whose bytes have been replaced by a
// placeholder ([Agent.scrubBlindImagePartsLocked]). A replay does the same thing
// by accident and for the same reason — [rememberParts] indexes whatever part
// was rebuilt, placeholder or picture — so a scrubbed message and a replayed one
// draw alike.
//
// An empty path records nothing: an index entry pointing nowhere would make
// [sessionFile.imageRefs] claim a picture it cannot name.
func (s *sessionFile) rememberImagePath(part ai.ContentPart, path string) {
	if s == nil {
		return
	}
	if path = strings.TrimSpace(path); path == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.images == nil {
		s.images = make(map[string]string)
	}
	s.images[partKey(part)] = path
}

// isNote reports whether one message is a line the SESSION wrote — the answer
// [shapeEntries] turns into the "note" role a surface draws in its own lane
// rather than in the person's.
//
// The NIL RECEIVER answers false, for the reason [sessionFile.imageRefs] answers
// nil: a session with no file wrote no journal, so there is no mark to have
// read, and the caller should not have to check for a file first.
func (s *sessionFile) isNote(message ai.Message) bool {
	if s == nil {
		return false
	}
	key := noteKey(message)
	if key == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.notes[key]
}

// steerMark reports whether one message was spliced into a running turn, and
// what the record kept about it — nil for every message that was not one, which
// is nearly all of them.
//
// The NIL RECEIVER answers nil, for the reason [sessionFile.isNote] answers
// false: a session with no file wrote no journal, so there is no mark to have
// read, and the caller should not have to check for a file first.
func (s *sessionFile) steerMark(message ai.Message) *SteerMark {
	if s == nil {
		return nil
	}
	key := noteKey(message)
	if key == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	mark, spliced := s.steers[key]
	if !spliced {
		return nil
	}
	return &mark
}

// caption returns what the narrator said about the batch this call opened, and
// the family it named — "" and "" for every call that is not a batch's anchor,
// which is most of them.
//
// The NIL RECEIVER answers empty, for the reason [sessionFile.steerMark] answers
// nil: a session with no file wrote no journal, so there is nothing to have read.
func (s *sessionFile) caption(callID string) (string, ActionCategory) {
	if s == nil {
		return "", ""
	}
	callID = strings.TrimSpace(callID)
	if callID == "" {
		return "", ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	mark, told := s.captions[callID]
	if !told {
		return "", ""
	}
	return mark.Text, mark.Category
}

// appendCaption journals one step's narration against the batch it is about.
//
// IT IS CALLED WHERE THE EVENT IS SENT and with the same anchor, so the record
// and the live stream cannot disagree about which step was named. A second
// narration of the same batch simply lands after the first; the replay takes the
// last, which is what the person was left looking at.
//
// An anchorless or wordless caption is not written: neither could be found again,
// and a line nothing can look up is a line that only grows the file.
func (s *sessionFile) appendCaption(callID, text string, category ActionCategory) {
	if s == nil {
		return
	}
	callID, text = strings.TrimSpace(callID), strings.TrimSpace(text)
	if callID == "" || text == "" {
		return
	}
	mark := journalCaption{CallID: callID, Text: text, Category: category}
	s.mu.Lock()
	if s.captions == nil {
		s.captions = make(map[string]journalCaption, 4)
	}
	s.captions[callID] = mark
	s.mu.Unlock()
	s.writeLine(sessionEntry{Type: "caption", Caption: &mark, Timestamp: stamp()})
}

// rememberSteer marks one message as a splice, in a map the caller owns — the
// file's, under its lock, or the one a replay is still building. It is
// [rememberNote]'s twin and keeps its shape on purpose: the two facts are
// remembered the same way because they are the same kind of fact about a line.
func rememberSteer(steers map[string]SteerMark, message ai.Message, mark SteerMark) {
	if steers == nil {
		return
	}
	if key := noteKey(message); key != "" {
		steers[key] = mark
	}
}

// steerStamp is one steer's send instant as the file holds it, and the time back
// out of it. An unparseable or absent stamp is the zero time, which a surface
// reads as "not known" by the emptiness law rather than as the epoch.
func steerStamp(at time.Time) string {
	if at.IsZero() {
		return ""
	}
	return at.UTC().Format(time.RFC3339Nano)
}

func steerInstant(at string) time.Time {
	parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(at))
	if err != nil {
		return time.Time{}
	}
	return parsed
}

// taskReplyTags returns the typed identities stored beside a completion note.
func (s *sessionFile) taskReplyTags(message ai.Message) []TaskReplyTag {
	if s == nil {
		return nil
	}
	key := noteKey(message)
	if key == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]TaskReplyTag(nil), s.replyTags[key]...)
}

func rememberReplyTags(index map[string][]TaskReplyTag, message ai.Message, tags []TaskReplyTag) {
	if len(tags) == 0 {
		return
	}
	if key := noteKey(message); key != "" {
		index[key] = append([]TaskReplyTag(nil), tags...)
	}
}

// rememberNote marks one message as the session's own, in a map the caller owns
// — the file's, under its lock, and the one a replay is still building.
func rememberNote(notes map[string]bool, message ai.Message) {
	if notes == nil {
		return
	}
	if key := noteKey(message); key != "" {
		notes[key] = true
	}
}

// rememberDeliveries adds the durable delivery ids one recorded line carries, in
// a map the caller owns.
func rememberDeliveries(delivered map[string]bool, ids []string) {
	for _, id := range ids {
		if id = strings.TrimSpace(id); id != "" && delivered != nil {
			delivered[id] = true
		}
	}
}

// rememberNoteDeliveries files one line's ids under the line's own fingerprint.
func rememberNoteDeliveries(index map[string][]string, message ai.Message, ids []string) {
	if index == nil || len(ids) == 0 {
		return
	}
	if key := noteKey(message); key != "" {
		index[key] = append([]string(nil), ids...)
	}
}

// noteDeliveriesOf is what one recorded note was the record of, for the
// compaction that writes it again.
func (s *sessionFile) noteDeliveriesOf(message ai.Message) []deliveryID {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []deliveryID
	for _, id := range s.noteDeliveries[noteKey(message)] {
		out = append(out, deliveryID(id))
	}
	return out
}

// recorded answers whether this journal already holds the line that carried one
// durable delivery. It is the record's own answer, so a resume can tell a
// landing it has already told from one it still owes even when the checkpoint
// that would have said so was never written ([durableDelivery]).
func (s *sessionFile) recorded(id deliveryID) bool {
	if s == nil || id == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.delivered[string(id)]
}

// noteKey fingerprints a session-authored line: its role and its text, through
// the same [partKey] the pictures are indexed by.
//
// ONE TEXT PART IS THE WHOLE SHAPE of these messages — [Agent.enqueueNote]
// builds them from a string — so anything else is not one and is left alone. Two
// notes with the same words share a key, which is the right answer: they are the
// same line and both are the session's.
func noteKey(message ai.Message) string {
	if len(message.Content) != 1 || message.Content[0].Type != "text" {
		return ""
	}
	return message.Role + "|" + partKey(message.Content[0])
}

// rememberParts records where one message's non-text parts came from.
//
// The refs are the message's LAST parts, and that is a fact both builders of
// such a message state: [imageUserMessage] appends the pictures after the
// optional text, and [replayedMessage] rebuilds them in the same order. Anything
// shorter than its own references is left alone rather than guessed at.
func (s *sessionFile) rememberParts(message ai.Message, refs []journalPart) {
	if s == nil || len(refs) == 0 || len(message.Content) < len(refs) {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rememberParts(s.images, message, refs)
}

// rememberParts is the indexing itself, for a map the caller owns — the file's,
// under its lock, and the one a replay is still building.
func rememberParts(images map[string]string, message ai.Message, refs []journalPart) {
	if images == nil || len(refs) == 0 || len(message.Content) < len(refs) {
		return
	}
	parts := message.Content[len(message.Content)-len(refs):]
	for index, part := range parts {
		if path := strings.TrimSpace(refs[index].Path); path != "" {
			images[partKey(part)] = path
		}
	}
}

// partKey fingerprints one content part: its kind, its length, and its two
// ends.
//
// It is a fingerprint and not the content because the content is a multi-megabyte
// data URL, and it is BOTH ends because base64 of the same media type opens with
// the same handful of bytes for every picture — a key made of the head alone
// would collide across photos of the same size. Two parts that match this and
// are different bytes would have to agree on all three, which within one
// conversation is a photo attached twice.
func partKey(part ai.ContentPart) string {
	body := part.Text
	if part.ImageURL != nil {
		body = part.ImageURL.URL
	}
	const ends = 48
	size := len(body)
	if size > 2*ends {
		body = body[:ends] + body[size-ends:]
	}
	return part.Type + ":" + strconv.Itoa(size) + ":" + body
}

// openSessionFile opens (or creates) the journal, claims it, and replays it
// into the live transcript. The replay starts AFTER the latest compaction
// marker, with that marker's summary as the context prefix — resuming means
// resuming the conversation the model last had, not the one that was already
// summarized away.
//
// IT ALSO HANDS BACK WHAT IS ABOVE THAT MARKER ([replayedSession.earlier]).
// That region is not part of the transcript and is never sent; it is what a
// surface scrolls back into, so that the boundary reads as the seam it is
// rather than as the beginning of the conversation.
//
// The claim comes BEFORE the replay, not after. A lock taken at the end would
// leave two processes reading the same file concurrently and only then finding
// out one of them must back off, and the loser would have paid for a replay it
// cannot use. Locked first, the loser fails at the door.
// id is the name the header of a FRESHLY CREATED file carries, and it is the
// caller's rather than this function's because a session is a folder named by
// its id (place.go): the folder has to be minted before the transcript inside
// it can be, so by the time the journal is opened the id already exists. Empty
// is the legacy flat layout, where nobody outside had an opinion and the file
// names itself. A resumed file keeps the id it was written with either way.
func openSessionFile(path, cwd, model, id string) (*sessionFile, replayedSession, error) {
	if directory := filepath.Dir(path); directory != "" && directory != "." {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			return nil, replayedSession{}, fmt.Errorf("session file: %w", err)
		}
	}

	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, replayedSession{}, fmt.Errorf("session file: %w", err)
	}
	locked, err := lockSessionFile(file, path)
	if err != nil {
		_ = file.Close()
		return nil, replayedSession{}, err
	}
	journal := &sessionFile{file: file, locked: locked}

	// Creating the file above does not make it an existing session: existed is
	// "this file has lines in it", and a file this call just created has none.
	replayed, err := replaySessionFile(path)
	if err != nil {
		// The claim is released here rather than left to the caller: this
		// returns no journal, so nobody else has a handle to close.
		_ = journal.Close()
		return nil, replayedSession{}, err
	}
	journal.title = replayed.title
	journal.shortTitle = replayed.shortTitle
	journal.id = replayed.id
	journal.images = replayed.images
	journal.notes = replayed.notes
	journal.replyTags = replayed.replyTags
	journal.delivered = replayed.delivered
	journal.noteDeliveries = replayed.noteDeliveries
	journal.steers = replayed.steers
	journal.captions = replayed.captions
	journal.restored = replayed.usage

	if !replayed.existed {
		// The header names the session once. A resumed file keeps its
		// original: the id is what a second window looks a session up by.
		journal.id = strings.TrimSpace(id)
		if journal.id == "" {
			journal.id = NewSessionID()
		}
		journal.writeLine(sessionHeader{
			Type:      "session",
			Version:   sessionFileVersion,
			ID:        journal.id,
			Cwd:       cwd,
			Model:     model,
			Timestamp: stamp(),
		})
	}
	return journal, replayed, nil
}

// lockSessionFile claims the journal for this process with a non-blocking
// exclusive flock, and reports whether the claim was actually taken.
//
// flock is the right primitive here because the kernel releases it when the
// holding process dies, however it dies. That is the whole reason there is no
// pid file and no staleness check: a held flock IS a live writer, so a crashed
// aforge leaves nothing behind to clean up or to second-guess. The lock rides
// the open file description, so it lives exactly as long as the descriptor the
// journal holds.
//
// A filesystem that cannot flock at all (some network mounts answer EINVAL or
// ENOLCK) is not a reason to refuse the session. The interleave this guards
// against is possible but rare; being unable to open your own transcript is
// certain. So an unsupported lock opens unlocked, and the caller carries
// locked=false so Close does not unlock what it never took.
func lockSessionFile(file *os.File, path string) (bool, error) {
	err := filelock.Lock(file, true, true)
	switch {
	case err == nil:
		return true, nil
	case filelock.IsBusy(err):
		// EAGAIN on Linux, EWOULDBLOCK on darwin — the same value, and the one
		// answer that means "somebody else holds this".
		return false, &SessionLockedError{Path: path}
	default:
		return false, nil
	}
}

// replaySessionFile reads a journal into messages. It reports whether the file
// had any content — an empty or missing file is a new session, not an error.
//
// A line that does not parse is skipped rather than fatal: the one line a
// crash can corrupt is the last one, and losing a session because its tail was
// half-written is the wrong trade against losing that tail. The replayed
// transcript is then repaired (see repairTranscript) — a half-written tail can
// be a half-written tool batch, which is not a lost line but an illegal
// transcript.
func replaySessionFile(path string) (replayedSession, error) {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return replayedSession{}, nil
		}
		return replayedSession{}, fmt.Errorf("session file: %w", err)
	}
	defer file.Close()

	// TRUE, because this is the transcript that gets SENT: a resumed turn has to
	// put the same bytes on the wire the original turn did.
	replayed, err := readJournal(file, path, true)
	if err != nil {
		return replayed, err
	}
	// THE REPAIR BELONGS TO THE CALLER THAT IS GOING TO SEND THESE MESSAGES, and
	// this is that caller: a resumed conversation goes to a provider on its next
	// request and an illegal transcript is a 400 forever. A reader that only
	// DRAWS the record wants the record ([ReadTranscript]) — an unanswered call
	// is what it is there to show, not a shape to mend.
	original := append([]ai.Message(nil), replayed.messages...)
	replayed.messages = repairTranscript(replayed.messages)
	replayed.reasoning = reasoningAfterRepair(replayed.messages, original, replayed.reasoning)
	// The earlier region goes through the SAME repair as the live one. It is
	// never sent, so the 400 the repair exists to prevent cannot happen to it —
	// but a tool result whose call is missing is a row a surface would draw with
	// nothing above it either way, and two shapings of one journal that disagreed
	// about which lines are real would be the seam lying in a second way.
	replayed.earlier = repairTranscript(replayed.earlier)
	// The overlap was counted against the lines the file holds and is applied to
	// the transcript the repair left behind, so it is clamped to it. The repair
	// only ever drops an unanswered trailing batch and orphaned results — the tail
	// and the rare stray — so the two agree in every session that was not killed
	// mid-batch, and a message of drift at the seam is a row nobody can see.
	if replayed.overlap > len(replayed.messages) {
		replayed.overlap = len(replayed.messages)
	}
	return replayed, nil
}

// readJournal is the scan itself, over any reader of a session file: every line
// rebuilt into the message it was, with the three indexes a display shaping asks
// the journal for built in the same pass.
//
// IT REPAIRS NOTHING. What it hands back is what the file says, unanswered calls
// and all, which is what a page reading somebody else's work has to be able to
// draw ([ReadTranscript]); [replaySessionFile] is where a transcript that is
// about to be SENT is made legal again.
//
// rebuild says whether a journaled picture is rebuilt into its BYTES or kept as
// a reference to where it was ([journalPart.reference]). A transcript that is
// about to be sent needs the bytes; a reading that only draws must not pay three
// syscalls and a hash per picture per read, and on a hosted record must not open
// the other machine's paths at all.
//
// path is named only in the error a newer file's format version raises, so a
// reader with no path (a tail carried across a wire) passes "".
//
// ── A READER KEEPS EVERYTHING IT COULD READ AND NAMES THE LINE IT COULD NOT ──
//
// A line too long for the scanner's buffer used to end this function with an
// error and NO MESSAGES, and every caller took that for "this file is not
// readable": a conversation with one very large paste in it resumed EMPTY. The
// person had not lost a line, they had lost the session. So the scan now keeps
// every message above the line it could not get past and says which line that
// was ([replayedSession.unread]) — the same answer on the resume path and at
// both reading doors, because it is the same fact about the same file.
func readJournal(reader io.Reader, path string, rebuild bool) (replayedSession, error) {
	var (
		messages   []ai.Message
		reasoning  []provider.MessageReasoning
		earlier    []ai.Message
		overlap    int
		title      string
		shortTitle string
		id         string
		lines      int
		scanned    int
		spent      Usage
		created    []fileChange
	)
	// The picture index is built as the messages are, because this is the one
	// pass that holds both halves at once: the reference the journal wrote and
	// the part it was rebuilt into (see [sessionFile.images]). It survives a
	// compaction marker for the same reason the title does — where a picture came
	// from is a fact about the file, not about the tail of the transcript.
	images := make(map[string]string)
	// And the note index with it, for the same reason and in the same pass: the
	// mark is on the LINE, and once the line has been rebuilt into a message
	// there is nothing left to read it off (see [sessionFile.notes]).
	notes := make(map[string]bool)
	// And the caption index, for the notes' reason exactly: a `caption` line is
	// news about a batch that is not carried by any message, so this pass is the
	// only place it can be picked up (see [sessionFile.captions]).
	captions := make(map[string]journalCaption)
	replyTags := make(map[string][]TaskReplyTag)
	delivered := make(map[string]bool)
	noteDeliveries := make(map[string][]string)
	// And the splice index, in the same pass and for the same reason: a steer is
	// an ordinary user message once it has been rebuilt, and the mark that says
	// it was typed INTO the turn above it is on the line (steer.go).
	steers := make(map[string]SteerMark)
	scanner := bufio.NewScanner(reader)
	// A tool result can be tens of kilobytes; the default 64KiB token limit
	// would end the replay at the first big one.
	scanner.Buffer(make([]byte, 0, 64<<10), 8<<20)
	for scanner.Scan() {
		// scanner.Bytes() is the scanner's own buffer and is only valid until the
		// next Scan. Every reader of it in this loop is one of the two unmarshals
		// below, both of which consume it before the loop turns over and copy
		// every string they keep out of it. Text() would instead allocate a copy
		// of the line, and []byte(...) of that copy a second one — two copies of
		// every line of the journal, and a tool result is tens of kilobytes.
		// Counted before the blank check so the number below is a LINE OF THE
		// FILE, which is what somebody opening it in an editor needs, and not a
		// count of the lines this reader found interesting.
		scanned++
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		lines++
		var entry sessionEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			continue
		}
		switch entry.Type {
		case "session":
			// The header names the format. A file written by a newer aforge can
			// hold entry types and fields this build does not know, and every
			// one of them would be dropped in silence — a session that resumes
			// looking complete and is not. Say so instead.
			var header sessionHeader
			if err := json.Unmarshal(line, &header); err != nil {
				continue
			}
			if header.Version > sessionFileVersion {
				return replayedSession{existed: true}, &newerFormatError{
					Path: path, Version: header.Version, Reads: sessionFileVersion}
			}
			// FIRST one wins, unlike the title: the header is written once, at
			// creation, and a second one in the same file would be a file two
			// processes wrote — in which case the older identity is the one the
			// conversation actually has.
			if id == "" {
				id = strings.TrimSpace(header.ID)
			}
		case "message":
			if entry.Role == "" {
				continue
			}
			message := replayedMessage(entry, rebuild)
			rememberParts(images, message, entry.Parts)
			if entry.Note {
				rememberNote(notes, message)
				rememberReplyTags(replyTags, message, entry.ReplyTags)
				rememberDeliveries(delivered, entry.Deliveries)
				rememberNoteDeliveries(noteDeliveries, message, entry.Deliveries)
			}
			if entry.Steer != nil {
				rememberSteer(steers, message, SteerMark{
					At:       steerInstant(entry.Steer.At),
					Consumed: entry.Steer.Consumed,
					Landing:  entry.Steer.Landing,
				})
			}
			messages = append(messages, message)
			reasoning = append(reasoning, provider.MessageReasoning{
				Field: entry.ReasoningField, Text: entry.Reasoning,
				Details: append(json.RawMessage(nil), entry.ReasoningDetails...),
				Model:   entry.ReasoningModel,
			})
		case "compaction":
			// THE REGION THIS MARKER REPLACES IS KEPT BEFORE IT IS THROWN AWAY,
			// which is the one thing this pass does that the live transcript has
			// no use for. It is what a surface scrolls back into: the journal
			// holds every line of the conversation above the marker, and without
			// this the boundary would masquerade as the beginning of the chat.
			//
			// It is taken with the SAME reducer state that is about to be
			// discarded — rewind cuts already applied, an older marker's window
			// already in place — so the region is the transcript exactly as it
			// stood one instant before this pass edited it, and not a naive
			// re-read of the lines above the marker (which would resurrect turns
			// a rewind took back).
			//
			// The LAST marker wins because each one overwrites the snapshot the
			// one before it took. See [replayedSession.earlier] for why the
			// nested regions are not stacked.
			earlier = append(earlier[:0], messages...)
			// Everything before this marker is what the pass replaces. The
			// name is not a message and survives the cut: a compacted session
			// is the same session, still called what it was called.
			rebuilt := compactionMessages(entry, rebuild)
			// AND THE REGION IS ONLY KEPT IF IT CAN BE PLACED. The pass wrote its
			// whole rebuilt window back below the marker, so the lines above it
			// and the first Window lines below it are two renderings of ONE
			// conversation — a reader that could not say where the copy ends has
			// nothing it can honestly draw, and drops the region rather than
			// showing the session to itself twice.
			if overlap = compactionOverlap(entry); overlap < 0 {
				earlier, overlap = earlier[:0], 0
			}
			messages = append(messages[:0], rebuilt...)
			reasoning = append(reasoning[:0], make([]provider.MessageReasoning, len(rebuilt))...)
			// The frames message is the FIRST of them when an old marker carried
			// pages, which is the order [compactionMessages] builds and the only
			// place those references belong: the summary beside it is words.
			if len(entry.Parts) > 0 && len(rebuilt) > 0 {
				rememberParts(images, rebuilt[0], entry.Parts)
			}
		case "caption":
			// ONE BATCH'S NARRATION. It rebuilds no message — nothing was ever
			// said to the model here — so it only indexes, and the LAST line for
			// a batch wins the way the title's last line does: the narrator can
			// speak twice about one step while it runs, and what a person was
			// left looking at is what the record should give back.
			if entry.Caption == nil {
				continue
			}
			mark := *entry.Caption
			if strings.TrimSpace(mark.CallID) == "" || strings.TrimSpace(mark.Text) == "" {
				// A line with no anchor names no step, and a line with no words
				// says nothing. Either would be a caption that could only ever
				// be found by accident.
				continue
			}
			captions[mark.CallID] = mark
		case "steer":
			// A STEER THAT FELL THROUGH, and nothing is rebuilt from it
			// ([sessionFile.appendSteerFellThrough]). Those words never reached
			// the turn they were aimed at, so they are not a message of it — and
			// they DID reach the conversation, as the ordinary question they
			// became a moment later, which the message lines below already carry.
			// Replaying the line as well would put the sentence in twice.
			//
			// The arm is written out rather than left to the switch's silence so
			// that the next reader finds the reason here instead of concluding the
			// line was forgotten about.
			continue
		case "rewind":
			// The turn this line took back. Everything after it in the file is
			// ordinary conversation again — a rewind is followed by the person
			// saying the thing better — so the replay drops N and keeps reading
			// rather than stopping here.
			if entry.Dropped <= 0 {
				continue
			}
			if entry.Dropped >= len(messages) {
				messages = messages[:0]
				reasoning = reasoning[:0]
				continue
			}
			messages = messages[:len(messages)-entry.Dropped]
			reasoning = reasoning[:len(reasoning)-entry.Dropped]
		case "usage":
			// EVERY line is added, and none is ever taken back. This is the one
			// arm that accumulates rather than rebuilds: a compaction below
			// replaces the message window, and money spent before it stays spent.
			if entry.Usage == nil {
				continue
			}
			used := entry.Usage
			spent.Input += used.Input
			spent.Output += used.Output
			spent.CacheRead += used.CacheRead
			spent.CacheWrite += used.CacheWrite
			spent.CostUSD += used.CostUSD
			spent.Duration += time.Duration(used.DurationMS) * time.Millisecond
			spent.Calls += used.Calls
			if used.Empty && used.Role == string(roles.RoleReflex) {
				spent.EmptyReflex += used.Calls
			}
			// Turns counts the conversation's own steps and nothing else, which
			// is the law the live counters keep ([Agent.addUsage] bumps it,
			// [Agent.addAuxiliaryUsage] deliberately does not). The aux mark on
			// the line is what lets a replay keep the same distinction.
			if !used.Aux {
				spent.Turns += used.Calls
			}
		case "call":
			// DROPPED ON PURPOSE, and this arm exists to say so rather than to
			// leave it to the switch falling off the end. A call line is the
			// SHAPE of one request — who served it, how much of its prompt was
			// warm — and every dollar on it is already counted in the seal that
			// closed its turn. Folding it in here would bill the session twice
			// for the same money.
		case "error":
			// DROPPED ON PURPOSE, for the reason a call line is, and one of its
			// own: a failed call cost nothing to bill and put nothing in the
			// transcript. It is evidence for whoever reads the file afterwards,
			// and replaying it would put a provider's refusal into somebody's
			// conversation as though the model had said it.
		case "abandoned":
			// DROPPED ON PURPOSE, for the reason a call line is and one of its
			// own. Every dollar on it is already counted — the calls it sums each
			// wrote their own line and each moved the machine's ledger when they
			// were made (usage_ledger.go) — so folding it in would bill the
			// abandoned turn twice. And it is not a message: what the turn had
			// said before it was let go of is already in the transcript above it,
			// and a resumed session must open on that rather than on a note about
			// how the last one ended. The line is for whoever reads the file.
		case "mark", "ceiling", "division", "carry", "failure":
			// DROPPED ON PURPOSE, for the reason a call line is: these are the
			// RECORD of a decision the harness took mid-turn, and a decision is
			// not a message and not money. Whatever the mark's reader cost is
			// already on the usage line beside it and on its own call line, and
			// what the ceiling did to the turn is already in the transcript —
			// the line the person read, and the task the graph admitted. A
			// division's parts are nodes in the graph's own checkpoint and its
			// receipt is already in the worker's transcript. A carry line is the
			// same kind of fact about the same moment: which rung of the brief
			// ladder the worker opened on, which the spec in the graph already
			// holds. A failure line is the boundary's reading of a call that
			// already has its own error line above it.
			// Replaying them would put machinery into somebody's conversation.
		case "created":
			// KEPT, and it is the ONE non-message line this replay carries
			// forward. The others in this switch are the record of a decision or
			// of money, and a resumed session re-derives both; this one carries a
			// fact nothing can re-derive — whether a file was there BEFORE the
			// session touched it — and a resumed session that lost it would end
			// by looking at everything it made and being unable to say what it
			// had made ([journalCreated]).
			if entry.Created == nil || strings.TrimSpace(entry.Created.Path) == "" {
				continue
			}
			created = append(created, fileChange{
				path:    entry.Created.Path,
				shown:   entry.Created.Shown,
				created: true,
			})
		case "principal":
			// DROPPED ON PURPOSE, for the reason a mark line is: it is the RECORD
			// of a decision the session's goal owner made, and a decision is not
			// a message and not money. What it decided is already in the
			// transcript — the brief the turn carried on with, the line the
			// person read — and replaying it would put machinery into somebody's
			// conversation.
		case "title":
			// LAST one wins. A name written twice is a name that was changed,
			// and the file's order is the order it was changed in. A name that
			// is the namer's own instruction is read as NO name ([healedTitle],
			// title.go), which is what heals the sessions that were already
			// written down under one.
			if named := healedTitle(entry.Title); named != "" {
				title = named
				shortTitle = compactTitle(healedTitle(entry.ShortTitle))
			}
		}
	}
	// The line the reading stopped at, and zero for a file read to its end. It is
	// a FACT ON THE READING and not an error, because an error here is what every
	// caller already reads as "refuse to open this file" — which is the loss this
	// arm exists to end. The one thing that still refuses is a file written by a
	// newer aforge, above, where refusing is the honest answer.
	var unread int
	if scanner.Err() != nil {
		// The failing Scan never enters the loop, so the line nobody could read is
		// the one AFTER the last one counted.
		unread = scanned + 1
	}
	return replayedSession{
		messages:       messages,
		unread:         unread,
		reasoning:      reasoning,
		earlier:        earlier,
		overlap:        overlap,
		title:          title,
		shortTitle:     shortTitle,
		id:             id,
		images:         images,
		notes:          notes,
		replyTags:      replyTags,
		delivered:      delivered,
		noteDeliveries: noteDeliveries,
		steers:         steers,
		captions:       captions,
		usage:          spent,
		created:        created,
		existed:        lines > 0,
	}, nil
}

// replayedMessage rebuilds one journaled message into the live transcript.
//
// A line with no references is rebuilt exactly as it always was: one text part,
// even when the text is empty — an assistant message that was pure tool calls
// journals empty content, and giving it no content at all would change a shape
// the provider has been accepting all along.
//
// A line WITH references is text-then-parts, which is the order [imageUserMessage]
// assembled and therefore the order the model read the first time. The text part
// is dropped when there was no text, for the same reason it was never added.
func replayedMessage(entry sessionEntry, rebuild bool) ai.Message {
	message := ai.Message{
		Role:       entry.Role,
		Content:    []ai.ContentPart{{Type: "text", Text: entry.Content}},
		ToolCalls:  entry.ToolCalls,
		ToolCallID: entry.ToolCallID,
	}
	if len(entry.Parts) == 0 {
		return message
	}
	content := make([]ai.ContentPart, 0, len(entry.Parts)+1)
	if entry.Content != "" {
		content = append(content, ai.ContentPart{Type: "text", Text: entry.Content})
	}
	for _, part := range entry.Parts {
		content = append(content, rebuiltPart(part, rebuild))
	}
	message.Content = content
	return message
}

// rebuiltPart is the one place the two rebuilds are chosen between: the bytes
// for a transcript about to be sent, the reference for a reading that only
// draws ([journalPart.reference] says why in full).
func rebuiltPart(part journalPart, rebuild bool) ai.ContentPart {
	if rebuild {
		return part.contentPart()
	}
	return part.reference()
}

// compactionMessages rebuilds one compaction marker into the context prefix it
// left behind.
//
// A MARKER WRITTEN BY THE CURRENT PASS LEAVES NOTHING BEHIND, and answers nil.
// Its pass rearranged the transcript rather than replacing it — stubs, a fold
// marker, the verbatim tail — and re-journaled the whole rebuilt window on the
// far side of the line, so the window comes back as ordinary message lines and
// this has nothing to add (loop.go's [Agent.compact]).
//
// THE OTHER TWO SHAPES ARE LEGACY and are read for one reason: a session
// compacted by an older aforge has to still resume as itself.
//
//   - SUMMARY — the prose a summarizer wrote. One note, exactly as it was.
//   - PAGES — the frames rung's images, each re-read only while its digest still
//     matches ([journalPart]), with the summary of any overflow after them.
//
// Neither is written any more.
func compactionMessages(entry sessionEntry, rebuild bool) []ai.Message {
	summary := strings.TrimSpace(entry.Summary)
	if len(entry.Parts) == 0 {
		if summary == "" {
			return nil
		}
		return []ai.Message{textMessage("user", legacyCompactionNote(entry.Summary))}
	}
	content := make([]ai.ContentPart, 0, len(entry.Parts)+1)
	content = append(content, ai.ContentPart{Type: "text", Text: legacyFramesNote})
	for _, part := range entry.Parts {
		content = append(content, rebuiltPart(part, rebuild))
	}
	messages := []ai.Message{{Role: "user", Content: content}}
	if summary != "" {
		messages = append(messages, textMessage("user", legacyCompactionNote(entry.Summary)))
	}
	return messages
}

// compactionOverlap is how many of the messages BELOW one marker are the pass's
// own rewritten copy of the conversation ABOVE it, and -1 when the file does not
// say and the region therefore cannot be placed.
//
// It reads [sessionEntry.Window] and nothing else, which is the whole of its
// discipline. Two shapes answer -1:
//
//   - A MARKER WRITTEN BEFORE THE FIELD EXISTED. The window is down there and
//     its length is not, so a reader can tell that the conversation is written
//     twice and not where the second copy stops.
//   - A LEGACY MARKER, which re-journaled the tail it kept with no count either.
//
// In both cases the region above is dropped and the session behaves exactly as
// it did before any of this: the transcript below the marker is the whole of
// what a surface can draw. A session picks the ability up the next time it
// compacts, because that pass writes the number.
//
// IT DOES NOT GUESS. The length is derivable from Folded — a fold replaces its
// run with one line, so the window is the region less the folded messages plus
// one — and deriving it was rejected: those counts are the RECORD of what a pass
// did, the pass is free to change what it does, and a wrong length here does not
// fail, it silently draws somebody's conversation twice.
func compactionOverlap(entry sessionEntry) int {
	if entry.Window <= 0 {
		return -1
	}
	return entry.Window
}

// legacyFramesNote is what a pre-phase-3 frames marker put in front of its page
// images. It is a string a resume has to be able to reproduce, so it lives here
// beside the only reader left of it.
const legacyFramesNote = "[context compacted] Everything before this point was rendered verbatim to page " +
	"images rather than summarized — the transcript is kept as images below, in order, and " +
	"nothing in it was shortened or rephrased. It is not something either of us said, and any " +
	"question inside it is still open."

// legacyCompactionNote wraps an old marker's summary as the user-role message it
// was written as. User role because it was context handed TO the model rather
// than something it produced, and marked in plain words because a model that
// mistakes a summary for a transcript will answer questions inside it.
func legacyCompactionNote(summary string) string {
	return "[context compacted] Everything before this point was summarized to fit the " +
		"context window. This note is the record of that conversation — it is not something " +
		"either of us said, and any question inside it is still open.\n\n" + summary
}

// replayedSession is what one pass over the journal recovered: the live
// transcript, the session's name, and whether the file had any lines at all.
//
// It is a struct rather than three returns because the three grow together —
// the name arrived here after the other two — and a reader of a call site
// should not have to count positions to know which bool is which.
type replayedSession struct {
	messages []ai.Message
	// reasoning is aligned with messages. Legacy lines and rewritten messages
	// carry zero entries, which means they serialize exactly as before.
	reasoning []provider.MessageReasoning
	// earlier is the transcript as it stood ONE INSTANT BEFORE the latest
	// compaction marker — the conversation the pass edited away, which the file
	// still holds in full and the model no longer carries. Nil for a journal
	// that was never compacted, which is nearly all of them.
	//
	// THE REGION IS ONE HOP AND IS NOT STACKED, and that is a deliberate reading
	// of a file that could be read the other way. A modern pass RE-JOURNALS THE
	// WHOLE REBUILT WINDOW behind its marker ([sessionFile.appendCompaction]), so
	// the lines after an older marker already contain everything before it, edited
	// — a walk that ignored the older markers to "reach the raw beginning" would
	// hand a surface the same conversation twice, once as it happened and once as
	// that pass rewrote it. Applying every marker but the last one instead costs
	// nothing a person can see: a pass never folds a USER message ([Agent.foldLocked]),
	// so the region still opens on the conversation's very first words, with the
	// older passes' stubs and fold lines in it exactly where the file puts them.
	earlier []ai.Message
	// overlap is how many messages at the START of messages are the pass's own
	// rewritten copy of earlier — the rebuilt window it journaled behind its
	// marker (see [compactionOverlap]). The conversation, told once and whole, is
	// therefore `earlier` followed by `messages[overlap:]`.
	//
	// Zero whenever earlier is empty, and never anything else: a region that
	// cannot be placed is not kept.
	overlap    int
	title      string
	shortTitle string
	// id is the header's session id, empty for a file that has no header yet.
	id string
	// images is where this file's pictures came from, keyed by [partKey] — the
	// index [sessionFile.images] is opened holding.
	images map[string]string
	// notes is which of those messages the session wrote itself, keyed by
	// [noteKey] — the index [sessionFile.notes] is opened holding.
	notes map[string]bool
	// replyTags is the typed identity stored on task completion notes.
	replyTags map[string][]TaskReplyTag
	// delivered is the set of durable delivery ids this file already recorded,
	// and noteDeliveries the same ids under their line's fingerprint.
	delivered      map[string]bool
	noteDeliveries map[string][]string
	// steers is which of those messages were spliced into a turn that was
	// already running, keyed by [noteKey] — the index [sessionFile.steers] is
	// opened holding (steer.go).
	steers map[string]SteerMark
	// captions is what the narrator said about each batch, keyed by the batch's
	// first call id — the index [sessionFile.captions] is opened holding.
	captions map[string]journalCaption
	// unread is the 1-based line of the file this reading could not get past, and
	// zero for a file read to its end. Everything above it is in `messages`; the
	// number is what lets a caller say WHICH line rather than "something went
	// wrong somewhere" (see [readJournal]).
	unread int
	// usage is the SUM of the file's usage lines — what this conversation has
	// spent across every process that ever held it. Summed rather than stored,
	// so the total cannot drift from the lines it is made of.
	usage Usage
	// created is every file an earlier process of this session made that was not
	// there before — the "created" lines, in the order they were written. It is
	// the one fact in this file that cannot be re-derived from the transcript,
	// and it is what lets a resumed session still answer for what it left behind
	// ([journalCreated]).
	created []fileChange
	existed bool
}

// repairTranscript makes a replayed transcript legal to send.
//
// A batch's results are journaled only after the whole batch has run
// (loop.go), so a session killed mid-batch leaves an assistant message whose
// tool_calls nobody answered. That is not a cosmetic gap: every provider
// rejects the shape with a 400, and because the transcript is append-only the
// session would be rejected on this request and on every request after it,
// forever — a file that can never be resumed. The mirror shape, a tool result
// whose call was never journaled, is rejected the same way.
//
// Two rules, in order:
//
//  1. an unanswered trailing batch is dropped, together with whatever partial
//     results it did journal — the tools ran, but the model never saw them, so
//     the honest resume is the one where the call was never made;
//  2. a tool message with no call above it is dropped.
func repairTranscript(messages []ai.Message) []ai.Message {
	for index := len(messages) - 1; index >= 0; index-- {
		if len(messages[index].ToolCalls) == 0 {
			continue
		}
		// Only the LAST batch can be the interrupted one, and only if nothing
		// but its results follows: an assistant or user message after it is
		// proof the conversation moved on, which it could not have done
		// through a transcript the provider was refusing.
		answered := make(map[string]bool)
		trailing := true
		for _, later := range messages[index+1:] {
			if later.Role != "tool" {
				trailing = false
				break
			}
			answered[later.ToolCallID] = true
		}
		if trailing {
			for _, call := range messages[index].ToolCalls {
				if !answered[call.ID] {
					messages = messages[:index]
					break
				}
			}
		}
		break
	}

	paired := toolResultCalls(messages)
	repaired := messages[:0]
	for index, message := range messages {
		if message.Role == "tool" && paired[index] == nil {
			continue
		}
		repaired = append(repaired, message)
	}
	if len(repaired) == 0 {
		// nil rather than an empty slice: a fresh session's transcript is nil,
		// and a resume that repaired away to nothing is exactly that.
		return nil
	}
	return repaired
}

func reasoningAfterRepair(repaired, original []ai.Message, reasoning []provider.MessageReasoning) []provider.MessageReasoning {
	byPart := make(map[*ai.ContentPart]provider.MessageReasoning, len(original))
	for index, message := range original {
		if index < len(reasoning) && len(message.Content) > 0 {
			byPart[&message.Content[0]] = reasoning[index]
		}
	}
	kept := make([]provider.MessageReasoning, len(repaired))
	for index, message := range repaired {
		if len(message.Content) > 0 {
			kept[index] = byPart[&message.Content[0]]
		}
	}
	return kept
}

// appendMessage journals one message: its text flattened, and the durable
// references for whatever else it carried.
//
// refs are variadic because almost nothing has any — the model's replies, the
// tool results, the compaction tail — and a call site that passes none writes
// exactly the line it wrote before this existed.
//
// The references are the CALLER's, not derived from the content here: a data URL
// in a part is bytes with no provenance, and by the time a message reaches the
// journal there is no way to recover the path it was read from (see
// [userMessage]).
func (s *sessionFile) appendMessage(message ai.Message, refs ...journalPart) bool {
	return s.append(message, false, refs)
}

func (s *sessionFile) appendReasonedMessage(message ai.Message, reasoning provider.MessageReasoning) {
	s.appendWithReasoning(message, false, nil, reasoning)
}

// journalPath is the file this journal is writing — the real filesystem path,
// empty when there is no file. Compaction interpolates the fold marker from
// this rather than inventing a second location: it is the path the model can
// grep or read.
func (s *sessionFile) journalPath() string {
	if s.closed || s.file == nil {
		return ""
	}
	name := s.file.Name()
	if name == "" {
		return ""
	}
	if filepath.IsAbs(name) {
		return name
	}
	abs, err := filepath.Abs(name)
	if err != nil {
		// A relative name is still the file we are writing, and pointing at it
		// beats a fold marker that pretends there is no journal.
		return name
	}
	return abs
}

// journalName is [sessionFile.journalPath] for a caller holding no lock, and
// the empty string for a session with no file. It reads a name and never the
// file, so it is cheap enough to ask at every task admission (admission.go) and
// on the request path, where the snapshot view names a journal without reading
// one (toolcompact.go).
func (s *sessionFile) journalName() string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.journalPath()
}

// messageLines names the journal this session is writing and the most recent
// line carrying each of first and last, so a fold marker can point at a range
// in a file the model can grep or read rather than at a store id neither tool
// can open. A line is 0 when that message never reached the file, and the path
// then stands on its own, which is still somewhere to look.
//
// ONE PASS FOR BOTH ENDS. The record is read whole and parsed line by line, so
// asking for the two ends separately would read and parse the whole of a long
// conversation twice to learn two numbers.
func (s *sessionFile) messageLines(first, last ai.Message) (journal string, fromLine, toLine int) {
	if s == nil {
		return "", 0, 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	journal = s.journalPath()
	if journal == "" {
		return "", 0, 0
	}
	content, err := os.ReadFile(journal)
	if err != nil {
		// The path still stands: this journal holds the file OPEN, so it is
		// there to grep whatever went wrong with this one read. What is not
		// known is which lines, and the marker says the file alone rather
		// than a line number nothing verified.
		return journal, 0, 0
	}
	wantFirst, wantLast := chatRefKey(first), chatRefKey(last)
	lineNumber := 0
	for _, line := range bytes.Split(content, []byte{'\n'}) {
		lineNumber++
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var entry sessionEntry
		if json.Unmarshal(line, &entry) != nil || entry.Type != "message" {
			continue
		}
		candidate := ai.Message{Role: entry.Role, ToolCallID: entry.ToolCallID,
			Content: []ai.ContentPart{{Type: "text", Text: entry.Content}}, ToolCalls: entry.ToolCalls}
		// Both ends are asked separately rather than in a switch, because a
		// run of one message has the same key at both ends and a fold of one
		// message still knows the line it sat on.
		key := chatRefKey(candidate)
		if key == wantFirst {
			fromLine = lineNumber
		}
		if key == wantLast {
			toLine = lineNumber
		}
	}
	return journal, fromLine, toLine
}

// appendNote is appendMessage for a line the SESSION wrote (see
// [sessionEntry.Note]). It is a separate door rather than a flag on the common
// one because exactly one caller has the answer — [Agent.recordUserLocked],
// which is holding the [userMessage] the mark comes off — and every other call
// site should stay the call it was.
// noteMarks is what a session-authored line carries besides its words: the
// identity of the task it is about, and the durable deliveries it is the record
// of ([durableDelivery]).
type noteMarks struct {
	tags       []TaskReplyTag
	deliveries []deliveryID
}

func (s *sessionFile) appendNote(message ai.Message, marks noteMarks) bool {
	return s.append(message, true, nil, marks)
}

// appendSteer is appendMessage for a person's line that was typed INTO work
// already running: spliced into this conversation's turn (steer.go), or carried
// across to a node the person is standing in the room of (task_room.go). It is
// the same message line every other user message writes, with the mark that says
// it did not open the turn it sits in and the instant the person actually sent
// it.
//
// ONE DOOR FOR BOTH, because the record keeps one fact about both: the person
// corrected work that was moving. The two engine doors stay two doors — one
// splices a turn and one delivers to another agent, and they promise different
// things — and what the mark carries says which promise was kept ([SteerMark]).
//
// It is a separate door rather than a flag on the common one, on
// [sessionFile.appendNote]'s terms: exactly one caller has the answer —
// [Agent.recordUserLocked], which is holding the [userMessage] the mark comes
// off — and every other call site should stay the call it was.
//
// A steer carries no pictures (neither door takes any), which is why this takes
// no references.
func (s *sessionFile) appendSteer(message ai.Message, mark SteerMark) bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	if s.steers == nil {
		s.steers = make(map[string]SteerMark, 4)
	}
	rememberSteer(s.steers, message, mark)
	s.mu.Unlock()
	return s.writeLine(sessionEntry{
		Type:      "message",
		Role:      message.Role,
		Content:   messageContentText(message),
		Steer:     &journalSteer{At: steerStamp(mark.At), Consumed: mark.Consumed, Landing: mark.Landing},
		Timestamp: stamp(),
	})
}

// appendSteerFellThrough journals a steer that NEVER REACHED THE MODEL: the turn
// it was aimed at ended — answered, faulted or stopped — with the sentence still
// waiting for a step boundary that never came (steer.go).
//
// IT IS NOT A MESSAGE LINE, and that is the whole point of it. These words are
// not in that turn's transcript and never were, so a `message` line would be the
// record claiming they were read. What replays into the conversation is the
// ordinary question they became a moment later, on the follow-up queue; this
// line is the part of the truth the conversation alone cannot tell — that the
// question started life as a correction to the turn above it.
//
// Nothing is rebuilt from it. A replay reads it and carries on, which is what
// keeps a session written by this build resumable by one that reads the words
// and not the mark.
func (s *sessionFile) appendSteerFellThrough(note SteerNote) {
	if s == nil {
		return
	}
	s.writeLine(sessionEntry{
		Type:      "steer",
		Role:      "user",
		Content:   note.Words,
		Steer:     &journalSteer{At: steerStamp(note.At), Consumed: false, Landing: note.Landing},
		Timestamp: stamp(),
	})
}

func (s *sessionFile) append(message ai.Message, note bool, refs []journalPart, marks ...noteMarks) bool {
	return s.appendWithReasoning(message, note, refs, provider.MessageReasoning{}, marks...)
}

// appendWithReasoning answers whether the line reached the file, for the one
// caller that may not act on a write that did not happen ([writeLine]).
func (s *sessionFile) appendWithReasoning(message ai.Message, note bool, refs []journalPart, reasoning provider.MessageReasoning, marks ...noteMarks) bool {
	// Indexed as it is written, not only as it is replayed: a picture attached
	// an hour ago is one a rewind or a /compact can put back through the display
	// shaping in THIS process, long before anybody resumes the file. The same is
	// true of a note: the woken turn it belongs to is drawn in THIS process, and
	// a /compact or a rewind can put its line back through the shaping.
	s.rememberParts(message, refs)
	if note {
		s.mu.Lock()
		if s.notes == nil {
			s.notes = make(map[string]bool, 4)
		}
		rememberNote(s.notes, message)
		if len(marks) > 0 && len(marks[0].tags) > 0 {
			if s.replyTags == nil {
				s.replyTags = make(map[string][]TaskReplyTag)
			}
			rememberReplyTags(s.replyTags, message, marks[0].tags)
		}
		s.mu.Unlock()
	}
	// The single text part is what nearly every message is, and its text is
	// already the string the line wants; a Builder would copy a whole tool
	// result to arrive back at it.
	var text string
	if len(message.Content) == 1 && message.Content[0].Type == "text" {
		text = message.Content[0].Text
	} else {
		var flattened strings.Builder
		for _, part := range message.Content {
			if part.Type == "text" {
				flattened.WriteString(part.Text)
			}
		}
		text = flattened.String()
	}
	var (
		tags       []TaskReplyTag
		deliveries []string
	)
	if len(marks) > 0 {
		tags = marks[0].tags
		for _, id := range marks[0].deliveries {
			deliveries = append(deliveries, string(id))
		}
	}
	wrote := s.writeLine(sessionEntry{
		Type:             "message",
		Role:             message.Role,
		Content:          text,
		ToolCalls:        message.ToolCalls,
		ToolCallID:       message.ToolCallID,
		ReasoningField:   reasoning.Field,
		Reasoning:        reasoning.Text,
		ReasoningDetails: append(json.RawMessage(nil), reasoning.Details...),
		ReasoningModel:   reasoning.Model,
		Parts:            refs,
		Note:             note,
		ReplyTags:        tags,
		Deliveries:       deliveries,
		Timestamp:        stamp(),
	})
	// The ids are indexed only once the line is really on disk: this index is
	// what a resume trusts to say a landing has already been told.
	if wrote && len(deliveries) > 0 {
		s.mu.Lock()
		if s.delivered == nil {
			s.delivered = make(map[string]bool, len(deliveries))
		}
		if s.noteDeliveries == nil {
			s.noteDeliveries = make(map[string][]string, 1)
		}
		rememberDeliveries(s.delivered, deliveries)
		rememberNoteDeliveries(s.noteDeliveries, message, deliveries)
		s.mu.Unlock()
	}
	return wrote
}

// appendCompaction journals one pass: the marker, then the whole rebuilt window
// again.
//
// The re-journal is what makes a compacted session resumable as itself. Replay
// discards everything before the marker — that is the marker's meaning — so
// whatever the pass decided the window should be has to sit on the far side of
// it, or a resume comes back holding a prefix the pass deliberately edited.
//
// IT IS THE WHOLE WINDOW AND NOT THE TAIL, which is the change this pass makes
// to the file's meaning. The old pass only ever edited by DELETION: everything
// above the cut became one summary, so the tail was the only thing whose text
// the marker had to carry forward. The new pass edits in place — a tool result
// becomes a stub, a run of assistant work becomes one line — and those edits sit
// above the marker among lines the file already holds in their original form. So
// the window is written out entire, and the lines above the marker stay exactly
// what they were: the RECORD of what happened, which is what the journal is for.
//
// The counts ride the marker so a surface reading the file back can say what the
// pass did without re-deriving it. Nothing rebuilds from them.
func (s *sessionFile) appendCompaction(pass compactionPass, tokensBefore int, window []ai.Message, sidecars ...[]provider.MessageReasoning) {
	s.writeLine(sessionEntry{
		Type:         "compaction",
		TokensBefore: tokensBefore,
		Stubbed:      pass.stubbed,
		Folded:       pass.folded,
		// The length is written BEFORE the window it describes, which is the only
		// order that survives a crash halfway through: a reader that finds fewer
		// lines than the number promised has a truncated file and can say so,
		// where a count written afterwards would simply never arrive.
		Window:    len(window),
		Timestamp: stamp(),
	})
	for index, message := range window {
		// A KEPT LINE IS RE-JOURNALED AS WHAT IT WAS. The window is written again
		// on the far side of the marker (above), and a note re-written without its
		// mark would come back from the next resume as the person's words — this
		// pass is the one place a message is journaled twice.
		if s.isNote(message) {
			s.appendNote(message, noteMarks{
				tags:       s.taskReplyTags(message),
				deliveries: s.noteDeliveriesOf(message),
			})
			continue
		}
		// AND SO IS A SPLICED ONE, for the same reason: a steer re-written without
		// its mark would come back from the next resume as a question of its own,
		// and the turn it was typed into would lose the correction that shaped it.
		if mark := s.steerMark(message); mark != nil {
			s.appendSteer(message, *mark)
			continue
		}
		var reasoning provider.MessageReasoning
		if len(sidecars) > 0 && index < len(sidecars[0]) {
			reasoning = sidecars[0][index]
		}
		s.appendReasonedMessage(message, reasoning)
	}
}

// appendRewind journals one rewind: the count of messages it removed from the
// live transcript. Nothing in the file is rewritten — the dropped lines stay
// where they are, and the marker is what a replay reads them against. That is
// what keeps the journal a record of what happened rather than of what is
// currently believed: a rewound turn really did run, and its tool calls really
// did touch the workspace.
func (s *sessionFile) appendRewind(dropped int) {
	if dropped <= 0 {
		return
	}
	s.writeLine(sessionEntry{Type: "rewind", Dropped: dropped, Timestamp: stamp()})
}

// appendTitle journals the session's name. It is one line, appended like any
// other: a later name simply lands after this one, and the replay takes the
// last. Nothing rewrites the file.
func (s *sessionFile) appendTitle(title, short string) {
	title = strings.TrimSpace(title)
	if title == "" {
		return
	}
	s.mu.Lock()
	s.title, s.shortTitle = title, strings.TrimSpace(short)
	s.mu.Unlock()
	s.writeLine(sessionEntry{Type: "title", Title: title, ShortTitle: strings.TrimSpace(short), Timestamp: stamp()})
}

// appendUsage journals what one seal cost: the turn's own figures, or one
// auxiliary call's beside it.
//
// It is the file's half of the one-source-of-truth law. The session total is
// the SUM of these lines and is stored nowhere else, so a resumed conversation
// knows what it spent by adding them up (see [replaySessionFile]) rather than
// by trusting a number some earlier process wrote down.
//
// A SEAL THAT SPENT NOTHING WRITES NOTHING. An instantly-cancelled turn, and
// the zero-token seals the harness, an orchestrated run and the image turn send
// because their spend went through the auxiliary door, all leave no row — the
// emptiness law applied to the file.
//
// The NIL RECEIVER writes nothing, for the reason [sessionFile.isNote] answers
// false: a memory-only session has no journal, and the caller should not have
// to test for a file before sealing a turn.
func (s *sessionFile) appendUsage(used Usage, model string, aux bool, role string) {
	if s == nil {
		return
	}
	if used.Input == 0 && used.Output == 0 && used.CostUSD == 0 {
		return
	}
	s.writeLine(sessionEntry{
		Type: "usage",
		Usage: &journalUsage{
			Model:      strings.TrimSpace(model),
			Input:      used.Input,
			Output:     used.Output,
			CacheRead:  used.CacheRead,
			CacheWrite: used.CacheWrite,
			CostUSD:    used.CostUSD,
			Calls:      used.Calls,
			DurationMS: used.Duration.Milliseconds(),
			Aux:        aux,
			Empty:      used.EmptyReflex > 0,
			Role:       strings.TrimSpace(role),
		},
		Timestamp: stamp(),
	})
}

// appendCall writes ONE response's own accounting down, beside the seal that
// will sum it.
//
// A RESPONSE THAT REPORTED NO USAGE WRITES NOTHING. The emptiness law, and the
// same test the seal keeps: a call with no tokens and no cost is a call the
// provider said nothing about, and a row of zeroes would read as a fact. A
// stream that was cut before its final chunk is exactly that case.
//
// The nil receiver writes nothing, as everywhere in this file: a memory-only
// session has no journal and no caller should have to know it.
func (s *sessionFile) appendCall(call journalCall) {
	if s == nil {
		return
	}
	if call.Input == 0 && call.Output == 0 && call.CacheRead == 0 && call.CacheWrite == 0 && call.CostUSD == 0 {
		return
	}
	s.writeLine(sessionEntry{Type: "call", Call: &call, Timestamp: stamp()})
}

// appendError writes ONE FAILED CALL down (see [journalError]).
//
// A FAILURE ALWAYS WRITES, which is where this parts company with every other
// append in this file. The emptiness law is about numbers nobody reported; a
// call that failed with no status, no provider and no words is not an absence of
// news — it is the news, and it is precisely the shape the measured run left
// behind. The one thing that writes nothing is the nil receiver, as everywhere
// here: a memory-only session has no journal and no caller should have to know.
func (s *sessionFile) appendError(failure journalError) {
	if s == nil {
		return
	}
	s.writeLine(sessionEntry{Type: "error", Error: &failure, Timestamp: stamp()})
}

// appendFailure writes ONE CLASSIFICATION down (see [journalFailure]).
//
// A CLASSIFICATION ALWAYS WRITES, for [sessionFile.appendError]'s reason: the
// whole point of the line is that a decision was taken about a failure, and a
// decision nobody wrote down cannot be measured. The nil receiver writes
// nothing, as everywhere in this file.
func (s *sessionFile) appendFailure(failure journalFailure) {
	if s == nil {
		return
	}
	s.writeLine(sessionEntry{Type: "failure", Failure: &failure, Timestamp: stamp()})
}

// appendMark writes ONE mark's reading down (see [journalMark]).
//
// A MARK THAT NEVER HAPPENED WRITES NOTHING, which is the emptiness law applied
// to a file a person reads: a turn that crossed no mark, and a session that
// cannot reach a reader at all, leave the journal exactly as it was before any
// of this existed. The caller's own guard is the one that knows — a read that
// was never attempted is not a read — and this repeats it on the decision,
// because a line with no decision on it says nothing about anything.
//
// The nil receiver writes nothing, as everywhere in this file.
func (s *sessionFile) appendMark(mark journalMark) {
	if s == nil || strings.TrimSpace(mark.Decision) == "" {
		return
	}
	s.writeLine(sessionEntry{Type: "mark", Mark: &mark, Timestamp: stamp()})
}

// appendCeiling writes down what the last mark did with the turn (see
// [journalCeiling]). A ceiling that did not fire writes nothing, for
// [sessionFile.appendMark]'s reason.
func (s *sessionFile) appendCeiling(ceiling journalCeiling) {
	if s == nil || strings.TrimSpace(ceiling.Decision) == "" {
		return
	}
	s.writeLine(sessionEntry{Type: "ceiling", Ceiling: &ceiling, Timestamp: stamp()})
}

// appendCarry writes down ONE RUNG of the brief ladder (see [journalCarry]).
//
// A rung with no outcome on it writes nothing, for [sessionFile.appendCeiling]'s
// reason: the whole value of the line is saying what that rung DID, and a line
// that cannot say it is a line that says a rung existed — which the code already
// says.
func (s *sessionFile) appendCarry(carry journalCarry) {
	if s == nil || strings.TrimSpace(carry.Rung) == "" || strings.TrimSpace(carry.Outcome) == "" {
		return
	}
	s.writeLine(sessionEntry{Type: "carry", Carry: &carry, Timestamp: stamp()})
}

// appendDivision writes down one division put to the road (see
// [journalDivision]). A division nobody asked for writes nothing, for
// [sessionFile.appendMark]'s reason: the whole value of the line is telling
// never-asked from refused, and a line with no decision on it says neither.
func (s *sessionFile) appendDivision(division journalDivision) {
	if s == nil || strings.TrimSpace(division.Decision) == "" {
		return
	}
	s.writeLine(sessionEntry{Type: "division", Division: &division, Timestamp: stamp()})
}

// appendPrincipal writes down one decision the session's goal owner made (see
// [journalPrincipal]). A moment with no event on it writes nothing, for
// [sessionFile.appendMark]'s reason.
func (s *sessionFile) appendPrincipal(moment journalPrincipal) {
	if s == nil || strings.TrimSpace(moment.Event) == "" {
		return
	}
	s.writeLine(sessionEntry{Type: "principal", Principal: &moment, Timestamp: stamp()})
}

// appendAbandoned writes down ONE TURN THAT WAS LET GO OF (see
// [journalAbandoned]).
//
// AN ABANDONMENT ALWAYS WRITES, which is where this parts company with the
// emptiness law the appends above keep. The line's whole content is the fact
// that it happened: a turn let go of before it had spent a cent is exactly the
// case a reader most needs to be able to see, and a guard on the figures would
// suppress it. The one thing that writes nothing is the nil receiver, as
// everywhere in this file.
//
// AND IT IS WRITTEN ONCE PER TURN, which is [Agent.Abandon]'s guarantee and not
// this function's: the door reports false for a session with no turn in flight,
// so a second deadline landing on an already-abandoned turn never reaches here.
func (s *sessionFile) appendAbandoned(turn journalAbandoned) {
	if s == nil {
		return
	}
	s.writeLine(sessionEntry{Type: "abandoned", Abandoned: &turn, Timestamp: stamp()})
}

// appendCreated writes down one file the session made (see [journalCreated]). A
// line with no path on it writes nothing, for [sessionFile.appendMark]'s reason.
func (s *sessionFile) appendCreated(made journalCreated) {
	if s == nil || strings.TrimSpace(made.Path) == "" {
		return
	}
	s.writeLine(sessionEntry{Type: "created", Created: &made, Timestamp: stamp()})
}

// writeLine marshals one entry and appends it. A failed write is dropped
// rather than raised: the journal is a record of the conversation, and a
// person mid-turn cannot act on "the transcript did not save" — the next
// Close reports the state of the file.
// writeLine answers whether the line REACHED THE FILE. Nearly every caller
// ignores it — a journal that cannot be written is not a reason to stop the
// conversation — but a durable delivery may not be acknowledged on a write that
// did not happen ([Agent.recordUserLocked]), so the failure has to be sayable.
func (s *sessionFile) writeLine(entry any) bool {
	payload, err := json.Marshal(entry)
	if err != nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false
	}
	payload = append(payload, '\n')
	if _, err := s.file.Write(payload); err != nil {
		return false
	}
	return true
}

// Close flushes the file, releases the claim, and closes the descriptor.
// Writes are unbuffered appends, so the flush is the kernel's; Close is what
// makes the file safe for another aforge to open. Calling it twice is safe.
//
// The explicit unlock is belt-and-braces — closing the descriptor drops the
// flock on its own — but it states the release at the place a reader looks for
// it, and it puts the release before the close rather than as a side effect of
// it. Nothing can slip into the gap: closed is already set, so no write reaches
// the file after this point.
func (s *sessionFile) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	if err := s.file.Sync(); err != nil {
		// Sync failing on a tmpfs or a pipe is not a lost transcript; the
		// close below is the one that matters.
		_ = err
	}
	if s.locked {
		s.locked = false
		_ = filelock.Unlock(s.file)
	}
	return s.file.Close()
}

func stamp() string { return time.Now().UTC().Format(time.RFC3339Nano) }

// InUse reports whether another aforge is holding this transcript open.
//
// It is the same flock [lockSessionFile] takes, asked as a question rather than
// as a claim: the lock is tried and released at once, so the answer is "somebody
// else has it right now" and nothing is left behind. A migration and a launch
// groom both need it — moving or removing a session another window is writing
// is the one way either of them could cost somebody a live conversation — and
// both would rather skip a folder than take one.
//
// A file that is not there, cannot be opened, or sits on a filesystem with no
// locking answers FALSE, for the reason [lockSessionFile] opens unlocked on such
// a filesystem: the guard is worth having where it works and is never worth
// refusing the work over.
func InUse(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer file.Close()
	if err := filelock.Lock(file, true, true); err != nil {
		return filelock.IsBusy(err)
	}
	_ = filelock.Unlock(file)
	return false
}

// NewSessionID is 16 random hex characters: enough to name every session a
// machine will ever hold without a coordinator.
//
// It is EXPORTED because the folder is named by it (place.go): the surface
// mints the id, makes the directory, and hands the same id back here for the
// header — one law for what a session id is, applied at both ends.
func NewSessionID() string {
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err != nil {
		// crypto/rand failing is not a reason to refuse to open a session.
		return fmt.Sprintf("%016x", time.Now().UnixNano())
	}
	return hex.EncodeToString(raw[:])
}
