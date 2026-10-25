package tui3

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/knowledge"
	"github.com/charmbracelet/x/ansi"
)

// Knowledge keeps the existing saved-memory controls intact. The runtime owns
// consent and compilation; this place only holds a reading and navigation state.
type knowledgePlace struct {
	open, loading       bool
	vault               *knowledge.Vault
	reading             knowledge.Snapshot
	filter              editor
	cursor, top, scroll int
	selected            string
	history             []string
	folded              map[string]bool
	links               bool
	article             bool
	linkCursor          int
	footer              string
	targets             []knowledgeTarget
}

type knowledgeTarget struct {
	id, title, folder string
	depth             int
}
type knowledgeCellLink struct {
	start, end int
	id         string
}
type knowledgeHit struct {
	tree   int
	links  []knowledgeCellLink
	create bool
}

// Wrapping carries canonical link spans into terminal cell coordinates, including
// links split over two rows and wide Unicode characters.
func knowledgeParagraph(p knowledge.Paragraph, width int) ([]string, [][]knowledgeCellLink) {
	raw := []rune(strings.ReplaceAll(p.Text, "\t", " "))
	var text []rune
	var spans []struct {
		start, end int
		id         string
	}
	cursor := 0
	for _, l := range p.InlineLinks {
		if l.Start < cursor || l.Start < 0 || l.End > len(raw) || l.End <= l.Start {
			continue
		}
		text = append(text, raw[cursor:l.Start]...)
		start := len(text)
		label := string(raw[l.Start:l.End])
		if l.Label != "" {
			label = l.Label
		}
		text = append(text, []rune("[["+label+"]]")...)
		spans = append(spans, struct {
			start, end int
			id         string
		}{start, len(text), l.ID})
		cursor = l.End
	}
	text = append(text, raw[cursor:]...)
	lines, offsets := wrapWithOffsets(string(text), max(4, width))
	cells := make([][]knowledgeCellLink, len(lines))
	for i, line := range lines {
		r := []rune(line)
		lo, hi := offsets[i], offsets[i]+len(r)
		for _, s := range spans {
			start, end := max(lo, s.start), min(hi, s.end)
			if start < end {
				cells[i] = append(cells[i], knowledgeCellLink{ansi.StringWidth(string(r[:start-lo])), ansi.StringWidth(string(r[:end-lo])), s.id})
			}
		}
	}
	return lines, cells
}

type knowledgeReadingMsg struct {
	vault   *knowledge.Vault
	reading knowledge.Snapshot
	err     error
}

func (a *app) knowledgeVault() *knowledge.Vault {
	if host, ok := a.agent.(interface{ Knowledge() *knowledge.Vault }); ok {
		return host.Knowledge()
	}
	if a.hosted() || strings.TrimSpace(a.workspace) == "" {
		return nil
	}
	profile := config.ProfilePath(a.profileDir, "")
	if root := strings.TrimSpace(a.homeRoot); root != "" {
		profile = root
	}
	return knowledge.Open(filepath.Clean(profile), a.workspace)
}

func (a *app) knowledgeRead(create bool) tea.Cmd {
	p := &a.kb
	v := a.knowledgeVault()
	if v == nil || p.vault == nil || p.vault.Directory() != v.Directory() {
		open := p.open
		*p = knowledgePlace{open: open, vault: v}
	} else {
		v = p.vault
	}
	if v == nil {
		p.footer = "Knowledge is available in a local project conversation."
		return nil
	}
	if p.loading {
		return nil
	}
	p.loading = true
	agent := a.agent
	return func() tea.Msg {
		if create {
			if err := v.Create(); err != nil {
				return knowledgeReadingMsg{vault: v, err: err}
			}
		}
		r, err := v.Snapshot()
		if capture, ok := agent.(interface{ KnowledgeCaptureError() string }); ok {
			if problem := capture.KnowledgeCaptureError(); problem != "" {
				if r.LastError != "" {
					r.LastError += " · "
				}
				r.LastError += "Capture paused: " + problem
			}
		}
		return knowledgeReadingMsg{vault: v, reading: r, err: err}
	}
}

func (a *app) adoptKnowledge(msg knowledgeReadingMsg) {
	p := &a.kb
	if p.vault != msg.vault {
		return
	}
	p.loading = false
	if msg.err != nil {
		p.footer = "Knowledge could not be read: " + msg.err.Error()
	} else {
		p.reading = msg.reading
		p.footer = ""
		p.rank()
	}
	a.touch()
}

