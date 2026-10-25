package tui3

import tea "charm.land/bubbletea/v2"

// The wheel belongs to the pane under the pointer, independently of which
// article keyboard navigation last opened. Cached frame hits keep this bounded
// to the visible terminal rows and avoid layout or disk work per wheel event.
func (placeMemory) wheelAt(a *app, x, y, delta int) (tea.Cmd, bool) {
	if !a.kb.open {
		return placeMemory{}.wheel(a, delta)
	}
	p := &a.kb
	if y < 0 || y >= len(p.hitRows) {
		return nil, true
	}
	hit, ok := p.hitRows[y].(knowledgeHit)
	if !ok {
		return nil, true
	}

	left := max(24, min(42, a.width/3))
	tree := a.width >= 85 && x <= left || a.width < 85 && !p.article && !p.links
	if tree {
		if hit.tree < 0 {
			return nil, true
		}
		room := 0
		for _, row := range p.hitRows {
			if h, ok := row.(knowledgeHit); ok && h.tree >= 0 {
				room++
			}
		}
		room = max(1, room)
		p.top = max(0, min(p.top+delta, max(0, len(p.targets)-room)))
		p.cursor = max(p.top, min(p.cursor, min(p.top+room-1, max(0, len(p.targets)-1))))
	} else if !hit.article {
		return nil, true
	} else if p.links {
		p.linkCursor = max(0, min(p.linkCursor+delta, len(p.pageLinks())-1))
	} else {
		p.scroll = max(0, p.scroll+delta)
	}
	p.hoverLink = ""
	a.touch()
	return nil, true
}
