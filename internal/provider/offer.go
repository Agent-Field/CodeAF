package provider

import (
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/calllog"
)

// ── A PIN IS ASKED, NEVER OVERRIDDEN ────────────────────────────────────────
//
// The controller's arithmetic is the same for a pinned lane as for any other:
// the wait has gone on longer than acting would cost, and there is somewhere
// better to be. What changes is the ACT. A person who named a machine said
// something, and a build that quietly went somewhere else the first time that
// machine was slow would be answering a question nobody asked — while a build
// that waited forever on it would be the defect this whole design exists to
// remove. So the wait is turned into one sentence:
//
//	coreweave is slow · switch to auto? (y)
//
// FOUR RULES AND NO MORE.
//
//  1. IT IS RAISED ONCE per request. A second offer for one answer is nagging.
//  2. `y` FIRES THE RESCUE AT ONCE, to the lane the frontier already named.
//     There is no second choice made at the worst possible moment.
//  3. ANY VISIBLE TOKEN WITHDRAWS IT. The pin came good and the question is
//     moot. A thinking delta does not: nothing has arrived that a person can
//     read.
//  4. THE PIN ITSELF IS UNTOUCHED. Accepting is for THIS answer; the next
//     request goes to the pinned machine, because that is what a pin means.
//
// AND WITH NOBODY TO ASK, THE ANSWER IS STILL DELIVERED. A headless run — no
// phase reader registered, which is the same seam the surface uses — borrows at
// the ceiling, once, and says so on the row. A pin is a person's instruction,
// and an instruction whose author cannot be reached at the moment it becomes
// expensive is honoured by getting them their answer and writing down what was
// done. The alternative is a run that waits forever on a machine that has gone
// quiet, with nobody watching to notice.

// offerWindow is how long an open offer stands before it is dropped.
//
// It is [PhaseWindow] and not a figure of its own: the offer rides one phase
// post, a surface stops drawing a phase it has not heard about for that long,
// and an offer that outlived the sentence describing it would be a keystroke
// landing on a question the person can no longer see.
const offerWindow = PhaseWindow

// offer is one open question and what answering it does.
type offer struct {
	// lane is the pinned machine that went quiet and alt where the rescue would
	// go. Both are for the sentence; the act itself is accept's.
	lane string
	alt  string
	at   time.Time
	// accept is the rescue, fired on the answering goroutine and never under
	// the registry's lock: it starts a request.
	accept func()
}

// offers is every question this process currently has open.
//
// A MAP AND A MUTEX AND NOTHING ELSE. There is at most one entry per request in
// flight that has a pin and a stall, which is a handful in the worst session
// anybody has had, and the sweep below keeps a surface that never answers from
// holding one forever.
var offers struct {
	mu   sync.Mutex
	open map[string]*offer
}

// raiseOffer opens one question and hands back the token that answers it. The
// token is opaque: a surface carries it from the phase news back to
// [AnswerOffer] and never reads it, which is what makes the keystroke land on
// the request that asked rather than on whichever one is in flight when it is
// pressed.
func raiseOffer(lane, alt string, at time.Time, accept func()) string {
	if alt == "" || accept == nil {
		return ""
	}
	token := calllog.NewID()
	offers.mu.Lock()
	defer offers.mu.Unlock()
	if offers.open == nil {
		offers.open = map[string]*offer{}
	}
	sweepOffers(at)
	offers.open[token] = &offer{lane: strings.TrimSpace(lane), alt: alt, at: at, accept: accept}
	return token
}

// withdrawOffer takes a question back down: the lane came good, the request
// ended, or somebody answered it. It is idempotent, because all three of those
// can happen at once.
func withdrawOffer(token string) {
	if token == "" {
		return
	}
	offers.mu.Lock()
	defer offers.mu.Unlock()
	delete(offers.open, token)
}

// AnswerOffer answers an open offer and reports whether one was still open.
//
// FALSE IS A REAL ANSWER AND NOT A FAILURE: the lane came good while the person
// was reaching for the key, or the request finished, or the offer aged out. A
// surface that is told false draws nothing and says nothing, because the thing
// it would have said is no longer true.
//
// The rescue runs OUTSIDE the lock. It starts a request, and a registry held
// across that would be a lock held across a handshake.
func AnswerOffer(ask string, yes bool) bool {
	ask = strings.TrimSpace(ask)
	if ask == "" {
		return false
	}
	offers.mu.Lock()
	open, found := offers.open[ask]
	delete(offers.open, ask)
	offers.mu.Unlock()
	if !found {
		return false
	}
	if yes {
		open.accept()
	}
	return true
}

// OpenOffer is what is being asked about one request, for a surface that has to
// draw it: the machine that went quiet and the one a `y` would go to. It is
// empty when that token names nothing open.
func OpenOffer(ask string) (lane, alt string, open bool) {
	offers.mu.Lock()
	defer offers.mu.Unlock()
	question, found := offers.open[strings.TrimSpace(ask)]
	if !found {
		return "", "", false
	}
	return question.lane, question.alt, true
}

// sweepOffers drops what has aged out. It runs with the lock held, on the one
// path that is already writing, so an unanswered question costs a map entry
// until the next stall and never longer.
func sweepOffers(now time.Time) {
	for token, question := range offers.open {
		if now.Sub(question.at) > offerWindow {
			delete(offers.open, token)
		}
	}
}
