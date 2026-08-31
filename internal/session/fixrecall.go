package session

// The one line a failed tool result grows, and the bookkeeping that earns it.
//
// fixstore.go is the memory; this file is the moment it is used. Both halves
// happen at [Agent.executeTool]'s chokepoint (loop.go), which is the whole
// reason the chokepoint is worth having: the batch, the early start that begins
// a read while the response is still streaming, a task node's turns and an
// adaptive run's worker all pass through that one function, so wiring here wires
// every one of them at once and no future call site can forget.
//
// ── THE LANE ──
//
// A "lane" is one tool inside one turn. The lane remembers the last failure that
// produced a signature, and nothing else; the next call on the SAME tool answers
// for it. A success says the failure was fixed and the succeeding call is the
// patch. A failure with the same signature, after a patch was offered, says the
// patch did not work.
//
// The lane lives on the episode, so IT DIES WITH THE TURN — the same law the
// loop detector's window is written under (looped.go): a fix credited to a
// command run half an hour and four subjects later is not evidence about
// anything, and a patch blamed across that gap is worse.
//
// ── WHAT THE MODEL IS NOT TOLD ──
//
// There is no tool for this and prompts/system.md does not mention it. The model
// is given no verb, nothing to call, and nothing to ask for. It meets the
// sidecar exactly once — as one more line at the bottom of a result it was going
// to read anyway — because a capability the model can INVOKE is a capability it
// will invoke speculatively, and this one is only ever worth anything at the
// instant something has already gone wrong.

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// fixLaneTools is which hands keep an error→fix lane at all.
//
// ── A PATCH MUST BE AN INSTRUCTION, NOT AN OPERAND ──
//
// One hand qualifies, and it is bash: the argument that says what a bash call
// did is a COMMAND, and offering one back to a model that has just hit the same
// error is offering it a move.
//
// read, write, edit and ls are absent because their defining argument is a PATH,
// and the reason is the normalization law from the other direction: their
// commonest failure — "no such file or directory" — normalizes to one signature
// that every missing file on the machine shares, so the patch stored under it
// would be one arbitrary path, correct for the call that recorded it and wrong
// for every later one.
//
// GREP AND FIND WERE HERE UNTIL THE PATTERN CAME BACK AS A CURE. Their defining
// argument is a search pattern, which is the same objection one step along: a
// pattern is what the model was LOOKING FOR, not a thing to do about a failure.
// A real session's store held `/\/+$` filed as the answer to grep's "Path not
// found", and the line under it read "what ran next and it went away: /\/+$" —
// a regex handed to a model as a command (fixremedy.go's first measured
// failure). No amount of counting that entry better could have made it right.
//
// Adding a hand here is one line, and the question to ask before doing it is
// whether its argument is something the next session could TYPE.
var fixLaneTools = map[string]bool{
	"bash": true,
}

// fixLane is one turn's memory of what has just failed, per tool.
//
// The mutex is load-bearing: a tool batch runs its calls concurrently
// (loop.go's runToolsWarm), so two hands can be failing at the same instant.
type fixLane struct {
	mu      sync.Mutex
	pending map[string]fixPending
}

// fixPending is one unanswered failure.
type fixPending struct {
	// signature is the key the failure normalized to.
	signature string
	// advised is the patch that was injected beside that failure, empty when
	// nothing was said. It is what makes the NEXT event a verdict on this
	// store's advice rather than an unrelated observation.
	advised string
	// from is the store the advice came from, so the outcome is counted where
	// the advice was found.
	from *fixStore
}

func newFixLane() *fixLane { return &fixLane{pending: map[string]fixPending{}} }

func (l *fixLane) remember(tool string, pending fixPending) {
	if l == nil {
		return
	}
	l.mu.Lock()
	l.pending[tool] = pending
	l.mu.Unlock()
}

