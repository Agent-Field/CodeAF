package session

import (
	"path/filepath"
	"testing"
	"time"
)

func metadataTitleAgent(t *testing.T) (*Agent, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "0123456789abcdef")
	a, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
		c.Place = Place{Dir: dir, Workspace: c.Workspace}
	})
	a.mu.Lock()
	a.stampUserLocked("diagnose parser migration failures")
	a.mu.Unlock()
	return a, dir
}

func waitMetaPatch(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("metadata transaction failed to settle")
	}
}

// The old spend snapshot is prepared first, then held while a complete title
// transaction lands. Releasing the spend must preserve the new title, whichever
// metadata fields existed when that spend was prepared.
func TestAnOldSpendSnapshotCannotOverwriteAnEarlyTitle(t *testing.T) {
	a, dir := metadataTitleAgent(t)
	ready, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	go func() {
		snapshot := a.metaSnapshot()
		close(ready)
		<-release
		a.writeSpendSnapshot(dir, snapshot, .25, 140)
		close(done)
	}()
	waitMetaPatch(t, ready)
	a.stampTitle("parser migration failures")
	named, err := LoadMeta(dir)
	if err != nil || named.Title != "parser migration failures" {
		close(release)
		waitMetaPatch(t, done)
		t.Fatalf("title did not commit before old spend resumed: %+v %v", named, err)
	}
	close(release)
	waitMetaPatch(t, done)
	meta, err := LoadMeta(dir)
	if err != nil || meta.Title != named.Title || meta.SpentUSD != .25 || meta.Tokens != 140 {
		t.Fatalf("old spend lost title or cost: %+v %v", meta, err)
	}
}

// An earned title is the conversation's name rather than the first-message
// placeholder. Metadata keeps the journal's full 80-byte guard through a
// concurrent usage stamp and the next open, so Home and the reopened session
// cannot disagree about a valid name merely because it passed 56 bytes.
func TestALongEarnedTitleSurvivesMetadataUsageAndReopen(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "0123456789abcdef")
	path := filepath.Join(dir, "transcript.jsonl")
	a, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
		c.SessionFile = path
		c.Place = Place{Dir: dir, Workspace: c.Workspace}
	})
	title := "github organization star history across repositories and notable followers"
	short := "star history"
	if len(title) <= metaTitleLimit || len(title) > titleLimit {
		t.Fatalf("fixture title length = %d, want >%d and <=%d", len(title), metaTitleLimit, titleLimit)
	}

	stale := a.metaSnapshot()
	if !a.setTitleIfUnnamed(title, short) {
		t.Fatal("the unnamed conversation refused its earned title")
	}
	a.writeSpendSnapshot(dir, stale, .25, 140)
	meta, err := LoadMeta(dir)
	if err != nil || meta.Title != title || meta.ShortTitle != short || meta.SpentUSD != .25 || meta.Tokens != 140 {
		t.Fatalf("usage stamp clipped or lost the earned title pair: %+v %v", meta, err)
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := newAgent(Config{Workspace: a.config.Workspace, Model: "test/model", System: "SYSTEM", SessionFile: path, Place: Place{Dir: dir, Workspace: a.config.Workspace}}, &scriptedCompleter{})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	if reopened.Title() != title || reopened.ShortTitle() != short {
		t.Fatalf("reopened title pair = %q / %q", reopened.Title(), reopened.ShortTitle())
	}
}

// A transaction may already own metaMu when recordUserLocked owns a.mu. It
// must finish without reaching back for a.mu, or the two locks deadlock.
func TestAMetadataPatchFinishesWhileAUserStampOwnsTheAgentLock(t *testing.T) {
	a, dir := metadataTitleAgent(t)
	snapshot := a.metaSnapshot()
	writing, release, firstDone := make(chan struct{}), make(chan struct{}), make(chan struct{})
	go func() {
		a.updateMeta(dir, snapshot, func(meta *Meta) {
			close(writing)
			<-release
			meta.Title = "parser migration failures"
		})
		close(firstDone)
	}()
	waitMetaPatch(t, writing)
	userLocked, userDone := make(chan struct{}), make(chan struct{})
	go func() {
		a.mu.Lock()
		close(userLocked)
		a.stampUserLocked("include the failing integration suite")
		a.mu.Unlock()
		close(userDone)
	}()
	waitMetaPatch(t, userLocked)
	close(release)
	waitMetaPatch(t, firstDone)
	waitMetaPatch(t, userDone)
	meta, err := LoadMeta(dir)
	if err != nil || meta.Title != "parser migration failures" || meta.LastUserAt.IsZero() {
		t.Fatalf("serialized user stamp lost committed title: %+v %v", meta, err)
	}
}

func TestAnAcceptedUserMessageStillRefreshesTheStoredModel(t *testing.T) {
	a, dir := metadataTitleAgent(t)
	a.SetModel("test/replacement-model")
	a.mu.Lock()
	a.stampUserLocked("check the integration regression using this model")
	a.mu.Unlock()
	meta, err := LoadMeta(dir)
	if err != nil || meta.Model != "test/replacement-model" {
		t.Fatalf("new user message retained the previous model: %+v %v", meta, err)
	}
}

func TestOldNamingAndSpendSnapshotsPreserveAnAnchoredWorkspace(t *testing.T) {
	a, dir := metadataTitleAgent(t)
	a.mu.Lock()
	a.config.Place.Owned = true
	a.mu.Unlock()
	meta, _ := LoadMeta(dir)
	meta.Owned = true
	if err := SaveMeta(dir, meta); err != nil {
		t.Fatal(err)
	}
	stale := a.metaSnapshot()
	workspace := t.TempDir()
	anchored, err := a.AnchorWorkspace(workspace)
	if err != nil {
		t.Fatal(err)
	}
	a.updateMeta(dir, stale, func(meta *Meta) { meta.Title = "parser migration failures" })
	a.writeSpendSnapshot(dir, stale, .5, 270)
	meta, err = LoadMeta(dir)
	if err != nil || meta.Owned || meta.Workspace != anchored || meta.Title != "parser migration failures" || meta.Tokens != 270 {
		t.Fatalf("old snapshot undid workspace anchoring: %+v %v", meta, err)
	}
}

func TestNamingSpendAndAnchoringShareTheirMetadataTransaction(t *testing.T) {
	a, dir := metadataTitleAgent(t)
	a.mu.Lock()
	a.config.Place.Owned = true
	a.mu.Unlock()
	meta, _ := LoadMeta(dir)
	meta.Owned = true
	if err := SaveMeta(dir, meta); err != nil {
		t.Fatal(err)
	}
	start, named, spent := make(chan struct{}), make(chan struct{}), make(chan struct{})
	go func() { <-start; a.stampTitle("parser migration failures"); close(named) }()
	go func() { <-start; a.writeSpend(dir, .5, 270); close(spent) }()
	close(start)
	anchored, err := a.AnchorWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	waitMetaPatch(t, named)
	waitMetaPatch(t, spent)
	meta, err = LoadMeta(dir)
	if err != nil || meta.Owned || meta.Workspace != anchored || meta.Title != "parser migration failures" || meta.Tokens != 270 {
		t.Fatalf("concurrent metadata patch lost a field: %+v %v", meta, err)
	}
}
