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
// The wire is [routeVerdictContract], the ladder that falls back to the
// person's own words is [routeAcceptance], and the bound is the shaper's
// ([taskShapeAcceptanceLimit]). What is new here is one brief and one moment:
// the question is asked about the ASK rather than about a finished turn, and it
// is asked once, at the start of the first turn, before any of the work has had
// a chance to argue for a definition of done that suits it.
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
// asked for this and it is already being done, so the only field that carries
// anything is the acceptance.
const sessionAcceptanceBrief = `You are given ONE ask, in the person's own words, at the moment somebody started working on it. Nobody will be watching while it is worked on.

You write ONE thing: the DONE WHEN sentence for the WHOLE of that ask — the observable condition that says the whole thing is finished, not the part that was easiest to reach.

Write it so that somebody who cannot see this ask, cannot see the work, and cannot ask anybody anything can stand in front of the result and say yes or no. Name what must exist and the check that shows it.

Answer {"work": true, "goal": "<the ask, self-contained>", "acceptance": "<done when>", "why": "<one line>"}.

` + routeVerdictContract

// sessionAcceptanceQuestion is the ask as the writer reads it: the person's own
// words and nothing else. There is no turn to show it — this is asked before
// anything has happened — which is the whole difference from
// [routeJudgeQuestion].
func sessionAcceptanceQuestion(ask string) string {
	return "WHAT THE PERSON ASKED FOR:\n" + clip(ask, routeAskBytes) +
		"\n\nWrite the DONE WHEN sentence for the whole of it. Answer with one JSON object."
}

// openAcceptance writes this session's acceptance, once, at the start of the
// first turn a [Steward] runs.
//
// EVERY WAY IT CAN FAIL LEAVES THE SESSION WITH NO ACCEPTANCE, and that is a
// working session rather than a broken one: [Steward.Decide] falls back on the
// landings and the checks, which is less than it would have had and is still
// more than the engine ever had. A run must not fail to start because a
// sidecar model was unreachable.
//
// IT IS BILLED TO THE ERRAND POCKET, for [Agent.readMark]'s reason: it is a
// side-call to a different model that the person did not ask for.
//
// AND IT IS ASKED ON THE MASTERMIND TIER. This one sentence is what the whole
// unattended run is measured against for its entire life, it is written once,
// and it costs one call — the cheapest place in this whole road to be right.
func (a *Agent) openAcceptance(ctx context.Context, hub *eventHub) {
	steward := a.steward()
	if steward == nil || steward.Acceptance() != "" {
		return
	}
	ask := steward.Ask()
	if ask == "" {
		return
	}
	a.mu.Lock()
	model := a.model
	a.mu.Unlock()
	verdict, ok := a.putRouteQuestion(ctx, roles.RoleRouterConfirm, model,
		sessionAcceptanceBrief, sessionAcceptanceQuestion(ask))
	if !ok {
		// The ladder's lower rungs still say something true about a whole ask —
		// "everything asked for below is actually done" over the person's own
		// words — so a writer that could not be reached costs the sharpness of
		// the sentence and not the sentence.
		verdict = routeVerdict{}
	}
	if !steward.setAcceptance(routeAcceptance(verdict, ask)) {
		return
	}
	a.journalAcceptance(steward)
	// AND THE PERSON READS IT ONCE. There is no new surface for this: it is one
	// notice line on the turn that wrote it, drawn exactly where a task's own
	// "done when" is drawn — and on a session nobody is watching, the hub is nil
	// and the line is simply not drawn (tools_settings.go states the law).
	hub.send(Event{Kind: EventNotice, Text: sessionAcceptanceLead + steward.Acceptance()})
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
	})
}
