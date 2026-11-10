package exec

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/Agent-Field/codeaf/internal/executor"
)

// Unix parity 2 contract items 1/2: gated bytes, shell or status expectations.
// Text contract C1: Unix foreground commands load login profiles; Windows
// inherits PATH without a login shell. Detached commands retain login/pipefail.
func TestTextContractShellEnvironment(t *testing.T) {
	t.Setenv("CODEAF_PROFILE_DIR", t.TempDir())
	t.Setenv("CODEAF_TEXT_TEST", "inherited")
	t.Setenv("PATH", os.Getenv("PATH")+string(os.PathListSeparator)+"codeaf-text-inherited")
	command := `shopt -q login_shell || exit 9; printf '%s' "$CODEAF_TEXT_TEST"`
	if runtime.GOOS == "windows" {
		command = `shopt -q login_shell && exit 9; [[ "$PATH" == *codeaf-text-inherited* ]] || exit 8; printf '%s' "$CODEAF_TEXT_TEST"`
	}
	tools := NewToolbox(workspace(t), "1", nil)
	got := tools.Execute(t.Context(), "sh", textContractArgs(map[string]any{"cmd": command}))
	if got.IsError || got.Content != "inherited" {
		t.Fatalf("foreground = %+v", got)
	}
	command = `shopt -q login_shell || exit 9; printf '%s' "$CODEAF_TEXT_TEST"`
	var output bytes.Buffer
	cmd, err := detachedCommand(executor.Host, t.Context(), t.TempDir(), command, nil, &output, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Run(); err != nil || output.String() != "inherited" {
		t.Fatalf("detached = %q, %v", output.String(), err)
	}
	cmd, err = detachedCommand(executor.Host, t.Context(), t.TempDir(), "false | true", nil, &output, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Run(); err == nil {
		t.Fatal("detached command ignored pipeline failure")
	}
}

// Unix parity 2 contract items 1/2: gated bytes, shell or status expectations.
// Text contract C2/C4: UTF-16 preserves its encoding on all OSes. Ordinary
// Unix edits and rewrites use literal bytes; Windows preserves EOL style.
func TestTextContractHeadlessEditWrite(t *testing.T) {
	for _, fixture := range []struct {
		name                   string
		raw, edited, rewritten []byte
	}{
		{"utf8", []byte("a\r\nb\r\n"), []byte("a\r\nb\r\nc\r\n"), []byte("x\r\ny\r\n")},
		{"le", []byte{255, 254, 'a', 0, 13, 0, 10, 0, 'b', 0, 13, 0, 10, 0}, []byte{255, 254, 'a', 0, 13, 0, 10, 0, 'b', 0, 13, 0, 10, 0, 'c', 0, 13, 0, 10, 0}, []byte{255, 254, 'x', 0, 13, 0, 10, 0, 'y', 0, 13, 0, 10, 0}},
		{"be", []byte{254, 255, 0, 'a', 0, 13, 0, 10, 0, 'b', 0, 13, 0, 10}, []byte{254, 255, 0, 'a', 0, 13, 0, 10, 0, 'b', 0, 13, 0, 10, 0, 'c', 0, 13, 0, 10}, []byte{254, 255, 0, 'x', 0, 13, 0, 10, 0, 'y', 0, 13, 0, 10}},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			space := workspace(t)
			tools := NewToolbox(space, "1", nil)
			path := filepath.Join(space.Root(), "file.txt")
			if err := os.WriteFile(path, fixture.raw, 0o644); err != nil {
				t.Fatal(err)
			}
			for _, step := range []struct {
				tool string
				args map[string]any
				want []byte
			}{
				{"edit", map[string]any{"path": "file.txt", "old": "a\nb\n", "new": "a\nb\nc\n"}, fixture.edited},
				{"write", map[string]any{"path": "file.txt", "text": "x\ny\n"}, fixture.rewritten},
			} {
				got := tools.Execute(t.Context(), step.tool, textContractArgs(step.args))
				if runtime.GOOS != "windows" && fixture.name == "utf8" && step.tool == "edit" {
					if !got.IsError {
						t.Fatal("Unix literal edit accepted LF against CRLF")
					}
					raw, err := os.ReadFile(path)
					if err != nil || !bytes.Equal(raw, fixture.raw) {
						t.Fatalf("refused edit changed file: %x, %v", raw, err)
					}
					// The original Unix exact CRLF match still works, with literal replacement bytes.
					got = tools.Execute(t.Context(), "edit", textContractArgs(map[string]any{"path": "file.txt", "old": "a\r\nb\r\n", "new": "a\nb\nc\n"}))
					step.want = []byte("a\nb\nc\n")
				}
				if runtime.GOOS != "windows" && fixture.name == "utf8" && step.tool == "write" {
					step.want = []byte("x\ny\n")
				}
				if got.IsError {
					t.Fatalf("%s = %+v", step.tool, got)
				}
				raw, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(raw, step.want) {
					t.Fatalf("%s bytes = %x, %v; want %x", step.tool, raw, err, step.want)
				}
			}
		})
	}
}

// Unix parity 2 contract items 1/2: gated bytes, shell or status expectations.
// Text contract C3: only Windows normalizes CRLF in headless output.
func TestTextContractHeadlessOutput(t *testing.T) {
	t.Setenv("CODEAF_PROFILE_DIR", t.TempDir())
	tools := NewToolbox(workspace(t), "1", nil)
	got := tools.Execute(t.Context(), "sh", textContractArgs(map[string]any{"cmd": `printf 'version\r\nprogress\rnext\r\n'`}))
	want := "version\r\nprogress\rnext\r\n"
	if runtime.GOOS == "windows" {
		want = "version\nprogress\rnext\n"
	}
	if got.IsError || got.Content != want {
		t.Fatalf("sh = %+v", got)
	}
}

func textContractArgs(args map[string]any) string { raw, _ := json.Marshal(args); return string(raw) }
