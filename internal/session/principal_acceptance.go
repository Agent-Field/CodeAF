package session

// The unattended session keeps the person's original request for its existing
// checks and execution-state reports. This does not create a second completion
// protocol or buy a classifier to decide where the answer must be delivered.

import "context"

// openAcceptance writes this session's acceptance, once, at the start of the
// first turn a [Steward] runs.
//
// IT GRANTS NO COMMAND AUTHORITY. A command in prose remains evidence, not
// permission to execute it again. No model rewrites the request or invents a
// destination contract at completion.
func (a *Agent) openAcceptance(_ context.Context, hub *eventHub) {
	steward := a.steward()
	if steward == nil || steward.Acceptance() != "" {
		return
	}
	ask := steward.Ask()
	if ask == "" {
		return
	}
	if !steward.setAcceptanceDelivery(ask, sessionAcceptance(ask), nil, deliveryContract{}) {
		return
	}
	a.journalAcceptance(steward)
	// AND THE PERSON READS IT ONCE. There is no new surface for this: it is one
	// notice line on the turn that wrote it, drawn exactly where a task's own
	// "done when" is drawn — and on a session nobody is watching, the hub is nil
	// and the line is simply not drawn (tools_settings.go states the law).
	hub.send(Event{Kind: EventNotice, Text: sessionAcceptanceLead + clip(steward.Acceptance(), briefAskLimit)})
}

// sessionAcceptance keeps the complete original request as the internal
// contract. Display and prompt surfaces apply their own bounds separately.
func sessionAcceptance(ask string) string {
	return ask
}

// sessionAcceptanceLead opens the one line a person reads about what this
// session is working towards. It is the label the landed-work card already uses
// for the same fact, so the two never teach two vocabularies for one thing
// (internal/tui3's taskdone.go spells it "done when · ").
const sessionAcceptanceLead = "done when · "

// journalAcceptance writes the frozen sentence down where the rest of this
// session's decisions are, so an autopsy can read what the run was actually
// measured against instead of inferring it.
func (a *Agent) journalAcceptance(steward *Steward) {
	a.mu.Lock()
	file := a.file
	a.mu.Unlock()
	file.appendPrincipal(journalPrincipal{
		Who:        "steward",
		Event:      "acceptance",
		Acceptance: steward.Acceptance(),
		Checks:     steward.declaredChecks(),
		Delivery:   steward.deliveryReceipt(),
	})
}
