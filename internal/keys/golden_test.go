package keys

import (
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/lawcheck"
)

var _ = lawcheck.UpdateFlag()

// The vault document and its envelope are unexported, so their golden rows
// live here; the exported schemas are in internal/lawcheck.
func TestGoldenVault(t *testing.T) {
	for name, sample := range map[string]any{
		"vault.envelope": envelope{V: 1, CellKeyID: "00112233445566778899aabbccddeeff", Nonce: "00112233445566778899aabbccddeeff0011223344556677", Data: "cafe"},
		"vault.doc": vaultDoc{V: 1, Updated: 1759049990000, Secrets: map[string]Entry{
			"a41c9e0b7d3f2586c1e0b94a7d3f2851": {Name: "DATABASE_URL", Value: "postgres://x", Scope: "p_8c1f2a"},
		}},
	} {
		t.Run(name, func(t *testing.T) {
			lawcheck.Golden(t, filepath.Join("testdata", name+".golden.json"), sample)
		})
	}
}
