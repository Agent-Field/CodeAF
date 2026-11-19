package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/session"
)

// A saved harness reaches the tools through this bridge and not through the
// chat's consent path, so the credential floor has to stand on the bridge.
func TestTheHarnessBridgeRefusesTheProfileKey(t *testing.T) {
	const fake = "sk-or-v1-FAKE0000000000000000000000000000000000000000000000000000"
	stateRoot := t.TempDir()
	t.Setenv("CODEAF_HOME", stateRoot)
	if err := os.WriteFile(filepath.Join(stateRoot, "config.json"), []byte(`{"api_key":"`+fake+`"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	tools := v3HarnessTools(t.TempDir(), session.HarnessBeltSeams{})
	out, err := tools(context.Background(), "bash", "cat "+filepath.Join(stateRoot, "config.json"))
	if strings.Contains(out, "FAKE0000") || (err != nil && strings.Contains(err.Error(), "FAKE0000")) {
		t.Fatal("the profile key came back through the harness bridge")
	}
	if err == nil || !strings.Contains(err.Error(), "reaches for a credential") {
		t.Fatalf("the harness bridge did not refuse the read: %v", err)
	}
}
