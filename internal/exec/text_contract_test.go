package exec

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/executor"
)

// Text contract C1: foreground commands keep the inherited PATH without a login
// shell; detached jobs and services retain a login shell and pipefail.
func TestTextContractShellEnvironment(t *testing.T) {
	t.Setenv("CODEAF_PROFILE_DIR", t.TempDir())
	t.Setenv("CODEAF_TEXT_TEST", "inherited")
	t.Setenv("PATH", os.Getenv("PATH")+string(os.PathListSeparator)+"codeaf-text-inherited")
	command := `shopt -q login_shell && exit 9; [[ "$PATH" == *codeaf-text-inherited* ]] || exit 8; printf '%s' "$CODEAF_TEXT_TEST"`
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

// Text contract C2/C4: the headless edit/write tools preserve UTF-16 and CRLF.
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

// Text contract C3: model-visible headless output normalizes CRLF only.
func TestTextContractHeadlessOutput(t *testing.T) {
	t.Setenv("CODEAF_PROFILE_DIR", t.TempDir())
	tools := NewToolbox(workspace(t), "1", nil)
	got := tools.Execute(t.Context(), "sh", textContractArgs(map[string]any{"cmd": `printf 'version\r\nprogress\rnext\r\n'`}))
	if got.IsError || strings.Contains(got.Content, "\r\n") || got.Content != "version\nprogress\rnext\n" {
		t.Fatalf("sh = %+v", got)
	}
}

func textContractArgs(args map[string]any) string { raw, _ := json.Marshal(args); return string(raw) }
