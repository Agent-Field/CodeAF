package session

// THE LAW REGISTRY: EVERY LAW IS STATED ONCE, IN THE ONE PLACE ITS CLASS SAYS.
//
// The prompt diet found the same rule written three times — "ask through `ask`,
// never in prose" sat in two Tool Policy bullets and a third time in the belt
// fact under them — and a rule written three times is not three times as
// obeyed. It is three sentences to keep in step, three bills paid on every
// request of every turn (prefixbudget_test.go weighs that), and, when one copy
// is edited and the others are not, a page that contradicts itself in front of
// a model that has to pick.
//
// So duplication is a BUILD FAILURE here, the way the manual law and the icon
// law are. Each law unit below carries an id, the class that decides where it
// is delivered, and the KEY SENTENCE a reader would recognise it by. This test
// renders the widest page any agent can be handed (prefixbudget_test.go's
// [widestPage]) and marshals the belt the shipping conversation door assembles
// (belt_wiring_test.go's [v3ShapedAgent]), and asks three questions of every
// unit:
//
//   - it is SAID: the key sentence is somewhere in the page or the tool block,
//     so that a law cannot be deleted by accident and leave a live registry
//     entry describing text nobody sends;
//   - it is said ONCE: the sentence occurs in exactly one place, and the
//     failure names the sentence and both places, because a lane that has just
//     written the second copy has no other way to see the first;
//   - a VERB-class law names a tool this belt CARRIES: prose about a verb the
//     model does not have is prose it will never be able to act on, and the
//     class exists precisely so that such a law travels with the tool and
//     disappears with it.
//
// WHAT TO DO WHEN IT FAILS. Not delete the registry line — that is the move
// that makes the gate meaningless. Decide which of the two places the law
// belongs in by its class, delete the OTHER copy, and leave the key sentence
// pointing at the survivor. If the two copies say genuinely different things,
// then one of them is not this law and wants its own id.

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"testing"
)

// lawClass is the delivery class from the diet's taxonomy
// (docs/design/prompt-diet/DESIGN.md §2). Every byte of law is filed under
// exactly one of them, and the class is what decides where the sentence lives.
type lawClass string

const (
	// lawCore rides prompts/system.md and is paid for on every request:
	// identity, tone, the routing table, workflow, delivery, the critical few.
	lawCore lawClass = "core"
	// lawVerb rides the tool's own description and costs only while the belt
	// carries that tool: what the verb does, its fields, its limits.
	lawVerb lawClass = "verb"
	// lawEvent rides the harness message that announces the event and costs
	// only on the turn it happens: a landing note, a `[carry on]`, a job exit.
	lawEvent lawClass = "event"
	// lawDemand rides `manual` and the `Loaded:` reply of `load_capability`,
	// and costs only when the model pulls it.
	lawDemand lawClass = "demand"
)

// lawUnit is one law: where it is delivered, and the sentence that IS it.
type lawUnit struct {
	// id is what a lane says out loud when it moves the law.
	id string
	// class is the one mechanism that delivers this law.
	class lawClass
	// key is the sentence a reader recognises the law by, verbatim and on ONE
	// line — the page wraps at about 78 columns, so a key that spans a wrap is
	// a key that matches nothing. It must carry no double quote, because half
	// the corpus this test searches is JSON and a quote there is escaped.
	key string
	// tool, on a lawVerb unit, is the tool whose description owns the sentence.
	// The belt must carry it; anything else is prose for a verb nobody has.
	tool string
}

