package session

// WHAT THE WORKER IS TAUGHT ABOUT HOW TO SPEND ITS TIME, AND WHY IT IS TAUGHT
// AS A PRINCIPLE.
//
// Two unattended runs of the same brief, measured side by side. One met a
// reading of zero and went hunting for what every count had in common inside
// two minutes; it was off zero eleven minutes later. The other answered the same
// zero by reading its own work for twelve minutes, took the reading four times
// in thirty-seven, spent well over half of its calls changing things it had
// never measured, and built from scratch a component that already existed in a
// form it could have used — without ever spending the one step it would have
// cost to ask.
//
// So prompts/task.md carries three principles, and this test holds them to being
// principles. A prompt that taught the CASE instead — a language, a tool, a file
// name, a kind of work — would be a prompt that is wrong for whatever the next
// brief turns out to be, and aforge's workers are handed every kind of work
// there is. The same reason shape.md names no domain (task_shape.go states it).

import (
	"strings"
	"testing"
)

// TestTheTaskPromptTeachesTheMeasureIsTheLoop pins the three laws by their own
// words. The failure names the missing sentence, because a lane that reworded
// one has no other way to see which.
func TestTheTaskPromptTeachesTheMeasureIsTheLoop(t *testing.T) {
	for _, want := range []string{
		// (a) the check is the loop, and the interval shrinks at zero.
		"WHEN THE WORK COMES WITH ITS OWN MEASURE, THE MEASURE IS THE LOOP, NOT THE\nREPORT",
		"THE INTERVAL BETWEEN TWO READINGS IS YOUR UNIT OF WORK",
		"SHRINKS when the reading is zero",
		"changed but never\nmeasured is not progress",
		// (b) one zero everywhere is one shared fault.
		"NOTHING ON EVERY COUNT IS ONE SHARED FAULT, NOT MANY SEPARATE ONES",
		"FIND THAT SHARED PATH AND PROVE IT CARRIES\nONE CASE END TO END BEFORE YOU TOUCH ANY SINGLE PART",
		// (c) ask whether it exists before making it.
		"BEFORE YOU MAKE A THING YOURSELF, SPEND ONE STEP ASKING WHETHER IT ALREADY\nEXISTS IN A FORM YOU CAN USE",
		"THE COST OF ASKING IS ONE STEP; THE COST OF NOT ASKING IS THE WHOLE THING",
	} {
		if !strings.Contains(taskPrompt, want) {
			t.Errorf("prompts/task.md does not say %q", want)
		}
	}
}

// AND IT TEACHES THEM WITHOUT NAMING A TRADE. aforge is handed prose, research,
// data, operations and code by the same door, and a worker reading a law written
// in one trade's nouns reads a law that is not about the job in front of it. So
// the passage carries no vocabulary from any of them, and no number either — a
// number in a prompt is a target a model optimises against, and the work does
// not come with its size written on it (proportion_test.go states that half).
func TestTheTimePassageIsAPrincipleAndNamesNoTrade(t *testing.T) {
	taught := section(taskPrompt, "## How you spend the time", "\n## ")
	if taught == "" {
		t.Fatal("prompts/task.md no longer carries the passage at all")
	}
	for _, trade := range []string{
		"compile", "build system", "crate", "library", "package manager",
		"parser", "test suite", "unit test", "scorer", "benchmark",
		"repository", "codebase", "function", "class", "api",
	} {
		if strings.Contains(strings.ToLower(taught), trade) {
			t.Errorf("the passage names %q, so it teaches one trade's work rather than any work:\n%s", trade, taught)
		}
	}
	if at := strings.IndexAny(taught, "0123456789"); at >= 0 {
		t.Errorf("the passage carries a number, which reads as a target rather than a habit:\n%s", taught)
	}
}
