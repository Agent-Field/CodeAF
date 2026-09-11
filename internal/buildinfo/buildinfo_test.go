package buildinfo

import (
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"
	"time"
)

func TestStampedBuildFormatsEveryKnownFact(t *testing.T) {
	previousLocal := time.Local
	time.Local = time.FixedZone("EDT", -4*60*60)
	t.Cleanup(func() { time.Local = previousLocal })

	info := resolve(
		"1265feda",
		"true",
		"2026-08-27T17:28:00Z",
		func() (*debug.BuildInfo, bool) {
			t.Fatal("a link-time stamp fell through to the Go build record")
			return nil, false
		},
	)
	if got := info.String(); got != "1265feda (dirty) built 2026-08-27 13:28" {
		t.Fatalf("String() = %q", got)
	}
}

func TestUnstampedBuildFallsBackToTheGoBuildRecord(t *testing.T) {
	info := resolve("", "", "", func() (*debug.BuildInfo, bool) {
		return &debug.BuildInfo{
			Main: debug.Module{Version: "(devel)"},
			Settings: []debug.BuildSetting{
				{Key: "vcs.revision", Value: "abcdef0123456789"},
				{Key: "vcs.modified", Value: "true"},
			},
		}, true
	})
	if got := info.String(); got != "abcdef01 (dirty)" {
		t.Fatalf("String() = %q, want the Go build record", got)
	}
}

func TestUnknownBuildSaysDevWithoutInventingAStamp(t *testing.T) {
	info := resolve("", "", "", func() (*debug.BuildInfo, bool) { return nil, false })
	if got := info.String(); got != "dev" {
		t.Fatalf("String() = %q, want dev", got)
	}
}

func TestDetectorNoticesEachNewerExecutableOnce(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "aforge")
	if err := os.WriteFile(path, []byte("old"), 0o700); err != nil {
		t.Fatal(err)
	}
	started := time.Date(2026, 8, 27, 13, 20, 0, 0, time.Local)
	setMtime(t, path, started.Add(-time.Minute))
	detector := NewDetector(path, started)

	if got := detector.Notice(); got != "" {
		t.Fatalf("the executable present at process start produced %q", got)
	}

	first := started.Add(8 * time.Minute)
	setMtime(t, path, first)
	if got := detector.Notice(); !strings.Contains(got, first.Format("15:04")) {
		t.Fatalf("the replacement produced %q", got)
	}
	if got := detector.Notice(); got != "" {
		t.Fatalf("the same replacement was announced twice: %q", got)
	}

	second := first.Add(17 * time.Minute)
	setMtime(t, path, second)
	if got := detector.Notice(); !strings.Contains(got, second.Format("15:04")) {
		t.Fatalf("the next replacement produced %q", got)
	}
}

func setMtime(t *testing.T, path string, stamp time.Time) {
	t.Helper()
	if err := os.Chtimes(path, stamp, stamp); err != nil {
		t.Fatal(err)
	}
}

func TestBuildIdentityDistinguishesRebuildsWithinOneDisplayMinute(t *testing.T) {
	previous := current
	t.Cleanup(func() { current = previous })
	current = Info{Revision: "same-revision", Dirty: true, BuiltAt: time.Date(2026, 9, 8, 20, 0, 1, 0, time.UTC)}
	first, display := Identity(), String()
	current.BuiltAt = current.BuiltAt.Add(time.Second)
	if String() != display {
		t.Fatal("fixture does not share a display minute")
	}
	if Identity() == first {
		t.Fatal("a dirty rebuild retained the prior engine identity")
	}
}

// TWO BUILDS OF ONE COMMIT ARE ONE ENGINE. The window that met the engine
// thirteen seconds after it was linked was told it had met an older aforge, and
// the difference it read was one nobody had made (#730).
func TestBuildIdentityKeepsOneCleanRevisionAcrossRebuilds(t *testing.T) {
	previous := current
	t.Cleanup(func() { current = previous })
	current = Info{Revision: "same-revision", BuiltAt: time.Date(2026, 9, 8, 20, 0, 1, 0, time.UTC)}
	first := Identity()
	current.BuiltAt = current.BuiltAt.Add(13 * time.Second)
	if Identity() != first {
		t.Fatalf("a rebuild of one commit became another engine: %s then %s", first, Identity())
	}
}

// And another commit is another engine, which is the whole reason the question
// is asked before a window is handed to a host.
func TestBuildIdentityDistinguishesCleanRevisions(t *testing.T) {
	previous := current
	t.Cleanup(func() { current = previous })
	current = Info{Revision: "first-revision", BuiltAt: time.Date(2026, 9, 8, 20, 0, 1, 0, time.UTC)}
	first := Identity()
	current.Revision = "second-revision"
	if Identity() == first {
		t.Fatal("another clean revision retained the prior engine identity")
	}
}

// A build with no source to name keeps the moment it was made, because that is
// the only thing left that tells one of them from the next.
func TestBuildIdentityDistinguishesUnstampedRebuilds(t *testing.T) {
	previous := current
	t.Cleanup(func() { current = previous })
	current = Info{BuiltAt: time.Date(2026, 9, 8, 20, 0, 1, 0, time.UTC)}
	first := Identity()
	current.BuiltAt = current.BuiltAt.Add(time.Second)
	if Identity() == first {
		t.Fatal("an unstamped rebuild retained the prior engine identity")
	}
}
