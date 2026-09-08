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
// consumed by a later call and is replaced by the same tail the checkpoint
// digest already proved is enough to keep a verdict ([checkpointResultTail],
// [checkpointResultBytes]). When those tails together would exceed the digest
// budget ([checkpointDigestBytes]), older ones shrink to a one-line account
// ([stubOutcome]) so prompt growth stays bounded in the old results even as the
// call count climbs.
//
// A TOOL RESULT MAY BE SHORTENED AND MUST NOT BE DROPPED. The provider pairs
// every call with a result by id; deleting a result is a malformed request,
// not a saving.

import (
	"strings"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// compactToolHistory returns a shallow copy of messages whose consumed tool
// results before frozen have been compacted for the next turn-loop request.
// The input slice and the messages it still shares with the live transcript
// are not written.
func compactToolHistory(messages []ai.Message, frozen int) []ai.Message {
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
	out := append([]ai.Message(nil), messages...)
	for _, index := range old {
		text := messageContentText(out[index])
		if compactLeaveVerbatim(text) {
			continue
		}
		out[index] = replaceToolText(out[index], checkpointResultTail(text))
	}
	// THE DIGEST BUDGET IS THE CEILING FOR CONSUMED EVIDENCE, the same 5k-token
	// account the checkpoint reader already proved replaces a 57–91k raw read.
	// Newest-of-old keep their tails; the far end shrinks to a line. The newest
	// batch is outside this sum on purpose — it is still the working evidence.
	//
	// ONE WALK, OLDEST FIRST. Replacing a tail with its one-line account is
	// enough to drop the pile under the budget on a long turn; looping until
	// the meter moved invited a restamp of the same line forever, because a
	// stubOutcome of a stubOutcome is not always shorter than the line it
	// started from.
	for _, index := range old {
		if toolResultBytes(out, old) <= checkpointDigestBytes {
			break
		}
		original := messageContentText(messages[index])
		if compactLeaveVerbatim(original) {
			continue
		}
		line := stubOutcome(original)
		if len(messageContentText(out[index])) <= len(line) {
			continue
		}
		out[index] = replaceToolText(out[index], line)
	}
	return out
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

// compactLeaveVerbatim is the cheap half of a result: already a stub, or
// already no larger than the digest's one-result bound. Replacing either
// would spend a rewrite to save nothing.
func compactLeaveVerbatim(text string) bool {
	if strings.HasPrefix(strings.TrimSpace(text), stubMarker) {
		return true
	}
	return len(text) <= checkpointResultBytes
}

func replaceToolText(message ai.Message, text string) ai.Message {
	return ai.Message{
		Role:       message.Role,
		ToolCallID: message.ToolCallID,
		Content:    []ai.ContentPart{{Type: "text", Text: text}},
	}
}

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
