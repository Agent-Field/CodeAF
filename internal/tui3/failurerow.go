package tui3

import (
	"strconv"
	"strings"
)

// ── A REQUEST THAT FAILED IS A ROW ───────────────────────────────────────────
//
// THE FAILURE STORY IS ONE STORY, TOLD ONCE, WHEREVER WORK IS DRAWN. A provider
// that says no, a stream that is cut, a model the ladder moves off, a ladder
// that runs out — those are four moments in the same account of one request, and
// until this file each of them was spelled by whichever surface happened to see
// it. The measured defect (2026-09-10, conversation 57d51779f63ac603) is what
// that costs: the engine failed a follow-up request four times over ninety
// seconds and the only trace anywhere on the screen was the word "trying again"
// on the status line, which is gone the moment the line is redrawn. Two minutes
// later the person was reading a step that still claimed to be running, with
// nothing under it, and said "things are just stuck".
//
// So the words live HERE and nowhere else, and every surface that draws work
// composes its row through [failureRow]: the conversation ([feed.retry] and the
// error arm of [app.applyEvent]), a node's page (room.go's [app.roomEvent]) and
// the status line's own short form ([app.waitingWords]). One story in three
// places rather than three stories.
//
// IT IS THE PERSON'S VOCABULARY AND NOT THE MACHINERY'S. "transport", "verdict",
// "the ladder", "retry budget" and "attempt N/M" are all words this program uses
// about itself and none of them is a word anybody waiting for a reply would
// reach for. What a person wants to know is: what went wrong, is anything still
// being done about it, and — when nothing is — that it has stopped.

// failure is ONE THING THAT WENT WRONG WITH ONE REQUEST, in the fields every
// surface that draws work has an answer for.
//
// EVERY FIELD IS ALLOWED TO BE EMPTY, and an empty one draws nothing rather than
// an empty slot — the emptiness law, which matters more here than almost
// anywhere else: a failure is exactly the moment a surface knows least, and a
// row that padded out what it does not know ("moving to " with no model after
// it, "0 of 0") would be the screen inventing detail about the one event the
// person is trying to read carefully.
type failure struct {
	// reason is WHAT WENT WRONG, in whatever words the layer that saw it used:
	// the engine's notice for a cut or a hop, the provider's own sentence for a
	// refusal. It is drawn as written and never re-worded, because the surface
	// does not know better than the layer that was there.
	reason string
	// told is the WHOLE SENTENCE a layer that already saw this failure composed
	// about it — what happened and what is being done, in one line — and it
	// STANDS IN PLACE OF the words this file would otherwise supply.
	//
	// IT EXISTS BECAUSE THE ENGINE ALREADY WRITES ONE. Every retry notice on the
	// wire today is a finished sentence in exactly this surface's register: `the
	// reply was cut short — asking again`, `the model went quiet mid-reply —
	// asking again`, `the reply kept losing its thread — finishing this one on
	// kimi-k3` (internal/session's loop.go). A composer that appended its own
	// `asking again` to those would say it twice, and one that re-worded them
	// would be second-guessing the layer that was actually there. So a sentence
	// that arrives whole is drawn whole, and only the arithmetic is added to it.
	told string
	// next is the model the ladder is moving to, drawn by its BASENAME the way
	// every other row on this surface spells a model: the vendor is routing.
	//
	// THE MODEL BEING LEFT IS NOT A FIELD, and the omission is render.go's law
	// about the same moment ([retryWord]): a retry that named the model it is
	// still asking would suggest the second attempt went somewhere else, and the
	// name was already on the line that was cut. So the engine's own
	// `RetryNews.Model` has no home on this row, deliberately.
	next string
	// attempt is which try this is, 1-based, and attempts how many there are in
	// all. BOTH OR NEITHER: an ordinal with no total ("try 3" out of who knows
	// how many) reads as an alarm rather than as progress, so the arithmetic is
	// drawn only when the surface holds both halves of it.
	attempt, attempts int
	// gaveUp says the tries are over and nothing further is being done, which is
	// the one fact on this row a person cannot infer from the others.
	gaveUp bool
	// tries is how many were spent in all, for a give-up. It is separate from
	// [failure.attempts] because a ladder that ran out and a ladder that is
	// half-way through are counted from different places — the engine states the
	// budget, and the surface can count only what it watched happen.
	tries int
}

// The words themselves. They are constants because the manual quotes them and
// the tmux suite waits for them, and a sentence spelled at its call site is a
// sentence those two find by luck.
const (
	// askingAgainWord is what a retry says about itself. It is present tense and
	// it is a promise: something is being done, and the person does not have to.
	askingAgainWord = "asking again"
	// movingToWord opens the hop. The model that comes after it is the one the
	// rest of the reply will be written by, which is worth a row of its own —
	// the voice is about to change under somebody who is reading.
	movingToWord = "moving to "
	// gaveUpWord and gaveUpTriesWord are the END of the story, and the row that
	// was missing entirely. "gave up" is the plainest true sentence available:
	// not "failed", which says nothing about whether anything is still happening,
	// and not "error", which is a category rather than an outcome.
	gaveUpWord      = "gave up"
	gaveUpTriesWord = " after "
	// errorNoteWord opens the line for an error that ended a turn NOBODY tried
	// again for, and it is the sentence this surface has always written. It is a
	// constant beside the others because the two lines are one decision made in
	// one place ([feed.failureNote]).
	errorNoteWord = "error: "
)

