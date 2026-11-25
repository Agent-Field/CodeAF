package schema

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
)

func nodeID(seed string) string {
	digest := sha256.Sum256([]byte(seed))
	return fmt.Sprintf("node-%x", digest[:10])
}

// WithLegacyNodeIDs upgrades existing kit 0 screen bytes before checking or
// serving them. It leaves explicit IDs and an already upgraded screen intact.
func WithLegacyNodeIDs(raw []byte) []byte {
	var screen map[string]any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if decoder.Decode(&screen) != nil || screen == nil {
		return raw
	}
	seen := map[string]bool{}
	var visit func(any, string, func(map[string]any, string))
	visit = func(value any, at string, apply func(map[string]any, string)) {
		node, ok := value.(map[string]any)
		if !ok {
			return
		}
		apply(node, at)
		for i, child := range ruleValues(node, "children/*") {
			visit(child.value, fmt.Sprintf("%s/%d", at, i), apply)
		}
	}
	for _, root := range []string{"nav", "body"} {
		visit(screen[root], root, func(n map[string]any, _ string) {
			seen[asString(n["id"])] = true
		})
	}
	changed := false
	for _, root := range []string{"nav", "body"} {
		visit(screen[root], root, func(n map[string]any, path string) {
			if _, explicit := n["id"]; explicit {
				return
			}
			seed := "legacy/" + asString(screen["screen"]) + "/" + path + "/" + asString(n["part"])
			id := nodeID(seed)
			for seen[id] {
				seed += "/"
				id = nodeID(seed)
			}
			n["id"] = id
			seen[id] = true
			changed = true
		})
	}
	if changed {
		if out, err := json.MarshalIndent(screen, "", "  "); err == nil {
			return append(out, '\n')
		}
	}
	return raw
}
