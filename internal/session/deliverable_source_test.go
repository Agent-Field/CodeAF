package session

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const sourceBody = "# authored\n"

// mergedWorkerRow is a worker's row whose branch is gone, with the authored
// bytes at its checkout path: what a merge leaves when no settling ran.
func mergedWorkerRow(t *testing.T) (DeliverableRow, string, string) {
	t.Helper()
	repo := canonicalFile(t.TempDir())
	gitT(t, repo, "init", "-q", "-b", "main")
	path := filepath.Join(repo, "plan.md")
	os.WriteFile(path, []byte(sourceBody), 0o644)
	gitT(t, repo, "add", ".")
	gitT(t, repo, "commit", "-q", "-m", "landed")
	sum := sha256.Sum256([]byte(sourceBody))
	index := filepath.Join(t.TempDir(), "artifacts.jsonl")
	fact := &DeliverableFact{ID: NewDeliverableID(), Task: "4", Branch: "task/gone-aaaaaa", Rel: "plan.md", Authored: true,
		File: &DeliverableFile{Bytes: int64(len(sourceBody)), SHA256: hex.EncodeToString(sum[:])}}
	RecordArtifact(index, Artifact{Path: path, Session: "agent", Title: "plan.md", Kind: DeliverableMarkdown, Created: time.Now(), Deliver: fact})
	row, err := FindDeliverableAfter(t, index, fact.ID)
	if err != nil {
		t.Fatal(err)
	}
	return row, repo, index
}

// The authority's word is checked again on the bytes Keep copied: a vouch that
// turns out wrong publishes nothing.
func TestAWrongVouchIsNeverPublished(t *testing.T) {
	row, repo, index := mergedWorkerRow(t)
	path := row.Artifact.Path
	os.WriteFile(path, []byte(strings.ToUpper(sourceBody)), 0o644) // same size, other bytes
	live := ResolveMedia(MediaRow{Fact: MediaFact{Origin: MediaGenerated, Canonical: path}}, []string{repo})
	rememberDigest(path+"\x00"+live.Version+"\x00"+row.Fact.File.SHA256, digestSame) // a wrong verdict
	defer func() { deliverDigests.Lock(); deliverDigests.seen = map[string]bool{}; deliverDigests.Unlock() }()
	if got := ResolveDeliverableSource(context.Background(), row, []string{repo}, repo); got.Live.State != MediaAvailable {
		t.Fatalf("the primed verdict was not used: %s", got.Live.State)
	}
	dest := t.TempDir()
	_, _, err := KeepDeliverable(context.Background(), index, row, []string{repo}, t.TempDir(), KeepRequest{Dir: dest, Workspace: repo})
	if !errors.Is(err, ErrKeepChanged) {
		t.Fatalf("a copy of other bytes: %v", err)
	}
	if entries, _ := os.ReadDir(dest); len(entries) != 0 {
		t.Fatalf("other bytes were published: %v", entries)
	}
}

// A check that cannot finish proves nothing: a caller gone, or every permit
// taken past the deadline, leaves nothing to open; a verdict does not outlive
// a rewrite of the file.
func TestACheckoutCheckThatCannotFinishVouchesForNothing(t *testing.T) {
	row, repo, _ := mergedWorkerRow(t)
	roots := []string{repo}
	gone, cancel := context.WithCancel(context.Background())
	cancel()
	if got := ResolveDeliverableSource(gone, row, roots, repo); got.Live.State == MediaAvailable || got.Live.Path != "" {
		t.Fatalf("a cancelled check vouched: %+v", got.Live)
	}
	for i := 0; i < cap(deliverCheckPermits); i++ {
		deliverCheckPermits <- struct{}{}
	}
	old := deliverSourceMax
	deliverSourceMax = 50 * time.Millisecond
	got := ResolveDeliverableSource(context.Background(), row, roots, repo)
	deliverSourceMax = old
	for i := 0; i < cap(deliverCheckPermits); i++ {
		<-deliverCheckPermits
	}
	if got.Live.State == MediaAvailable || got.Live.Path != "" {
		t.Fatalf("a check with no permit vouched: %+v", got.Live)
	}
	if got := ResolveDeliverableSource(context.Background(), row, roots, repo); got.Live.State != MediaAvailable || got.Expect != row.Fact.File.SHA256 {
		t.Fatalf("the authored bytes: %+v", got)
	}
	os.WriteFile(row.Artifact.Path, []byte(strings.ToUpper(sourceBody)), 0o644)
	if got := ResolveDeliverableSource(context.Background(), row, roots, repo); got.Live.State != MediaChanged || got.Live.Path != "" {
		t.Fatalf("a remembered verdict outlived a rewrite: %+v", got.Live)
	}
}
