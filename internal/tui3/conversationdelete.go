package tui3

import (
	tea "charm.land/bubbletea/v2"
	"errors"
	"github.com/Agent-Field/codeaf/internal/session"
	teamstore "github.com/Agent-Field/codeaf/internal/teams"
	"path/filepath"
	"strconv"
	"strings"
)

// Permanent deletion owns a confirmation and explicit choices for every team
// this conversation manages. No destructive choice is selected by default.
type conversationDeleteSheet struct {
	on, busy                bool
	managing                bool
	token                   *byte
	taskID                  string
	pendingTeam, pendingKey string
	newSaid                 teamWriteSaid
	file, name, message     string
	choices                 map[string]string
	affected                map[string][]string
	detailsTop, detailsMax  int
	cursor, top             int
	hits                    []wallHit
	rect                    wallRect
}

type conversationDeleteOption struct {
	word, team, replacement string
	action                  int
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
	a.cdelete = conversationDeleteSheet{on: true, token: new(byte), file: file, name: name, choices: map[string]string{}}
	a.cdelete.affected = map[string][]string{}
	for _, t := range a.wall.teams {
		if !t.Closed() && t.Manager == a.convKey(file) {
			ids := []string{t.ID}
			for _, d := range a.teamTree().Descendants(t.ID) {
				ids = append(ids, d.ID)
			}
			a.cdelete.affected[t.ID] = ids
		}
	}
	a.touch()
	return nil
}

func (a *app) conversationDeleteOptions() []conversationDeleteOption {
	s := &a.cdelete
	key := a.convKey(s.file)
	if !s.managing {
		return []conversationDeleteOption{{word: "cancel", action: 1}, {word: "yes", action: 2}}
	}
	options := []conversationDeleteOption{{word: "cancel", action: 1}}
	for _, t := range a.wall.teams {
		if t.Closed() || t.Manager != key {
			continue
		}
		for _, m := range t.Members {
			if m.Key == key {
				continue
			}
			word := "Replace manager of " + t.Name + " with @" + m.Handle + " · " + m.Word
			if chosen, ok := s.choices[t.ID]; ok && chosen == m.Key {
				word += " (selected)"
			}
			options = append(options, conversationDeleteOption{word: word, team: t.ID, replacement: m.Key})
		}
		word := "Disband " + a.teamsAffectedNames(t.ID)
		if t.Root {
			word = "Remove the global manager"
		}
		if chosen, ok := s.choices[t.ID]; ok && chosen == "" {
			word += " (selected)"
		}
		if a.start != nil && !a.shared {
			options = append(options, conversationDeleteOption{word: "+ New manager for " + t.Name, team: t.ID, action: 3})
		}
		options = append(options, conversationDeleteOption{word: word, team: t.ID})
	}
	options = append(options, conversationDeleteOption{word: "yes", action: 2})
	return options
}

