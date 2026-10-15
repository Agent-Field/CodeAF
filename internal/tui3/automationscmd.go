package tui3

// automationscmd.go is `/automations` typed: the bare command opens the place,
// and the exact forms after it add, change, run, pause, resume or delete one
// with no model in between (internal/automation's command.go is the grammar).
//
// A TYPED ADD OR EDIT STILL ENDS ON A CARD. Nothing is saved until the person
// says so, whichever of the three doors they came through — the conversation,
// this command, or `e` on the list — so the line is read, applied, and drawn as
// the same card the model's proposal draws, with the same facts on it. Only the
// answer differs: it is this window's to apply, through the seam, rather than an
// engine's to hear.
//
// RUN, PAUSE, RESUME AND DELETE DO NOT ASK. They are typed out on purpose with
// the automation's id in them, which is the confirmation; the receipt says what
// was done.

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/automation"
	"github.com/Agent-Field/codeaf/internal/session"
)

// typedAutomationKind is the question a typed automation's card asks. It is the
// surface's own question, answered here ([questionShown.local]).
const typedAutomationKind session.QuestionKind = "surface-automation"

// typedAutomationID keeps this window's card ids out of the engine's: an
// engine's proposals count up from one, so a typed card counts from far above
// anything a conversation could reach, and the transcript can pair either kind
// of question with its own card by id alone (question.go).
func typedAutomationID(seq uint64) uint64 { return 1<<40 + seq }

// The words a typed card says. An edit's head and answers are the engine's
// own ([session.AutomationChangeHead], [session.AutomationOptions]), because a
// change asked for in the conversation raises the same card and the two must
// not drift apart.
const (
	typedAddHead    = "save this automation?"
	typedEditHead   = session.AutomationChangeHead
	typedSaveLabel  = "Save"
	typedNoLabel    = "Don't save"
	typedNoSays     = "Nothing is saved."
	typedSaveSays   = "It runs on this schedule while codeaf is open."
	typedReason     = "you typed it, so nothing is saved until you say so"
	typedNoAutoWord = "no automation has the id "
	typedSavedWord  = "saved "
)

// automationsCommand is `/automations` and everything after it.
func (a *app) automationsCommand(rest string) tea.Cmd {
	cmd, err := automation.ParseCommand(rest)
	if err != nil {
		a.note(err.Error())
		return nil
	}
	if cmd.Verb == automation.VerbList {
		return a.openAutomations()
	}
	if !a.autos.on() {
		a.note(autoNoStoreWord)
		return nil
	}
	if cmd.Verb == automation.VerbAdd {
		base := automation.Automation{
			Workspace: a.automationWorkspace(),
			Schedule:  automation.Schedule{Zone: a.autos.Zone},
			Origin:    automation.Origin{Transcript: a.file},
		}
		now := a.now()
		item, err := cmd.Apply(base, now)
		if err != nil {
			a.note(err.Error())
			return nil
		}
		next, _ := item.Schedule.First(now)
		a.confirmTypedAutomation(item, next, false)
		return nil
	}
	item, ok := a.automationByID(cmd.ID)
	if !ok {
		a.note(typedNoAutoWord + cmd.ID)
		return nil
	}
	switch cmd.Verb {
	case automation.VerbEdit:
		now := a.now()
		edited, err := cmd.Apply(item, now)
		if err != nil {
			a.note(err.Error())
			return nil
		}
		// THE CARD'S NEXT RUN IS THE ONE THE SAVE WILL GIVE IT, by the rule the
		// conversation's change keeps: none for a finished one whose moment
		// stays, none while paused, the one it has for a schedule left alone.
		next, err := automation.NextAfterChange(item, edited, now)
		if err != nil {
			a.note(err.Error())
			return nil
		}
		a.confirmTypedAutomation(edited, next, true)
		return nil
	case automation.VerbRun:
		return a.runAutomationNow(item)
	case automation.VerbPause:
		return a.setAutomationStatus(item, automation.StatusPaused)
	case automation.VerbResume:
		return a.setAutomationStatus(item, automation.StatusActive)
	case automation.VerbDelete:
		return a.deleteAutomation(item)
	}
	return nil
}

// automationWorkspace is where a typed automation runs unless it names a
// folder: the project this window is in, and otherwise the home folder — of the
// machine the automation is kept on, which over --host is not this one, so a
// window attached elsewhere sends `~` and the far store reads it there
// (internal/automation's [automation.ExpandHome]).
func (a *app) automationWorkspace() string {
	if workspace := strings.TrimSpace(a.workspace); workspace != "" {
		return workspace
	}
	if a.hosted() {
		return "~"
	}
	return errandHomeDir()
}

