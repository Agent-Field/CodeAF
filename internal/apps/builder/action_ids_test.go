package builder

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/spec"
)

// The object is the one restaurant-inventory-orders kept failing on.
func TestAWrappedSelectionReferenceIsUnwrapped(t *testing.T) {
	files := spec.Files{
		spec.AppFile:                   []byte(`{"records":{}}`),
		"screens/purchase-orders.json": []byte(`{"body":{"part":"Button","on":{"click":[{"records.update":{"ids":["$selection.purchase-orders.ids"],"set":{"status":"Sent"}}},{"records.delete":{"ids":["a","b"]},"confirm":"Delete?"}]}}}`),
	}
	if changed := unwrapSelectionIDs(files); len(changed) != 1 {
		t.Fatalf("changed = %v", changed)
	}
	screen := string(files["screens/purchase-orders.json"])
	if !strings.Contains(screen, `"ids": "$selection.purchase-orders.ids"`) || !strings.Contains(screen, `"a"`) {
		t.Errorf("screen = %s", screen)
	}
}
