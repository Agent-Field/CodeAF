// Package compaction ports the deterministic core of
// src/session/compaction.ts:35-363 and 418-526.
package compaction

import (
	"encoding/json"
	"math"
	"regexp"
	"strings"
	"unicode/utf16"

	"github.com/Agent-Field/swe-pro-go/internal/engine/msgmodel"
	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
	"github.com/Agent-Field/swe-pro-go/internal/session/ledgers"
	"github.com/Agent-Field/swe-pro-go/internal/session/overflow"
)

const (
	PruneMinimum               = 20_000
	PruneProtect               = 40_000
	ToolOutputMaxChars         = 2_000
	DefaultTailTurns           = 2
	MinPreserveRecentTokens    = 2_000
	MaxPreserveRecentTokens    = 8_000
	EvidenceToolOutputMaxChars = 2_000
	EventCompacted             = "session.compacted"
)

var PruneProtectedTools = []string{"skill"}

const SummaryTemplate = `Output exactly the Markdown structure shown inside <template> and keep the section order unchanged. Do not include the <template> tags in your response.
<template>
## Goal
- [the user's original request copied VERBATIM when it fits in a few sentences; only condense to a faithful summary when the request is genuinely too long to keep]

## Acceptance criteria (verbatim)
- [exact acceptance / done-criteria text copied verbatim from the transcript, or "(none stated)"]

## Active delegations
- [each live subagent session_id or background task id: its plandb task and current status, copied verbatim from the transcript, or "(none)"]

## Constraints & Preferences
- [user constraints, preferences, specs, or "(none)"]

## Progress
### Done
- [completed work or "(none)"]

### In Progress
- [current work or "(none)"]

### Blocked
- [blockers or "(none)"]

## Key Decisions
- [decision and why, or "(none)"]

## Next Steps
- [ordered next actions or "(none)"]

## Critical Context
- [important technical facts, errors, open questions, or "(none)"]

## Relevant Files
- [file or directory path: why it matters, or "(none)"]
</template>

Rules:
- Keep every section, even when empty.
- Use terse bullets, not prose paragraphs.
- Preserve exact file paths, commands, error strings, and identifiers when known.
- Copy acceptance criteria and active-delegation identifiers (session_ids, background task ids, plandb task ids) VERBATIM — never paraphrase an id or a criterion.
- Do not mention the summary process or that context was compacted.`

type Turn struct {
	Start int    `json:"start"`
	End   int    `json:"end"`
	ID    string `json:"id"`
}

type Tail struct {
	Start int    `json:"start"`
	ID    string `json:"id"`
}

type CompletedCompaction struct {
	UserIndex      int
	AssistantIndex int
	Summary        *string
}

func summaryText(message msgmodel.WithParts) *string {
	parts := []string{}
	for _, raw := range message.Parts {
		part, ok := raw.(msgmodel.TextPart)
		if !ok {
			continue
		}
		text := jscompat.Trim(part.Text)
		if text != "" {
			parts = append(parts, text)
		}
	}
	text := jscompat.Trim(strings.Join(parts, "\n\n"))
	if text == "" {
		return nil
	}
	return &text
}

func completedCompactions(messages []msgmodel.WithParts) []CompletedCompaction {
	users := map[string]int{}
	for i, message := range messages {
		user, ok := message.Info.(msgmodel.User)
		if !ok || !hasCompaction(message.Parts) {
			continue
		}
		users[user.ID] = i
	}
	out := []CompletedCompaction{}
	for assistantIndex, message := range messages {
		assistant, ok := message.Info.(msgmodel.Assistant)
		if !ok || !boolPointer(assistant.Summary) ||
			assistant.Finish == nil || *assistant.Finish == "" || assistant.Error != nil {
			continue
		}
		userIndex, ok := users[assistant.ParentID]
		if !ok {
			continue
		}
		out = append(out, CompletedCompaction{
			UserIndex: userIndex, AssistantIndex: assistantIndex,
			Summary: summaryText(message),
		})
	}
	return out
}

func BuildDurableBlockerPin(openBlockers []ledgers.OpenBlocker) string {
	if len(openBlockers) == 0 {
		return ""
	}
	lines := []string{
		"# DURABLE OPEN BLOCKERS (authoritative — from .codeaf/blocker-ledger.jsonl)",
		"These blockers are recorded on disk and survive context loss. They are the",
		"run's current unresolved worklist. Reproduce EVERY one verbatim in the",
		`"### Blocked" section of your summary — never drop, merge, or paraphrase one`,
		"away just because the conversation above no longer mentions it:",
	}
	for _, blocker := range openBlockers {
		lines = append(lines, "- ["+blocker.BlockerID+"] "+blocker.Text)
	}
	return strings.Join(lines, "\n")
}

