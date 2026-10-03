package tui3

import (
	"fmt"
	teamstore "github.com/Agent-Field/codeaf/internal/teams"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// A membership sheet edits the selected team, never a conversation's lifetime.
// Candidate sessions are read once off-loop, and frames use only this snapshot.
type teamMembershipSheet struct {
	on              bool
	removing        bool
	choosingManager bool
	team, member    string
	filter          editor
	rows            []chatTab
	shown           []chatTab
	cursor, top     int
	new             bool
	loading         bool
	message         string
	rect            wallRect
	hits            []wallHit
	generation      int
}

func (a *app) teamMembershipOpen(id, member string) tea.Cmd {
	t, ok := a.teamByID(id)
	if !ok || t.Closed() || t.Root && member != "" {
		return nil
	}
	gen := a.tmembers.generation + 1
	a.tmembers = teamMembershipSheet{on: true, team: id, member: member, generation: gen}
	a.touch()
	if member != "" {
		return nil
	}
	a.tmembers.loading = true
	door, root, hosted := a.world, a.placesRoot(), a.hosted()
	return a.besideLine(func() func(bool) tea.Cmd {
		world, known := worldSeam(door, root, hosted)
		return func(bool) tea.Cmd {
			if !a.tmembers.on || a.tmembers.generation != gen {
				return nil
			}
			a.tmembers.loading = false
			if !known {
				a.tmembers.message = "Conversations are still loading; close and try again"
			}
			seen := map[string]bool{}
			add := func(tab chatTab) {
				if tab.key == "" || tab.start || tab.work || seen[tab.key] {
					return
				}
				seen[tab.key] = true
				a.tmembers.rows = append(a.tmembers.rows, tab)
			}
			for _, tab := range a.tabList() {
				add(tab)
			}
			for _, project := range world.Projects {
				for _, row := range project.Sessions {
					if row.Archived {
						continue
					}
					where := row.ProjectDir
					if where == "" {
						where = row.Workspace
					}
					add(chatTab{key: a.convKey(row.Transcript), file: row.Transcript, where: where, word: homeName(row)})
				}
			}
			a.touch()
			return nil
		}
	})
}

// Choosing leadership is separate from adding members and cannot create a chat.
func (a *app) teamChooseManagerOpen(id string) tea.Cmd {
	t, ok := a.teamByID(id)
	if !ok || t.Closed() || t.Root {
		return nil
	}
	a.tmembers = teamMembershipSheet{on: true, choosingManager: true, team: id, generation: a.tmembers.generation + 1}
	for _, m := range t.Members {
		if m.Key != t.Manager {
			word := "@" + m.Handle
			if m.Word != "" {
				word += " · " + m.Word
			}
			a.tmembers.rows = append(a.tmembers.rows, chatTab{key: m.Key, file: m.File, where: m.Where, word: word})
		}
	}
	a.tmembers.shown = append([]chatTab(nil), a.tmembers.rows...)
	a.touch()
	return nil
}

func (a *app) teamMembershipShut() {
	if a.tmembers.on && a.conversationOpening {
		a.cancelConversationOpening()
	}
	gen := a.tmembers.generation + 1
	a.tmembers = teamMembershipSheet{generation: gen}
	a.touch()
}

func (a *app) teamMembershipRows() []chatTab {
	t, ok := a.teamByID(a.tmembers.team)
	if !ok {
		return nil
	}
	var rows []chatTab
	filter := strings.ToLower(a.tmembers.filter.String())
	for _, row := range a.tmembers.rows {
		eligible := !teamHolds(t, row.key)
		if a.tmembers.choosingManager {
			eligible = teamHolds(t, row.key) && row.key != t.Manager && (!a.tp.previews[row.key].missing || a.trafficHeld(row.key))
		}
		if !eligible || !strings.Contains(strings.ToLower(row.word+" "+row.where+" "+row.file), filter) {
			continue
		}
		rows = append(rows, row)
	}
	return rows
}

func (a *app) teamMembershipChoose(index int) tea.Cmd {
	s := &a.tmembers
	if index < 0 {
		return nil
	}
	t, ok := a.teamByID(s.team)
	if !ok || t.Closed() {
		s.message = "This team is no longer active"
		a.touch()
		return nil
	}
	if s.choosingManager {
		if index == 0 {
			a.teamMembershipShut()
			return nil
		}
		if !a.teamManagerPickerVisible() {
			return nil
		}
		rows := s.shown
		if index > len(rows) {
			return nil
		}
		key := rows[index-1].key
		if !t.Holds(key) || t.Manager == key || a.tp.previews[key].missing && !a.trafficHeld(key) {
			s.message, s.cursor = "This member is no longer eligible; choose again", 0
			a.touch()
			return nil
		}
		if err := a.teamEdit(func(f *teamstore.File) error {
			current, ok := f.Team(t.ID)
			if !ok || current.Closed() || !current.Holds(key) {
				return fmt.Errorf("this member is no longer in the active team")
			}
			return f.SetManager(t.ID, key)
		}); err != nil {
			s.message = err.Error()
			a.touch()
			return nil
		}
		a.teamMembershipShut()
		return a.teamsRead(true)
	}
	if s.removing {
		if index == 0 {
			a.teamMembershipShut()
			return nil
		}
		if index != 1 {
			return nil
		}
		if _, visible := a.simpleConfirmCard(a.teamRemovalQuestion(t), s.cursor, s.message); !visible {
			return nil
		}
		if err := a.teamRemove(t.ID, []string{s.member}); err != nil {
			s.message = err.Error()
			a.touch()
			return nil
		}
		a.teamMembershipShut()
		return a.teamsRead(true)
	}
	if s.member != "" {
		if index >= len(a.teamMembershipActions(t)) {
			return nil
		}
		if index == 2 {
			key := s.member
			if err := a.teamEdit(func(f *teamstore.File) error {
				current, ok := f.Team(t.ID)
				if !ok || current.Closed() || current.Manager == "" || current.Manager == key {
					return fmt.Errorf("this membership no longer has another manager in this team")
				}
				return f.SetHome(key, t.ID)
			}); err != nil {
				s.message = err.Error()
				a.touch()
				return nil
			}
		} else if index == 1 {
			m, ok := t.Member(s.member)
			if !ok {
				return nil
			}
			if err := a.teamMakeManager(t.ID, chatTab{key: m.Key, file: m.File, where: m.Where, word: m.Word}); err != nil {
				s.message = err.Error()
				return nil
			}
		} else {
			if t.Manager == s.member {
				s.message = teamManagerRemovalWord
				a.touch()
				return nil
			}
			if err := a.teamRemove(t.ID, []string{s.member}); err != nil {
				s.message = err.Error()
				return nil
			}
		}
		a.teamMembershipShut()
		return a.teamsRead(true)
	}
	if index == 0 {
		s.new = true
		s.filter.reset()
		s.cursor = 0
		a.touch()
		return nil
	}
	rows := a.teamMembershipRows()
	if index > len(rows) {
		return nil
	}
	if err := a.teamAdd(t.ID, []chatTab{rows[index-1]}); err != nil {
		s.message = err.Error()
		return nil
	}
	a.teamMembershipShut()
	return a.teamsRead(true)
}

// A new member's first assignment waits for the store to accept membership.
// Navigating away during the write leaves that assignment as its own draft.
type teamMemberStart struct {
	key, prompt, team string
	said              teamWriteSaid
}

func (a *app) teamMembershipSubmit() tea.Cmd {
	pending := a.tmemberStart
	if pending.key == "" || pending.said.pending {
		return nil
	}
	a.tmemberStart = teamMemberStart{}
	if pending.said.why != "" {
		a.note(teamNotSaved("the membership", pending.said.why))
		return nil
	}
	if a.pageShowing() || a.wall.on || a.startingChat() || a.teamViews.id != pending.team || a.frontTabKey() != pending.key || a.input.String() != pending.prompt {
		return nil
	}
	a.input.reset()
	return a.submit(pending.prompt)
}

func (a *app) teamMembershipStart() tea.Cmd {
	s := &a.tmembers
	prompt := strings.TrimSpace(s.filter.String())
	t, ok := a.teamByID(s.team)
	if !ok || t.Closed() || prompt == "" {
		return nil
	}
	if !a.canStart() || a.tmemberStart.key != "" {
		s.message = newUnavailableWord
		a.touch()
		return nil
	}
	id, where, gen := t.ID, a.teamWhere(t), s.generation
	finish := func(opened tea.Cmd) tea.Cmd {
		tab := chatTab{key: a.frontTabKey(), file: a.file, where: a.workspace, word: promptName(prompt)}
		if err := a.teamAdd(id, []chatTab{tab}); err != nil {
			a.note(err.Error())
			a.input.setText(prompt)
			return opened
		}
		a.tmemberStart = teamMemberStart{key: tab.key, prompt: prompt, team: id, said: a.teamWriteWatch(nil)}
		a.input.setText(prompt)
		a.teamMembershipShut()
		a.teamViewSet(id)
		a.leavePlace()
		a.closeRoom()
		return opened
	}
	// A shared handle is swapped by its existing synchronous door. A stale
	// asynchronous result cannot undo a swap the connection already performed.
	if a.shared {
		previous := a.teamViews.id
		a.teamViewSet("")
		opened, refusal := a.teamsStartIn(where)
		if refusal != "" {
			a.teamViewSet(previous)
			s.message = refusal
			a.touch()
			return nil
		}
		return finish(opened)
	}
	ask, whole := a.startDoor(where)
	old := a.front()
	if !whole && where != "" && where != old.Workspace {
		s.message = newUnavailableWord
		a.touch()
		return nil
	}
	return a.conversationLater(ask, func(words string) { a.tmembers.message = words }, func(conv Conversation) tea.Cmd {
		current, active := a.teamByID(id)
		if !a.tmembers.on || a.tmembers.generation != gen || !active || current.Closed() {
			if conv.Agent != nil {
				leaveOffFrame(conv.Agent)
			}
			a.tmembers.message = "This team is no longer active"
			return nil
		}
		if !whole {
			old.Agent, old.SessionFile = conv.Agent, conv.SessionFile
			conv = old
		}
		return finish(a.takeBeside(conv))
	})
}

func (a *app) teamMembershipKey(msg tea.KeyPressMsg) tea.Cmd {
	s := &a.tmembers
	a.touch()
	switch msg.String() {
	case "esc":
		if a.conversationOpening {
			a.cancelConversationOpening()
		}
		a.teamMembershipShut()
	case "up":
		s.cursor = max(s.cursor-1, 0)
	case "down":
		last := len(a.teamMembershipRows())
		if s.choosingManager {
			last = len(s.shown)
		}
		if s.member != "" {
			if t, ok := a.teamByID(s.team); ok {
				last = len(a.teamMembershipActions(t)) - 1
				if s.removing {
					last = 1
				}
			}
		}
		s.cursor = min(s.cursor+1, last)
	case "enter":
		if s.new {
			return a.teamMembershipStart()
		}
		return a.teamMembershipChoose(s.cursor)
	case "backspace":
		s.filter.deleteBackward()
		s.cursor = 0
	default:
		if s.member == "" {
			if text := msg.Key().Text; text != "" {
				s.filter.insert(text)
				s.cursor = 0
			}
		}
	}
	return nil
}

func (a *app) teamMembershipMouse(msg tea.Msg, m tea.Mouse) (tea.Cmd, bool) {
	s := &a.tmembers
	switch msg.(type) {
	case tea.MouseMotionMsg:
		if !s.new {
			for _, hit := range s.hits {
				if hit.arg >= 0 && m.X >= hit.x0 && m.X < hit.x1 && m.Y >= hit.y0 && m.Y < hit.y1 && s.cursor != hit.arg {
					s.cursor = hit.arg
					a.touch()
					break
				}
			}
		}
	case tea.MouseWheelMsg:
		last := len(a.teamMembershipRows())
		if s.choosingManager {
			last = len(s.shown)
		}
		if t, ok := a.teamByID(s.team); ok && s.member != "" {
			last = len(a.teamMembershipActions(t)) - 1
			if s.removing {
				last = 1
			}
		}
		s.cursor = min(max(s.cursor+placeWheelDelta(m.Button), 0), max(last, 0))
		a.touch()
	case tea.MouseClickMsg:
		if m.Button != tea.MouseLeft {
			return nil, true
		}
		for _, hit := range s.hits {
			if m.X >= hit.x0 && m.X < hit.x1 && m.Y >= hit.y0 && m.Y < hit.y1 {
				if hit.arg < 0 {
					if a.conversationOpening {
						a.cancelConversationOpening()
					}
					a.teamMembershipShut()
					return nil, true
				}
				s.cursor = hit.arg
				if s.new {
					return a.teamMembershipStart(), true
				}
				return a.teamMembershipChoose(hit.arg), true
			}
		}
		if !s.rect.holds(m.X, m.Y) {
			if a.conversationOpening {
				a.cancelConversationOpening()
			}
			a.teamMembershipShut()
		}
	}
	return nil, true
}

// A hidden picker must not accept a leadership change from keys or old mouse hits.
func (a *app) teamManagerPickerVisible() bool {
	width, height := a.size()
	return min(width-2, 76)-4 >= ansi.StringWidth("enter choose · esc cancel") && height >= 10
}

func (a *app) teamMembershipOver(frame string) string {
	if !a.tmembers.on {
		return frame
	}
	s := &a.tmembers
	t, ok := a.teamByID(s.team)
	if !ok {
		a.teamMembershipShut()
		return frame
	}
	if s.removing {
		card, visible := a.simpleConfirmCard(a.teamRemovalQuestion(t), s.cursor, s.message)
		s.hits = nil
		if !visible {
			return frame
		}
		s.hits = card.hits
		s.rect = wallRect{card.x, card.y, card.x + card.w, card.y + len(card.rows)}
		return a.confirmCardOver(frame, card)
	}
	width, height := a.size()
	w := min(width-2, 76)
	inner := w - 4
	if inner < 12 || height < 10 || s.choosingManager && !a.teamManagerPickerVisible() {
		s.hits = nil
		s.rect = wallRect{}
		return frame
	}
	title := "Add member to " + t.Name
	if s.choosingManager {
		title = "Choose manager for " + t.Name
	}
	var lines []wallCardLine
	if s.member != "" {
		m, _ := t.Member(s.member)
		title = "@" + m.Handle + " of " + t.Name
		for i, word := range a.teamMembershipActions(t) {
			lines = append(lines, wallCardLine{s: wallPopRowPaint(a.pal, word, inner, s.cursor == i), hits: []wallHit{{x1: inner, y1: 1, arg: i}}})
		}
		lines = append(lines, wallCardLine{s: a.pal.dim(fit("Removal keeps this conversation and its current work", inner))})
	} else if s.new {
		title = "New member in " + t.Name
		lines = append(lines, wallCardLine{s: a.pal.dim("Describe its first assignment")}, wallCardLine{s: fit(s.filter.String()+a.linearMark("▏", "|"), inner)})
		lines = append(lines, wallCardLine{s: a.pal.underline("Create member · enter"), hits: []wallHit{{x1: inner, y1: 1}}})
	} else {
		lines = append(lines, wallCardLine{s: a.pal.dim("Filter  ") + fit(s.filter.String()+a.linearMark("▏", "|"), inner-8)})
		rows := a.teamMembershipRows()
		if s.choosingManager {
			// Preserve the highlighted identity across a team refresh; never substitute a row.
			if s.cursor > 0 && s.cursor <= len(s.shown) {
				selected := s.shown[s.cursor-1].key
				s.cursor = 0
				for i, row := range rows {
					if row.key == selected {
						s.cursor = i + 1
						break
					}
				}
				if s.cursor == 0 {
					s.message = "This member is no longer eligible; choose again"
				}
			}
			s.shown = append([]chatTab(nil), rows...)
		}
		nameWidth := max(inner*2/3, 1)
		projectWidth := max(inner-nameWidth-2, 1)
		lines = append(lines, wallCardLine{s: a.pal.dim(teamsPad("Name", nameWidth) + "  " + teamsPad("Project", projectWidth))})
		all := []string{"+ New conversation"}
		if s.choosingManager {
			all[0] = "cancel"
		}
		for _, r := range rows {
			project := filepath.Base(r.where)
			if r.where == "" {
				project = ""
			}
			all = append(all, teamsPad(r.word, nameWidth)+"  "+teamsPad(project, projectWidth))
		}
		s.cursor = min(max(s.cursor, 0), len(all)-1)
		room := max(height-10, 1)
		s.top = min(s.top, max(len(all)-room, 0))
		if s.cursor < s.top {
			s.top = s.cursor
		}
		if s.cursor >= s.top+room {
			s.top = s.cursor - room + 1
		}
		for i := s.top; i < min(s.top+room, len(all)); i++ {
			lines = append(lines, wallCardLine{s: wallPopRowPaint(a.pal, fit(all[i], inner), inner, s.cursor == i), hits: []wallHit{{x1: inner, y1: 1, arg: i}}})
		}
		if s.loading {
			lines = append(lines, wallCardLine{s: a.pal.dim("Loading conversations")})
		}
	}
	if s.message != "" {
		lines = append(lines, wallCardLine{s: a.pal.warn(fit(s.message, inner))})
	}
	if s.choosingManager {
		hint := "enter choose · esc cancel"
		lines = append(lines, wallCardLine{s: a.pal.dim(strings.Repeat(" ", max(inner-ansi.StringWidth(hint), 0)) + fit(hint, inner))})
	} else {
		lines = append(lines, wallCardLine{s: a.pal.dim("Cancel · esc"), hits: []wallHit{{x1: inner, y1: 1, arg: -1}}})
	}
	h := len(lines) + 2
	x, y := (width-w)/2, max((height-h)/3, 1)
	card := wallCardBuild(a.pal, title, lines, x, y, w, 1, 0)
	s.hits = card.hits
	s.rect = wallRect{card.x, card.y, card.x + card.w, card.y + len(card.rows)}
	rows := strings.Split(frame, "\n")
	for len(rows) < height {
		rows = append(rows, "")
	}
	for dy, row := range card.rows {
		if y+dy < len(rows) {
			rows[y+dy] = wallSplice(rows[y+dy], row, x, width)
		}
	}
	return strings.Join(rows[:height], "\n")
}

func (a *app) teamMembershipActions(t team) []string {
	actions := []string{"Remove from this team", "Make manager"}
	if t.Manager != "" && t.Manager != a.tmembers.member {
		actions = append(actions, "Report to this team’s manager")
	}
	return actions
}

func (a *app) teamRemovalQuestion(t team) string {
	m, _ := t.Member(a.tmembers.member)
	name := m.Word
	if m.Handle != "" {
		name = "@" + m.Handle
	}
	return "Remove " + name + " from " + t.Name + "?"
}
