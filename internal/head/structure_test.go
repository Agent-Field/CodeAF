package head

import (
	"context"
	"strings"
	"testing"
)

// The scale rule used to ask "one worker or many?" and use the answer to decide
// "does anyone lay the work out at all?" — two different questions wearing one
// label. Measured live on `~deepseek/deepseek-v4-flash-latest`, that collapsed
// structure the person had SAID OUT LOUD: "plan and run this as two dependent
// steps, not one … step two must not start before step one is done" compiled to
// scale "task" three times out of three, and so did an ask that both enumerates
// and stratifies ("a short note on each one first, and then one comparison
// table … that uses all three notes"). One leaf each, no planning pass, and the
// ordering the person asked for recorded nowhere.
//
// The rule now separates the two questions it was conflating, and every phrase
// pinned here is one of the joints where they used to fuse.
func TestScaleFollowsStructureRatherThanTheNumberOfAnswers(t *testing.T) {
	for name, required := range map[string]string{
		"structure is the reading, not the count of deliverables": "it means the MAKING of this has structure",
		"a chain is structure exactly as a fan is":                "A chain is structure exactly as a fan is",
		"an ordering is planned, never remembered":                "a thing to be planned, not a thing to be remembered halfway through",
		"the reading is about the work, not its container":        "Answer both about the WORK and never about what it arrives in",
		"smallness is not indivisibility":                         "the smallness of a job is never a reason to call it \"task\"",
		"project commits to no width":                             "commits you to NOTHING about how many workers there will be",
		"the person's own steps are authority":                    "has already told you it has structure",
		"parts and scale are different questions":                 "parts and scale answer different questions and neither settles the other",
		"an empty parts list says nothing about scale":            "parts [] says nothing whatever about the scale",
		"parts cannot express an order":                           "it DELETES it",
	} {
		if !strings.Contains(compilerSystemPrompt, required) {
			t.Errorf("the compiler prompt lost %s: %q", name, required)
		}
	}
	// The whole point is that it teaches a reading rather than a catalogue of
	// asks, so no shape of work may be named anywhere in it.
	for _, forbidden := range []string{"tokenizer", "haiku", "multiplexer", "essay", "press release", "itinerary"} {
		if strings.Contains(strings.ToLower(compilerSystemPrompt), forbidden) {
			t.Errorf("the compiler prompt grew a task-shaped example: %q", forbidden)
		}
	}
}

// The prompt already said "scale is that judgement's answer, not a label chosen
// first", and measured live that is exactly what went wrong: the SAME model, on
// the same prompt and the same sentence, answered "task" when it wrote the label
// straight out and "project" both times it was additionally asked to quote the
// rule it was following. The judgement was reachable; nothing made it be reached
// first. So the reading became a field of its own, and the label follows it here
// rather than in a paragraph the model may skim.
func TestTheLabelFollowsTheReadingAndOnlyEverWidens(t *testing.T) {
	for name, want := range map[string]string{
		"the reading is asked for first": `"structure" is the FIRST field in the object`,
		"and the label must follow it":   `"scale" must be the answer that reading forces`,
		"the field opens the shape line":  `{"structure":"enumerates|stratifies|one_judgement|single_act"`,
	} {
		if !strings.Contains(compilerSystemPrompt, want) {
			t.Errorf("the compiler prompt no longer states %s: %q missing", name, want)
		}
	}

	for name, tc := range map[string]struct {
		structure, scale, want string
	}{
		"a job that enumerates is a project whatever the label said": {StructureEnumerates, ScaleTask, ScaleProject},
		"and so is one that stratifies":                              {StructureStratifies, ScaleTask, ScaleProject},
		"the judgement boundary is left exactly where it was":        {StructureOneJudgment, ScaleTask, ScaleTask},
		"and so is a genuinely single act":                           {StructureSingleAct, ScaleTask, ScaleTask},
		"a missing reading changes nothing":                          {"", ScaleTask, ScaleTask},
		"an unknown reading changes nothing":                         {"sideways", ScaleTask, ScaleTask},
		"casing and spacing are not a contradiction":                 {"  Stratifies ", ScaleTask, ScaleProject},
		// It only ever widens. A lookup's answer IS the deliverable, so there is
		// nothing to plan and a promotion would buy a pass over nothing; and a
		// label already at project cannot be widened further.
		"a lookup is never promoted":      {StructureEnumerates, ScaleLookup, ScaleLookup},
		"and a project is left untouched": {StructureSingleAct, ScaleProject, ScaleProject},
	} {
		if got := reconcileScale(tc.structure, tc.scale); got != tc.want {
			t.Errorf("%s: reconcileScale(%q, %q) = %q, want %q", name, tc.structure, tc.scale, got, tc.want)
		}
	}
}

// End to end through the compile, because a reconciliation that the Compile path
// never reaches is a function with a unit test and no effect.
func TestACompiledBriefCarriesTheWidenedScale(t *testing.T) {
	client := &fakeClient{responses: []string{
		`{"goal":"Write a note on each of the three, then one table drawing on all three.",` +
			`"title":"Three notes and a table","structure":"enumerates","scale":"task",` +
			`"parts":[],"builds_on":[],"assumptions":["one markdown file"],"trial_of":0}`,
	}}
	brief, err := NewCompiler(client).Compile(context.Background(), "a note on each, then a table", "")
	if err != nil {
		t.Fatal(err)
	}
	if brief.Scale != ScaleProject {
		t.Fatalf("a brief that read its own work as enumerating still compiled to %q", brief.Scale)
	}
	if brief.Structure != StructureEnumerates {
		t.Fatalf("the reading was lost on the way out: %q", brief.Structure)
	}
	// Widening the scale must not invent width: parts is the count of separate
	// ANSWERS and the model said there was one.
	if len(brief.Parts) != 0 {
		t.Fatalf("the reconciliation grew parts from nowhere: %v", brief.Parts)
	}
}

// max_tokens on a reasoning model is the WHOLE completion budget and the
// thinking is spent out of it before the first character of the answer. The
// floor was 1000, sized for a reply, and measured live three ordinary
// multi-part asks came back `finish_reason:"length"` with `content:null` and
// completion_tokens equal to the cap to the digit — 1106 of 1106, 1100 of 1100,
// 1124 of 1124 — with the retry at double the room doing the same. Nothing was
// truncated because nothing was ever written.
//
// The quantity was wrong, not the size: a reply's length tracks how many pieces
// the WORK has, and the ask's own length says nothing about that.
func TestTheCompileBudgetIsSizedForTheAnswerRatherThanTheAsk(t *testing.T) {
	// The shortest possible ask still gets room to think and answer, because the
	// shortest asks are exactly the ones that can name the most work.
	if got := compileReplyTokens(""); got < 6000 {
		t.Fatalf("a short ask is budgeted %d tokens, which is a cap a reasoning model cannot answer inside", got)
	}
	// And a nine-character sentence naming six deliverables is budgeted within a
	// whisker of a long one, which is the property that broke: the ask's length
	// may not be the only thing that moves the number.
	short := compileReplyTokens("six things:")
	if short < 6000 {
		t.Fatalf("a short ask naming a lot of work is budgeted %d tokens", short)
	}
	// It still breathes with the ask, because the goal echoes it verbatim.
	long := strings.Repeat("x", 9000)
	if compileReplyTokens(long) <= short {
		t.Fatalf("the budget stopped growing with the ask it has to echo")
	}
}
