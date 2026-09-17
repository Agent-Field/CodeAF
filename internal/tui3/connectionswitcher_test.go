package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/connect"
	"github.com/Agent-Field/codeaf/internal/modelsource"
)

// The switcher's shared fixtures: a default service on the usual base URL and
// the custom connections the profile holds, in persisted order. The default
// service's PreferredModel answers the Models door (a.models), so a test whose
// walk reaches the default side passes []Model{{ID: config.DefaultModel}} and
// the move lands on the bare default model rather than on whatever
// BuiltinModels happens to list first.
func connectionDefaultService() modelsource.Connected {
	return testDefaultService("sk-default-1234567890")
}

func connectionCustomService(id, written string) modelsource.Connected {
	return modelsource.Connected{
		Source: modelsource.Source{
			ID: id, Written: written, Name: written,
			Address: "http://127.0.0.1:9001/v1", KeyOptional: true,
			// A custom connection that lists models is the only kind the
			// switcher can move onto: modelsForConnectedService refuses to
			// look at one whose Listing says <base>/models does not exist, and
			// the real row config.PrepareCustomSource writes carries
			// ListingModels.
			Listing: modelsource.ListingModels,
		},
		Key: "sk-custom-1234567890", Address: "http://127.0.0.1:9001/v1",
	}
}

func connectionWriteSources(t *testing.T, dir string, written ...string) []modelsource.Connected {
	t.Helper()
	customs := make([]modelsource.Connected, 0, len(written))
	rows := make([]config.PersistedSource, 0, len(written))
	for at, name := range written {
		id := modelsource.CustomID
		if at > 0 {
			id = modelsource.CustomID + "-" + strings.ToLower(name)
		}
		rows = append(rows, config.PersistedSource{
			ID: id, Written: name, Address: "http://127.0.0.1:900" + itoa(at+1) + "/v1",
			Key: "sk-custom-1234567890", Order: at + 1,
		})
		customs = append(customs, connectionCustomService(id, name))
	}
	if err := config.WriteSources(dir, rows); err != nil {
		t.Fatal(err)
	}
	return customs
}

// connectionFindRow returns the /connect panel's row for a model-connection id,
// with its position; ok is false when the panel drew no such row, which is
// itself the answer the emptiness tests pin.
func connectionFindRow(t *testing.T, a *app, id string) (connect.Status, int, bool) {
	t.Helper()
	for at := range a.connPanel.hits {
		row, ok := a.connPanel.at(at)
		if ok && row.ID == modelConnectionID(id) {
			return row, at, true
		}
	}
	return connect.Status{}, -1, false
}

// connectionTabRow walks into the settings place the way a person does and
// lands on the Providers tab, returning the switcher row's item, nil when the
// tab drew no switcher. The page must be raised through its real door
// ([app.showPage]) rather than by calling raiseSettings alone: the page id is
// what modelServiceFollowup reads to choose between the sheet's foot and the
// conversation's notes, so a panel raised without it writes its receipts into
// the wrong room.
func connectionTabRow(t *testing.T, a *app) *sheetItem {
	t.Helper()
	a.prepareModelServices()
	a.showPage(pageSettings)
	for i, tab := range settingTabs {
		if tab == tabProviders {
			a.sheet.tab = i
		}
	}
	a.sheet.build()
	for at, item := range a.sheet.items {
		if item.service != nil && item.service.switcher {
			return &a.sheet.items[at]
		}
	}
	return nil
}

