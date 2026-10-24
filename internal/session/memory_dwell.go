package session

import (
	"sort"
	"strings"
	"unicode"

	"github.com/Agent-Field/codeaf/internal/reflex"
	"github.com/Agent-Field/codeaf/internal/store"
)

// reuseMemoryDwell keeps adjacent turns byte-identical. Material changes open
// a new admission episode; mutations refresh active text while retaining the
// current episode's admitted lines within the store's ordinary budget.
func (a *Agent) reuseMemoryDwell(ctx *store.MemoryContext, revision int64) bool {
	if a.memory == nil {
		return false
	}
	a.memory.mu.Lock()
	defer a.memory.mu.Unlock()
	if a.memory.selection == nil || memoryMaterialContextChanged(a.memory.dwellContext, *ctx) {
		return false
	}
	if a.memory.mutationSeq == revision {
		a.memory.selectionContext = *ctx
		return true
	}
	for _, memory := range a.memory.selection.Memories {
		ctx.DwellIDs = append(ctx.DwellIDs, memory.ID)
	}
	return false
}

func memoryMaterialContextChanged(previous, current store.MemoryContext) bool {
	if previous.Project != current.Project {
		return true
	}
	if strings.Join(previous.Tasks, "\n") != strings.Join(current.Tasks, "\n") {
		return true
	}
	if memoryExplicitTopicSwitch(strings.Join(current.Keywords, " ")) {
		return true
	}
	if memoryTopicChanged(memoryGoal(previous.Brief), memoryGoal(current.Brief)) {
		return true
	}
	return memoryTopicChanged(strings.Join(previous.Keywords, " "), strings.Join(current.Keywords, " "))
}

func memoryExplicitTopicSwitch(text string) bool {
	lower := strings.ToLower(text)
	for _, phrase := range []string{"new topic", "switch to", "new goal", "different task", "now focus on", "set that aside"} {
		if strings.Contains(lower, phrase) {
			return true
		}
	}
	return false
}
func memoryGoal(brief string) string {
	for _, line := range strings.Split(brief, "\n") {
		if strings.HasPrefix(line, "goal: ") {
			return strings.TrimPrefix(line, "goal: ")
		}
	}
	return ""
}
func memoryTopicChanged(previous, current string) bool {
	before, after := memoryContextTopics(previous), memoryContextTopics(current)
	if len(after) == 0 {
		return false
	}
	if len(before) == 0 {
		return true
	}
	if len(after) <= 2 && memoryTopicContinuation(current) {
		return false
	}
	shared := 0
	for _, word := range after {
		if _, found := sort.Find(len(before), func(i int) int { return strings.Compare(word, before[i]) }); found {
			shared++
		}
	}
	return float64(shared)/float64(min(len(before), len(after))) < 0.25
}
func memoryTopicContinuation(text string) bool {
	for _, word := range strings.Fields(strings.ToLower(text)) {
		switch strings.Trim(word, ".,?!:;") {
		case "it", "that", "this", "there":
			return true
		}
	}
	return false
}
func memoryInflightTasks(brief string) []string {
	var tasks []string
	inFlight := false
	for _, line := range strings.Split(brief, "\n") {
		if line == "in flight:" {
			inFlight = true
			continue
		}
		if !inFlight {
			continue
		}
		if !strings.HasPrefix(line, "- ") {
			break
		}
		tasks = append(tasks, strings.TrimPrefix(line, "- "))
	}
	return tasks
}

func memoryContextTopics(text string) []string {
	noise := map[string]bool{"the": true, "and": true, "for": true, "with": true, "this": true, "that": true, "what": true, "how": true, "should": true, "would": true, "could": true, "please": true, "about": true, "from": true, "into": true, "our": true, "your": true, "now": true, "also": true, "again": true, "show": true, "add": true, "give": true, "an": true, "example": true, "examples": true, "keep": true, "two": true, "sentences": true, "you": true, "can": true, "does": true, "why": true, "next": true, "hello": true, "hi": true, "thanks": true, "okay": true, "yes": true, "continue": true}
	words := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	seen := map[string]bool{}
	for _, word := range words {
		if len(word) >= 2 && !noise[word] {
			seen[word] = true
		}
	}
	topics := make([]string, 0, len(seen))
	for word := range seen {
		topics = append(topics, word)
	}
	sort.Strings(topics)
	return topics
}

// recordMemoryContradictions is negative feedback only from a person's actual
// correction. Replacement attribution remains useful when the new line was
// already saved by a tool and the noticing pass therefore captures no Keep.
func (a *Agent) recordMemoryContradictions(found reflex.NoticeResult, kept []store.Memory, ctx store.MemoryContext) {
	indices := append([]int(nil), found.Contradicted...)
	if found.Keep != "" && found.Durability != "ephemeral" && found.Replaces > 0 {
		indices = append(indices, found.Replaces)
	}
	seen := map[int]bool{}
	for _, index := range indices {
		if index < 1 || index > len(kept) || seen[index] {
			continue
		}
		seen[index] = true
		if err := a.memory.store.RecordMemoryContextEvent(kept[index-1].ID, "contradicted", ctx); err != nil {
			a.journalMemoryFailure("contradicted", err)
		}
	}
}

func memoryFeedbackContradicts(found reflex.NoticeResult, index int) bool {
	for _, contradicted := range found.Contradicted {
		if contradicted == index {
			return true
		}
	}
	return found.Replaces == index && found.Keep != "" && found.Durability != "ephemeral"
}
