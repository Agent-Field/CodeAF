//go:build !windows

package tool

import (
	"github.com/Agent-Field/codeaf/internal/executor"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// C18 preserves both senior-dev search helpers' complete inherited-pipe output.
func TestSearchHelpersRetainOutputFromInheritedPipe(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"rg", "git"} {
		script := "#!/bin/sh\nprintf 'first\\000'\n(sleep 0.4; printf 'second\\000') &\nexit 0\n"
		if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"first", "second"} {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	ctx := executor.With(t.Context(), executor.Host)
	result, err := (execRipgrepRunner{}).Run(ctx, dir, nil)
	if err != nil || result.code != 0 || string(result.stdout) != "first\x00second\x00" {
		t.Fatalf("rg: %+v %v", result, err)
	}
	files, ok := gitTrackedFiles(ctx, dir)
	if !ok || !reflect.DeepEqual(files, []string{"first", "second"}) {
		t.Fatalf("git files: %v %t", files, ok)
	}
}
