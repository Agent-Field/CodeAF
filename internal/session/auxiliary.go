package session

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/effort"
	"github.com/Agent-Field/codeaf/internal/lane"
	"github.com/Agent-Field/codeaf/internal/provider"
	"github.com/Agent-Field/codeaf/internal/roles"
	"github.com/Agent-Field/codeaf/internal/taxonomy"
)

// ── ONE CALL NOBODY TYPED ───────────────────────────────────────────────────
//
// An auxiliary call is an errand: the name a session gives itself, the two words
// a task is called, the judge that reads one turn and answers one question. They
// are all the same shape — resolve a role to a model, ask once, bill the person
// for it — and until this file each of them spelled that shape itself, which is
// how three of them ended up with three different answers to the two questions
// below.
//
// HOW LONG IT MAY TAKE. The adapter bounds a completion between five and fifteen
// minutes, scaled off the output cap (internal/provider's
// adaptiveCompletionTimeout). That is the right bound for a person's turn and a
// ridiculous one for eight words of title: a session with a wedged errand held a
// provider slot and a goroutine for a quarter of an hour, and the person waiting
// for their own turn behind it was told nothing. So every errand carries the
// bound of its role's TIER ([roles.PatienceFor]) — derived, never a table of
// per-role seconds — and a caller that knows its own call is tighter still says
// so and wins, because the nearer of two deadlines on one context is the one in
// force.
//
// THAT BOUND IS THE ERRAND'S AND NOT EACH RUNG'S. See [Agent.callRoleChecked]
// for the whole of why, and for what does bound one rung: the same stream guard
// the turn runs under, which cuts silence and leaves an answer that is being
// written alone.
//
// AND WHAT HAPPENS WHEN THE MODEL CANNOT ANSWER AT ALL. The errand used to be
// abandoned: a role pinned to a small model that is down, or a tier naming a
// slug an account has lost access to, and the session simply had no title, no
// task name, no judgement — quietly, with the model the person is talking to
// sitting there able to do the work.
//
// So it FALLS THROUGH ONE RUNG. The rung is the roles ladder's own next entry —
// pin, then tier, then the model the conversation is on — and that is the whole
// reason it is the ladder and not the adapter's fallback chain. The ladder is
// ALREADY the one answer to "which model answers this call"; its floor is the
// session model, which is by construction a model that works, because it is the
// one answering the person's own turns. The adapter's chain answers a different
// question — "where does this CONVERSATION go" — and it already applies to these
// calls underneath, on a refusal and on pacing that will not clear
// (internal/provider's endpoints.go). Spelling "the next model" twice over one
// request is how two answers drift.
//
// ONE rung, and then the failure is real. A ladder walked to the bottom on every
// errand would turn one bad minute at a provider into three charges and three
// waits for an answer nobody asked for.

// roleFallThroughs is how many rungs BELOW the resolved one an errand may try.
// One: see above.
const roleFallThroughs = 1

// callRole makes one auxiliary call and reports which model actually answered.
//
// The returned model is not decoration and callers must bill THAT one: an
// errand that fell through and was billed against the model that refused it
// would put a charge on a row nobody's request ever reached.
//
// `sessionDefault` is the ladder's floor, exactly as [roles.ResolveCall] takes
// it — except under [Config.OneModel], where the floor is the conversation's own
// model whatever the caller passed, for the reason spelled at that seam below.
// Options are the caller's own — temperature, output cap, a response format
// — and the model is added here so no caller can name one that disagrees with
// the rung it is on.
func (a *Agent) callRole(
	ctx context.Context,
	role roles.Role,
	sessionDefault string,
	messages []ai.Message,
	options ...ai.Option,
) (*ai.Response, string, error) {
	return a.callRoleChecked(ctx, role, sessionDefault, messages, nil, options...)
}

