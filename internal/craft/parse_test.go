package craft

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

// presentationYAML is the shape the whole package is written against: an
// outline, a fan-out over what the outline produced, a draft, a check that
// buys revision rounds, and an export. Every other fixture here is this one
// with one law broken.
const presentationYAML = `name: presentation
description: Turn a topic into a presentation — a slide deck with speaker notes.
params:
  - name: topic
    description: What the deck is about.
    required: true
  - name: slides
    description: How many slides the deck should hold.
    default: "10"
steps:
  - id: outline
    brief: |-
      Draft a {{slides}}-slide outline for a presentation about {{topic}}.
      One slide title per line, nothing else.
    model: work
  - id: research
    brief: Gather the two strongest supporting facts for this slide of the {{topic}} deck, each with a source.
    needs:
      - outline
    for_each:
      source: outline
      fan: 6
  - id: draft
    brief: Write the deck from the outline and the research, speaker notes under every slide.
    needs:
      - outline
      - research
    model: boost
    skill: deckbuild
  - id: check
    needs:
      - draft
    verify:
      script: verifiers/deck_shape.sh
      until_pass:
        revise:
          - draft
        max_rounds: 3
  - id: export
    brief: Export the approved deck to PDF and say where it landed.
    needs:
      - check
limits:
  cost_usd: 4
  minutes: 45
`

func parseValid(t *testing.T, data string) *Workflow {
	t.Helper()
	w, err := Parse([]byte(data))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if problems := w.Validate(); len(problems) > 0 {
		t.Fatalf("validate: %v", problems)
	}
	return w
}

func TestParseReadsThePresentationShapeAndSurvivesARoundTrip(t *testing.T) {
	w := parseValid(t, presentationYAML)

	if w.Name != "presentation" || len(w.Steps) != 5 || len(w.Params) != 2 {
		t.Fatalf("parsed %s with %d steps and %d params", w.Name, len(w.Steps), len(w.Params))
	}
	if w.Limits.CostUSD != 4 || w.Limits.WallClock != 45*time.Minute {
		t.Fatalf("limits parsed as %v / %v", w.Limits.CostUSD, w.Limits.WallClock)
	}
	if w.Commit != "" {
		t.Fatalf("a file stamped its own commit: %q", w.Commit)
	}
	research := w.Steps[1]
	if research.ForEach == nil || research.ForEach.Source != "outline" || research.ForEach.Fan != 6 {
		t.Fatalf("for_each parsed as %+v", research.ForEach)
	}
	check := w.Steps[3]
	if check.Verify == nil || check.Verify.Script != "verifiers/deck_shape.sh" {
		t.Fatalf("verify parsed as %+v", check.Verify)
	}
	if check.Verify.UntilPass == nil || check.Verify.UntilPass.MaxRounds != 3 ||
		!reflect.DeepEqual(check.Verify.UntilPass.Revise, []string{"draft"}) {
		t.Fatalf("until_pass parsed as %+v", check.Verify.UntilPass)
	}
	if w.Steps[2].Skill != "deckbuild" || w.Steps[2].Model != "boost" {
		t.Fatalf("draft parsed as %+v", w.Steps[2])
	}

	data, err := w.Marshal()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	again := parseValid(t, string(data))
	if !reflect.DeepEqual(w, again) {
		t.Fatalf("round trip changed the workflow\nfirst:  %+v\nsecond: %+v\nfile:\n%s", w, again, data)
	}
	// The marshal is the diff the user reads, so it keeps types.go's order.
	if got := string(data); !strings.HasPrefix(got, "name: presentation\ndescription:") {
		t.Fatalf("marshal did not lead with name then description:\n%s", got)
	}
}

func TestParseRefusesShapeItCannotMean(t *testing.T) {
	for _, probe := range []struct {
		name string
		yaml string
		want string
	}{
		{"empty file", "   \n", "the file is empty"},
		{"comments only", "# nothing here\n", "no document"},
		{"unknown step field", "name: x\nsteps:\n  - id: a\n    breif: hi\n", "unknown step field breif — did you mean brief?"},
		{"unknown workflow field", "name: x\ndescriptoin: hi\nsteps: []\n", "unknown workflow field descriptoin — did you mean description?"},
		{"unknown limits field", "name: x\nsteps: []\nlimits:\n  cost_us: 1\n", "unknown limits field cost_us — did you mean cost_usd?"},
		{"steps is not a list", "name: x\nsteps: nope\n", "line 2: expected a list of steps here, found text (nope)"},
		{"needs is not a list", "name: x\nsteps:\n  - id: a\n    brief: b\n    needs: outline\n", "expected a list of names here, found text (outline)"},
		{"broken yaml", "name: x\nsteps: [\n", "craft: yaml: line 2:"},
	} {
		t.Run(probe.name, func(t *testing.T) {
			_, err := Parse([]byte(probe.yaml))
			if err == nil {
				t.Fatalf("parsed a file that should not parse")
			}
			if !strings.Contains(err.Error(), probe.want) {
				t.Fatalf("error was %q, wanted it to contain %q", err, probe.want)
			}
		})
	}
}