// failureRow is THE ONE COMPOSER. Every surface's failure row is this string.
//
// The shapes, in the order the fields decide them:
//
//	the reply was cut short — asking again         a sentence that arrived whole
//	the reply was cut short — asking again · 2 of 4    …once it knows the budget
//	nobody answered in time · asking again · 2 of 4    composed from the fields
//	nobody answered in time · moving to kimi-k3        a hop off a model
//	gave up after 4 tries · API error (429)            the ladder spent
//	gave up · nobody answered in time                  a give-up nobody counted
//
// The reason comes FIRST on a retry and LAST on a give-up, and the asymmetry is
// deliberate: while something is still being done the news is what went wrong,
// and once nothing is, the news is that it has stopped.
func failureRow(f failure) string {
	var parts []string
	reason := strings.TrimSpace(f.reason)
	if f.gaveUp {
		word := gaveUpWord
		if reason == "" {
			reason = strings.TrimSpace(f.told)
		}
		if f.tries > 1 {
			word += gaveUpTriesWord + strconv.Itoa(f.tries) + " " + failureTriesWord(f.tries)
		}
		parts = append(parts, word)
		if reason != "" {
			parts = append(parts, reason)
		}
		return strings.Join(parts, partDot)
	}
	if told := strings.TrimSpace(f.told); told != "" {
		parts = append(parts, told)
	} else {
		if reason != "" {
			parts = append(parts, reason)
		}
		if next := modelBase(f.next); next != "" {
			parts = append(parts, movingToWord+next)
		} else {
			parts = append(parts, askingAgainWord)
		}
	}
	// AND THE ARITHMETIC RIDES LAST, where a person reading the sentence has
	// already been told what happened and what is being done about it. It is the
	// same spelling the phase line uses for the same fact (internal/provider's
	// phase.go, `2 of 4`), so the two rows about one wait cannot drift.
	if word := failureCountWord(f); word != "" {
		parts = append(parts, word)
	}
	return strings.Join(parts, partDot)
}

// failureCountWord is `2 of 4`, and "" whenever either half is missing — see
// [failure.attempt] for why it is both or neither.
func failureCountWord(f failure) string {
	if f.attempt <= 0 || f.attempts <= 0 || f.attempt > f.attempts {
		return ""
	}
	return strconv.Itoa(f.attempt) + " of " + strconv.Itoa(f.attempts)
}

func failureTriesWord(n int) string {
	if n == 1 {
		return "try"
	}
	return "tries"
}

// failureDetail is the SHORT form of the same struct, for a row that has already
// said what it is about: the status line writes "trying again" out of its own
// vocabulary (render.go's [retryWord]) and then hangs this on the end of it.
//
// It is the same function reading the same fields, which is the whole point —
// the status line used to be the ONLY place a retry appeared and it said nothing
// but the two words, so a person who looked away missed the entire event.
func failureDetail(f failure) string {
	if next := modelBase(f.next); next != "" {
		return movingToWord + next
	}
	return failureCountWord(f)
}

// ── WHERE THE STRUCT IS FILLED IN ────────────────────────────────────────────
//
// THIS IS THE SEAM, AND IT IS ONE FUNCTION WIDE ON PURPOSE.
//
// Today [session.EventRetrying] carries a sentence and nothing else: the engine
// composes `the request failed — asking again`, `hopNotice`, `cutNotice` and
// hands over the words (internal/session's loop.go). So the surface fills in
// what it can see — the sentence as the reason, and the tries it has watched go
// past — and leaves the arithmetic empty, which draws as nothing.
//
// The engine is growing the fields: `Event.Retry *RetryNews{Model, Attempt,
// Attempts int, Reason, Next string}` is in flight beside this change. WHEN IT
// LANDS, THE ONLY EDIT IS IN HERE — [retryFailure] reads ev.Retry instead of
// ev.Text, the ordinals stop being empty, and every row in every surface gains
// `· 2 of 4` and `· moving to <model>` without another line moving anywhere.
// Nothing else in this package reads the event's shape.
func retryFailure(text string, seen int) failure {
	return failure{
		// The engine's sentence is finished prose and is carried as such — see
		// [failure.told] for why appending to it would say the same thing twice.
		told: strings.TrimSpace(text),
		// seen is what this surface has WATCHED, and it is carried even though
		// nothing draws it yet: it is what the give-up below counts, and it is
		// the field the engine's own attempt number replaces.
		attempt: seen + 1,
	}
}

// gaveUpFailure is the end of the ladder, built from the engine's error and from
// how many tries this surface watched go past ([feed.retries]).
//
// THE COUNT IS THE SURFACE'S OWN and it is honest about being that: it counts
// the retries it drew, so a page attached half-way through a turn counts from
// where it attached. The engine's own figure is inside its sentence — `after 3
// retries: …` — and that prefix is taken OFF here rather than drawn, because the
// row is about to say the same thing in the person's words and a line that says
// it twice, once in each vocabulary, is the machinery leaking.
func gaveUpFailure(text string, seen int) failure {
	return failure{reason: strings.TrimSpace(stripRetryPrefix(text)), gaveUp: true, tries: seen + 1}
}

// retryPrefixWord is the engine's own count, spelled its way, at the head of the
// error a spent ladder ends on (internal/session's loop.go: `after %d retries:
// %w`). It is matched rather than parsed for a number: what is wanted is the
// SENTENCE AFTER IT, and a prefix this surface does not recognise is left alone.
const retryPrefixWord = "after "

func stripRetryPrefix(text string) string {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, retryPrefixWord) {
		return text
	}
	head, rest, ok := strings.Cut(text, ": ")
	if !ok || !strings.HasSuffix(head, " retries") {
		return text
	}
	if rest = strings.TrimSpace(rest); rest != "" {
		return rest
	}
	return text
}
