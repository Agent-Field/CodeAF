package tui3

import (
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/session"
)

// taskRowVerbs supplies the same task options to home and the Tasks page.
// Tasks stop while active and delete once their whole subtree has settled.
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
		word := "delete"
		active := taskTreeActive(a.taskActionRows(row, entry), row.ID, entry.ID)
		if active {
			word = "stop"
		}
		verbs = append(verbs, verb{key: 'x', word: word, do: func() tea.Cmd {
			if active {
				return a.stopTaskRecord(row, entry)
			}
			return a.askRecordDelete(row, &entry)
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