// Every structural law, each with the sentence it is supposed to say. The
// error text is asserted because the reader is a model repairing a file: a law
// whose message drifts into vagueness has stopped doing its job even while it
// still refuses the file.
func TestValidateNamesTheStepAndSaysWhatIsWrong(t *testing.T) {
	for _, probe := range []struct {
		name string
		yaml string
		want string
	}{
		{
			"unknown need suggests the near miss",
			"name: deck\nsteps:\n  - id: outline\n    brief: a\n  - id: draft\n    brief: b\n    needs: [outine]\n",
			"step draft: needs unknown step outine — did you mean outline?",
		},
		{
			"unknown need with nothing close suggests nothing",
			"name: deck\nsteps:\n  - id: outline\n    brief: a\n  - id: draft\n    brief: b\n    needs: [zzzzzzz]\n",
			"step draft: needs unknown step zzzzzzz\n",
		},
		{
			"duplicate step id",
			"name: deck\nsteps:\n  - id: draft\n    brief: a\n  - id: draft\n    brief: b\n",
			"step draft: duplicate id",
		},
		{
			"a step with neither brief nor verify",
			"name: deck\nsteps:\n  - id: draft\n    model: work\n",
			"step draft: has neither a brief nor a verify",
		},
		{
			"a step with both",
			"name: deck\nsteps:\n  - id: draft\n    brief: a\n    verify:\n      script: verifiers/x.sh\n",
			"step draft: has both a brief and a verify",
		},
		{
			"a dependency cycle",
			"name: deck\nsteps:\n  - id: a\n    brief: a\n    needs: [c]\n  - id: b\n    brief: b\n    needs: [a]\n  - id: c\n    brief: c\n    needs: [b]\n",
			"dependency cycle a → c → b → a",
		},
		{
			"a verifier outside verifiers/",
			"name: deck\nsteps:\n  - id: check\n    verify:\n      script: scripts/x.sh\n",
			`step check: verify.script "scripts/x.sh" must live under verifiers/`,
		},
		{
			"a verifier that climbs out",
			"name: deck\nsteps:\n  - id: check\n    verify:\n      script: verifiers/../../etc/x.sh\n",
			`must not climb out of verifiers/ with ..`,
		},
		{
			"an absolute verifier",
			"name: deck\nsteps:\n  - id: check\n    verify:\n      script: /usr/bin/true\n",
			"must be repo-relative, not absolute",
		},
		{
			"for_each over an unknown step",
			"name: deck\nsteps:\n  - id: outline\n    brief: a\n  - id: research\n    brief: b\n    needs: [outline]\n    for_each:\n      source: outlin\n",
			"step research: for_each.source names unknown step outlin — did you mean outline?",
		},
		{
			"for_each over a step it does not need",
			"name: deck\nsteps:\n  - id: outline\n    brief: a\n  - id: research\n    brief: b\n    for_each:\n      source: outline\n",
			"for_each.source names outline but the step does not need it",
		},
		{
			"a for_each that is also a verify",
			"name: deck\nsteps:\n  - id: outline\n    brief: a\n  - id: check\n    needs: [outline]\n    for_each:\n      source: outline\n    verify:\n      script: verifiers/x.sh\n",
			"step check: is both a for_each and a verify",
		},
		{
			"until_pass revising an unknown step",
			"name: deck\nsteps:\n  - id: draft\n    brief: a\n  - id: check\n    needs: [draft]\n    verify:\n      script: verifiers/x.sh\n      until_pass:\n        revise: [draf]\n",
			"step check: until_pass.revise names unknown step draf — did you mean draft?",
		},
		{
			"until_pass revising work the check never read",
			"name: deck\nsteps:\n  - id: draft\n    brief: a\n  - id: aside\n    brief: b\n  - id: check\n    needs: [draft]\n    verify:\n      script: verifiers/x.sh\n      until_pass:\n        revise: [aside]\n",
			"until_pass.revise names aside, which the check does not depend on",
		},
		{
			"a brief referencing an undeclared param",
			"name: deck\nparams:\n  - name: topic\nsteps:\n  - id: draft\n    brief: write about {{topc}}\n",
			"step draft: brief references {{topc}}, which no param declares — did you mean topic?",
		},
		{
			"a param declared twice",
			"name: deck\nparams:\n  - name: topic\n  - name: topic\nsteps:\n  - id: draft\n    brief: a\n",
			"param topic: declared twice",
		},
		{
			"a required param carrying a default",
			"name: deck\nparams:\n  - name: topic\n    required: true\n    default: weather\nsteps:\n  - id: draft\n    brief: a\n",
			"param topic: required but carries a default",
		},
		{
			"a skill given as a path",
			"name: deck\nsteps:\n  - id: draft\n    brief: a\n    skill: ../../bin/rm\n",
			`step draft: skill "../../bin/rm" must be a plain executable name from skills/`,
		},
		{
			"a name that is not a slug",
			"name: My Deck\nsteps:\n  - id: draft\n    brief: a\n",
			`workflow "My Deck": name must be lowercase letters, digits, dash or underscore`,
		},
		{
			"no steps at all",
			"name: deck\nsteps: []\n",
			"workflow: no steps",
		},
	} {
		t.Run(probe.name, func(t *testing.T) {
			w, err := Parse([]byte(probe.yaml))
			if err != nil {
				t.Fatalf("fixture did not parse: %v", err)
			}
			problems := w.Validate()
			if len(problems) == 0 {
				t.Fatalf("validated a file that breaks a law")
			}
			var said strings.Builder
			for _, problem := range problems {
				said.WriteString(problem.Error())
				said.WriteString("\n")
			}
			if !strings.Contains(said.String(), probe.want) {
				t.Fatalf("errors were:\n%swanted one containing %q", said.String(), probe.want)
			}
		})
	}
}