func BuildPrompt(previousSummary *string, context []string) string {
	anchor := "Create a new anchored summary from the conversation history above."
	if previousSummary != nil && *previousSummary != "" {
		anchor = strings.Join([]string{
			"Update the anchored summary below using the conversation history above.",
			"Preserve still-true details, remove stale details, and merge in the new facts.",
			"<previous-summary>",
			*previousSummary,
			"</previous-summary>",
		}, "\n")
	}
	parts := []string{anchor, SummaryTemplate}
	parts = append(parts, context...)
	return strings.Join(parts, "\n\n")
}

func preserveRecentBudget(cfg overflow.Config, model overflow.Model) float64 {
	if cfg.Compaction != nil && cfg.Compaction.PreserveRecentTokens != nil {
		return *cfg.Compaction.PreserveRecentTokens
	}
	usable := overflow.Usable(overflow.UsableInput{Cfg: cfg, Model: model})
	return math.Min(
		MaxPreserveRecentTokens,
		math.Max(MinPreserveRecentTokens, math.Floor(usable*0.25)),
	)
}

func IsSyntheticUser(message msgmodel.WithParts) bool {
	if _, ok := message.Info.(msgmodel.User); !ok {
		return false
	}
	for _, raw := range message.Parts {
		part, ok := raw.(msgmodel.TextPart)
		if !ok || part.Synthetic == nil || !*part.Synthetic {
			return false
		}
	}
	return true
}

func Turns(messages []msgmodel.WithParts) []Turn {
	result := []Turn{}
	for i, message := range messages {
		user, ok := message.Info.(msgmodel.User)
		if !ok || hasCompaction(message.Parts) || IsSyntheticUser(message) {
			continue
		}
		result = append(result, Turn{
			Start: i, End: len(messages), ID: user.ID,
		})
	}
	for i := 0; i < len(result)-1; i++ {
		result[i].End = result[i+1].Start
	}
	return result
}

var (
	driftTokenRE     = regexp.MustCompile(`[A-Za-z0-9_./-]+`)
	driftLetterRE    = regexp.MustCompile(`[A-Za-z]`)
	driftExtensionRE = regexp.MustCompile(`\.[A-Za-z][A-Za-z0-9]{0,5}$`)
)

func driftPaths(text string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, token := range driftTokenRE.FindAllString(text, -1) {
		if utf16Length(token) < 3 || !driftLetterRE.MatchString(token) {
			continue
		}
		if !strings.Contains(token, "/") && !driftExtensionRE.MatchString(token) {
			continue
		}
		if !seen[token] {
			seen[token] = true
			out = append(out, token)
		}
	}
	return out
}

func driftMessageText(message msgmodel.WithParts) string {
	chunks := []string{}
	for _, raw := range message.Parts {
		switch part := raw.(type) {
		case msgmodel.TextPart:
			if part.Text != "" {
				chunks = append(chunks, part.Text)
			}
		case msgmodel.ToolPart:
			if completed, ok := part.State.(msgmodel.ToolStateCompleted); ok &&
				completed.Output != "" {
				chunks = append(chunks, completed.Output)
			}
			for _, field := range part.State.ToolInput().Fields() {
				var value string
				if json.Unmarshal(field.Value, &value) == nil {
					chunks = append(chunks, value)
				}
			}
		}
	}
	return strings.Join(chunks, "\n")
}

func WorkingSetDrift(messages []msgmodel.WithParts, window ...int) float64 {
	windowTurns := 6
	if len(window) > 0 {
		windowTurns = window[0]
	}
	if len(messages) == 0 {
		return 0
	}
	realTurns := Turns(messages)
	boundary := 0
	if len(realTurns) > windowTurns {
		index := len(realTurns) - windowTurns
		if index < 0 || index >= len(realTurns) {
			panic("compaction: workingSetDrift turn window index out of range")
		}
		boundary = realTurns[index].Start
	}
	texts := make([]string, len(messages))
	tokens := make([]int, len(messages))
	for i, message := range messages {
		texts[i] = driftMessageText(message)
		tokens[i] = int(math.Ceil(float64(utf16Length(texts[i])) / 4))
	}
	recentPaths := map[string]bool{}
	for i := boundary; i < len(messages); i++ {
		for _, path := range driftPaths(texts[i]) {
			recentPaths[path] = true
		}
	}
	total, stale := 0, 0
	for i, count := range tokens {
		total += count
		if i >= boundary {
			continue
		}
		referenced := false
		for _, path := range driftPaths(texts[i]) {
			if recentPaths[path] {
				referenced = true
				break
			}
		}
		if !referenced {
			stale += count
		}
	}
	if total == 0 {
		return 0
	}
	return float64(stale) / float64(total)
}

func HeadTailTruncate(text string, maxChars float64) string {
	length := float64(utf16Length(text))
	if maxChars <= 0 || length <= maxChars {
		return text
	}
	headChars := math.Floor(maxChars / 4)
	tailChars := maxChars - headChars
	head := sliceUTF16Range(text, 0, jsSliceIndex(headChars, utf16Length(text)))
	tailStart := jsSliceStart(length-tailChars, utf16Length(text))
	tail := sliceUTF16Range(text, tailStart, utf16Length(text))
	omitted := length - headChars - tailChars
	return head + "\n[Tool output truncated for evidence: omitted " +
		jscompat.FormatNumber(omitted) + " chars]\n" + tail
}

