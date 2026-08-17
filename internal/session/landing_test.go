package session

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// These are the tests for Decision 26's second half: NOTHING OF OURS LIVES IN
// THE PERSON'S FOLDER. Every one of them is a pair — what a session with a
// folder does, and what a session without one still does — because the whole
// design of the seam is that the second answer never changed.

// newPlace is one session folder under a temp directory, borrowed or owned.
func newPlace(t *testing.T, owned bool) Place {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "0123456789abcdef")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	place := Place{Dir: dir, Owned: owned}
	if owned {
		place.Workspace = place.Work()
		if err := os.MkdirAll(place.Work(), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	return place
}

// ── droppings ───────────────────────────────────────────────────────────────

// A job log is a dropping. With a folder it lands in logs/jobs; without one it
// lands exactly where it always did, in the workspace's dot directory.
func TestJobLogsFollowTheSessionFolder(t *testing.T) {
	workspace := t.TempDir()
	place := newPlace(t, false)

	registry := newJobRegistry(workspace, place, nil)
	job, err := registry.newJob("a build", jobKindBash)
	if err != nil {
		t.Fatalf("newJob: %v", err)
	}
	defer job.sink.close()

	if want := filepath.Join(place.Logs(), "jobs", "1.log"); job.logPath != want {
		t.Fatalf("job log = %q, want %q", job.logPath, want)
	}
	if _, err := os.Stat(filepath.Join(workspace, ".aforge-v3")); !os.IsNotExist(err) {
		t.Fatalf("the session littered the workspace: %v", err)
	}
}

func TestJobLogsKeepTheLegacyPathWithoutAFolder(t *testing.T) {
	workspace := t.TempDir()
	registry := newJobRegistry(workspace, Place{}, nil)
	job, err := registry.newJob("a build", jobKindBash)
	if err != nil {
		t.Fatalf("newJob: %v", err)
	}
	defer job.sink.close()

	if want := filepath.Join(workspace, ".aforge-v3", "jobs", "1.log"); job.logPath != want {
		t.Fatalf("job log = %q, want %q", job.logPath, want)
	}
}

// The id-claim loop is the reason two windows never share a log, and moving the
// directory must not have moved that: two registries on ONE session folder still
// take one name each.
func TestTheIDClaimSurvivesTheMoveIntoTheFolder(t *testing.T) {
	workspace := t.TempDir()
	place := newPlace(t, false)
	first, second := newJobRegistry(workspace, place, nil), newJobRegistry(workspace, place, nil)

	one, err := first.newJob("the first window's build", jobKindBash)
	if err != nil {
		t.Fatalf("newJob: %v", err)
	}
	defer one.sink.close()
	two, err := second.newJob("the second window's build", jobKindBash)
	if err != nil {
		t.Fatalf("newJob: %v", err)
	}
	defer two.sink.close()

	if one.logPath == two.logPath {
		t.Fatalf("two windows claimed one log: %s", one.logPath)
	}
	if one.id == two.id {
		t.Fatalf("two windows claimed job id %d", one.id)
	}
}

// A stub's bytes follow the folder too, and the LINE the model reads follows
// the bytes: a path out of the workspace is named absolutely, because a
// relative one would be a path nothing can open.
func TestStubBytesFollowTheSessionFolder(t *testing.T) {
	workspace := t.TempDir()
	place := newPlace(t, false)

	path, err := writeStub(place, workspace, strings.Repeat("output\n", 400))
	if err != nil {
		t.Fatalf("writeStub: %v", err)
	}
	if !filepath.IsAbs(path) {
		t.Fatalf("stub path %q is relative to a workspace it is not inside", path)
	}
	if filepath.Dir(filepath.FromSlash(path)) != filepath.Join(place.Logs(), "stubs") {
		t.Fatalf("stub landed at %q, want it under %q", path, place.Logs())
	}
	if _, err := os.Stat(filepath.FromSlash(path)); err != nil {
		t.Fatalf("the stub line names a path with nothing at it: %v", err)
	}
	if _, err := os.Stat(filepath.Join(workspace, ".aforge-v3")); !os.IsNotExist(err) {
		t.Fatalf("the session littered the workspace: %v", err)
	}
}

// And without a folder the stub is still named the way the model's own read tool
// takes it: relative to the workspace.
func TestStubBytesKeepTheLegacyPathWithoutAFolder(t *testing.T) {
	workspace := t.TempDir()
	path, err := writeStub(Place{}, workspace, strings.Repeat("output\n", 400))
	if err != nil {
		t.Fatalf("writeStub: %v", err)
	}
	if !strings.HasPrefix(path, ".aforge-v3/stubs/") {
		t.Fatalf("stub path = %q, want it under .aforge-v3/stubs", path)
	}
	if _, err := os.Stat(filepath.Join(workspace, filepath.FromSlash(path))); err != nil {
		t.Fatalf("the stub line names a path with nothing at it: %v", err)
	}
}

// A compaction page is a dropping as well, and its journal reference is
// absolute in both layouts because a RESUME opens it from wherever it stands.
func TestFramePagesFollowTheSessionFolder(t *testing.T) {
	pages, err := renderFrames("a session", []string{"the first half"}, framesMaxPages)
	if err != nil {
		t.Fatalf("renderFrames: %v", err)
	}
	workspace := t.TempDir()
	place := newPlace(t, false)

	ref, err := writeFrame(place, workspace, pages[0])
	if err != nil {
		t.Fatalf("writeFrame: %v", err)
	}
	if filepath.Dir(filepath.FromSlash(ref.Path)) != filepath.Join(place.Logs(), "frames") {
		t.Fatalf("page landed at %q, want it under %q", ref.Path, place.Logs())
	}
	if _, err := os.Stat(filepath.FromSlash(ref.Path)); err != nil {
		t.Fatalf("the journal names a page with nothing at it: %v", err)
	}
	if _, err := os.Stat(filepath.Join(workspace, ".aforge-v3")); !os.IsNotExist(err) {
		t.Fatalf("the session littered the workspace: %v", err)
	}

	legacy, err := writeFrame(Place{}, workspace, pages[0])
	if err != nil {
		t.Fatalf("writeFrame: %v", err)
	}
	if want := filepath.Join(workspace, ".aforge-v3", "frames"); filepath.Dir(filepath.FromSlash(legacy.Path)) != want {
		t.Fatalf("page landed at %q, want it under %q", legacy.Path, want)
	}
}

// ── deliverables ────────────────────────────────────────────────────────────

// A BORROWED session paints into its own artifacts/ and never into the
// repository it was opened in — that is the whole of "the person's repo is
// borrowed, never littered".
func TestAPaintedPictureLandsInArtifactsForABorrowedSession(t *testing.T) {
	picture := pngOfSize(t, 8, 6)
	painter := &scriptedMedia{base64: base64.StdEncoding.EncodeToString(picture), mediaType: "image/png"}
	place := newPlace(t, false)
	index := filepath.Join(t.TempDir(), "artifacts.jsonl")

	agent, workspace := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.Media = painter
		config.MediaModel = mediaModels(map[string]string{modalityImage: "paint/model"})
		config.Place = place
		config.ArtifactsIndex = index
	})
	if result, isError := runTool(t, agent, "generate_image", `{"prompt":"Sunset over the Harbour"}`); isError {
		t.Fatalf("generate_image failed: %s", result)
	}

	entries, err := os.ReadDir(place.Artifacts())
	if err != nil || len(entries) != 1 {
		t.Fatalf("artifacts directory = %v, %v; want one picture", entries, err)
	}
	if _, err := os.Stat(filepath.Join(workspace, ".aforge-v3")); !os.IsNotExist(err) {
		t.Fatalf("the session painted into the person's repository: %v", err)
	}

	// And the row: a deliverable nobody can find again is not a deliverable.
	rows := ReadArtifacts(index)
	if len(rows) != 1 {
		t.Fatalf("artifact rows = %d, want 1", len(rows))
	}
	if rows[0].Kind != "image" {
		t.Fatalf("row kind = %q, want image", rows[0].Kind)
	}
	if rows[0].Title != "sunset over the harbour" {
		t.Fatalf("row title = %q, want the prompt's own words", rows[0].Title)
	}
	if rows[0].Path != filepath.Join(place.Artifacts(), entries[0].Name()) {
		t.Fatalf("row path = %q, want the file that was written", rows[0].Path)
	}
}

