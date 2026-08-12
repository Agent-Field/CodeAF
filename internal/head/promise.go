package head

import (
	"context"
	"strings"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// A promise the machinery cannot keep is the affordance lying, and the law
// against it was written in the voice prompt and enforced nowhere. Live, the
// head told someone "when it finishes and the plots are written to disk, I'll
// open them for you" — three separate claims, one of them about a hand it does
// not have. Nothing looked at the reply before it was posted, so the honesty law
// held exactly as often as the model happened to obey it.
//
// This is the enforcement, and its whole subtlety is that the line MOVED. Since
// a settled job now wakes the head (absorb.go), "I'll come back to you with what
// it finds" is a promise the machinery keeps — for work actually commissioned in
// this turn, and only then. What no wake will ever make true is a promise about
// the person's own screen: nothing in the belt opens, previews or displays a
// file, and no job running headless can either. Those stay unkeepable however
// the sentence is worded.
//
// The order of repair matters. A model asked to say the same thing again without
// the promise writes a sentence; a regex cutting one out of the middle of a
// paragraph writes rubble. So the correction is a re-ask, and the deterministic
// strip is only the backstop for a model that will not take it.

// reportBackCues are the ways a reply says "I will speak again when it lands".
// Backed when this turn actually commissioned work — the delivery wakes the head
// and it does speak again — and unkeepable when it did not, because then nothing
// will ever wake.
var reportBackCues = []string{
	"let you know", "report back", "follow up with you", "keep you posted",
	"keep you updated", "ping you", "come back to you", "circle back",
	"update you when", "update you as soon as", "tell you as soon as",
	"message you when", "notify you",
}

// handCues are promises about the person's own screen or machine. No tool on
// the belt does any of these and no headless job can do them either, so they are
// unkeepable whatever else the turn did.
var handCues = []string{
	"i'll open", "i will open", "ill open", "open them for you", "open it for you",
	"open the file for you", "i'll preview", "i will preview", "i'll pull up",
	"i will pull up", "i'll bring up", "i will bring up", "i'll display",
	"i will display", "i'll watch", "i will watch", "i'll keep an eye",
	"i will keep an eye", "i'll check back", "i will check back",
	"i'll email you", "i will email you", "i'll text you", "i will text you",
	"i'll message you on", "i will message you on",
}

// dispatchCues are the ways a reply says "this is now under way" — a receipt
// for work. They are the second failure and a worse one than the first, because
// a promise merely goes unkept while a receipt is false the moment it is read:
// the head diagnosed a broken PDF, ran out of belt before it could commission
// the repair, and wrote "I've commissioned a fix … the corrected PDF will land
// here when it's done". Nothing was in hand and nothing was coming. The person
// waited on a conversation that believed work existed.
//
// They are unkeepable only when this turn neither commissioned nor changed
// anything, because that is exactly when no tool can have produced the receipt.
// A turn that steered, revised or corrected something has a real act to describe
// and is left alone.
var dispatchCues = []string{
	"i've commissioned", "i have commissioned", "ive commissioned", "commissioned a",
	"i've kicked off", "i have kicked off", "i've started", "i have started",
	"i've put", "i have put", "i've queued", "i have queued", "i've set the",
	"i've asked", "i have asked", "i've handed", "i have handed",
	"is in hand", "are in hand", "the fix is in", "is under way", "is underway",
	"is being reworked", "is being rebuilt", "is being regenerated", "is being fixed",
	"will land here", "will land in this", "will be regenerated", "will arrive here",
	"is on its way", "work is going on", "in progress now",
}

// unkeepablePromise returns the first sentence of a reply that promises
// something this head cannot do — or claims something it did not do — and ""
// when every claim in it is backed.
//
// commissioned is "this turn turned words into work"; acted is the wider "this
// turn changed something". The first backs a promise to come back with what the
// work finds, because only commissioned work wakes the head. The second backs a
// receipt, because steering and correcting are dispatches too.
func unkeepablePromise(reply string, commissioned, acted bool) string {
	for _, sentence := range replySentences(reply) {
		lowered := strings.ToLower(sentence)
		for _, cue := range handCues {
			if strings.Contains(lowered, cue) {
				return sentence
			}
		}
		if !commissioned && !acted {
			for _, cue := range dispatchCues {
				if strings.Contains(lowered, cue) {
					return sentence
				}
			}
		}
		if commissioned {
			continue
		}
		for _, cue := range reportBackCues {
			if strings.Contains(lowered, cue) {
				return sentence
			}
		}
	}
	return ""
}

// promiseCorrection is what the model is asked when one is found. It states the
// fact the reply got wrong rather than the words to use: the sentence is the
// model's to write, and a canned replacement would be a fourth voice in the
// thread.
const promiseCorrection = `That reply claims something that is not true of this turn: %s

You have no way to open, preview, display or run anything on their screen, no way to watch something and speak later about it, and no channel outside this conversation. Work you commission does wake you when it lands, so coming back with what it FINDS is the one thing of this kind you may say — and only about work you have actually put in hand this turn.

And nothing is under way because you meant to start it. A tool result in front of you is the only thing that makes work exist; without one, there is no fix in hand, nothing being reworked, and nothing that will land here. What is true is what you found, and that the work still needs starting.

Say the same thing again, in the same voice and the same length, with that claim replaced by what is actually true. Reply with the message only.`

// keepable enforces the promise law over one turn's words. It costs one extra
// call, and only on a reply that made a promise — which is rare, and is exactly
// the moment a reply is worth another look.
func (h *Head) keepable(ctx context.Context, client Client, messages []ai.Message,
	reply string, commissioned, acted bool) string {
	offending := unkeepablePromise(reply, commissioned, acted)
	if offending == "" || client == nil {
		return reply
	}
	corrected := ""
	asked := append(append([]ai.Message(nil), messages...),
		ai.Message{Role: "assistant", Content: []ai.ContentPart{{Type: "text", Text: reply}}},
		textMessage("user", strings.Replace(promiseCorrection, "%s", offending, 1)))
	if response, err := client.CompleteWithMessages(ctx, asked,
		ai.WithMaxTokens(orchestratorMaxTokens)); err == nil && response != nil {
		corrected = strings.TrimSpace(response.Text())
	}
	if corrected != "" && unkeepablePromise(corrected, commissioned, acted) == "" {
		return corrected
	}
	// The model would not take the correction. Cut the sentence rather than post
	// the claim: a reply one sentence shorter is worse writing, and a promise
	// nobody can keep is worse than bad writing. A reply that was NOTHING but
	// the promise is left standing, because going silent is the one outcome no
	// route may produce and the floor's own words would be a stranger reply
	// than the one they are reading.
	if stripped := stripSentences(reply, commissioned, acted); stripped != "" {
		return stripped
	}
	// Everything in it was a claim. Rather than post the lot or post nothing,
	// drop back to the hardest class alone: a reply that still overstates what
	// was commissioned is bad, and one that promises to open a file on their
	// screen is the promise this whole file exists for. Ranking them is the only
	// choice left when there is no sentence without a claim in it.
	if stripped := stripSentences(reply, true, true); stripped != "" {
		return stripped
	}
	return reply
}

// stripSentences drops every sentence carrying an unkeepable promise, keeping
// the paragraph breaks around what is left.
func stripSentences(reply string, commissioned, acted bool) string {
	kept := make([]string, 0, 8)
	for _, sentence := range replySentences(reply) {
		if unkeepablePromise(sentence, commissioned, acted) == "" {
			kept = append(kept, sentence)
		}
	}
	return strings.TrimSpace(strings.Join(kept, " "))
}

// replySentences splits a reply into the units a promise lives in. Line breaks
// end a sentence as surely as a full stop does — a bulleted promise is one
// bullet — and the terminator stays on the sentence so rejoining reads as
// English.
//
// A full stop only ends a sentence when whitespace or the end of the reply
// follows it, which is what keeps a file name whole: "series.csv" is one word
// and cutting it in half would leave a reply naming a file that does not exist.
func replySentences(reply string) []string {
	sentences := make([]string, 0, 8)
	runes := []rune(reply)
	start := 0
	flush := func(end int) {
		if trimmed := strings.TrimSpace(string(runes[start:end])); trimmed != "" {
			sentences = append(sentences, trimmed)
		}
		start = end
	}
	for index, r := range runes {
		switch r {
		case '\n':
			flush(index)
			start = index + 1
		case '.', '!', '?':
			if index+1 == len(runes) || runes[index+1] == ' ' ||
				runes[index+1] == '\n' || runes[index+1] == '\t' {
				flush(index + 1)
			}
		}
	}
	flush(len(runes))
	return sentences
}
