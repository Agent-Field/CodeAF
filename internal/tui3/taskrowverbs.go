package tui3

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/session"
)

// taskRowVerbs supplies the same task options to home and the Tasks page.
// Permanent deletion addresses the task and its descendants inside its owner.
func (a *app) taskRowVerbs(row session.SessionRow, entry session.TaskIndexEntry) []verb {
	var verbs []verb
	if row.Transcript != "" && entry.ID != "" && (a.deleteTask != nil || !a.hosted()) {
		verbs = append(verbs, verb{key: 'x', word: "delete", do: func() tea.Cmd { return a.taskDeleteOpen(row, entry) }})
	}
	dir := strings.TrimSpace(row.Dir)
	if dir == "" && strings.TrimSpace(row.Transcript) != "" {
		dir = session.DirOf(row.Transcript)
	}
	if a.hosted() {
		return verbs
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
