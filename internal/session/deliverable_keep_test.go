package session

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func keepFixture(t *testing.T) (index string, row DeliverableRow, roots []string, session string) {
	t.Helper()
	agent, workspace, idx := deliverableAgent(t, true)
	writeNewFile(t, agent, "plan.md", "# plan\nbody\n")
	rows := listDeliverables(t, idx, DeliverableQuery{}, 10)
	if len(rows) != 1 {
		t.Fatalf("rows %d", len(rows))
	}
	return idx, rows[0], []string{workspace, agent.config.Place.Work(), agent.config.Place.Artifacts()}, agent.journalID()
}

func TestKeepCopiesOnceNeverOverwritesAndRecordsAfterTheCopy(t *testing.T) {
	index, row, roots, _ := keepFixture(t)
	dest := t.TempDir()
	home := t.TempDir()
	updated, path, err := KeepDeliverable(context.Background(), index, row, roots, home, KeepRequest{Dir: dest})
	if err != nil {
		t.Fatal(err)
	}
	want, _ := os.ReadFile(row.Artifact.Path)
	got, err := os.ReadFile(path)
	if err != nil || string(got) != string(want) || filepath.Dir(path) != canonicalFile(dest) {
		t.Fatalf("copy %q %v", path, err)
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", info.Mode())
	}
	if updated.Fact.State != DeliverKept || updated.Fact.Kept == nil || updated.Fact.Kept.Path != path || updated.Fact.Kept.How != KeptCopied || updated.Fact.Kept.SHA256 == "" {
		t.Fatalf("not recorded: %+v", updated.Fact)
	}
	if left, _ := filepath.Glob(filepath.Join(dest, ".codeaf-keep-*")); len(left) != 0 {
		t.Fatalf("temp left behind: %v", left)
	}
	// Second Keep of a kept deliverable is refused.
	if _, _, err := KeepDeliverable(context.Background(), index, row, roots, home, KeepRequest{Dir: t.TempDir()}); !errors.Is(err, ErrKeepAlready) {
		t.Fatalf("second keep: %v", err)
	}
	// The source is untouched.
	if after, _ := os.ReadFile(row.Artifact.Path); string(after) != string(want) {
		t.Fatal("source changed")
	}
}

func TestKeepRefusesACollisionWithoutWritingOrRecording(t *testing.T) {
	index, row, roots, session := keepFixture(t)
	dest := t.TempDir()
	taken := filepath.Join(dest, "plan.md")
	os.WriteFile(taken, []byte("mine"), 0o600)
	_, _, err := KeepDeliverable(context.Background(), index, row, roots, t.TempDir(), KeepRequest{Dir: dest})
	var exists *KeepExistsError
	if !errors.As(err, &exists) {
		t.Fatalf("want exists, got %v", err)
	}
	if b, _ := os.ReadFile(taken); string(b) != "mine" {
		t.Fatal("overwrote")
	}
	if left, _ := filepath.Glob(filepath.Join(dest, ".codeaf-keep-*")); len(left) != 0 {
		t.Fatalf("temp left: %v", left)
	}
	got, _ := FindDeliverable(index, session, row.ID, time.Time{})
	if got.Fact.State == DeliverKept {
		t.Fatal("false Keep recorded")
	}
	// A different name works.
	if _, p, err := KeepDeliverable(context.Background(), index, row, roots, t.TempDir(), KeepRequest{Dir: dest, Name: "plan-2.md"}); err != nil || !strings.HasSuffix(p, "plan-2.md") {
		t.Fatalf("rename: %v", err)
	}
}

