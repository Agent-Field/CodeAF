//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/workspace"
)

// The subprocess proves the shipped assembly supplies the tools. The other
// journeys exercise the same session runtime in process for inspectable receipts.
func testOrganizationBinary(t *testing.T) {
	started := time.Now()
	w := newWorld(t)
	pinEveryTextModel(t)
	path := filepath.Join(repoRoot(t), "bin", "aforge")
	if _, err := os.Stat(path); err != nil {
		t.Fatal("build the review binary with make build before a live run")
	}
	profile := filepath.Join(w.home, "config.json")
	raw, err := os.ReadFile(profile)
	if err != nil {
		t.Fatal(err)
	}
	var rows map[string]any
	if err = json.Unmarshal(raw, &rows); err != nil {
		t.Fatal(err)
	}
	rows[config.KeyMemoryEnabled] = "off"
	// Change only the disposable profile. The settings action itself would
	// install/uninstall the person's OS timer, which this test must never do.
	rows[config.KeyStandingBackground] = "off"
	raw, err = json.Marshal(rows)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(profile, raw, 0600); err != nil {
		t.Fatal(err)
	}
	name := "Binary organization " + orgNonce(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "chat", "--no-host", "--one-model", "--model", e2eModel, "--reasoning", "low", "--yolo", "--max-cost", "0.15", "--once", fmt.Sprintf("Use the collections tool to create exactly one collection named %q, then add this conversation to it by omitting ref entirely on the add call. Do not use exec, write, or any other way to modify files. Do not start tasks or ongoing work.", name))
	cmd.Dir = t.TempDir()
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("binary journey failed: %v\n%s", err, output)
	}
	if strings.Count(string(output), "tool: collections") < 2 {
		t.Fatalf("binary did not expose collection tool receipts: %s", output)
	}
	for _, verb := range []string{"bash", "exec", "write", "tasks", "propose_task", "shared_context"} {
		if strings.Contains(string(output), "tool: "+verb) {
			t.Fatalf("unexpected binary action %s", verb)
		}
	}
	usd, models := ledgerSince(t, started)
	if usd > 0.15 {
		t.Fatalf("binary exceeded rail: $%.6f", usd)
	}
	for _, model := range models {
		if model != e2eModel {
			t.Fatalf("unconfigured binary model %s", model)
		}
	}
	t.Logf("BINARY RECEIPT spend=$%.6f models=%v", usd, models)
	s, err := workspace.Open(orgStorePath())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	collections, err := s.Collections(context.Background())
	if err != nil || len(collections) != 1 || collections[0].Name != name {
		t.Fatalf("binary collections=%+v %v", collections, err)
	}
	refs, err := s.Members(context.Background(), collections[0].ID)
	if err != nil || len(refs) != 1 || refs[0].Kind != workspace.ConversationKind || refs[0].ID == "unfiled" {
		t.Fatalf("binary membership=%+v %v", refs, err)
	}
	t.Logf("BINARY collection=%s conversation=%s memory=off", collections[0].ID, refs[0].ID)
}
