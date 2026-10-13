package check

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/spec"
)

func TestSurfaceDocBindingPassesExistingKitLaw(t *testing.T) {
	for _, name := range []string{"Pages", "Canvas", "Text", "Grid", "Timeline", "Graph", "Map"} {
		t.Run(name, func(t *testing.T) {
			files, _ := fixture(t)
			surface := strings.ToLower(name)
			if name == "Pages" {
				surface = "canvas"
			}
			files["screens/deal.json"] = []byte(`{"screen":"deal","title":"Deck","body":{"part":"` + name + `","bind":{"doc":"$record.deck","surface":"` + surface + `"}}}`)
			bundle, err := spec.Decode(files)
			if err != nil {
				t.Fatal(err)
			}
			kit, err := spec.LoadKit()
			if err != nil {
				t.Fatal(err)
			}
			if issues := lint(bundle, kit); len(issues) != 0 {
				t.Fatal(issues)
			}
		})
	}
}
