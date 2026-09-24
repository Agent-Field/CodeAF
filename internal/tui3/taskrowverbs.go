package tui3

import (
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/session"
)

// taskRowVerbs supplies the same task options to home and the Tasks page.
// Archive addresses the task inside its owner, never the conversation itself.
func (a *app) taskRowVerbs(row session.SessionRow, entry session.TaskIndexEntry) []verb {
	if a.hosted() {
		return nil
	}
	var verbs []verb
	dir := strings.TrimSpace(row.Dir)
	if dir == "" && strings.TrimSpace(row.Transcript) != "" {
		dir = filepath.Dir(row.Transcript)
	}
	if dir != "" && row.ID != "" && row.ID == entry.SessionID && entry.ID != "" {
		word := "close"
		archived := row.ArchivedTasks[entry.ID]
		if archived {
			word = "reopen"
		}
		verbs = append(verbs, verb{key: 'x', word: word, do: func() tea.Cmd {
			return a.putTaskAway(dir, entry, !archived)
		}})
	}
	if workspace := strings.TrimSpace(row.Workspace); workspace != "" {
		if a.canStart() {
			verbs = append(verbs, verb{key: 'n', word: "new in project", do: func() tea.Cmd {
				var opened tea.Cmd
				if !a.at(pageHome) {
					opened = a.showPage(pageHome)
				}
				return tea.Batch(opened, a.homeStartInProject(workspace))
			}})
		}
		verbs = append(verbs,
			verb{key: 'o', word: "open folder", do: func() tea.Cmd {
				if err := processOpener(workspace); err != nil {
					a.taskRowNotice("could not open " + workspace)
				} else {
					a.taskRowNotice("opened " + workspace)
				}
				return nil
			}},
			verb{key: 'p', word: "copy project", do: func() tea.Cmd {
				a.taskRowNotice("copied " + workspace)
				return tea.Raw(osc52(workspace, a.tmux))
			}},
		)
	}
	return verbs
}

func (a *app) taskRowNotice(words string) {
	if a.at(pageHome) {
		a.home.say(words, "")
	} else {
		a.taskSheet.actionNote = words
	}
	a.touch()
}

// putTaskAway persists only the visibility preference. Both lists then read
// fresh metadata, so engine updates cannot resurrect a task somebody put away.
func (a *app) putTaskAway(dir string, entry session.TaskIndexEntry, archived bool) tea.Cmd {
	if err := session.SetTaskArchived(dir, entry.SessionID, entry.ID, archived); err != nil {
		a.taskRowNotice("could not change task visibility: " + err.Error())
		return nil
	}
	if a.at(pageHome) {
		a.refreshHome()
	} else {
		p := &a.taskSheet
		if archived {
			if p.closed == nil {
				p.closed = make(map[tasksKey]bool)
			}
			p.closed[tasksKeyOf(entry)] = true
			p.closedQuery = p.query.String()
		} else {
			delete(p.closed, tasksKeyOf(entry))
		}
		p.world = a.readWorld()
		p.mine = a.taskSheetMine()
		p.reading = readTasks(p.world, p.mine, p.reading.win, p.order, p.reading.seen, a.now())
		p.cursor = a.tasksSettle(p.cursor)
	}
	word := "task reopened"
	if archived {
		word = "task closed · find it by typing its name in tasks"
	}
	a.taskRowNotice(word)
	return a.taskPaneFollow()
}