// callRoleChecked lets naming refuse an unusable answer before the next rung
// is abandoned. The check accounts for rejected replies, because those still cost.
func (a *Agent) callRoleChecked(ctx context.Context, role roles.Role, sessionDefault string,
	messages []ai.Message, accept func(*ai.Response, string) bool, options ...ai.Option,
) (*ai.Response, string, error) {
	a.mu.Lock()
	source := a.config.RolesSource
	// ONE MODEL MEANS ONE MODEL AT EVERY RUNG. A crew-only caller passes an empty
	// floor deliberately — it is a quality judgement about a profile that HAS a
	// crew, and it refuses to let the running model mark its own work — but the
	// person who passed `--one-model` has already said the conversation's model
	// is the crew. Left alone, those callers had no pin, no tier and no floor
	// under the flag, so [roles.Ladder] answered ErrNoModel and a measured run
	// had no mark reader and no brief writer at all (#443). It is decided HERE,
	// at the one seam every errand passes through, so the next crew-only caller
	// is correct without knowing the flag exists.
	//
	// THE FLOOR IS a.model AND NOT THE TURN'S LATCHED MODEL, because it is the
	// same live conversation model every other errand already passes as its own
	// floor (title.go, taskname.go, route_judge.go all read it live). A /model
	// typed mid-turn reaches the turn's own next request now ([Agent.SetModel]),
	// so reading it live here moves the crew-only rungs WITH the work rather than
	// leaving the errands beside a turn talking to a model they do not know about.
	if a.config.OneModel {
		sessionDefault = a.model
	}
	a.mu.Unlock()
	// ONE ESC SPENDS ONE PLANNER AND ONE TITLE. A second claim in the same
	// generation is silence — the leftover race and the redirect must not
	// each fire their own (interrupt_fan.go, F13/F17).
	if err := a.interrupt.allow(role); err != nil {
		return nil, "", err
	}
	rungs, err := roles.Ladder(roles.Source(source), role, sessionDefault)
	if err != nil {
		return nil, "", err
	}
	// EVERY ERRAND ARMS ITS CUT MONEY HERE. [Agent.callRole] is the one door every
	// ERRAND passes through, including errands whose own deadline is not derived
	// from a turn; arming them separately would leave the next one able to lose
	// the receipt this shared seam exists to keep.
	//
	// AND "ERRAND" IS NARROWER THAN "MODEL CALL", WHICH IS WHAT THIS SENTENCE USED
	// TO CLAIM. It said this was the door all auxiliary provider calls pass
	// through, and ten did not: the guardian, vision, the shaper, the spell-out,
	// the intake, the planner, the designer, the ceiling's handoff draft, a saved
	// program's own step and the standing sentinel each reached the wire by
	// another road. Some of them still do, legitimately — vision STREAMS into the
	// room in the person's own turn, the handoff draft is billed to the turn on
	// the turn's own model, a program's step runs on the program's model and no
	// role's — and an errand ladder wrapped round any of those would be the wrong
	// shape twice over. What every one of them owes is the TAG, and that is the
	// claim that is now true and enforced: clientdoor.go's [callPurpose] is the
	// one door every REQUEST passes through, this one is the one door every
	// ERRAND passes through, and neither pretends to be the other.
	ctx = provider.WithReconcile(ctx, a.reconciled)
	if len(rungs) > roleFallThroughs+1 {
		rungs = rungs[:roleFallThroughs+1]
	}
	patience := roles.PatienceFor(role)
	// THE PATIENCE IS THE ERRAND'S AND NOT EACH RUNG'S, and that is the whole of
	// what changed here on 2026-09-10.
	//
	// It used to be divided evenly — `what is left / rungs still to come`, armed
	// as a hard [context.WithTimeout] on the call. A WALL CLOCK CANNOT TELL A
	// RUNG THAT IS SILENT FROM A RUNG THAT IS ANSWERING, so it killed both.
	// Measured on task 1 of conversation 57d51779f63ac603: the division review's
	// three minutes became ninety seconds a rung, the first rung wrote its first
	// token in one second and was still writing at ninety when it was cut, the
	// second did the same, and the parts were admitted with nobody having read
	// them — after three minutes and twenty seconds in which the person watching
	// saw one word.
	//
	// WHAT BOUNDS ONE RUNG NOW IS THE GUARD THE TURN ALREADY RUNS UNDER. Every
	// completion this process makes is served over the event stream whether or
	// not anybody is reading it (internal/provider's [Client.CompleteWithMessages]
	// — the fail-safe that armed only when a person was looking was decoration,
	// and stopped being one). So a rung that goes quiet is cut by streamguard.go's
	// first-delta and mid-stream bounds in tens of seconds, and a rung that is
	// writing is bounded by the wall its lane's own measured history earns it,
	// which re-arms for as long as the stream keeps pace (#786). An errand
	// inherits both for nothing.
	//
	// AND THAT IS ALSO WHY THE SPLIT IS NO LONGER NEEDED. It was written for
	// 2026-08-28, where a mastermind endpoint sat silent for a review's entire
	// three minutes and the fall-through rung — whose floor is the model
	// answering the person's own turns, alive by construction — was never asked.
	// The guard ends that rung in tens of seconds now, so the rung below is
	// reached with nearly the whole budget still on the clock, which is more time
	// than an even share ever gave it.
	//
	// So the tier's patience bounds the ERRAND, once. Every rung runs on whatever
	// is left of it, and a caller with a nearer deadline of its own still wins,
	// because [context.WithTimeout] keeps the nearer of the two.
	//
	// EXCEPT FOR WHAT THE RUNGS BELOW ARE OWED, which is [errandReserve] and is
	// the one piece of the old arithmetic worth keeping. The guard cuts a rung
	// that is not working — but there is one shape it cannot see, a gateway that
	// takes `stream: true` and answers a whole completion in one piece
	// (internal/provider's [Client.completionInOnePiece] arms no stall watch), and
	// a wedged endpoint behind one of those would eat the whole errand and leave
	// the ladder's floor unasked. A fifth held back is enough that the floor —
	// the model already answering the person's own turns — is always reachable,
	// and far enough above the guard's own wall that it never cuts a stream the
	// guard was happy with. That is the difference between a reserve and the even
	// split it replaces.
	errandCtx, endErrand := context.WithTimeout(ctx, patience)
	// AND THE ERRAND SAYS WHAT IT IS FOR, ON EVERY ROW IT WRITES.
	//
	// THE ROLE IS THE TAG, because the role is what the errand IS — naming a
	// conversation, judging a route, writing a caption — and it is the same word
	// internal/lane's roles.go and the journal's own call line already use, so
	// three records of one call agree about what to call it. It is carried to
	// the door as a [callPurpose] rather than stamped on the context here: the
	// door is what every request in this package passes over, and it is the only
	// thing that spells the tag (clientdoor.go says why that had to move).
	defer endErrand()
	tell := errandWatchFrom(ctx)

	var lastErr error
	// The rung's index is what tells a reader that an errand which walked its
	// whole ladder was ONE errand and not three.
	//
	// ── ONE REQUEST PER RUNG, AND NO COUNT ANYWHERE ─────────────────────────
	//
	// There was an inner loop here, `for tries := 1; tries <= errandTriesPerRung`
	// with the constant pinned at one — a budget of one, walked as a loop, which
	// is a shape that reads as though somebody could raise it and is the last
	// count in this file (docs/design/recovery/DESIGN.md §7). What bounds an
	// errand is `patience` above, a DEADLINE on the whole ladder, and what bounds
	// one rung is the reserve the rungs below it are owed
	// ([errandRungContext]). The rung below is a better move than another try —
	// a different model, on a different lane, reached at once with no backoff to
	// pay — and nobody typed this call, so a ladder that spent a turn's patience
	// per rung on a title would turn one bad minute at a provider into several
	// charges and several waits for an answer nobody asked for.
	for attempt, rung := range rungs {
		// AND THE CALLER THAT HAS SOMEBODY WATCHING IS TOLD, before the wait
		// rather than after it. A person looking at a task being sized has one
		// phase word and nothing under it; which model is being asked, and
		// which of how many, is the difference between a wait and a hang.
		tell(errandNews{Model: rung.Model, Rung: attempt + 1, Rungs: len(rungs)})
		// WithoutStream because nobody asked for this call: left on the turn's
		// stream it would type itself into the room in the model's voice. It
		// takes the OBSERVER off and not the transport — the request is still
		// served as a stream and still guarded as one, which is the sentence
		// above about what bounds a rung.
		//
		// And IntentBackground for the other half of the same sentence. Nobody
		// asked for it and nobody is waiting on it, so a second saved here buys
		// nothing — which is what the lane chooser prices, and what an answer
		// nobody reads is worth (internal/provider's workload.go). It does not
		// choose a road: which road every call takes is the routing row's answer
		// and this build no longer writes one for anybody (velocity.go's
		// [provider.DefaultRouting]). This is the one place that says it,
		// because this is the one place an errand is made.
		//
		// AND THE ROLE ITSELF, WHICH IS THE SENTENCE ABOVE SAID PROPERLY.
		// internal/lane's roles.go holds what an errand's second is worth, what
		// bar its answer has to clear, and — the half a person feels — whether
		// anybody is reading THIS stream. Every errand made here is a side call
		// of somebody's turn, so none of them owns the phase clock: a naming
		// errand that answered while a person was waiting on their own slow
		// answer used to take the status line away from it, which was half of
		// the reported defect the clock exists for. The intent stays beside it
		// because what a wait is worth is still read off it, and it is now a
		// reading of the role rather than a second opinion about it.
		callCtx := provider.WithRole(
			provider.WithRoutingIntent(provider.WithoutStream(errandCtx), provider.IntentBackground),
			errandRole(role))
		// AN ERRAND ASKS THE LADDER LIKE EVERYTHING ELSE, and the ladder's
		// answer for it is nothing (internal/effort's RoleErrand): naming a
		// conversation and judging a route are the session's own housekeeping,
		// and the depth a person set so their QUESTION would be thought about is
		// not spent on the label. The scope is deliberately narrow — the role
		// and the tier's own suffix, and none of the fields above them — because
		// an errand belongs to the machine and not to the conversation it runs
		// beside.
		//
		// The tier's suffix goes in the task scope because that is what it is: a
		// rung somebody wrote onto this piece of work when they configured the
		// crew, sitting above the role's floor and below nothing.
		if tier, ok := effort.Parse(rung.Effort); ok {
			if asked := effort.Resolve(effort.Scope{Task: tier, Role: effort.RoleErrand}); asked != effort.None {
				// WithEffortRung and not the configured setter: a level carried
				// on a tier value is a HARNESS default, which the adapter drops
				// for a model no catalog can vouch for. A person's own ctrl+t is
				// the other setter and does not reach an errand at all.
				callCtx = provider.WithEffortRung(callCtx, asked)
			}
		}
		// AND A SLOT FOR WHOEVER ANSWERS, so the errand's own call line can name
		// the endpoint the way a turn's does. An errand routes by price, which
		// means it is exactly the kind of request whose server cannot be guessed
		// from the model name. It is made fresh per attempt: a retry served by
		// somebody else must not be billed to the endpoint that failed.
		served := &provider.ServedEndpoint{}
		callCtx = provider.WithServedEndpoint(callCtx, served)
		callCtx, releaseRung := errandRungContext(callCtx, len(rungs)-attempt-1)
		response, called, callErr := a.completeWithNamedModel(callCtx, callPurpose(role), messages, rung.Model, options...)
		if strings.TrimSpace(called) == "" {
			called = rung.Model
		}
		releaseRung()
		if callErr == nil && response != nil {
			// AND THE ERRAND WRITES ITS OWN CALL LINE, exactly as a step of the
			// turn does (loop.go's [Agent.addUsage]). Without it the journal's
			// call lines covered only the conversation's own requests, and a
			// measured run's lines summed to $0.123 against a real bill of $0.739
			// — the whole of the difference being three side-calls to a
			// mastermind. See [journalCall] for why that is a record worth
			// nothing and why the role rides the line.
			a.journalRoleCall(response, role, called, served.Name())
			if accept == nil || accept(response, called) {
				return response, called, nil
			}
			// AN ANSWER THE CALLER CANNOT USE IS NOT A TRANSPORT FAILURE and is
			// never asked for again from the same rung: the endpoint did its job
			// and the model said something unusable, which asking again on the
			// same model is the least likely thing to change. The ladder moves.
			lastErr = errInvalidName
			tell(errandNews{Model: rung.Model, Rung: attempt + 1, Rungs: len(rungs),
				Failed: true, Why: errandUnusableWords})
			if ctx.Err() != nil {
				return nil, called, ctx.Err()
			}
			continue
		}
		lastErr = callErr
		if lastErr == nil {
			lastErr = errEmptyAnswer
		}
		// A CANCELLED ERRAND IS NOT A FAILED ONE, and it leaves no row. When a
		// turn ends, the captions and titles it started die with `context
		// canceled`, and each was journaled as an error and then read by the
		// boundary as `class: work, the work did not come back done` — a
		// failure in every autopsy for an errand nobody was waiting on any more
		// (the 2026-09-10 transcript carries one per turn). The turn loop has
		// never journaled a request its own context cancelled (loop.go); an
		// errand keeps the same rule. A DEADLINE is still a failure and still
		// written, below: that is the caller running out of patience, which is
		// news, where a cancel is the caller walking away.
		if errors.Is(ctx.Err(), context.Canceled) {
			return nil, rung.Model, ctx.Err()
		}
		// AND THE ERRAND'S FAILURE IS WRITTEN DOWN TOO, on the same row shape a
		// step of the turn writes (loop.go's [Agent.journalFailedCall]). An
		// errand that cannot be reached is silent by design — the caller reads
		// silence as "nothing to say" — and a silence nobody records is a bill
		// with no explanation next to it. No estimate is written: an errand's
		// request is a digest this session assembled, not the transcript, and the
		// transcript's own count would be a number about something else.
		//
		// THE ROW IS WRITTEN BEFORE THE ERRAND IS ABANDONED, and that ordering is
		// the measured failure. It used to come after the check below, so an
		// errand cut by its CALLER'S deadline — the one failure that leaves the
		// caller with nothing to say and no idea why — returned having written
		// nothing at all. SWE-Marathon s4, 00:01:54Z: the mastermind that writes a
		// handed-over turn's brief was asked, [checkpointHandoffWindow] elapsed
		// ninety seconds later to the millisecond, the ladder fell to the person's
		// bare sentence, and the journal held no error row, no call row and no
		// word of why the worker started blind. A deadline is a failure like any
		// other and it is now recorded like one.
		a.journalFailedCall(callCtx, called, string(role), lastErr, attempt+1, 0)
		// AND THE BOUNDARY READS IT, on the same row shape and for the same
		// reason the turn's own failures are read: an errand cut by a deadline
		// and an errand refused by an upstream are two different pieces of news
		// and the file could not tell them apart
		// (taxonomy_boundary.go's [Agent.readErrandFailure]).
		//
		// AND NOW THE LADDER ACTS ON WHAT IT SAYS. It used to be read and
		// dropped — the rung below was the only retry an errand had, whatever
		// the failure was — which meant an endpoint under strain spent a rung
		// that a two-second pause would have got an answer out of, and an errand
		// with one rung left had no move at all. [errandAsksAgain] is where the
		// verdict is turned into this ladder's two moves.
		// AND IT IS ASKED AS A LADDER. `fallback` is the fact the boundary cannot
		// know and the whole difference between moving on and giving up: this
		// errand has a rung below, whose floor is the model already answering the
		// person's own turns. It is what turns a spent transport budget into
		// [taxonomy.ActionHop] rather than [taxonomy.ActionGiveUp].
		// AND THE BOUNDARY IS TOLD THIS RUNG IS SPENT. One request is the
		// whole of a rung, so by the time its failure is read there is
		// nothing left to ask of THIS model — which is what turns the
		// verdict into the hop the ladder below is for.
		verdict, evidence := a.readErrandFailure(lastErr, role, called,
			transportLadder{attempt: 1, outOfTime: true, fallback: attempt+1 < len(rungs)})
		tell(errandNews{Model: called, Rung: attempt + 1, Rungs: len(rungs),
			Failed: true, Why: errandFailureWords(lastErr)})
		// The person's own interrupt ends the errand where it stands, and the
		// caller is handed the context's own error so it can tell "nobody
		// answered in time" from "the provider refused" without reading the row
		// this just wrote.
		if ctx.Err() != nil {
			return nil, called, ctx.Err()
		}
		// AND SO DOES THE ERRAND'S OWN PATIENCE, for the same reason: walking a
		// ladder on a budget that is already spent is more requests that cannot
		// land.
		if errandCtx.Err() != nil {
			return nil, called, errandCtx.Err()
		}
		if !errandWalksOn(verdict, evidence, attempt+1 < len(rungs)) {
			return nil, called, lastErr
		}
	}
	return nil, "", lastErr
}

