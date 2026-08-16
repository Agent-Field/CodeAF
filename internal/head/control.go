package head

import (
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/manual"
)

// What is left of the control loop.
//
// The loop itself is gone — there is one loop now, and it runs on every message
// rather than behind a keyword trigger (loop.go). What survives here is the
// vocabulary the trigger was made of, and it survives because it turned out to
// be worth something other than what it was built for: these tables are a cheap,
// instant reading of what a sentence is ABOUT, and a reading is exactly what a
// prompt can carry as evidence. hints.go renders them. Nothing in this file can
// answer a message, open a loop, or stop anything reaching the tools.
//
// The trigger's own arms — controlLoopApplies and the two anchor readers — are
// deleted rather than demoted. Their whole job was deciding whether the tools
// were worth opening, and that question no longer exists.

// controlVerbs are the words that make a sentence plausibly about existing
// work. This list is deliberately not a grammar and never grows into one: it is
// a cheap trigger, not a reading. A false positive costs one model call that
// returns the sentinel; a false negative costs nothing but today's behaviour.
var controlVerbs = map[string]bool{
	"cancel": true, "cancelled": true, "stop": true, "stopped": true, "halt": true,
	"kill": true, "abort": true, "abandon": true, "scrap": true, "ditch": true,
	"pause": true, "paused": true, "hold": true, "freeze": true, "suspend": true,
	"resume": true, "unpause": true, "unfreeze": true, "continue": true,
	"restart": true, "retry": true, "rerun": true, "redo": true, "again": true,
	"skip": true, "drop": true, "keep": true, "leave": true, "except": true,
	"finish": true, "complete": true, "wrap": true, "hurry": true, "rush": true,
	"wait": true, "until": true, "first": true, "prioritize": true,
	"reprioritize": true, "deprioritize": true, "cheaper": true, "faster": true,
}

func controlVerbPresent(message string) bool {
	for _, word := range surgeryWords(strings.ToLower(message)) {
		if controlVerbs[word] {
			return true
		}
	}
	return false
}

// controlStatusCues are the words a person uses to ask where something is up
// to. They are not verbs and they were the hole: "hows it going, what's the
// plan?" typed over three live multi-part jobs carries no control verb, points
// at no single job by pronoun, borrows none of a job's words and follows no
// worker's post — so every arm of the trigger declined, and the router answered
// a question about structure from a board it reads as two counts.
//
// Like controlVerbs this is a cheap trigger and never a grammar. It only opens
// the loop while something is live, because with an empty board these words are
// ordinary conversation and the reads have nothing to be about.
var controlStatusCues = map[string]bool{
	"plan": true, "plans": true, "planned": true, "dag": true,
	"progress": true, "status": true, "eta": true, "remaining": true,
	"outstanding": true, "blocked": true, "blocking": true, "stuck": true,
	"slowest": true, "steps": true, "breakdown": true,
}

// controlStatusAsks are the weaker half: words that mean "where is this up to"
// only inside a question. "going" is a status word in "how's it going" and a
// travel plan in "I'm going out", and the question mark — or the word that
// stands in for one when nobody types it — is the whole difference.
var controlStatusAsks = map[string]bool{
	"going": true, "doing": true, "happening": true, "underway": true,
	"along": true, "far": true, "left": true, "waiting": true, "stage": true,
}

func controlStatusCued(message string) bool {
	lower := strings.ToLower(message)
	words := surgeryWords(lower)
	asked := strings.Contains(lower, "?")
	weak := false
	for _, word := range words {
		if controlStatusCues[word] {
			return true
		}
		if selfQuestionLeads[word] {
			asked = true
		}
		if controlStatusAsks[word] {
			weak = true
		}
	}
	return weak && asked
}

// The second arm. Everything above is about work; this is about aforge. A
// person learning what their employee can do asks in the same register they ask
// for work in — "can you look at images?", "what happens overnight?" — and the
// only reliable difference is that one points at aforge and the other points at
// a deliverable. So the trigger reads shape rather than topic: a question, aimed
// at aforge or at something the manual is titled after, and not carrying a verb
// that means "go do this". A false negative costs nothing but today's routing,
// which is why every clause here is a reason NOT to open.

// selfQuestionPhrases are self-questions said in full, in the idiom
// asksForCompetence and asksForStandingWatch already use. They skip the shape
// test because they are already unambiguous.
var selfQuestionPhrases = []string{
	"what can you do", "what do you do", "what are you", "who are you",
	"how do you work", "how does aforge work", "what is aforge",
	"what happens when i'm gone", "what happens when i am gone",
	"while i'm gone", "while i am gone", "what happens overnight",
	"what happens every day", "what do you do all day",
	"explain yourself", "tell me about yourself",
}

// selfQuestionLeads mark a sentence as a question even without a question mark,
// which is how most people type one.
//
// "hows" is here for the same reason "whats" is: the apostrophe is optional in
// typing and "how's" already reduces to "how" when the words are split, while
// "hows" reduced to nothing anybody had written down.
var selfQuestionLeads = map[string]bool{
	"how": true, "hows": true, "what": true, "whats": true, "why": true, "when": true,
	"where": true, "which": true, "who": true, "can": true, "could": true,
	"does": true, "do": true, "did": true, "is": true, "are": true,
	"explain": true, "tell": true,
}

// selfReferenceWords are the ways a person names their employee.
var selfReferenceWords = map[string]bool{
	"you": true, "your": true, "yours": true, "yourself": true, "aforge": true,
}

// selfQuestionVetoes are the verbs that mean the sentence is an assignment,
// however question-shaped it is. "can you build me a parser" is work, and work
// takes the ordinary route.
var selfQuestionVetoes = map[string]bool{
	"build": true, "write": true, "create": true, "draft": true, "fix": true,
	"implement": true, "add": true, "generate": true, "summarize": true,
	"summarise": true, "research": true, "find": true, "search": true,
	"download": true, "install": true, "send": true, "email": true,
	"deploy": true, "refactor": true, "translate": true, "buy": true,
	"publish": true, "compile": true, "analyze": true, "analyse": true,
	"review": true, "check": true, "look": true, "read": true, "scrape": true,
}

// selfQuestionPhrased is the unambiguous half, and the only half any earlier
// recognizer is allowed to consult. "what happens every day while I'm gone"
// carries durable-sounding words without being an instruction, and standing
// intent has to decline it before the loop ever gets a turn.
func selfQuestionPhrased(lower string) bool {
	for _, phrase := range selfQuestionPhrases {
		if strings.Contains(lower, phrase) {
			return true
		}
	}
	return false
}

// selfQuestionCued is the trigger. It opens the loop with the manual in reach
// even when nothing at all is live.
func selfQuestionCued(message string) bool {
	lower := strings.ToLower(strings.TrimSpace(message))
	if lower == "" {
		return false
	}
	if selfQuestionPhrased(lower) {
		return true
	}
	words := surgeryWords(lower)
	shaped := strings.Contains(lower, "?")
	referenced := false
	for _, word := range words {
		if selfQuestionVetoes[word] {
			return false
		}
		if selfQuestionLeads[word] {
			shaped = true
		}
		if selfReferenceWords[word] {
			referenced = true
		}
	}
	if !shaped {
		return false
	}
	return referenced || manual.Cued(lower)
}
