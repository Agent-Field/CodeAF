package main

import (
	"sort"

	"github.com/Agent-Field/codeaf/internal/apps/spec"
)

// coverage records which screens parts reach one record type.
type coverage struct{ list, page, form, create, update, board bool }

func (c *checker) noteCoverage(n *spec.Node, typ string) {
	cov := c.cover[typ]
	if cov == nil {
		return
	}
	switch n.Part {
	case "RecordList", "List", "Table", "Board":
		cov.list = true
		cov.board = cov.board || n.Part == "Board" || hasProp(n, "groupBy")
	case "RecordPage":
		cov.page = true
	case "Form":
		cov.form = true
	}
}

func hasProp(n *spec.Node, name string) bool { _, ok := n.Props[name]; return ok }

// checkCoverage turns the quality bar into notes: every main record type has
// a list, a detail view, a way to create and a way to edit; a status-like enum
// gets a board.
func (c *checker) checkCoverage() {
	types := make([]string, 0, len(c.cover))
	for t := range c.cover {
		types = append(types, t)
	}
	sort.Strings(types)
	for _, t := range types {
		cov := c.cover[t]
		c.noteMissing(t, "list", cov.list)
		c.noteMissing(t, "detail", cov.page || cov.form)
		c.noteMissing(t, "create", cov.create)
		c.noteMissing(t, "edit", cov.form || cov.update)
	}
	c.checkNavCovers()
}

func (c *checker) noteMissing(t, what string, ok bool) {
	if !ok {
		c.note("%s: no %s", t, what)
	}
}

// checkNavCovers notes a top-level screen (one without params) that the nav
// does not list by title.
func (c *checker) checkNavCovers() {
	listed := map[string]bool{}
	for _, s := range c.b.Screens {
		if s.Nav == nil {
			continue
		}
		items, _ := s.Nav.Props["items"].([]any)
		for _, item := range items {
			title, _ := item.(string)
			listed[title] = true
		}
	}
	for _, id := range c.b.ScreenIDs() {
		if s := c.b.Screens[id]; len(s.Params) == 0 && !listed[s.Title] {
			c.note("%s: top-level screen is not in the nav", id)
		}
	}
}
