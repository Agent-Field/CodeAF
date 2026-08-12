package plan

import (
	"fmt"
	"strings"
)

// Bundle lays several requests that do not feed each other side by side: every
// part in stage one, a synthesis that reads them all in stage two, no planner
// call anywhere.
//
// It exists because the prompt route was measured to fail. The spine was told,
// in its own words, that a bundle's listed order is never a gate; on the live
// run it restated that rule — "none of them depends on the others" — and then
// emitted one stage per request with invented cross-part needs, and three
// independent asks ran end to end. The judgment of WHETHER the requests are
// independent stays with the model that read the whole ask (the intent
// compiler names the parts); what stops being a judgment is the layout, which
// is geometry once independence is declared: parts share a stage, and a stage
// is never manufactured from the order words arrived in.
//
// The shape is the ensemble's — N workers, one merge — because that is the one
// graph shape in the system whose parallelism is load-bearing and proven.
func Bundle(goal string, parts []string) *Graph {
	graph := &Graph{Goal: goal}
	kept := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			kept = append(kept, part)
		}
	}
	// The names are the person's own words for the parts, not an invention, and
	// they are cut as a set rather than one at a time: recognisable in a rail
	// BESIDE ITS SIBLINGS is a property of the group, not of any one string.
	names := clipTitles(kept)
	ids := make([]int, 0, len(kept))
	for index, part := range kept {
		ids = append(ids, graph.Add(Node{
			Stage:   1,
			Title:   names[index],
			Summary: part,
			Brief:   part,
			Size:    SizeAtomic,
			Kind:    KindWork,
		}))
	}
	graph.Add(Node{
		Stage:   2,
		Bundle:  true,
		Title:   "Deliver together",
		Summary: fmt.Sprintf("Assemble the %d finished results into one delivery, in the order they were asked.", len(ids)),
		Needs:   ids,
		Kind:    KindSynthesis,
		Brief: fmt.Sprintf("The %d results above were asked for in one breath and finished independently. "+
			"Your final message is the whole delivery: every result in full, in the order the person asked, "+
			"each under its own words from the request. Nothing is summarised away and nothing is added between them "+
			"— with one exception, and it is your whole reason for existing as a separate step: when one part's "+
			"finding impeaches another part's number, reconciling them is YOUR work. A total delivered one line "+
			"above the discovery that inflates it is wrong, not thorough. Recompute from the shared material when "+
			"you can, and when you cannot, lead the affected result with its corrected value, never the impeached one. "+
			"Assembling is not itself news: which results arrived thin, which files were empty, what you had to go and "+
			"re-read — that is your working, and the delivery opens on the first result rather than on a report about "+
			"gathering them. If a part is genuinely missing, say so where that part belongs, in one line, and deliver "+
			"the rest.", len(ids)),
	})
	return graph
}

// railWidth is how much of a part's own words a rail row can hold.
const railWidth = 48

// clipTitles names a whole set of siblings at once, and that is the entire
// point of it taking the set.
//
// A prefix cut is a fine name for one string and a terrible one for twelve that
// share a stem. The measured case: twelve leaves of one enumerated bundle, each
// beginning "Write a one-paragraph technical profile of ", the database name —
// the only word that told them apart — sitting past character 48. The rail
// showed the same row twelve times. Clipping is a display decision, so it is
// made where the display's actual constraint lives: not "is this string short
// enough" but "does this name still pick this part out from the ones next to
// it".
//
// So a collision is repaired by keeping the DIVERGENCE rather than the head.
// The group's shared opening is cut down to a stub, and what follows the point
// where the parts stop agreeing — the discriminating word, whatever it is —
// gets the rest of the width. Nothing here knows what an enumeration looks
// like; it only knows that two rows reading the same are worth less than either
// of them read alone.
func clipTitles(parts []string) []string {
	normal := make([]string, len(parts))
	names := make([]string, len(parts))
	for index, part := range parts {
		normal[index] = strings.Join(strings.Fields(part), " ")
		names[index] = clipWords(normal[index], railWidth)
	}

	groups := make(map[string][]int, len(parts))
	for index, name := range names {
		groups[name] = append(groups[name], index)
	}
	for _, group := range groups {
		if len(group) < 2 {
			continue
		}
		from := divergence(normal, group)
		if from == 0 {
			// The parts are identical from their first word, which means the
			// tail cannot tell them apart either. The numbering pass below is
			// the only honest answer.
			continue
		}
		for _, index := range group {
			names[index] = keepTail(normal[index], from)
		}
	}

	// The backstop: parts that really are the same words, or whose divergence
	// repeats inside the width, still get one row each. A number is a poor name
	// and a duplicate is a worse one.
	seen := make(map[string]int, len(names))
	for index, name := range names {
		seen[name]++
		if count := seen[name]; count > 1 {
			suffix := fmt.Sprintf(" (%d)", count)
			names[index] = clipWords(name, railWidth-len([]rune(suffix))) + suffix
		}
	}
	return names
}

// clipTitle bounds one part's display name to rail width on a word boundary. It
// is what a part gets when nothing beside it competes for the same name.
func clipTitle(part string) string {
	return clipWords(strings.Join(strings.Fields(part), " "), railWidth)
}

// clipWords cuts to at most limit characters, preferring the last word boundary
// in the second half so a name ends on a whole word rather than mid-syllable.
func clipWords(text string, limit int) string {
	if limit < 1 {
		limit = 1
	}
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	cut := limit
	for index := limit; index > limit/2; index-- {
		if runes[index] == ' ' {
			cut = index
			break
		}
	}
	return strings.TrimRight(string(runes[:cut]), " ")
}

// divergence is the first character, on a word boundary, at which the named
// parts stop all saying the same thing.
func divergence(normal []string, group []int) int {
	shared := []rune(normal[group[0]])
	for _, index := range group[1:] {
		other := []rune(normal[index])
		limit := len(shared)
		if len(other) < limit {
			limit = len(other)
		}
		at := limit
		for offset := 0; offset < limit; offset++ {
			if shared[offset] != other[offset] {
				at = offset
				break
			}
		}
		shared = shared[:at]
	}
	// Back off to the start of the word the parts disagree inside, so the tail
	// opens on a whole word.
	for len(shared) > 0 && shared[len(shared)-1] != ' ' {
		shared = shared[:len(shared)-1]
	}
	return len(shared)
}

// keepTail builds a name out of a stub of the shared opening and as much of the
// distinguishing tail as the width allows.
func keepTail(text string, from int) string {
	runes := []rune(text)
	if from >= len(runes) {
		return clipWords(text, railWidth)
	}
	const joint = " … "
	tail := clipWords(strings.TrimSpace(string(runes[from:])), railWidth/2)
	head := clipWords(text, railWidth-len([]rune(joint))-len([]rune(tail)))
	if head == "" {
		return clipWords(tail, railWidth)
	}
	return head + joint + tail
}
