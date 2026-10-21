package executor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// C18 preserves ordinary host shell text when cells are off.
func TestC18CellsOffHostKeepsOrdinaryShellBehavior(t *testing.T) {
	t.Setenv("CODEAF_CELLS", "0")
	r, err := Host.In(t.TempDir()).Exec(context.Background(), ExecRequest{Argv: []string{"bash", "-lc", "printf '%s' '.cell'"}}, nil)
	if err != nil || strings.TrimSpace(string(r.Stdout)) != ".cell" {
		t.Fatalf("cells off plain shell refused harmless text: output=%q error=%v", r.Stdout, err)
	}
}

// C18 preserves host shell text even beside a cell while keeping actual cell
// state protected for sandboxed tools that own that workspace.
func TestC18HostTextAndCellStateProtectionStayDistinct(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".cell"), 0o700); err != nil {
		t.Fatal(err)
	}
	req := ExecRequest{Argv: []string{"bash", "-lc", "printf '%s' '.cell'"}}
	result, err := Host.In(root).Exec(t.Context(), req, nil)
	if err != nil || string(result.Stdout) != ".cell" {
		t.Fatalf("host shell text was filtered: %q %v", result.Stdout, err)
	}
	if _, err := (Local{Root: root, Class: Sandboxed}).Exec(t.Context(), req, nil); err == nil || !strings.Contains(err.Error(), "harness-owned .cell directory") {
		t.Fatalf("cell state protection was lost: %v", err)
	}
}