// lawRegistry is the filed corpus. It is not every sentence in the prompt — it
// is every law a wave has had to move, plus the ones pinned elsewhere by
// substring, which are exactly the sentences most likely to be copied.
var lawRegistry = []lawUnit{
	// ── the decision ladder, which the diet collapsed from three copies to one
	{id: "ask.last-rung", class: lawCore, key: "Use `ask` only as the last rung"},
	{id: "ask.through-the-tool", class: lawCore, key: "ask THROUGH `ask`, never in prose"},
	{id: "ask.not-what-the-record-answers", class: lawCore, key: "Never ask for what the record, the tools or the repo already answer"},

	// ── the one sentence that replaces ten per-tool copies. A job, a watch, a
	// task and a quick task all report themselves; lanes deleting a tool's own
	// copy of that rule are deleting it on the strength of THIS line, so its
	// wording is a fixture and not a draft.
	{id: "handoff.reports-itself", class: lawCore, key: "never sleep, tail or poll for it"},

	// ── the working laws that other tests pin by substring, registered here so
	// that a second copy of one is caught at the same moment as a first edit.
	{id: "batch.one-breath", class: lawCore, key: "ASK FOR EVERYTHING YOU NEED IN ONE BREATH"},
	{id: "verify.depth-of-checking", class: lawCore, key: "DEPTH OF CHECKING FOLLOWS THE SIZE OF WHAT YOUR ANSWER CHANGES"},
	{id: "handoff.asked-again", class: lawCore, key: "THE QUESTION IS ASKED AGAIN WHILE YOU WORK"},
	{id: "handoff.dowry", class: lawCore, key: "The brief is the dowry"},
	{id: "handoff.no-planner", class: lawCore, key: "THERE IS NO PLANNER ON YOUR BELT"},
	{id: "record.check-the-transcript", class: lawCore, key: "BEFORE RUNNING A COMMAND, CHECK THE TRANSCRIPT"},
	{id: "record.numbers-come-from-here", class: lawCore, key: "NUMBERS AND FACTS COME FROM THE CONVERSATION"},
	{id: "bash.waits", class: lawCore, key: "finishes or its armed bound"},
	{id: "files.absolute-paths", class: lawCore, key: "EVERY file you name carries its FULL ABSOLUTE PATH"},
	{id: "images.travel-with-the-message", class: lawCore, key: "ATTACHED PICTURES TRAVEL IN THE MESSAGE WITH YOU"},
	{id: "elsewhere.other-windows", class: lawCore, key: "OTHER AFORGE WINDOWS ON THIS PROJECT ARE VISIBLE TO YOU"},

	// ── and the laws the page gave up to the verb that owns them. Each of
	// these was a page sentence until the diet; the description had said it all
	// along, in more words and with the field names beside it.
	{id: "write.in-parts", class: lawVerb, tool: "write", key: "Write very large files in parts"},
	{id: "read.offset-limit", class: lawVerb, tool: "read", key: "Use offset/limit for large files"},
	{id: "watch.instead-of-polling", class: lawVerb, tool: "watch", key: "instead of polling it every turn"},
	{id: "capabilities.continue-this-turn", class: lawVerb, tool: loadCapabilityToolName, key: "Full schemas arrive on the next model request; continue in this same turn"},
	{id: "task.lands-as-a-turn", class: lawVerb, tool: "propose_task", key: "starts a turn here when it lands, so never wait or poll"},
	{id: "tasks.stop-ends-work", class: lawVerb, tool: "tasks", key: "To END running work use stop"},
	{id: "tasks.not-to-wait", class: lawVerb, tool: "tasks", key: "Never to WAIT for handed-off work"},
}

// lawPlace is one searchable region of the fixed prefix, named the way a lane
// would have to go and edit it.
type lawPlace struct {
	name string
	text string
}

// TestEveryLawIsStatedOnceAndInItsOwnPlace is the gate.
func TestEveryLawIsStatedOnceAndInItsOwnPlace(t *testing.T) {
	agent := v3ShapedAgent(t)
	definitions := agent.beltDefinitions()
	if len(definitions) == 0 {
		t.Fatal("the belt is empty, so this test would pass on nothing")
	}

	places := []lawPlace{{name: "prompts/system.md (the widest page)", text: widestPage()}}
	carried := make(map[string]bool, len(definitions))
	for _, definition := range definitions {
		encoded, err := json.Marshal(definition)
		if err != nil {
			t.Fatal(err)
		}
		name := definition.Function.Name
		carried[name] = true
		places = append(places, lawPlace{name: "the `" + name + "` tool definition", text: string(encoded)})
	}

	seen := make(map[string]string, len(lawRegistry))
	for _, law := range lawRegistry {
		if was, repeated := seen[law.id]; repeated {
			t.Errorf("%s is registered twice (%q and %q): one law, one id", law.id, was, law.key)
			continue
		}
		seen[law.id] = law.key
		if strings.Contains(law.key, `"`) {
			t.Errorf("%s: its key sentence carries a double quote, which is escaped in the tool block and so matches nothing there", law.id)
			continue
		}

		// WHERE IT IS SAID, AND HOW OFTEN, counted per place so the failure can
		// name them both.
		found := make([]string, 0, 2)
		total := 0
		for _, place := range places {
			count := strings.Count(place.text, law.key)
			if count == 0 {
				continue
			}
			total += count
			if count > 1 {
				found = append(found, fmt.Sprintf("%s (%d times)", place.name, count))
				continue
			}
			found = append(found, place.name)
		}

		switch {
		case total == 0:
			t.Errorf("%s is registered as %s law and its key sentence is nowhere in the prefix: %q\n"+
				"a law that was deleted leaves its registry line behind, so either put the sentence back or delete the line with it",
				law.id, law.class, law.key)
		case total > 1:
			sort.Strings(found)
			t.Errorf("%s says the same law %d times: %q\n  in %s\n"+
				"every byte here is sent again on every request of every turn, and two copies of one rule drift apart the first time somebody edits one — keep the copy its %s class calls for and delete the other",
				law.id, total, law.key, strings.Join(found, "\n  in "), law.class)
		}

		// AND A VERB-CLASS LAW TRAVELS WITH A VERB THIS BELT ACTUALLY HAS.
		if law.class != lawVerb {
			continue
		}
		if law.tool == "" {
			t.Errorf("%s is verb-class and names no tool, so nothing says which belt it disappears with", law.id)
			continue
		}
		if !carried[law.tool] {
			t.Errorf("%s is verb-class on `%s`, which this belt does not carry: the law is prose about a verb the model cannot call, so either the tool belongs on the belt or the law belongs somewhere the model can act on it",
				law.id, law.tool)
		}
	}
}
