package tui3

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/Agent-Field/codeaf/internal/session"
)

// Processes are inspected on demand; completed logs never occupy the work sidebar.
const processListPage = -1

func (a *app) showProcesses(arg string) tea.Cmd {
	if arg = strings.TrimSpace(arg); arg != "" {
		id, err := strconv.Atoi(arg)
		if err != nil || a.jobAt(id) == nil {
			a.note("use /processes or /processes <id> for a process in this conversation")
			return nil
		}
		a.jobList = true
		return a.openJobPage(id)
	}
	a.jobList, a.jobPage, a.jobCursor = true, processListPage, 0
	a.jobHover = -1
	a.jobDraw = nil
	a.touch()
	return nil
}

func (a *app) processRows() []session.JobNotice {
	rows := make([]session.JobNotice, 0, len(a.jobs))
	for i := len(a.jobs) - 1; i >= 0; i-- {
		rows = append(rows, a.jobs[i])
	}
	return rows
}

func (a *app) processListKey(key string) tea.Cmd {
	rows := a.processRows()
	a.jobHover = -1
	switch key {
	case "esc", "left":
		a.closeJobPage()
	case "up", "k", "ctrl+p":
		a.jobCursor = max(0, a.jobCursor-1)
	case "down", "j", "ctrl+n":
		a.jobCursor = min(max(0, len(rows)-1), a.jobCursor+1)
	case "home", "g":
		a.jobCursor = 0
	case "end", "G":
		a.jobCursor = max(0, len(rows)-1)
	case "enter", "right":
		if a.jobCursor >= 0 && a.jobCursor < len(rows) {
			return a.openJobPage(rows[a.jobCursor].ID)
		}
	}
	a.touch()
	return nil
}

// The row index is encoded in its hit so a click uses the exact frame that was drawn.
func (a *app) processListFrame(width, height int) ([]string, []jobPageHit, int, int) {
	if height < 1 || width < 1 {
		return nil, nil, 0, 0
	}
	var lines []string
	var hits []jobPageHit
	add := func(text string, hit jobPageHit) { lines = append(lines, text); hits = append(hits, hit) }
	title := "Processes · this conversation"
	back := "esc back"
	if width >= len(title)+len(back)+2 {
		title += strings.Repeat(" ", width-len(title)-len(back)) + back
	}
	add(a.pal.ink(fit(title, width)), jobPageHitHead)
	add(a.pal.dim(rule(width)), jobPageHitNone)
	rows := a.processRows()
	room := max(1, height-4)
	top := clampTop(a.jobCursor-room+1, len(rows), room)
	if len(rows) == 0 {
		add(a.pal.dim(fit("Processes and their logs appear here when you start background work.", width)), jobPageHitNone)
	}
	for i := top; i < len(rows) && i < top+room; i++ {
		text := " " + jobRowLine(rows[i], a.now(), max(1, width-2), a.pal)
		if i == a.jobCursor {
			text = a.pal.selected(text, 0)
		} else if i == a.jobHover {
			text = a.pal.cursor(text, 0)
		}
		add(text, jobPageHit(1000+i))
	}
	add(a.pal.dim(rule(width)), jobPageHitNone)
	add(paintHint(hintFit("↑↓ choose · enter open log · esc back", width), a.pal, a.pal.dim), jobPageHitFoot)
	if len(lines) > height {
		lines = lines[:height]
		hits = hits[:height]
	}
	return lines, hits, 0, 0
}
