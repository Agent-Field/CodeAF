package main

import (
	"slices"
	"strconv"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/schema"
	"github.com/Agent-Field/codeaf/internal/appwire"
)

const appViewMaxPage = 500

// DataViewQuery resolves the declaration before querying the same scoped data
// store as built-in record parts. The response already reflects field policy.
func (b *appBackend) DataViewQuery(p appwire.DataViewParams) (*appwire.DataQueryResult, error) {
	if p.App == "" || p.Screen == "" || p.View == "" || p.Limit < 0 || p.Limit > appViewMaxPage {
		return nil, appwire.Errorf(appwire.CodeBadParams, "Choose an app, screen, view and valid page size.")
	}
	var raw []byte
	if p.Draft != "" {
		jobs, err := b.AppBuilds()
		if err != nil {
			return nil, err
		}
		raw, err = jobs.ViewScreen(p.App, p.Draft, p.Screen)
		if err != nil {
			return nil, appwire.Errorf(appwire.CodeNotFound, "The draft screen is unavailable for this app.")
		}
	} else {
		_, def, err := b.appContentDefinition(p.App)
		if err != nil {
			return nil, err
		}
		raw, _ = def.Screen(p.Screen)
	}
	screen, err := schema.ParseScreen(raw)
	if err != nil {
		return nil, appwire.Errorf(appwire.CodeNotFound, "The screen is unavailable.")
	}
	view, ok := screen.Views[p.View]
	if !ok || !appAssistantViewSupported(view) || len(view.Aggregate) != 0 {
		return nil, appwire.Errorf(appwire.CodeBadParams, "Choose a supported view that returns individual records.")
	}
	ir, err := declaredViewIR(view, p.Args)
	if err != nil {
		return nil, err
	}
	// A row view carries the grouping keys so the surface can put individual
	// records in sections or lanes. Aggregate views use the metric door.
	ir.Group = nil
	if len(ir.Select) > 0 {
		for _, field := range view.Group {
			if !slices.Contains(ir.Select, field) {
				ir.Select = append(ir.Select, field)
			}
		}
	}
	if p.Query != "" {
		lens, err := queryIR(view.Type, p.Query, nil)
		if err != nil {
			return nil, err
		}
		ir.Where = append(ir.Where, lens.Where...)
		if lens.Sort != "" {
			ir.Sort = lens.Sort
		}
	}
	limit, after := p.Limit, p.Cursor
	remaining := -1
	if view.Window != nil && view.Window.Limit > 0 {
		remaining = view.Window.Limit
		if after != "" {
			count, cursor, ok := strings.Cut(after, ":")
			remaining, err = strconv.Atoi(count)
			if !ok || err != nil || remaining <= 0 || remaining >= view.Window.Limit || cursor == "" {
				return nil, appwire.Errorf(appwire.CodeBadParams, "Reload this view to continue.")
			}
			after = cursor
		}
		if remaining <= appViewMaxPage && (limit == 0 || limit > remaining) {
			limit = remaining
		}
	}
	s, err := b.dataStore(p.Draft)
	if err != nil {
		return nil, err
	}
	page, source, err := b.appQueryRows(s, p.DataScope, dataScope(p.DataScope), &ir, after, limit)
	if err != nil {
		return nil, err
	}
	if remaining >= 0 {
		page.Total = min(page.Total, view.Window.Limit)
		remaining -= len(page.Rows)
		if remaining <= 0 {
			page.Next = ""
		} else if page.Next != "" {
			page.Next = strconv.Itoa(remaining) + ":" + page.Next
		}
	}
	return &appwire.DataQueryResult{Rows: wireRows(page.Rows), Next: page.Next, Total: page.Total, Source: &source}, nil
}
