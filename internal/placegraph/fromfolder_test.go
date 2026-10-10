package placegraph

import (
	"errors"
	"path/filepath"
	"testing"
)

func fromFolderStore(t *testing.T) *Store {
	t.Helper()
	st, err := Open(Options{Path: filepath.Join(t.TempDir(), "places.json")})
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func TestAFolderBecomesAPlaceNamedAfterIt(t *testing.T) {
	st := fromFolderStore(t)
	dir := mkdir(t, realDir(t), "notes")
	got, err := st.FromFolder(dir, noDeny)
	if err != nil {
		t.Fatal(err)
	}
	p := got.Place
	if !got.Created || p.Name != "notes" || len(p.Parents) != 0 || !p.Tint.Valid() {
		t.Fatalf("place %+v created=%v", p, got.Created)
	}
	if len(p.Context.Sources) != 1 || p.Context.Sources[0].Kind != SourceFolder || p.Context.Sources[0].Ref != dir {
		t.Fatalf("sources %+v", p.Context.Sources)
	}
}

func TestARepoSubfolderBecomesTheRepoPlace(t *testing.T) {
	st := fromFolderStore(t)
	repo := mkdir(t, realDir(t), "shop")
	writeHead(t, mkdir(t, repo, ".git"))
	sub := mkdir(t, repo, "app", "src")
	got, err := st.FromFolder(sub, noDeny)
	if err != nil {
		t.Fatal(err)
	}
	src := got.Place.Context.Sources[0]
	if got.Place.Name != "shop" || src.Kind != SourceRepo || src.Ref != repo {
		t.Fatalf("name %q source %+v", got.Place.Name, src)
	}
}

func TestFromFolderIsIdempotent(t *testing.T) {
	st := fromFolderStore(t)
	repo := mkdir(t, realDir(t), "shop")
	writeHead(t, mkdir(t, repo, ".git"))
	first, err := st.FromFolder(repo, noDeny)
	if err != nil {
		t.Fatal(err)
	}
	again, err := st.FromFolder(mkdir(t, repo, "pkg"), noDeny)
	if err != nil {
		t.Fatal(err)
	}
	if again.Created || again.Place.ID != first.Place.ID || again.Receipt.ID != "" {
		t.Fatalf("second drop %+v", again)
	}
	snap, _ := st.Snapshot()
	if len(snap.Places) != 1 {
		t.Fatalf("%d places", len(snap.Places))
	}
}

func TestFromFolderRefusesWhatASourceRefuses(t *testing.T) {
	st := fromFolderStore(t)
	if _, err := st.FromFolder(filepath.Join(realDir(t), "nope"), noDeny); !errors.Is(err, ErrSourceRefused) {
		t.Fatalf("got %v", err)
	}
}

func TestMatchingUnplacedChatsAreOffered(t *testing.T) {
	st := fromFolderStore(t)
	root := mkdir(t, realDir(t), "now")
	other := mkdir(t, realDir(t), "elsewhere")
	got, err := st.FromFolder(root, noDeny)
	if err != nil {
		t.Fatal(err)
	}
	filed, _, err := st.CreatePlace(NewPlace{Name: "Filed"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.AddChat("c-filed", filed.ID, AddedByYou); err != nil {
		t.Fatal(err)
	}
	snap, _ := st.Snapshot()
	ws := map[string]string{
		"c-root":    root,
		"c-nested":  filepath.Join(root, "deep", "er"),
		"c-sibling": root + "-2",
		"c-other":   other,
		"c-filed":   root,
		"c-blank":   "",
	}
	offered := snap.MatchingUnplacedChats(got.Place.Context.Sources[0].Ref, ws)
	if len(offered) != 2 || offered[0] != "c-nested" || offered[1] != "c-root" {
		t.Fatalf("offered %v", offered)
	}
}