// THE SWITCHER ROWS READ THE CONVERSATION'S LIVE MODEL, NOT THE SHARED PROFILE
// (config.ActiveConnectionFor): with one custom connection connected and this
// conversation on a default-service model, the Providers row says it is
// answering on the default service — whatever the profile slot holds — and
// enter moves the conversation onto the custom connection's qualified preferred
// model through the one model-change road (switchActiveConnection).
func TestTheSwitcherRowNamesTheDefaultServiceAndEnterMovesOntoTheCustomConnection(t *testing.T) {
	dir := t.TempDir()
	customs := connectionWriteSources(t, dir, "homelab")
	sources := modelsource.NewSet(append([]modelsource.Connected{connectionDefaultService()}, customs...)...)
	a := modelServiceTestApp(t, dir, "openai/gpt-4.1-mini", sources, []Model{{ID: config.DefaultModel}})
	a.modelsForService = func(service modelsource.Connected) []Model {
		return []Model{{ID: "qwen-local"}}
	}

	item := connectionTabRow(t, a)
	if item == nil {
		t.Fatal("the Providers tab drew no switcher row with a custom connection connected")
	}
	if !strings.Contains(item.service.value, "answering on openrouter") {
		t.Fatalf("the switcher row did not name the default service: %q", item.service.value)
	}
	if !strings.Contains(item.service.value, "enter moves it to homelab") {
		t.Fatalf("the switcher row did not offer the custom connection: %q", item.service.value)
	}

	a.sheet.cursor = indexOfSheetItem(a, item)
	if cmd := a.activate(); cmd != nil {
		t.Fatalf("enter on the switcher row started a command: %v", cmd)
	}
	if a.model != "homelab/qwen-local" {
		t.Fatalf("enter moved the conversation to %q, want homelab/qwen-local", a.model)
	}
	// The receipt lands on the sheet's own foot while the panel is up
	// (modelServiceFollowup), which is where a person who just pressed enter
	// is standing.
	if got := a.sheet.msg; !strings.Contains(got, "homelab/qwen-local") {
		t.Fatalf("the switch receipt did not name the move: %q", got)
	}
}

// ENTER IS A WRAP, NOT A STAIR: the same row takes the conversation back off
// the custom connection and onto the default service's preferred bare model,
// which is the whole shape of a two-entry ring (nextConnection).
func TestEnterOnTheSwitcherRowMovesACustomConversationBackToTheDefaultService(t *testing.T) {
	dir := t.TempDir()
	customs := connectionWriteSources(t, dir, "homelab")
	sources := modelsource.NewSet(append([]modelsource.Connected{connectionDefaultService()}, customs...)...)
	a := modelServiceTestApp(t, dir, "homelab/qwen-local", sources, []Model{{ID: config.DefaultModel}})
	a.modelsForService = func(service modelsource.Connected) []Model {
		return []Model{{ID: "qwen-local"}}
	}

	item := connectionTabRow(t, a)
	if item == nil {
		t.Fatal("the Providers tab drew no switcher row with a custom connection connected")
	}
	if !strings.Contains(item.service.value, "answering on homelab") {
		t.Fatalf("the switcher row did not name the custom connection: %q", item.service.value)
	}
	a.sheet.cursor = indexOfSheetItem(a, item)
	if cmd := a.activate(); cmd != nil {
		t.Fatalf("enter on the switcher row started a command: %v", cmd)
	}
	if a.model != config.DefaultModel {
		t.Fatalf("enter moved the conversation to %q, want the default service's %q", a.model, config.DefaultModel)
	}
}

