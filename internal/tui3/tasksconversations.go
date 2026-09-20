package tui3

import (
	"sort"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
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
			if row.At.IsZero() {
				row.At = rows[i].At
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

// Filtering hides rows without changing which section their conversation belongs in.
func (t *tasksTree) keepConversationStates(full tasksTree) {
	groups := make(map[tasksKey]tasksGroup, len(full.groups))
	for _, group := range full.groups {
		groups[group.chat.key] = group
	}
	for i := range t.groups {
		group := &t.groups[i]
		if original, ok := groups[group.chat.key]; ok {
			group.section = original.section
			group.chat.state = original.chat.state
			group.chat.question = original.chat.question
			group.chat.rank.at = original.chat.rank.at
			for _, root := range group.roots {
				t.under(root, func(item tasksItem, _ int) { t.filed[tasksKeyOf(item.entry)] = group.section })
			}
		}
	}
	sort.SliceStable(t.groups, func(i, j int) bool {
		if t.groups[i].section != t.groups[j].section {
			return t.groups[i].section < t.groups[j].section
		}
		return tasksByAge.less(t.groups[i].chat.rank, t.groups[j].chat.rank, t.sort.back)
	})
}
