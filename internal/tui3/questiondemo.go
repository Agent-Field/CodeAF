package tui3

import (
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// THE FIXTURE DOOR ONTO THE QUESTION PAGE, and why one exists at all.
//
// A question with a diagram under each answer, four axes to compare on and a
// dial for an input shape is a question NOTHING IN THIS PROGRAM RAISES YET: the
// tool that lets a model ask one is lane E2's and the cards that promote into
// this page are lane S1's, so on the day this page landed there was no way to
// put a real one on a real screen. The alternative to this file was a page whose
// author had only ever seen it through a test's string comparison, and
// CLAUDE.md's `make demo-home` exists because the owner has already ruled on
// that trade once: a fixture with something on every place is how you SEE a page
// full, and a page nobody has looked at is not done.
//
// SO IT IS THE SAME SHAPE AS `make demo-home` AND UNDER THE SAME TERMS. It is
// reached only by naming a case in the environment, it is read through
// [Options.Env] like every other environment fact this surface takes, it writes
// nothing anywhere, and it is not a person-facing feature: there is no key, no
// slash command and no row that mentions it. It goes when E2's `ask` tool and
// S1's cards give the page real questions to draw.

// questionDemoEnv is the variable that names a case. Its values are the case
// names in [questionDemos].
const questionDemoEnv = "AFORGE_QUESTION_DEMO"

// openDemoQuestion raises one of the fixtures where the environment names it.
func (a *app) openDemoQuestion(env func(string) string) {
	if env == nil {
		return
	}
	name := strings.TrimSpace(env(questionDemoEnv))
	if name == "" {
		return
	}
	build, known := questionDemos[name]
	if !known {
		return
	}
	a.openQuestionRoom(build())
	// THE SETTLE GUARD IS SPENT BEFORE THE FIRST FRAME on a fixture, and only on
	// a fixture. It exists to protect a person from a page that appeared under a
	// hand already moving; a page raised by the launch itself appeared under
	// nobody, and a screen capture that has to sleep a quarter of a second first
	// is a screen capture whose timing is part of the test.
	a.qroom.shown = a.qroom.shown.Add(-questionSettle)
}

// questionDemos is one fixture per form this page draws, named by the case the
// screen is filed under.
var questionDemos = map[string]func() session.Question{
	"reading":   demoQuestionReading,
	"compare":   demoQuestionReading,
	"comment":   demoQuestionReading,
	"composing": demoQuestionReading,
	"decide":    demoQuestionReading,
	"blanks":    demoQuestionBlanks,
	"checklist": demoQuestionChecklist,
	"pairs":     demoQuestionPairs,
	"dial":      demoQuestionDial,
	"layout":    demoQuestionLayout,
}

// demoQuestionReading is the page's own worked example: a choice with three
// answers, bodies, consequence lines, a diagram, dimensions to compare on, and a
// pick with a reason, a confidence and what would change its mind.
func demoQuestionReading() session.Question {
	return session.Question{
		ID:     1,
		Kind:   session.QuestionTask,
		Ask:    session.AskChoice,
		Form:   session.FormRoom,
		Asker:  session.Asker{Kind: session.AskerModel},
		Head:   "which store should the ledger sit on?",
		Reason: "a schema change is next and it is cheaper before there are rows",
		Stakes: session.StakesCostly,
		Blocking: session.Blocking{
			Turn: true,
		},
		Scope: []session.AnswerScope{session.ScopeOnce, session.ScopeProject},
		Pick: &session.Pick{
			Key:         "1",
			Reason:      "because it is the only store the reporting job already reads",
			Confidence:  session.ConfidenceFairly,
			WouldChange: "the ledger ever has to run on a machine with no server on it",
		},
		Options: []session.AnswerOption{
			{
				Key: "1", Label: "postgres",
				Body:        "Rows already carry a foreign key into it and the migration is one file.\n+ one place to back up\n- another service to run locally",
				Dimensions:  map[string]string{"runs on": "a server", "backing up": "one dump", "reporting": "reads it directly"},
				Blocks:      []session.Block{{Kind: session.BlockDiagram, Title: "what it would look like", Body: "  app --> pg --> report"}},
				Consequence: "the ledger and the rest of the project share one connection",
			},
			{
				Key: "2", Label: "sqlite beside the project",
				Body:        "One file in the repository's own folder.\n+ nothing to run\n- the reporting job reads a copy",
				Dimensions:  map[string]string{"runs on": "the file", "backing up": "copy the file", "reporting": "reads a copy"},
				Consequence: "the ledger travels with the checkout",
			},
			{
				Key: "3", Label: "a file per day",
				Body:        "Append-only, one file a day, nothing to migrate ever.\n+ nothing to migrate\n- every question about it is a script",
				Dimensions:  map[string]string{"runs on": "the disk", "backing up": "copy the folder", "reporting": "a script each time"},
				Consequence: "the ledger is readable with cat and nothing else",
			},
		},
	}
}

// demoQuestionBlanks is a sentence with holes in it, of three different kinds.
func demoQuestionBlanks() session.Question {
	return session.Question{
		ID: 2, Kind: session.QuestionTask, Ask: session.AskClarification, Form: session.FormRoom,
		Asker:  session.Asker{Kind: session.AskerModel},
		Head:   "where should the notes land, and under what name?",
		Reason: "the folder you named last time is gone",
		Stakes: session.StakesReversible,
		Input: session.InputShape{
			Kind:   session.InputBlanks,
			Prompt: "land them in {folder} as {name} and keep the old copy: {keep}",
			Blanks: []session.Blank{
				{Label: "folder", Kind: session.BlankPath, Default: "~/notes"},
				{Label: "name", Kind: session.BlankText, Default: "a new file"},
				{Label: "keep", Kind: session.BlankChoice, Default: "no", Choices: []string{"no", "yes"}},
			},
		},
	}
}

// demoQuestionChecklist is several answers at once, with an order that matters.
func demoQuestionChecklist() session.Question {
	return session.Question{
		ID: 3, Kind: session.QuestionTask, Ask: session.AskChoice, Form: session.FormRoom,
		Asker:  session.Asker{Kind: session.AskerModel},
		Head:   "which of these should go in this pass?",
		Reason: "they touch the same three files and doing them apart means three rebases",
		Stakes: session.StakesReversible,
		Pick:   &session.Pick{Key: "1", Reason: "the imports have to move before anything else compiles"},
		Input:  session.InputShape{Kind: session.InputChecklist, Prompt: "everything ticked goes in one commit."},
		Options: []session.AnswerOption{
			{Key: "1", Label: "rewrite the imports", Safe: true},
			{Key: "2", Label: "move the tests beside them", Safe: true},
			{Key: "3", Label: "delete the old package"},
			{Key: "4", Label: "rename the module"},
		},
	}
}

// demoQuestionPairs is a run of this-or-that.
func demoQuestionPairs() session.Question {
	return session.Question{
		ID: 4, Kind: session.QuestionTask, Ask: session.AskJudgement, Form: session.FormRoom,
		Asker:  session.Asker{Kind: session.AskerModel},
		Head:   "when these pull against each other, which way?",
		Reason: "three of the four decisions left in this task come down to one of these",
		Stakes: session.StakesReversible,
		Input: session.InputShape{
			Kind:   session.InputPairs,
			Prompt: "which matters more here?",
			Blanks: []session.Blank{
				{Label: "speed against completeness", Choices: []string{"finishing tonight", "keeping every old row"}},
				{Label: "cost against certainty", Choices: []string{"one cheap pass", "two passes and a check"}},
				{Label: "now against later", Choices: []string{"ship it and patch", "get it right first"}},
			},
		},
	}
}

// demoQuestionDial is a setting with a sentence under it.
func demoQuestionDial() session.Question {
	return session.Question{
		ID: 5, Kind: session.QuestionStanding, Ask: session.AskJudgement, Form: session.FormRoom,
		Asker:  session.Asker{Kind: session.AskerEngine},
		Head:   "how much may it decide on its own here?",
		Reason: "you have taken its pick on the last four questions in this project",
		Stakes: session.StakesReversible,
		Input: session.InputShape{
			Kind:   session.InputDial,
			Prompt: "for questions in this project",
			Dial: &session.Dial{
				Min: 0, Max: 2, Default: 1,
				Labels: []string{"ask me everything", "tell me, then act", "just do it"},
			},
		},
	}
}

// demoQuestionLayout is two answers whose evidence is a pair of pre-formatted
// panes — the block kind that has a width of its own opinion.
func demoQuestionLayout() session.Question {
	return session.Question{
		ID: 6, Kind: session.QuestionTask, Ask: session.AskChoice, Form: session.FormRoom,
		Asker:  session.Asker{Kind: session.AskerModel},
		Head:   "which of these two layouts for the status line?",
		Reason: "both fit at eighty columns and they cut different things first",
		Stakes: session.StakesReversible,
		Pick:   &session.Pick{Key: "1", Reason: "the model name is what people look for", Confidence: session.ConfidenceSure},
		Options: []session.AnswerOption{
			{
				Key: "1", Label: "money on the right",
				Blocks: []session.Block{{Kind: session.BlockLayout, Title: "wide, then narrow", Rows: [][]string{
					{"deepseek-v4-flash", "  84k of 128k", "  $0.14"},
					{"deepseek-v4-fl...", "  84k", "  $0.14"},
				}}},
			},
			{
				Key: "2", Label: "money under the model",
				Blocks: []session.Block{{Kind: session.BlockDiff, Title: "what moves", Body: "- model  context  money\n+ model  money\n+ context"}},
			},
		},
	}
}
