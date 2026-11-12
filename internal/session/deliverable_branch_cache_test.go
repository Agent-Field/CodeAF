package session

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// withCacheBudget runs a test under a small retained budget.
func withCacheBudget(t *testing.T, bytes int64, slots int, folder int64) {
	t.Helper()
	oldBytes, oldSlots, oldFolder := deliverCacheRetainMax, deliverCacheSlotsMax, deliverCacheFolderMax
	deliverCacheRetainMax, deliverCacheSlotsMax, deliverCacheFolderMax = bytes, slots, folder
	t.Cleanup(func() {
		deliverCacheRetainMax, deliverCacheSlotsMax, deliverCacheFolderMax = oldBytes, oldSlots, oldFolder
	})
}

// branchRepo is a repository whose branches each hold files at rels, every
// branch unmerged. It answers the repository.
func branchRepo(t *testing.T, branches []string, rels []string) string {
	t.Helper()
	repo := canonicalFile(t.TempDir())
	gitT(t, repo, "init", "-q", "-b", "main")
	os.WriteFile(filepath.Join(repo, "a.txt"), []byte("a\n"), 0o644)
	gitT(t, repo, "add", ".")
	gitT(t, repo, "commit", "-q", "-m", "base")
	for _, branch := range branches {
		gitT(t, repo, "checkout", "-q", "-b", branch, "main")
		for _, rel := range rels {
			full := filepath.Join(repo, filepath.FromSlash(rel))
			os.MkdirAll(filepath.Dir(full), 0o755)
			os.WriteFile(full, []byte(branch+" "+rel+"\n"), 0o644)
		}
		gitT(t, repo, "add", ".")
		gitT(t, repo, "commit", "-q", "-m", "work")
	}
	gitT(t, repo, "checkout", "-q", "main")
	return repo
}

func blobOf(t *testing.T, repo, branch, rel string) string {
	t.Helper()
	return strings.TrimSpace(gitOut(t, repo, "rev-parse", "refs/heads/"+branch+":"+rel))
}

// Many folders and many branches, every name long: the cache keeps within its
// budget however many it meets, and every answer is still git's exact one -
// past the budget it asks again, of the pinned tip, rather than guess.
func TestTheCacheKeepsWithinItsBudgetAcrossManyFoldersAndTips(t *testing.T) {
	withCacheBudget(t, 16<<10, 64, 4<<10)
	long := strings.Repeat("n", 150)
	var rels []string
	for i := 0; i < 30; i++ {
		rels = append(rels, fmt.Sprintf("f%02d-%s/%s-%02d.md", i, long, long, i))
	}
	var branches []string
	for i := 0; i < 6; i++ {
		branches = append(branches, fmt.Sprintf("task/b%d-aaaaaa", i))
	}
	repo := branchRepo(t, branches, rels)
	cache := NewDeliverSourceCache()
	ctx := context.Background()
	for _, branch := range branches {
		for _, rel := range rels {
			oid, verdict := branchBlobIn(ctx, cache, repo, branch, rel)
			if verdict != branchUnmerged || oid != blobOf(t, repo, branch, rel) {
				t.Fatalf("%s:%s answered %q (%d)", branch, rel[:12], oid, verdict)
			}
			if bytes, slots := cache.Retained(); bytes > deliverCacheRetainMax || slots > deliverCacheSlotsMax {
				t.Fatalf("the cache keeps %d bytes in %d slots past its budget", bytes, slots)
			}
		}
		if _, verdict := branchBlobIn(ctx, cache, repo, branch, "f00-"+long+"/absent.md"); verdict != branchSettled {
			t.Fatalf("an absent file answered %d", verdict)
		}
	}
	cache.mu.Lock()
	full := cache.full
	cache.mu.Unlock()
	if !full {
		t.Fatal("the budget was never reached, so this proved nothing")
	}
	t.Logf("%d answers in %d git calls", len(branches)*len(rels), cache.GitCalls())
}

