package tui3

import (
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

// memoryPanelRows matches the model picker's twelve-row reading window.
const memoryPanelRows = 12

const (
	memoryFilterHint = "filter · ↑↓ · enter expand · del forget · tab scope · esc close"
	memoryEditHint   = "edit memory · enter save · esc cancel"
)

// memoryStore is the exact durable seam the panel needs. Keeping it narrow
// makes the memory-off state structural and makes every mutation testable.
type MemoryStore interface {
	ListMemories(scope string, limit int) ([]store.Memory, error)
	UpdateMemory(id, title, text string, tags []string) error
	ForgetMemory(id string) error
	RestoreMemory(id string) error
	MemoryProvenance(id string) (string, string, time.Time, error)
}

type memoryStore = MemoryStore

type memoryOrigin struct {
	title string
	at    time.Time
}

// memoryPanel is the whole /memory session. all is loaded once, up to 500
// rows, and filtering is entirely client-side with the model picker's ladder.
type memoryPanel struct {
	open     bool
	all      []store.Memory
	lower    []string
	score    []int
	hits     []int
	cursor   int
	top      int
	scope    int
	filter   editor
	expanded string
	edit     *editor
	origins  map[string]memoryOrigin
	undoID   string
	undoName string
	footer   string
}

var memoryScopes = []string{"", store.MemoryScopeUser, store.MemoryScopeProject, store.MemoryScopeEnv}

func (p *memoryPanel) close() { *p = memoryPanel{} }

func (p *memoryPanel) start(memories []store.Memory) {
	*p = memoryPanel{open: true, all: memories, origins: make(map[string]memoryOrigin)}
	p.reindex()
}

func (p *memoryPanel) reindex() {
	p.lower = make([]string, len(p.all))
	p.score = make([]int, len(p.all))
	for i, memory := range p.all {
		p.lower[i] = strings.ToLower(memory.Title + " " + memory.Text + " " + strings.Join(memory.Tags, " "))
	}
	p.rank()
}

func (p *memoryPanel) rank() {
	tokens := strings.Fields(strings.ToLower(p.filter.String()))
	p.hits = p.hits[:0]
	wantScope := memoryScopes[p.scope]
	for i, haystack := range p.lower {
		if wantScope != "" && p.all[i].Scope != wantScope {
			continue
		}
		total, matched := 0, true
		for _, token := range tokens {
			score, ok := tokenScore(haystack, token)
			if !ok {
				matched = false
				break
			}
			total += score
		}
		if matched {
			p.score[i] = total
			p.hits = append(p.hits, i)
		}
	}
	if len(tokens) > 0 {
		sort.SliceStable(p.hits, func(i, j int) bool { return p.score[p.hits[i]] < p.score[p.hits[j]] })
	}
	p.cursor, p.top = 0, 0
}

func (p *memoryPanel) choice() (store.Memory, bool) {
	if p.cursor < 0 || p.cursor >= len(p.hits) {
		return store.Memory{}, false
	}
	return p.all[p.hits[p.cursor]], true
}

func (p *memoryPanel) move(delta int) {
	p.cursor = moveCursor(p.cursor, delta, len(p.hits))
	p.top = listTop(p.cursor, p.top, len(p.hits), memoryPanelRows)
}

func (p *memoryPanel) remove(id string) {
	for i := range p.all {
		if p.all[i].ID == id {
			p.all = append(p.all[:i], p.all[i+1:]...)
			break
		}
	}
	p.reindex()
}

func (p *memoryPanel) add(memory store.Memory) {
	p.all = append([]store.Memory{memory}, p.all...)
	p.reindex()
}

func (p *memoryPanel) height(width int) int {
	if !p.open {
		return 0
	}
	if p.expanded != "" {
		return memoryPanelRows
	}
	if len(p.hits) == 0 {
		return 2
	}
	rows := overlayWindow(width, p.top, len(p.hits), memoryPanelRows-1, func(at int) string {
		return p.note(p.all[p.hits[at]])
	})
	return rows + 1
}

func (p *memoryPanel) note(memory store.Memory) string {
	parts := []string{memory.Type, memory.Scope}
	if origin, ok := p.origins[memory.ID]; ok {
		if age := store.AgeLabel(origin.at, time.Now()); age != "" {
			parts = append(parts, age)
		}
	}
	return strings.Join(parts, " · ")
}

func (p *memoryPanel) rows(width, n int, pal palette, hover int) []string {
	if n <= 0 {
		return nil
	}
	fill := newOverlayFill(width, n, pal, hover)
	if p.expanded != "" {
		memory, ok := p.choiceByID(p.expanded)
		if !ok {
			fill.plain(pal.dim("  memory is no longer available"))
			out, _ := fill.done()
			return out
		}
		fill.plain(pal.bold("  " + memory.Title))
		for _, line := range wrapText(memory.Text, width-4) {
			if !fill.plain("  " + line) {
				break
			}
		}
		if len(memory.Tags) > 0 {
			fill.plain(pal.dim("  tags · " + strings.Join(memory.Tags, ", ")))
		}
		fill.plain(pal.dim("  used · " + itoa(memory.UseCount)))
		origin := p.origins[memory.ID]
		age := store.AgeLabel(origin.at, time.Now())
		if age == "" {
			age = "some time"
		}
		fill.plain(pal.dim("  created · " + age + " ago"))
		learned := "  learned"
		learned += " " + age
		if origin.title != "" {
			learned += " in '" + origin.title + "'"
		} else {
			learned += " ago"
		}
		fill.plain(pal.dim(learned))
		fill.plain(pal.dim("  enter edit · esc list"))
		out, _ := fill.done()
		return out
	}
	if len(p.hits) == 0 {
		fill.plain(pal.dim("  nothing is remembered here"))
	} else {
		p.top = listTop(p.cursor, p.top, len(p.hits), overlayItems(n-1, width))
		for at := p.top; at < len(p.hits) && fill.room(); at++ {
			memory := p.all[p.hits[at]]
			label := memory.Title
			if memory.Text != "" && memory.Text != memory.Title {
				label += " — " + memory.Text
			}
			if memory.UseCount >= 5 {
				label = "* " + label
			}
			if !fill.add(at, label, p.note(memory), at == p.cursor, false) {
				break
			}
		}
	}
	footer := "scope · all"
	if memoryScopes[p.scope] != "" {
		footer = "scope · " + memoryScopes[p.scope]
	}
	if p.footer != "" {
		footer = p.footer + "  ·  " + footer
	}
	fill.plain(pal.dim("  " + footer))
	out, _ := fill.done()
	return out
}

func (p *memoryPanel) choiceByID(id string) (store.Memory, bool) {
	for _, memory := range p.all {
		if memory.ID == id {
			return memory, true
		}
	}
	return store.Memory{}, false
}

func wrapText(text string, width int) []string {
	if width < 1 {
		width = 1
	}
	words := strings.Fields(text)
	if len(words) == 0 {
		return []string{""}
	}
	lines := []string{words[0]}
	for _, word := range words[1:] {
		last := len(lines) - 1
		if len([]rune(lines[last]+" "+word)) <= width {
			lines[last] += " " + word
		} else {
			lines = append(lines, word)
		}
	}
	return lines
}

func (a *app) openMemory() {
	_, ok := a.brain()
	if !ok || a.memory == nil {
		a.note("memory is off · turn it on under /settings")
		return
	}
	memories, err := a.memory.ListMemories("", 500)
	if err != nil {
		a.note("could not read what is remembered · " + err.Error())
		return
	}
	a.memPanel.start(memories)
	for _, memory := range memories {
		_, title, at, err := a.memory.MemoryProvenance(memory.ID)
		if err == nil {
			a.memPanel.origins[memory.ID] = memoryOrigin{title: title, at: at}
		}
	}
	a.touch()
}

func (a *app) memoryKey(msg tea.KeyPressMsg) {
	p := &a.memPanel
	if p.edit != nil {
		switch msg.String() {
		case "esc":
			p.edit = nil
		case "enter":
			memory, ok := p.choiceByID(p.expanded)
			if ok && a.memory.UpdateMemory(memory.ID, memory.Title, p.edit.String(), memory.Tags) == nil {
				for i := range p.all {
					if p.all[i].ID == memory.ID {
						p.all[i].Text = p.edit.String()
					}
				}
				p.reindex()
			}
			p.edit = nil
		default:
			listNavigate(msg, p.edit, func(int) {}, func() {}, memoryPanelRows)
		}
		a.touch()
		return
	}
	if p.expanded != "" {
		switch msg.String() {
		case "esc":
			p.expanded = ""
		case "enter", "e":
			memory, ok := p.choiceByID(p.expanded)
			if ok {
				box := editor{}
				box.setText(memory.Text)
				p.edit = &box
			}
		}
		a.touch()
		return
	}
	switch msg.String() {
	case "esc":
		p.close()
	case "enter":
		if memory, ok := p.choice(); ok {
			p.expanded = memory.ID
		}
	case "delete", "ctrl+d":
		if memory, ok := p.choice(); ok && a.memory.ForgetMemory(memory.ID) == nil {
			p.undoID, p.undoName = memory.ID, memory.Title
			p.footer = "forgot '" + memory.Title + "' — u to undo"
			p.remove(memory.ID)
		}
	case "u":
		if p.undoID != "" && a.memory.RestoreMemory(p.undoID) == nil {
			// The tombstoned row is retained as the one-deep undo payload.
			memories, err := a.memory.ListMemories("", 500)
			if err == nil {
				p.all = memories
				p.reindex()
			}
			p.footer = "restored '" + p.undoName + "'"
			p.undoID, p.undoName = "", ""
		}
	case "tab":
		p.scope = (p.scope + 1) % len(memoryScopes)
		p.rank()
	default:
		listNavigate(msg, &p.filter, p.move, p.rank, memoryPanelRows)
	}
	a.touch()
}
