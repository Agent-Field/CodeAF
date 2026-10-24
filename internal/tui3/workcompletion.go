package tui3

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/charmbracelet/x/ansi"
)

// workCompletion retains a runtime receipt without making another conversational answer.
// owner is an established request turn; zero leaves historical ownership unproven.
type workCompletion struct {
	at                 int
	owner              int
	key                string
	task               uint64
	job                int
	title, state, text string
}

// workCompletionTurn reads admission provenance, never the time the work happened to finish.
func (a *app) workCompletionTurn(id uint64) (int, bool) {
	return a.workCompletionParentTurn(id, map[uint64]bool{})
}

func (a *app) workCompletionParentTurn(id uint64, seen map[uint64]bool) (int, bool) {
	if seen[id] {
		return 0, false
	}
	seen[id] = true
	for _, e := range a.entries {
		if e.kind == entryTask && e.card != nil && e.card.id == id {
			return e.turn, true
		}
		if e.completion != nil && e.completion.task == id && e.completion.owner != 0 {
			return e.completion.owner, true
		}
	}
	if node := a.tasks[id]; node != nil {
		if parent, err := strconv.ParseUint(node.parent, 10, 64); err == nil && parent != id {
			if owner, ok := a.workCompletionParentTurn(parent, seen); ok {
				return owner, true
			}
		}
		if node.workOwned {
			return node.workTurn, true
		}
	}
	return 0, false
}

// recordWorkProcess holds one process identity through its live and finished states.
func (a *app) recordWorkProcess(notice session.JobNotice) {
	if notice.Kind == session.JobKindTask || notice.ID == 0 {
		return
	}
	for i := range a.entries {
		c := a.entries[i].completion
		if c == nil || c.job != notice.ID {
			continue
		}
		c.title, c.state = notice.Label(), string(notice.State)
		return
	}
	// Restored terminal rows have no admission provenance; replay provides it when known.
	if notice.Over() {
		return
	}
	a.entries = append(a.entries, entry{kind: entryNote, turn: a.turn, completion: &workCompletion{owner: a.turn, key: "job/" + itoa(notice.ID), job: notice.ID, title: notice.Label(), state: string(notice.State)}})
}

func (a *app) openWorkCompletion(c *workCompletion) tea.Cmd {
	if c == nil {
		return nil
	}
	if c.job != 0 {
		return a.openJobPage(c.job)
	}
	if c.task != 0 {
		id := strconv.FormatUint(c.task, 10)
		for _, entry := range a.taskSheetOwnRows() {
			if entry.ID == id && a.taskSheetOwnsEntry(&entry) {
				return a.openTaskRecord(&entry)
			}
		}
		if node := a.tasks[c.task]; node != nil {
			return a.openTaskRecord(&session.TaskIndexEntry{ID: id, Title: node.label, Label: node.label, Status: string(node.state), SessionID: a.taskSheetSelfID(), Crew: node.crew, Cost: node.spent(), Model: node.model})
		}
	}
	return nil
}

// revealWorkCompletion is the toast's door into the existing request disclosure.
func (a *app) revealWorkCompletion(turn int) {
	a.openWorkfold(turn)
	a.touch()
	rows := a.visible(a.bodyWidth())
	for i, r := range rows {
		if r.hit == hitWorkFold && r.turn == turn {
			a.offset = max(0, i-a.viewHeight()/3)
			a.stick = false
			return
		}
	}
}

func workCompletionKey(c workCompletion) string {
	if c.task != 0 && c.key == "task/"+strconv.FormatUint(c.task, 10)+"/0" {
		return "task/" + strconv.FormatUint(c.task, 10)
	}
	if c.key != "" {
		return c.key
	}
	if c.task != 0 {
		return "task/" + strconv.FormatUint(c.task, 10)
	}
	if c.job != 0 {
		return "job/" + itoa(c.job)
	}
	return c.text
}

