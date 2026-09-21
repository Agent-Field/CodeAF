package tui3

import (
	"path/filepath"
	"sort"
)

// Home previews recent conversations; the Sessions tab holds their full trees.
const homeSessionsLimit = 15

type sessionsPanel struct{ homePanelBase }

func (sessionsPanel) rows(in *homeGridInput) homePanelRows {
	byFile := make(map[string]switcherRow)
	for _, row := range in.rows {
		if row.kind == switcherConversation {
			byFile[filepath.Clean(row.session.Transcript)] = row
		}
	}
	// Closed conversations remain part of history and retain their dim styling.
	for _, project := range in.world.Projects {
		for _, row := range project.Sessions {
			if row.Archived {
				byFile[filepath.Clean(row.Transcript)] = switcherRow{kind: switcherConversation,
					session: row, project: project.Name, title: homeName(row),
					place: switcherWhere(row, project)}
			}
		}
	}
	// The tab keeper has fresher titles and ownership than the disk snapshot.
	for _, row := range in.closedChats {
		row.session.Archived = true
		byFile[filepath.Clean(row.session.Transcript)] = row
	}
	for _, row := range in.openChats {
		byFile[filepath.Clean(row.session.Transcript)] = row
	}
	rows := make([]switcherRow, 0, len(byFile))
	for _, row := range byFile {
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].session.At.Equal(rows[j].session.At) {
			return rows[i].session.Transcript < rows[j].session.Transcript
		}
		return rows[i].session.At.After(rows[j].session.At)
	})
	rows = rows[:min(len(rows), homeSessionsLimit)]
	lines := make([]homeLine, 0, len(rows))
	for _, row := range rows {
		row.age = sinceAt(row.session.At, in.now)
		cell := recentCell(row, in)
		if row.here {
			cell = recentOwnCell(row, in)
		}
		cell.panel, cell.chatKey, cell.closed = panelSessions, row.chatKey, row.session.Archived
		cell.key = "sessions:" + row.session.Transcript
		if row.needs {
			cell.mark = cellMarkNeeds
		}
		if !in.desc {
			cell.sub, cell.grows = "", false
		}
		lines = append(lines, switcherRowLine(row, cell))
	}
	homeDecorateQuestions(in, lines)
	return homePanelCut(in, panelSessions, lines)
}
