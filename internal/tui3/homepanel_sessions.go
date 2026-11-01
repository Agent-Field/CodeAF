package tui3

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/Agent-Field/codeaf/internal/session"
)

// Home previews recent conversations; the Sessions tab holds their full trees.
const homeSessionsLimit = 15

type sessionsPanel struct{ homePanelBase }

// homeSessionID extracts a conversation's stable chat id, recovering it from a
// structured session or cell folder when the row has not populated ID yet.
func homeSessionID(row session.SessionRow) string {
	if id := strings.TrimSpace(row.ID); id != "" {
		return id
	}
	if dir, ok := session.FolderOf(row.Transcript); ok {
		if id := strings.TrimSpace(filepath.Base(dir)); id != "" && id != "." && id != string(filepath.Separator) {
			return id
		}
	}
	return ""
}

// homeSessionKey keys Home's conversation rows by chat id rather than by
// transcript path, falling back to the cleaned file path only for bare test
// transcripts that live outside a session folder and carry no id.
func homeSessionKey(row session.SessionRow) string {
	if id := homeSessionID(row); id != "" {
		return "id:" + id
	}
	return "file:" + filepath.Clean(row.Transcript)
}

func preferHomeSessionRow(candidate, existing switcherRow) bool {
	if candidate.here != existing.here {
		return candidate.here
	}
	if candidate.session.Archived != existing.session.Archived {
		return !candidate.session.Archived
	}
	if candidate.session.Open != existing.session.Open {
		return candidate.session.Open
	}
	if !candidate.session.At.Equal(existing.session.At) {
		return candidate.session.At.After(existing.session.At)
	}
	candMoving := strings.Contains(filepath.ToSlash(candidate.session.Transcript), "/.moving/")
	existMoving := strings.Contains(filepath.ToSlash(existing.session.Transcript), "/.moving/")
	if candMoving != existMoving {
		return candMoving
	}
	return candidate.session.Transcript < existing.session.Transcript
}

func (sessionsPanel) rows(in *homeGridInput) homePanelRows {
	byID := make(map[string]switcherRow)
	put := func(row switcherRow) {
		if row.session.ID == "" {
			row.session.ID = homeSessionID(row.session)
		}
		key := homeSessionKey(row.session)
		if prev, exists := byID[key]; exists && !preferHomeSessionRow(row, prev) {
			return
		}
		byID[key] = row
	}
	for _, row := range in.rows {
		if row.kind == switcherConversation {
			put(row)
		}
	}
	// Closed conversations remain part of history and retain their dim styling.
	for _, project := range in.world.Projects {
		for _, row := range project.Sessions {
			if row.Archived {
				put(switcherRow{kind: switcherConversation,
					session: row, project: project.Name, title: homeName(row),
					place: switcherWhere(row, project)})
			}
		}
	}
	// The tab keeper has fresher titles and ownership than the disk snapshot.
	for _, row := range in.closedChats {
		if row.session.ID == "" {
			row.session.ID = homeSessionID(row.session)
		}
		row.session.Archived = true
		key := homeSessionKey(row.session)
		if prev, exists := byID[key]; exists && (prev.here || prev.session.Open) {
			continue
		}
		byID[key] = row
	}
	for _, row := range in.openChats {
		if row.session.ID == "" {
			row.session.ID = homeSessionID(row.session)
		}
		byID[homeSessionKey(row.session)] = row
	}
	rows := make([]switcherRow, 0, len(byID))
	for _, row := range byID {
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].session.At.Equal(rows[j].session.At) {
			return homeSessionKey(rows[i].session) < homeSessionKey(rows[j].session)
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
		cell.key = "sessions:" + homeSessionKey(row.session)
		if row.needs && !needsLandingsSpeakFor(row.session, needsCallTitlesOn(in, row.session.ID)) {
			cell.mark = cellMarkNeeds
		}
		if !in.desc {
			cell.sub, cell.grows = "", false
		}
		lines = append(lines, switcherRowLine(row, cell))
	}
	homeDecorateQuestions(in, lines)
	return in.others.into(in, lines)
}
