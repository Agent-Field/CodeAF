package session

// WHAT A SURFACE IS TAUGHT ABOUT HOW TO SPEND ITS TIME, WHY IT IS TAUGHT AS A
// PRINCIPLE, AND WHY BOTH SURFACES READ THE SAME WORDS.
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
// So three principles are taught, and this file holds them to being principles.
// A prompt that taught the CASE instead — a language, a tool, a file name, a
// kind of work — would be a prompt that is wrong for whatever the next brief
// turns out to be, and aforge's workers are handed every kind of work there is.
// The same reason shape.md names no domain (task_shape.go states it).
//
// AND IT HOLDS THEM TO BEING TAUGHT WHERE THE APPROACH IS ACTUALLY PICKED.
// Twelve further runs said the approach — and with it the whole outcome — is
// settled in the first couple of minutes of the CONVERSATION, before any task
// exists: the runs whose chat spent one step asking whether the thing already
// existed reached a real result three times out of three, and the runs whose
// chat set about making it by hand reached one none of five times in four hours.
// The principles were on the worker's page alone, so the surface that was
// deciding never read them. They are now in prompts/discipline.md, substituted
// into prompts/system.md, which the conversation and every worker both read —
// ONE wording, ONE copy each (prompt.go's [disciplinePrompt] states the law).

import (
	"strings"
	"testing"
)

// workerSystem is the prompt a task node actually reads: prompts/system.md with
// the shared discipline in it, plus prompts/task.md appended. It is rendered
// rather than read off a variable because the assembly is the thing under test —
// a fragment that stopped being substituted would still sit in its own file.
func workerSystem(t *testing.T) string {
	t.Helper()
	session, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	return renderSystem(Config{
		Workspace: t.TempDir(), InTask: true, tasker: session.graph(), taskDepth: 1,
	})
}

// chatSystem is the prompt the conversation reads: the same page with no task
// page under it.
func chatSystem(t *testing.T) string {
	t.Helper()
	return renderSystem(Config{Workspace: t.TempDir()})
}

// TestTheTaskPromptTeachesTheMeasureIsTheLoop pins the three laws by their own
// words, in the prompt a worker is given. The failure names the missing
// sentence, because a lane that reworded one has no other way to see which.
func TestTheTaskPromptTeachesTheMeasureIsTheLoop(t *testing.T) {
	worker := workerSystem(t)
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
		if !strings.Contains(worker, want) {
			t.Errorf("the prompt a task reads does not say %q", want)
		}
	}
}

// TestTheChatIsTaughtTheSameWorkingDisciplineAsTheWorker is the law the twelve
// runs bought: THE SURFACE THAT PICKS THE APPROACH CARRIES THE DISCIPLINE FOR
// PICKING IT. The chat picks it — before there is a task to pick it for — so a
// discipline the chat cannot read is a discipline that arrives after the
// decision it was meant to shape.
//
// It is asserted on the two ASSEMBLED prompts and not on the file, because one
// wording in one file is worth nothing if a surface never has it substituted in.
func TestTheChatIsTaughtTheSameWorkingDisciplineAsTheWorker(t *testing.T) {
	fragment := strings.TrimRight(disciplinePrompt, "\n")
	if fragment == "" {
		t.Fatal("prompts/discipline.md is empty")
	}
	for _, surface := range []struct {
		name     string
		rendered string
	}{
		{"the conversation", chatSystem(t)},
		{"a task", workerSystem(t)},
	} {
		// EXACTLY ONCE. Absent is the defect this test was written for; twice is
		// the other one — a paragraph of the fixed prefix bought twice on every
		// request of every turn (prefixbudget_test.go weighs it), and a law with
		// two places to drift apart from.
		if count := strings.Count(surface.rendered, fragment); count != 1 {
			t.Errorf("the working discipline appears %d times in the prompt %s reads, not once",
				count, surface.name)
		}
		if !strings.Contains(surface.rendered, "How you spend the time") {
			t.Errorf("the prompt %s reads never heads the discipline", surface.name)
		}
	}

	// AND THE TWO ARE THE SAME BYTES. Nothing may reword one surface's copy: the
	// section each reads is cut out of its own rendered prompt and compared.
	chat := section(chatSystem(t), "# How you spend the time", "\n# ")
	worker := section(workerSystem(t), "# How you spend the time", "\n# ")
	if chat == "" || worker == "" {
		t.Fatal("one of the two prompts no longer carries the passage at all")
	}
	if chat != worker {
		t.Errorf("the chat and the worker are taught different words:\n--- chat ---\n%s\n--- worker ---\n%s",
			chat, worker)
	}
}

// TestTheDeliverableFilePathLawIsSaidOnce is the same one-wording law the
// discipline fragment already obeys, applied to the deliverable-file path
// rule. prompts/system.md is read by every surface this package renders, so a
// second copy in prompts/task.md is a paragraph every worker paid for twice
// (prefixbudget_test.go weighs the assembled prompt) and a law with two
// places to drift apart from.
//
// The needle is the idea, not the ALL-CAPS house-rule line: the restatement
// that used to live in task.md said "full absolute path" in different words,
// and counting only the system.md wording would have called that once.
func TestTheWorkerPromptStatesTheDeliverableFilePathLawOnce(t *testing.T) {
	const law = "full absolute path"
	chat := chatSystem(t)
	worker := workerSystem(t)
	if !strings.Contains(strings.ToLower(chat), law) {
		t.Error("the conversation prompt lost the deliverable-file path law")
	}
	// EXACTLY ONCE. Absent is the defect this test was written for; twice is
	// the other one — a paragraph of the fixed prefix bought twice on every
	// request of every turn, and a law with two places to drift apart from.
	if count := strings.Count(strings.ToLower(worker), law); count != 1 {
		t.Errorf("the deliverable-file path law appears %d times in the prompt a worker reads, not once", count)
	}
	// THE DECLARATION DOOR IS A DIFFERENT LAW and stays on the task page:
	// declaredFiles / declarationWord read a last line spelled exactly
	// `files:`. Dropping the path restatement must not take that with it.
	if !strings.Contains(worker, "    files: path/one.go, path/two.json") {
		t.Error("the worker prompt lost the files: declaration door")
	}
}

// AND IT TEACHES THEM WITHOUT NAMING A TRADE. aforge is handed prose, research,
// data, operations and code by the same door, and a surface reading a law written
// in one trade's nouns reads a law that is not about the job in front of it. So
// the passage carries no vocabulary from any of them, and no number either — a
// number in a prompt is a target a model optimises against, and the work does
// not come with its size written on it (proportion_test.go states that half).
func TestTheTimePassageIsAPrincipleAndNamesNoTrade(t *testing.T) {
	taught := section(chatSystem(t), "# How you spend the time", "\n# ")
	if taught == "" {
		t.Fatal("the prompt no longer carries the passage at all")
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