func TestValidateRefusesAShapeBiggerThanAPlan(t *testing.T) {
	var file strings.Builder
	file.WriteString("name: sprawl\nsteps:\n")
	for i := 0; i <= MaxSteps; i++ {
		file.WriteString("  - id: s")
		file.WriteString(string(rune('a' + i%26)))
		file.WriteString(strings.Repeat("x", i/26+1))
		file.WriteString("\n    brief: work\n")
	}
	w, err := Parse([]byte(file.String()))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	found := false
	for _, problem := range w.Validate() {
		if strings.Contains(problem.Error(), "past the ceiling of 24") {
			found = true
		}
	}
	if !found {
		t.Fatalf("%d steps were accepted", len(w.Steps))
	}
}

func TestClampHoldsTheCeilingsAndIsIdempotent(t *testing.T) {
	greedy := `name: greedy
steps:
  - id: outline
    brief: a
  - id: research
    brief: b
    needs: [outline]
    for_each:
      source: outline
      fan: 100
  - id: check
    needs: [research]
    verify:
      script: verifiers/x.sh
      until_pass:
        max_rounds: 99
limits:
  cost_usd: 500
  minutes: 6000
`
	w := parseValid(t, greedy)
	w.Clamp()
	if w.Limits.CostUSD != MaxRunBudgetUSD || w.Limits.WallClock != MaxWallClock {
		t.Fatalf("limits clamped to %v / %v", w.Limits.CostUSD, w.Limits.WallClock)
	}
	if w.Steps[1].ForEach.Fan != MaxFanCap {
		t.Fatalf("fan clamped to %d", w.Steps[1].ForEach.Fan)
	}
	until := w.Steps[2].Verify.UntilPass
	if until.MaxRounds != MaxRounds {
		t.Fatalf("rounds clamped to %d", until.MaxRounds)
	}
	// An empty revise means the check's direct needs, written out once here so
	// no reader of a compiled workflow has to know the rule.
	if !reflect.DeepEqual(until.Revise, []string{"research"}) {
		t.Fatalf("revise defaulted to %v", until.Revise)
	}

	before, err := w.Marshal()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	w.Clamp()
	after, err := w.Marshal()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(before) != string(after) {
		t.Fatalf("clamp is not idempotent:\n%s\n---\n%s", before, after)
	}
}