// errandReserve is the fraction of what is left of an errand that is held back
// for EACH rung still below the one being asked. A fifth: enough that the floor
// is always reachable, small enough that it is never the thing that ends a rung
// which is answering.
//
// IT IS NOT THE OLD EVEN SPLIT. That divided the whole budget by the rungs
// remaining and handed the first one half of it, which is why a stream still
// writing at ninety seconds of its hundred and eighty died. This hands the first
// rung four fifths and keeps a fifth for the floor.
const errandReserve = 5

// errandRungContext bounds one rung at what it may spend: everything left except
// what the `below` rungs under it are owed.
//
// A rung with nothing under it is handed the errand's own context unchanged,
// which is the ordinary case and pays nothing.
func errandRungContext(ctx context.Context, below int) (context.Context, context.CancelFunc) {
	deadline, ok := ctx.Deadline()
	if below <= 0 || !ok {
		return ctx, func() {}
	}
	left := time.Until(deadline)
	owed := time.Duration(below) * (left / errandReserve)
	if owed <= 0 || owed >= left {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, left-owed)
}

// errandNews is one moment of an errand's ladder, for a caller that has somebody
// watching and owes them a sentence about the wait.
//
// IT CARRIES FACTS AND ONE PLAIN-WORDS HALF, never a finished sentence. What a
// person reads is composed by the caller — the line under a task's phase word
// reads nothing like anything a title errand would ever say — and a sentence
// written here would be this file having an opinion about a surface it cannot
// see.
type errandNews struct {
	// Model is the rung being asked, Rung is its 1-based place on the ladder,
	// and Rungs how many rungs there are.
	Model string
	Rung  int
	Rungs int
	// Failed says this news is that the request above came to nothing, and Why
	// is what to tell somebody about it in their own words.
	Failed bool
	Why    string
}

