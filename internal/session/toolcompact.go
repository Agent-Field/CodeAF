package session

// Tool-result compaction is a VIEW of the turn loop's frozen history, not a
// rewrite of the transcript.
//
// F9 measured the cost: every tool round-trip re-sends the whole conversation,
// and old tool results are the bulk (compl/prompt ≈ 0.047 — the model reads
// about twenty-one times what it writes). Cross-turn stubbing (stub.go) waits
// until a turn has finished, and the current-turn fold (turnfold.go) waits
// until the working set crosses tens of thousands of tokens. This pass keeps
// older results bounded before another turn starts paying to resend them.
//
// THE LIVE TRANSCRIPT STAYS WHOLE. Only the snapshot about to leave for the
// provider is rewritten. The journal is the record — stub.go's law, applied
// here for the same reason: a request that quietly shrank the file it was
// built from would be a different kind of file than the one this session
// promises. Scroll-back, rewind and the auditor read the original bytes.
//
// THE FROZEN REGION IS THE ONLY REGION THIS PASS MAY REWRITE. The turn records
// its boundary before its first request. Every later request applies the same
// rewrite only before that boundary, while results appended during the turn
// remain byte-for-byte as the previous request sent them. Moving that boundary
// after each tool round would turn the last request's newest result into this
// request's compacted result and make the provider re-bill the whole suffix.
//
// THE SYSTEM PROMPT IS NEVER TOUCHED. Message 0 is the cache-friendly prefix
// F2 is already paying for; rewriting it would re-price the whole request.
//
// THE NEWEST FROZEN BATCH STAYS VERBATIM. Everything earlier is replaced by a
// reduced view of itself: the head that says what ran, the tail the checkpoint
// digest already proved keeps a verdict ([checkpointResultBytes]), the exact
// count of the bytes cut between them, and where the whole of it can be read
// back. Over the digest budget ([checkpointDigestBytes]) the oldest shrink
// further, to the one-line account stub.go already writes ([stubLine]).
//
// Nothing here reads a result and decides what it meant: an ai.Message carries
// no isError, so calling a result a success because its prose looked calm would
// invent the one fact the model most needs. Head, tail and counts are mechanical.
//
// Every reduction names a place the model can open, or says it cannot
// ([Agent.fullResultPointer]).
//
// A TOOL RESULT MAY BE SHORTENED AND MUST NOT BE DROPPED. The provider pairs
// every call with a result by id; deleting a result is a malformed request,
// not a saving.