// An OWNED session's workspace IS ours, so a picture lands in it like any other
// file the work produced — no dot directory, no artifacts/ detour.
func TestAPaintedPictureLandsInTheWorkspaceForAnOwnedSession(t *testing.T) {
	picture := pngOfSize(t, 8, 6)
	painter := &scriptedMedia{base64: base64.StdEncoding.EncodeToString(picture), mediaType: "image/png"}
	place := newPlace(t, true)
	index := filepath.Join(t.TempDir(), "artifacts.jsonl")

	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.Workspace = place.Work()
		config.Media = painter
		config.MediaModel = mediaModels(map[string]string{modalityImage: "paint/model"})
		config.Place = place
		config.ArtifactsIndex = index
	})
	if result, isError := runTool(t, agent, "generate_image", `{"prompt":"a harbour"}`); isError {
		t.Fatalf("generate_image failed: %s", result)
	}

	entries, err := os.ReadDir(place.Work())
	if err != nil {
		t.Fatalf("read the owned workspace: %v", err)
	}
	if len(entries) != 1 || entries[0].IsDir() {
		t.Fatalf("owned workspace holds %v, want one picture in it", entries)
	}
	if !strings.HasSuffix(entries[0].Name(), "-a-harbour.png") {
		t.Fatalf("generated name %q lost the timestamp-slug shape", entries[0].Name())
	}
	if _, err := os.Stat(place.Artifacts()); !os.IsNotExist(err) {
		t.Fatalf("an owned session made an artifacts directory it has no use for: %v", err)
	}
	if rows := ReadArtifacts(index); len(rows) != 1 {
		t.Fatalf("artifact rows = %d, want 1", len(rows))
	}
}

// A session with no folder keeps the dot directory it always had, and records
// nothing when nobody gave it an index — a test and a headless --once both.
func TestAPaintedPictureKeepsTheLegacyPathWithoutAFolder(t *testing.T) {
	picture := pngOfSize(t, 8, 6)
	painter := &scriptedMedia{base64: base64.StdEncoding.EncodeToString(picture), mediaType: "image/png"}
	agent, workspace := newPainterAgent(t, painter, "paint/model")

	if result, isError := runTool(t, agent, "generate_image", `{"prompt":"a harbour"}`); isError {
		t.Fatalf("generate_image failed: %s", result)
	}
	entries, err := os.ReadDir(filepath.Join(workspace, ".aforge-v3", "images"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("legacy image directory = %v, %v; want one picture", entries, err)
	}
}

// PlaceSession reads the id off the folder's own name, and answers NOTHING for
// a session that has no folder rather than inventing one.
func TestPlaceSessionNamesTheFolderAndNothingElse(t *testing.T) {
	place := newPlace(t, false)
	if got, want := PlaceSession(place), filepath.Base(place.Dir); got != want {
		t.Fatalf("session id = %q, want %q", got, want)
	}
	if got := PlaceSession(Place{}); got != "" {
		t.Fatalf("a session with no folder named itself %q", got)
	}
}
