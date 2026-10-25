package tui3

import (
	"strings"

	"github.com/Agent-Field/codeaf/internal/knowledge"
)

// Rebuilt once when a snapshot is adopted. Reading, linking and pointer frames
// do not walk unrelated pages or edges in the vault.
type knowledgeIndex struct {
	pages map[string]int
	links map[string][]knowledge.Link
}

func (p *knowledgePlace) indexKnowledge() {
	index := knowledgeIndex{pages: make(map[string]int, len(p.reading.Pages)), links: make(map[string][]knowledge.Link, len(p.reading.Pages))}
	seen := make(map[string]map[string]bool, len(p.reading.Pages))
	for i, n := range p.reading.Pages {
		index.pages[n.ID] = i
		index.links[n.ID] = append([]knowledge.Link(nil), n.Links...)
		seen[n.ID] = make(map[string]bool, len(n.Links))
		for _, l := range n.Links {
			seen[n.ID][l.ID] = true
		}
	}
	for _, n := range p.reading.Pages {
		for _, l := range n.Links {
			targets, ok := seen[l.ID]
			if !ok || targets[n.ID] {
				continue
			}
			index.links[l.ID] = append(index.links[l.ID], knowledge.Link{ID: n.ID, Kind: "backlink"})
			targets[n.ID] = true
		}
	}
	p.index = index
}

func (p *knowledgePlace) indexedPage(id string) (knowledge.Page, bool) {
	i, ok := p.index.pages[id]
	if !ok {
		return knowledge.Page{}, false
	}
	return p.reading.Pages[i], true
}

func (p *knowledgePlace) treeLine(i int) string {
	n := p.targets[i]
	mark := "  "
	if n.folder != "" {
		mark = "▾ "
		if p.folded[n.folder] {
			mark = "▸ "
		}
	}
	return strings.Repeat("  ", n.depth) + mark + n.title
}