import (
	"fmt"
	"strings"
	"sync"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

const (
	// compactHeadBytes is how much of a reduced result's opening is kept beside
	// its tail. The tail is the verdict; the head is what ran and where an error
	// lands. It is derived from the tail bound rather than written as a second
	// number, so the two cannot drift apart.
	compactHeadBytes = checkpointResultBytes / 2

	// compactViewBytes is the floor under which a result is left verbatim: the
	// most a reduced view can keep. Below it the view repeats most of the result
	// and charges a header for having done so.
	compactViewBytes = compactHeadBytes + checkpointResultBytes

	// compactReducedMarker opens every reduced view. A later pass recognizes one
	// by it, so a view is never reduced again and two requests of the same round
	// produce the same bytes.
	compactReducedMarker = "[reduced"

	// compactNoSource is what a reduction says instead of a pointer when this
	// session can name nowhere to read the original back. A path that is not
	// there costs the model a call and returns nothing.
	compactNoSource = "not retrievable"
)

// resultSource answers where the full bytes of one frozen tool result can be
// read back, and answers empty when nothing can be named. It is a function so
// the snapshot view stays a pure rewrite of messages while the session that owns
// the journal and the droppings decides what a pointer is.
type resultSource func(ai.Message) string

func (s resultSource) of(message ai.Message) string {
	if s == nil {
		return ""
	}
	return s(message)
}

// toolCompactMemo is this reduction, CARRIED BETWEEN REQUESTS.
//
// THE DEFECT IT CLOSES, and it is the one this file's own header should have
// predicted: the frozen region is by definition the part of the transcript that
// cannot change, and this pass rebuilt its reduced form from scratch on EVERY
// request of every tool round. For each old result that meant a SHA-256 over the
// whole of its text (the pointer's key, chatlog.go's chatRefKey), a copy of that
// text out of its content parts ([messageContentText] builds a string), a trim,
// a head, a tail and an allocated view — work proportional to the total bytes of
// everything the conversation has ever read, paid again and again while the
// conversation got longer. A two-hundred-message conversation with a few
// megabytes of tool output behind it paid it before every single request.
//
// SO THE REDUCED FORM IS HELD PER MESSAGE AND REBUILT ONLY FOR MESSAGES THAT ARE
// NEW OR HAVE MOVED. The guard is per index and it is cheap: a tool result's own
// call id and the length of its text. Anything that rewrites a frozen message in
// place — the end-of-turn stubbing pass (stub.go) is the one thing that does —
// changes one of the two and is recomputed; a message that is what it was is
// answered from here.
//
// AND THE HELD MESSAGE IS THE SAME VALUE EVERY TIME, which is worth more than
// the arithmetic: the provider's encode memo keeps one encoding per position and
// compares the message it holds against the one it is given (internal/provider's
// memo.go). A freshly composed view compares equal only after a byte-by-byte
// walk of its text; the held one compares equal on the string's own pointer.
type toolCompactMemo struct {
	mu sync.Mutex
	// place is the workspace the pointers in these views were resolved against.
	// A stub path is relative to its workspace, so an anchor move
	// ([Agent.AnchorWorkspace]) invalidates every view here at once.
	place string
	held  []compactedResult
}

// compactedResult is one frozen tool result as this pass leaves it: the view
// that replaces it, the one-line form the budget walk may fall back to, and the
// two facts that say whether the message at that index is still the one these
// were made from.
type compactedResult struct {
	made   bool
	callID string
	// wasBytes is [messageBytes] of the ORIGINAL message. It is the guard's
	// second fact and it is chosen because it is the one size that can be taken
	// without copying anything: messageBytes sums the lengths of the content
	// parts, where messageContentText builds a whole new string out of them.
	wasBytes int

	view      ai.Message
	viewBytes int
	line      ai.Message
	lineBytes int
}

// fresh says whether this entry was made from the message now at its index.
//
// THE TWO FACTS ARE THE CALL ID AND THE BYTE WEIGHT, and between them they cover
// every way this package rewrites a frozen result: a stub replaces the text,
// which moves the weight; a history rebuilt around a different call moves the
// id. The one thing they cannot tell apart is a result at the same index, under
// the same tool call id, rewritten to EXACTLY its old byte count — which nothing
// in this package does, because the rewrites it has all replace a result with a
// pointer that is shorter. It is written down rather than guarded against:
// guarding means digesting the whole result again, which is the cost this memo
// exists to remove.
func (c compactedResult) fresh(message ai.Message) bool {
	return c.made && c.callID == message.ToolCallID && c.wasBytes == messageBytes(message)
}

// begin brings the memo to this request's workspace and length, dropping
// everything when the workspace has moved under it.
func (m *toolCompactMemo) begin(place string, length int) {
	if m.place != place {
		m.place, m.held = place, nil
	}
	switch {
	case len(m.held) < length:
		m.held = append(m.held, make([]compactedResult, length-len(m.held))...)
	case len(m.held) > length:
		// A transcript that SHRANK is one a compaction rewrote wholesale, and
		// nothing held beyond its new end describes a message that still exists.
		m.held = m.held[:length]
	}
}

// compactToolHistory returns a shallow copy of messages whose consumed tool
// results before frozen have been compacted for the next turn-loop request.
// The input slice and the messages it still shares with the live transcript
// are not written.
func (a *Agent) compactToolHistory(messages []ai.Message, frozen int, source resultSource) []ai.Message {
	if frozen > len(messages) {
		frozen = len(messages)
	}
	if frozen <= 1 {
		return messages
	}
	newest := newestToolBatchStart(messages[:frozen])
	if newest < 0 {
		return messages
	}
	var old []int
	for index := 1; index < newest && index < len(messages); index++ {
		if messages[index].Role == "tool" {
			old = append(old, index)
		}
	}
	if len(old) == 0 {
		return messages
	}
	calls := toolResultCalls(messages[:newest])
	out := append([]ai.Message(nil), messages...)

	memo := &a.toolCompact
	memo.mu.Lock()
	defer memo.mu.Unlock()
	memo.begin(a.compactPlace(), len(messages))

	// spent is the running weight of the old results in out, carried rather than
	// recomputed. The budget walk below used to re-add every old result on every
	// iteration — quadratic in the call count, on the hot path of every request.
	spent := 0
	for _, index := range old {
		entry := &memo.held[index]
		if !entry.fresh(messages[index]) {
			*entry = makeCompactedResult(messages[index], toolResultName(calls[index]),
				source.of(messages[index]))
		}
		out[index] = entry.view
		spent += entry.viewBytes
	}
	// The digest budget is the ceiling for consumed evidence — the same 5k-token
	// account the checkpoint reader is held to. Newest-of-old keep their views;
	// the far end shrinks to a line. The newest batch is outside this sum, being
	// still the working evidence.
	//
	// One walk, oldest first: looping until the meter moved invited a restamp of
	// the same line forever, because a line made from a line is not always
	// shorter. The budget is a ceiling to walk towards rather than a promise —
	// several hundred calls weigh more than it even as single lines — so what
	// this owes is to have reduced everything it could.
	for _, index := range old {
		if spent <= checkpointDigestBytes {
			break
		}
		entry := &memo.held[index]
		if entry.lineBytes == 0 {
			continue
		}
		before, after := entry.viewBytes, entry.lineBytes
		if !compactViewEarnsItsRewrite(before, after) {
			continue
		}
		out[index] = entry.line
		spent -= before - after
	}
	return out
}

// makeCompactedResult is where the per-result work is DONE, exactly once per
// result per workspace. Everything expensive in this file happens here: the
// pointer (which digests the result's whole text, chatlog.go), the string built
// out of the content parts, the trim, the head and the tail.
//
// A RESULT THAT MUST BE LEFT ALONE IS STILL AN ENTRY. Its view is the message
// itself and its line is empty, so the walks above ask the memo rather than
// re-deciding — and a result already stubbed or already reduced is recognised
// once rather than on every request for the rest of the conversation.
func makeCompactedResult(message ai.Message, tool, source string) compactedResult {
	entry := compactedResult{
		made:      true,
		callID:    message.ToolCallID,
		wasBytes:  messageBytes(message),
		view:      message,
		viewBytes: messageBytes(message),
	}
	text := messageContentText(message)
	if compactLeaveVerbatim(text) {
		return entry
	}
	view := reducedResultView(tool, text, source)
	// A reduction that does not reclaim enough is not a reduction: it would
	// spend a rewrite, and the cold prefix behind it, to save almost nothing.
	// See [compactViewEarnsItsRewrite].
	if compactViewEarnsItsRewrite(len(text), len(view)) {
		entry.view = replaceToolText(message, view)
		entry.viewBytes = messageBytes(entry.view)
	}
	// The far end's one line is composed here too, beside the view, because the
	// budget walk below needs it only sometimes and needs it from the ORIGINAL
	// text every time — and composing it there meant rebuilding that text on
	// every request of every round for results the budget never reached.
	entry.line = replaceToolText(message, reducedOutcomeLine(tool, text, source))
	entry.lineBytes = messageBytes(entry.line)
	return entry
}

// compactPlace is the workspace the memo's pointers were resolved against, read
// under the lock an anchor moves it with ([Agent.resultPlaceNow] states the same
// rule for one request's own reading).
func (a *Agent) compactPlace() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return strings.TrimSpace(a.config.Workspace)
}

