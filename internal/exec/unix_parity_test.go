package exec

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// Unix parity contract 2: cron and systemd foreground commands retain login bash.
func TestUnixParityForegroundLogin(t *testing.T) {
	t.Setenv("CODEAF_PROFILE_DIR", t.TempDir())
	tools := NewToolbox(workspace(t), "1", nil)
	want := "login"
	if runtime.GOOS == "windows" {
		want = "inherited"
	}
	got := tools.Execute(t.Context(), "sh", `{"cmd":"if shopt -q login_shell; then printf login; else printf inherited; fi"}`)
	if got.IsError || got.Content != want {
		t.Fatalf("foreground = %+v; want %q", got, want)
	}
}

// Unix parity contract 2: literal CRLF edits and exact rewrite bytes remain valid.
func TestUnixParityHeadlessFileBytes(t *testing.T) {
	space := workspace(t)
	tools := NewToolbox(space, "1", nil)
	path := filepath.Join(space.Root(), "file.txt")
	if err := os.WriteFile(path, []byte("a\r\nb\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := tools.Execute(t.Context(), "edit", `{"path":"file.txt","old":"a\r\n","new":"x\n"}`)
	if got.IsError {
		t.Fatal(got.Content)
	}
	want := "x\nb\r\n"
	if runtime.GOOS == "windows" {
		want = "x\r\nb\r\n"
	}
	assertParityFile(t, path, want)
	got = tools.Execute(t.Context(), "write", `{"path":"file.txt","text":"literal\r\nnext\n"}`)
	if got.IsError {
		t.Fatal(got.Content)
	}
	want = "literal\r\nnext\n"
	if runtime.GOOS == "windows" {
		want = "literal\r\nnext\r\n"
	}
	assertParityFile(t, path, want)
}

func assertParityFile(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil || string(got) != want {
		t.Fatalf("file = %q, %v; want %q", got, err, want)
	}
}
