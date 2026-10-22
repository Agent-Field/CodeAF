package tui3

import (
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/modelsource"
	"github.com/Agent-Field/codeaf/internal/modelsource/sourcestub"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

func rotationApp(t *testing.T) (*app, string) {
	t.Helper()
	a, dir := sheetApp(t)
	a.modelRotation = []string{a.model, "vendor/second", "vendor/third"}
	a.models = func() []Model {
		return []Model{{ID: a.modelRotation[0], ContextLength: 128000}, {ID: "vendor/second", ContextLength: 256000}, {ID: "vendor/third", ContextLength: 64000}}
	}
	a.saveModel = func(id string) error { return config.WriteChatModel(dir, id) }
	return a, dir
}

func TestModelRotationClickChangesAgentWindowAndKeepsDraft(t *testing.T) {
	a, dir := rotationApp(t)
	a.input.setText("keep this draft")
	a.input.cursor = 5
	for _, want := range []string{"vendor/second", "vendor/third", "openai/gpt-4.1-mini"} {
		line := seamLine(t, a)
		span := a.seamModelCycleSpan
		if !span.pressable() || !strings.Contains(line, a.icon(tokens.GActionTransfer)) {
			t.Fatalf("missing switch: %s", line)
		}
		if a.seamModelSpan.to > span.from || a.seamEffortSpan.to > span.from && a.seamEffortSpan.pressable() {
			t.Fatal("doors overlap")
		}
		drive(t, a, tea.MouseClickMsg{X: span.to - 1, Y: seamRowY(a), Button: tea.MouseLeft})
		if a.model != want || a.agent.Model() != want || config.ChatModelAt(dir) != want {
			t.Fatalf("surface/agent/saved = %s/%s/%s", a.model, a.agent.Model(), config.ChatModelAt(dir))
		}
		if a.input.String() != "keep this draft" || a.input.cursor != 5 || a.pick.open {
			t.Fatal("switch disturbed draft or opened picker")
		}
		if got := seamLine(t, a); !strings.Contains(got, want) {
			t.Fatalf("stale TUI: %s", got)
		}
	}
	a.switchModel("vendor/outside", 0)
	drive(t, a, tea.KeyPressMsg{Code: tea.KeyF3})
	if a.model != a.modelRotation[0] {
		t.Fatal("outside model did not return to first entry")
	}
}

func TestModelRotationSettingsChoosesOrderedModelsWithoutSwitchingChat(t *testing.T) {
	a, dir := rotationApp(t)
	a.openModelRotationSettings()
	cursorTo(t, a, config.KeyChatModelRotation)
	drive(t, a, key("enter"))
	if a.sheet.sel == nil {
		t.Fatal("rotation did not open catalog")
	}
	original := a.model
	for _, id := range []string{"vendor/third", "vendor/second", "vendor/third", "vendor/third"} {
		a.sheet.sel.pick.filter.setText(id)
		a.sheet.sel.pick.rank()
		drive(t, a, key("enter"))
		if a.sheet.sel == nil {
			t.Fatal("choice closed before rotation was ready")
		}
	}
	want := []string{"vendor/second", "vendor/third"}
	if got := config.ChatModelRotationAt(dir); !reflect.DeepEqual(got, want) {
		t.Fatalf("saved = %v", got)
	}
	if !reflect.DeepEqual(a.modelRotation, want) || a.model != original {
		t.Fatal("preferences not live or chat changed during setup")
	}
	drive(t, a, key("esc"))
	a.closeSettings()
	drive(t, a, tea.KeyPressMsg{Code: tea.KeyF3})
	if a.model != "vendor/second" {
		t.Fatal("saved rotation not used")
	}
}

func TestModelRotationSetupFallbackAndGlyphWidths(t *testing.T) {
	for _, tier := range []tokens.GlyphSet{tokens.Plain, tokens.NerdFont, tokens.ASCII} {
		for _, width := range []int{44, 80, 120, 200} {
			a, _ := rotationApp(t)
			a.width = width
			a.pal.icons = tier
			a.pal.ascii = tier == tokens.ASCII
			line := seamLine(t, a)
			if len([]rune(line)) > width+5 {
				t.Fatalf("switch overflow at %d: %s", width, line)
			}
			if a.seamModelCycleSpan.pressable() && !strings.Contains(line, "1/3") {
				t.Fatalf("rotation position missing: %s", line)
			}
			a.modelRotation = nil
			a.touch()
			a.width = width
			a.pal.icons = tier
			a.pal.ascii = tier == tokens.ASCII
			_ = seamLine(t, a)
			span := a.seamModelCycleSpan
			if span.pressable() {
				t.Fatal("unconfigured rotation drew a switch")
			}
			drive(t, a, tea.KeyPressMsg{Code: tea.KeyF3})
			if !a.at(pageSettings) {
				t.Fatal("F3 did not open setup")
			}
			a.page = pageSettings
			if a.modelCycleSuffix() != "" {
				t.Fatal("switch leaked into settings")
			}
		}
	}
}

func TestModelRotationClickReachesTheRealSessionHTTPBackend(t *testing.T) {
	server := sourcestub.New("vendor/first", "vendor/second")
	defer server.Close()
	dir := t.TempDir()
	source := testDefaultService("test-key-not-a-secret")
	source.Source.Address, source.Address = server.URL(), server.URL()
	sources := modelsource.NewSet(source)
	agent, err := session.New(session.Config{Workspace: t.TempDir(), Model: "vendor/first", Sources: sources})
	if err != nil {
		t.Fatal(err)
	}
	defer agent.Close()
	a := modelServiceTestAppWithAgent(t, dir, agent.Model(), sources, []Model{{ID: "vendor/first", ContextLength: 128000}, {ID: "vendor/second", ContextLength: 256000}}, agent)
	a.welcome = welcome{spent: true}
	a.modelRotation = []string{"vendor/first", "vendor/second"}
	drainModelServiceTurn(t, agent, "first")
	_ = seamLine(t, a)
	span := a.seamModelCycleSpan
	drive(t, a, tea.MouseClickMsg{X: span.to - 1, Y: seamRowY(a), Button: tea.MouseLeft})
	drainModelServiceTurn(t, agent, "second")
	calls := completionRequests(server)
	if len(calls) != 2 || completionModel(t, calls[0]) != "vendor/first" || completionModel(t, calls[1]) != "vendor/second" {
		t.Fatalf("HTTP backend calls: %+v", calls)
	}
	if a.ctxWindow != 256000 {
		t.Fatalf("context window=%d", a.ctxWindow)
	}
	if got := seamLine(t, a); !strings.Contains(got, "vendor/second") {
		t.Fatalf("TUI stale after backend switch: %s", got)
	}
}

func TestModelRotationNeverAppearsInATaskRoomOrChangesItsModel(t *testing.T) {
	a, _ := roomModelApp(t, "vendor/task")
	original := a.model
	a.modelRotation = []string{original, "vendor/other"}
	if a.modelCycleSuffix() != "" {
		t.Fatal("chat rotation appeared in task room")
	}
	drive(t, a, tea.KeyPressMsg{Code: tea.KeyF3})
	if a.model != original || a.tasks[9].model != "vendor/task" {
		t.Fatal("chat rotation changed a task room")
	}
}

func TestModelRotationSetupRowsKeepTheirPointerOwners(t *testing.T) {
	a, _ := rotationApp(t)
	a.openModelRotationSettings()
	drive(t, a, key("enter"))
	for _, saved := range []string{"", "vendor/second", "vendor/second, vendor/third"} {
		row, _ := a.sheet.registry.Row(config.KeyChatModelRotation)
		if err := row.Apply(saved); err != nil {
			t.Fatal(err)
		}
		lines, owners := a.sheet.selectLines(90, 15, a.pal, a.reasoningFor)
		if len(lines) != len(owners) || len(lines) != 15 {
			t.Fatalf("lines=%d owners=%d", len(lines), len(owners))
		}
		for i, line := range lines {
			if strings.Contains(plain(line), "Choose at least two") && owners[i] != -1 {
				t.Fatal("setup guidance became a selectable model")
			}
		}
	}
}