func EvidenceBlocksFromMessages(
	messages []msgmodel.WithParts, maxToolChars ...float64,
) []string {
	maxChars := float64(EvidenceToolOutputMaxChars)
	if len(maxToolChars) > 0 {
		maxChars = maxToolChars[0]
	}
	blocks := []string{}
	for _, message := range messages {
		parts := []string{}
		for _, raw := range message.Parts {
			switch part := raw.(type) {
			case msgmodel.TextPart:
				if jscompat.Trim(part.Text) != "" {
					parts = append(parts, part.Text)
				}
			case msgmodel.ToolPart:
				completed, ok := part.State.(msgmodel.ToolStateCompleted)
				if ok && completed.Output != "" && jscompat.Trim(completed.Output) != "" {
					parts = append(parts, HeadTailTruncate(completed.Output, maxChars))
				}
			}
		}
		text := strings.Join(parts, "\n")
		if jscompat.Trim(text) != "" {
			blocks = append(blocks, text)
		}
	}
	return blocks
}

type Model struct {
	Message  msgmodel.Model
	Overflow overflow.Model
}

type EstimateFunc func(messages []msgmodel.WithParts, model Model) (float64, error)

type Selection struct {
	Head        []msgmodel.WithParts
	TailStartID *string
}

func selectMessages(
	messages []msgmodel.WithParts,
	cfg overflow.Config,
	model Model,
	estimate EstimateFunc,
) (Selection, error) {
	limit := float64(DefaultTailTurns)
	if cfg.Compaction != nil && cfg.Compaction.TailTurns != nil {
		limit = *cfg.Compaction.TailTurns
	}
	if limit <= 0 {
		return Selection{Head: messages}, nil
	}
	budget := preserveRecentBudget(cfg, model.Overflow)
	all := Turns(messages)
	if len(all) == 0 {
		return Selection{Head: messages}, nil
	}
	start := jsSliceStart(-limit, len(all))
	recent := all[start:]
	sizes := make([]float64, len(recent))
	for i, turn := range recent {
		size, err := estimate(messages[turn.Start:turn.End], model)
		if err != nil {
			return Selection{}, err
		}
		sizes[i] = size
	}
	total := float64(0)
	var keep *Tail
	for i := len(recent) - 1; i >= 0; i-- {
		turn := recent[i]
		size := sizes[i]
		if total+size <= budget {
			total += size
			value := Tail{Start: turn.Start, ID: turn.ID}
			keep = &value
			continue
		}
		remaining := budget - total
		split, err := splitTurn(messages, turn, model, remaining, estimate)
		if err != nil {
			return Selection{}, err
		}
		if split != nil {
			keep = split
		}
		break
	}
	if keep == nil || keep.Start == 0 {
		return Selection{Head: messages}, nil
	}
	id := keep.ID
	return Selection{Head: messages[:keep.Start], TailStartID: &id}, nil
}

func splitTurn(
	messages []msgmodel.WithParts,
	turn Turn,
	model Model,
	budget float64,
	estimate EstimateFunc,
) (*Tail, error) {
	if budget <= 0 || turn.End-turn.Start <= 1 {
		return nil, nil
	}
	for start := turn.Start + 1; start < turn.End; start++ {
		size, err := estimate(messages[start:turn.End], model)
		if err != nil {
			return nil, err
		}
		if size > budget {
			continue
		}
		return &Tail{Start: start, ID: messages[start].Info.MessageID()}, nil
	}
	return nil, nil
}

func estimateTokens(input string) float64 {
	return math.Max(0, math.Floor(float64(utf16Length(input))/4+0.5))
}

func hasCompaction(parts msgmodel.Parts) bool {
	for _, part := range parts {
		if _, ok := part.(msgmodel.CompactionPart); ok {
			return true
		}
	}
	return false
}

func boolPointer(value *bool) bool { return value != nil && *value }

func utf16Length(value string) int { return len(utf16.Encode([]rune(value))) }

func sliceUTF16Range(value string, start, end int) string {
	units := utf16.Encode([]rune(value))
	if start < 0 {
		start = 0
	}
	if end > len(units) {
		end = len(units)
	}
	if start > end {
		start = end
	}
	return string(utf16.Decode(units[start:end]))
}

func jsSliceIndex(value float64, length int) int {
	if math.IsNaN(value) {
		return 0
	}
	if math.IsInf(value, 1) {
		return length
	}
	if math.IsInf(value, -1) {
		return 0
	}
	index := int(math.Trunc(value))
	if index < 0 {
		index += length
		if index < 0 {
			return 0
		}
	}
	if index > length {
		return length
	}
	return index
}

func jsSliceStart(value float64, length int) int {
	return jsSliceIndex(value, length)
}
