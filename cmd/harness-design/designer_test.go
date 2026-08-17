package main

import (
	"strconv"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/subharness"
	"github.com/Agent-Field/aforge-v2/internal/subharness/prompts"
)

// The guide is an asset now, so the thing that used to be a compile error — a
// brief that has drifted from the package it describes — is a runtime one. These
// tests are where that error is meant to be found: at `go test`, not in a run
// somebody paid for.

func TestTheGuidesRender(t *testing.T) {
	for _, guide := range []struct {
		name  string
		build func([]toolSpec) (string, error)
	}{
		{"designer", designerSystem},
		{"reviewer", reviewSystem},
	} {
		text, err := guide.build(availableTools)
		if err != nil {
			t.Fatalf("%s: %v", guide.name, err)
		}
		if strings.ContainsAny(text, "«»") {
			t.Errorf("%s: a placeholder survived rendering", guide.name)
		}
		// The runtime's own placeholder must NOT have been eaten by the guide's:
		// a tool.call's args say {{input}} and the designer has to be told so.
		if !strings.Contains(text, "{{input}}") {
			t.Errorf("%s: the {{input}} the runtime substitutes is no longer described", guide.name)
		}
		for _, must := range []string{
			strconv.Itoa(subharness.MaxNodes),
			strconv.Itoa(subharness.MaxDynCap),
			strings.Join(subharness.VerifyLadder(), " < "),
			strings.Join(subharness.DynLadder(), " < "),
			availableTools[0].name,
		} {
			if !strings.Contains(text, must) {
				t.Errorf("%s: this build's %q is not in the rendered guide", guide.name, must)
			}
		}
	}
}

func TestReviewerCarriesTheWholeLaw(t *testing.T) {
	designer, err := designerSystem(availableTools)
	if err != nil {
		t.Fatal(err)
	}
	reviewer, err := reviewSystem(availableTools)
	if err != nil {
		t.Fatal(err)
	}
	// A critic judging its recollection of the law rather than the law is the one
	// failure this arrangement is built to prevent.
	if !strings.HasPrefix(reviewer, designer) {
		t.Fatal("the reviewer brief does not begin with the whole designer guide")
	}
	if !strings.Contains(reviewer, "PART FOUR") {
		t.Fatal("the reviewer brief carries no review pass")
	}
}

// machinery is the one place this binary's numbers meet that document, so a
// number the guide stopped quoting must fail here rather than quietly stop being
// told to the model. (That Render refuses drift in either direction is the
// prompts package's own test; this one is that THESE values are the ones the
// REAL guide reads.)
func TestMachineryIsExactlyWhatTheGuidesRead(t *testing.T) {
	values := machinery(availableTools)
	for name := range values {
		partial := map[string]string{}
		for key, value := range values {
			if key != name {
				partial[key] = value
			}
		}
		if _, err := prompts.Render(prompts.Designer, partial); err == nil {
			t.Errorf("the guide no longer reads «%s», so machinery supplies it for nothing", name)
		}
	}
}

func TestEstimateCallsCountsWhatCostsMoney(t *testing.T) {
	h := subharness.Harness{Program: subharness.Program{Nodes: []subharness.Node{
		// One turn: no tools, so nothing makes it ask for a second.
		{Id: "a", Kind: subharness.KindAgentLoop, Fields: subharness.Fields{"brief": "x", "max_turns": "4"}},
		// Four: it has a tool and may reach for it.
		{Id: "b", Kind: subharness.KindAgentLoop, Fields: subharness.Fields{"brief": "x", "tools": "echo", "max_turns": "4"}},
		{Id: "c", Kind: subharness.KindVerify, Fields: subharness.Fields{"check": "x"}},
		// Free: the condition language decides it.
		{Id: "d", Kind: subharness.KindBranch, Fields: subharness.Fields{"when": "ok"}},
		// A model call: a sentence is a judgement.
		{Id: "e", Kind: subharness.KindBranch, Fields: subharness.Fields{"when": "the memo names its sources"}},
		// Costs nothing — no model is asked anything.
		{Id: "f", Kind: subharness.KindToolCall, Fields: subharness.Fields{"tool": "echo"}},
		{Id: "g", Kind: subharness.KindHumanGate, Fields: subharness.Fields{"ask": "ok?"}},
	}}}
	if got, want := estimateCalls(h), 1+4+1+0+1+0+0; got != want {
		t.Errorf("estimateCalls = %d, want %d", got, want)
	}
}

func TestDiffReadsThePagesNotTheClaim(t *testing.T) {
	before := subharness.Harness{
		Id:      subharness.Id{Name: "x", Desc: "a thing"},
		Program: subharness.Program{Nodes: []subharness.Node{{Id: "a", Kind: subharness.KindAgentLoop, Fields: subharness.Fields{"brief": "one"}}}},
		Verify:  subharness.Verify{Ladder: subharness.VerifyAdversarial},
		Dyn:     subharness.Dyn{Ladder: subharness.DynFixed},
	}
	if d := diff(before, before); !d.empty() {
		t.Fatalf("a page differs from itself: %+v", d)
	}

	after := before
	after.Verify = subharness.Verify{Ladder: subharness.VerifySchema}
	after.Program = subharness.Program{Nodes: []subharness.Node{
		{Id: "a", Kind: subharness.KindAgentLoop, Fields: subharness.Fields{"brief": "two"}},
		{Id: "b", Kind: subharness.KindVerify, Fields: subharness.Fields{"check": "y"}},
	}}
	d := diff(before, after)
	if len(d.AddedNodes) != 1 || !strings.HasPrefix(d.AddedNodes[0], "b ") {
		t.Errorf("added nodes = %v", d.AddedNodes)
	}
	if len(d.Rebriefed) != 1 {
		t.Errorf("field changes = %v", d.Rebriefed)
	}
	if !strings.Contains(d.Verify, "lowered") {
		t.Errorf("verify delta = %q, want it to say the rung was lowered", d.Verify)
	}
}
