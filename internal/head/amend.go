package head

import (
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// The commissioning door's one question: is this a new job, or is it the person
// changing the one they are already waiting on?
//
// The bug, from a real journal (2026-08-11): "deep research 20 best stocks as a
// task" commissioned a job; forty seconds later "no you can use internet search"
// commissioned a SECOND job with an essentially identical reading. Both ran, the
// person paid twice, and the thread carried two identical commitment lines —
// while the head's own reply said "I've corrected that in my notes", which is
// the loop knowing perfectly well it was amending one intent and journaling a
// fork anyway.
//
// The prompt already forbade it ("work raised while something is running is a
// change to that work before it is a second job"). A law only the prompt keeps
// is a law the model may quietly not keep, and this one costs money and doubles
// the work every time it is broken. So it moves into the door: a follow-up that
// changes work already underway is journaled as a REVISION of that work, by the
// same call the revise tool makes, and the loop is told what happened.
//
// WHAT MAKES IT A CHANGE is reference, never resemblance. Two jobs about stocks
// are two jobs; a sentence that points at the work in flight — by deixis ("it",
// "that job"), by correcting constraints just stated ("no…", "actually…",
// "instead…"), or by naming enough of the running work's own words to rank
// against it — is one job being corrected. Resemblance alone never fires this:
// "another look at the stock list" while a stock job runs is a second ask, and
// the person is entitled to it.

// correctiveOpeners are the ways a person opens a sentence that revises what
// they just asked for. It is a list of OPENINGS rather than a bag of words
// because a correction announces itself at the front — "no you can use internet
// search" — and the same words mid-sentence are usually about the subject
// rather than about the request.
//
// It is this file's own list and not [redirectCue]'s, which is deliberate.
// redirectCue feeds a HINT: it may be generous, because the worst a wrong
// reading costs there is a line of evidence the loop ignores. This list guards a
// DOOR, and the two want different tunings — this one has to catch a bare "no"
// with no comma after it, which the hint vocabulary never needed to.
var correctiveOpeners = []string{
	"no ", "no,", "no.", "nope", "not that", "don't", "dont ", "do not ",
	"actually", "instead", "wait", "i meant", "scratch that", "forget that",
	"you can ", "you could ", "you should ", "you don't", "you dont ",
	"change it", "change that", "rather than",
	// "make it …" is deliberately absent. It is how a person opens a correction
	// ("make it warmer") and equally how they open a fresh instruction about a
	// thing that does not exist yet ("make it a table, three columns"), and this
	// list decides whether a job gets commissioned at all — so a phrase that is
	// a coin flip belongs to the prompt's judgment rather than to a door's.
}

// correctiveOpener reports that this message opens by revising something
// already asked for.
func correctiveOpener(message string) bool {
	lower := strings.ToLower(strings.TrimSpace(message))
	for _, opener := range correctiveOpeners {
		if strings.HasPrefix(lower, opener) {
			return true
		}
	}
	return false
}

// amendment is what a follow-up turned out to be: the live job it changes, or
// the live jobs that could each be the one it changes.
type amendment struct {
	target store.SurgeryTarget
	rivals []store.SurgeryTarget
}

// amendmentFor reads one message as a change to work already underway, and
// answers with what it changes.
//
// Every step is a refusal to guess. No live work of the person's is no
// amendment, whatever the words say. Words that point at nothing running are no
// amendment either — that is the guard against a new ask being swallowed. And a
// sentence that genuinely points at the work but cannot say WHICH, with several
// live, comes back as rivals rather than as a choice made on the person's
// behalf: the loop asks, which is the standard door.
func (h *Head) amendmentFor(user store.Message) (amendment, bool, error) {
	message := strings.TrimSpace(user.Body)
	if h == nil || h.store == nil || message == "" {
		return amendment{}, false, nil
	}
	cue, cued := redirectCue(message)
	// A CORRECTIVE OPENING IS REQUIRED, and deixis alone is not enough. "make
	// it warmer" and "always answer from the result, and rerun the scans" both
	// point at live work by pronoun or by name while being, respectively, a
	// judgment call and a plain new ask — and a door that swallowed either would
	// take work the person asked for and quietly turn it into an edit. What
	// fires this is the person REVISING something they just said: the cue
	// vocabulary, or one of the openings a revision announces itself with.
	//
	// Impatience is excluded from the cue half: "hurry up" changes when, not
	// what, and it has its own verb (expedite). Reading it as a revision would
	// edit a plan because somebody was waiting.
	if !(cued && cue != urgencyCue) && !correctiveOpener(message) {
		return amendment{}, false, nil
	}
	// The graph read is paid for only after the cheap reading says there is
	// something to look for.
	active, err := h.activeUserJobs()
	if err != nil || len(active) == 0 {
		return amendment{}, false, err
	}

	// Which one. The three readings run strongest first: the words themselves,
	// then what the conversation was just about, then the whole board when there
	// is only one thing on it.
	ranked, err := h.rankRedirectTargets(message, active)
	if err != nil {
		return amendment{}, false, err
	}
	strong := make([]store.SurgeryTarget, 0, len(ranked))
	for _, candidate := range ranked {
		if candidate.Score >= RedirectAnchorScore {
			strong = append(strong, candidate)
		}
	}
	if len(strong) == 1 {
		return amendment{target: strong[0]}, true, nil
	}
	if len(strong) > 1 {
		if len(strong) > RedirectCandidateLimit {
			strong = strong[:RedirectCandidateLimit]
		}
		return amendment{rivals: strong}, true, nil
	}
	if adjacent, found, err := h.adjacencyTarget(user, active); err == nil && found {
		return amendment{target: adjacent}, true, nil
	}
	if len(active) == 1 {
		// One thing running and a sentence that corrects: there is nothing else
		// it could be about. This is the case the live journal caught — a single
		// job in flight, a constraint being changed forty seconds after it was
		// commissioned, and a second identical job commissioned instead.
		return amendment{target: active[0]}, true, nil
	}
	rivals := active
	if len(rivals) > RedirectCandidateLimit {
		rivals = rivals[:RedirectCandidateLimit]
	}
	return amendment{rivals: rivals}, true, nil
}

// amendInstead is what the commissioning door does with an amendment: it
// journals the revision the person actually asked for, through the same call
// the revise tool makes, and tells the loop plainly what happened so the reply
// says the true thing.
//
// The ambiguous case hands the candidates back rather than picking, which is
// the shape every other belt verb uses for the same problem (control, rule,
// service): the loop asks one question with the rows as options.
func (run *beltRun) amendInstead(change amendment) (string, bool) {
	if len(change.rivals) > 0 {
		lines := make([]string, 0, len(change.rivals))
		for _, rival := range change.rivals {
			lines = append(lines, "- "+rival.Node.ID+" | "+surgeryTargetLabel(rival.Node))
		}
		return "that reads as a change to work already running rather than as a new job, and more than one live job could be the one they mean — " +
			"ask which, with these as the options, and commission nothing:\n" + strings.Join(lines, "\n"), false
	}
	job := change.target.Node
	seq, err := run.head.journalRevision(run.user, store.CommandRedirect, job.ID, strings.TrimSpace(run.user.Body))
	if err != nil {
		return "that could not be queued: " + err.Error(), true
	}
	label := surgeryTargetLabel(job)
	run.record(seq, "Taking that to "+label+" — I'll say what changed once it lands.")
	return "that was a change to " + label + ", which is still running, so it went there as a revision and NO second job was commissioned. " +
		"Say that it is in hand for that work; never say a new job was started. If they really meant a separate job, they will say so and spawn takes separate=true", false
}
