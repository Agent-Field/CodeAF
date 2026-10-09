package desktopbridge

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/placegraph"
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
)

// folderRig is a using rig whose bridge also has the door that opens a chat in another folder. It records
// every folder the door was asked for, and answers with a connection that says it works THERE.
type folderRig struct {
	*usingRig
	mu      sync.Mutex
	asked   []string
	files   []string
	closed  int
	answers func(workspace string) string // what the engine reports; nil answers the folder it was asked for
}

func newFolderRig(t *testing.T) *folderRig {
	t.Helper()
	rig := &folderRig{usingRig: newUsingRig(t)}
	rig.b.UseOpenIn(func(workspace, file string) (Connection, error) {
		rig.mu.Lock()
		rig.asked = append(rig.asked, workspace)
		rig.files = append(rig.files, file)
		reported := workspace
		sessionFile := rig.file
		if strings.TrimSpace(file) != "" {
			sessionFile = file
		}
		if rig.answers != nil {
			reported = rig.answers(workspace)
		}
		rig.mu.Unlock()
		return Connection{Agent: rig.agent, Welcome: remote.Welcome{SessionFile: sessionFile, Workspace: reported, Model: Model, Persistent: true, Launch: rig.launch}, Local: true, Close: func() { rig.mu.Lock(); rig.closed++; rig.mu.Unlock() }}, nil
	})
	return rig
}

func (r *folderRig) foldersAsked() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.asked...)
}

func (r *folderRig) filesAsked() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.files...)
}

func (r *folderRig) placeWith(name string, sources ...placegraph.Source) string {
	r.t.Helper()
	p, _, err := r.p.Store.CreatePlace(placegraph.NewPlace{Name: name, Context: placegraph.Context{Sources: sources}})
	if err != nil {
		r.t.Fatal(err)
	}
	return p.ID
}