func (a *app) conversationDeleteChoose(index int) tea.Cmd {
	s := &a.cdelete
	if s.busy {
		return nil
	}
	options := a.conversationDeleteOptions()
	if index < 0 || index >= len(options) {
		return nil
	}
	option := options[index]
	if option.action == 1 {
		a.cdelete = conversationDeleteSheet{}
		a.touch()
		return nil
	}
	if option.action == 3 {
		if s.newSaid.pending || a.conversationOpening {
			return nil
		}
		return a.conversationDeleteNewManager(option.team)
	}
	if option.action == 0 {
		s.choices[option.team] = option.replacement
		s.message = ""
		a.touch()
		return nil
	}
	if !s.managing {
		if _, visible := a.simpleConfirmCard(a.conversationDeleteQuestion(), s.cursor, s.message); !visible {
			return nil
		}
	}
	width, height := a.size()
	if s.managing && (width < 24 || height < 14) {
		s.message = "Resize the terminal to review deletion"
		a.touch()
		return nil
	}
	key := a.convKey(s.file)
	if s.newSaid.pending {
		s.message = "Saving the new manager membership"
		a.touch()
		return nil
	}
	if !s.managing && s.taskID == "" {
		for _, t := range a.wall.teams {
			if !t.Closed() && t.Manager == key {
				s.managing = true
				s.cursor = 0
				a.touch()
				return nil
			}
		}
	}
	for _, t := range a.wall.teams {
		if s.taskID == "" && !t.Closed() && t.Manager == key {
			if _, chosen := s.choices[t.ID]; !chosen {
				s.message = "Choose a replacement or disband " + t.Name + " first"
				a.touch()
				return nil
			}
		}
	}
	file, name, taskID, choices := s.file, s.name, s.taskID, make(map[string]string, len(s.choices))
	for id, replacement := range s.choices {
		choices[id] = replacement
	}
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
	affected := s.affected
	s.busy, s.message = true, "Deleting conversation"
	if taskID != "" {
		s.message = "Deleting task"
	}
	a.touch()
	return a.offLoop(func() func(bool) tea.Cmd {
		err := door(file, choices, affected)
		return func(bool) tea.Cmd {
			if err != nil {
				a.cdelete.busy, a.cdelete.message = false, err.Error()
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
	case "pgdown":
		a.cdelete.detailsTop = min(a.cdelete.detailsTop+5, a.cdelete.detailsMax)
		a.touch()
	case "pgup":
		a.cdelete.detailsTop = max(a.cdelete.detailsTop-5, 0)
		a.touch()
	case "up", "shift+tab":
		a.cdelete.cursor = max(a.cdelete.cursor-1, 0)
		a.cdelete.detailsTop = 0
		a.touch()
	case "down", "tab":
		a.cdelete.cursor = min(a.cdelete.cursor+1, len(a.conversationDeleteOptions())-1)
		a.cdelete.detailsTop = 0
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
	if !a.cdelete.on {
		return frame
	}
	s := &a.cdelete
	if !s.managing {
		card, visible := a.simpleConfirmCard(a.conversationDeleteQuestion(), s.cursor, s.message)
		s.hits = nil
		if !visible {
			return frame
		}
		s.hits = card.hits
		s.rect = wallRect{card.x, card.y, card.x + card.w, card.y + len(card.rows)}
		return a.confirmCardOver(frame, card)
	}
	width, height := a.size()
	w := min(width-2, 90)
	inner := w - 4
	if inner < 18 || height < 14 {
		return frame
	}
	var lines []wallCardLine
	lines = append(lines, wallCardLine{s: a.pal.ink(fit("Choose replacement managers or disband their teams", inner))})
	options := a.conversationDeleteOptions()
	room := min(max(height-11, 1), 8)
	s.cursor = min(s.cursor, len(options)-1)
	s.top = min(s.top, max(len(options)-room, 0))
	if s.cursor < s.top {
		s.top = s.cursor
	}
	if s.cursor >= s.top+room {
		s.top = s.cursor - room + 1
	}
	for i := s.top; i < min(s.top+room, len(options)); i++ {
		lines = append(lines, wallCardLine{s: wallPopRowPaint(a.pal, fit(options[i].word, inner), inner, i == s.cursor), hits: []wallHit{{x1: inner, y1: 1, arg: i}}})
	}
	details := wrap(options[s.cursor].word, inner)
	detailRoom := max(height-len(lines)-7, 1)
	s.detailsMax = max(len(details)-detailRoom, 0)
	s.detailsTop = min(s.detailsTop, s.detailsMax)
	lines = append(lines, wallCardLine{s: a.pal.dim(fit("Details · pgup/pgdown", inner))})
	for _, text := range details[s.detailsTop:min(s.detailsTop+detailRoom, len(details))] {
		lines = append(lines, wallCardLine{s: a.pal.ink(text)})
	}
	if s.message != "" {
		lines = append(lines, wallCardLine{s: a.pal.warn(fit(s.message, inner))})
	}
	x, y := (width-w)/2, max((height-len(lines)-2)/3, 1)
	card := wallCardBuild(a.pal, "Choose managers", lines, x, y, w, 1, 0)
	s.hits = card.hits
	s.rect = wallRect{card.x, card.y, card.x + card.w, card.y + len(card.rows)}
	rows := strings.Split(frame, "\n")
	for len(rows) < height {
		rows = append(rows, "")
	}
	for i, row := range card.rows {
		if y+i < height {
			rows[y+i] = wallSplice(rows[y+i], row, x, width)
		}
	}
	return strings.Join(rows[:height], "\n")
}

func (a *app) conversationDeleteQuestion() string {
	if a.cdelete.taskID != "" {
		return "Stop work and permanently delete this task?"
	}
	return "Stop work and permanently delete the transcript?"
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

// A brand new replacement is created beside the existing conversation. Its
// membership must reach the store before deletion can appoint it as manager.
func (a *app) conversationDeleteNewManager(id string) tea.Cmd {
	t, ok := a.teamByID(id)
	if !ok || t.Closed() || a.start == nil || a.shared {
		return nil
	}
	file, token := a.cdelete.file, a.cdelete.token
	ask, _ := a.startDoor(a.teamWhere(t))
	return a.conversationLater(ask, func(word string) { a.cdelete.message = word }, func(conv Conversation) tea.Cmd {
		if a.behind == nil {
			a.behind = map[string]*kept{}
		}
		key := a.convKey(conv.SessionFile)
		a.behind[key] = &kept{conv: conv, watch: startBehindWatch(key, conv.Agent, a.stirs)}
		a.rememberOpen(key)
		if !a.cdelete.on || a.cdelete.file != file || a.cdelete.token != token {
			a.touch()
			return nil
		}
		m := teamstore.Member{Key: key, File: conv.SessionFile, Where: conv.Workspace, Word: "Manager of " + t.Name}
		if err := a.teamEdit(func(f *teamstore.File) error {
			current, ok := f.Team(id)
			if !ok || current.Closed() {
				return errors.New("this team is no longer active")
			}
			return f.AddMember(id, m)
		}); err != nil {
			a.cdelete.message = err.Error()
			a.touch()
			return nil
		}
		a.cdelete.pendingTeam, a.cdelete.pendingKey = id, key
		a.cdelete.newSaid = a.teamWriteWatch(nil)
		a.cdelete.message = "Saving the new manager membership"
		a.touch()
		return nil
	})
}
