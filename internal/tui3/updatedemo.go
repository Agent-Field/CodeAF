package tui3

import (
	"errors"
	"strings"

	"github.com/Agent-Field/codeaf/internal/session"
	codeupdate "github.com/Agent-Field/codeaf/internal/update"
)

// Deterministic render fixtures for the update offer, on the same terms as the
// question page's (questiondemo.go): a case named in the environment raises the
// REAL state machine so a screenshot can be taken without waiting out a grace
// or paying for a download. It is not a person-facing feature, there is no key
// or command that reaches it, and it writes nothing.
//
// EVERY CASE DISABLES THE UPDATER'S HOOKS, so no fixture can make a request or
// replace a file: they exist to draw the surface, and a capture harness must be
// able to run them on a machine with no network at all.
const updateDemoEnv = "CODEAF_UPDATE_DEMO"

// openDemoUpdate raises a fixture where the environment names one.
func (a *app) openDemoUpdate(env func(string) string) {
	if env == nil {
		return
	}
	name := strings.TrimSpace(env(updateDemoEnv))
	if name == "" {
		return
	}
	// A FIXTURE MAY NOT TOUCH THE WORLD. Clearing every updater seam first means
	// even a case that looks like a download cannot start one.
	a.updateCheck = nil
	a.resolveUpdate = nil
	a.installUpdate = nil
	a.updateAuto = nil

	const released, running = "v0.9.3", "v0.9.2"
	a.updateRunning = running
	a.updateCurl = updateFallbackCurl
	// THE GREETING IS SPENT, exactly as it is on a second launch: the welcome
	// stands where the keys row would, and a fixture whose subject IS that row
	// would otherwise be a picture of the greeting instead.
	a.welcome = welcome{spent: true}

	switch name {
	case "offer":
		a.offer.raise(released, running, a.now())
		a.note(a.updateOfferNote())
	case "downloading":
		a.offer.downloading()
		a.offer.tag = released
		a.note(a.updateOfferNote())
	case "ready":
		a.offer.ready(a.now())
		a.offer.tag = released
		a.note(a.updateOfferNote())
	case "ready-working":
		// A REPRESENTATIVE RUNNING TURN, drawn from the same events a real one
		// sends: a question asked, a tool begun, an answer arriving, and a draft
		// still in the box. Nothing here is a promise — the offer is simply
		// raised over work that is genuinely in progress.
		a.turn = 1
		a.state = stateWorking
		a.entries = append(a.entries, entry{kind: entryUser, text: "tighten the retry loop in the sync client"})
		a.ingest(session.Event{Kind: session.EventToolBegin, Tool: "bash", CallID: "fixture-call"})
		a.ingest(session.Event{Kind: session.EventTextDelta, Text: "Reading the retry loop now."})
		a.input.setText("and keep the backoff capped")
		a.offer.ready(a.now())
		a.offer.tag = released
		a.note(a.updateOfferNote())
	case "manual":
		a.offer.downloading()
		a.offer.tag = released
		a.note(a.updateOfferNote())
	case "off":
		a.note(quietUpdateNotice(codeupdate.Available{Latest: released, Running: running}))
	case "failure":
		// THE PRODUCTION COMPLETION, DRAWN BY THE PRODUCTION HANDLER. A
		// synthetic error goes through [app.updateStopped] on the automatic
		// road, so the frame shows the sentence a real failed background
		// install shows rather than a copy of it kept here: the fixture cannot
		// promise more than the surface says.
		a.updateStopped(true, released, errors.New("the checksum did not match"))
	}
	// EVERY CAPTURE SAYS WHAT IT IS, written LAST so it is the line still on
	// screen whichever state the fixture drew. A frame taken from a fixture can
	// therefore never be mistaken for a scene a person reached.
	a.note("fixture · codeaf update states · CODEAF_UPDATE_DEMO=" + name + " · no network, nothing installed")
}

// updateFallbackCurl is the road a capture names when no door has handed this
// surface its own line; the real door always does (cmd/codeaf's runSurface).
const updateFallbackCurl = "curl -fsSL https://agentfield.ai/get/codeaf | bash"