// automationByID finds an automation by its id, or by the start of it when only
// one id starts that way and it is at least [automationIDPrefixMin] long —
// nobody types sixteen hex digits to pause something, and the id reaches the
// box whole from `→` then `e` on its row.
//
// A ONE-CHARACTER START IS NOT AN ID. `delete` asks nothing when it is typed,
// because typing the id is the confirmation, and a single digit that happened
// to start one automation's id is not somebody naming it.
func (a *app) automationByID(id string) (automation.Automation, bool) {
	id = strings.TrimSpace(id)
	if id == "" {
		return automation.Automation{}, false
	}
	var found []automation.Automation
	for _, item := range a.watch.list {
		if item.ID == id {
			return item, true
		}
		if len(id) >= automationIDPrefixMin && strings.HasPrefix(item.ID, id) {
			found = append(found, item)
		}
	}
	if len(found) == 1 {
		return found[0], true
	}
	return automation.Automation{}, false
}

// automationIDPrefixMin is the fewest characters of an id a typed line may name
// an automation by.
const automationIDPrefixMin = 4

// confirmTypedAutomation draws the card a typed add or edit ends on, with the
// next run the save will give it, and asks.
func (a *app) confirmTypedAutomation(item automation.Automation, next time.Time, edit bool) {
	now := a.now()
	a.typedSeq++
	notice := session.AutomationNotice{
		ID:         typedAutomationID(a.typedSeq),
		Automation: item,
		WhenWords:  automation.Describe(item.Schedule, now),
		Exact:      automation.Exact(item.Schedule),
		Next:       next,
		Options: []session.AnswerOption{
			{Key: session.AutomationSaveKey, Label: typedSaveLabel, Consequence: typedSaveSays},
			{Key: session.AutomationNoKey, Label: typedNoLabel, Consequence: typedNoSays, Safe: true},
		},
	}
	head := typedAddHead
	if edit {
		head = typedEditHead
		notice.Options = session.AutomationOptions(item)
	}
	if a.auto != nil && !a.auto.settled() {
		a.auto.verdict = autoEndedWord
	}
	card := &automationCard{id: notice.ID, notice: notice, head: head}
	a.auto = card
	// A DECISION OUTRANKS A PANEL, for the engine card's reason (app.go's
	// [session.EventAutomationProposal] case): the card is answered from the
	// conversation, so whatever was over it comes down.
	a.closeSettings()
	a.closeHome()
	if a.pageShowing() {
		a.leavePlace()
	}
	a.closeLive()
	a.closeLists()
	a.entries = append(a.entries, entry{kind: entryAutomation, turn: a.turn, auto: card})
	question := session.AutomationQuestion(notice)
	question.Kind = typedAutomationKind
	question.Ref = "typed/" + itoa(int(notice.ID))
	question.Asker = session.Asker{Kind: session.AskerSurface}
	question.Head = head
	question.Reason = typedReason
	question.Blocking = session.Blocking{}
	question.Input = session.InputShape{}
	question.Asked = now
	a.raiseQuestion(questionShown{
		question: question,
		local: func(answer session.Answer) tea.Cmd {
			return a.typedAutomationAnswered(card, item, edit, answer.FirstKey())
		},
	})
	a.follow()
	a.touch()
}

// typedAutomationAnswered applies a typed card's answer: Save goes through the
// seam, off the loop, and anything else saves nothing.
func (a *app) typedAutomationAnswered(card *automationCard, item automation.Automation, edit bool, key string) tea.Cmd {
	if card.settled() {
		return nil
	}
	a.input.reset()
	a.touch()
	if key != session.AutomationSaveKey {
		card.verdict = autoNotSaved
		return nil
	}
	card.verdict, card.answer = autoSavedWord, typedSaveLabel
	door := a.autos.Create
	if edit {
		door = a.autos.Update
	}
	if door == nil {
		card.verdict = autoNotSaved
		a.note(autoNoStoreWord)
		return nil
	}
	now := a.now()
	return a.offLoop(func() func(bool) tea.Cmd {
		saved, err := door(item)
		return func(bool) tea.Cmd {
			if err != nil {
				card.verdict = autoNotSaved
				a.note(err.Error())
				a.touch()
				return nil
			}
			receipt := typedSavedWord + strings.TrimSpace(saved.Title)
			if !saved.Next.IsZero() {
				receipt += " · " + autoNextWord + automation.Moment(saved.Next, now)
			}
			a.note(receipt)
			a.touch()
			return a.readAutomations()
		}
	})
}
