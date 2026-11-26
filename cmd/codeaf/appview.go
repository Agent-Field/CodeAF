package main

import (
	"github.com/Agent-Field/codeaf/internal/apps/schema"
	"github.com/Agent-Field/codeaf/internal/appwire"
)

// DataViewQuery resolves the declaration before querying the same scoped data
// store as built-in record parts. The response already reflects field policy.
func (b *appBackend) DataViewQuery(p appwire.DataViewParams) (*appwire.DataQueryResult, error) {
	if p.App == "" || p.Screen == "" || p.View == "" || p.Limit < 0 || p.Limit > 500 {
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
	if !ok || !appAssistantViewSupported(view) || len(view.Group) != 0 || len(view.Aggregate) != 0 {
		return nil, appwire.Errorf(appwire.CodeBadParams, "Choose a supported view that returns individual records.")
	}
	ir, err := declaredViewIR(view, p.Args)
	if err != nil {
		return nil, err
	}
	limit := p.Limit
	if view.Window != nil && view.Window.Limit > 0 && (limit == 0 || limit > view.Window.Limit) {
		limit = view.Window.Limit
	}
	s, err := b.dataStore(p.Draft)
	if err != nil {
		return nil, err
	}
	page, source, err := b.appQueryRows(s, p.DataScope, dataScope(p.DataScope), &ir, p.Cursor, limit)
	if err != nil {
		return nil, err
	}
	if view.Window != nil && view.Window.Limit > 0 && p.Cursor == "" && len(page.Rows) == view.Window.Limit {
		page.Next = ""
	}
	return &appwire.DataQueryResult{Rows: wireRows(page.Rows), Next: page.Next, Total: page.Total, Source: &source}, nil
}
