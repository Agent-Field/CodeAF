package head

import (
	"strings"
	"unicode"
)

// Impatience is a fifth way a person changes work already underway, and the
// only one that is not about what the work is. "do it faster" asks for the
// same deliverable sooner; compiling it as new work does the exact opposite —
// it puts a second job in the queue and makes the wait longer. So urgency
// rides redirection's anchor and spends itself on the job already running.
//
// Urgency never asks a disambiguating question. A question costs the one thing
// the user just said they are out of, and every action urgency takes is cheap
// and reversible — claim order, a line in a worker's transcript, a trim of
// unstarted steps. The receipt names the job it pressed so a wrong guess costs
// one word to correct.

// urgencyCue is the fifth cue class's name, shared by the recognizer and the
// one branch that reads it.
const urgencyCue = "urgency"

// urgencyPhrases are impatience said in full. Each one is about when the answer
// arrives, never about what is in it.
var urgencyPhrases = []string{
	"asap", "as soon as possible", "right now", "right away", "straight away",
	"hurry up", "in a hurry", "speed it up", "speed this up", "wrap it up",
	"wrap this up", "just finish", "finish it now", "no more delay",
	"immediately", "immediatly",
	"give me the result", "give me the results", "give me result",
	"give me results", "give me the answer", "give me an answer",
	"give me what you have", "what you have so far",
	"i need it now", "i want it now", "need this now", "need it now",
	"taking too long", "taking so long",
}

// urgencyWords are impatience in one word. The same words describe a
// deliverable ("a fast parser"), so they count only where nothing marks them as
// a property of the thing being built.
var urgencyWords = map[string]bool{
	"fast": true, "faster": true, "quickly": true, "quicker": true,
	"hurry": true, "asap": true, "immediately": true, "urgent": true,
	"urgently": true, "sooner": true,
}

// adjectivalLead is the word before an urgency word that turns it into a
// property of a noun. "write a fast parser" is work; "finish it fast" is not.
var adjectivalLead = map[string]bool{
	"a": true, "an": true, "more": true, "very": true, "really": true,
	"super": true, "is": true, "are": true, "be": true, "been": true,
	"being": true, "was": true, "blazing": true, "blazingly": true,
	"extremely": true, "reasonably": true, "ultra": true, "how": true,
}

// buildVerbs turn speed into a specification rather than a schedule: "make the
// research code faster" asks for a different deliverable, not an earlier one.
var buildVerbs = map[string]bool{
	"make": true, "makes": true, "making": true, "made": true,
	"run": true, "runs": true, "running": true, "load": true, "loads": true,
	"render": true, "renders": true, "perform": true, "performs": true,
}

// buildVerbReach is how far back an urgency word looks for the verb that would
// make it a property. Six words covers "make the finance research code faster"
// — a full object noun phrase — without reaching into a previous clause.
const buildVerbReach = 6

// urgencyCued reads a sentence as pressure on delivery. It is only half the
// recognition: without redirection's anchor to a live user job, an urgent-
// sounding sentence is ordinary new work and takes the ordinary path.
func urgencyCued(lower string) bool {
	for _, phrase := range urgencyPhrases {
		if strings.Contains(lower, phrase) {
			return true
		}
	}
	words := strings.FieldsFunc(lower, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	})
	for index, word := range words {
		if !urgencyWords[word] || describesTheDeliverable(words, index) {
			continue
		}
		return true
	}
	// "how long" on its own is a status question, and the honest answer to a
	// status question is a read of the snapshot — never an acceleration. It
	// counts as impatience only beside a second sign of pressure.
	return strings.Contains(lower, "how long") &&
		(strings.Contains(lower, "still") || strings.Contains(lower, "already") ||
			strings.Contains(lower, "!"))
}

// describesTheDeliverable decides that one urgency word is about the thing
// being built rather than about when it arrives. Both readings are common and
// only one of them is safe to act on, so the doubtful case is always the
// deliverable — that reading takes the ordinary path and costs nothing.
func describesTheDeliverable(words []string, index int) bool {
	if index > 0 && adjectivalLead[words[index-1]] {
		return true
	}
	start := index - buildVerbReach
	if start < 0 {
		start = 0
	}
	for _, word := range words[start:index] {
		if buildVerbs[word] {
			return true
		}
	}
	return false
}

// impatientCorrection is the shape the live failure took: "not just a summary,
// I want the answer". It is a correction — the user is saying what the work is
// for — but none of correction's vocabulary appears in it.
func impatientCorrection(lower string) bool {
	pivots := []string{
		"not just", "not jsut", "not only", "not a ", "not an ", "not the ",
		"i don't want", "i dont want", "i didn't ask", "i didnt ask",
		"i did not ask", "that's not what", "thats not what",
	}
	pivoted := false
	for _, pivot := range pivots {
		if strings.HasPrefix(lower, pivot) || strings.Contains(lower, " "+pivot) {
			pivoted = true
			break
		}
	}
	if !pivoted {
		return false
	}
	for _, want := range []string{
		"i want", "i need", "want the", "want a", "want an", "need the",
		"need a", "need an", "give me", "i asked for", "i'm after", "im after",
	} {
		if strings.Contains(lower, want) {
			return true
		}
	}
	return false
}
