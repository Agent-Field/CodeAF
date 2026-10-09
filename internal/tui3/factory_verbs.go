package tui3

import (
	tea "charm.land/bubbletea/v2"
)

// ── AN ACTION IS ITS KEY ────────────────────────────────────────────────────
//
// The item page's actions (`open on github`, `refresh`, `dismiss`) stand as
// rows under the left column's facets (factory_item.go's
// [app.factoryItemActions]); the right-hand column of verbs they once stood
// in is gone (owner's layout, 2026-10-09), and the run's own verbs are the
// top bar's control (factory_bar.go). A PRESS ON AN ACTION IS ITS KEY: the key
// is sent down the place's own key path, so a press and a key cannot do two
// different things.

// factoryVerbRow is one action: its word and the key a press on it sends.
type factoryVerbRow struct {
	word, key string
}

// factoryVerbsOff says whether an action pressed now would type rather than
// act: a typing row has the keys.
func (a *app) factoryVerbsOff() bool { return a.fp.act.ask != nil }

// factoryVerbPress is a press on an action: ITS KEY, sent down the place's
// own key path (place_factory.go's owns, then key), so the press runs the
// very function the key runs and never a copy of it; while a typing row has
// the keys, nothing.
func (a *app) factoryVerbPress(v factoryVerbRow) tea.Cmd {
	if a.factoryVerbsOff() {
		return nil
	}
	msg := factoryKeyPress(v.key)
	if cmd, took := (placeFactory{}).owns(a, msg); took {
		return cmd
	}
	return (placeFactory{}).key(a, msg)
}

// factoryKeyPress is the key press a key's spelling names: `space`, or the
// one character it is.
func factoryKeyPress(k string) tea.KeyPressMsg {
	if k == keySelect {
		return tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
	}
	r := []rune(k)
	if len(r) != 1 {
		return tea.KeyPressMsg{}
	}
	return tea.KeyPressMsg{Code: r[0], Text: k}
}
