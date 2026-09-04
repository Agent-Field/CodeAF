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
// THE NEWEST FROZEN BATCH STAYS VERBATIM. Everything earlier has already been
// consumed by a later call and is replaced by a REDUCED VIEW of itself: the head
// that says what ran, the tail the checkpoint digest already proved is enough to
// keep a verdict ([checkpointResultTail], [checkpointResultBytes]), the exact
// count of the bytes cut from between them, and where the whole of it can be
// read back. When those views together would exceed the digest budget
// ([checkpointDigestBytes]), older ones shrink to the one-line account stub.go
// already writes ([stubLine]) so prompt growth stays bounded in the old results
// even as the call count climbs.
//
// NOTHING HERE READS A RESULT AND DECIDES WHAT IT MEANT. An ai.Message carries
// no isError, so a reduction that called a result a success because its prose
// looked calm would be inventing the one fact the model most needs. Head, tail
// and byte counts are mechanical; the model reads them and judges for itself.
//
// A REDUCTION SAYS WHERE THE BYTES ARE, or says it cannot. The pointer comes
// from the caller's own resolver ([Agent.frozenResultSource]) and is resolved
// from state already in memory — the store's ref map, the open journal's name —
// because this runs on EVERY request of every tool round and a per-request
// journal read would cost more than the pass saves.
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
	// compactHeadBytes is how much of a reduced result's OPENING is kept beside
	// its tail. The tail is a verdict ([checkpointResultBytes]); the head is what
	// was run and, for the tools that fail loudest, the error itself — a compiler
	// banner, `no such file`, a stack's first frame. Half the tail's bound is a
	// couple of lines of it, and the pair still replaces kilobytes with hundreds
	// of bytes. It is derived from the tail bound rather than written as a second
	// number, so the two cannot drift apart.
	compactHeadBytes = checkpointResultBytes / 2

	// compactViewBytes is the floor under which a result is left verbatim: the
	// most a reduced view can keep. Below it the view would repeat most of the
	// result and then charge a header for having done so.
	compactViewBytes = compactHeadBytes + checkpointResultBytes

	// compactReducedMarker opens every reduced view and is how a later pass
	// recognizes one, so a view is never reduced a second time and two requests
	// of the same round produce the same bytes.
	compactReducedMarker = "[reduced"

	// compactNoSource is what a reduction says instead of a pointer when this
	// session can name nowhere to read the original back: no store thread, no
	// journal. A path that is not there is worse than an honest absence — the
	// model spends a call on it and learns nothing.
	compactNoSource = "not retrievable"
)

// resultSource answers where the full bytes of one frozen tool result can be
// read back, and answers empty when nothing can be named. It is a function so
// the snapshot view stays a pure rewrite of messages while the session that owns
// the store and the journal decides what a pointer is.
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
	// The call each result answers, found ONCE for the whole pass. Asking
	// [toolNameFor] per result walks back up the transcript per result, which is
	// the same quadratic shape the running total below removes.
	names := toolCallNames(messages[:newest])
	out := append([]ai.Message(nil), messages...)

	// spent is the running weight of the old results in out, carried rather than
	// recomputed. The budget walk below used to re-add every old result on every
	// iteration — quadratic in the call count, on the hot path of every request
	// of every tool round of every turn.
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
	// THE DIGEST BUDGET IS THE CEILING FOR CONSUMED EVIDENCE, the same 5k-token
	// account the checkpoint reader already proved replaces a 57–91k raw read.
	// Newest-of-old keep their views; the far end shrinks to a line. The newest
	// batch is outside this sum on purpose — it is still the working evidence.
	//
	// ONE WALK, OLDEST FIRST. Replacing a view with its one-line account is
	// enough to drop the pile under the budget on a long turn; looping until
	// the meter moved invited a restamp of the same line forever, because a
	// line made from a line is not always shorter than the line it started from.
	//
	// The budget is a CEILING TO WALK TOWARDS, not a promise: several hundred
	// calls weigh more than it even when every one of them is a single line, and
	// what this owes that turn is to have reduced everything it could.
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

// frozenResultSource is this session's answer to "where can that result be read
// back", built once per request and asked once per reduced result.
//
// THE STORE'S REF IS FIRST and the journal is second, which is stub.go's order
// and for stub.go's reasons: a store ref survives a workspace being deleted, and
// a session with no store is still writing a journal that holds every result
// whole. The journal is named with the call id beside it because the id is the
// token that finds the one line among thousands, and because a bare path is a
// pointer at a file rather than at a result.
//
// BOTH ANSWERS COME FROM MEMORY. The ref map is already in hand and the path is
// the open file's own name; neither reads the journal, which this may not do —
// it is asked on every request of every tool round.
func (a *Agent) frozenResultSource() resultSource {
	journal := a.file.path()
	return func(message ai.Message) string {
		if ref := a.chatlog.ref(message); ref != "" {
			return ref
		}
		if journal == "" {
			return ""
		}
		if id := strings.TrimSpace(message.ToolCallID); id != "" {
			return "grep " + id + " in " + journal
		}
		return journal
	}
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
//	[reduced view: bash · 41208 bytes · full: store:412]
//	go build ./...
//	…[40608 bytes elided]…
//	FAIL	./internal/session	0.412s
//
// THE ELIDED COUNT IS COUNTED, not the size minus the bounds: a rune boundary
// moves both cuts by a byte or two, and a number that is nearly right about how
// much is missing is a number a model cannot use to decide whether to fetch it.
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

// reducedOutcomeLine is the far end's one line, and it is stub.go's line
// verbatim — same shape, same fields, one formatter — because a model that has
// learned to read a stub has already learned to read this.
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
// reason: a string cut through a multi-byte character is not a string anybody can
// read. The head walks its cut BACK so it never keeps half a rune; the tail walks
// forward so it never starts on one.
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