// The two things that go wrong with a rung, in the words a person reads.
//
// TWO SENTENCES AND NO MORE. Somebody watching their work be sized wants to know
// whether the model is slow or gone; the provider's own sentence is a payload
// and belongs in the journal row [Agent.callRoleChecked] already writes.
const (
	errandLateWords      = "did not answer in time"
	errandUnreachedWords = "could not be reached"
	errandUnusableWords  = "answered with nothing usable"
)

// errandFailureWords is why one rung came to nothing, for a person.
func errandFailureWords(err error) string {
	if err == nil {
		return ""
	}
	if _, cut := provider.CutFrom(err); cut || errors.Is(err, context.DeadlineExceeded) {
		return errandLateWords
	}
	return errandUnreachedWords
}

// errandWalksOn reads one failed rung's verdict the way a LADDER has to read it,
// which is not the question a caller with one model asks of the same verdict.
//
// [taxonomy.ActionHop] IS THE ANSWER THIS FUNCTION EXISTS FOR: a transport
// budget spent with somewhere left to go. That "somewhere" is the fact only this
// file has — a rung below, whose floor is the model already answering the
// person's own turns — and it reaches the boundary as [transportLadder]'s
// `fallback`, which is what makes the hop reachable at all.
// [taxonomy.ActionGiveUp] is the same news with nowhere left, and it ends the
// errand.
//
// AND [taxonomy.ActionRetry] IS READ AGAINST THE ERRAND'S OWN PATIENCE RATHER
// THAN THE MODEL'S. It means the MODEL has not run out of anything, which is the
// right answer for a turn and the wrong one for an errand: one request is the
// whole of a rung here, so by the time a rung's failure is read this rung has
// nothing left. So a retry with a rung below is this ladder's hop, taken at once
// and without the backoff:
// asking a different model is what "ask again" means here, and it is more than
// [taxonomy.Verdict.Rotate] was asking for. With no rung below it is the end.
// (Honouring it as a SAME-RUNG retry was measured on 2026-09-10 and is not
// available until the transport budget can be the caller's: it fires on the
// first failure, and the pauses timed out the naming job and two turns a person
// waits through.)
//
// AND ONE FAILURE THE TAXONOMY CANNOT DECIDE ON ITS OWN. Everything it cannot
// place lands in [taxonomy.Work] — which is right for a 4XX THAT NAMED NOBODY,
// the router reading our own bytes and saying no, where the rung below is a
// second charge for the same refusal — and wrong for "that model is down", which
// arrives as a sentence nothing can classify and is the exact case the ladder was
// built for (taskname_test.go's fall-through). A class that catches both cannot
// be the gate, so the doomed request is recognised by the EVIDENCE, narrowly: the
// router reading our own bytes and saying no. Every other work verdict falls
// through one rung, as this file's header has always said it does.
//
// AND IT READS THE EVIDENCE RATHER THAN A STATUS (#854, the last entry on that
// change's `seamsOwed`). This spelled `Status >= 400 && Status < 500 && Upstream
// == ""` by hand, which was a FOURTH rule about what a 4xx means beside the three
// the one classifier had just folded into one — and a fourth rule is the defect
// that design closed. [taxonomy.Evidence.OurBytes] is exactly that sentence,
// decided once at the refusal door, and [taxonomy.Evidence.Overflow] is beside it
// because an errand cannot compact: a request that did not fit will not fit the
// rung below either.
func errandWalksOn(verdict taxonomy.Verdict, evidence taxonomy.Evidence, fallback bool) bool {
	if verdict.Class == taxonomy.Transport {
		return verdict.Action == taxonomy.ActionHop ||
			(verdict.Action == taxonomy.ActionRetry && fallback)
	}
	return !evidence.OurBytes && !evidence.Overflow
}

