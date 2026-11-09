package cellstore

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/keys"
)

func TestGitDiscoveryCeilingsKeepIgnoredWordsOutOfSavedHistory(t *testing.T) {
	forEachTransport(t, func(t *testing.T) {
		for _, boundary := range []string{"none", "workspace", "workspace trailing slash", "above repository", "between", "repository ceiling", "several", "several with ancestor fence", "symlink workspace", "symlink ancestor", "repository root", "repository inside boundary"} {
			t.Run(boundary, func(t *testing.T) {
				r := newRig(t)
				outer := t.TempDir()
				repo := filepath.Join(outer, "repo")
				between := filepath.Join(repo, "area")
				r.tree = filepath.Join(between, "work")
				r.write("included.txt", "F2_INCLUDED_WORDS\n")
				fixtureGit(t, repo, "init", "-q")
				ceiling, ignored := "", true
				switch boundary {
				case "workspace":
					ceiling = r.tree
				case "workspace trailing slash":
					ceiling = r.tree + string(filepath.Separator)
				case "above repository":
					ceiling = outer
				case "between":
					ceiling, ignored = between, false
				case "repository ceiling":
					ceiling, ignored = repo, false
				case "several":
					ceiling = strings.Join([]string{r.tree, outer}, string(os.PathListSeparator))
				case "several with ancestor fence":
					ceiling, ignored = strings.Join([]string{r.tree, between, outer}, string(os.PathListSeparator)), false
				case "symlink workspace", "symlink ancestor":
					alias := filepath.Join(t.TempDir(), "alias")
					if err := os.Symlink(between, alias); err != nil {
						t.Fatal(err)
					}
					r.tree = filepath.Join(alias, "work")
					ceiling = r.tree
					if boundary == "symlink ancestor" {
						ceiling, ignored = alias, false
					}
				case "repository root":
					r.tree = repo
				case "repository inside boundary":
					ceiling = between
					fixtureGit(t, r.tree, "init", "-q")
				}
				// The enclosing checkout hides the whole workspace only when Git cannot discover it.
				pattern := "confidential.txt\n"
				if !ignored {
					pattern = "area/work/\n"
				}
				if err := os.WriteFile(filepath.Join(repo, ".gitignore"), []byte(pattern), 0o644); err != nil {
					t.Fatal(err)
				}
				if boundary == "repository inside boundary" {
					r.write(".gitignore", pattern)
				}
				r.write("included.txt", "F2_INCLUDED_WORDS\n")
				r.write("confidential.txt", "CONFIDENTIAL_BOARD_MINUTES_F2\n")
				t.Setenv(gitCeilingVar, ceiling)
				if found := (keys.Scanner{}).Paths(r.tree, []string{"confidential.txt"}); len(found) != 0 {
					t.Fatalf("marker must test Git exclusion independently of credential screening: %+v", found)
				}
				git := exec.Command("git", "rev-parse", "--show-toplevel") //codeaf:plumbing Git is the discovery oracle for this capture fixture.
				git.Dir = r.tree
				root, gitErr := git.Output()
				if (gitErr == nil) != ignored {
					t.Fatalf("fixture Git discovery = %q, %v; want repository=%t", root, gitErr, ignored)
				}
				e := realEngine(t)
				e.Workspace, e.Guard = r.tree, r.guard
				sealed, err := e.Seal(t.Context(), r.cell, TurnInfo{})
				if err != nil {
					t.Fatal(err)
				}
				target := e.target(newCell(t))
				target.Tree, target.CellDir = t.TempDir(), t.TempDir()
				if _, err := e.transport().Do(t.Context(), target, materializeOp{Head: sealed.Turn.ID}); err != nil {
					t.Fatal(err)
				}
				if raw, err := os.ReadFile(filepath.Join(target.Tree, "included.txt")); err != nil || string(raw) != "F2_INCLUDED_WORDS\n" {
					t.Fatalf("included saved words = %q, %v", raw, err)
				}
				raw, err := os.ReadFile(filepath.Join(target.Tree, "confidential.txt"))
				if ignored {
					if !os.IsNotExist(err) {
						t.Fatalf("Git-ignored confidential words entered saved history: %q, %v", raw, err)
					}
				} else if err != nil || string(raw) != "CONFIDENTIAL_BOARD_MINUTES_F2\n" {
					t.Fatalf("a fenced enclosing checkout hid saved words: %q, %v", raw, err)
				}
			})
		}
	})
}