func (p *knowledgePlace) rank() {
	q := strings.ToLower(strings.TrimSpace(p.filter.String()))
	pages := append([]knowledge.Page(nil), p.reading.Pages...)
	pretty := false
	for _, n := range pages {
		if n.Kind == "subject" || n.Kind == "map" {
			pretty = true
			break
		}
	}
	sort.Slice(pages, func(i, j int) bool {
		return strings.Join(pages[i].Folder, "/")+pages[i].Title < strings.Join(pages[j].Folder, "/")+pages[j].Title
	})
	p.targets = nil
	seen := map[string]bool{}
	for _, n := range pages {
		if pretty && n.Kind != "subject" && n.Kind != "map" {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(n.Title+" "+strings.Join(n.Folder, " ")+" "+knowledgePageText(n)), q) {
			continue
		}
		blocked := false
		for i := range n.Folder {
			path := strings.Join(n.Folder[:i+1], "/")
			if !seen[path] {
				p.targets = append(p.targets, knowledgeTarget{title: n.Folder[i], folder: path, depth: i})
				seen[path] = true
			}
			if p.folded[path] && q == "" {
				blocked = true
				break
			}
		}
		if !blocked {
			p.targets = append(p.targets, knowledgeTarget{id: n.ID, title: n.Title, depth: len(n.Folder)})
		}
	}
	if len(p.targets) == 0 {
		p.cursor = 0
	} else {
		p.cursor = max(0, min(p.cursor, len(p.targets)-1))
	}
	if p.selected == "" {
		for _, n := range p.targets {
			if n.id != "" {
				p.selected = n.id
				break
			}
		}
	}
}

func knowledgePageText(n knowledge.Page) string {
	var out strings.Builder
	for _, s := range n.Sections {
		out.WriteString(s.Heading + "\n")
		for _, p := range s.Paragraphs {
			out.WriteString(p.Text + "\n")
		}
	}
	return out.String()
}

func (p *knowledgePlace) page() (knowledge.Page, bool) {
	for _, n := range p.reading.Pages {
		if n.ID == p.selected {
			return n, true
		}
	}
	return knowledge.Page{}, false
}

func (p *knowledgePlace) navigate(id string) {
	if id == "" {
		return
	}
	if id == p.selected {
		p.article = true
		p.links = false
		return
	}
	for _, n := range p.reading.Pages {
		if n.ID == id {
			p.history = append(p.history, p.selected)
			p.selected = id
			p.scroll = 0
			p.links = false
			p.article = true
			return
		}
	}
	p.footer = "That linked page is no longer available."
}

func (a *app) knowledgeOwns(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	p := &a.kb
	k := msg.String()
	if k == "alt+u" && a.mem.edit == nil {
		p.open = !p.open
		a.touch()
		if p.open {
			return a.knowledgeRead(false), true
		}
		return nil, true
	}
	if !p.open {
		return nil, false
	}
	if a.bar.on || ((k == "up" || k == "ctrl+p") && !p.links && p.cursor == 0) {
		return nil, false
	}
	switch k {
	case "esc":
		switch {
		case p.links:
			p.links = false
		case !p.filter.empty():
			p.filter.reset()
			p.rank()
		case len(p.history) > 0:
			p.selected = p.history[len(p.history)-1]
			p.history = p.history[:len(p.history)-1]
			p.scroll = 0
		case p.article:
			p.article = false
		default:
			p.open = false
		}
	case "enter":
		if !p.reading.Enabled {
			return a.knowledgeRead(true), true
		}
		if p.links {
			if links := p.pageLinks(); len(links) > 0 {
				p.navigate(links[min(p.linkCursor, len(links)-1)].ID)
			}
		} else if len(p.targets) > 0 {
			n := p.targets[p.cursor]
			if n.folder != "" {
				if p.folded == nil {
					p.folded = map[string]bool{}
				}
				p.folded[n.folder] = !p.folded[n.folder]
				p.rank()
			} else {
				p.navigate(n.id)
			}
		}
	case "ctrl+l":
		p.links = !p.links
		p.linkCursor = 0
		p.scroll = 0
	case "ctrl+r":
		return a.knowledgeRead(false), true
	case "up", "down", "ctrl+p", "ctrl+n":
		delta := 1
		if k == "up" || k == "ctrl+p" {
			delta = -1
		}
		if p.links {
			p.linkCursor = max(0, min(p.linkCursor+delta, len(p.pageLinks())-1))
		} else {
			p.cursor = max(0, min(p.cursor+delta, len(p.targets)-1))
		}
	case "pgdown":
		p.scroll += 12
	case "pgup":
		p.scroll = max(0, p.scroll-12)
	case "left":
		if len(p.history) > 0 {
			p.selected = p.history[len(p.history)-1]
			p.history = p.history[:len(p.history)-1]
			p.scroll = 0
		}
	case "right":
		if len(p.targets) > 0 {
			n := p.targets[p.cursor]
			if n.folder != "" {
				delete(p.folded, n.folder)
				p.rank()
			} else {
				p.navigate(n.id)
			}
		}
	default:
		if strings.HasPrefix(k, "alt+") || k == "tab" || k == "shift+tab" {
			return nil, false
		}
		if msg.Key().Text != "" || k == "backspace" || k == "delete" {
			listNavigate(msg, &p.filter, func(int) {}, func() {}, memoryPlaceRows)
			p.cursor = 0
			p.rank()
		} else {
			return nil, false
		}
	}
	a.touch()
	return nil, true
}

