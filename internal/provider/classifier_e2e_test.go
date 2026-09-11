package provider

import (
	"testing"

	lanes "github.com/Agent-Field/aforge-v2/internal/lane"
	"github.com/Agent-Field/aforge-v2/internal/lane/lanestub"
	"github.com/Agent-Field/aforge-v2/internal/taxonomy"
)

// ── FIVE REFUSALS, ONE CALL SITE, ONE VERDICT EACH ──────────────────────────
//
// These are the five shapes docs/design/recovery/DESIGN.md §7 R2 names, staged
// against a REAL router (internal/lane/lanestub) and a real [Client], because
// what is being proved is the whole road: the refusal door reading the wire into
// typed facts, [Evidence] carrying them, and [taxonomy.Classify] turning them
// into exactly one move.
//
// EACH OF THEM USED TO BE ANSWERED BY A DIFFERENT RULE, and that is why they are
// in one table rather than five files. An account exclusion was
// `refusalobject.go`'s; an emptied set was `classOf`'s; our own bytes and a
// withdrawn model were the same 404 to everybody; and an overflow was a regex in
// internal/session that ran AFTER the verdict and returned before it could be
// read. A shape added to this table is a shape somebody had to state as a fact
// first, which is the point.

// overflowEnvelope is the router relaying an OpenAI-compatible overflow: the
// sentence is prose and says nothing structural, and the `code` is the fact.
const overflowEnvelope = "This endpoint's maximum context length is 65536 tokens. " +
	"However, you requested about 71000 tokens."

// knownCatalog is a price resolver that carries every model but the one named.
// It is what makes "the router no longer carries this model" a fact rather than
// the absence of a catalog — [Client.withdrawnModel] demands the positive form.
func knownCatalog(missing string) func(string) (float64, float64, bool) {
	return func(model string) (float64, float64, bool) {
		if model == missing {
			return 0, 0, false
		}
		return 0.66e-6, 1.98e-6, true
	}
}

