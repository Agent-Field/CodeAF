package tui3

import (
	"errors"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/Agent-Field/codeaf/internal/sanitize"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/charmbracelet/x/ansi"
)

const conversationOpeningWord = "opening conversation… · esc cancels"

// A first message gets its own screen before the engine has opened its journal.
// The old conversation and both composers remain intact until opening succeeds.
// This is a receipt of the gesture, not a claim that the model has accepted it.
type conversationPreview struct {
	text string
}

func (a *app) previewConversation(text string) {
	if a.conversationOpening {
		a.conversationPreview = &conversationPreview{text: text}
		a.touch()
	}
}

func (a *app) conversationPreviewFrame(width, height int) (string, int, int) {
	a.caret = false
	rows := a.headRows(width, a.tabsRow(width), a.pal)
	rows = append(rows, "", a.pal.dim("new chat"), "")
	if text := a.conversationPreview.text; text != "" {
		// Only a viewport can be shown while opening. Bound the work before
		// sanitizing and wrapping a potentially very large pasted message.
		text = text[:min(len(text), max(width, 1)*max(height, 1)*4)]
		body := strings.Split(ansi.Hardwrap(sanitize.Text(text), max(width-4, 1), true), "\n")
		for i, line := range body {
			lead := userLead
			if i == 0 {
				lead = a.pal.youGlyph()
			}
			rows = append(rows, a.pal.muted(lead+line))
		}
		rows = append(rows, "")
	}
	rows = append(rows, a.pal.dim(conversationOpeningWord))
	// The acknowledgement stays visible even when a pasted prompt fills the pane.
	if len(rows) > height && height > 0 {
		rows = append(rows[:height-1], a.pal.dim(conversationOpeningWord))
	}
	for len(rows) < height {
		rows = append(rows, "")
	}
	for i := range rows {
		rows[i] = fit(rows[i], width)
	}
	return strings.Join(rows, "\n"), 0, 0
}

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
				a.conversationPreview = nil
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
	a.conversationPreview = nil
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
	cmd := a.conversationLater(ask, say, func(conv Conversation) tea.Cmd {
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
	a.previewConversation(text)
	return cmd
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
		// AND THE CALLER'S OWN LANDING WORK ([app.homeTakenSaid]) — the
		// takeover's sentence and its setup card — is spent here, in the
		// conversation the person is arriving in.
		var said tea.Cmd
		if a.homeTakenSaid != nil {
			said, a.homeTakenSaid = a.homeTakenSaid(), nil
		}
		return tea.Batch(opened, a.homeLandOnTask(line), said)
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
