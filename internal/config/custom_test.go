package config

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/modelsource"
)

// The surfaces' action rows ("add custom connection", "active connection")
// carry sentinel ids that must never belong to a connection too: cursor and
// armed state are keyed by row id, and IsCustomID-based code would read a
// colliding row as a connection. THE NAMESPACE IS THE GUARANTEE, NOT A BAN:
// a sentinel id lives outside the custom/custom- namespace (modelsource.
// IsCustomID is false for it), and PrepareCustomSource — which mints custom
// or custom-<name> — can therefore never produce one, whatever a person
// names a connection. The literals are the tui3 sentinels' values; config
// cannot import tui3 to read the constants themselves.
func TestPrepareCustomSourceNeverMintsASentinelRowId(t *testing.T) {
	pre := [][]PersistedSource{
		nil,
		{{ID: modelsource.CustomID, Written: "homelab", Address: "http://127.0.0.1:9001/v1", Order: 1}},
	}
	for _, name := range []string{"add", "new-custom-connection", "switch-connection", "custom"} {
		for _, rows := range pre {
			dir := t.TempDir()
			if rows != nil {
				if err := WriteSources(dir, rows); err != nil {
					t.Fatal(err)
				}
			}
			row := PrepareCustomSource(dir, "http://127.0.0.1:9000/v1", name)
			if !modelsource.IsCustomID(row.ID) {
				t.Fatalf("name %q minted id %q, which is outside the custom namespace", name, row.ID)
			}
			for _, sentinel := range []string{"new-custom-connection", "switch-connection"} {
				if row.ID == sentinel {
					t.Fatalf("name %q minted id %q, which is a sentinel row id", name, row.ID)
				}
			}
		}
	}
}