// A folder too big to keep is listed ONCE, parsed only up to its budget and
// given up, then answered rel by rel - exactly - keeping only its mark.
func TestAnOversizeFolderIsAnsweredRelByRel(t *testing.T) {
	withCacheBudget(t, 2<<20, 16384, 8<<10)
	var rels []string
	for i := 0; i < 400; i++ {
		rels = append(rels, fmt.Sprintf("big/file-%03d.md", i))
	}
	repo := branchRepo(t, []string{"task/big-aaaaaa"}, rels)
	cache := NewDeliverSourceCache()
	for _, rel := range rels[:40] {
		oid, verdict := branchBlobIn(context.Background(), cache, repo, "task/big-aaaaaa", rel)
		if verdict != branchUnmerged || oid != blobOf(t, repo, "task/big-aaaaaa", rel) {
			t.Fatalf("%s answered %q (%d)", rel, oid, verdict)
		}
	}
	// two for the tip, one listing given up, then one per rel
	if calls := cache.GitCalls(); calls != 2+1+40 {
		t.Fatalf("%d git calls, want %d", calls, 2+1+40)
	}
	bytes, slots := cache.Retained()
	if slots != 2 || bytes > 1024 {
		t.Fatalf("an oversize folder kept %d bytes in %d slots", bytes, slots)
	}
}

// A listing under a budget too small to keep anything still finds every row,
// on its branch, page by page within each page's deadline; nothing is left
// out and no cursor is lost.
func TestAListingUnderAFullCacheLosesNoRow(t *testing.T) {
	withCacheBudget(t, 1, 1, 1)
	var rels []string
	for i := 0; i < 60; i++ {
		rels = append(rels, fmt.Sprintf("d%02d/%s.md", i, strings.Repeat("r", 100)))
	}
	repo := branchRepo(t, []string{"task/full-aaaaaa"}, rels)
	index := filepath.Join(t.TempDir(), "artifacts.jsonl")
	want := map[string]bool{}
	for _, rel := range rels {
		fact := &DeliverableFact{ID: NewDeliverableID(), Task: "1", Branch: "task/full-aaaaaa", Rel: rel, Authored: true, File: &DeliverableFile{Bytes: 1, SHA256: strings.Repeat("0", 64)}}
		RecordArtifact(index, Artifact{Path: filepath.Join(repo, filepath.FromSlash(rel)), Session: "agent", Title: filepath.Base(rel), Kind: DeliverableMarkdown, Created: time.Now(), Deliver: fact})
		want[fact.ID] = true
	}
	seen := map[string]bool{}
	for cursor, pages := "", 0; ; pages++ {
		if pages > 100 {
			t.Fatal("the cursor never ended")
		}
		deadline := time.Now().Add(2 * time.Second)
		ctx, cancel := context.WithDeadline(context.Background(), deadline)
		cache := NewDeliverSourceCache()
		page, err := ListDeliverables(index, DeliverableQuery{}, cursor, 25, deadline, 0, func(row DeliverableRow) (int, bool, error) {
			source := ResolveDeliverableSourceWith(ctx, cache, row, []string{repo}, repo)
			if source.Undecided && ctx.Err() != nil {
				return 0, false, ErrDeliverPageEnd
			}
			if !source.OnBranch() {
				return 0, false, fmt.Errorf("%s is %s", row.Fact.Rel, source.Live.State)
			}
			return 0, true, nil
		})
		cancel()
		if errors.Is(err, ErrMediaBusy) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		if bytes, slots := cache.Retained(); bytes != 0 || slots != 0 {
			t.Fatalf("a full cache kept %d bytes in %d slots", bytes, slots)
		}
		for _, row := range page.Rows {
			if seen[row.ID] {
				t.Fatalf("%s listed twice", row.ID)
			}
			seen[row.ID] = true
		}
		if page.Next == "" {
			break
		}
		cursor = page.Next
	}
	if len(seen) != len(want) {
		t.Fatalf("found %d of %d rows", len(seen), len(want))
	}
}
