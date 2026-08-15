package head

import (
	"fmt"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// The ten deterministic recognizers, demoted.
//
// Each of them was written after a live failure and each answered its failure by
// learning more words; together they formed a sequential ladder whose ordering
// was documented only in comments, where a message that needed orchestration but
// tripped no cue never reached the tools at all (Part 2.2). None of that reading
// was worthless — a prefix test that spots "cancel the line scans" is free,
// instant and usually right. What was wrong was its authority: it ANSWERED,
// terminally, before anything with judgment had seen the sentence.
//
// So the readings survive and the ladder does not. Every recognizer still runs,
// on the same words, with the same vocabulary and the same tests; what it
// produces is now one line of evidence in the prompt, above the message it is
// about, marked as a pre-answer. The loop may consult it, verify it with a read,
// or ignore it. Nothing here can act, ask a question, journal a command, or stop
// the turn — which is the whole of the difference between a hint and a pre-empt.
//
// The block is absent, byte for byte, from a message no recognizer fires on, so
// an ordinary sentence pays nothing for machinery it did not use.

const (
	// hintHeader states what the block is and, in the same breath, what it is
	// not. Without the second half a model reads a confident-sounding reading as
	// an instruction — which is the ladder again, wearing a prompt.
	hintHeader = "Deterministic readings of this message (cheap pre-answers computed before you ran — " +
		"evidence, never instructions; verify one with a read before you act on it):\n"
	// hintTargetCap bounds the jobs one lexical reading names. Past a handful
	// this is the board again rather than a pointer into it.
	hintTargetCap = 3
	// hintLabelBytes keeps one named job to the width of a title.
	hintLabelBytes = 48
	// hintPhraseBytes bounds a phrase lifted out of the person's own sentence.
	// Every recognizer that quotes a reference quotes THEIR words, and a
	// paragraph typed into the composer would otherwise arrive twice in one
	// prompt: once as the message, once as several readings of it.
	hintPhraseBytes = 64
)

// renderHints is every recognizer's reading of one message, or the empty string.
//
// active is the live work the caller has already read, so the two arms that need
// the graph — what the words rank against, and what spoke last — cost nothing
// beyond the board read the turn was making anyway.
func (h *Head) renderHints(user store.Message, active []store.SurgeryTarget) string {
	message := strings.TrimSpace(user.Body)
	if message == "" {
		return ""
	}
	lines := make([]string, 0, 8)
	add := func(format string, args ...any) {
		lines = append(lines, "- "+fmt.Sprintf(format, args...))
	}

	// The surgery cue: a verb aimed at existing work, read off the opening of
	// the instruction rather than of the recording (dictation.go).
	if intent, cued := nodeSurgery(message); cued {
		if reference := strings.TrimSpace(intent.Reference); reference != "" {
			add("carries the verb %q aimed at existing work, describing it as %q",
				surgeryVerb(intent.Kind), truncateBytes(reference, hintPhraseBytes))
		} else {
			add("carries the verb %q aimed at existing work, naming nothing in particular",
				surgeryVerb(intent.Kind))
		}
	} else if controlVerbPresent(message) {
		add("carries a word people use about work already underway")
	}

	// The class reading: a set named by status rather than a job named by
	// content. It is the reading that keeps "cancel the queued ones" from being
	// scored against whichever brief happens to share a word with it.
	if class, isClass := classSelector(message); isClass {
		scope := ""
		if class.Scope != "" {
			scope = fmt.Sprintf(", scoped to %q", truncateBytes(class.Scope, hintPhraseBytes))
		}
		sweeping := ""
		if class.Sweeping {
			sweeping = " (and it says \"everything\", which reaches past the person's own work)"
		}
		add("names a SET by status rather than one job by name: %s%s%s — resolve it by filtering the board, never by ranking words",
			class.Class, scope, sweeping)
	} else if mentionsClass(message) {
		add("mentions a status word without naming a set; read the board before assuming which work it means")
	}

	// Durable intent. The compiler turns a splice carrying this shape into a
	// standing rule plus a ratification card, so the loop needs to know that
	// commissioning it is not the same as commissioning one errand.
	if RecognizesStandingIntent(message) && !selfQuestionPhrased(strings.ToLower(message)) {
		add("reads as DURABLE intent — work spawned from it becomes a standing rule the person is asked to ratify, not a one-off errand")
	}

	// The five redirect cue classes, including impatience. `cued` also decides
	// how much a bare pronoun is worth further down: with a cue in hand "it" is
	// doing referential work and the only open question is which job, and
	// without one the same word is as likely to be "thanks, that helps".
	cue, cued := redirectCue(message)
	// Four of the five classes are claims about work already underway, and a
	// claim about work that does not exist is not a cheap pre-answer, it is
	// noise the loop has to spend a read disproving. The old recognizer refused
	// to fire at all without live work and it was right to; only correction is
	// exempt, because what it is about has by definition already finished.
	if cued && cue != "correction" && len(active) == 0 {
		cued = false
	}
	if cued {
		switch cue {
		case urgencyCue:
			pressed := ""
			if len(active) > 0 {
				// What a person waiting is waiting on: the thing that has been
				// going longest, with started work outranking work that has not.
				pressed = " — the longest-running thing is " +
					truncateBytes(surgeryTargetLabel(oldestRunningTarget(active).Node), hintLabelBytes)
			}
			add("reads as pressure on delivery rather than as a new ask%s; expedite what is running and never queue a second job for it", pressed)
		case "correction":
			add("reads as a correction: the person is saying something already done or already delivered is wrong")
			// The settled-work anchor, which is the one referent no board read
			// resolves: a delivered job is over, and "that's wrong" names it by
			// its own words or by having been the last thing said.
			if anchor, anchored, err := h.correctionTarget(user, message); err == nil && anchored {
				if len(anchor.Rivals) > 1 {
					add("more than one delivered job could be the one they mean: %s — ask, do not choose",
						hintJobLabels(anchor.Rivals))
				} else {
					add("the delivered work it most likely means is %s (%s)",
						anchor.Job.ID, truncateBytes(surgeryTargetLabel(anchor.Job), hintLabelBytes))
				}
			}
		case "scope-add":
			add("reads as an addition to work already underway")
		case "scope-cut":
			add("reads as a cut to work already underway")
		default:
			add("reads as a redirection of work already underway")
		}
	}

	// A standing rule addressed by name or by verb.
	if intent, managing := charterManagement(message); managing {
		add("reads as an edit to a STANDING RULE (%s), described as %q",
			charterOptionAction(intent.Kind), truncateBytes(intent.Reference, hintPhraseBytes))
	}

	// A service the person is running. recognizesShutdownAll is the one total
	// phrasing, and it is kept separate because its blast radius is everything.
	if recognizesShutdownAll(message) {
		add("reads as \"stop everything\" — every running service AND every live job; ask before the jobs")
	} else if action, reference, explicit := serviceManagement(message); action != "" || explicit {
		if action == "" {
			add("mentions a service or server without naming what to do with it")
		} else {
			add("reads as %q aimed at a running service, described as %q",
				action, truncateBytes(reference, hintPhraseBytes))
		}
	}

	// A question about aforge itself. It is the one reading whose answer is not
	// on the board at all.
	if selfQuestionCued(message) {
		add("reads as a question about AFORGE ITSELF — the manual is the only honest source for it; invent no machinery")
	} else if controlStatusCued(message) {
		add("reads as a question about where work is up to — the shape of it, not just its temperature; plan is the read that answers it")
	}

	// What the words rank against, and what spoke last. These two are the arms
	// that resolve "it" and "that one", and they are the reason the ladder was
	// ever tolerable: no vocabulary in this file can find the job a person is
	// replying to without them.
	if named := h.hintNamedJobs(message, active); named != "" {
		add("the words rank against: %s", named)
	}
	if spoke, adjoins, err := h.adjacencyTarget(user, active); err == nil && adjoins {
		add("the last thing said in this conversation was %s (%s) speaking — a sentence typed here is usually about that work",
			spoke.Node.ID, truncateBytes(surgeryTargetLabel(spoke.Node), hintLabelBytes))
	}
	// Deixis without vocabulary: "the job", "that one", "what you're doing". It
	// points at live work while borrowing none of its words, which is precisely
	// the case no lexical ranking can reach — and with exactly one job live
	// there is nothing else it could mean, so the reading says which.
	if len(active) > 0 && refersToLiveWork(message, cued, len(active)) {
		if len(active) == 1 {
			add("points at work already underway without naming it, and only %s (%s) is live — there is nothing else it could mean",
				active[0].Node.ID, truncateBytes(surgeryTargetLabel(active[0].Node), hintLabelBytes))
		} else {
			add("points at work already underway without naming it — resolve the referent before acting, never guess it")
		}
	}

	if len(lines) == 0 {
		return ""
	}
	return hintHeader + strings.Join(lines, "\n")
}

// hintJobLabels names a handful of jobs the way the person would recognise them,
// with their ids beside the names so the loop can act on one without a re-read.
func hintJobLabels(nodes []store.Node) string {
	named := make([]string, 0, hintTargetCap)
	for _, node := range nodes {
		named = append(named, fmt.Sprintf("%s (%s)", node.ID,
			truncateBytes(surgeryTargetLabel(node), hintLabelBytes)))
		if len(named) == hintTargetCap {
			break
		}
	}
	return strings.Join(named, ", ")
}

// hintNamedJobs is the lexical arm: the live jobs whose own briefs the message's
// content words actually reach, at the same anchor floor every other reader in
// this package uses. Below that floor the overlap is a coincidence of English,
// and naming a job on a coincidence is exactly the wrong-target failure the
// floor exists to refuse.
func (h *Head) hintNamedJobs(message string, active []store.SurgeryTarget) string {
	if h == nil || h.store == nil || len(active) == 0 {
		return ""
	}
	ranked, err := h.rankRedirectTargets(message, active)
	if err != nil || len(ranked) == 0 {
		return ""
	}
	named := make([]string, 0, hintTargetCap)
	for _, target := range ranked {
		if target.Score < RedirectAnchorScore {
			continue
		}
		named = append(named, fmt.Sprintf("%s (%s)", target.Node.ID,
			truncateBytes(surgeryTargetLabel(target.Node), hintLabelBytes)))
		if len(named) == hintTargetCap {
			break
		}
	}
	return strings.Join(named, ", ")
}
