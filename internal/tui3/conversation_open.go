package tui3

import (
	"errors"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/Agent-Field/codeaf/internal/session"
)

const conversationOpeningWord = "opening conversation… · esc cancels"

// conversationLater separates preparing a conversation from committing its
// screen. Only the captured door runs off-loop; drafts and selection are read
// and changed on the loop. A later gesture invalidates the result, so a slow
// engine cannot send an old draft into a newly selected conversation.
func (a *app) conversationLater(ask func() (Conversation, error), say func(string), take func(Conversation) tea.Cmd, onError ...func(error) tea.Cmd) tea.Cmd {
	if a.conversationOpening {
		return nil
	}
	a.conversationRequest++
	request, page, ctx := a.conversationRequest, a.page, a.ctx
	a.conversationOpening = true
	say(conversationOpeningWord)
	a.touch()
	return a.offLoop(func() func(bool) tea.Cmd {
		conv, err := ask()
		if ctx.Err() != nil {
			if conv.Agent != nil {
				leaveAgent(conv.Agent)
			}
			return nil
		}
		return func(here bool) tea.Cmd {
			current := a.conversationOpening && request == a.conversationRequest
			if current {
				a.conversationOpening = false
			}
			if !current || !here || !a.at(page) {
				if conv.Agent != nil {
					leaveOffFrame(conv.Agent)
				}
				return nil
			}
			a.touch()
			if err != nil {
				if len(onError) > 0 {
					return onError[0](err)
				}
				say(err.Error())
				return nil
			}
			return take(conv)
		}
	})
}

func (a *app) cancelConversationOpening() {
	a.conversationOpening = false
	a.conversationRequest++
	if a.at(pageHome) {
		a.home.say("", "")
	} else {
		a.note("opening cancelled")
	}
	a.touch()
}

// startDoor captures the callbacks before a command leaves the update loop.
// The legacy fresh callback returns only an agent and a file, so the caller
// still folds the other conversation fields through finishRenew.
func (a *app) startDoor(workspace string) (func() (Conversation, error), bool) {
	if start := a.start; start != nil {
		return func() (Conversation, error) { return start(workspace) }, true
	}
	fresh := a.fresh
	return func() (Conversation, error) {
		agent, file, err := fresh()
		return Conversation{Agent: agent, SessionFile: file}, err
	}, false
}

func (a *app) renewLater(say func(string), after func() tea.Cmd) tea.Cmd {
	if a.shared {
		cmd, ok := a.renewRefusing(say)
		if ok && after != nil {
			return tea.Batch(cmd, after())
		}
		return cmd
	}
	if !a.canStart() {
		say(newUnavailableWord)
		return nil
	}
	replacing := a.renewReplaces()
	ask, whole := a.startDoor("")
	return a.conversationLater(ask, say, func(conv Conversation) tea.Cmd {
		cmd := a.finishRenew(conv, whole, replacing)
		if after != nil {
			return tea.Batch(cmd, after())
		}
		return cmd
	})
}

func (a *app) homeStartLater(text, place string) tea.Cmd {
	if (strings.TrimSpace(text) != "" || place != "" || len(a.home.chips) > 0) && a.updateStopsTurn() {
		return nil
	}
	say := func(text string) { a.home.say(text, "") }
	if !a.canStart() {
		say(newUnavailableWord)
		return nil
	}
	if !a.mainComposer().empty() && (a.agent == nil || a.convKey(a.file) == "") {
		say(startDraftUnownedWord)
		return nil
	}
	where := place
	if where == "" {
		where = strings.TrimSpace(a.targetWhere())
	}
	beside := where != "" && (place != "" || where != strings.TrimSpace(a.workspace))
	if beside && a.start == nil {
		say(newUnavailableWord)
		return nil
	}
	doorWhere := ""
	if beside {
		doorWhere = where
	}
	ask, whole := a.startDoor(doorWhere)
	replacing := a.renewReplaces()
	carried := a.home.chips
	return a.conversationLater(ask, say, func(conv Conversation) tea.Cmd {
		var started tea.Cmd
		if beside {
			started = a.takeBeside(conv)
			a.teamJoinFront()
		} else {
			started = a.finishRenew(conv, whole, replacing)
		}
		a.closeHome()
		// Home owns its tray; the previous conversation keeps its own draft.
		a.putComposer(composerState{chips: carried})
		if place != "" {
			a.applyTargetModel()
			return started
		}
		pins := a.applyTargetPins()
		if len(a.chips) > 0 {
			return tea.Batch(started, pins, a.submitImages(text))
		}
		if strings.TrimSpace(text) == "" {
			return tea.Batch(started, pins)
		}
		// A SHELL LINE GOES THROUGH THE SHELL'S OWN DOOR, exactly as the
		// synchronous road sends it ([app.homeStartWithProjectNow]). Sent as a
		// message it would reach the model as words rather than run in the
		// project the person chose.
		if _, bash := session.BashCommand(text); bash {
			return tea.Batch(started, pins, a.submitBash(text))
		}
		return tea.Batch(started, pins, a.submit(text))
	})
}

func (a *app) homeOpenLater(line homeLine, takeover bool) tea.Cmd {
	a.teamViewSet("")
	where, file := homeWhere(line), line.row.Transcript
	open, resume := a.open, a.resume
	if open == nil && resume == nil {
		a.home.say(resumeUnavailableWord, "")
		return nil
	}
	ask := func() (Conversation, error) {
		if !homeFolderThere(where) {
			return Conversation{}, errors.New(homeGoneWord + " · " + where)
		}
		if open != nil {
			return open(where, file)
		}
		agent, err := resume(file)
		return Conversation{Agent: agent, Workspace: where, SessionFile: file}, err
	}
	say := func(text string) { a.home.say(text, "") }
	return a.conversationLater(ask, say, func(conv Conversation) tea.Cmd {
		opened := a.takeBeside(conv)
		a.closeHome()
		return tea.Batch(opened, a.homeLandOnTask(line))
	}, func(err error) tea.Cmd {
		if errors.Is(err, session.ErrSessionLocked) || err.Error() == sessionBusyWord {
			if takeover {
				return a.homeTakeoverEnter(line)
			}
			say(sessionBusyWord)
		} else {
			say(err.Error())
		}
		return nil
	})
}