// resultPlace is where this session can put a result's bytes and where its
// journal is, taken as ONE READING. [Agent.AnchorWorkspace] rewrites the
// workspace under a.mu while the snapshot view runs without that lock, so the
// view reads this once at its request boundary and every pointer in that request
// answers from the same place.
type resultPlace struct {
	workspace string
	droppings Place
	journal   string
}

// resultPlaceNow is the reading for a caller that holds nothing;
// resultPlaceLocked is the same reading for the stub pass and the turn fold,
// which already hold a.mu and must not take it again.
func (a *Agent) resultPlaceNow() resultPlace {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.resultPlaceLocked()
}

func (a *Agent) resultPlaceLocked() resultPlace {
	return resultPlace{
		// The FAMILY'S folder, which is a worker's commissioning conversation and
		// not the repository it borrowed (landing.go).
		workspace: strings.TrimSpace(a.config.Workspace),
		droppings: a.config.droppingsPlace(),
		journal:   a.file.journalName(),
	}
}

// filedCap bounds the pointer memo at the most results one request could carry —
// a default window's bytes over the smallest result the stub pass will file. Past
// it the memo is dropped whole rather than evicted one at a time: a miss costs one
// write, and a long conversation must not grow a map for the life of the process.
const filedCap = defaultContextWindow * bytesPerToken / stubMinBytes