func folder(t *testing.T, path string) placegraph.Source {
	t.Helper()
	src, err := placegraph.NewSource(placegraph.SourceFolder, path, placegraph.AddedByYou, placegraph.SourcePolicy{Deny: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	return src
}

func (r *folderRig) snapshotOf(body map[string]any) (Snapshot, int, apiError) {
	r.t.Helper()
	var out struct {
		Snapshot
		apiError
	}
	code := r.do("POST", "/sessions", body, &out)
	return out.Snapshot, code, out.apiError
}

func TestANewChatInAPlaceIsOpenedInItsFirstFolderAndSaysSo(t *testing.T) {
	rig := newFolderRig(t)
	root := t.TempDir()
	first, second := filepath.Join(root, "lexer"), filepath.Join(root, "docs")
	os.MkdirAll(first, 0o755)
	os.MkdirAll(second, 0o755)
	place := rig.placeWith("Lexer", folder(t, first), folder(t, second))
	snap, code, _ := rig.snapshotOf(map[string]any{"place": place})
	want, _ := filepath.EvalSymlinks(first)
	if code != 200 || snap.Workspace != want {
		t.Fatalf("open = %d, workspace %q, want %q", code, snap.Workspace, want)
	}
	if got := rig.foldersAsked(); len(got) != 1 || got[0] != want || rig.filesAsked()[0] != "" || rig.opens.Load() != 0 {
		t.Fatalf("door asked for %v files %v, bridge workspace opens %d", got, rig.filesAsked(), rig.opens.Load())
	}
	if wf := snap.WorkingFolder; wf == nil || wf.From != "place" || wf.Path != want || wf.Note != "" {
		t.Fatalf("working folder = %+v", wf)
	}
	// The chat is still filed before its first turn, exactly as before this change.
	state, _ := rig.p.Store.Snapshot()
	if ms := state.PlacesOf(rig.chat); len(ms) != 1 || ms[0].PlaceID != place {
		t.Fatalf("memberships: %+v", ms)
	}
}

func TestAPlaceWithNoFolderKeepsTheBridgeWorkspaceAndSaysNothing(t *testing.T) {
	rig := newFolderRig(t)
	link := placegraph.Source{ID: "page", Kind: placegraph.SourceURL, Ref: "https://example.com/spec", AddedBy: placegraph.AddedByYou}
	for name, place := range map[string]string{"no sources": rig.placeWith("Empty"), "only a page": rig.placeWith("Spec", link)} {
		rig.opens.Store(0)
		raw := rig.do("POST", "/sessions", map[string]any{"place": place}, new(json.RawMessage))
		if raw != 200 || rig.opens.Load() != 1 || len(rig.foldersAsked()) != 0 {
			t.Fatalf("%s: code %d, bridge opens %d, folders %v", name, raw, rig.opens.Load(), rig.foldersAsked())
		}
		var body map[string]json.RawMessage
		rig.do("POST", "/sessions", map[string]any{"place": place}, &body)
		if _, said := body["workingFolder"]; said {
			t.Fatalf("%s: said something about a folder it never had", name)
		}
	}
}

func TestAPlaceWhoseFoldersCannotBeUsedFallsBackToTheLaunchWorkspaceAndExplainsWhy(t *testing.T) {
	rig := newFolderRig(t)
	gone := filepath.Join(t.TempDir(), "gone")
	os.MkdirAll(gone, 0o755)
	place := rig.placeWith("Old", folder(t, gone))
	os.Remove(gone)
	snap, code, _ := rig.snapshotOf(map[string]any{"place": place})
	if code != 200 || rig.opens.Load() != 1 || len(rig.foldersAsked()) != 0 {
		t.Fatalf("code %d opens %d folders %v", code, rig.opens.Load(), rig.foldersAsked())
	}
	wf := snap.WorkingFolder
	if wf == nil || wf.From != "launch" || wf.Path != "" || len(wf.Skipped) != 1 || !strings.Contains(wf.Note, "gone") || !strings.Contains(wf.Note, "not on this disk right now") {
		t.Fatalf("working folder = %+v", wf)
	}
}

func TestASymlinkedSourceIsOpenedAtItsRealFolder(t *testing.T) {
	rig := newFolderRig(t)
	root := t.TempDir()
	real := filepath.Join(root, "real")
	os.MkdirAll(real, 0o755)
	link := filepath.Join(root, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skip("no symlinks")
	}
	// A record that kept the link's spelling (the add door stores it resolved; an older file may not).
	place := rig.placeWith("Linked", placegraph.Source{ID: "s", Kind: placegraph.SourceFolder, Ref: link, AddedBy: placegraph.AddedByYou})
	if _, code, _ := rig.snapshotOf(map[string]any{"place": place}); code != 200 {
		t.Fatalf("open = %d", code)
	}
	if want, _ := filepath.EvalSymlinks(real); len(rig.foldersAsked()) != 1 || rig.foldersAsked()[0] != want {
		t.Fatalf("door asked for %v, want the real folder %s", rig.foldersAsked(), want)
	}
}

func TestAnEngineThatOpensTheChatSomewhereElseIsRefusedAndNothingIsFiled(t *testing.T) {
	rig := newFolderRig(t)
	dir := t.TempDir()
	elsewhere := t.TempDir()
	rig.answers = func(string) string { return elsewhere }
	place := rig.placeWith("Lexer", folder(t, dir))
	_, code, e := rig.snapshotOf(map[string]any{"place": place})
	if code != 409 || !strings.Contains(e.Error, elsewhere) {
		t.Fatalf("open = %d %+v", code, e)
	}
	rig.mu.Lock()
	closed := rig.closed
	rig.mu.Unlock()
	state, _ := rig.p.Store.Snapshot()
	if closed != 1 || len(state.PlacesOf(rig.chat)) != 0 {
		t.Fatalf("closed %d, memberships %+v", closed, state.PlacesOf(rig.chat))
	}
}

func TestAWindowCannotNameTheFolderAndAReattachNeverMovesTheWorkspace(t *testing.T) {
	rig := newFolderRig(t)
	dir := t.TempDir()
	attacker := t.TempDir()
	place := rig.placeWith("Lexer", folder(t, dir))
	// A path is not a field of this request. The decoder refuses the body, so
	// the door is never asked, rather than being asked and trusted to ignore it.
	for _, key := range []string{"workspace", "workingFolder", "folder", "cwd", "path"} {
		if _, code, e := rig.snapshotOf(map[string]any{"place": place, key: attacker}); code != 400 || !strings.Contains(e.Error, "invalid request") {
			t.Fatalf("%s was accepted: %d %s", key, code, e.Error)
		}
	}
	if len(rig.foldersAsked()) != 0 {
		t.Fatalf("a refused body still opened %v", rig.foldersAsked())
	}
	if _, code, _ := rig.snapshotOf(map[string]any{"place": place}); code != 200 {
		t.Fatalf("the place itself did not open: %d", code)
	}
	for _, asked := range rig.foldersAsked() {
		if want, _ := filepath.EvalSymlinks(dir); asked != want {
			t.Fatalf("the engine was asked to open %s", asked)
		}
	}
	// A saved conversation reopens on the workspace its own record names, through the bridge's own door.
	before := len(rig.foldersAsked())
	rig.opens.Store(0)
	if _, code, _ := rig.snapshotOf(map[string]any{"sessionFile": filepath.Join(t.TempDir(), "other", "transcript.jsonl")}); code != 200 || rig.opens.Load() != 1 || len(rig.foldersAsked()) != before {
		t.Fatalf("a reattach used the folder door: code %d opens %d", code, rig.opens.Load())
	}
}

func TestABridgeWithNoFolderDoorKeepsTheChatWhereItWasLaunchedAndSaysSo(t *testing.T) {
	rig := newUsingRig(t) // no UseOpenIn
	dir := t.TempDir()
	src, _ := placegraph.NewSource(placegraph.SourceFolder, dir, placegraph.AddedByYou, placegraph.SourcePolicy{Deny: []string{}})
	p, _, _ := rig.p.Store.CreatePlace(placegraph.NewPlace{Name: "Lexer", Context: placegraph.Context{Sources: []placegraph.Source{src}}})
	var snap Snapshot
	if code := rig.do("POST", "/sessions", map[string]any{"place": p.ID}, &snap); code != 200 || rig.opens.Load() != 1 {
		t.Fatalf("open = %d, opens %d", code, rig.opens.Load())
	}
	if wf := snap.WorkingFolder; wf == nil || wf.From != "launch" || wf.Note == "" {
		t.Fatalf("working folder = %+v", wf)
	}
}

// librarySession writes one saved conversation under this test's state root and
// points CODEAF_HOME at it. The transcript is empty; the identity is the record.
func librarySession(t *testing.T, id, workspace string) string {
	t.Helper()
	root := t.TempDir()
	t.Setenv(home.EnvVar, root)
	dir := filepath.Join(root, "v3", "projects", "bucket", id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "transcript.jsonl")
	if err := os.WriteFile(file, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := session.SaveMeta(dir, session.Meta{ID: id, Workspace: workspace}); err != nil {
		t.Fatal(err)
	}
	return file
}

func TestASavedConversationReopensOnTheWorkspaceItsRecordNames(t *testing.T) {
	rig := newFolderRig(t)
	work := t.TempDir()
	file := librarySession(t, "0123456789abcdef", work)
	want, _ := filepath.EvalSymlinks(work)
	snap, code, e := rig.snapshotOf(map[string]any{"sessionFile": file})
	if code != 200 {
		t.Fatalf("open = %d %s", code, e.Error)
	}
	if code != 200 || snap.Workspace != want || snap.SessionFile != file {
		t.Fatalf("open = %d workspace %q file %q", code, snap.Workspace, snap.SessionFile)
	}
	if got, files := rig.foldersAsked(), rig.filesAsked(); len(got) != 1 || got[0] != want || len(files) != 1 || files[0] != file || rig.opens.Load() != 0 {
		t.Fatalf("door %v files %v bridge opens %d", got, files, rig.opens.Load())
	}
	if snap.WorkingFolder != nil {
		t.Fatalf("a reopened chat explained a place choice it did not make: %+v", snap.WorkingFolder)
	}
}

func TestASessionFileOutsideTheLibraryCannotNameAWorkspace(t *testing.T) {
	rig := newFolderRig(t)
	t.Setenv(home.EnvVar, t.TempDir())
	outside := t.TempDir()
	dir := filepath.Join(outside, "0123456789abcdef")
	os.MkdirAll(dir, 0o700)
	file := filepath.Join(dir, "transcript.jsonl")
	os.WriteFile(file, []byte("{}\n"), 0o600)
	if err := session.SaveMeta(dir, session.Meta{ID: "0123456789abcdef", Workspace: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	if _, code, _ := rig.snapshotOf(map[string]any{"sessionFile": file}); code != 200 || len(rig.foldersAsked()) != 0 || rig.opens.Load() != 1 {
		t.Fatalf("code %d folders %v opens %d", code, rig.foldersAsked(), rig.opens.Load())
	}
}

func TestARecordedWorkspaceInsideTheLibraryIsNotFollowed(t *testing.T) {
	rig := newFolderRig(t)
	root := t.TempDir()
	t.Setenv(home.EnvVar, root)
	inside := filepath.Join(root, "owned-work")
	if err := os.MkdirAll(inside, 0o755); err != nil {
		t.Fatal(err)
	}
	id := "0123456789abcdef"
	dir := filepath.Join(root, "v3", "projects", "bucket", id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "transcript.jsonl")
	if err := os.WriteFile(file, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := session.SaveMeta(dir, session.Meta{ID: id, Workspace: inside}); err != nil {
		t.Fatal(err)
	}
	if _, code, _ := rig.snapshotOf(map[string]any{"sessionFile": file}); code != 200 || len(rig.foldersAsked()) != 0 || rig.opens.Load() != 1 {
		t.Fatalf("code %d folders %v opens %d", code, rig.foldersAsked(), rig.opens.Load())
	}
}

func TestARecordedWorkspaceThatLeadsIntoADeniedFolderIsNotFollowed(t *testing.T) {
	rig := newFolderRig(t)
	secrets := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(secrets, link); err != nil {
		t.Skip("no symlinks")
	}
	rig.door.Sources = placegraph.SourcePolicy{Deny: []string{secrets}}
	file := librarySession(t, "0123456789abcdef", link)
	if _, code, _ := rig.snapshotOf(map[string]any{"sessionFile": file}); code != 200 || len(rig.foldersAsked()) != 0 || rig.opens.Load() != 1 {
		t.Fatalf("code %d folders %v opens %d", code, rig.foldersAsked(), rig.opens.Load())
	}
}