// THREE ENTERS WALK THE RING AND COME HOME: with two custom connections the
// ring is default, homelab, lab, and the switcher wraps from lab back onto the
// default service rather than stopping at the end of the list
// (switchActiveConnection, nextConnection).
func TestEnterOnTheSwitcherRowWalksTwoCustomConnectionsAndWrapsToTheDefaultService(t *testing.T) {
	dir := t.TempDir()
	customs := connectionWriteSources(t, dir, "homelab", "lab")
	sources := modelsource.NewSet(append([]modelsource.Connected{connectionDefaultService()}, customs...)...)
	a := modelServiceTestApp(t, dir, "openai/gpt-4.1-mini", sources, []Model{{ID: config.DefaultModel}})
	a.modelsForService = func(service modelsource.Connected) []Model {
		return []Model{{ID: "qwen-local"}}
	}
	if item := connectionTabRow(t, a); item == nil {
		t.Fatal("the Providers tab drew no switcher row with two custom connections connected")
	}
	// The door the row offers is the service's own Written word — homelab, lab,
	// and the default service's openrouter — and the model the enter lands on
	// is the connection's qualified preferred id, bare on the default service.
	for _, step := range []struct{ door, model string }{
		{"homelab", "homelab/qwen-local"},
		{"lab", "lab/qwen-local"},
		{"openrouter", config.DefaultModel},
	} {
		item := connectionTabRow(t, a)
		if item == nil {
			t.Fatalf("the Providers tab drew no switcher row before the move to %q", step.model)
		}
		if !strings.Contains(item.service.value, "enter moves it to "+step.door) {
			t.Fatalf("the switcher row did not offer %q: %q", step.door, item.service.value)
		}
		a.sheet.cursor = indexOfSheetItem(a, item)
		if cmd := a.activate(); cmd != nil {
			t.Fatalf("enter on the switcher row started a command: %v", cmd)
		}
		if a.model != step.model {
			t.Fatalf("the ring walked the conversation to %q, want %q", a.model, step.model)
		}
	}
}

// THE RING NEEDS TWO AND THE ADD ROW NEEDS ONE: with only the default service
// there is no switcher row on the tab, no switch row and no add row on the
// /connect panel — the add row stands on the panel once at least one custom
// connection is connected, not once the ring is non-empty (modelConnectionRows).
func TestADefaultOnlyProfileDrawsNoSwitcherAndNoAddRowOnTheConnectPanel(t *testing.T) {
	dir := t.TempDir()
	a := modelServiceTestApp(t, dir, "openai/gpt-4.1-mini",
		modelsource.NewSet(connectionDefaultService()), nil)
	if item := connectionTabRow(t, a); item != nil {
		t.Fatalf("a default-only profile drew a switcher row: %q", item.service.value)
	}
	a.openConnect()
	for _, id := range []string{connectionSwitchRowID, customAddRowID} {
		if _, _, ok := connectionFindRow(t, a, id); ok {
			t.Fatalf("the /connect panel drew the %q row on a default-only profile", id)
		}
	}
}

// THE /CONNECT PANEL'S SWITCH ROW IS THE TAB ROW'S OTHER DOOR: enter on the
// row whose id is modelConnectionID(connectionSwitchRowID) moves this
// conversation's model exactly as the tab's row does (connectAct).
func TestTheConnectPanelSwitchRowMovesTheConversationLikeTheTabRow(t *testing.T) {
	dir := t.TempDir()
	customs := connectionWriteSources(t, dir, "homelab")
	sources := modelsource.NewSet(append([]modelsource.Connected{connectionDefaultService()}, customs...)...)
	a := modelServiceTestApp(t, dir, "openai/gpt-4.1-mini", sources, []Model{{ID: config.DefaultModel}})
	a.modelsForService = func(service modelsource.Connected) []Model {
		return []Model{{ID: "qwen-local"}}
	}
	a.openConnect()
	row, at, ok := connectionFindRow(t, a, connectionSwitchRowID)
	if !ok {
		t.Fatal("the /connect panel drew no active-connection row with a custom connection connected")
	}
	if !strings.Contains(row.Blurb, "answering on openrouter") {
		t.Fatalf("the panel's switch row did not name the default service: %q", row.Blurb)
	}
	if cmd := a.connectAct(at); cmd != nil {
		t.Fatalf("enter on the switch row started a command: %v", cmd)
	}
	if a.model != "homelab/qwen-local" {
		t.Fatalf("enter on the panel's switch row moved the conversation to %q, want homelab/qwen-local", a.model)
	}
}