func (p *knowledgePlace) pageLinks() []knowledge.Link {
	n, ok := p.page()
	if !ok {
		return nil
	}
	links := append([]knowledge.Link(nil), n.Links...)
	seen := map[string]bool{}
	for _, l := range links {
		seen[l.ID] = true
	}
	for _, other := range p.reading.Pages {
		for _, l := range other.Links {
			if l.ID == n.ID && !seen[other.ID] {
				links = append(links, knowledge.Link{ID: other.ID, Kind: "backlink"})
				seen[other.ID] = true
			}
		}
	}
	return links
}

func (a *app) knowledgeBody(width, room int) []placeRow {
	p := &a.kb
	var rows []placeRow
	add := func(s string) { rows = append(rows, placeRow{text: " " + fit(s, max(1, width-2)), hit: -1}) }
	add(a.pal.muted("Knowledge") + a.pal.dim("  · alt+u Saved memory"))
	if p.vault == nil {
		add("")
		add("Knowledge is available in a local project conversation.")
		return rows
	}
	if p.loading {
		add(a.pal.dim("Reading knowledge…"))
	}
	if !p.reading.Enabled {
		add("")
		add(a.pal.accent("› Create knowledge base"))
		rows[len(rows)-1].hit = knowledgeHit{tree: -1, create: true}
		add("")
		for _, line := range wrapText("Start learning from future chats in this project. Existing conversations are not imported automatically. Saved memory continues to hold your preferences and rules.", max(1, width-4)) {
			add(line)
		}
		if p.footer != "" {
			add("")
			add(p.footer)
		}
		return rows
	}
	add(a.pal.dim(fmt.Sprintf("%d pages · %d queued · new updates $%.4f", len(p.reading.Pages), p.reading.Pending, p.reading.CostUSD)))
	if p.reading.LastError != "" {
		add(a.pal.bad("Update paused: ") + p.reading.LastError)
	} else if p.reading.NeedsUpdate {
		add(a.pal.dim("Updating knowledge…"))
	}
	if len(p.reading.Pages) == 0 {
		add("")
		add("Your knowledge base is ready.")
		add("New completed chats will appear here after a background update.")
		return rows
	}
	add(a.pal.dim("Search: ") + p.filter.String())
	left := max(24, min(42, width/3))
	narrow := width < 85
	if narrow {
		left = width - 2
	}
	leftRows := []string{}
	for _, n := range p.targets {
		mark := "◇ "
		if n.folder != "" {
			mark = "▾ "
			if p.folded[n.folder] {
				mark = "▸ "
			}
		}
		leftRows = append(leftRows, strings.Repeat("  ", n.depth)+mark+n.title)
	}
	article := []string{}
	articleHits := map[int][]knowledgeCellLink{}
	selectedConnection := -1
	if n, ok := p.page(); ok {
		article = append(article, a.pal.muted(n.Title), "")
		if n.NeedsEditorialUpdate {
			article = append(article, a.pal.dim("New evidence appears under Recent knowledge; guide integration is pending."), "")
		}
		rightWidth := width - left - 5
		if narrow {
			rightWidth = width - 4
		}
		for _, s := range n.Sections {
			if s.Heading != "" {
				article = append(article, a.pal.muted(s.Heading), "")
			}
			for _, para := range s.Paragraphs {
				lines, cells := knowledgeParagraph(para, max(1, rightWidth))
				for i, line := range lines {
					articleHits[len(article)] = cells[i]
					article = append(article, line)
				}
				article = append(article, "")
			}
		}
		links := p.pageLinks()
		if p.links {
			article = []string{a.pal.muted("Linked pages"), ""}
			articleHits = map[int][]knowledgeCellLink{}
		}
		if len(links) > 0 {
			article = append(article, a.pal.muted("Linked pages · ctrl+l to follow"))
			for i, l := range links {
				title := l.ID
				for _, target := range p.reading.Pages {
					if target.ID == l.ID {
						title = target.Title
						break
					}
				}
				text := fmt.Sprintf("%d. %s", i+1, title)
				if l.Kind == "backlink" {
					text += " ←"
				}
				if p.links && i == p.linkCursor {
					selectedConnection = len(article)
					text = a.pal.accent("› " + text)
				}
				articleHits[len(article)] = []knowledgeCellLink{{0, max(1, rightWidth), l.ID}}
				article = append(article, fit(text, max(1, rightWidth)))
			}
		}
		if len(n.Evidence) > 0 && !p.links {
			article = append(article, "", a.pal.muted("Sources"))
			for _, e := range n.Evidence {
				article = append(article, a.pal.dim(e.Role+" · "+e.TS+" · "+e.Source))
				article = append(article, wrapText("“"+e.Quote+"”", max(1, rightWidth))...)
			}
		}
	}
	bodyRoom := max(1, room-len(rows))
	if p.links && selectedConnection >= 0 {
		p.scroll = placeTop(p.scroll, selectedConnection, len(article), bodyRoom)
	}
	if narrow {
		if p.links || p.article {
			p.scroll = min(p.scroll, max(0, len(article)-bodyRoom))
			for i, line := range article[min(p.scroll, len(article)):] {
				add(line)
				rows[len(rows)-1].hit = knowledgeHit{tree: -1, links: knowledgeShift(articleHits[p.scroll+i], 1)}
			}
		} else {
			p.top = placeTop(p.top, p.cursor, len(leftRows), bodyRoom)
			for i, line := range leftRows[p.top:] {
				if p.top+i == p.cursor {
					line = a.pal.accent("› " + line)
				}
				add(line)
				rows[len(rows)-1].hit = knowledgeHit{tree: p.top + i}
			}
		}
	} else {
		p.scroll = min(p.scroll, max(0, len(article)-bodyRoom))
		p.top = placeTop(p.top, p.cursor, len(leftRows), bodyRoom)
		for i := 0; i < bodyRoom; i++ {
			l, r := "", ""
			if j := p.top + i; j < len(leftRows) {
				l = leftRows[j]
				if j == p.cursor {
					l = a.pal.accent("› " + l)
				}
			}
			if j := p.scroll + i; j < len(article) {
				r = article[j]
			}
			add(taskPanePad(fit(l, left), left) + a.pal.dim(" │ ") + r)
			rows[len(rows)-1].hit = knowledgeHit{tree: p.top + i, links: knowledgeShift(articleHits[p.scroll+i], left+4)}
		}
	}
	if len(rows) > room {
		rows = rows[:room]
	}
	return rows
}

