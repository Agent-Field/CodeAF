package bare

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// Unix parity contract 2: ordinary chat rewrites retain the supplied bytes.
func TestUnixParityChatWriteBytes(t *testing.T) {
	dir := t.TempDir()
	mustWriteFile(t, dir, "file.txt", "old\r\n")
	got, bad := runTool(t, newWriteTool(dir), map[string]any{"path": "file.txt", "content": "new\n"})
	if bad {
		t.Fatal(got)
	}
	want := "new\n"
	if runtime.GOOS == "windows" {
		want = "new\r\n"
	}
	raw, err := os.ReadFile(filepath.Join(dir, "file.txt"))
	if err != nil || string(raw) != want {
		t.Fatalf("rewrite = %q, %v; want %q", raw, err, want)
	}
}

// Unix parity contract 2: a writable Unix file need not be readable to replace it.
func TestUnixParityWriteOnlyFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix file permissions")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "file.txt")
	mustWriteFile(t, dir, "file.txt", "old")
	if err := os.Chmod(path, 0o200); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })
	if _, err := os.ReadFile(path); err == nil {
		t.Skip("runner can read write-only files")
	}
	got, bad := runTool(t, newWriteTool(dir), map[string]any{"path": "file.txt", "content": "new"})
	if bad {
		t.Fatal(got)
	}
}
