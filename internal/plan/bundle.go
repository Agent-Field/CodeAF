package plan

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
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

// sequencePrompt recovers the one thing the bundle layout cannot express and
// therefore destroys: the order the parts actually had.
//
// A bundle is laid out on a declaration — the call that read the whole ask said
// these requests do not feed each other — and that declaration is the single
// point of failure in the cheap route, because it is the one judgment nothing
// downstream can check. When it is wrong the parts are admitted with no edges
// at all, and no edge is not a weak claim about order, it is a positive claim
// that there is none: a part whose whole job is to work over its siblings is
// claimable the instant the job is admitted, runs against nothing, and invents
// what it was supposed to read. The layout is geometry, but only once the
// order is known, and a flat list carries no order to know it from.
//
// So the same question every planned graph already answers is asked of the
// parts, once, in the cheapest possible form. It is written against the bias
// bind.go names: asked what depends on what, a model returns a chain. The empty
// answer is stated as the normal one, the test for an edge is made operational
// — name the material that crosses over — and the one case that motivated the
// pass is described by its structure rather than by any list of words a
// gathering request might happen to use.
const sequencePrompt = `You decide which of these requests must wait for another.

They arrived together in one breath. Each is run by a separate agent that
receives its own request and the outputs of whatever you list for it — and
nothing else. So the list does two jobs at once: it decides when a request may
start, and it decides what its agent is allowed to see.

One request waits for another only when it cannot produce a correct, complete
result without reading that one's actual output. Name to yourself the specific
fact, figure, file, or finding that crosses over. If you cannot name one, there
is no edge.

These are not reasons to wait: sharing a subject, being spoken later in the
sentence, matching format or tone, or being "informed by" another request.

Most of the time nothing waits. Requests asked in one breath usually stand
alone, every edge you record is time the person spends waiting that they would
not otherwise spend, and an empty list is the normal and expected answer.

The exception is the request whose own job is to work over what the others
produce — to assemble them, compare them, weigh them against each other, or
write them up as a single thing. That request cannot begin before the requests
it works over have finished. Leaving it empty would start it beside the very
material it exists to consume, and it would then produce that material itself
rather than wait. Name every request whose output it works from.

Answer with one bare JSON object and nothing else.`

var sequenceSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "waits": {
      "type": "array",
      "items": {
        "type": "object",
        "properties": {
          "request": { "type": "integer" },
          "after":   { "type": "array", "items": { "type": "integer" } }
        },
        "required": ["request", "after"],
        "additionalProperties": false
      }
    }
  },
  "required": ["waits"],
  "additionalProperties": false
}`)

type sequenceReply struct {
	Waits []struct {
		Request int   `json:"request"`
		After   []int `json:"after"`
	} `json:"waits"`
}

// Sequence records the order a declared bundle actually has, on the graph
// Bundle laid out. It is the bundle route's whole check on the declaration it
// was built from, and it costs one call however many parts there are.
//
// Edges are added rather than assigned: the synthesis already needs every part
// and must keep needing them whatever this pass answers, and AddNeed drops any
// edge that would close a cycle, so a model that answers a mutual wait leaves
// the graph runnable instead of unsplicable. A failed call leaves the layout
// exactly as Bundle wrote it — the same flat shape the route had before this
// pass existed — which is a worse plan than a sequenced one and still a
// plan, and the error is returned so the caller can say so.
func Sequence(ctx context.Context, client Completer, graph *Graph) (Usage, error) {
	if graph == nil {
		return Usage{}, nil
	}
	parts := make([]Node, 0, len(graph.Nodes))
	for _, node := range graph.Nodes {
		if node.Stage == 1 {
			parts = append(parts, node)
		}
	}
	// One part cannot wait for a sibling it does not have.
	if len(parts) < 2 {
		return Usage{}, nil
	}

	ctx = provider.WithCall(ctx, provider.ClassPlanBind)
	var listed strings.Builder
	for _, part := range parts {
		fmt.Fprintf(&listed, "%d. %s — %s\n", part.ID, part.Title, part.Summary)
	}
	messages := []ai.Message{
		systemMessage(sequencePrompt),
		userMessage("The whole ask, as it was made: " + graph.Goal),
		userMessage("For each of these requests, list the requests it must wait for:\n" + listed.String()),
	}
	var reply sequenceReply
	response, err := structured(ctx, client, messages, sequenceSchema, &reply)
	if err != nil {
		return usageFrom(response), fmt.Errorf("sequence bundle: %w", err)
	}

	inside := make(map[int]bool, len(parts))
	for _, part := range parts {
		inside[part.ID] = true
	}
	// An id nobody has heard of is this call's characteristic failure, and the
	// same one bind reports: the answer is numbers, and a model that has lost
	// the list invents them. The reachable edges are still wired — a bundle that
	// gets three of its four edges is ordered better than one that gets none —
	// and the verdict says the answer was not clean.
	clean := true
	for _, entry := range reply.Waits {
		if !inside[entry.Request] {
			clean = false
			continue
		}
		for _, after := range entry.After {
			if !inside[after] {
				clean = false
				continue
			}
			if after == entry.Request {
				continue
			}
			_ = graph.AddNeed(entry.Request, after)
		}
	}
	if clean {
		provider.Report(ctx, provider.VerdictVerifiedSuccess)
	} else {
		provider.Report(ctx, provider.VerdictSemanticFailure)
	}
	return usageFrom(response), nil
}

// usageFrom is Usage in the shape the callers of this pass account in.
func usageFrom(response *ai.Response) Usage {
	var usage Usage
	usage.Add(usageOf(response))
	return usage
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
