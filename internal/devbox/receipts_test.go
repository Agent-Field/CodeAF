package devbox

import (
	"bytes"
	"context"
	"io"
	"path/filepath"
	"strings"
	"testing"
)

// R5.7: every recovery receipt preserves the actual folder, including shell-significant names.
func TestDevboxR57RecoveryReceiptsKeepTheFolder(t *testing.T) {
	for _, mode := range []string{"default", "plain", "spaces", "apostrophe", "tilde"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			t.Chdir(root)
			var out bytes.Buffer
			f := fixtureFlow(t, "", &out)
			dir := filepath.Join(f.Home, "devbox", "box")
			suffix := ""
			switch mode {
			case "plain":
				dir = filepath.Join(root, "custom")
				suffix = " --dir " + dir
			case "spaces":
				dir = filepath.Join(root, "custom folder")
				suffix = " --dir '" + dir + "'"
			case "apostrophe":
				dir = filepath.Join(root, "owner's $box")
				suffix = " --dir '" + root + "/owner'\"'\"'s $box'"
			case "tilde":
				dir = "~/box"
				suffix = " --dir '~/box'"
			}
			f.Pair = func(context.Context, string, func(Offer)) error {
				t.Fatal("R5.7 missing outputs started pairing")
				return nil
			}
			f.RunTerraform = func(ctx context.Context, path, dir string, args, env []string, stdout, stderr io.Writer) error {
				if args[0] == "output" {
					_, err := io.WriteString(stdout, `{}`)
					return err
				}
				return RunTerraform(ctx, path, dir, args, env, stdout, stderr)
			}
			opts := Options{Name: "box", Dir: dir, Yes: true}
			err := f.Run(t.Context(), "up", opts)
			if err == nil || !strings.Contains(err.Error(), "`codeaf devbox down box"+suffix+"`") {
				t.Fatalf("R5.7 pairing receipt: %v", err)
			}
			err = f.Run(t.Context(), "up", opts)
			if err == nil || !strings.Contains(err.Error(), "`codeaf devbox status box"+suffix+"`") {
				t.Fatalf("R5.7 existing receipt: %v", err)
			}
			if mode == "default" && strings.Contains(err.Error(), "--dir") {
				t.Fatal("R5.7 default unnecessarily changes command")
			}
		})
	}
}