// ── telling somebody what the errand is doing ───────────────────────────────

type errandWatchKey struct{}

// withErrandWatch asks the ladder to say what it is doing, rung by rung, to a
// caller that has somebody waiting on the answer.
//
// IT IS A CONTEXT VALUE AND NOT AN ARGUMENT because one caller in ten wants it.
// [Agent.callRole] is the door every errand in this package passes through, and
// widening its signature for the division review would put an ignored nil at
// every other site — which is how a seam stops being read.
//
// THE WATCHER IS CALLED ON THE ERRAND'S OWN GOROUTINE, in order, before each
// request and after each failure. It must be as trivial as a stream observer is:
// anything that blocks here blocks the errand.
func withErrandWatch(ctx context.Context, tell func(errandNews)) context.Context {
	if tell == nil {
		return ctx
	}
	return context.WithValue(ctx, errandWatchKey{}, tell)
}

// errandWatchFrom is the watcher on this context, and a hand that does nothing
// on every errand nobody is watching — which is nearly all of them.
func errandWatchFrom(ctx context.Context) func(errandNews) {
	tell, _ := ctx.Value(errandWatchKey{}).(func(errandNews))
	if tell == nil {
		return func(errandNews) {}
	}
	return tell
}

// journalRoleCall writes ONE errand's own accounting down, on the same line
// shape a step of the turn writes (see [journalCall]).
//
// IT IS EVIDENCE AND NEVER SPEND, exactly as the turn's line is: the caller
// folds the money into the session's totals through [Agent.addAuxiliaryUsage],
// and the replay drops these lines rather than adding them a second time. What
// this buys is the question the summed lines could not answer — which model was
// asked what, and what that one request cost — for the half of the bill that has
// nobody's turn behind it.
//
// THE MODEL FALLS BACK TO THE RUNG. A provider that names itself in the response
// is the better answer, because it is who actually served the request; a
// provider that names nothing would otherwise leave the line saying only that
// somebody was paid, so the rung the ladder resolved stands in for it.
//
// A response that reported no usage writes nothing, which [sessionFile.appendCall]
// enforces on its own side too — the emptiness law, and a stream cut before its
// final chunk is exactly that case.
func (a *Agent) journalRoleCall(response *ai.Response, role roles.Role, rung, endpoint string) {
	if response == nil || response.Usage == nil {
		return
	}
	model := strings.TrimSpace(response.Model)
	if model == "" {
		model = strings.TrimSpace(rung)
	}
	usage := response.Usage
	a.file.appendCall(journalCall{
		Model:      model,
		Endpoint:   strings.TrimSpace(endpoint),
		Role:       string(role),
		Input:      usage.PromptTokens,
		CacheRead:  usage.CacheReadTokens(),
		CacheWrite: usage.CacheCreationTokens(),
		Output:     usage.CompletionTokens,
		CostUSD:    costOf(usage),
	})
}

