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

// A page that is never re-emitted cannot be retyped wrong, which is the whole
// argument for review-by-patch. This is that argument as a test: a critic that
// changes one brief leaves every other byte of the draft alone.
func TestAPatchedPageKeepsTheDraftsOwnBytes(t *testing.T) {
	draft := subharness.Harness{
		Id: subharness.Id{Name: "voice", Desc: "compare three ways to add voice input"},
		Program: subharness.Program{
			Nodes: []subharness.Node{
				{Id: "survey", Kind: subharness.KindAgentLoop, Fields: subharness.Fields{
					"brief": "read the three options — local whisper.cpp, a cloud STT API, push-to-talk via an external app",
				}},
				{Id: "score", Kind: subharness.KindAgentLoop, Fields: subharness.Fields{"brief": "score them"}},
			},
			Edges: []subharness.Edge{{"survey", "score"}},
		},
		Verify: subharness.Verify{Ladder: subharness.VerifyAccept},
		Dyn:    subharness.Dyn{Ladder: subharness.DynFixed},
	}
	revised, results, err := subharness.ApplyReport(draft, []subharness.Op{
		{Op: subharness.OpReplaceBrief, Node: "score", Text: "score each option on latency, cost, privacy and implementation risk"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || !results[0].Applied() {
		t.Fatalf("results = %+v", results)
	}
	survey, _ := revised.Program.Node("survey")
	if !strings.Contains(survey.Fields.Get("brief"), "whisper.cpp") {
		t.Errorf("the untouched brief changed: %q", survey.Fields.Get("brief"))
	}
}

// The critic's optional fields mean "the draft's, unchanged" — the same law as
// the ops: text it is not changing is not in its reply at all.
func TestARevisionKeepsWhatItDidNotRestate(t *testing.T) {
	draft := design{Cues: []string{"event log"}, Justification: strings.Repeat("x", 500)}

	kept := revision{}.design(draft)
	if len(kept.Cues) != 1 || kept.Cues[0] != "event log" || kept.Justification != draft.Justification {
		t.Errorf("an omitted field did not fall back to the draft: %+v", kept)
	}

	changed := revision{Cues: []string{"journal"}, Justification: "a new derivation"}.design(draft)
	if len(changed.Cues) != 1 || changed.Cues[0] != "journal" || changed.Justification != "a new derivation" {
		t.Errorf("a restated field did not replace the draft's: %+v", changed)
	}
}

// The three passes are the guide's own and they print in the guide's order even
// when the critic writes them in another; a pass label nobody asked for is kept
// rather than dropped, because a finding is a finding.
func TestFindingsGroupByPassInTheGuidesOrder(t *testing.T) {
	r := revision{Findings: []finding{
		{Pass: "quality", Text: "a rung the program cannot keep"},
		{Pass: "speed", Text: "two lanes drawn in a line"},
		{Pass: "taste", Text: "an opinion"},
	}}
	got := r.byPass()
	if len(got) != 4 {
		t.Fatalf("passes = %+v", got)
	}
	for at, want := range []string{"speed", "cost", "quality", "taste"} {
		if got[at].Name != want {
			t.Errorf("pass %d = %q, want %q", at, got[at].Name, want)
		}
	}
	if len(got[1].Found) != 0 {
		t.Errorf("cost found something nobody said: %v", got[1].Found)
	}
	if r.findings() != 3 {
		t.Errorf("findings = %d", r.findings())
	}
}

// The critic owes a finding on every verify; this is the mechanical half of the
// same question, and it must tell the two shapes apart without reading a word of
// the review. A verify with a branch on its outcome can send a failure somewhere;
// one with a single ordinary successor, or none at all, cannot.
func TestVerifyPathsNamesTheStrandedChecks(t *testing.T) {
	h := subharness.Harness{
		Program: subharness.Program{Nodes: []subharness.Node{
			{Id: "work", Kind: subharness.KindAgentLoop, Fields: subharness.Fields{"brief": "do it"}},
			// Forked: a failure has an arm of its own.
			{Id: "gate", Kind: subharness.KindVerify, Fields: subharness.Fields{"check": "the scores agree"}},
			{Id: "fork", Kind: subharness.KindBranch, Fields: subharness.Fields{"when": "failed"}},
			{Id: "rework", Kind: subharness.KindAgentLoop, Fields: subharness.Fields{"brief": "fix it"}},
			// Walked through: the same node runs on either verdict.
			{Id: "second", Kind: subharness.KindVerify, Fields: subharness.Fields{"check": "it reads well"}},
			// Last: a failure ends the run with the work done.
			{Id: "deliver", Kind: subharness.KindAgentLoop, Fields: subharness.Fields{"brief": "hand it over"}},
			{Id: "last", Kind: subharness.KindVerify, Fields: subharness.Fields{"check": "nothing is missing"}},
		}, Edges: []subharness.Edge{
			{"work", "gate"}, {"gate", "fork"},
			{"fork", "rework"}, {"fork", "second"}, {"rework", "second"},
			{"second", "deliver"}, {"deliver", "last"},
		}},
	}
	got := verifyPaths(h)
	for _, want := range []string{
		`verify gate: forked at "fork" on "failed"`,
		`verify second: STRANDED — "deliver" runs whether it passed or failed`,
		`verify last: STRANDED — it is a last node`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("verifyPaths did not say %q:\n%s", want, got)
		}
	}

	if none := verifyPaths(subharness.Harness{Program: subharness.Program{Nodes: []subharness.Node{
		{Id: "only", Kind: subharness.KindAgentLoop, Fields: subharness.Fields{"brief": "write it"}},
	}}}); !strings.Contains(none, "checks nothing of its own") {
		t.Errorf("a program with no verify reported %q", none)
	}
}

// Both duties are in what the critic is actually shown, with the ops each one is
// supposed to reach for — a duty that lives only in a design note is a duty the
// model never reads.
func TestTheReviewerBriefCarriesBothDuties(t *testing.T) {
	reviewer, err := reviewSystem(availableTools)
	if err != nil {
		t.Fatal(err)
	}
	for _, must := range []string{
		"FAN-OUT CANDIDATE", "parallel.split", "parallel.join",
		"STRANDED VERIFY", subharness.KindBranch, subharness.DynWidth,
	} {
		if !strings.Contains(reviewer, must) {
			t.Errorf("the reviewer brief never says %q", must)
		}
	}
}

// The transport rule is in what the model is actually shown, in both stages —
// not just in the document one of them reads.
func TestBothBriefsSayTheSyntaxIsASCII(t *testing.T) {
	for _, guide := range []struct {
		name  string
		build func([]toolSpec) (string, error)
	}{
		{"designer", designerSystem},
		{"reviewer", reviewSystem},
	} {
		text, err := guide.build(availableTools)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(text, "ASCII") {
			t.Errorf("%s: nothing in the brief says the syntax is ASCII", guide.name)
		}
	}
}

// A reply that arrives fenced, smart-quoted or trailing-comma'd is a punctuation
// problem and this rig will not spend a design attempt on one.
func TestTheDesignEnvelopeSurvivesAMangledReply(t *testing.T) {
	raw := "Here you go:\n```json\n{“cues”: [“event log”], “justification”: “because”, “harness”: {“id”: {“name”: “journal”}},}\n```"
	salvaged, err := subharness.SalvageDetail(raw)
	if err != nil {
		t.Fatal(err)
	}
	var envelope design
	if err := strict(salvaged.JSON, &envelope); err != nil {
		t.Fatalf("the salvaged envelope will not decode: %v", err)
	}
	if envelope.Justification != "because" || len(envelope.Harness) == 0 {
		t.Errorf("envelope = %+v", envelope)
	}
}
