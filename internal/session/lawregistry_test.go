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

	"github.com/Agent-Field/codeaf/internal/manual"
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
	// at, on a unit whose class is NOT delivered in the fixed prefix, names the
	// one place that IS. A demand law lives on a manual page or in a capability
	// group's prose; an event law lives in the harness message that announces
	// the event. [lawElsewhere] resolves the spelling:
	//
	//	manual:<page>  a page of internal/manual/chat
	//	group:<name>   a capabilityGroup's prose (tools_capabilities.go)
	//	code:<symbol>  a const in this package that a message is built from
	//
	// For those classes the gate runs BACKWARDS: the key sentence must be
	// absent from the page and the tool block — that is the whole point of
	// moving it — and present where `at` says.
	at string
}

// lawElsewhere is the text behind an `at`, for the classes that are not
// delivered in the fixed prefix.
func lawElsewhere(t *testing.T, at string) string {
	t.Helper()
	kind, name, ok := strings.Cut(at, ":")
	if !ok {
		t.Fatalf("%q is not a place: it wants manual:<page>, group:<name> or code:<symbol>", at)
	}
	switch kind {
	case "manual":
		page, found := manual.Chat().Page(name)
		if !found {
			t.Fatalf("there is no chat manual page called %q", name)
		}
		return page
	case "group":
		prose := capabilityProse(name)
		if strings.TrimSpace(prose) == "" {
			t.Fatalf("the %s capability group has no prose", name)
		}
		return prose
	case "code":
		// The harness messages a law can ride on, by the symbol a lane would go
		// and edit. A name missing here is a message nobody has registered yet.
		messages := map[string]string{
			"standingNewsRule":     standingNewsRule,
			"checkpointChoiceRule": checkpointChoiceRule,
			"carriedResultsRule":   carriedResultsRule,
		}
		text, known := messages[name]
		if !known {
			t.Fatalf("code:%s is not a message this test knows; add it beside standingNewsRule", name)
		}
		return text
	}
	t.Fatalf("%q names no kind of place this test can read", at)
	return ""
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
	// AND THE KEY IS THE FAMILY FRAGMENT RATHER THAN THE WHOLE SENTENCE (lane F,
	// 2026-09-10). `jobs` said this twice in its own description and `watch`
	// said it once more; all three are deleted now on the strength of the page's
	// line, and `watch.instead-of-polling` went with them because the law it
	// filed no longer has a verb-class home. Matching on the shorter fragment is
	// what makes a re-addition anywhere in the belt fail here instead of going
	// unnoticed until the next audit.
	{id: "handoff.reports-itself", class: lawCore, key: "never sleep, tail or poll"},

	// ── the working laws that other tests pin by substring, registered here so
	// that a second copy of one is caught at the same moment as a first edit.
	{id: "batch.one-breath", class: lawCore, key: "ASK FOR EVERYTHING YOU NEED IN ONE BREATH"},
	// THE GOAL OF HANDING WORK OUT, which is the page's picture paragraph
	// (beltfacts.go's [handoffFacts]) and nowhere else: that is the one text
	// every agent that can fan out reads, the conversation included. It was
	// first written onto the fan-out page, which only a task node reads, so the
	// agent whose fan-out is uncapped never saw it.
	{id: "handoff.wall-time-goal", class: lawCore, key: "THE GOAL IS THE SHORTEST WALL TIME FOR THE WHOLE JOB"},
	{id: "verify.depth-of-checking", class: lawCore, key: "DEPTH OF CHECKING FOLLOWS THE SIZE OF WHAT YOUR ANSWER CHANGES"},
	{id: "handoff.asked-again", class: lawCore, key: "THE QUESTION IS ASKED AGAIN WHILE YOU WORK"},
	{id: "handoff.dowry", class: lawCore, key: "The brief is the dowry"},
	{id: "handoff.no-planner", class: lawCore, key: "THERE IS NO PLANNER ON YOUR BELT"},
	{id: "record.check-the-transcript", class: lawCore, key: "BEFORE RUNNING A COMMAND, CHECK THE TRANSCRIPT"},
	{id: "record.numbers-come-from-here", class: lawCore, key: "NUMBERS AND FACTS COME FROM THE CONVERSATION"},
	{id: "bash.waits", class: lawCore, key: "finishes or its armed bound"},
	{id: "files.absolute-paths", class: lawCore, key: "EVERY file you name carries its FULL ABSOLUTE PATH"},
	{id: "images.travel-with-the-message", class: lawCore, key: "ATTACHED PICTURES TRAVEL IN THE MESSAGE WITH YOU"},
	{id: "elsewhere.other-windows", class: lawCore, key: "OTHER codeaf WINDOWS ON THIS PROJECT ARE VISIBLE TO YOU"},

	// ── the routing triggers, which are the page's own and nobody else's. A
	// description says what a verb DOES; it cannot say when to reach for it
	// without teaching every model that carries the belt a rule it did not ask
	// for (docs/design/prompt-diet/DESIGN.md §4), so the trigger is core law and
	// the contract is verb law, and these three came back to the page when lane
	// F took the routing prose off the descriptions.
	{id: "tasks.look-inside", class: lawCore, key: "Look inside running or landed work with `tasks` and its id"},
	{id: "tasks.continue-is-not-a-new-task", class: lawCore, key: "never a fresh `propose_task`"},
	{id: "read.what-read-cannot-turn-into-text", class: lawCore, key: "What `read` cannot turn into text → `read_document`"},
	// ── and the two triggers for a program codeaf carries (delegate_door.go's
	// [delegateFact], the owner's call of 2026-09-24): the work a program's own
	// guide claims goes to it, and so does work the person asks one for. What
	// each program is for is its guide's; these say only that codeaf prefers it.
	{id: "program.work-it-is-for", class: lawCore, key: "AND WORK A PROGRAM BUILT INTO CODEAF IS FOR GOES TO IT WHOLE"},
	{id: "program.the-one-asked-for", class: lawCore, key: "so does work the person asks one for, by name or as `/name`."},
	// ── the mark codeaf leaves on work it did in somebody's name. It is core
	// rather than verb: `bash` is where it happens, but `bash` is pi's own
	// description and this law is codeaf's, and it is stated in ONE place for
	// both surfaces there are — the leaf loop's contract and this page read the
	// same constant (internal/exec's [exec.AttributionLaw]).
	{id: "attribution.sign-git-work", class: lawCore, key: "SIGN GIT WORK YOU DO WITH `bash`, GENTLY AND ONCE."},

	// ── and the laws the page gave up to the verb that owns them. Each of
	// these was a page sentence until the diet; the description had said it all
	// along, in more words and with the field names beside it.
	{id: "write.in-parts", class: lawVerb, tool: "write", key: "Write a very large file in parts"},
	{id: "read.offset-limit", class: lawVerb, tool: "read", key: "page the rest with offset/limit"},
	{id: "capabilities.continue-this-turn", class: lawVerb, tool: loadCapabilityToolName, key: "Full schemas arrive on the next model request; continue in this same turn"},
	{id: "task.lands-as-a-turn", class: lawVerb, tool: "propose_task", key: "starts a turn here when it lands, so never wait or poll"},
	{id: "tasks.stop-ends-work", class: lawVerb, tool: "tasks", key: "To END running work use stop"},
	{id: "tasks.not-to-wait", class: lawVerb, tool: "tasks", key: "Never to WAIT for handed-off work"},

	// ── and the laws the page gave up to whoever PULLS them. Each of these was
	// a run of prose in prompts/system.md, paid for on every request of every
	// turn, for a verb the belt was not carrying and a moment most turns never
	// reach. Each now arrives with the load that fetches those verbs, or on the
	// manual page a person asks the question on, and the gate here runs
	// backwards: it fails if one of them comes BACK onto the page.
	{id: "media.prompt-decides-quality", class: lawDemand, at: "group:media", key: "a prompt built from the genre's own clichés"},
	{id: "harness.recipe-or-program", class: lawDemand, at: "group:harnesses", key: "a saved PROGRAM rather than a recipe"},
	{id: "settings.refusal-is-theirs", class: lawDemand, at: "group:settings", key: "relay it exactly as written, and point them at `/settings`"},
	{id: "standing.background-checks", class: lawDemand, at: "manual:keeping-an-eye", key: "background checks are on out of the box, and nobody asks you first"},
	{id: "standing.a-minute-is-a-timer", class: lawDemand, at: "manual:keeping-an-eye", key: "ordinary standing one-off"},
	{id: "accounts.never-sent-twice", class: lawDemand, at: "manual:accounts", key: "Each outgoing mail or Slack message goes out **once**"},

	// ── and the one that rides with the EVENT. A fired standing item announces
	// itself and says what to do about itself, so the page explaining that
	// message was the second copy of a rule the message already carries.
	{id: "standing.news-is-not-a-request", class: lawEvent, at: "code:standingNewsRule", key: "Do not call stand again for it"},

	// ── and the three laws of a turn that has run long (inherit.go). WHAT
	// `inherit` DOES IS A FACT ABOUT ONE FIELD OF ONE VERB, so it rides that
	// verb's schema and disappears with it — the page said it too for a while,
	// and a page sentence is paid for by every request of every turn whether or
	// not the belt carries the tool it is about. What choosing between the roads
	// COSTS rides the note that announces the moment, because it is worth
	// nothing until a turn is actually long enough to be told. The third is what
	// a worker that could not be handed the transcript is handed instead, and it
	// rides that document.
	{id: "handoff.what-you-read-need-not-be-read-again", class: lawVerb, tool: "quick_task",
		key: "Opens on this conversation"},
	{id: "checkpoint.the-turn-chooses", class: lawEvent, at: "code:checkpointChoiceRule",
		key: "A worker that does not inherit opens on a brief about your work"},
	{id: "handoff.carried-results-already-happened", class: lawEvent, at: "code:carriedResultsRule",
		key: "Do not run these again to see what they said"},
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
	// AND THE PAGES ONLY A TASK NODE IS HANDED. A node's page is the widest page
	// with these laid under it (prompt.go's [renderSystemAt]), so a law restated
	// on one of them is said twice to every worker that reads it — and a law
	// moved onto one of them is a law the conversation no longer hears. Neither
	// shows up in the conversation's own page, which is why they are searched
	// here by name.
	for _, page := range []lawPlace{
		{name: "prompts/worker.md", text: workerPrompt},
		{name: "prompts/revise.md", text: revisePrompt},
		{name: "prompts/fanout.md", text: fanoutPrompt},
		{name: "prompts/divide.md", text: dividePrompt},
		{name: "prompts/quick.md", text: quickPrompt},
	} {
		places = append(places, page)
	}
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

		// A LAW THAT IS NOT DELIVERED IN THE PREFIX IS CHECKED THE OTHER WAY
		// ROUND: absent from the page and the tool block, present in the one
		// place its class delivers it from.
		if law.class == lawDemand || law.class == lawEvent {
			if law.at == "" {
				t.Errorf("%s is %s law and names no place, so nothing says where it is actually delivered", law.id, law.class)
				continue
			}
			if total > 0 {
				sort.Strings(found)
				t.Errorf("%s is %s law and its key sentence is back in the fixed prefix: %q\n  in %s\n"+
					"it is delivered from %s, and a copy on the page is the byte the diet took out — delete it there",
					law.id, law.class, law.key, strings.Join(found, "\n  in "), law.at)
			}
			if !strings.Contains(lawElsewhere(t, law.at), law.key) {
				t.Errorf("%s is registered at %s and its key sentence is not there: %q\n"+
					"a law moved out of the prefix and then edited out of its new home is a law nobody sends at all",
					law.id, law.at, law.key)
			}
			continue
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
