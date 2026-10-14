package builder

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/builder/prompt"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
)

func TestWritePromptIncludesEmbeddedCatalog(t *testing.T) {
	raw, err := spec.KitJSON()
	if err != nil {
		t.Fatal(err)
	}
	catalog := catalogText()
	if catalog != compact(raw) {
		t.Fatal("WRITE catalog differs from embedded kit.json")
	}
	files, _ := spec.GoldenCRM()
	text, err := prompt.Render(prompt.KindWrite, prompt.Inputs{
		Plan: string(files[spec.PlanFile]), Catalog: catalog, Schemas: schemaFor(spec.ScreenSchemaURL),
		Space: "personal", Budget: "$20", AgentModel: DefaultModel, Only: []string{"screens/pipeline.json"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, catalog) {
		t.Fatal("WRITE prompt omits the catalog")
	}
	for _, name := range []string{"FilterTokens", "StatStrip", "ScreenSubtitle"} {
		if !strings.Contains(catalog, `"name":"`+name+`"`) {
			t.Fatalf("WRITE catalog omits %s", name)
		}
	}
	t.Log("WRITE includes the exact compact embedded catalog and all board 08 parts")
}