// fullResultPointer is the ONE answer to "where can the whole of that result be
// read back", shared by the snapshot view here, the end-of-turn stub pass
// (stub.go) and the current-turn fold (turnfold.go). One resolver, so a stub and
// a reduced view never point at two different kinds of thing.
//
// A STORE REF IS NOT A POINTER. `store:412` was the first answer all three used,
// and nothing on this belt fetches a store message by id: `search_conversations`
// searches words and clips every hit to one line (tools_conversations.go). So the
// pointer is a place the belt's own verbs open:
//
//  1. the bytes filed in this session's droppings ([writeStub]), which `read`
//     takes and pages through at any size;
//  2. the session journal with the call id to grep for, when there is nowhere to
//     file — a weaker pointer, since a journal line is JSON and `grep` clips a
//     long line, but a real one at a real path;
//  3. nothing, said as nothing.
//
// The file is written once per result and remembered, because this is asked on
// every request of every tool round and the answer cannot cost a write each time.
// The memo is keyed by the WORKSPACE as well as the result: a stub path is
// relative to the workspace it was filed in, so an answer kept across an anchor
// would name a file the model's own read tool now resolves somewhere else.
//
// A write that failed is remembered too, as the absence it is. Retrying per
// result per request would be an I/O spin on the request path with no policy
// behind it; the retry happens when the workspace changes or the memo is dropped.
//
// It takes no session lock, so a caller holding a.mu may ask it.
func (a *Agent) fullResultPointer(message ai.Message, place resultPlace) string {
	text := messageContentText(message)
	if strings.TrimSpace(text) == "" {
		return ""
	}
	key := place.workspace + "\x00" + chatRefKey(message)
	a.filedMu.Lock()
	pointer, known := a.filed[key]
	a.filedMu.Unlock()
	if !known && place.workspace != "" {
		pointer, _ = writeStub(place.droppings, place.workspace, text)
		a.filedMu.Lock()
		if a.filed == nil || len(a.filed) >= filedCap {
			a.filed = make(map[string]string, 32)
		}
		a.filed[key] = pointer
		a.filedMu.Unlock()
	}
	if pointer != "" {
		return pointer
	}
	if place.journal == "" {
		return ""
	}
	if id := strings.TrimSpace(message.ToolCallID); id != "" {
		return "grep " + id + " in " + place.journal
	}
	return place.journal
}

// toolResultName leaves an orphan result unnamed rather than borrowing another
// batch's tool name.
func toolResultName(call *ai.ToolCall) string {
	if call == nil {
		return ""
	}
	return call.Function.Name
}

// reducedResultView is what a consumed result is sent as: a header naming the
// tool, the result's true size and where the whole of it lives, then the head,
// then the exact count of what was cut, then the tail.
//
//	[reduced view: bash · 41208 bytes · full: logs/stubs/9c2f.txt]
//	go build ./...
//	…[40608 bytes elided]…
//	FAIL	./internal/session	0.412s
//
// The elided count is measured, not the size minus the two bounds: a rune
// boundary moves both cuts, and a count that is nearly right is one a model
// cannot decide from.
//
// THE JOB FOOTER IS NOT PART OF THE RESULT AND IS NOT CARRIED INTO THE VIEW.
// The tail is the half of a long result that is worth keeping — a command's
// verdict is at the end of it — and a footer appended after the fact
// (jobfooter.go) sits exactly there, so a compacted build log ended in the
// elapsed time of an unrelated background job instead of the line that said
// whether it passed. The state of the jobs is fresh on the result the model is
// reading NOW; a stale copy of it four turns back is not worth the tail it
// takes.
func reducedResultView(tool, text, source string) string {
	trimmed := strings.TrimSpace(stripJobFooter(text))
	head := compactHead(trimmed, compactHeadBytes)
	tail := compactTail(trimmed, checkpointResultBytes)
	elided := len(trimmed) - len(head) - len(tail)
	if elided <= 0 {
		return trimmed
	}
	return fmt.Sprintf("%s view: %s · %d bytes · full: %s]\n%s\n…[%d bytes elided]…\n%s",
		compactReducedMarker, compactToolName(tool), len(trimmed), compactSource(source),
		head, elided, tail)
}

// reducedOutcomeLine is the far end's one line, and it is stub.go's formatter:
// a model that has learned to read a stub has already learned to read this.
func reducedOutcomeLine(tool, text, source string) string {
	return stubLine(tool, text, compactSource(source))
}