func knowledgeShift(links []knowledgeCellLink, by int) []knowledgeCellLink {
	out := append([]knowledgeCellLink(nil), links...)
	for i := range out {
		out[i].start += by
		out[i].end += by
	}
	return out
}

func (placeMemory) pressAt(a *app, x, y int) (tea.Cmd, bool) {
	if !a.kb.open {
		return placeMemory{}.press(a, y)
	}
	p := &a.kb
	_, hits, _, _ := a.placeDraw(placeMemory{}, a.width, a.height)
	if y < 0 || y >= len(hits) {
		return nil, true
	}
	if hit, ok := hits[y].(knowledgeHit); ok {
		if hit.create && !p.reading.Enabled {
			return a.knowledgeRead(true), true
		}
		for _, link := range hit.links {
			if x >= link.start && x < link.end {
				p.navigate(link.id)
				a.touch()
				return nil, true
			}
		}
		left := max(24, min(42, a.width/3))
		if (a.width < 85 || x <= left) && hit.tree >= 0 && hit.tree < len(p.targets) {
			p.cursor = hit.tree
			return a.knowledgeOwnsEnter(), true
		}
	}
	return nil, true
}
func (a *app) knowledgeOwnsEnter() tea.Cmd {
	cmd, _ := a.knowledgeOwns(tea.KeyPressMsg{Code: tea.KeyEnter})
	return cmd
}
