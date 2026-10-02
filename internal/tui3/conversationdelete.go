package tui3

import (
	tea "charm.land/bubbletea/v2"
	"github.com/Agent-Field/codeaf/internal/session"
	teamstore "github.com/Agent-Field/codeaf/internal/teams"
	"path/filepath"
	"strconv"
)

// Permanent deletion never changes team leadership. A replacement must be chosen
// in Teams first, and no destructive choice is selected by default.
type conversationDeleteSheet struct {
	on, busy            bool
	taskID              string
	file, name, message string
	cursor              int
	hits                []wallHit
	rect                wallRect
}

type conversationDeleteOption struct {
	word   string
	action int
}

func (a *app) conversationDeleteOpen(file, name string) tea.Cmd {
	return a.deleteSheetOpen(file, name, false)
}

func (a *app) deleteSheetOpen(file, name string, task bool) tea.Cmd {
	if file == "" {
		a.note("This conversation has no saved transcript yet")
		return nil
	}
	if !task && a.deleteConversation == nil && a.hosted() {
		a.note("This engine does not offer permanent deletion")
		return nil
	}
	if a.deletedSessionRows == nil {
		a.deletedSessionRows = map[tasksKey]bool{}
	}
	a.cdelete = conversationDeleteSheet{on: true, file: file, name: name}
	a.touch()
	return nil
}

const teamManagerRemovalWord = teamstore.ManagerRemovalInstruction

func (a *app) conversationDeleteOptions() []conversationDeleteOption {
	return []conversationDeleteOption{{word: "cancel", action: 1}, {word: "delete", action: 2}}
}

func (a *app) conversationDeleteChoose(index int) tea.Cmd {
	s := &a.cdelete
	if s.busy || index < 0 || index > 1 {
		return nil
	}
	if index == 0 {
		a.cdelete = conversationDeleteSheet{}
		a.touch()
		return nil
	}
	if _, visible := a.deleteConfirmCard(s.cursor, s.message); !visible {
		return nil
	}
	if s.taskID == "" {
		for _, t := range a.wall.teams {
			if !t.Closed() && t.Manager == a.convKey(s.file) {
				s.message, s.cursor = teamManagerRemovalWord, 0
				a.touch()
				return nil
			}
		}
	}
	return a.conversationDeleteRun()
}

func (a *app) conversationDeleteRun() tea.Cmd {
	s := &a.cdelete
	key := a.convKey(s.file)
	file, name, taskID := s.file, s.name, s.taskID
	door := a.deleteConversation
	if door == nil {
		profile, root := a.profileDir, a.placesRoot()
		var owner Agent
		if key == a.frontTabKey() {
			owner = a.agent
		} else if held := a.behind[key]; held != nil {
			owner = held.conv.Agent
		}
		door = func(file string, choices map[string]string, affected map[string][]string) error {
			return session.DeleteConversationUnder(root, profile, file, choices, func(string) error {
				if owner != nil {
					leaveAgentFor(owner, session.StopByPerson)
				}
				return nil
			}, affected)
		}
	}
	if taskID != "" {
		remove := a.deleteTask
		if remove == nil {
			root := a.placesRoot()
			remove = func(file, id string) error { return session.DeleteTaskUnder(root, file, id, nil) }
		}
		door = func(file string, _ map[string]string, _ map[string][]string) error { return remove(file, taskID) }
	}
	s.busy, s.message = true, ""
	a.touch()
	return a.offLoop(func() func(bool) tea.Cmd {
		err := door(file, nil, nil)
		return func(bool) tea.Cmd {
			if err != nil {
				a.cdelete.busy, a.cdelete.message = false, err.Error()
				a.cdelete.cursor = 0
				a.touch()
				return a.teamsRead(true)
			}
			a.cdelete = conversationDeleteSheet{}
			if taskID != "" {
				return a.taskDeletionFinished(file, taskID)
			}
			a.deletedSessionRows[tasksChatKey(filepath.Base(filepath.Dir(file)))] = true
			a.teamViewSet("")
			cleanup := func() tea.Cmd {
				if held := a.behind[key]; held != nil {
					a.letGoKept(key, held, leaveAgent)
				}
				a.forget(key)
				delete(a.tabShut, key)
				delete(a.unreadChats, key)
				a.chatTabBar = tabBar{}
				a.refreshHome()
				a.touch()
				return a.teamsRead(true)
			}
			if key == a.frontTabKey() {
				if cmd, moved := a.leaveFront(leaveAgent, false); moved {
					return tea.Batch(cmd, cleanup())
				}
				a.forgetSteerOwner(draftOwnerOf(a.host, a.workspace, a.file))
				a.detachConversation()
				a.agent, a.file = nil, ""
				a.input.reset()
				return tea.Batch(cleanup(), a.openHome())
			}
			a.home.say("Deleted "+name, "")
			return cleanup()
		}
	})
}

