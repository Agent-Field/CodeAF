package builder

import (
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/spec"
)

// unwrapSelectionIDs rewrites ids: ["$selection.<list>.ids"] to the reference
// itself. A list holding one reference to a list is never what was meant: the
// action would receive the text of the reference as its only id. It returns
// the paths it changed.
func unwrapSelectionIDs(files spec.Files) []string {
	var changed []string
	for _, path := range files.Paths() {
		if path != spec.AppFile && !strings.HasPrefix(path, "screens/") {
			continue
		}
		doc, ok := decodeObject(files[path])
		if ok && unwrapIDs(doc) && encodeInto(files, path, doc) {
			changed = append(changed, path)
		}
	}
	return changed
}

func unwrapIDs(value any) bool {
	changed := false
	switch value := value.(type) {
	case map[string]any:
		if list, ok := value["ids"].([]any); ok && len(list) == 1 {
			if ref, _ := list[0].(string); strings.HasPrefix(ref, "$") && strings.HasSuffix(ref, ".ids") {
				value["ids"], changed = ref, true
			}
		}
		for _, child := range value {
			changed = unwrapIDs(child) || changed
		}
	case []any:
		for _, child := range value {
			changed = unwrapIDs(child) || changed
		}
	}
	return changed
}
