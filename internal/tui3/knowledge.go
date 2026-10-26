package tui3

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/knowledge"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
	"github.com/charmbracelet/x/ansi"
)

const knowledgeBuildLabel = "› Build knowledge base"

// Knowledge keeps the existing saved-memory controls intact. The runtime owns
// consent and compilation; this place only holds a reading and navigation state.
type knowledgePlace struct {
	open, loading       bool
	vault               *knowledge.Vault
	reading             knowledge.Snapshot
	index               knowledgeIndex
	filter              editor
	cursor, top, scroll int
	selected            string
	hoverLink           string
	history             []string
	folded              map[string]bool
	links               bool
	sources             bool
	sourceScroll        int
	article             bool
	linkCursor          int
	footer              string
	matches             map[string]bool
	targets             []knowledgeTarget
	hitRows             []placeHit
	sourceRender        knowledgeSourceRender
	edit                *knowledgePageEdit
	markdownRender      knowledgeMarkdownRender
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
	tree       int
	article    bool
	links      []knowledgeCellLink
	create     bool
	editor     bool
	editOffset int
	editLine   string
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
	vault            *knowledge.Vault
	reading          knowledge.Snapshot
	err              error
	mutation, editID string
	mutationApplied  bool
}

func (a *app) knowledgeVault() *knowledge.Vault {
	if a.knowledgeProfile != nil && !a.hosted() {
		return a.knowledgeProfile
	}
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
	return knowledge.Open(filepath.Clean(profile))
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
	cached := p.reading
	return func() tea.Msg {
		if create {
			if err := v.StartBuild(); err != nil {
				return knowledgeReadingMsg{vault: v, err: err}
			}
		}
		r, err := v.Status()
		if err == nil {
			if r.Revision == cached.Revision {
				r.Pages = cached.Pages
			} else {
				r, err = v.Snapshot()
			}
		}
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
	a.knowledgeAdoptEdit(msg)
	if msg.err != nil {
		p.footer = "Knowledge could not be read: " + msg.err.Error()
	} else {
		changed := p.reading.Revision != msg.reading.Revision || p.reading.Enabled != msg.reading.Enabled
		p.reading = msg.reading
		if !changed {
			a.touch()
			return
		}
		p.sourceRender = knowledgeSourceRender{}
		p.markdownRender = knowledgeMarkdownRender{}
		p.indexKnowledge()
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
		if q != "" && !p.matches[n.ID] {
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

// Folding the whole tree includes folders currently hidden by a collapsed
// parent. Opening that parent later therefore preserves its children's state.
func (p *knowledgePlace) foldAll(collapsed bool) {
	p.folded = map[string]bool{}
	if collapsed {
		for _, page := range p.reading.Pages {
			for i := range page.Folder {
				p.folded[strings.Join(page.Folder[:i+1], "/")] = true
			}
		}
	}
	p.filter.reset()
	p.cursor, p.top = 0, 0
	p.hoverLink = ""
	p.rank()
}

func (p *knowledgePlace) page() (knowledge.Page, bool) {
	return p.indexedPage(p.selected)
}

func (p *knowledgePlace) navigate(id string) {
	if p.sources {
		p.toggleSources()
	}
	p.hoverLink = ""
	if id == "" {
		return
	}
	if id == p.selected {
		p.article = true
		p.links = false
		return
	}
	if _, ok := p.indexedPage(id); ok {
		p.history = append(p.history, p.selected)
		p.selected = id
		p.syncCursor()
		p.scroll = 0
		p.links = false
		p.article = true
		return
	}
	p.footer = "That linked page is no longer available."
}

func (a *app) knowledgeOwns(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	p := &a.kb
	if p.open && p.edit != nil {
		return a.knowledgeEditKey(msg), true
	}
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
	case "ctrl+e":
		return a.knowledgeEditAction("@edit")
	case "ctrl+d":
		return a.knowledgeEditAction("@delete")
	case "ctrl+left":
		p.foldAll(true)
	case "ctrl+right":
		p.foldAll(false)
	case "esc":
		switch {
		case p.sources:
			p.toggleSources()
		case p.links:
			p.links = false
		case !p.filter.empty():
			p.filter.reset()
			p.rank()
		case len(p.history) > 0:
			p.selected = p.history[len(p.history)-1]
			p.history = p.history[:len(p.history)-1]
			p.scroll = 0
			p.syncCursor()
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
	case "ctrl+o":
		p.toggleSources()
	case "ctrl+l":
		if p.sources {
			p.toggleSources()
		}
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
		if p.sources {
			p.toggleSources()
			break
		}
		if len(p.history) > 0 {
			p.selected = p.history[len(p.history)-1]
			p.history = p.history[:len(p.history)-1]
			p.scroll = 0
			p.syncCursor()
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
			a.touch()
			return a.knowledgeSearch(), true
		} else {
			return nil, false
		}
	}
	a.touch()
	return nil, true
}

func (p *knowledgePlace) toggleSources() {
	p.hoverLink = ""
	if p.sources {
		p.sources = false
		p.scroll = p.sourceScroll
		return
	}
	n, ok := p.page()
	if !ok || len(n.Evidence) == 0 {
		return
	}
	p.sources, p.article, p.links = true, true, false
	p.sourceScroll, p.scroll = p.scroll, 0
}

// Multiple claims may quote the same source. Present that source once while
// retaining its distinct quotations; identifiers remain an implementation detail.
func knowledgeSources(evidence []knowledge.Evidence) []knowledge.Evidence {
	var out []knowledge.Evidence
	indexes := map[string]int{}
	quotes := map[string]map[string]bool{}
	for _, e := range evidence {
		id := e.Source
		if id == "" {
			id = e.Role + "\x00" + e.TS + "\x00" + e.Quote
		}
		if i, ok := indexes[id]; ok {
			if e.Quote != "" && !quotes[id][e.Quote] {
				out[i].Quote += "\n\n" + e.Quote
				quotes[id][e.Quote] = true
			}
			continue
		}
		indexes[id] = len(out)
		quotes[id] = map[string]bool{e.Quote: true}
		out = append(out, e)
	}
	return out
}

func knowledgeSourceLabel(e knowledge.Evidence) string {
	role := "Conversation"
	switch e.Role {
	case "user", "human":
		role = "You"
	case "assistant", "agent":
		role = "Assistant"
	case "tool":
		role = "Tool result"
	}
	if date, err := time.Parse(time.RFC3339Nano, e.TS); err == nil {
		return role + " · " + date.Format("Jan 2, 2006")
	}
	return role
}

func (p *knowledgePlace) pageLinks() []knowledge.Link {
	return p.index.links[p.selected]
}

func (a *app) knowledgeBody(width, room int) []placeRow {
	p := &a.kb
	if p.edit != nil {
		return a.knowledgeEditBody(width, room)
	}
	left := max(24, min(42, width/3))
	narrow := width < 85
	if narrow {
		left = width - 2
	}
	var rows []placeRow
	add := func(s string) { rows = append(rows, placeRow{text: " " + fit(s, max(1, width-2)), hit: -1}) }
	add(a.pal.bold(a.pal.muted("Knowledge")))
	if p.reading.Enabled {
		stats := a.pal.data(fmt.Sprint(len(p.reading.Pages))) + a.pal.dim(" pages")
		if p.reading.Pending > 0 {
			stats += a.pal.dim(" · ") + a.pal.data(fmt.Sprint(p.reading.Pending)) + a.pal.dim(" queued")
		}
		if p.reading.CostUSD > 0 {
			stats += a.pal.dim(" · ") + a.pal.data(fmt.Sprintf("$%.4f", p.reading.CostUSD))
		}
		add(stats)
	}
	label := "Saved memory"
	switchInk := a.pal.muted(label)
	if p.hoverLink == "@switch" {
		switchInk = a.pal.underline(switchInk)
	}
	add(switchInk + a.pal.dim(a.chords.say(" · alt+u")))
	rows[len(rows)-1].hit = knowledgeHit{tree: -1, links: []knowledgeCellLink{{1, 1 + ansi.StringWidth(label), "@switch"}}}
	add("")
	if p.vault == nil {
		add("")
		add("Knowledge is available in a local project conversation.")
		return rows
	}
	if p.loading {
		add(a.pal.dim("Reading knowledge…"))
	}
	if !p.reading.Enabled {
		if p.reading.Build.Error != "" {
			add(a.pal.bad(p.reading.Build.Error))
		}
		add("")
		build := a.pal.bold(a.pal.ink(knowledgeBuildLabel))
		if p.hoverLink == "@build" {
			build = a.pal.cursor(a.pal.accent(knowledgeBuildLabel), ansi.StringWidth(knowledgeBuildLabel))
		}
		add(build)
		rows[len(rows)-1].hit = knowledgeHit{tree: -1, create: true}
		add("")
		for _, line := range wrapText("Build knowledge from all saved chats across projects. This runs in the background; you can keep chatting. Saved memory holds your preferences and rules.", max(1, width-4)) {
			add(line)
		}
		if p.footer != "" {
			add("")
			add(p.footer)
		}
		return rows
	}
	progressWidth := max(1, width-2)
	if !narrow && len(p.reading.Pages) > 0 {
		progressWidth = left
	}
	if p.reading.Build.Phase == knowledge.BuildRunning {
		for _, line := range wrap(a.pal.ink("Building knowledge base…")+a.pal.dim(" You can keep chatting."), progressWidth) {
			add(line)
		}
		if p.reading.Build.Chats > 0 {
			for _, line := range wrap(knowledgeProgress("Chats imported", p.reading.Build.Imported, p.reading.Build.Chats, a.pal), progressWidth) {
				add(line)
			}
		}
		if p.reading.Build.Sources > 0 {
			processed := p.reading.Build.Sources - p.reading.Pending
			for _, line := range wrap(knowledgeProgress("Sources processed", processed, p.reading.Build.Sources, a.pal), progressWidth) {
				add(line)
			}
			if p.reading.Pending == 0 {
				add(a.pal.ink("Organizing knowledge pages…"))
			}
		}
	} else if p.reading.Build.Phase == knowledge.BuildFailed || p.reading.Build.Phase == knowledge.BuildPartial {
		add(a.pal.bad("Knowledge build paused"))
		add(a.pal.dim(p.reading.Build.Error))
	} else if p.reading.LastError != "" {
		add(a.pal.bad("Update paused"))
		for _, line := range wrapText(p.reading.LastError, max(1, left)) {
			add(a.pal.dim(line))
		}
	} else if p.reading.NeedsUpdate {
		add(a.pal.dim("Updating knowledge…"))
	}
	if len(p.reading.Pages) == 0 {
		add("")
		if p.reading.Build.Phase == knowledge.BuildRunning || p.reading.Build.Phase == knowledge.BuildFailed || p.reading.Build.Phase == knowledge.BuildPartial {
			return rows
		}
		add("Knowledge will populate as you chat.")
		add("New completed chats will appear here after a background update.")
		return rows
	}
	search := a.pal.dim("Search: ") + p.filter.String()
	if p.hoverLink == "@search" {
		search = a.pal.cursor(search, left)
	}
	add(search)
	rows[len(rows)-1].hit = knowledgeHit{tree: -1, links: []knowledgeCellLink{{1, max(2, width-2), "@search"}}}
	controls := []struct{ label, id string }{{"Collapse all", "@collapse"}, {"Expand all", "@expand"}}
	var controlText strings.Builder
	var controlHits []knowledgeCellLink
	controlWidth := 0
	for i, control := range controls {
		if i > 0 {
			controlText.WriteString(a.pal.dim("  "))
			controlWidth += 2
		}
		painted := taskLinkInk(a.pal, control.label)
		if p.hoverLink == control.id {
			painted = taskLinkHotInk(a.pal, control.label)
		}
		controlText.WriteString(painted)
		end := controlWidth + ansi.StringWidth(control.label)
		controlHits = append(controlHits, knowledgeCellLink{controlWidth + 1, end + 1, control.id})
		controlWidth = end
	}

	add(controlText.String())
	rows[len(rows)-1].hit = knowledgeHit{tree: -1, links: controlHits}
	add("")
	sidebar := rows
	if !narrow {
		rows = nil
	}
	article := []string{}
	articleHits := map[int][]knowledgeCellLink{}
	selectedConnection := -1
	if n, ok := p.page(); ok {
		article = append(article, a.pal.muted(n.Title))
		actions, hits := knowledgeActionRow(a.pal, p.hoverLink, "Edit", "@edit", "Delete", "@delete")
		articleHits[len(article)] = hits
		article = append(article, actions, "")
		if n.HumanEdited {
			article = append(article, a.pal.dim("Edited by you · additions are not source-verified"), "")
		}
		if n.NeedsEditorialUpdate {
			article = append(article, a.pal.dim("New evidence appears under Recent knowledge; guide integration is pending."), "")
		}
		rightWidth := width - left - 5
		if narrow {
			rightWidth = width - 4
		}
		rightWidth = min(rightWidth, 100)
		for section, s := range n.Sections {
			if s.Heading != "" {
				article = append(article, a.pal.muted(s.Heading), "")
			}
			for paragraph, para := range s.Paragraphs {
				var lines []string
				var cells [][]knowledgeCellLink
				if n.HumanEdited {
					lines, cells = a.knowledgeHumanParagraph(n, section, paragraph, max(1, rightWidth))
				} else {
					lines, cells = knowledgeParagraph(para, max(1, rightWidth))
				}
				for i, line := range lines {
					if len(cells[i]) == 0 {
						article = append(article, line)
						continue
					}
					articleHits[len(article)] = cells[i]
					var painted strings.Builder
					at := 0
					for _, cell := range cells[i] {
						painted.WriteString(ansi.Cut(line, at, cell.start))
						label := ansi.Cut(line, cell.start, cell.end)
						if p.hoverLink == cell.id {
							painted.WriteString(taskLinkHotInk(a.pal, label))
						} else {
							painted.WriteString(taskLinkInk(a.pal, label))
						}
						at = cell.end
					}
					painted.WriteString(ansi.Cut(line, at, ansi.StringWidth(line)))
					article = append(article, painted.String())
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
			if !p.links {
				article = append(article, a.pal.muted("Linked pages")+a.pal.dim(a.chords.say(" · ctrl+l")))
			}
			for i, l := range links {
				title := l.ID
				if target, ok := p.indexedPage(l.ID); ok {
					title = target.Title
				}
				text := fmt.Sprintf("%d. %s", i+1, title)
				if l.Kind == "backlink" {
					text += " · backlink"
				}
				if p.links && i == p.linkCursor {
					selectedConnection = len(article)
					text = a.pal.accent("› " + text)
				}
				articleHits[len(article)] = []knowledgeCellLink{{0, max(1, rightWidth), l.ID}}
				if !p.links {
					if p.hoverLink == l.ID {
						text = taskLinkHotInk(a.pal, text)
					} else {
						text = taskLinkInk(a.pal, text)
					}
				}
				article = append(article, fit(text, max(1, rightWidth)))
			}
		}
		if len(n.Evidence) > 0 && !p.links {
			sourceHeading := "Sources"
			if n.HumanEdited {
				sourceHeading = "Original sources"
			}
			if p.sources {
				back := taskLinkInk(a.pal, "Return to article")
				if p.hoverLink == "@sources" {
					back = taskLinkHotInk(a.pal, "Return to article")
				}
				article = []string{a.pal.muted(n.Title), "", a.pal.muted(sourceHeading), back + a.pal.dim(a.chords.say(" · esc")), ""}
				articleHits = map[int][]knowledgeCellLink{3: {{0, 17, "@sources"}}}
				if n.HumanEdited {
					article = append(article, a.pal.dim("Background only · does not verify your edits."), "")
				}
				article = append(article, a.knowledgeSourceRows(n, rightWidth)...)

			} else {
				label := fmt.Sprintf("%s · %d", sourceHeading, len(knowledgeSources(n.Evidence)))
				article = append(article, "")
				articleHits[len(article)] = []knowledgeCellLink{{0, ansi.StringWidth(label), "@sources"}}
				painted := taskLinkInk(a.pal, label)
				if p.hoverLink == "@sources" {
					painted = taskLinkHotInk(a.pal, label)
				}
				article = append(article, painted+a.pal.dim(a.chords.say(" · ctrl+o open")))
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
				rows[len(rows)-1].hit = knowledgeHit{tree: -1, article: true, links: knowledgeShift(articleHits[p.scroll+i], 1)}
			}
		} else {
			p.top = placeTop(p.top, p.cursor, len(p.targets), bodyRoom)
			for i := 0; i < bodyRoom && p.top+i < len(p.targets); i++ {
				line := p.treeLine(p.top + i)
				if p.top+i == p.cursor {
					line = knowledgeTreeSelection(line, left, a.pal)
				}
				add(line)
				rows[len(rows)-1].hit = knowledgeHit{tree: p.top + i}
			}
		}
	} else {
		treeRoom := max(1, bodyRoom-len(sidebar))
		p.scroll = min(p.scroll, max(0, len(article)-bodyRoom))
		p.top = placeTop(p.top, p.cursor, len(p.targets), treeRoom)
		for i := 0; i < bodyRoom; i++ {
			l, r := "", ""
			hit := knowledgeHit{tree: -1}
			if i < len(sidebar) {
				l = strings.TrimPrefix(sidebar[i].text, " ")
				if h, ok := sidebar[i].hit.(knowledgeHit); ok {
					hit.links = append(hit.links, h.links...)
					for j := range hit.links {
						hit.links[j].end = min(hit.links[j].end, left+1)
					}
				}
			} else if j := p.top + i - len(sidebar); j < len(p.targets) {
				l = p.treeLine(j)
				if j == p.cursor {
					l = knowledgeTreeSelection(l, left, a.pal)
				}
				hit.tree = j
			} else {
				hit.tree = p.top + i - len(sidebar)
			}
			if j := p.scroll + i; j < len(article) {
				r = article[j]
				hit.article = true
			}
			hit.links = append(hit.links, knowledgeShift(articleHits[p.scroll+i], left+4)...)
			add(taskPanePad(fit(l, left), left) + a.pal.dim(" │ ") + r)
			rows[len(rows)-1].hit = hit
		}
	}

	if len(rows) > room {
		rows = rows[:room]
	}
	return rows
}

// Stage percentages count completed items, not elapsed time or overall build work.
func knowledgeProgress(label string, done, total int, pal palette) string {
	done = max(0, min(done, total))
	return pal.dim(label+": ") + pal.data(fmt.Sprint(done)) + pal.dim(" of ") +
		pal.data(fmt.Sprint(total)) + pal.dim(" · ") + pal.data(fmt.Sprintf("%d%%", done*100/total))
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
		if cmd, ok := a.savedKnowledgeSwitchAt(x, y); ok {
			return cmd, true
		}
		return placeMemory{}.press(a, y)
	}
	p := &a.kb
	_, hits, _, _ := a.placeDraw(placeMemory{}, a.width, a.height)
	if y < 0 || y >= len(hits) {
		return nil, true
	}
	if hit, ok := hits[y].(knowledgeHit); ok {
		if hit.create && !p.reading.Enabled && x >= 1 && x < 1+ansi.StringWidth(knowledgeBuildLabel) {
			return a.knowledgeRead(true), true
		}
		for _, link := range hit.links {
			if x >= link.start && x < link.end {
				if cmd, took := a.knowledgeEditAction(link.id); took {
					return cmd, true
				}
				if link.id == "@switch" {
					return a.knowledgeOwnsSwitch(), true
				} else if link.id == "@search" {
					a.bar.on = false
				} else if link.id == "@collapse" {
					p.foldAll(true)
				} else if link.id == "@expand" {
					p.foldAll(false)
				} else if link.id == "@sources" {
					p.toggleSources()
				} else {
					p.navigate(link.id)
				}
				a.touch()
				return nil, true
			}
		}
		left := max(24, min(42, a.width/3))
		if hit.editor && p.edit != nil {
			a.knowledgeEditorCaret(hit, x)
			return nil, true
		}
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

// Pointer affordances use the same cell map as clicks, including wrapped links.
func (placeMemory) hoverAt(a *app, x, y int) bool {
	if !a.kb.open {
		return a.savedKnowledgeHoverAt(x, y)
	}
	hits := a.kb.hitRows
	moved := false
	next := ""
	if y >= 0 && y < len(hits) {
		if hit, ok := hits[y].(knowledgeHit); ok {
			if hit.create && x >= 1 && x < 1+ansi.StringWidth(knowledgeBuildLabel) {
				next = "@build"
			}
			left := max(24, min(42, a.width/3))
			if (a.width < 85 || x <= left) && hit.tree >= 0 && hit.tree < len(a.kb.targets) {
				moved = a.selectPlaceRow(&a.kb.cursor, hit.tree)
			}
			for _, link := range hit.links {
				if x >= link.start && x < link.end {
					next = link.id
					if a.kb.links {
						for i, target := range a.kb.pageLinks() {
							if target.ID == link.id {
								moved = a.selectPlaceRow(&a.kb.linkCursor, i) || moved
								break
							}
						}
					}
					break
				}
			}
		}
	}
	if next != a.kb.hoverLink {
		a.kb.hoverLink = next
		moved = true
	}
	if moved {
		a.touch()
	}
	return true
}

func (p *knowledgePlace) syncCursor() {
	for i, target := range p.targets {
		if target.id == p.selected {
			p.cursor = i
			return
		}
	}
}
func knowledgeTreeSelection(line string, width int, pal palette) string {
	if pal.profile < tokens.ANSI256 || pal.linear {
		return fit("> "+line, width)
	}
	return placeBand(fit(line, width), width, pal)
}
func (placeMemory) rememberHits(a *app, hits []placeHit) {
	if a.kb.open {
		a.kb.hitRows = hits
	}
}

func (a *app) knowledgeOwnsSwitch() tea.Cmd {
	cmd, _ := a.knowledgeOwns(tea.KeyPressMsg{Code: 'u', Mod: tea.ModAlt})
	return cmd
}

// Search runs outside the update/render loop and queries SQLite's FTS index.
// A result for an older filter never replaces the current result set.
type knowledgeSearchMsg struct {
	vault *knowledge.Vault
	query string
	hits  []knowledge.Result
	err   error
}

func (a *app) knowledgeSearch() tea.Cmd {
	p := &a.kb
	query := strings.TrimSpace(p.filter.String())
	if query == "" {
		p.matches = nil
		p.rank()
		return nil
	}
	v := p.vault
	if v == nil {
		return nil
	}
	return func() tea.Msg {
		hits, err := v.Search(query, 100)
		return knowledgeSearchMsg{vault: v, query: query, hits: hits, err: err}
	}
}
func (a *app) adoptKnowledgeSearch(msg knowledgeSearchMsg) {
	p := &a.kb
	if p.vault != msg.vault || strings.TrimSpace(p.filter.String()) != msg.query {
		return
	}
	if msg.err != nil {
		p.footer = "Knowledge search could not complete: " + msg.err.Error()
		a.touch()
		return
	}
	p.matches = make(map[string]bool, len(msg.hits))
	for _, hit := range msg.hits {
		p.matches[hit.Page.ID] = true
	}
	p.rank()
	a.touch()
}
