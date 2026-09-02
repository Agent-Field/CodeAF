package lane

import (
	"testing"
	"time"
)

// ── THE WIRE OVERRULES THE SHEET ────────────────────────────────────────────
//
// The endpoints page says which machines a router PUBLISHES for a model. It
// does not say which machines the router will SERVE it from, and on 2026-09-01
// those were two different sets: three of five tool-capable lanes on the sheet
// were not in the serving set the completion resolved against, so a pin chosen
// from the sheet earned a 404 every time and the frontier went on offering it
// (issue #266).

// A LANE THE WIRE HAS REFUSED IS NOT A CANDIDATE. It is not slow, not derated
// and not dear — it is a machine that cannot serve this model, and a gate that
// left it in is how a pin gets chosen from a set the router has already
// emptied.
func TestALaneTheWireRefusedLeavesTheCandidateSet(t *testing.T) {
	t.Cleanup(ForgetRefusals)
	ForgetRefusals()

	const model = "vendor/model"
	belief := Belief{
		ID:    ID{Model: model, Lane: "Ghost"},
		Facts: Facts{Tools: true, Context: 128000, MaxOut: 8000, Quant: "fp8", Uptime5m: 100},
	}
	request := Request{Model: model, Tools: true, Now: time.Now()}
	if !capable(belief, request, gateOptions{}) {
		t.Fatal("a healthy published lane was refused before anything refused it")
	}

	RefuseServing(model, "Ghost")
	if capable(belief, request, gateOptions{}) {
		t.Fatal("a lane the router refused this model from is still a candidate")
	}
	// AND ONLY THAT ONE. A refusal is about a pairing, so the same machine
	// serving something else is untouched.
	other := belief
	other.ID = ID{Model: "vendor/other", Lane: "Ghost"}
	if !capable(other, Request{Model: "vendor/other", Tools: true, Now: request.Now}, gateOptions{}) {
		t.Fatal("refusing one model took the machine away from every model")
	}
}

// UNKNOWN IS YES, which is the reading every other gate in [capable] takes. This
// half of the serving set holds refusals and nothing else, so a machine nobody
// has been refused by has said nothing.
func TestALaneNobodyHasBeenRefusedByStillServes(t *testing.T) {
	t.Cleanup(ForgetRefusals)
	ForgetRefusals()
	if !Serves("vendor/model", "Haven") {
		t.Fatal("a lane with no refusal against it was treated as refused")
	}
	if !Serves("vendor/model", "") {
		t.Fatal("an unnamed lane was treated as refused, which would refuse everything")
	}
}

// THE REFUSAL IS FILED UNDER THE LEDGER KEY. The whole defect had the bare id
// and the dated slug disagreeing about who serves a model; a refusal written
// under one spelling and read under another would rebuild that disagreement one
// layer down.
func TestARefusalIsFiledUnderTheOneNameEveryLayerFoldsTo(t *testing.T) {
	t.Cleanup(func() {
		ForgetRefusals()
		UseServable(nil)
	})
	ForgetRefusals()
	// The catalog's own fold, installed the way a surface installs it: the
	// floating alias and the dated slug are one deployment.
	UseServable(func(model string) string {
		if model == "~vendor/model-latest" {
			return "vendor/model-0731"
		}
		return model
	})

	RefuseServing("~vendor/model-latest", "Ghost")
	if Serves("vendor/model-0731", "Ghost") {
		t.Fatal("a refusal collected under the alias was invisible under the slug the sheet publishes")
	}
	// And a tier suffix is not a deployment either, which is the fold's first
	// layer ([BareModel]).
	if Serves("vendor/model-0731:high", "Ghost") {
		t.Fatal("a refusal was lost across a tier suffix")
	}
}
