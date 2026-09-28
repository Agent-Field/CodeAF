//go:build !windows

package compaction

import (
	"strconv"
	"strings"

	"github.com/Agent-Field/codeaf/internal/seniordev/engine/msgmodel"
)

// The verbatim tail a compaction keeps ahead of the summary.
//
// The newest messages are kept verbatim at every boundary and only what lies
// before them is summarized. Old tool outputs INSIDE the tail are truncated in
// the store, so a tail always fits: measured on untruncated multi-kilobyte
// command outputs, one assistant message could cost more than the whole budget
// and no split point would ever be found.
//
// Reasoning is cut too, in every kept message including the newest. The
// same-model projection re-sends every reasoning part with its provider
// metadata, and a single message can carry far more reasoning than tool
// traffic. Reasoning is the model's scratch, not state; signed reasoning
// (Anthropic) is left alone because the provider validates it byte for byte.
const (
	// DefaultPreserveRecentFraction sizes the token budget for the truncated
	// older messages of the tail as a fraction of the high watermark, so the
	// tail grows with the budget. The newest message is kept on top of it.
	DefaultPreserveRecentFraction = 0.2
	// TailToolOutputMaxChars caps each completed tool output of an OLDER tail
	// message. Head/tail truncation keeps the start and, mostly, the end, so
	// an error at the bottom of a test run survives.
	TailToolOutputMaxChars = 4_000
	// TailReasoningMaxChars caps each unsigned reasoning part of EVERY tail
	// message to its head.
	TailReasoningMaxChars = 1_000
)

// tailSelection is the outcome of selectTail: where the tail starts, what it
// costs, and the parts that must be rewritten in the store so the kept
// messages really are the size they were measured at.
type tailSelection struct {
	// StartID names the first kept message; nil only when there are no
	// messages at all. When everything fits it is the first message, and the
	// head is empty.
	StartID *string
	// Head is everything before the tail: the messages to summarize.
	Head []msgmodel.WithParts
	// Messages counts kept messages and Tokens is their estimated size after
	// truncation. Truncated lists the rewritten parts; TruncatedOutputs and
	// TruncatedReasoning count them by kind.
	Messages           int
	Tokens             float64
	Truncated          []msgmodel.Part
	TruncatedOutputs   int
	TruncatedReasoning int
}

// selectTail walks backwards from the newest message. The newest message is
// always kept: it is the observation the model has not yet acted on (the
// trigger fires when the turn that produced it finishes), so its tool outputs
// stay whole and only its reasoning is cut. Older messages are kept, oldest
// cut first, while their truncated size fits `budget`.
func selectTail(
	messages []msgmodel.WithParts,
	budget float64,
	model Model,
	estimate EstimateFunc,
	maxToolChars float64,
) (tailSelection, error) {
	if len(messages) == 0 {
		return tailSelection{}, nil
	}
	newest := len(messages) - 1
	newestCopy, cuts := truncateForTail(messages[newest], 0, TailReasoningMaxChars)
	total, err := estimate([]msgmodel.WithParts{newestCopy}, model)
	if err != nil {
		return tailSelection{}, err
	}
	selection := tailSelection{Messages: 1, Tokens: total}
	selection.absorb(cuts)
	start := newest
	older := float64(0)
	var olderCuts []msgmodel.Part
	for i := newest - 1; i >= 0; i-- {
		copy, cuts := truncateForTail(messages[i], maxToolChars, TailReasoningMaxChars)
		size, err := estimate([]msgmodel.WithParts{copy}, model)
		if err != nil {
			return tailSelection{}, err
		}
		if older+size > budget {
			break
		}
		older += size
		start = i
		olderCuts = append(cuts, olderCuts...)
	}
	selection.absorb(olderCuts)
	// Everything fitting (start == 0) means there is no head to summarize,
	// but the tail must STILL be named: FilterCompacted keeps only what a
	// compaction's tail_start_id points at, and a boundary without one keeps
	// nothing before it: a "no-head" boundary would otherwise project only the
	// summary and lose the tail it meant to keep.
	id := messages[start].Info.MessageID()
	selection.StartID = &id
	selection.Head = messages[:start]
	selection.Messages = len(messages) - start
	selection.Tokens = total + older
	return selection, nil
}

func (selection *tailSelection) absorb(parts []msgmodel.Part) {
	for _, part := range parts {
		switch part.(type) {
		case msgmodel.ToolPart:
			selection.TruncatedOutputs++
		case msgmodel.ReasoningPart:
			selection.TruncatedReasoning++
		}
	}
	selection.Truncated = append(selection.Truncated, parts...)
}

// truncateForTail returns a copy of the message whose completed tool outputs
// longer than maxToolChars (0 = leave them) and whose unsigned reasoning parts
// longer than maxReasoningChars are cut, plus the rewritten parts. The copy is
// what the tail will cost; the parts are what the store must be told.
func truncateForTail(
	message msgmodel.WithParts, maxToolChars, maxReasoningChars float64,
) (msgmodel.WithParts, []msgmodel.Part) {
	out := message
	out.Parts = append(msgmodel.Parts(nil), message.Parts...)
	var cuts []msgmodel.Part
	for index, raw := range out.Parts {
		switch part := raw.(type) {
		case msgmodel.ToolPart:
			completed, ok := part.State.(msgmodel.ToolStateCompleted)
			if !ok || maxToolChars <= 0 || float64(charCount(completed.Output)) <= maxToolChars {
				continue
			}
			completed.Output = truncateTailOutput(completed.Output, maxToolChars)
			part.State = completed
			out.Parts[index] = part
			cuts = append(cuts, part)
		case msgmodel.ReasoningPart:
			if maxReasoningChars <= 0 || signedReasoning(part) ||
				float64(charCount(part.Text)) <= maxReasoningChars {
				continue
			}
			part.Text = truncateTailReasoning(part.Text, maxReasoningChars)
			out.Parts[index] = part
			cuts = append(cuts, part)
		}
	}
	return out, cuts
}

// signedReasoning reports provider-signed reasoning (Anthropic's signature
// field), which must reach the provider unchanged.
func signedReasoning(part msgmodel.ReasoningPart) bool {
	anthropic, ok := part.Metadata.Field("anthropic")
	if !ok {
		return false
	}
	signature, ok := msgmodel.RawObject(anthropic).Field("signature")
	return ok && strings.TrimSpace(string(signature)) != "null" && len(signature) > 0
}

// truncateTailOutput keeps a quarter of the cap from the start and the rest
// from the end, and says so in words the model can act on.
func truncateTailOutput(text string, maxChars float64) string {
	length := float64(charCount(text))
	if maxChars <= 0 || length <= maxChars {
		return text
	}
	headChars := int(maxChars / 4)
	tailChars := int(maxChars) - headChars
	head := sliceChars(text, 0, headChars)
	tail := sliceChars(text, charCount(text)-tailChars, charCount(text))
	omitted := length - float64(headChars) - float64(tailChars)
	return strings.Join([]string{
		head,
		"[Tool output truncated at a context compaction: omitted " +
			strconv.FormatFloat(omitted, 'f', -1, 64) + " chars. Re-run the command if you need the full output.]",
		tail,
	}, "\n")
}

// truncateTailReasoning keeps the head of a reasoning part.
func truncateTailReasoning(text string, maxChars float64) string {
	length := float64(charCount(text))
	if maxChars <= 0 || length <= maxChars {
		return text
	}
	head := sliceChars(text, 0, int(maxChars))
	return head + "\n[Reasoning truncated at a context compaction: omitted " +
		strconv.FormatFloat(length-maxChars, 'f', -1, 64) + " chars.]"
}