// THE LIVE MODEL WINS OVER THE PROFILE: the engine host writes the shared
// profile slot asynchronously, so another conversation's pick — or this one's,
// unsaved — must not move the row. The profile says the custom connection; this
// conversation's model says the default service; the row answers the
// conversation, and enter moves it onto the custom connection
// (config.ActiveConnectionFor).
func TestTheSwitcherRowAnswersTheConversationsModelEvenWhenTheProfileSlotDisagrees(t *testing.T) {
	dir := t.TempDir()
	customs := connectionWriteSources(t, dir, "homelab")
	if err := config.WriteChatModel(dir, "homelab/qwen-local"); err != nil {
		t.Fatal(err)
	}
	sources := modelsource.NewSet(append([]modelsource.Connected{connectionDefaultService()}, customs...)...)
	a := modelServiceTestApp(t, dir, "openai/gpt-4.1-mini", sources, []Model{{ID: config.DefaultModel}})
	a.modelsForService = func(service modelsource.Connected) []Model {
		return []Model{{ID: "qwen-local"}}
	}
	if got := config.ChatModelAt(dir); got != "homelab/qwen-local" {
		t.Fatalf("the profile slot was not written: %q", got)
	}

	item := connectionTabRow(t, a)
	if item == nil {
		t.Fatal("the Providers tab drew no switcher row with a custom connection connected")
	}
	if !strings.Contains(item.service.value, "answering on openrouter") {
		t.Fatalf("the row read the shared profile instead of this conversation's model: %q", item.service.value)
	}
	a.sheet.cursor = indexOfSheetItem(a, item)
	if cmd := a.activate(); cmd != nil {
		t.Fatalf("enter on the switcher row started a command: %v", cmd)
	}
	if a.model != "homelab/qwen-local" {
		t.Fatalf("enter moved the conversation to %q, want homelab/qwen-local", a.model)
	}
}

// A WORKING TURN FREEZES THE MODEL AND THE ROW NAMES WHERE IT IS GOING: while
// a turn is answering, enter on the switcher row records the move in
// [app.deferredModelServiceModel] — the row's own reading of the deferred
// target — and leaves this conversation's model untouched until the turn
// settles (switchActiveConnection).
func TestEnterOnTheSwitcherRowWhileWorkingDefersTheMoveAndKeepsNamingIt(t *testing.T) {
	dir := t.TempDir()
	customs := connectionWriteSources(t, dir, "homelab")
	sources := modelsource.NewSet(append([]modelsource.Connected{connectionDefaultService()}, customs...)...)
	a := modelServiceTestApp(t, dir, "openai/gpt-4.1-mini", sources, []Model{{ID: config.DefaultModel}})
	a.modelsForService = func(service modelsource.Connected) []Model {
		return []Model{{ID: "qwen-local"}}
	}
	a.state = stateWorking

	item := connectionTabRow(t, a)
	if item == nil {
		t.Fatal("the Providers tab drew no switcher row with a custom connection connected")
	}
	a.sheet.cursor = indexOfSheetItem(a, item)
	if cmd := a.activate(); cmd != nil {
		t.Fatalf("enter on the switcher row started a command: %v", cmd)
	}
	if a.deferredModelServiceModel != "homelab/qwen-local" {
		t.Fatalf("enter deferred %q, want homelab/qwen-local", a.deferredModelServiceModel)
	}
	if a.model != "openai/gpt-4.1-mini" {
		t.Fatalf("a working turn's model was changed to %q", a.model)
	}
	// AND THE ROW NAMES THE DEFERRED TARGET: the conversation is going there,
	// and a row that still read the frozen model would offer the move it has
	// already made ([app.conversationModel]).
	next := connectionTabRow(t, a)
	if next == nil {
		t.Fatal("the switcher row went missing while a move was deferred")
	}
	if !strings.Contains(next.service.value, "answering on homelab") {
		t.Fatalf("the row did not read the deferred target: %q", next.service.value)
	}
}

// indexOfSheetItem finds an item's position in the sheet's list, so a test can
// put the cursor on a row it holds.
func indexOfSheetItem(a *app, item *sheetItem) int {
	for at := range a.sheet.items {
		if &a.sheet.items[at] == item {
			return at
		}
	}
	return -1
}
