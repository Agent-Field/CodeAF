package subharness

import "strings"

// WHICH MODEL A RUN RIDES, AND WHO IS ALLOWED TO SAY.
//
// An agent.loop already names its model: `model` is one of its fields
// (kinds.go), and a harness whose second step must be the big model says so on
// the page, where it is read by whoever opens the harness. That is the model
// choice this package was built with, and it is a PROPERTY OF THE PROGRAM.
//
// [ModelExecOpts.Model] is the other one, and it belongs to the RUN: a person
// said "research the pricing tiers with opus", and the model they named is a
// fact about this run of the harness rather than about the harness itself
// (internal/session's harness.go reads the sentence; cmd/aforge hands the answer
// down). Nothing on the page changes, nothing is saved, and the next run of the
// same harness is the ordinary one again.
//
// SO THERE ARE THREE ANSWERS AND THEY ARE ORDERED, NARROWEST FIRST:
//
//	the node's own `model`   — this step must be this model, whatever else is true
//	the run's model          — this run was asked for, and the page did not care
//	the client's model       — what the surface built the bridge on
//
// A node that names a model beats the run because the page is the place a
// requirement gets written down: a verify step pinned to a cheap model, a
// drafting step pinned to a strong one, are decisions the harness's author made
// about the harness working at all, and a person naming a model for one run did
// not mean to overrule them.
//
// ── WHY IT IS THE NODE AND NOT THE CLIENT ──
//
// The obvious shape is to pin the run's model onto the provider client and let
// every call ride it. It cannot be done from here: [provider.Client] holds its
// key, its base URL and its measured endpoints privately and exposes no way to
// rebuild one with a different model, so a helper outside that package can only
// hand back the client it was given. The node is where the same decision is
// expressible without inventing a second client — [modelEnv.say] already sends
// `fields.model` when a node carries one — and it has the better property
// anyway: the override lands on exactly the kind it is defined for, and the
// pinning law above falls out of one `if` rather than out of two layers of
// default.
//
// ── AND WHY ONLY agent.loop ──
//
// The other calls the bridge makes are JUDGEMENTS — a verify's verdict, a
// free-text condition's yes or no — and they are asked of the client's own
// model on purpose (exec_model.go says so where they are made). A run that was
// asked to think with an expensive model was not asked to pay that model to
// answer YES or NO, and a person naming a model for the WORK has said nothing
// about who marks it.

// withModel is the run override applied to one node on its way to the executor.
//
// It answers the node UNCHANGED for everything the override does not reach — no
// override, a kind that is not an agent.loop, a node that pinned its own model —
// so the ordinary run allocates nothing and behaves exactly as it did before
// this file existed.
//
// The fields are COPIED before the model is written. [Fields] is a map and the
// node it came from is the saved program's own: writing through it would mean a
// run mutating the harness in memory, so the second run of a harness in one
// process would find the first run's model already on the page.
func withModel(node Node, model string) Node {
	if model = strings.TrimSpace(model); model == "" {
		return node
	}
	if node.Kind != KindAgentLoop {
		return node
	}
	// THE NODE'S OWN MODEL WINS. See the ordering at the top of this file: a
	// step pinned on the page is a requirement of the harness, and the run's
	// model is a preference about the run.
	if node.Fields.Get("model") != "" {
		return node
	}
	fields := make(Fields, len(node.Fields)+1)
	for name, value := range node.Fields {
		fields[name] = value
	}
	fields["model"] = model
	node.Fields = fields
	return node
}
