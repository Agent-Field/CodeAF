package cellstore

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/keys"
)

func TestAChatInsideAnIgnoredFolderSavesItsWordsWithoutIgnoredFiles(t *testing.T) {
	forEachTransport(t, func(t *testing.T) {
		for _, layout := range []string{"folder ignored", "parent ignored", "nested repository", "workspace ceiling", "ancestor ceiling", "no repository", "broken repository"} {
			t.Run(layout, func(t *testing.T) {
				r := newRig(t)
				repo := t.TempDir()
				r.tree = filepath.Join(repo, "area", "work")
				r.write("included.txt", "project words\n")
				r.write("secret.txt", "CONFIDENTIAL_BOARD_MINUTES_W3\n")
				if layout != "no repository" {
					fixtureGit(t, repo, "init", "-q")
					pattern := "area/work/\n"
					if layout == "parent ignored" {
						pattern = "area/\n"
					}
					if err := os.WriteFile(filepath.Join(repo, ".gitignore"), []byte(pattern), 0o600); err != nil {
						t.Fatal(err)
					}
				}
				ceiling := ""
				if layout == "workspace ceiling" {
					ceiling = r.tree
				} else if layout == "ancestor ceiling" {
					ceiling = filepath.Dir(r.tree)
				} else if layout == "nested repository" {
					fixtureGit(t, r.tree, "init", "-q")
					r.write(".gitignore", "secret.txt\n")
				} else if layout == "broken repository" {
					r.write(".git", "gitdir: missing-directory\n")
				}
				t.Setenv(gitCeilingVar, ceiling)
				if found := (keys.Scanner{}).Paths(r.tree, []string{"secret.txt"}); len(found) != 0 {
					t.Fatalf("the marker must not match the credential scanner: %+v", found)
				}
				oracle := exec.Command("git", "check-ignore", "-q", "--", "secret.txt") //codeaf:plumbing Git decides whether the fixture's marker is ignored.
				oracle.Dir = r.tree
				ignored := oracle.Run() == nil
				wantIgnored := layout != "ancestor ceiling" && layout != "no repository" && layout != "broken repository"
				if ignored != wantIgnored {
					t.Fatalf("Git ignored secret.txt = %v, want %v", ignored, wantIgnored)
				}
				transcript, err := r.cell.Path(cell.TranscriptPath)
				if err != nil {
					t.Fatal(err)
				}
				words := []byte("{\"type\":\"message\",\"role\":\"user\",\"content\":\"chat words always save\"}\n")
				if err := os.WriteFile(transcript, words, 0o600); err != nil {
					t.Fatal(err)
				}
				e := realEngine(t)
				e.Workspace, e.Guard = r.tree, r.guard
				sealed, err := e.Seal(t.Context(), r.cell, TurnInfo{})
				if layout == "broken repository" {
					if err == nil || !strings.Contains(err.Error(), ignoredReadFailure) {
						t.Fatalf("a genuine Git failure must refuse saving: %v", err)
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				target := e.target(newCell(t))
				target.Tree, target.CellDir = t.TempDir(), t.TempDir()
				if _, err := e.transport().Do(t.Context(), target, materializeOp{Head: sealed.Turn.ID}); err != nil {
					t.Fatal(err)
				}
				raw, err := os.ReadFile(filepath.Join(target.CellDir, "transcript.jsonl"))
				if err != nil || !bytes.Equal(raw, words) {
					t.Fatalf("the saved chat lost its words: %q, %v", raw, err)
				}
				raw, err = os.ReadFile(filepath.Join(target.Tree, "secret.txt"))
				if ignored {
					if !os.IsNotExist(err) {
						t.Fatalf("Git-ignored confidential words entered saved history: %q, %v", raw, err)
					}
					if hits := grepStore(t, e.StoreDir(r.cell), "CONFIDENTIAL_BOARD_MINUTES_W3"); len(hits) != 0 {
						t.Fatalf("ignored confidential words entered saved objects: %v", hits)
					}
				} else if err != nil || string(raw) != "CONFIDENTIAL_BOARD_MINUTES_W3\n" {
					t.Fatalf("Git did not ignore the ordinary project file: %q, %v", raw, err)
				}
				_, err = os.Stat(filepath.Join(target.Tree, "included.txt"))
				whole := ignored && layout != "nested repository"
				if (whole && !os.IsNotExist(err)) || (!whole && err != nil) {
					t.Fatalf("the whole-project ignore answer was not respected: %v", err)
				}
			})
		}
	})
}
