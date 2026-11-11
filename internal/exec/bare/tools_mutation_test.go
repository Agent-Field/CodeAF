package bare

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"testing/synctest"
)

func TestDanglingLeafAliasesSerializeWritesAndObservers(t *testing.T) {
	for _, name := range []string{"relative", "absolute", "chain", "directory alias", "aliased parent with traversal", "target traversal through directory alias"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			target := filepath.Join(root, "target.md")
			alias := "link.md"
			linkTarget := "target.md"
			switch name {
			case "absolute":
				linkTarget = target
			case "chain":
				if err := os.Symlink("target.md", filepath.Join(root, "middle.md")); err != nil {
					t.Fatal(err)
				}
				linkTarget = "middle.md"
			case "directory alias":
				if err := os.Symlink(root, filepath.Join(root, "dir")); err != nil {
					t.Fatal(err)
				}
				linkTarget = filepath.Join("dir", "target.md")
			case "aliased parent with traversal", "target traversal through directory alias":
				parent := filepath.Join(root, "real", "sub")
				if err := os.MkdirAll(parent, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(parent, filepath.Join(root, "dir")); err != nil {
					t.Fatal(err)
				}
				linkTarget = "dir/../target.md"
				if name == "aliased parent with traversal" {
					alias, linkTarget = filepath.Join("dir", "link.md"), filepath.Join("..", "target.md")
				}
				target = filepath.Join(root, "real", "target.md")
			}
			if err := os.Symlink(linkTarget, filepath.Join(root, alias)); err != nil {
				t.Fatal(err)
			}
			write := newWriteTool(root)
			synctest.Test(t, func(t *testing.T) {
				entered, resume := make(chan struct{}), make(chan struct{})
				events, done := make(chan string, 2), make(chan error, 2)
				ctx := WithWriteObserver(context.Background(), func(event WriteEvent) {
					if string(event.Content) == "first" {
						close(entered)
						<-resume
					}
					events <- string(event.Content)
				})
				run := func(path, content string) {
					args, _ := json.Marshal(map[string]string{"path": path, "content": content})
					out, bad, err := write.Execute(ctx, args)
					if bad && err == nil {
						err = fmt.Errorf("write: %s", out)
					}
					done <- err
				}
				// Choose the alias queue while the leaf target is absent. Pause
				// its observer after creation, then start a write to the target.
				go run(alias, "first")
				select {
				case <-entered:
				case err := <-done:
					t.Fatalf("alias write stopped before observation: %v", err)
				}
				go run(target, "second")
				synctest.Wait()
				close(resume)
				for range 2 {
					if err := <-done; err != nil {
						t.Fatal(err)
					}
				}
				if first, second := <-events, <-events; first != "first" || second != "second" {
					t.Fatalf("observers escaped serialization: %q, %q", first, second)
				}
			})
			if got, err := os.ReadFile(target); err != nil || string(got) != "second" {
				t.Fatalf("final bytes: %q, %v", got, err)
			}
		})
	}
}

func TestWriteThroughSymlinkLoopsReturnsError(t *testing.T) {
	for _, target := range []string{"link.md", "other.md"} {
		t.Run(target, func(t *testing.T) {
			root := t.TempDir()
			if err := os.Symlink(target, filepath.Join(root, "link.md")); err != nil {
				t.Fatal(err)
			}
			if target == "other.md" {
				if err := os.Symlink("link.md", filepath.Join(root, target)); err != nil {
					t.Fatal(err)
				}
			}
			if out, bad := runTool(t, newWriteTool(root), map[string]string{"path": "link.md", "content": "data"}); !bad {
				t.Fatalf("looping symlink accepted: %s", out)
			}
		})
	}
}