func TestKeepRefusesBadDestinationsNamesAndChangedFiles(t *testing.T) {
	index, row, roots, _ := keepFixture(t)
	home := t.TempDir()
	cases := []struct {
		name string
		req  KeepRequest
		want error
	}{
		{"relative", KeepRequest{Dir: "relative/dir"}, ErrKeepDest},
		{"missing", KeepRequest{Dir: filepath.Join(t.TempDir(), "nope")}, ErrKeepDest},
		{"home", KeepRequest{Dir: home}, ErrKeepDest},
		{"traversal name", KeepRequest{Dir: t.TempDir(), Name: "../x.md"}, ErrKeepName},
		{"stale version", KeepRequest{Dir: t.TempDir(), ExpectVersion: "file:1:1:1:1:1"}, ErrKeepChanged},
	}
	for _, c := range cases {
		if _, _, err := KeepDeliverable(context.Background(), index, row, roots, home, c.req); !errors.Is(err, c.want) {
			t.Errorf("%s: %v", c.name, err)
		}
	}
	// A symlink to the home folder resolves first and is refused.
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(home, link); err == nil {
		if _, _, err := KeepDeliverable(context.Background(), index, row, roots, home, KeepRequest{Dir: link}); !errors.Is(err, ErrKeepDest) {
			t.Errorf("symlinked home: %v", err)
		}
	}
	// An edited file needs the version the person saw.
	os.WriteFile(row.Artifact.Path, []byte("# plan\nedited later\n"), 0o600)
	if _, _, err := KeepDeliverable(context.Background(), index, row, roots, home, KeepRequest{Dir: t.TempDir()}); !errors.Is(err, ErrKeepChanged) {
		t.Errorf("changed without expect: %v", err)
	}
	live := ResolveDeliverable(row, roots)
	if live.State != MediaChanged {
		t.Fatalf("live %s", live.State)
	}
	if _, _, err := KeepDeliverable(context.Background(), index, row, roots, home, KeepRequest{Dir: t.TempDir(), ExpectVersion: live.Version}); err != nil {
		t.Errorf("changed with the version seen: %v", err)
	}
}