// take reads and clears one tool's pending failure.
func (l *fixLane) take(tool string) (fixPending, bool) {
	if l == nil {
		return fixPending{}, false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	pending, known := l.pending[tool]
	if known {
		delete(l.pending, tool)
	}
	return pending, known
}

// peek reads one tool's pending failure without clearing it.
func (l *fixLane) peek(tool string) (fixPending, bool) {
	if l == nil {
		return fixPending{}, false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	pending, known := l.pending[tool]
	return pending, known
}

// ── the citizen ─────────────────────────────────────────────────────────────

// fixMemory is the sidecar as a control-plane citizen (hooks.go). It implements
// `episode-init` and nothing else: the lane is turn state and is hung on the
// episode there, exactly as the loop detector's window and the change ledger's
// list are, so that the one thing this mechanism remembers cannot outlive the
// turn that saw it.
//
// The USE of the lane is not a hook. It happens inside executeTool because the
// line has to be on the result BEFORE runTurn records the tool message — the
// post-feedback seam runs after that write, and a citizen there would be
// annotating a string nobody was ever going to send.
type fixMemory struct{ agent *Agent }

func (fixMemory) Name() string { return "fixes" }

func (fixMemory) EpisodeInit(ep *episode) { ep.fixes = newFixLane() }

// ── the shelf, built once per agent ─────────────────────────────────────────

// fixShelfFor builds this agent's two stores on first use.
//
// On FIRST USE rather than at construction, for the reason the working-state
// store is built that way (session.go's stateOnce): a conversation in which
// nothing ever fails should touch no file, and the cost of the store is a read
// nobody has needed yet.
func (a *Agent) fixShelfFor() *fixShelf {
	a.fixOnce.Do(func() { a.fixShelf = newFixShelf(a.config.fixesBucket()) })
	return a.fixShelf
}

// ── the injection ───────────────────────────────────────────────────────────

// noteToolOutcome is the whole of the sidecar's live behaviour, and it is called
// once per executed tool call with that call's result.
//
// It answers with the result the model will read: unchanged on success, and on a
// failure this store recognises, the same text with ONE line appended.
//
// It is a silent no-op in four cases, and they are most of what happens: a call
// on a hand that keeps no lane, a call on an agent with no turn around it, a
// success that answers no earlier failure, and a failure whose text normalizes
// to nothing worth keying on.
func (ep *episode) noteToolOutcome(call ai.ToolCall, result toolResult) toolResult {
	if ep == nil || ep.agent == nil || ep.fixes == nil {
		return result
	}
	tool := call.Function.Name
	if !fixLaneTools[tool] {
		return result
	}
	shelf := ep.agent.fixShelfFor()
	if shelf == nil {
		return result
	}
	if result.isError {
		return ep.noteFailure(shelf, tool, result)
	}
	ep.noteSuccess(shelf, tool, call)
	return result
}

// noteFailure consults the store, settles any verdict the previous failure on
// this lane was owed, and appends the line.
func (ep *episode) noteFailure(shelf *fixShelf, tool string, result toolResult) toolResult {
	// THE FIRST QUESTION IS WHETHER ANY COMMAND COULD HAVE FIXED THIS AT ALL. A
	// door's refusal, a hand that is not on the belt, and a program this machine
	// does not have are all failures whose "fix" would be nothing but the next
	// thing the model typed (fixblame.go's law, and the run that put it there).
	//
	// The lane is left exactly as it was, for the reason an unkeyable failure
	// leaves it: a refusal in the middle of a broken lane does not mend the lane,
	// and clearing it would throw away the signature the eventual success was
	// going to confirm. It is simply NOT AN EVENT this sidecar has anything to
	// say about — nothing is learned, and nothing is offered.
	if blamelessFailure(result.text) {
		return result
	}

	signature, keyed := fixSignature(tool, result.text)
	if !keyed {
		// A failure with nothing to key on leaves the lane exactly as it was. It
		// is still the same broken lane — a command that fails twice, once
		// unreadably, is one problem — and clearing it here would throw away the
		// signature the eventual success was going to confirm.
		return result
	}

	// THE SAME ERROR, AFTER A PATCH WAS OFFERED, IS THE PATCH FAILING. Only the
	// same signature counts: a retry that failed differently is a retry that
	// changed something, and blaming the patch for the new error would be
	// counting a step forward as a step back.
	if previous, waiting := ep.fixes.peek(tool); waiting && previous.advised != "" && previous.signature == signature {
		shelf.blame(signature, previous.advised)
		if previous.from != nil {
			previous.from.outcome(false)
		}
	}

	advice := shelf.consult(signature)
	pending := fixPending{signature: signature}
	if len(advice) > 0 {
		pending.advised = advice[0].patch
		pending.from = advice[0].from
	}
	ep.fixes.remember(tool, pending)
	if len(advice) == 0 {
		return result
	}
	result.text = fixAnnotate(result.text, advice)
	return result
}

// noteSuccess records the command that made the lane's last failure go away.
//
// The patch is the VERBATIM argument that says what the call did — bash's
// command, read's path — taken from [glossField], which is this package's one
// answer to "which argument is the work". No model is asked to summarise it and
// no model is asked whether it really was the fix: the pairing is an
// observation, and the counts are only worth anything while they stay one.
func (ep *episode) noteSuccess(shelf *fixShelf, tool string, call ai.ToolCall) {
	pending, waiting := ep.fixes.take(tool)
	if !waiting || pending.signature == "" {
		return
	}
	patch := fixPatchOf(call)
	if patch == "" {
		return
	}
	// AND JUNK IS KEPT OUT OF THE FILE, not merely kept quiet on the way out.
	// The same reading that decides whether a patch may be spoken decides
	// whether it is worth writing down (fixremedy.go): a string nothing could
	// ever run is not a remedy this session simply has too little evidence for,
	// it is not a remedy, and an entry for it would sit in a file two scopes
	// wide until the decay took it away.
	if !fixRunnableRemedy(patch) {
		return
	}
	// The verdict on this store's own advice is narrow on purpose. It is a
	// `worked` only when the patch that was OFFERED is the patch that then
	// succeeded; a retry that succeeded with something else means the line was
	// read and set aside, which is a fact about the model's judgement and not
	// about the advice's accuracy.
	//
	// IT IS THE SAME QUESTION THE ENTRY'S OWN `worked` COUNT ANSWERS, so it is
	// asked once and carried into both — the store-wide counter and the patch's
	// own record. What the two do with it differs: the counter belongs to the
	// store that ANSWERED, because it is that file's own hit rate, and the count
	// belongs to the patch in both files, because "this command was handed back
	// and it worked" is a fact about the command and not about which file
	// remembered it.
	advised := pending.advised != "" && fixCleanPatch(pending.advised) == patch
	shelf.confirm(pending.signature, patch, advised)
	if advised && pending.from != nil {
		pending.from.outcome(true)
	}
}

// fixAnnotate appends the advice to a failed result.
//
// THE WORDING IS THE PRODUCT, AND IT SAYS ONLY WHAT WAS OBSERVED. It states what
// happened, how well it went, and what to try, in that order, in a person's
// words — because this line lands in the model's context AND on the person's
// screen, in the output of the tool row they can open. There is no jargon in it
// and no name for the machinery behind it.
//
// There are TWO SHAPES, and the difference between them is the difference
// between a patch that has been handed back and seen to work and a patch nobody
// has ever re-run:
//
//	this exact error was fixed 7/8 times before · what worked: go build ./...
//	this exact error came up here before · what ran next and it went away: go build ./...
//
// THE FIRST IS ONLY EVER PRINTED WHEN THIS PATCH HAS WORKED — when it was
// offered on this error, the offer was taken, and the error went away
// (fixEntry.Worked). The second is the honest shape of the weaker observation
// every entry starts life as: a command followed a failure and the failure did
// not come back, which is a pairing and not yet a cure. The store that put this
// distinction here was carrying `worked: 0` in its own counters while telling
// the model "fixed 1/1 times · what worked: pwd" — a claim about a command
// nothing had ever seen work.
//
// A patch that has worked still reads as a fraction (`3/3`), which is the honest
// shape — "3 times before" would hide the denominator that makes the number mean
// anything. The denominator is the offers that were TAKEN and the ones that came
// back, because those are the only two things a fraction here can be measuring.
func fixAnnotate(text string, advice []fixAdvice) string {
	lines := make([]string, 0, len(advice))
	for index, one := range advice {
		if index >= fixAdviceLimit {
			break
		}
		if one.worked > 0 {
			lines = append(lines, fmt.Sprintf(
				"this exact error was fixed %d/%d times before · what worked: %s",
				one.worked, one.worked+one.failed, one.patch,
			))
			continue
		}
		lines = append(lines, fmt.Sprintf(
			"this exact error came up here before · what ran next and it went away: %s",
			one.patch,
		))
	}
	if len(lines) == 0 {
		return text
	}
	joined := strings.Join(lines, "\n")
	if strings.TrimSpace(text) == "" {
		return joined
	}
	return strings.TrimRight(text, "\n") + "\n\n" + joined
}

// fixPatchOf reads the one argument that says what a call DID.
//
// It reuses [glossField] rather than keeping a second table, on this codebase's
// one-source-of-truth law: the argument a person is shown a call by is the
// argument that identifies the work, and a second list would drift from the
// first the first time a tool was added.
//
// It reads the field WHOLE rather than through [glossValue], which is the one
// difference between a gloss and a patch. A gloss is a headline and takes the
// first line; a patch is meant to be repeated, and a shell command cut off after
// its first line is advice that would not run.
//
// A tool with no gloss field — one whose work no single argument describes —
// yields no patch and records nothing. That is the right refusal: a patch has to
// be something a model can read and repeat.
func fixPatchOf(call ai.ToolCall) string {
	field, known := glossField[call.Function.Name]
	if !known {
		return ""
	}
	var args map[string]json.RawMessage
	if err := json.Unmarshal([]byte(call.Function.Arguments), &args); err != nil {
		return ""
	}
	raw, present := args[field]
	if !present {
		return ""
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		value = string(raw)
	}
	return fixCleanPatch(value)
}