// The two failures this file names itself. Both are the shape a caller has
// always handled — an error, and nothing to read — rather than anything new.
var (
	errNoCompleter = errStr("session: no model client")
	errEmptyAnswer = errStr("the model answered with nothing")
)

type errStr string

func (e errStr) Error() string { return string(e) }

// errandRole is what one of this package's errands is FOR, in the vocabulary the
// router and the phase clock share (internal/lane's roles.go).
//
// It is a table and not a stamp at each call site for the reason the roles table
// itself is one: [Agent.callRole] is the one door every errand in this package
// goes through, so naming the lane role here names it once for all of them, and
// a role added to internal/roles that nobody thought about lands on the
// conservative answer rather than on a free one.
//
// THE DEFAULT IS AUXILIARY BECAUSE THAT IS WHAT AN ERRAND IS: a side call of a
// turn, made without the turn's stream, that nobody is reading. Only two kinds
// of errand differ, and they differ in what the answer is worth rather than in
// who is waiting — a GATE reading finished work has to be right where a title
// merely has to be short, and the MEMORY reflex is charged and kept on its own
// ledger.
func errandRole(role roles.Role) lane.Role {
	switch role {
	case roles.RoleRouter, roles.RoleRouterConfirm, roles.RoleMarkReader,
		roles.RoleGuardian, roles.RoleAuditor:
		return lane.RoleJudge
	case roles.RoleReflex, roles.RoleConsolidate:
		return lane.RoleMemory
	case roles.RolePlanner, roles.RoleDesigner, roles.RoleDivision:
		return lane.RoleDesign
	}
	return lane.RoleAuxiliary
}