func TestEveryRefusalShapeProducesOneVerdictFromOneCallSite(t *testing.T) {
	limits := taxonomy.Limits{}.Floored()

	// THE FALLBACK IS THE CALLER'S FACT AND IS SAID ONCE, HERE. It is what
	// separates [taxonomy.ActionHop] from [taxonomy.ActionGiveUp] and it is not
	// the transport's to know, so every row is read with a chain available —
	// which is the ordinary case and the one the moves are named for.
	read := func(t *testing.T, err error) taxonomy.Verdict {
		t.Helper()
		if err == nil {
			t.Fatal("the router answered; this scenario stages a refusal")
		}
		evidence := Evidence(err)
		evidence.Attempt = 1
		evidence.FallbackAvailable = true
		return taxonomy.Classify(evidence, limits)
	}

	t.Run("an account policy that emptied the set is a move", func(t *testing.T) {
		// THE MEASURED ONE (2026-09-10). The account's privacy switch takes the
		// only machine the request could reach, so nobody is asked — and every
		// other machine behind the model could have answered. It reached a person
		// as `the request itself was refused`.
		rig := newLaneRig(t, "recovery/account",
			lanestub.Lane{Name: "A", Profile: lanestub.Profile{Tools: true}, AccountExcluded: true},
		)
		ctx := WithLaneChoice(talking(), lanes.Choice{Only: []string{"A"}})
		_, err := rig.client.CompleteWithMessages(ctx, userMessages("hello"))
		verdict := read(t, err)
		// IT IS THE WIRE AND IT HAS A MOVE, which is the whole of the claim. The
		// adapter has climbed its shape ladder by the time a caller holds this
		// error — the ceiling dropped, the knobs off — so the machine and the
		// shape are both spent and what is left is another model. What must
		// never happen is the reading this refusal used to get: our own bytes,
		// class WORK, turn over, with two other machines idle.
		if verdict.Class != taxonomy.Transport || !verdict.Hops() {
			t.Fatalf("an account exclusion read as %s, want the wire and another model", verdict)
		}
		if verdict.EndsTurn() {
			t.Fatalf("an account exclusion ended the turn: %s", verdict)
		}
	})

	t.Run("a list that emptied the set is a move", func(t *testing.T) {
		rig := newLaneRig(t, "recovery/ignored",
			lanestub.Lane{Name: "A", Profile: lanestub.Profile{Tools: true}},
		)
		rig.server.RefusesWith(404, "All providers have been ignored", "")
		_, err := rig.client.CompleteWithMessages(talking(), userMessages("hello"))
		verdict := read(t, err)
		if verdict.Class != taxonomy.Transport || !verdict.Hops() {
			t.Fatalf("an emptied list read as %s, want the wire and another model", verdict)
		}
		if verdict.EndsTurn() {
			t.Fatalf("an emptied list ended the turn: %s", verdict)
		}
	})

	t.Run("a 400 about our own bytes is a change of shape", func(t *testing.T) {
		rig := newLaneRig(t, "recovery/ourbytes",
			lanestub.Lane{Name: "A", Profile: lanestub.Profile{Tools: true}},
		)
		rig.server.RefusesWith(400, "messages: field required", "")
		_, err := rig.client.CompleteWithMessages(talking(), userMessages("hello"))
		verdict := read(t, err)
		if verdict.Class != taxonomy.Shape || verdict.Action != taxonomy.ActionReshape {
			t.Fatalf("our own bytes read as %s, want a change of shape", verdict)
		}
		// AND IT IS NOT THE WORK. The whole of the old reading was
		// [taxonomy.Work] — the same word a job that could not be done gets — so
		// a turn ended blaming somebody's request for their conversation.
		if verdict.Class == taxonomy.Work {
			t.Fatalf("our own bytes were read as a verdict about the work: %s", verdict)
		}
	})

	t.Run("a model the router no longer carries is a change of model", func(t *testing.T) {
		rig := newLaneRigWithPrice(t, "recovery/withdrawn", nil,
			lanestub.Lane{Name: "A", Profile: lanestub.Profile{Tools: true}},
		)
		rig.client.config.ModelPrice = knownCatalog(rig.model)
		rig.server.RefusesWith(404, "No endpoints found for "+rig.model+".", "")
		_, err := rig.client.CompleteWithMessages(talking(), userMessages("hello"))
		verdict := read(t, err)
		if verdict.Class != taxonomy.Transport || !verdict.Hops() {
			t.Fatalf("a withdrawn model read as %s, want the next model", verdict)
		}
		// AND IT SPENDS NO BUDGET ON THE WAY. There is no machine to rotate to,
		// so every attempt buys the identical 404 — which is the whole of #838.
		if verdict.Retries() {
			t.Fatalf("a withdrawn model was asked again: %s", verdict)
		}
	})

	t.Run("a request that did not fit is compaction", func(t *testing.T) {
		rig := newLaneRig(t, "recovery/overflow",
			lanestub.Lane{Name: "A", Profile: lanestub.Profile{Tools: true}},
		)
		rig.server.RefusesWith(400, overflowEnvelope, "context_length_exceeded")
		_, err := rig.client.CompleteWithMessages(talking(), userMessages("hello"))
		verdict := read(t, err)
		if verdict.Class != taxonomy.Shape || !verdict.Compacts() {
			t.Fatalf("an overflow read as %s, want the request made smaller", verdict)
		}
		// A COMPACTION IS NOT AN ENDING, which is the half a turn loop reads.
		if verdict.EndsTurn() {
			t.Fatalf("a request that can still be made smaller ended the turn: %s", verdict)
		}
		// AND THE SECOND ONE IS. Compaction is offered once per turn; a request
		// that still does not fit after it is one that is not going to.
		evidence := Evidence(err)
		evidence.Attempt, evidence.FallbackAvailable, evidence.Compacted = 1, true, true
		again := taxonomy.Classify(evidence, limits)
		if again.Class != taxonomy.Work || !again.EndsTurn() {
			t.Fatalf("a second overflow read as %s, want the honest ending", again)
		}
	})
}

// TestTheOverflowCodeIsReadBeforeItsSentence holds the structural half of the
// overflow reading: the `code` answers on its own, so a router that rewords the
// sentence tomorrow is still understood.
//
// The prose arm survives as a HINT and is asked last ([overflowRefusal]) — which
// is the reverse of the regex this replaced, where the sentence was the whole
// test and ran in internal/session after the verdict had been computed.
func TestTheOverflowCodeIsReadBeforeItsSentence(t *testing.T) {
	if !overflowRefusal(400, "context_length_exceeded", "something nobody has seen before") {
		t.Fatal("the envelope's own code did not answer for an overflow")
	}
	if !overflowRefusal(413, "", "") {
		t.Fatal("a request-too-large status did not answer for an overflow")
	}
	if overflowRefusal(404, "", "No endpoints found matching your data policy") {
		t.Fatal("an emptied endpoint set was read as a request that did not fit")
	}
	if !overflowRefusal(400, "", "This endpoint's maximum context length is 65536 tokens") {
		t.Fatal("the hint arm stopped catching a body that carries no code at all")
	}
}
