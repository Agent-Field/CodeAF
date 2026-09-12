package session

// A long turn has a working set of its own.
//
// Cross-turn compaction protects the model's whole context window, but its
// trigger can be more than two hundred thousand tokens away. That is too late
// for a turn that makes a hundred small tool calls: every result is below the
// ordinary stub floor, yet every later call pays to read every earlier result.
// This pass bounds that accumulation without summarizing or deleting anything.

import (
	"encoding/json"
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
	// rounds, while stopping the measured 100k–150k request loop long before the
	// 256k trusted-window ceiling. Smaller windows use half their trusted size, so
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

// foldTurnOutputs replaces consumed read results from the CURRENT turn with
// pointers, oldest complete batch first, until the tool-output working set
// reaches its headroom target. An observation becomes consumed only after work
// made from it lands. Assistant text, calls and their arguments, mutating tool
// batches, and every observation not yet acted upon remain verbatim.
//
// One shape of repetition is folded WITHOUT filing a copy. A measured planning
// turn read one file in ten overlapping slices and carried all ten, verbatim,
// through twenty requests — yet a slice of a file is re-readable where it came
// from, which is the one kind of result whose bytes never needed a steward. So
// when the foldable region holds several reads of ONE path, the newest slice
// stays whole and each older slice becomes a pointer naming the file and the
// range it held (turnFoldReadRepeats); every other result keeps the filed
// pointer it always got.
func (a *Agent) foldTurnOutputs(seenThrough int, consumedReads map[*ai.ToolCall]bool, hub *eventHub) {
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
		batches := turnFoldBatches(a.messages, a.turnFloor, limit, consumedReads)
		keepNewest, _ := turnFoldReadRepeats(a.messages, batches)
		selected = turnFoldSelection(a.messages, batches, keepNewest, total, turnWorkingTarget(a.window())*bytesPerToken)
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
	batches := turnFoldBatches(a.messages, a.turnFloor, limit, consumedReads)
	keepNewest, superseded := turnFoldReadRepeats(a.messages, batches)
	// A pass that cannot buy the whole headroom does not run. Every rewrite
	// invalidates the provider cache from that point onward; repeatedly replacing
	// one tiny result while protected observations hold the working set above the
	// line is strictly worse than retaining the original context.
	selected = turnFoldSelection(a.messages, batches, keepNewest, total, target)
	if selected == 0 {
		a.mu.Unlock()
		return
	}
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
			// The newest slice of a file the turn read more than once stays
			// whole: it is the reading the model is most likely working from,
			// and the older slices' pointers already name how to bring any of
			// their bytes back. A batch with nothing left to replace is simply
			// skipped, the way a batch of already-stubbed results is.
			if keepNewest[index] {
				continue
			}
			message := a.messages[index]
			text := messageContentText(message)
			pointer := ""
			if slice, repeated := superseded[index]; repeated {
				// An older slice of a repeatedly read file needs no copy filed:
				// its bytes were never anywhere but the file itself, so the
				// pointer is the read that fetches that slice again — the same
				// kind of recipe the journal fallback already spells.
				pointer = slice.readRecipe()
			} else {
				// The same pointer the stub pass and the snapshot view give, and for
				// the same reason: a store ref is not one ([Agent.fullResultPointer]).
				pointer = a.fullResultPointer(message, place)
			}
			if pointer == "" {
				complete = false
				break
			}
			stub := ai.Message{
				Role:       message.Role,
				ToolCallID: message.ToolCallID,
				Content: []ai.ContentPart{{Type: "text", Text: stubLine(
					toolNameFor(a.messages, index), text, pointer)}},
			}
			prepared = append(prepared, turnFoldReplacement{index: index, message: stub})
			batchSaved += messageBytes(message) - messageBytes(stub)
		}
		if !complete {
			continue
		}
		replacements = append(replacements, prepared...)
		foldedBytes += batchSaved
		results += len(prepared)
	}

	if results == 0 || total-foldedBytes > target {
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

// turnFoldSelection reports how many oldest eligible batches are needed to
// reach the target, or zero when all eligible observations together cannot buy
// that headroom. It deliberately overestimates savings by the small pointer
// bodies; the materialization pass below checks the exact bytes before writing
// anything into the live transcript. A slice keepNewest holds whole buys
// nothing, and counting it would promise headroom the pass cannot deliver.
func turnFoldSelection(messages []ai.Message, batches []turnFoldBatch, keepNewest map[int]bool, total, target int) int {
	reclaimable := 0
	for batchIndex, batch := range batches {
		for _, index := range batch.indices {
			if keepNewest[index] {
				continue
			}
			reclaimable += messageBytes(messages[index])
		}
		if total-reclaimable <= target {
			return batchIndex + 1
		}
	}
	return 0
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
func turnFoldBatches(messages []ai.Message, start, limit int, consumedReads map[*ai.ToolCall]bool) []turnFoldBatch {
	var batches []turnFoldBatch
	for index := start; index < limit; index++ {
		if messages[index].Role != "assistant" || len(messages[index].ToolCalls) == 0 {
			continue
		}
		readBatch := true
		for callIndex := range messages[index].ToolCalls {
			call := &messages[index].ToolCalls[callIndex]
			if !earlyTools[call.Function.Name] || !consumedReads[call] {
				readBatch = false
				break
			}
		}
		if !readBatch {
			continue
		}
		end := index + 1
		for end < len(messages) && messages[end].Role == "tool" {
			end++
		}
		if end > limit || end == index+1 {
			continue
		}
		batch := turnFoldBatch{indices: make([]int, 0, end-index-1)}
		for cursor := index + 1; cursor < end; cursor++ {
			text := messageContentText(messages[cursor])
			if strings.HasPrefix(strings.TrimSpace(text), stubMarker) {
				continue
			}
			batch.indices = append(batch.indices, cursor)
		}
		if len(batch.indices) > 0 {
			batches = append(batches, batch)
		}
		index = end - 1
	}
	return batches
}

// readSlice is one read call's own recipe: the path it asked for and the slice
// of it, spelled in the read tool's own arguments. It is parsed from the
// call's arguments, never from the result's text — a result carries a
// continuation footer only when it was cut, while the arguments always say
// what was asked.
type readSlice struct {
	path   string
	offset int // 1-indexed, as the read tool takes it
	limit  int // 0 means the call named no limit
}

// readRecipe is the pointer an older slice folds into: the read that brings
// its bytes back. The pointer family already carries recipes and not just
// paths — the journal fallback is "grep <id> in <journal>" — and the read
// tool's own continuation footers teach the offset spelling, so this is the
// sentence family the prompt already teaches, adapted to name the range.
func (s readSlice) readRecipe() string {
	recipe := "read " + s.path
	if s.offset > 1 {
		recipe += fmt.Sprintf(" offset=%d", s.offset)
	}
	if s.limit > 0 {
		recipe += fmt.Sprintf(" limit=%d", s.limit)
	}
	return recipe
}

// turnFoldReadSlice answers which slice of which file the result at index
// holds: its call is found in the assistant message above it, the way
// toolNameFor finds a name, and only a genuine `read` call with a parseable
// path qualifies. Anything else — a grep, a find, an unreadable argument
// string — is not a slice and keeps the ordinary filed pointer.
func turnFoldReadSlice(messages []ai.Message, index int) (readSlice, bool) {
	id := messages[index].ToolCallID
	if id == "" {
		return readSlice{}, false
	}
	for above := index - 1; above > 0; above-- {
		for _, call := range messages[above].ToolCalls {
			if call.ID != id {
				continue
			}
			if call.Function.Name != "read" {
				return readSlice{}, false
			}
			var args struct {
				Path   string `json:"path"`
				Offset *int   `json:"offset"`
				Limit  *int   `json:"limit"`
			}
			if err := json.Unmarshal([]byte(call.Function.Arguments), &args); err != nil || strings.TrimSpace(args.Path) == "" {
				return readSlice{}, false
			}
			slice := readSlice{path: args.Path, offset: 1}
			if args.Offset != nil && *args.Offset > 1 {
				slice.offset = *args.Offset
			}
			if args.Limit != nil && *args.Limit > 0 {
				slice.limit = *args.Limit
			}
			return slice, true
		}
	}
	return readSlice{}, false
}

// turnFoldReadRepeats walks the foldable batches and decides, for every file
// read MORE THAN ONCE inside the region, which read is the newest slice — the
// one kept whole — and which are the older slices whose bytes the file itself
// hands back. The batches arrive in transcript order, so the last index of a
// path is its newest. A path is grouped by the exact string the calls named:
// a second spelling of the same file is a different recipe, and canonicalizing
// it is a filesystem question this pass may not ask under the session lock.
//
// FRESHNESS IS NOT THIS PASS'S QUESTION. Whether the newest slice of a file
// still says what it said — a later edit or a watch may have moved the file —
// belongs to the held-read seam at the live edge (heldreads.go), which owns
// the short-circuit of a read the world has outrun. This pass is transcript
// hygiene: the pointer it writes only ever says the range WAS read and how to
// read it back, so a stale file cannot turn it into a coverage claim, and a
// slice kept whole is the observation the model received, nothing more.
func turnFoldReadRepeats(messages []ai.Message, batches []turnFoldBatch) (keepNewest map[int]bool, superseded map[int]readSlice) {
	byPath := make(map[string][]int)
	slices := make(map[int]readSlice)
	for _, batch := range batches {
		for _, index := range batch.indices {
			slice, ok := turnFoldReadSlice(messages, index)
			if !ok {
				continue
			}
			slices[index] = slice
			byPath[slice.path] = append(byPath[slice.path], index)
		}
	}
	for _, indices := range byPath {
		if len(indices) < 2 {
			continue
		}
		if keepNewest == nil {
			keepNewest = make(map[int]bool)
			superseded = make(map[int]readSlice)
		}
		keepNewest[indices[len(indices)-1]] = true
		for _, index := range indices[:len(indices)-1] {
			superseded[index] = slices[index]
		}
	}
	return keepNewest, superseded
}

func turnFoldMarker(results, tokens int) string {
	return fmt.Sprintf("%s%d result%s · %s tokens]", foldMarkerPrefix, results, plural(results), approxTokens(tokens))
}
