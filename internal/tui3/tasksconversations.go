package tui3

import (
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// tasksConversationRows keeps main chats in the selected time window even
// before they delegate any work. The current chat's live facts override the
// stored snapshot, while its project address survives that refresh.
func tasksConversationRows(world session.World, mine tasksMine, win session.UsageWindow, now time.Time) []session.SessionRow {
	var rows []session.SessionRow
	at := make(map[string]int)
	put := func(row session.SessionRow) {
		if strings.TrimSpace(row.ID) == "" || strings.TrimSpace(row.Transcript) == "" {
			return
		}
		stamp := row.At
		if row.Open || row.Live {
			stamp = now
		}
		if !win.Holds(stamp) {
			return
		}
		if i, found := at[row.ID]; found {
			if row.Project == "" {
				row.Project = rows[i].Project
			}
			if row.ProjectDir == "" {
				row.ProjectDir = rows[i].ProjectDir
			}
			if row.Title == "" {
				row.Title = rows[i].Title
			}
			if strings.TrimSpace(row.Title) == "" {
				row.Title = homeName(row)
			}
			rows[i] = row
			return
		}
		if strings.TrimSpace(row.Title) == "" {
			row.Title = homeName(row)
		}
		at[row.ID] = len(rows)
		rows = append(rows, row)
	}
	for _, project := range world.Projects {
		for _, row := range project.Sessions {
			put(row)
		}
	}
	put(mine.row)
	return rows
}
