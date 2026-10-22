package store

import (
	"math"
	"regexp"
	"sort"
	"strings"
)

type memorySpecificity struct {
	Phrase, Entities    int
	Coverage, Proximity float64
}

func (s memorySpecificity) value() float64 {
	return float64(s.Phrase)*1e6 + float64(s.Entities)*1e3 + s.Coverage + s.Proximity/1000
}

type memorySpecificityInput struct {
	cues     map[string]bool
	phrases  map[string]bool
	entities map[string]bool
}

var memoryEntityPattern = regexp.MustCompile(`(?:~?/|\./)[[:alnum:]_.\-/]+|\b[vV]?\d+(?:\.\d+){1,3}\b|\b(?i:pr|issue|task)[ #:_-]*\d+\b|\b[A-Z]{2,8}\b`)

func memoryEntities(text string) map[string]bool {
	out := map[string]bool{}
	for _, entity := range memoryEntityPattern.FindAllString(text, -1) {
		entity = strings.TrimRight(entity, ".,;:")
		if !strings.Contains(entity, "/") {
			entity = strings.ToLower(entity)
			if strings.HasPrefix(entity, "pr") || strings.HasPrefix(entity, "issue") || strings.HasPrefix(entity, "task") {
				entity = strings.NewReplacer(" ", "", "#", "", ":", "", "_", "", "-", "").Replace(entity)
			}
		}
		out[entity] = true
	}
	return out
}
func memorySpecificityContext(ctx MemoryContext) memorySpecificityInput {
	out := memorySpecificityInput{cues: memoryCueMap(ctx), phrases: map[string]bool{}, entities: memoryEntities(memoryContextText(ctx))}
	for _, text := range append([]string{ctx.Brief, strings.Join(ctx.Keywords, " ")}, ctx.Tasks...) {
		words := memoryWords(text)
		for start := range words {
			for n := 2; n <= 5 && start+n <= len(words); n++ {
				out.phrases[strings.Join(words[start:start+n], "\x00")] = true
			}
		}
	}
	return out
}
func memorySpecificityFor(m Memory, input memorySpecificityInput) memorySpecificity {
	out := memorySpecificity{}
	words := memoryWords(m.Text)
	seen := map[string]bool{}
	first, last, matches := len(words), -1, 0
	for at, w := range words {
		if !input.cues[w] {
			continue
		}
		matches++
		first = min(first, at)
		last = at
		if seen[w] {
			continue
		}
		seen[w] = true
		out.Coverage += 1 / float64(max(1, m.TopicFans[w]))
	}
	for entity := range memoryEntities(m.Text) {
		if input.entities[entity] {
			out.Entities++
		}
	}
	out.Phrase = memoryPhraseOverlap(words, m.TopicFans, input)
	if matches > 0 {
		out.Proximity = float64(matches) / float64(last-first+1)
	}
	out.Coverage /= 1 + float64(len(words))/32
	return out
}
func memoryPhraseOverlap(words []string, fans map[string]int, input memorySpecificityInput) int {
	longest := 0
	for start := range words {
		for n := 2; n <= 5 && start+n <= len(words); n++ {
			segment := words[start : start+n]
			if input.phrases[strings.Join(segment, "\x00")] && memoryDistinctivePhrase(segment, fans, input.cues) {
				longest = max(longest, n)
			}
		}
	}
	return longest
}
func memoryDistinctivePhrase(words []string, fans map[string]int, cues map[string]bool) bool {
	for _, w := range words {
		if cues[w] {
			return true
		}
	}
	return false
}
func memoryAdmissionLess(a, b memoryScored) bool {
	if a.s.Phrase != b.s.Phrase {
		return a.s.Phrase > b.s.Phrase
	}
	if a.s.Entities != b.s.Entities {
		return a.s.Entities > b.s.Entities
	}
	if a.s.Coverage != b.s.Coverage {
		return a.s.Coverage > b.s.Coverage
	}
	if a.s.Proximity != b.s.Proximity {
		return a.s.Proximity > b.s.Proximity
	}
	return memoryRankLess(a, b)
}
func selectMemoryAdmissions(ranking []memoryScored, ctx MemoryContext, out *MemoryBlock) {
	used := 0
	chosen := map[string]bool{}
	for _, r := range ranking {
		if r.pin {
			admitMemory(out, r, "explicit pin", chosen)
			used += MemoryLineCost(r.m.Text)
		}
	}
	byID := map[string]memoryScored{}
	for _, r := range ranking {
		byID[r.m.ID] = r
	}
	for _, id := range ctx.DwellIDs {
		r, ok := byID[id]
		if ok && !chosen[id] && used+MemoryLineCost(r.m.Text) <= MemoryBlockRunes {
			admitMemory(out, r, "kept for this conversation", chosen)
			used += MemoryLineCost(r.m.Text)
		}
	}
	sort.SliceStable(ranking, func(i, j int) bool { return memoryAdmissionLess(ranking[i], ranking[j]) })
	remaining := max(0, MemoryBlockRunes-used)
	floor := int(math.Ceil(.4 * float64(remaining)))
	spent := 0
	for _, r := range ranking {
		if spent >= floor {
			break
		}
		if chosen[r.m.ID] || !memoryDurable(r.m) || r.a < -3 {
			continue
		}
		cost := MemoryLineCost(r.m.Text)
		if cost > MemoryBlockRunes-used {
			continue
		}
		admitMemory(out, r, "protected rule budget", chosen)
		used += cost
		spent += cost
	}
	used = spendContextualLane(ranking, out, chosen, used)
	fillMemoryAdmissions(ranking, out, chosen, used)
}
func admitMemory(out *MemoryBlock, r memoryScored, reason string, chosen map[string]bool) {
	chosen[r.m.ID] = true
	out.Memories = append(out.Memories, r.m)
	d := out.Decisions[r.m.ID]
	d.Riding = true
	d.Reason = reason
	out.Decisions[r.m.ID] = d
}
func fillMemoryAdmissions(ranking []memoryScored, out *MemoryBlock, chosen map[string]bool, used int) {
	for _, r := range ranking {
		if chosen[r.m.ID] {
			continue
		}
		d := out.Decisions[r.m.ID]
		switch {
		case r.a < -3:
			d.Reason = "below activation threshold"
		case used+MemoryLineCost(r.m.Text) > MemoryBlockRunes:
			d.Reason = "prompt budget"
		default:
			admitMemory(out, r, "specific match to this context", chosen)
			used += MemoryLineCost(r.m.Text)
			continue
		}
		out.Decisions[r.m.ID] = d
		out.Omitted++
	}
}
func memoryCueMap(ctx MemoryContext) map[string]bool {
	out := map[string]bool{}
	for _, w := range memoryContextCues(ctx) {
		out[w] = true
	}
	return out
}
func memoryLane(m Memory) string {
	if m.Rule || m.Origin == OriginPerson {
		return "pinned"
	}
	if memoryDurable(m) {
		return "durable"
	}
	return "contextual"
}
func memoryScopeReason(m Memory, owners map[string]bool) string {
	if len(owners) > 0 && !owners[m.Owner] {
		return "outside this conversation's owners"
	}
	if m.Status != "" && m.Status != MemoryActive {
		return "archived by explicit change"
	}
	return ""
}
func memoryContextText(ctx MemoryContext) string {
	return strings.Join(append([]string{ctx.Brief, strings.Join(ctx.Keywords, " ")}, ctx.Tasks...), " ")
}

func spendContextualLane(ranking []memoryScored, out *MemoryBlock, chosen map[string]bool, used int) int {
	for _, r := range ranking {
		if chosen[r.m.ID] || memoryDurable(r.m) || r.a < -3 {
			continue
		}
		cost := MemoryLineCost(r.m.Text)
		if used+cost > MemoryBlockRunes {
			continue
		}
		admitMemory(out, r, "specific match to this context", chosen)
		used += cost
	}
	return used
}
