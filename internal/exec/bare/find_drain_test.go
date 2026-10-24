package bare

import (
	"encoding/json"
	"github.com/Agent-Field/codeaf/internal/executor"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// C18 preserves fd's complete output when the helper exits before its child.
func TestFindRetainsOutputFromInheritedPipe(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires sh")
	}
	dir := t.TempDir()
	script := "#!/bin/sh\nprintf 'first.txt\\n'\n(sleep 0.4; printf 'second.txt\\n') &\nexit 0\n"
	if err := os.WriteFile(filepath.Join(dir, "fd"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	text, failed, err := newFindTool(dir, Caps{}).Execute(executor.With(t.Context(), executor.Host), json.RawMessage(`{"pattern":"*.txt"}`))
	if err != nil || failed || !strings.Contains(text, "first.txt") || !strings.Contains(text, "second.txt") {
		t.Fatalf("output %q, %t, %v", text, failed, err)
	}
}
