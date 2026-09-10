package session

// A long turn has a working set of its own.
//
// Cross-turn compaction protects the model's whole context window, but its
// trigger can be more than two hundred thousand tokens away. That is too late
// for a turn that makes a hundred small tool calls: every result is below the
// ordinary stub floor, yet every later call pays to read every earlier result.
// This pass bounds that accumulation without summarizing or deleting anything.

import (
	"fmt"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

const (
	// turnWorkingSetTokens is the largest tool-working transcript one turn is
	// allowed to carry before old results become pointers. Sixty-four thousand
	// tokens holds the recent 20k-token work window plus several substantial tool
	// rounds, while allowing reductions before the
	// model's full context window is exhausted. Smaller windows use half their trusted size, so
	// this bound never claims most of a model's context for tool history alone.
	turnWorkingSetTokens = 64_000
)

// turnWorkingSet is the trigger line for one model window.
func turnWorkingSet(window int) int {
	trusted := TrustedWindow(window)
	if trusted <= 0 {
		return 0
	}
	line := turnWorkingSetTokens
	if half := trusted / 2; half < line {
		line = half
	}
	return line
}

// turnWorkingTarget is deliberately below the trigger. EVERY PASS INVALIDATES
// THE PROVIDER CACHE FROM ITS FIRST REWRITE, so stopping just under the line
// would buy one round and repay the cold prefix on the next. The midpoint
// between the protected recent tail and the trigger buys a substantial run of
// growth while deriving the target from the two existing laws rather than
// introducing another number that can drift.
func turnWorkingTarget(window int) int {
	line := turnWorkingSet(window)
	if line <= 0 {
		return 0
	}
	keep := keepRecent(window)
	return keep + (line-keep)/2
}

type turnFoldBatch struct {
	indices []int
}

type turnFoldReplacement struct {
	index   int
	message ai.Message
}

// foldTurnOutputs reduces old observations from the CURRENT turn, oldest
// complete batch first, towards its headroom target. A result must have reached
// a model request and left the protected recent window. Research and shell
// observations follow the same rule as any other tool; no later file change is
// needed. Assistant text, calls, arguments and unseen results remain verbatim.
func (a *Agent) foldTurnOutputs(seenThrough int, hub *eventHub) {
	line := turnWorkingSet(a.window())
	if line <= 0 {
		return
	}

	// IS THERE A PASS AT ALL, ASKED BEFORE THE PASS SAYS ANYTHING. This runs at
	// every step boundary of every turn and almost always answers no, and the
	// answer wants the agent's lock while the SAYING must not have it — a phase
	// post reaches a surface, and a surface that answers it by asking this agent
	// a question would be waiting on the very lock the pass holds
	// (phasenews.go). So the cheap reading is taken and let go of first.
	a.mu.Lock()
	total := turnToolBytes(a.messages, a.turnFloor)
	selected := 0
	if total > line*bytesPerToken {
		limit := a.turnFoldLimitLocked(seenThrough)
		batches := turnFoldBatches(a.messages, a.turnFloor, limit)
		selected = len(batches)
	}
	a.mu.Unlock()
	if total <= line*bytesPerToken || selected == 0 {
		return
	}
	// A fold is the same kind of wait as a cross-turn compaction and wears the
	// same word: the turn has stopped to tidy what it has already read.
	a.tellPhase(provider.PhaseTidying, "this turn's results", time.Now())
	defer a.endPhase()

	a.mu.Lock()
	before := a.estimateTokensLocked()
	total = turnToolBytes(a.messages, a.turnFloor)
	if total <= line*bytesPerToken {
		a.mu.Unlock()
		return
	}
	if seenThrough > len(a.messages) {
		seenThrough = len(a.messages)
	}
	limit := a.turnFoldLimitLocked(seenThrough)
	if limit <= a.turnFloor {
		a.mu.Unlock()
		return
	}

	earlier := shapeEntries(a.messages, a.file)
	place := a.resultPlaceLocked()
	target := turnWorkingTarget(a.window()) * bytesPerToken
	batches := turnFoldBatches(a.messages, a.turnFloor, limit)
	// The target buys headroom, but a protected remainder must not veto useful
	// savings. The exact cold-suffix share below rejects trivial rewrites even
	// when there are eligible observations.
	calls := toolResultCalls(a.messages[:limit])
	replacements := make([]turnFoldReplacement, 0, 16)
	foldedBytes := 0
	results := 0

	for _, batch := range batches {
		if total-foldedBytes <= target {
			break
		}
		prepared := make([]turnFoldReplacement, 0, len(batch.indices))
		batchSaved := 0
		complete := true
		for _, index := range batch.indices {
			message := a.messages[index]
			text := messageContentText(message)
			// The same pointer the stub pass and the snapshot view give, and for
			// the same reason: a store ref is not one ([Agent.fullResultPointer]).
			pointer := a.fullResultPointer(message, place)
			if pointer == "" {
				complete = false
				break
			}
			view := reducedResultView(toolResultName(calls[index]), text, pointer)
			stub := replaceToolText(message, view)
			saved := messageBytes(message) - messageBytes(stub)
			if saved <= 0 {
				continue
			}
			prepared = append(prepared, turnFoldReplacement{index: index, message: stub})
			batchSaved += saved
		}
		if !complete {
			continue
		}
		replacements = append(replacements, prepared...)
		foldedBytes += batchSaved
		results += len(prepared)
	}

	if results == 0 {
		a.mu.Unlock()
		return
	}
	// REWRITING A PREFIX MAKES ITS WHOLE SUFFIX COLD. Reuse the ordinary
	// stubbing pass's worth test with actual savings, including view headers and
	// paths. A large partial reclamation can pay; a tiny rewrite every round
	// cannot. The model's next request is byte-stable when this test declines.
	cold := transcriptBytes(a.messages[replacements[0].index:])
	if foldedBytes*stubPrefixShare < cold {
		a.mu.Unlock()
		return
	}
	// THE PASS IS ANNOUNCED HERE AND NOWHERE EARLIER, because here is the first
	// line at which it is certain to happen: everything above this returns
	// without touching the transcript. [EventCompacting] opens a row a surface
	// settles on the [EventCompacted] at the foot of this function, and a row
	// opened for a pass that then did nothing would never close. It goes out
	// under the lock for [Agent.compact]'s reason — [eventHub.send] appends to
	// queues and cannot block.
	hub.send(Event{Kind: EventCompacting, Hint: "compacting " + approxTokens(before) + " tokens"})
	for _, replacement := range replacements {
		a.messages[replacement.index] = replacement.message
	}
	savedTokens := foldedBytes / bytesPerToken
	marker := turnFoldMarker(results, savedTokens)
	note := textMessage("user", marker)
	a.messages = append(a.messages, note)
	a.contextTokens = 0

	// The original transcript becomes the scroll-back region, and the rebuilt
	// window is journaled behind a compaction marker exactly as the cross-turn
	// pass does. The original result lines remain above it byte-for-byte; replay
	// therefore returns the same stubs and note the live model now carries.
	a.earlier = earlier
	a.earlierFloor = countEntries(a.messages)
	pass := compactionPass{stubbed: results, stored: a.chatlog != nil}
	if a.file != nil {
		window := make([]ai.Message, len(a.messages)-1)
		copy(window, a.messages[1:])
		a.file.appendCompaction(pass, before, window)
	}
	a.mu.Unlock()

	if hub != nil {
		hint := strings.TrimSuffix(strings.TrimPrefix(marker, "["), "]")
		hub.send(Event{Kind: EventCompacted, Hint: hint})
	}
}

func (a *Agent) turnFoldLimitLocked(seenThrough int) int {
	limit := a.cutPointLocked()
	if seenThrough < limit {
		limit = seenThrough
	}
	if limit < a.turnFloor {
		return a.turnFloor
	}
	return limit
}

// turnToolBytes is the current turn's actual tool-observation working set. The
// provider's prompt count also includes the system prompt, tool schemas,
// assistant prose and call arguments; using that number as this pass's trigger
// caused cache-breaking folds that reclaimed only a few dozen tokens.
func turnToolBytes(messages []ai.Message, start int) int {
	if start < 0 {
		start = 0
	}
	if start > len(messages) {
		start = len(messages)
	}
	total := 0
	for _, message := range messages[start:] {
		if message.Role == "tool" {
			total += messageBytes(message)
		}
	}
	return total
}

func transcriptBytes(messages []ai.Message) int {
	total := 0
	for _, message := range messages {
		total += messageBytes(message)
	}
	return total
}

// turnFoldBatches returns complete tool-result batches in transcript order. A
// partial batch at the horizon is left whole, because rewriting one sibling and
// not another would make one model decision carry two different histories of
// the observation it received.
func turnFoldBatches(messages []ai.Message, start, limit int) []turnFoldBatch {
	var batches []turnFoldBatch
	calls := toolResultCalls(messages[:limit])
	for index := start; index < limit; index++ {
		if messages[index].Role != "assistant" || len(messages[index].ToolCalls) == 0 {
			continue
		}
		end := index + 1
		for end < len(messages) && messages[end].Role == "tool" {
			end++
		}
		if end > limit || end-index-1 != len(messages[index].ToolCalls) {
			continue
		}
		batch := turnFoldBatch{indices: make([]int, 0, end-index-1)}
		complete := true
		for cursor := index + 1; cursor < end; cursor++ {
			if calls[cursor] == nil {
				complete = false
				break
			}
			text := messageContentText(messages[cursor])
			if compactLeaveVerbatim(text) {
				continue
			}
			batch.indices = append(batch.indices, cursor)
		}
		if complete && len(batch.indices) > 0 {
			batches = append(batches, batch)
		}
		index = end - 1
	}
	return batches
}

func turnFoldMarker(results, tokens int) string {
	return fmt.Sprintf("%s%d result%s · %s tokens]", foldMarkerPrefix, results, plural(results), approxTokens(tokens))
}
