package desktopbridge

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/placegraph"
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
)

// placeList is a local engine that can say which folders the conversation is
// about. The embedded Agent is never called: roots asserts only Places.
type placeList struct {
	Agent
	places []session.PlaceRef
}

func (p placeList) Places() []session.PlaceRef { return append([]session.PlaceRef(nil), p.places...) }

func rootsCall(b *Bridge, method, auth string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "/roots", nil)
	if auth != "" {
		r.Header.Set("Authorization", auth)
	}
	w := httptest.NewRecorder()
	b.roots(w, r)
	return w
}

func decodeRoots(t *testing.T, w *httptest.ResponseRecorder) []string {
	t.Helper()
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	dec := json.NewDecoder(strings.NewReader(w.Body.String()))
	dec.DisallowUnknownFields()
	var body rootsBody
	if err := dec.Decode(&body); err != nil {
		t.Fatalf("decode: %v body %s", err, w.Body.String())
	}
	if body.Roots == nil {
		t.Fatal("roots encoded as null; an empty answer is an empty list")
	}
	return body.Roots
}

func mustDir(t *testing.T, path string) string {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func mustReal(t *testing.T, path string) string {
	t.Helper()
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	return real
}

func park(local bool, welcome remote.Welcome, agent Agent) *conversation {
	return &conversation{
		conn: Connection{Local: local, Welcome: welcome, Agent: agent, Close: func() {}},
		done: make(chan struct{}),
	}
}

func rootsBridge(t *testing.T) *Bridge {
	t.Helper()
	b := New(testToken, nil)
	t.Cleanup(b.Close)
	return b
}

// TestRootsAreWorkspacesSessionFoldersAndPlaceFolders is the confine list:
// each local conversation's workspace and session folder, the folders that
// conversation's person named, and every folder or repository source in the
// place graph. A kept place, a file, a link, a chat, a relative path and a
// forwarded engine are not roots. Two spellings of one directory are one root.
func TestRootsAreWorkspacesSessionFoldersAndPlaceFolders(t *testing.T) {
	root := t.TempDir()
	workspace := mustDir(t, filepath.Join(root, "workspace"))
	link := filepath.Join(root, "workspace-link")
	if err := os.Symlink(workspace, link); err != nil {
		t.Fatal(err)
	}
	sessionDir := mustDir(t, filepath.Join(root, "session"))
	said := mustDir(t, filepath.Join(root, "said"))
	kept := mustDir(t, filepath.Join(root, "kept"))
	plain := mustDir(t, filepath.Join(root, "plain"))
	repo := mustDir(t, filepath.Join(root, "repo"))
	initGitFixture(t, repo)
	archived := mustDir(t, filepath.Join(root, "archived"))
	diskSaid := mustDir(t, filepath.Join(root, "disk-said"))
	diskKept := mustDir(t, filepath.Join(root, "disk-kept"))
	diskSession := mustDir(t, filepath.Join(root, "disk-session"))
	otherWorkspace := mustDir(t, filepath.Join(root, "other-workspace"))
	otherSession := mustDir(t, filepath.Join(root, "other-session"))
	missingParent := mustDir(t, filepath.Join(root, "missing-parent"))
	missing := filepath.Join(missingParent, "gone")
	note := filepath.Join(root, "note.txt")
	if err := os.WriteFile(note, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	remoteRoot := mustDir(t, filepath.Join(root, "remote"))
	remoteWorkspace := mustDir(t, filepath.Join(remoteRoot, "workspace"))
	remoteSession := mustDir(t, filepath.Join(remoteRoot, "session"))
	remoteSaid := mustDir(t, filepath.Join(remoteRoot, "said"))

	if err := session.SaveMeta(diskSession, session.Meta{
		ID: "disk",
		Places: []session.PlaceRef{
			{Path: diskSaid, Arrival: session.PlaceSaid},
			{Path: diskKept, Arrival: session.PlaceKept},
			{Path: "relative/kept-out", Arrival: session.PlaceSaid},
		},
	}); err != nil {
		t.Fatal(err)
	}

	noDeny := placegraph.SourcePolicy{Deny: []string{}}
	src := func(kind placegraph.SourceKind, ref string) placegraph.Source {
		t.Helper()
		got, err := placegraph.NewSource(kind, ref, placegraph.AddedByYou, noDeny)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	store, err := placegraph.Open(placegraph.Options{Path: filepath.Join(root, "places.json")})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.CreatePlace(placegraph.NewPlace{
		Name: "Active",
		Context: placegraph.Context{Sources: []placegraph.Source{
			src(placegraph.SourceFolder, plain),
			src(placegraph.SourceRepo, repo),
			src(placegraph.SourceFile, note),
			src(placegraph.SourceFolder, link),
			src(placegraph.SourceURL, "https://example.com/notes"),
			src(placegraph.SourceChat, "chat-one"),
		}},
	}); err != nil {
		t.Fatal(err)
	}
	archivedPlace, _, err := store.CreatePlace(placegraph.NewPlace{
		Name:    "Old",
		Context: placegraph.Context{Sources: []placegraph.Source{src(placegraph.SourceFolder, archived)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Archive(archivedPlace.ID); err != nil {
		t.Fatal(err)
	}

	b := rootsBridge(t)
	b.places = NewPlaces(store)
	b.sessions["live"] = park(true, remote.Welcome{
		Workspace:   link,
		SessionFile: filepath.Join(sessionDir, "session.jsonl"),
	}, placeList{places: []session.PlaceRef{
		{Path: said, Arrival: session.PlaceSaid},
		{Path: kept, Arrival: session.PlaceKept},
	}})
	b.sessions["other"] = park(true, remote.Welcome{
		Workspace:   otherWorkspace,
		SessionFile: filepath.Join(otherSession, "session.jsonl"),
	}, placeList{places: []session.PlaceRef{
		{Path: plain, Arrival: session.PlaceSaid},
	}})
	b.sessions["disk"] = park(true, remote.Welcome{
		Workspace:   missing,
		SessionFile: filepath.Join(diskSession, "session.jsonl"),
	}, &fakeAgent{})
	b.sessions["relative"] = park(true, remote.Welcome{
		Workspace:   "relative/ws",
		SessionFile: "relative/session.jsonl",
	}, placeList{places: []session.PlaceRef{
		{Path: said, Arrival: session.PlaceSaid},
	}})
	b.sessions["remote"] = park(false, remote.Welcome{
		Workspace:   remoteWorkspace,
		SessionFile: filepath.Join(remoteSession, "session.jsonl"),
	}, placeList{places: []session.PlaceRef{
		{Path: remoteSaid, Arrival: session.PlaceSaid},
	}})

	got := decodeRoots(t, rootsCall(b, http.MethodGet, "Bearer "+testToken))
	want := []string{
		mustReal(t, workspace),
		mustReal(t, sessionDir),
		mustReal(t, said),
		mustReal(t, plain),
		mustReal(t, repo),
		mustReal(t, archived),
		mustReal(t, diskSaid),
		mustReal(t, diskSession),
		mustReal(t, otherWorkspace),
		mustReal(t, otherSession),
		filepath.Join(mustReal(t, missingParent), "gone"),
	}
	sort.Strings(want)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("roots:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	for _, banned := range []string{kept, diskKept, note, remoteWorkspace, remoteSession, remoteSaid, "relative/ws", "relative/kept-out", "example.com", "chat-one"} {
		for _, root := range got {
			if root == banned || strings.Contains(root, banned) {
				t.Fatalf("root %q contains %q, which is not a confine root", root, banned)
			}
		}
	}

	// No conversation yet: the bridge is local, so the place folders remain.
	quiet := rootsBridge(t)
	quiet.places = NewPlaces(store)
	quietGot := decodeRoots(t, rootsCall(quiet, http.MethodGet, "Bearer "+testToken))
	quietWant := []string{mustReal(t, workspace), mustReal(t, plain), mustReal(t, repo), mustReal(t, archived)}
	sort.Strings(quietWant)
	if strings.Join(quietGot, "\n") != strings.Join(quietWant, "\n") {
		t.Fatalf("no conversation:\n%s\nwant:\n%s", strings.Join(quietGot, "\n"), strings.Join(quietWant, "\n"))
	}
}

// TestARemoteEngineReportsNoRoots is the forwarded-engine answer: an empty
// list, including when the place graph on this machine has folders. Those
// folders are not that engine's disk.
func TestARemoteEngineReportsNoRoots(t *testing.T) {
	root := t.TempDir()
	folder := mustDir(t, filepath.Join(root, "folder"))
	workspace := mustDir(t, filepath.Join(root, "workspace"))
	sessionDir := mustDir(t, filepath.Join(root, "session"))
	said := mustDir(t, filepath.Join(root, "said"))
	src, err := placegraph.NewSource(placegraph.SourceFolder, folder, placegraph.AddedByYou, placegraph.SourcePolicy{Deny: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	store, err := placegraph.Open(placegraph.Options{Path: filepath.Join(root, "places.json")})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.CreatePlace(placegraph.NewPlace{
		Name:    "Away",
		Context: placegraph.Context{Sources: []placegraph.Source{src}},
	}); err != nil {
		t.Fatal(err)
	}
	b := rootsBridge(t)
	b.places = NewPlaces(store)
	b.sessions["remote"] = park(false, remote.Welcome{
		Workspace:   workspace,
		SessionFile: filepath.Join(sessionDir, "session.jsonl"),
	}, placeList{places: []session.PlaceRef{{Path: said, Arrival: session.PlaceSaid}}})

	w := rootsCall(b, http.MethodGet, "Bearer "+testToken)
	if strings.TrimSpace(w.Body.String()) != `{"roots":[]}` {
		t.Fatalf("body %s", w.Body.String())
	}
	got := decodeRoots(t, w)
	if len(got) != 0 {
		t.Fatalf("roots %v", got)
	}
}

// TestRootsNeedTheToken is the same bearer gate as ServeHTTP. A missing or
// wrong token is refused before any path is written, and a request that is
// not GET is refused after the token.
func TestRootsNeedTheToken(t *testing.T) {
	root := t.TempDir()
	workspace := mustDir(t, filepath.Join(root, "workspace"))
	b := rootsBridge(t)
	b.sessions["live"] = park(true, remote.Welcome{Workspace: workspace}, nil)

	for _, auth := range []string{"", "Bearer", "Bearer wrong-token"} {
		w := rootsCall(b, http.MethodGet, auth)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("auth %q: status %d", auth, w.Code)
		}
		if strings.Contains(w.Body.String(), workspace) || strings.Contains(w.Body.String(), "roots") {
			t.Fatalf("auth %q leaked a root: %s", auth, w.Body.String())
		}
	}
	empty := New("", nil)
	t.Cleanup(empty.Close)
	empty.sessions["live"] = park(true, remote.Welcome{Workspace: workspace}, nil)
	if w := rootsCall(empty, http.MethodGet, "Bearer "+testToken); w.Code != http.StatusUnauthorized {
		t.Fatalf("empty bridge token: status %d", w.Code)
	}
	if w := rootsCall(b, http.MethodPost, "Bearer "+testToken); w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST: status %d body %s", w.Code, w.Body.String())
	}
	if got := decodeRoots(t, rootsCall(b, http.MethodGet, "Bearer "+testToken)); len(got) != 1 || got[0] != mustReal(t, workspace) {
		t.Fatalf("authorised roots %v", got)
	}
}
