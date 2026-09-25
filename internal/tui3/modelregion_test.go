package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/modelsource"
	"github.com/Agent-Field/codeaf/internal/modelsource/sourcestub"
)

// modelRegionApp is the real model-service catalog over an empty profile. The
// shelf keeps every later listing on the same test-local seams as the existing
// connection tests; opening a region choice itself spends no network call.
func modelRegionApp(t *testing.T) (*app, string) {
	t.Helper()
	dir := t.TempDir()
	a := modelServiceTestApp(t, dir, "openai/gpt-4.1-mini",
		modelsource.NewSet(testDefaultService("sk-default-1234567890")),
		[]Model{{ID: "openai/gpt-4.1-mini"}})
	installModelServiceShelf(a, dir)
	return a, dir
}

// openModelRegion walks through the panel's own enter door on one catalog row.
func openModelRegion(t *testing.T, a *app, source string) int {
	t.Helper()
	a.openConnect()
	want := modelConnectionID(source)
	for at := range a.connPanel.hits {
		row, ok := a.connPanel.at(at)
		if !ok || row.ID != want {
			continue
		}
		a.connPanel.cursor = at
		drive(t, a, key("enter"))
		return at
	}
	t.Fatalf("the models group did not contain %s", source)
	return -1
}

func modelRegionBlock(a *app) string {
	lines, _, _ := a.inputBlock(a.width)
	plainLines := make([]string, 0, len(lines))
	for _, line := range lines {
		plainLines = append(plainLines, plain(line))
	}
	return strings.Join(plainLines, "\n")
}

func regionUnderCursor(block, name string) bool {
	for _, line := range strings.Split(block, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "› "+name) {
			return true
		}
	}
	return false
}

func TestARegionedServiceAsksWithAChoiceAndNotABox(t *testing.T) {
	for _, source := range []string{"z-ai", "moonshot", "qwen"} {
		t.Run(source, func(t *testing.T) {
			a, _ := modelRegionApp(t)
			openModelRegion(t, a, source)

			block := modelRegionBlock(a)
			if !strings.Contains(block, "your region") {
				t.Fatalf("the choice does not name its question:\n%s", block)
			}
			international, china := strings.Index(block, "International"), strings.Index(block, "China")
			if international < 0 || china < international {
				t.Fatalf("the regions are not drawn in catalog order:\n%s", block)
			}
			if !regionUnderCursor(block, "International") {
				t.Fatalf("the first region is not under the cursor:\n%s", block)
			}
			for _, typedShape := range []string{"International, China", "pick one of"} {
				if strings.Contains(block, typedShape) || strings.Contains(plain(frame(a)), typedShape) {
					t.Fatalf("the region choice still draws %q:\n%s", typedShape, block)
				}
			}
			if a.connPanel.entry == nil || !a.connPanel.entry.choosing() {
				t.Fatal("the region question did not open as a closed choice")
			}
			_ = frame(a)
			if a.caret {
				t.Fatal("the region choice drew a caret with no box to hold it")
			}
		})
	}
}

func TestTheRegionChoiceWalksTakesAndBacksOut(t *testing.T) {
	a, _ := modelRegionApp(t)
	openModelRegion(t, a, "z-ai")

	drive(t, a, key("down"))
	if block := modelRegionBlock(a); !regionUnderCursor(block, "China") {
		t.Fatalf("down did not put China under the cursor:\n%s", block)
	}
	drive(t, a, key("up"))
	if block := modelRegionBlock(a); !regionUnderCursor(block, "International") {
		t.Fatalf("up did not put International under the cursor:\n%s", block)
	}
	drive(t, a, key("ctrl+n"))
	if block := modelRegionBlock(a); !regionUnderCursor(block, "China") {
		t.Fatalf("ctrl+n did not put China under the cursor:\n%s", block)
	}
	drive(t, a, key("ctrl+p"))
	if block := modelRegionBlock(a); !regionUnderCursor(block, "International") {
		t.Fatalf("ctrl+p did not put International under the cursor:\n%s", block)
	}
	drive(t, a, key("down"), key("enter"))
	if a.connPanel.entry == nil || !a.connPanel.entry.secret {
		t.Fatal("taking a region did not open the secret key box")
	}
	if block := modelRegionBlock(a); !strings.Contains(block, "your key") {
		t.Fatalf("taking a region did not ask for the key:\n%s", block)
	}

	b, dir := modelRegionApp(t)
	rowAt := openModelRegion(t, b, "z-ai")
	drive(t, b, key("esc"))
	if !b.connPanel.open || b.connPanel.entry != nil || b.connPanel.cursor != rowAt {
		t.Fatal("esc did not return to the Z.ai row with the panel open")
	}
	if b.modelDraft != nil && b.modelDraft.row.Region != "" {
		t.Fatalf("esc kept a region in the draft: %+v", b.modelDraft.row)
	}
	if rows := config.PersistedSources(dir); len(rows) != 0 {
		t.Fatalf("esc saved a model service: %+v", rows)
	}
}

