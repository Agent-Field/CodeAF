package workspacestore

import (
	"bytes"
	"encoding/json"
	"fmt"
	"unicode/utf8"
)

// The shared document, as the renderer projects it (workspace-sync/shared.ts):
//
//	{"schema":1, "tabs":[Tab…], "groups":[Group…], "closed":[Tab…], "nextNumber":N}
//
// It carries only what two windows on one place must agree on. The fields one
// window owns — which tab it shows, its recency order, which pane of a split it
// focuses, its scroll — are REFUSED here by name, so a client that forgets to
// strip them learns it at once instead of making every other window jump.
//
// This is a shape and bounds check, the server half of the renderer's
// readWorkspace (tabs/model.ts). It does not judge a pane's view fields beyond
// their size: the renderer validates those on read, and they never name a file
// the engine opens.

// windowLocal are the top-level fields a window keeps for itself.
var windowLocal = map[string]bool{"activeId": true, "recentIds": true, "scroll": true, "focus": true}

type sharedDoc struct {
	Schema     int               `json:"schema"`
	Tabs       []json.RawMessage `json:"tabs"`
	Groups     []json.RawMessage `json:"groups"`
	Closed     []json.RawMessage `json:"closed"`
	NextNumber int64             `json:"nextNumber"`
}

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: "+format, append([]any{ErrInvalid}, args...)...)
}

// Validate checks one shared tab-set document.
func Validate(raw json.RawMessage) error {
	if len(raw) > MaxDocumentBytes {
		return ErrTooLarge
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil || top == nil {
		return invalid("not a JSON object")
	}
	for name := range top {
		if windowLocal[name] {
			return invalid("%q belongs to one window and is never shared", name)
		}
	}
	var doc sharedDoc
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&doc); err != nil {
		return invalid("%v", err)
	}
	if doc.Schema != SchemaVersion {
		return invalid("schema %d", doc.Schema)
	}
	if len(doc.Tabs) == 0 || len(doc.Tabs) > MaxTabs {
		return invalid("%d tabs (1 to %d)", len(doc.Tabs), MaxTabs)
	}
	if len(doc.Closed) > MaxClosed {
		return invalid("%d closed tabs (at most %d)", len(doc.Closed), MaxClosed)
	}
	if len(doc.Groups) > MaxGroups {
		return invalid("%d groups (at most %d)", len(doc.Groups), MaxGroups)
	}
	if doc.NextNumber < 1 || doc.NextNumber >= 1_000_000 {
		return invalid("nextNumber %d", doc.NextNumber)
	}
	groups := map[string]bool{}
	for _, g := range doc.Groups {
		var group struct {
			ID        *string `json:"id"`
			Title     *string `json:"title"`
			Collapsed *bool   `json:"collapsed"`
		}
		if err := json.Unmarshal(g, &group); err != nil || group.ID == nil || group.Title == nil || group.Collapsed == nil {
			return invalid("a group needs id, title and collapsed")
		}
		if err := checkID(*group.ID); err != nil {
			return err
		}
		if err := checkTitle(*group.Title); err != nil {
			return err
		}
		if groups[*group.ID] {
			return invalid("group %q twice", *group.ID)
		}
		groups[*group.ID] = true
	}
	open := map[string]bool{}
	for _, t := range doc.Tabs {
		if err := checkTab(t, open); err != nil {
			return err
		}
	}
	closed := map[string]bool{}
	for _, t := range doc.Closed {
		if err := checkTab(t, closed); err != nil {
			return err
		}
	}
	for id := range closed {
		if open[id] {
			return invalid("tab %q is both open and closed", id)
		}
	}
	return nil
}

type paneFields struct {
	ID    *string `json:"id"`
	Title *string `json:"title"`
	Draft *string `json:"draft"`
	Kind  *string `json:"kind"`
}

// checkTab validates one tab (and its split's panes) and records every id it
// holds in seen, which must not already contain any of them.
func checkTab(raw json.RawMessage, seen map[string]bool) error {
	var tab struct {
		paneFields
		Pinned  *bool   `json:"pinned"`
		GroupID *string `json:"groupId"`
		Split   *struct {
			Panes []json.RawMessage `json:"panes"`
			Focus *json.RawMessage  `json:"focus"`
		} `json:"split"`
	}
	if err := json.Unmarshal(raw, &tab); err != nil {
		return invalid("a tab is not an object")
	}
	if tab.Pinned == nil {
		return invalid("a tab needs pinned")
	}
	if err := checkPane(tab.paneFields, seen); err != nil {
		return err
	}
	if tab.GroupID != nil {
		if err := checkID(*tab.GroupID); err != nil {
			return err
		}
	}
	if tab.Split == nil {
		return nil
	}
	if tab.Split.Focus != nil {
		return invalid("a split's focused pane belongs to one window and is never shared")
	}
	if len(tab.Split.Panes) < 2 || len(tab.Split.Panes) > MaxPanes {
		return invalid("a split holds 2 to %d panes", MaxPanes)
	}
	for _, p := range tab.Split.Panes {
		var pane paneFields
		if err := json.Unmarshal(p, &pane); err != nil {
			return invalid("a pane is not an object")
		}
		if err := checkPane(pane, seen); err != nil {
			return err
		}
	}
	return nil
}

func checkPane(p paneFields, seen map[string]bool) error {
	if p.ID == nil || p.Title == nil || p.Draft == nil {
		return invalid("a tab needs id, title and draft")
	}
	if err := checkID(*p.ID); err != nil {
		return err
	}
	if err := checkTitle(*p.Title); err != nil {
		return err
	}
	if p.Kind != nil && (len(*p.Kind) == 0 || len(*p.Kind) > 32) {
		return invalid("tab kind %q", *p.Kind)
	}
	if seen[*p.ID] {
		return invalid("tab %q twice", *p.ID)
	}
	seen[*p.ID] = true
	return nil
}

func checkID(id string) error {
	if id == "" || len(id) > MaxIDBytes || !utf8.ValidString(id) {
		return invalid("id %q", id)
	}
	for _, r := range id {
		if r < 0x20 || r == 0x7f {
			return invalid("id with a control character")
		}
	}
	return nil
}

func checkTitle(title string) error {
	if len(title) > MaxTitleBytes || !utf8.ValidString(title) {
		return invalid("title over %d bytes", MaxTitleBytes)
	}
	return nil
}