func (a *app) completionRows(cs []workCompletion, width int) []row {
	var out []row
	for i := range cs {
		c := cs[i]
		title := strings.TrimSpace(c.title)
		if title == "" {
			title = firstLine(c.text)
		}
		if title == "" {
			continue
		}
		word := c.state
		if word == "" {
			word = "finished"
		}
		if c.task != 0 && word == string(session.TaskFailed) {
			word = session.ProjectTask(session.TaskFacts{State: session.TaskFailed}).Word
		}
		action := ""
		door := false
		if c.task != 0 {
			door = a.tasks[c.task] != nil
			for _, saved := range a.comp.tasks {
				if saved.ID == strconv.FormatUint(c.task, 10) && a.taskSheetOwnsEntry(&saved) {
					door = true
					break
				}
			}
			if door {
				action = "open result"
			}
		}
		if c.job != 0 {
			door = a.jobAt(c.job) != nil
			if door {
				action = "open log"
			}
		}
		hit := hitNone
		if door {
			hit = hitWorkCompletion
		}
		tail := railSep + word
		if action != "" {
			tail += railSep + action
		}
		name := fit(title, max(1, width-2-ansi.StringWidth(tail)))
		line := "  " + a.pal.ink(name) + a.pal.dim(railSep+word)
		if action != "" {
			line += a.pal.dim(railSep) + a.pal.accent(action)
		}
		out = append(out, row{text: fitPainted(line, width), entry: c.at, hit: hit, completion: &c})
		if door && c.text != "" {
			open := c.at >= 0 && c.at < len(a.entries) && a.entries[c.at].open
			arrow := "▸"
			if open {
				arrow = "▾"
			}
			if a.pal.linear {
				arrow = ">"
				if open {
					arrow = "v"
				}
			}
			word := arrow + " details · ctrl+o"
			out = append(out, row{text: a.pal.dim("    " + word), entry: c.at, hit: hitWorkReceiptDetails, completion: &c})
			if open {
				for _, raw := range strings.Split(c.text, "\n") {
					for _, wrapped := range wrap(raw, max(1, width-6)) {
						out = append(out, row{text: a.pal.muted("      " + wrapped), entry: c.at, completion: &c})
					}
				}
			}
		}
		if !door && c.text != "" {
			for _, raw := range strings.Split(c.text, "\n") {
				for _, wrapped := range wrap(raw, max(1, width-2)) {
					out = append(out, row{text: a.pal.dim("  " + wrapped), entry: c.at, completion: &c})
				}
			}
		}
	}
	// Every child pays its own nesting before the shared work indentation.
	for i := range out {
		out[i].text = fitPainted(out[i].text, width)
	}
	return out
}

func (a *app) workTaskTurn(id uint64) (int, bool) { return a.workCompletionTurn(id) }
func (a *app) workProcessTurn(id int) (int, bool) {
	for _, e := range a.entries {
		if c := e.completion; c != nil && c.job == id {
			return c.owner, c.owner != 0
		}
	}
	return 0, false
}