func TestALetterPicksTheRegionAndTypesNowhere(t *testing.T) {
	a, _ := modelRegionApp(t)
	openModelRegion(t, a, "z-ai")

	drive(t, a, key("c"))
	if block := modelRegionBlock(a); !regionUnderCursor(block, "China") {
		t.Fatalf("c did not jump to China:\n%s", block)
	}
	if got := a.connPanel.entry.box.String(); got != "" {
		t.Fatalf("c typed into the hidden editor: %q", got)
	}
	drive(t, a, key("i"))
	if block := modelRegionBlock(a); !regionUnderCursor(block, "International") {
		t.Fatalf("i did not jump to International:\n%s", block)
	}
}

func TestTheChosenRegionIsTheOneTheConnectionUses(t *testing.T) {
	a, _ := modelRegionApp(t)
	openModelRegion(t, a, "z-ai")
	drive(t, a, key("down"), key("enter"))
	if a.modelDraft == nil || a.modelDraft.row.Region != "cn" {
		t.Fatalf("China stored region %+v, want cn", a.modelDraft)
	}

	b, _ := modelRegionApp(t)
	openModelRegion(t, b, "z-ai")
	drive(t, b, key("enter"))
	if b.modelDraft == nil || b.modelDraft.row.Region != "intl" {
		t.Fatalf("the preselected row stored region %+v, want intl", b.modelDraft)
	}
}

func TestASourceWithoutRegionsOpensItsOwnBoxAsBefore(t *testing.T) {
	t.Run("DeepSeek key", func(t *testing.T) {
		a, _ := modelRegionApp(t)
		openModelRegion(t, a, "deepseek")
		if a.connPanel.entry == nil || a.connPanel.entry.choosing() || !a.connPanel.entry.secret {
			t.Fatal("DeepSeek did not open its secret key box")
		}
		if block := modelRegionBlock(a); !strings.Contains(block, "your key") {
			t.Fatalf("DeepSeek did not ask for its key:\n%s", block)
		}
	})

	t.Run("custom address", func(t *testing.T) {
		a, _ := modelRegionApp(t)
		openModelRegion(t, a, "custom")
		if a.connPanel.entry == nil || a.connPanel.entry.choosing() || a.connPanel.entry.secret {
			t.Fatal("Custom OpenAI-compatible API did not open its visible address box")
		}
		if block := modelRegionBlock(a); !strings.Contains(block, "your base url") {
			t.Fatalf("Custom OpenAI-compatible API did not ask for its base URL:\n%s", block)
		}
	})

	t.Run("Ollama starts", func(t *testing.T) {
		server := sourcestub.New("llama3")
		defer server.Close()
		a, _ := modelRegionApp(t)
		for at := range a.modelCatalog {
			if a.modelCatalog[at].ID == "ollama" {
				a.modelCatalog[at].Address = server.URL()
			}
		}
		openModelRegion(t, a, "ollama")
		if a.connPanel.entry != nil {
			t.Fatal("Ollama opened an answer box")
		}
	})
}

func TestTheProvidersSheetAsksWithTheSameChoice(t *testing.T) {
	a, dir := modelRegionApp(t)
	var source modelsource.Source
	for _, candidate := range modelsource.Vendored() {
		if candidate.ID == "z-ai" {
			source = candidate
			break
		}
	}
	row := config.PersistedSource{
		ID: "z-ai", Written: "z-ai", Key: "plan-test-key", Region: "intl", Door: "coding-plan", Order: 1,
	}
	if err := config.WriteSources(dir, []config.PersistedSource{row}); err != nil {
		t.Fatal(err)
	}
	connected := modelsource.Connected{
		Source: source, Key: row.Key, Address: source.Regions[0].Address, Door: source.Doors[0],
	}
	a.sources = modelsource.NewSet(testDefaultService("sk-default-1234567890"), connected)
	a.openSettings()
	toProviders(t, a)
	found := false
	for at, item := range a.sheet.items {
		if item.service != nil && item.service.id == "z-ai" {
			a.sheet.cursor = at
			found = true
			break
		}
	}
	if !found {
		t.Fatal("Providers did not draw the connected Z.ai service")
	}
	drive(t, a, key("enter"))
	// ENTER ON A CONNECTED SERVICE OPENS ITS FOUR-ACTION MENU now; the region
	// answer lives behind the edit door, which the menu names "rename".
	if !strings.Contains(strings.Join(sheetLabels(a), "\n"), "change key") {
		t.Fatal("enter on a connected service did not open the four-action menu")
	}
	drive(t, a, key("down"), key("enter"))

	screen := strings.Join(sheetLabels(a), "\n")
	if !strings.Contains(screen, "your region") ||
		!regionUnderCursor(screen, "International") ||
		strings.Index(screen, "China") < strings.Index(screen, "International") {
		t.Fatalf("the sheet did not draw the shared region choice:\n%s", screen)
	}
	drive(t, a, key("down"))
	screen = strings.Join(sheetLabels(a), "\n")
	if !regionUnderCursor(screen, "China") {
		t.Fatalf("the sheet did not redraw with China under the cursor:\n%s", screen)
	}
	drive(t, a, key("enter"))
	screen = strings.Join(sheetLabels(a), "\n")
	if a.sheet.conn.entry == nil || !a.sheet.conn.entry.secret || !strings.Contains(screen, "your key") {
		t.Fatalf("the sheet did not open the key box on the same row:\n%s", screen)
	}
}