func (a *app) conversationDeleteKey(msg tea.KeyPressMsg) tea.Cmd {
	if a.cdelete.busy {
		return nil
	}
	switch msg.String() {
	case "esc":
		a.cdelete = conversationDeleteSheet{}
		a.touch()
	case "up", "shift+tab":
		a.cdelete.cursor = max(a.cdelete.cursor-1, 0)
		a.touch()
	case "down", "tab":
		a.cdelete.cursor = min(a.cdelete.cursor+1, len(a.conversationDeleteOptions())-1)
		a.touch()
	case "enter", "space":
		return a.conversationDeleteChoose(a.cdelete.cursor)
	}
	return nil
}

func (a *app) conversationDeleteMouse(msg tea.Msg, m tea.Mouse) tea.Cmd {
	if a.cdelete.busy {
		return nil
	}
	if _, click := msg.(tea.MouseClickMsg); click && m.Button == tea.MouseLeft {
		for _, hit := range a.cdelete.hits {
			if m.X >= hit.x0 && m.X < hit.x1 && m.Y >= hit.y0 && m.Y < hit.y1 {
				a.cdelete.cursor = hit.arg
				return a.conversationDeleteChoose(hit.arg)
			}
		}
	}
	if _, wheel := msg.(tea.MouseWheelMsg); wheel {
		a.cdelete.cursor = min(max(a.cdelete.cursor+placeWheelDelta(m.Button), 0), len(a.conversationDeleteOptions())-1)
		a.touch()
	}
	return nil
}

func (a *app) conversationDeleteOver(frame string) string {
	if !a.cdelete.on || a.cdelete.busy {
		return frame
	}
	s := &a.cdelete
	card, visible := a.deleteConfirmCard(s.cursor, s.message)
	s.hits = nil
	if !visible {
		return frame
	}
	s.hits = card.hits
	s.rect = wallRect{card.x, card.y, card.x + card.w, card.y + len(card.rows)}
	return a.confirmCardOver(frame, card)
}

func (a *app) deleteConfirmCard(cursor int, message string) (wallCard, bool) {
	return a.choiceConfirmCard("Stop work and permanently delete?", "", []string{"cancel", "delete"}, cursor, message, 64)
}

func (a *app) taskDeleteOpen(row session.SessionRow, entry session.TaskIndexEntry) tea.Cmd {
	if a.hosted() && a.deleteTask == nil {
		a.note("This engine does not offer permanent task deletion")
		return nil
	}
	cmd := a.deleteSheetOpen(row.Transcript, entry.Title, true)
	if a.cdelete.on {
		a.cdelete.taskID = entry.ID
	}
	return cmd
}

func (a *app) taskDeletionFinished(file, id string) tea.Cmd {
	owner := filepath.Base(filepath.Dir(file))
	rows := a.taskSheet.reading.items
	ids := map[string]bool{id: true}
	for changed := true; changed; {
		changed = false
		for _, row := range rows {
			parent := row.entry.Parent
			if row.plan != nil {
				parent = row.plan.Parent
			}
			if row.entry.SessionID == owner && ids[parent] && !ids[row.entry.ID] {
				ids[row.entry.ID] = true
				changed = true
			}
		}
	}
	if a.deletedSessionRows == nil {
		a.deletedSessionRows = map[tasksKey]bool{}
	}
	for id := range ids {
		a.deletedSessionRows[tasksKey{session: owner, id: id}] = true
		if a.convKey(file) == a.frontTabKey() {
			number, _ := strconv.ParseUint(id, 10, 64)
			delete(a.tasks, number)
		}
	}
	a.taskSheet.detailOn = false
	a.railStamp++
	a.taskSheet.actionNote = "Deleted task"
	a.refreshHome()
	a.touch()
	return nil
}