// compactSource is the pointer clause, or the honest absence of one.
func compactSource(source string) string {
	if strings.TrimSpace(source) == "" {
		return compactNoSource
	}
	return source
}

func compactToolName(tool string) string {
	if strings.TrimSpace(tool) == "" {
		return "tool"
	}
	return tool
}

// compactHead and compactTail cut on a rune boundary, for [checkpointResultTail]'s
// reason: a string cut through a multi-byte character is not one anybody can read.
// The head walks its cut back, the tail forward.
func compactHead(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	cut := limit
	for cut > 0 && !utf8RuneStart(text[cut]) {
		cut--
	}
	return text[:cut]
}

func compactTail(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	cut := len(text) - limit
	for cut < len(text) && !utf8RuneStart(text[cut]) {
		cut++
	}
	return text[cut:]
}

// newestToolBatchStart is the index of the first tool result in the most
// recent complete batch — the results the model has just been handed and has
// not yet stepped past. Trailing user notes and assistant prose are skipped
// so a steer that landed after the batch does not hide it.
//
// Negative means there is no batch, and therefore nothing to compact: the
// first request of a turn, or a turn that has not called a tool.
func newestToolBatchStart(messages []ai.Message) int {
	end := len(messages)
	for end > 0 && messages[end-1].Role != "tool" {
		end--
	}
	if end == 0 {
		return -1
	}
	start := end
	for start > 0 && messages[start-1].Role == "tool" {
		start--
	}
	return start
}

// compactLeaveVerbatim is the cheap half of a result: already a stub, already a
// reduced view, or already no larger than one view's own bound. Replacing any of
// them would spend a rewrite to save nothing — and reducing a reduction would
// make the same round's second request differ from its first.
func compactLeaveVerbatim(text string) bool {
	trimmed := strings.TrimSpace(text)
	if strings.HasPrefix(trimmed, stubMarker) || strings.HasPrefix(trimmed, compactReducedMarker) {
		return true
	}
	return len(text) <= compactViewBytes
}

// compactViewEarnsItsRewrite weighs a composed view against the result it would
// replace, and is the second half of [compactLeaveVerbatim]'s question.
//
// A REWRITE IN THE MIDDLE OF THE TRANSCRIPT IS NOT FREE. [compactLeaveVerbatim]
// bounds the RESULT — under [compactViewBytes] nothing is touched at all — but
// what decides whether a rewrite PAYS is the reclaim, and the reclaim is the
// result minus the head, minus the tail, and minus the pointer. The pointer is
// the part nobody sized: a 934-byte result was replaced by an 843-byte view
// whose `full:` clause was a ninety-character absolute path into a temporary
// home. Ninety-one bytes saved; five thousand two hundred and sixty bytes of
// conversation behind it re-billed at the uncached rate on that request and
// every one after it until the next break. It was measured on this wave's own
// branch and is written up with its numbers in
// docs/design/prompt-diet/BENCH.md §1c.
//
// So a reclaim smaller than a [stubPrefixShare] share of what it disturbs is
// declined and the result is left exactly as it is. That is stub.go's law, and
// stub.go's constant is read from there rather than typed again. The two weigh
// the share against different quantities — the stubbing pass holds the whole
// transcript and measures the cold tail it is about to make, while this one is
// composing a view a message at a time and has only the message in hand — and
// what they share is the discipline: A SAVING SMALLER THAN A SHARE OF WHAT IT
// TOUCHES IS A SAVING NOT WORTH HAVING.
//
// A result declined here is not lost and is not decided forever. It is looked at
// again on the next request, and the pass that has to hold the digest budget
// still reduces it to a line when the transcript's weight actually demands one.
func compactViewEarnsItsRewrite(was, now int) bool {
	reclaim := was - now
	return reclaim > 0 && reclaim*stubPrefixShare >= was
}

func replaceToolText(message ai.Message, text string) ai.Message {
	return ai.Message{
		Role:       message.Role,
		ToolCallID: message.ToolCallID,
		Content:    []ai.ContentPart{{Type: "text", Text: text}},
	}
}

// toolResultBytes is the weight of a set of results, recomputed from scratch. The
// pass itself carries a running total instead; this is what the tests check that
// total against.
func toolResultBytes(messages []ai.Message, indices []int) int {
	total := 0
	for _, index := range indices {
		if index < 0 || index >= len(messages) {
			continue
		}
		total += messageBytes(messages[index])
	}
	return total
}
