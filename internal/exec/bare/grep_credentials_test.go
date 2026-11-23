package bare

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A search of a home folder may pass through a key, and must not bring it back.
func TestGrepLeavesOutWhatItFindsInCredentialFiles(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CODEAF_HOME", filepath.Join(home, ".codeaf"))
	for name, body := range map[string]string{
		".ssh/id_rsa":         "needle in a key\n",
		".codeaf/config.json": "needle in the config\n",
		".aws/credentials":    "needle in aws\n",
		"notes/plain.txt":     "needle in a note\n",
	} {
		full := filepath.Join(home, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, withRipgrep := range []bool{true, false} {
		path, present := ripgrepPath()
		if withRipgrep && !present {
			continue
		}
		tool := newGrepToolUsing(home, DefaultCaps(), path, withRipgrep && present)
		args, _ := json.Marshal(map[string]string{"pattern": "needle", "path": home})
		text, failed, err := tool.Execute(context.Background(), args)
		if err != nil || failed {
			t.Fatalf("search failed: %v %q", err, text)
		}
		if !strings.Contains(text, "needle in a note") {
			t.Fatalf("the ordinary file was lost: %q", text)
		}
		for _, secret := range []string{"in a key", "in the config", "in aws"} {
			if strings.Contains(text, secret) {
				t.Fatalf("the search brought back %q: %s", secret, text)
			}
		}
		if !strings.Contains(text, "credential files were left out") {
			t.Fatalf("the search did not say what it left out: %q", text)
		}
	}
}