func TestClampFillsTheSilentFileWithDefaults(t *testing.T) {
	bare := `name: bare
steps:
  - id: outline
    brief: a
  - id: research
    brief: b
    needs: [outline]
    for_each:
      source: outline
  - id: check
    needs: [research]
    verify:
      script: verifiers/x.sh
      until_pass: {}
`
	w := parseValid(t, bare)
	w.Clamp()
	if w.Limits.CostUSD != DefaultRunBudgetUSD || w.Limits.WallClock != DefaultWallClock {
		t.Fatalf("limits defaulted to %v / %v", w.Limits.CostUSD, w.Limits.WallClock)
	}
	if w.Steps[1].ForEach.Fan != DefaultFanCap {
		t.Fatalf("fan defaulted to %d", w.Steps[1].ForEach.Fan)
	}
	if w.Steps[2].Verify.UntilPass.MaxRounds != DefaultMaxRounds {
		t.Fatalf("rounds defaulted to %d", w.Steps[2].Verify.UntilPass.MaxRounds)
	}
}

func TestFillReportsEveryMissingParamAtOnce(t *testing.T) {
	w := parseValid(t, `name: deck
params:
  - name: topic
    required: true
  - name: audience
    required: true
  - name: slides
    default: "10"
steps:
  - id: draft
    brief: write {{slides}} slides about {{topic}} for {{audience}}
`)

	values, err := w.Fill(map[string]string{"topic": "Q3", "audience": "the board"})
	if err != nil {
		t.Fatalf("fill: %v", err)
	}
	if values["slides"] != "10" || values["topic"] != "Q3" {
		t.Fatalf("filled %v", values)
	}
	if got := Substitute(w.Steps[0].Brief, values); got != "write 10 slides about Q3 for the board" {
		t.Fatalf("substituted to %q", got)
	}

	_, err = w.Fill(nil)
	if err == nil {
		t.Fatal("filled a workflow with no params given")
	}
	// Both at once: a model told about one missing param at a time invents the
	// rest rather than asking twice.
	if !strings.Contains(err.Error(), "missing required params: topic, audience") {
		t.Fatalf("error was %q", err)
	}

	_, err = w.Fill(map[string]string{"topci": "Q3", "audience": "the board"})
	if err == nil || !strings.Contains(err.Error(), "missing required param: topic (you passed topci)") {
		t.Fatalf("near-miss key reported as %v", err)
	}
}

// One id law. A step id becomes a node id by being slugged, and the compiler
// refuses any step whose id does not survive that unchanged — so a file this
// package accepts must be a file that compiles, or the distiller forges a
// workflow, commits it, announces it, and it fails forever with nobody
// watching.
func TestStepIDLawIsTheCompilersLaw(t *testing.T) {
	for _, probe := range []struct {
		id    string
		valid bool
	}{
		{"outline", true},
		{"first-draft", true},
		{"step2", true},
		{"step_one", false},
		{"first--draft", false},
		{"draft-", false},
		{"-draft", false},
		{"Draft", false},
		{"first draft", false},
		{"", false},
		{strings.Repeat("a", MaxIDBytes), true},
		{strings.Repeat("a", MaxIDBytes+1), false},
	} {
		if ValidStepID(probe.id) != probe.valid {
			t.Errorf("ValidStepID(%q) = %t", probe.id, !probe.valid)
		}
		w := &Workflow{Name: "deck", Steps: []Step{{ID: probe.id, Brief: "work"}}}
		refused := false
		for _, problem := range w.Validate() {
			// An empty id is refused a sentence earlier, by the law that every
			// step is named at all.
			if strings.Contains(problem.Error(), "it becomes a node id") ||
				strings.Contains(problem.Error(), "id is empty") {
				refused = true
			}
		}
		if refused == probe.valid {
			t.Errorf("Validate refused=%t for step id %q", refused, probe.id)
		}
	}
}

// The refusal is written for the model that will fix it: what the law is, and
// the id it probably meant.
func TestAnUnderscoredStepIDIsRefusedWithTheIDItMeant(t *testing.T) {
	w, err := Parse([]byte("name: deck\nsteps:\n  - id: step_one\n    brief: a\n"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	problems := w.Validate()
	if len(problems) == 0 {
		t.Fatalf("a step id the compiler always refuses validated")
	}
	if !strings.Contains(problems[0].Error(), "did you mean step-one?") {
		t.Fatalf("the refusal does not say what to write: %v", problems[0])
	}
}