func TestKeepCancelAndMissingSourceLeaveNothing(t *testing.T) {
	index, row, roots, _ := keepFixture(t)
	dest := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := KeepDeliverable(ctx, index, row, roots, t.TempDir(), KeepRequest{Dir: dest}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
	entries, _ := os.ReadDir(dest)
	if len(entries) != 0 {
		t.Fatalf("partial left: %v", entries)
	}
	os.Remove(row.Artifact.Path)
	if _, _, err := KeepDeliverable(context.Background(), index, row, roots, t.TempDir(), KeepRequest{Dir: dest}); !errors.Is(err, ErrKeepSource) {
		t.Fatalf("gone: %v", err)
	}
}

// A kept copy outlives the folder it came from: the original swept away, the
// deliverable is still available from the copy, judged by the copy's identity.
func TestAKeptCopyOutlivesItsSource(t *testing.T) {
	index, row, roots, session := keepFixture(t)
	dest := t.TempDir()
	updated, path, err := KeepDeliverable(context.Background(), index, row, roots, t.TempDir(), KeepRequest{Dir: dest})
	if err != nil {
		t.Fatal(err)
	}
	if live := ResolveDeliverable(updated, roots); live.State != MediaAvailable || live.Copy {
		t.Fatalf("with the source present: %+v", live)
	}
	if err := os.Remove(row.Artifact.Path); err != nil {
		t.Fatal(err)
	}
	got, _ := FindDeliverable(index, session, row.ID, time.Time{})
	live := ResolveDeliverable(got, roots)
	if live.State != MediaAvailable || !live.Copy || live.Path != path {
		t.Fatalf("source swept, copy intact: %+v", live)
	}
	os.WriteFile(path, []byte("the person edited their copy"), 0o600)
	if live := ResolveDeliverable(got, roots); live.State != MediaChanged || !live.Copy {
		t.Fatalf("edited copy: %+v", live)
	}
	os.Remove(path)
	if live := ResolveDeliverable(got, roots); live.State != MediaMissing || live.Copy {
		t.Fatalf("both gone: %+v", live)
	}
}

// A file swapped in between the check and the open is not copied as the one
// that was checked.
func TestKeepRefusesAFileSwappedInAfterTheCheck(t *testing.T) {
	index, row, roots, _ := keepFixture(t)
	dest := t.TempDir()
	keepBeforeOpen = func(path string) {
		other := path + ".other"
		os.WriteFile(other, []byte("# someone else's file\n"), 0o600)
		os.Rename(other, path)
	}
	defer func() { keepBeforeOpen = func(string) {} }()
	if _, _, err := KeepDeliverable(context.Background(), index, row, roots, t.TempDir(), KeepRequest{Dir: dest}); !errors.Is(err, ErrKeepChanged) {
		t.Fatalf("swapped source copied: %v", err)
	}
	if entries, _ := os.ReadDir(dest); len(entries) != 0 {
		t.Fatalf("left behind: %v", entries)
	}
}

// A name taken over after publication is another party's file: it is neither
// recorded as ours nor removed.
func TestKeepNeverRemovesAFileItDidNotPublish(t *testing.T) {
	index, row, roots, session := keepFixture(t)
	dest := t.TempDir()
	keepAfterPublish = func(final string) {
		other := final + ".theirs"
		os.WriteFile(other, []byte("theirs"), 0o600)
		os.Rename(other, final)
	}
	defer func() { keepAfterPublish = func(string) {} }()
	if _, _, err := KeepDeliverable(context.Background(), index, row, roots, t.TempDir(), KeepRequest{Dir: dest}); err == nil {
		t.Fatal("a taken-over name was recorded as kept")
	}
	if b, _ := os.ReadFile(filepath.Join(dest, "plan.md")); string(b) != "theirs" {
		t.Fatalf("someone else's file was touched: %q", b)
	}
	if got, _ := FindDeliverable(index, session, row.ID, time.Time{}); got.Fact.State == DeliverKept {
		t.Fatal("false Keep recorded")
	}
}

// An append that reports failure is ambiguous. The index is read before the
// copy is judged: committed means success, absent means the copy this call made
// is removed, and unreadable means it stays.
func TestKeepReconcilesAnAmbiguousAppend(t *testing.T) {
	t.Cleanup(func() {
		if refs, keys := keepReservations(&deliverKeepAdmission); refs != 0 || keys != 0 {
			t.Errorf("uncertain Keep retained admission %d/%d", refs, keys)
		}
	})
	defer func() { keepAppend = appendDeliverableUpdate }()
	// 1. the row reached the disk, then the call said it had not
	index, row, roots, session := keepFixture(t)
	dest := t.TempDir()
	keepAppend = func(idx string, base DeliverableRow, change func(*DeliverableFact)) (DeliverableRow, bool) {
		appendDeliverableUpdate(idx, base, change)
		return DeliverableRow{}, false
	}
	updated, path, err := KeepDeliverable(context.Background(), index, row, roots, t.TempDir(), KeepRequest{Dir: dest})
	if err != nil || updated.Fact.State != DeliverKept || updated.Fact.Kept.Path != path {
		t.Fatalf("a committed row was treated as lost: %v %+v", err, updated.Fact)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("accepted data was deleted")
	}
	// 2. the row is definitely not there: the copy goes
	index, row, roots, session = keepFixture(t)
	dest = t.TempDir()
	keepAppend = func(string, DeliverableRow, func(*DeliverableFact)) (DeliverableRow, bool) {
		return DeliverableRow{}, false
	}
	if _, _, err := KeepDeliverable(context.Background(), index, row, roots, t.TempDir(), KeepRequest{Dir: dest}); !errors.Is(err, ErrKeepRecord) {
		t.Fatalf("unrecorded: %v", err)
	}
	if entries, _ := os.ReadDir(dest); len(entries) != 0 {
		t.Fatalf("unrecorded copy left: %v", entries)
	}
	if got, _ := FindDeliverable(index, session, row.ID, time.Time{}); got.Fact.State == DeliverKept {
		t.Fatal("false Keep")
	}
	// 3. the index cannot be read to find out: the copy stays, named
	index, row, roots, _ = keepFixture(t)
	dest = t.TempDir()
	keepAppend = func(idx string, _ DeliverableRow, _ func(*DeliverableFact)) (DeliverableRow, bool) {
		os.Remove(idx)
		os.Mkdir(idx, 0o700)
		return DeliverableRow{}, false
	}
	_, _, err = KeepDeliverable(context.Background(), index, row, roots, t.TempDir(), KeepRequest{Dir: dest})
	if !errors.Is(err, ErrKeepUnconfirmed) {
		t.Fatalf("unreadable index: %v", err)
	}
	if b, err := os.ReadFile(filepath.Join(dest, "plan.md")); err != nil || len(b) == 0 {
		t.Fatalf("a copy the index may have accepted was deleted: %v", err)
	}
	if strings.Contains(err.Error(), dest) {
		t.Fatalf("the unconfirmed outcome names a path: %v", err)
	}
	// 4. the row is not there and the copy cannot be taken back: unconfirmed,
	// never the plain failure a caller would retry into a second copy
	index, row, roots, _ = keepFixture(t)
	dest = t.TempDir()
	keepAppend = func(string, DeliverableRow, func(*DeliverableFact)) (DeliverableRow, bool) {
		os.Chmod(dest, 0o500)
		return DeliverableRow{}, false
	}
	defer os.Chmod(dest, 0o700)
	if _, _, err := KeepDeliverable(context.Background(), index, row, roots, t.TempDir(), KeepRequest{Dir: dest}); !errors.Is(err, ErrKeepUnconfirmed) {
		t.Fatalf("a copy that could not be removed was reported as removed: %v", err)
	}
}
