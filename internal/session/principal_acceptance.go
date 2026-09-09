package session

// THE SESSION'S ACCEPTANCE: what the WHOLE ask has to satisfy.
//
// Every acceptance in this build until now has been one unit of work's. The
// judge writes one when it starts a task (route_judge.go's [routeAcceptance]),
// the shaper writes one for a proposal, and that node's auditor is the only
// thing that ever reads it. NOTHING HELD THE WHOLE ASK. A session could land
// four units of work, every one of them verified against its own sentence, and
// there was no sentence anywhere that said whether the thing the person
// actually asked for had happened.
//
// For an attended session that is right: the person is holding it. For an
// unattended one it is the missing half of the whole feature — a [Steward] that
// carries work on has to be carrying it on TOWARDS something, and a goal owner
// with no acceptance is a goal owner whose only reading of "finished" is the
// running model's own opinion of itself.
//
// ── IT IS THE JUDGE'S OWN MACHINERY, NOT A SECOND ONE ───────────────────────
//
// [routeAcceptance]'s lower rung frames the person's own words as the whole
// done-condition. No model rewrites a concrete request before the working model
// sees it. This keeps writing, data and code requests equally direct, and takes
// one synchronous call off the front of every unattended run.
//
// ── AND IT IS FROZEN ────────────────────────────────────────────────────────
//
// [Steward.setAcceptance] writes once and refuses every write after it. A
// done-condition the running model can edit is a done-condition the model
// grades itself against, which is the exact reading the checkpoint road was
// rebuilt to stop relying on. Only a person changes it, and the way they change
// it is by asking for something else.

import (
	"context"

	"github.com/Agent-Field/aforge-v2/internal/roles"
)

// sessionAcceptanceBrief asks for the done-condition of a whole ask.
//
// IT SHARES THE WIRE AND NOT THE QUESTION. [routeVerdictContract] is appended
// verbatim so the fields, their bounds and what each is FOR are one sentence in
// this package rather than two — the drift that ends with one door writing an
// acceptance nobody can check is exactly what that const exists to prevent. The
// `work` field is answered true here by construction: somebody has already
// asked for this and it is already being done. This late reading supplies only
// a destination; it cannot add commands after the baseline was taken.
const sessionAcceptanceBrief = `You are given ONE ask, in the person's own words, after finished work was retained outside the requested workspace.

Read only the requested final destination. Declare delivery: {"kind":"workspace|branch|report", "quote":"<verbatim words from the person's ask>"}. Use workspace when changed files must reach the person's requested working copy or when uncertain. Use branch only when the person explicitly wants a retained branch as the final result without integration. Use report only when the requested final result is the answer/report itself, not implementation elsewhere. For branch/report quote the original words establishing that destination, including any constraint about integration. Do not choose branch merely because workers use worktrees, and do not convert an implementation request into a report about implementation.

Answer {"work": true, "goal": "<the ask, self-contained>", "acceptance": "delivery matches the original request", "checks": [], "delivery": {"kind":"workspace", "quote":""}, "why": "<one line>"}. Do not declare commands.

` + routeVerdictContract

// sessionAcceptanceQuestion is the ask as the writer reads it: the person's own
// words and nothing else. There is no turn to show it — this is asked before
// anything has happened — which is the whole difference from
// [routeJudgeQuestion].
func sessionAcceptanceQuestion(ask string) string {
	return "WHAT THE PERSON ASKED FOR:\n" + ask +
		"\n\nDeclare only its requested final destination. Answer with one JSON object."
}

// completeRetainedContract asks the old acceptance writer only when a finished
// task left changed work somewhere other than the requested workspace. That is
// the one ending where `workspace`, `branch` and `report` produce different
// answers, so it is the one place where paying for the distinction can change
// what happens. The writer still sees only the original ask. Its rewritten
// acceptance and checks are discarded; only its structured destination may
// complete the contract frozen before work began.
func (a *Agent) completeRetainedContract(ctx context.Context, remains Remains) Remains {
	steward := a.steward()
	if steward == nil {
		return remains
	}
	needed := false
	for _, landing := range remains.Landings {
		if landing.needsDelivery() {
			needed = true
			break
		}
	}
	if !needed {
		return remains
	}
	ask := steward.Ask()
	if ask == "" {
		return remains
	}
	leader, ready := steward.beginDeliveryReading(ask)
	if !leader {
		if ready != nil {
			select {
			case <-ready:
			case <-ctx.Done():
				return remains
			}
		}
		remains.Delivery = steward.declaredDelivery()
		return remains
	}
	a.mu.Lock()
	model := a.model
	a.mu.Unlock()
	verdict, ok := a.putRouteQuestion(ctx, roles.RoleRouterConfirm, model,
		sessionAcceptanceBrief, sessionAcceptanceQuestion(ask), ask)
	delivery := routeDelivery(verdict, ask)
	if !ok || delivery.Kind == "" {
		// UNKNOWN IS CONSERVATIVE, AND IT IS SETTLED ONCE. A writer that could
		// not answer must not be bought again at every later ending, and treating
		// its silence as workspace delivery keeps retained work unfinished.
		delivery = deliveryContract{Kind: "workspace"}
	}
	if steward.finishDeliveryReading(ask, delivery) {
		a.journalDelivery(steward)
	}
	remains.Delivery = steward.declaredDelivery()
	return remains
}

// beginDeliveryReading elects one caller to take the lazy reading. Other
// endings wait on the same result without holding the Steward lock over I/O.
func (s *Steward) beginDeliveryReading(ask string) (bool, <-chan struct{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ask != s.ask || s.acceptance == "" || s.delivery.Kind != "" {
		return false, nil
	}
	if s.deliveryReading != nil {
		return false, s.deliveryReading
	}
	s.deliveryReading = make(chan struct{})
	return true, s.deliveryReading
}

// finishDeliveryReading freezes the conservative result and releases every
// ending waiting for it. It never changes the existing command authority.
func (s *Steward) finishDeliveryReading(ask string, delivery deliveryContract) bool {
	delivery = validDelivery(ask, delivery)
	if delivery.Kind == "" {
		delivery = deliveryContract{Kind: "workspace"}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	ready := s.deliveryReading
	written := ask == s.ask && s.acceptance != "" && s.delivery.Kind == ""
	if written {
		s.delivery = delivery
	}
	s.deliveryReading = nil
	if ready != nil {
		close(ready)
	}
	return written
}

// openAcceptance writes this session's acceptance, once, at the start of the
// first turn a [Steward] runs.
//
// IT GRANTS NO COMMAND AUTHORITY. A command in prose remains evidence, not
// permission to execute it again. The existing structured writer is deferred
// until a retained result makes its delivery distinction necessary.
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
	if ask == "" {
		return routeFallbackAcceptance
	}
	return routeAskAcceptance + ask
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

// journalDelivery records the late destination reading separately from the
// acceptance that was already frozen and shown before any work began.
func (a *Agent) journalDelivery(steward *Steward) {
	a.mu.Lock()
	file := a.file
	a.mu.Unlock()
	file.appendPrincipal(journalPrincipal{
		Who:      "steward",
		Event:    "delivery",
		Checks:   steward.declaredChecks(),
		Delivery: steward.deliveryReceipt(),
	})
}