// withWorkCompletions augments one existing disclosure, never the answer it surrounds.
func (a *app) withWorkCompletions(d deck, folds map[int]workfold) map[int]workfold {
	if d.lens.foldPast != foldTurns {
		return folds
	}
	type group struct {
		owner, anchor int
		unknown       bool
		items         []workCompletion
		keys          map[string]int
	}
	var groups []*group
	byTurn := map[int]*group{}
	unknown := map[int]*group{}
	unknownIdentity := map[string]*group{}
	add := func(c workCompletion, at int) {
		c.at = at
		if c.owner == 0 && c.task != 0 {
			if owner, ok := a.workCompletionTurn(c.task); ok && owner != 0 {
				c.owner = owner
			}
		}
		if c.state == string(session.JobRunning) || c.state == string(session.TaskQueued) {
			return
		}
		var g *group
		if c.owner != 0 {
			g = byTurn[c.owner]
		} else {
			g = unknownIdentity[workCompletionKey(c)]
			if g == nil {
				g = unknown[d.entries[at].turn]
			}
		}
		if g == nil {
			g = &group{owner: c.owner, anchor: at, unknown: c.owner == 0, keys: map[string]int{}}
			if c.owner != 0 {
				byTurn[c.owner] = g
			} else {
				unknown[d.entries[at].turn] = g
			}
			groups = append(groups, g)
		}
		key := workCompletionKey(c)
		if c.owner == 0 {
			unknownIdentity[key] = g
		}
		if prev, ok := g.keys[key]; ok {
			old := &g.items[prev]
			if c.text != "" && !strings.Contains(old.text, c.text) {
				if old.text != "" {
					old.text += "\n"
				}
				old.text += c.text
			}
			if c.title != "" {
				old.title = c.title
			}
			if c.state != "" {
				old.state = c.state
			}
			if c.task != 0 {
				old.task = c.task
			}
			if c.job != 0 {
				old.job = c.job
			}
			return
		}
		g.keys[key] = len(g.items)
		g.items = append(g.items, c)
	}
	latestTaskKey := map[uint64]string{}
	for _, e := range d.entries {
		if c := e.completion; c != nil && c.task != 0 {
			latestTaskKey[c.task] = workCompletionKey(*c)
		}
	}
	for i, e := range d.entries {
		if e.completion != nil {
			add(*e.completion, i)
		}
		if e.done == nil || !e.done.quiet {
			continue
		}
		owner, ok := a.workCompletionTurn(e.done.id)
		if !ok {
			owner = 0
		}
		key := latestTaskKey[e.done.id]
		if key == "" {
			key = "task/" + strconv.FormatUint(e.done.id, 10)
		}
		text := ""
		if node := a.tasks[e.done.id]; node != nil && node.crew != nil {
			text = "crew · " + a.crewLine(node.crew, node.spent())
		}
		add(workCompletion{owner: owner, key: key, task: e.done.id, title: e.done.title, state: e.done.status.Word, text: text}, i)
	}
	if len(groups) == 0 {
		return folds
	}
	if folds == nil {
		folds = map[int]workfold{}
	}
	for _, g := range groups {
		if !g.unknown && g.owner == d.runningTurn && d.runningTurn != 0 {
			continue
		}
		at := -1
		if !g.unknown {
			for index, f := range folds {
				if f.turn == g.owner && (at < 0 || index < at) {
					at = index
				}
			}
		}
		if at >= 0 {
			f := folds[at]
			f.completions = g.items
			folds[at] = f
			continue
		}
		anchor := g.anchor
		key := g.owner
		if g.unknown {
			key = -1000000000 - anchor
		} else {
			for i, e := range d.entries {
				if e.turn == g.owner {
					anchor = i
					if groupBreaks(&e) {
						anchor = i + 1
					}
					break
				}
			}
		}
		if anchor >= len(d.entries) {
			anchor = len(d.entries) - 1
		}
		if _, exists := folds[anchor]; exists {
			continue
		}
		folds[anchor] = workfold{key: key, turn: g.owner, start: anchor, answer: anchor, virtual: true, background: g.unknown, completions: g.items}
	}
	return folds
}

// Running work uses the same receipt projection without creating a second Worked chip.
func (a *app) workCompletionsFor(d deck, turn int) []workCompletion {
	d.runningTurn = 0
	for _, f := range a.withWorkCompletions(d, nil) {
		if f.turn == turn && !f.background {
			return f.completions
		}
	}
	return nil
}

func (a *app) selectedWorkCompletion() *workCompletion {
	for _, r := range a.visible(a.bodyWidth()) {
		if r.entry == a.sel && r.hit == hitWorkCompletion {
			return r.completion
		}
	}
	return nil
}

func (a *app) liveCompletionSummary(w liveWork, rows []row, width int) []row {
	if len(w.completions) > 0 && len(rows) > 0 {
		rows[0].text = fitPainted(rows[0].text+a.pal.dim(railSep+itoa(len(w.completions))+" finished"), max(1, width-workIndentCols(width)-actionGutter))
	}
	return rows
}

func (a *app) toggleWorkCompletionDetails(c *workCompletion) {
	if c == nil || c.at < 0 || c.at >= len(a.entries) {
		return
	}
	a.entries[c.at].open = !a.entries[c.at].open
	a.touch()
}
