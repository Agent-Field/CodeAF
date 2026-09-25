package tui3

import (
	"strings"

	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/subharness"
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
const questionDemoEnv = "CODEAF_QUESTION_DEMO"

// openDemoQuestion raises one of the fixtures where the environment names it.
func (a *app) openDemoQuestion(env func(string) string) {
	if env == nil {
		return
	}
	name := strings.TrimSpace(env(questionDemoEnv))
	if name == "" {
		return
	}
	if stage, known := stagedDemos[name]; known {
		// A FIXTURE THAT NEEDS SOMETHING IN THE TRANSCRIPT STAGES IT FIRST. A
		// permission is ABOUT a call, and the panel reads the call's own row
		// rather than repeating it (questionpanel.go's law: two renderings of one
		// command is how a person approves something other than what they read),
		// so a fixture that skipped the row would be a picture of the panel's
		// fallback rather than of the panel.
		a.raiseQuestion(questionShown{question: stage(a)})
		_ = a.questionRows(a.width)
		for i := range a.questions {
			a.questions[i].shown = a.questions[i].shown.Add(-questionSettle)
		}
		return
	}
	if stage, known := setDemos[name]; known {
		// AND SEVERAL QUESTIONS FROM ONE STEP, on the same terms: a set is
		// raised by a model calling `ask` more than once in one batch, or by a
		// batch of calls that each needs an approval — both of which cost a
		// real turn to see.
		for _, q := range stage(a) {
			a.raiseQuestion(questionShown{question: q})
		}
		_ = a.questionRows(a.width)
		for i := range a.questions {
			a.questions[i].shown = a.questions[i].shown.Add(-questionSettle)
		}
		return
	}
	if build, known := blockDemos[name]; known {
		// AND THE BLOCK'S OWN FIXTURES, on the same terms and for the same
		// reason. The five lanes that moved onto it in the questions wave — the
		// standing card, the harness offer, a finished design, the connect offer
		// and the key it asks for — are each raised by MINUTES of real work: a
		// reminder proposed by a turn, a design written by a model, an account
		// the session reached for. A screen capture that has to pay for all of
		// that first is a screen capture nobody takes twice, and a renderer
		// nobody has looked at is not done.
		a.raiseQuestion(questionShown{question: build()})
		// The DRAW is what stamps a question as seen, and the guard is measured
		// from the stamp — so the fixture spends it up front, exactly as the
		// page's own fixtures do below.
		_ = a.questionRows(a.width)
		for i := range a.questions {
			a.questions[i].shown = a.questions[i].shown.Add(-questionSettle)
		}
		return
	}
	build, known := questionDemos[name]
	if !known {
		return
	}
	q := build()
	a.raiseQuestionRoom(questionShown{question: q, shown: a.now(), pick: questionPointerStart(q)})
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

// ── the block's five, one per lane the questions wave moved ─────────────────

// blockDemos is one fixture per lane that moved onto the block, named by the
// case its screen is filed under. They build the question through the ENGINE'S
// OWN BUILDERS wherever there is one, so a fixture cannot drift from the thing
// it is a picture of.
var blockDemos = map[string]func() session.Question{
	// THE EVIDENCE ON THE PANEL (lane R): the page's own worked example and its
	// layout case, raised onto the block instead of the page, so the list with
	// the evidence beside it — and, narrower, unfolded under the pointer — can be
	// seen without a model.
	"evidence":        demoQuestionReading,
	"evidence-layout": demoQuestionLayout,
	"standing":        demoStandingCard,
	"harness-offer":   demoHarnessOffer,
	"design":          demoHarnessDesign,
	"connect":         demoConnectOffer,
	"connect-key":     demoConnectKey,
}

// demoStandingCard is the reminder a turn proposed: a choice with four answers
// and a correction lane under it.
func demoStandingCard() session.Question {
	return session.Question{
		ID:     1,
		Kind:   session.QuestionStanding,
		Ask:    session.AskChoice,
		Form:   session.FormCard,
		Asker:  session.Asker{Kind: session.AskerModel},
		Head:   session.StandingHeadCheck,
		Reason: session.StandingAskReason,
		Stakes: session.StakesReversible,
		// THE WORDS ARE THE CARD'S OWN CONSTANTS and never a second spelling of
		// them (standing.go, and the law a test in this package holds): a fixture
		// that said the answers itself would be a picture of a card this program
		// does not draw.
		Options: []session.AnswerOption{
			{Key: standYesKey, Label: standYesWord, Consequence: standYesCost},
			{Key: session.StandingOnceKey, Label: standOnceWord, Consequence: standOnceCost},
			{Key: session.StandingNoKey, Label: standNoWordChip, Safe: true, Consequence: standNoCost},
		},
		Input: session.InputShape{Kind: session.InputText, Prompt: standChangeCost},
		Scope: []session.AnswerScope{session.ScopeOnce, session.ScopeAlways},
	}
}

// demoHarnessOffer is the offer to run a saved program: one line, two answers.
func demoHarnessOffer() session.Question {
	return session.HarnessQuestion(2, session.Event{
		Kind: session.EventHarnessOffer, ID: 2, Text: "research",
		Model: "claude-opus-5", Hint: "finds an answer across sources and cites them",
	})
}

// demoHarnessDesign is a finished page waiting to be judged: three answers, and
// the page itself down in the conversation.
func demoHarnessDesign() session.Question {
	page := subharness.Harness{Id: subharness.Id{
		Name: "research-helper", Desc: "Research a topic with cited sources",
	}}
	return session.HarnessQuestion(3, session.Event{
		Kind: session.EventHarnessDesignDone, ID: 3, Text: "research a topic", Harness: &page,
	})
}

// demoConnectOffer is the account the turn reached for, in a browser.
func demoConnectOffer() session.Question {
	return session.ConnectQuestion("demo-google", "Google", false, "", false)
}

// demoConnectKey is the same question about an account that has no sign-in page:
// no yes to press, a way out, and the box under it collecting the key.
func demoConnectKey() session.Question {
	return session.ConnectQuestion("demo-notion", "Notion", true, "paste your Notion key", true)
}

// ── the three the surface raises about something already on screen ──────────

// stagedDemos are the fixtures that need a row in the transcript before the
// question means anything. They are given the app rather than returning a bare
// question, because what a permission is ABOUT is a call that is already drawn.
var stagedDemos = map[string]func(*app) session.Question{
	"permission":   demoPermission,
	"irreversible": demoIrreversible,
	"weighed":      demoWeighed,
}

// demoPermission is the ordinary gate: a command a person has to read, the tool
// that wants it named in the frame's top edge as an aside, and the three answers
// this engine offers for one.
func demoPermission(a *app) session.Question {
	return a.demoConsent("bash", "rm -rf build/", session.StakesCostly,
		[]session.AnswerScope{session.ScopeOnce, session.ScopeProject, session.ScopeAlways})
}

// demoIrreversible is the same gate over a call that cannot be taken back: the
// pointer opens on the answer that loses nothing, there is no clock, and
// `always` is not offered at all.
func demoIrreversible(a *app) session.Question {
	q := a.demoConsent("bash", "git push --force origin main", session.StakesIrreversible,
		[]session.AnswerScope{session.ScopeOnce})
	// The widening answer goes with the scope that offered it, which is the
	// engine's own narrowing where the memo would do nothing (consent.go).
	kept := q.Options[:0:0]
	for _, option := range q.Options {
		if option.Widening {
			continue
		}
		kept = append(kept, option)
	}
	q.Options = kept
	return q
}

// demoConsent is the shape both gates share, built from the engine's own
// answers for this kind so a fixture cannot drift from the thing it pictures.
func (a *app) demoConsent(tool, command string, stakes session.Stakes, scope []session.AnswerScope) session.Question {
	call := "demo-call-" + tool
	a.entries = append(a.entries, entry{
		kind: entryTool, tool: tool, text: tool + " " + command, turn: a.turn,
		status: toolConsent, stale: true, callID: call,
	})
	return session.Question{
		ID: 7, Kind: session.QuestionConsent, Ask: session.AskPermission,
		Form: session.FormLine, Asker: session.Asker{Kind: session.AskerEngine},
		Head:     "run a command in your project",
		Reason:   "this shape of command is not on the allow list",
		Subject:  session.SubjectRef{Kind: session.SubjectCall, CallID: call, Name: tool},
		Options:  session.AnswerOptions(session.QuestionConsent),
		Stakes:   stakes,
		Blocking: session.Blocking{Turn: true},
		Scope:    scope,
	}
}

// setDemos are the fixtures for several questions one step raised
// (questionset.go): the tabs and their review, and the one permission frame.
var setDemos = map[string]func(*app) []session.Question{
	"several":     demoSeveral,
	"permissions": demoPermissions,
}

// demoSeveral is three questions a model asked in one batch — one drawing
// they all share, a tab each and the review after them.
func demoSeveral(a *app) []session.Question {
	storage := demoWeighed(a)
	storage.ID, storage.Batch = 21, "demo-step"
	naming := session.Question{
		ID: 22, Kind: session.QuestionAsk, Ask: session.AskChoice, Batch: "demo-step",
		Asker:  session.Asker{Kind: session.AskerModel},
		Head:   "what should the index file be called?",
		Stakes: session.StakesReversible,
		Options: []session.AnswerOption{
			{Key: "1", Label: "session-index.db", Consequence: "says what it is"},
			{Key: "2", Label: "index.db", Consequence: "short, and there is only one"},
		},
		Pick: &session.Pick{Key: "1"},
	}
	tests := session.Question{
		ID: 23, Kind: session.QuestionAsk, Ask: session.AskChoice, Batch: "demo-step",
		Asker:  session.Asker{Kind: session.AskerModel},
		Head:   "which tests should cover it?",
		Stakes: session.StakesReversible,
		Options: []session.AnswerOption{
			{Key: "1", Label: "a crash test", Consequence: "kills the write half-way and reopens"},
			{Key: "2", Label: "round trip only", Consequence: "write, read back, compare"},
		},
	}
	return []session.Question{storage, naming, tests}
}

// demoPermissions is four reads one batch asked approval for — the grouped
// frame, with each call's row already in the transcript as a real gate's is.
func demoPermissions(a *app) []session.Question {
	out := make([]session.Question, 0, 4)
	for i, target := range []string{"~/notes/plan.md", "~/notes/todo.md", "~/notes/ideas.md", "~/notes/log.md"} {
		q := a.demoConsent("read", target, session.StakesCostly,
			[]session.AnswerScope{session.ScopeOnce, session.ScopeAlways})
		call := "demo-call-read-" + itoa(i)
		a.entries[len(a.entries)-1].callID = call
		q.ID, q.Batch, q.Subject.CallID = uint64(31+i), "demo-step", call
		q.Head = "needs your ok to run read"
		out = append(out, q)
	}
	return out
}

// demoWeighed is the ordinary panel: a choice whose answers carry what each one
// costs, with a pick, its reason, its confidence and what would change its mind.
// It is the drawing most questions in this program get, and it had no fixture.
func demoWeighed(*app) session.Question {
	return session.Question{
		ID: 8, Kind: session.QuestionAsk, Ask: session.AskChoice,
		Asker:  session.Asker{Kind: session.AskerModel},
		Head:   "which store should the session index sit on?",
		Reason: "a schema change is next and it is cheaper before there are rows",
		Stakes: session.StakesReversible,
		Options: []session.AnswerOption{
			{Key: "1", Label: "SQLite", Consequence: "one file beside the conversation · already a dependency"},
			{Key: "2", Label: "JSONL", Consequence: "append-only · nothing new to build against"},
			{Key: "3", Label: "BoltDB", Consequence: "fastest reads · one more dependency to carry"},
		},
		Pick: &session.Pick{
			Key: "1", Reason: "it survives a crash mid-write and the rest do not",
			Confidence: session.ConfidenceFairly, WouldChange: "the index ever has to be read from another machine",
		},
	}
}
