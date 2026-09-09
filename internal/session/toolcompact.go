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

// compactToolHistory returns a shallow copy of messages whose consumed tool
// results before frozen have been compacted for the next turn-loop request.
// The input slice and the messages it still shares with the live transcript
// are not written.
func compactToolHistory(messages []ai.Message, frozen int, source resultSource) []ai.Message {
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
	// The call each result answers, found once for the whole pass: [toolNameFor]
	// walks back up the transcript per result, which is the same quadratic shape
	// the running total below removes.
	names := toolCallNames(messages[:newest])
	out := append([]ai.Message(nil), messages...)

	// spent is the running weight of the old results in out, carried rather than
	// recomputed. The budget walk below used to re-add every old result on every
	// iteration — quadratic in the call count, on the hot path of every request.
	spent := 0
	for _, index := range old {
		text := messageContentText(messages[index])
		if !compactLeaveVerbatim(text) {
			view := reducedResultView(names[messages[index].ToolCallID], text, source.of(messages[index]))
			// A reduction that is not smaller is not a reduction: it would spend
			// a rewrite, and the cold prefix behind it, to save nothing.
			if len(view) < len(text) {
				out[index] = replaceToolText(messages[index], view)
			}
		}
		spent += messageBytes(out[index])
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
		original := messageContentText(messages[index])
		if compactLeaveVerbatim(original) {
			continue
		}
		line := reducedOutcomeLine(names[messages[index].ToolCallID], original, source.of(messages[index]))
		reduced := replaceToolText(out[index], line)
		before, after := messageBytes(out[index]), messageBytes(reduced)
		if after >= before {
			continue
		}
		out[index] = reduced
		spent -= before - after
	}
	return out
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

// toolCallNames maps a call id to the tool that was asked for, over one walk of
// the frozen region.
func toolCallNames(messages []ai.Message) map[string]string {
	names := make(map[string]string, 16)
	for _, message := range messages {
		for _, call := range message.ToolCalls {
			if call.ID != "" {
				names[call.ID] = call.Function.Name
			}
		}
	}
	return names
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
func reducedResultView(tool, text, source string) string {
	trimmed := strings.TrimSpace(text)
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
