package keys

import (
	"os"
	"path/filepath"
	"testing"
)

// TestK3WindowsSecretNamesFollowNTFSCaseRules proves K3: an upper-case dotenv
// or key-container name cannot bypass the same guard used for its lower-case
// spelling. Findings keep their original portable path for the vault.
func TestK3WindowsSecretNamesFollowNTFSCaseRules(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{".ENV", "Nested/Local.ENV", "Key.PFX"} {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("fixture value"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	got := map[string]string{}
	for _, finding := range ScanTree(root) {
		got[finding.Path] = finding.Rule
	}
	for name, rule := range map[string]string{".ENV": RuleDotenv, "Nested/Local.ENV": RuleDotenv, "Key.PFX": "private-key-file"} {
		if got[name] != rule {
			t.Errorf("K3 %q: rule %q, want %q", name, got[name], rule)
		}
	}
}
