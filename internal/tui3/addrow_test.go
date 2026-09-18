package tui3

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/modelsource"
)

// THE ADD ROW'S ID IS NOT A CONNECTION'S ID. A connection named `add` mints
// `custom-add`, which the sentinel's old literal would have collided with —
// cursor and armed state are keyed by row id, and IsCustomID-based code read
// the add row as a connection. The sentinel now lives outside the custom
// namespace (customAddRowID), so no minted id can ever be it.
func TestTheConnectPanelAddRowKeepsItsOwnIdWhenAConnectionIsNamedAdd(t *testing.T) {
	dir := t.TempDir()
	if err := config.WriteSources(dir, []config.PersistedSource{
		{ID: modelsource.CustomID, Written: "homelab", Address: "http://127.0.0.1:9001/v1", Key: "sk-homelab", Order: 1},
		{ID: "custom-add", Written: "add", Address: "http://127.0.0.1:9002/v1", Key: "sk-add", Order: 2},
	}); err != nil {
		t.Fatal(err)
	}
	sources := modelsource.NewSet(
		testDefaultService("sk-default-1234567890"),
		modelsource.Connected{
			Source: modelsource.Source{ID: modelsource.CustomID, Written: "homelab", Name: "homelab", Address: "http://127.0.0.1:9001/v1"},
			Key:    "sk-homelab", Address: "http://127.0.0.1:9001/v1",
		},
		modelsource.Connected{
			Source: modelsource.Source{ID: "custom-add", Written: "add", Name: "add", Address: "http://127.0.0.1:9002/v1"},
			Key:    "sk-add", Address: "http://127.0.0.1:9002/v1",
		},
	)
	a := modelServiceTestApp(t, dir, "openai/gpt-4.1-mini", sources, nil)
	a.openConnect()

	addAt, connectionAt := -1, -1
	ids := make(map[string]bool, 8)
	for at := range a.connPanel.hits {
		row, ok := a.connPanel.at(at)
		if !ok {
			continue
		}
		if ids[row.ID] {
			t.Fatalf("two panel rows share the id %q", row.ID)
		}
		ids[row.ID] = true
		source, model := modelConnectionSource(row.ID)
		if !model {
			continue
		}
		switch source {
		case customAddRowID:
			addAt = at
		case "custom-add":
			if row.Connected {
				connectionAt = at
			}
		}
	}
	if addAt < 0 {
		t.Fatal("the panel drew no add row")
	}
	if connectionAt < 0 {
		t.Fatal("the connection named `add` has no connected row of its own")
	}
	if cmd := a.connectAct(addAt); cmd == nil {
		t.Fatal("enter on the add row started nothing")
	}
	if a.modelDraft == nil || a.modelDraft.editing {
		t.Fatalf("enter on the add row is not the mint flow: draft=%v editing=%v",
			a.modelDraft != nil, a.modelDraft != nil && a.modelDraft.editing)
	}
	// ENTER ON A CONNECTED ROW ASKS BEFORE IT ACTS: the panel arms that row's
	// own disconnect question and starts nothing (connectAct) — the edit door
	// for a connected service is the Providers tab. What the arm proves here is
	// the distinctness: the question is asked about the connection's own id,
	// not about the add row it once shared one with.
	a.modelDraft = nil
	a.connectAct(connectionAt)
	if a.modelDraft != nil {
		t.Fatal("enter on a connected row started a draft")
	}
	if a.connPanel.armed != modelConnectionID("custom-add") {
		t.Fatalf("enter on the `add` connection armed %q, want its own id", a.connPanel.armed)
	}
}

// THE PROVIDERS TAB CARRIES THE ADD DOOR ON AN EMPTY PROFILE. With only the
// default service connected the tab draws no services head, no connection row
// and no switcher — the emptiness law — but the add row stands: the door is an
// action, not decoration, and it is the one a profile with no custom
// connection needs most (startCustomAdd).
func TestProvidersTabShowsTheAddRowBeforeAnyCustomConnectionExists(t *testing.T) {
	dir := t.TempDir()
	a := modelServiceTestApp(t, dir, "openai/gpt-4.1-mini",
		modelsource.NewSet(testDefaultService("sk-default-1234567890")), nil)
	a.prepareModelServices()
	a.raiseSettings()
	for i, tab := range settingTabs {
		if tab == tabProviders {
			a.sheet.tab = i
		}
	}
	a.sheet.build()

	addAt := -1
	for at, item := range a.sheet.items {
		if item.head == "services" {
			t.Fatal("an empty profile drew the services head")
		}
		if item.service != nil && item.service.switcher {
			t.Fatal("an empty profile drew the switcher row")
		}
		if item.service != nil && !item.service.addCustom {
			t.Fatalf("an empty profile drew a connection row: %q", item.service.name)
		}
		if item.service != nil && item.service.addCustom {
			if addAt >= 0 {
				t.Fatal("the tab drew the add row twice")
			}
			addAt = at
		}
	}
	if addAt < 0 {
		t.Fatal("the Providers tab drew no add row on an empty profile")
	}
	for at := addAt - 1; at >= 0; at-- {
		if a.sheet.items[at].row.Key == config.KeyAPIKey {
			break
		}
		if a.sheet.items[at].row.Key != "" || a.sheet.items[at].head != "" || a.sheet.items[at].service != nil {
			t.Fatalf("the add row does not sit under the openrouter key (first thing above it is at %d)", at)
		}
	}
	a.sheet.cursor = addAt
	if cmd := a.activate(); cmd == nil {
		t.Fatal("enter on the add row started nothing")
	}
	if a.modelDraft == nil || a.modelDraft.editing || a.modelDraft.step != modelConnectAddress {
		t.Fatalf("the add row did not open the mint flow: draft=%v editing=%v step=%v",
			a.modelDraft != nil, a.modelDraft != nil && a.modelDraft.editing,
			a.modelDraft != nil && a.modelDraft.step == modelConnectAddress)
	}
}
