package placegraph

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mkdir(t *testing.T, parts ...string) string {
	t.Helper()
	dir := filepath.Join(parts...)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	return dir
}

// realDir is t.TempDir with symlinks resolved, since NewSource stores the real
// path and a temp dir may itself live behind one (macOS /var → /private/var).
func realDir(t *testing.T) string {
	t.Helper()
	d, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestRelativeAndMissingPathsAreRefused(t *testing.T) {
	root := realDir(t)
	for name, ref := range map[string]string{
		"relative": "docs/brand.md",
		"dotted":   "../outside",
		"missing":  filepath.Join(root, "nope"),
	} {
		if _, err := NewSource(SourceFolder, ref, AddedByYou, noDeny); !errors.Is(err, ErrSourceRefused) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := NewSource("drive", root, AddedByYou, noDeny); !errors.Is(err, ErrInvalid) {
		t.Errorf("unknown kind: %v", err)
	}
	f := filepath.Join(root, "a.md")
	if err := os.WriteFile(f, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewSource(SourceFolder, f, AddedByYou, noDeny); !errors.Is(err, ErrSourceRefused) {
		t.Errorf("a file is not a folder: %v", err)
	}
	if _, err := NewSource(SourceFile, root, AddedByYou, noDeny); !errors.Is(err, ErrSourceRefused) {
		t.Errorf("a folder is not a file: %v", err)
	}
	src, err := NewSource(SourceFile, root+"/./a.md", AddedByAI, noDeny)
	if err != nil || src.Ref != f || src.Label != "a.md" || !strings.HasPrefix(src.ID, "src_") || src.AddedBy != AddedByAI {
		t.Fatalf("file = %+v %v", src, err)
	}
}

func TestOnlyWebURLsAreSources(t *testing.T) {
	for _, bad := range []string{"file:///etc/passwd", "javascript:alert(1)", "ftp://x.example/", "https://", "https://user:pw@x.example/", "not a url"} {
		if _, err := NewSource(SourceURL, bad, AddedByYou, noDeny); !errors.Is(err, ErrSourceRefused) {
			t.Errorf("%q: %v", bad, err)
		}
	}
	src, err := NewSource(SourceURL, "HTTPS://Example.COM/Brand", AddedByYou, noDeny)
	if err != nil || src.Ref != "https://example.com/Brand" || src.Label != "example.com" {
		t.Fatalf("url = %+v %v", src, err)
	}
}

func TestAFolderInsideARepoKeepsWhereItPointedAndNamesTheRepo(t *testing.T) {
	repo := mkdir(t, realDir(t), "proj")
	mkdir(t, repo, ".git")
	sub := mkdir(t, repo, "internal", "parse")

	src, err := NewSource(SourceFolder, sub, AddedByYou, noDeny)
	if err != nil || src.Kind != SourceFolder || src.Ref != sub || src.Label != "parse" {
		t.Fatalf("subfolder = %+v %v", src, err)
	}
	root, err := NewSource(SourceFolder, repo, AddedByYou, noDeny)
	if err != nil || root.Kind != SourceRepo || root.Ref != repo || root.Label != "proj" {
		t.Fatalf("root = %+v %v", root, err)
	}
	asked, err := NewSource(SourceRepo, sub, AddedByYou, noDeny)
	if err != nil || asked.Kind != SourceRepo || asked.Ref != repo {
		t.Fatalf("asking for the repo takes the root: %+v %v", asked, err)
	}
	plain := mkdir(t, realDir(t), "plain")
	if _, err := NewSource(SourceRepo, plain, AddedByYou, noDeny); !errors.Is(err, ErrSourceRefused) {
		t.Fatalf("a plain folder is not a repo: %v", err)
	}

	// And the resolver names the repository around a subfolder.
	s, _ := newStore(t)
	p := placeWith(t, s, "Parser", Context{Sources: []Source{src}}, Policy{})
	file(t, s, "c", p)
	b := snap(t, s).Resolve("c", ResolveOptions{Sources: noDeny})
	if len(b.Sources) != 1 || b.Sources[0].RepoRoot != repo || b.Sources[0].Status != SourceOK {
		t.Fatalf("resolved = %+v", b.Sources)
	}
}

func TestCredentialFoldersAreRefusedEvenThroughASymlink(t *testing.T) {
	home := realDir(t)
	ssh := mkdir(t, home, ".ssh")
	if err := os.WriteFile(filepath.Join(ssh, "id_ed25519"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	codeafHome := mkdir(t, home, "elsewhere", "codeaf-home")
	pol := DefaultSourcePolicy(home, codeafHome)
	project := mkdir(t, home, "project")
	link := filepath.Join(project, "keys")
	if err := os.Symlink(ssh, link); err != nil {
		t.Skip("no symlinks here:", err)
	}
	for _, ref := range []string{ssh, link, codeafHome, filepath.Join(home, ".aws")} {
		if _, err := NewSource(SourceFolder, ref, AddedByYou, pol); !errors.Is(err, ErrSourceRefused) {
			t.Errorf("%s: %v", ref, err)
		}
	}
	if _, err := NewSource(SourceFile, filepath.Join(link, "id_ed25519"), AddedByAI, pol); !errors.Is(err, ErrSourceRefused) {
		t.Errorf("a key file through a link: %v", err)
	}

	// A record that already holds one (an older build, a hand edit) is refused
	// at resolve time and never reaches the model.
	s, _ := newStore(t)
	p := placeWith(t, s, "P", Context{Sources: []Source{
		{ID: "a", Kind: SourceFolder, Ref: link, AddedBy: AddedByAI},
		{ID: "b", Kind: SourceFolder, Ref: project, AddedBy: AddedByYou},
	}}, Policy{})
	file(t, s, "c", p)
	b := snap(t, s).Resolve("c", ResolveOptions{Sources: pol})
	if len(b.Refused) != 1 || b.Refused[0].Ref != link || !strings.Contains(b.Refused[0].Reason, "credentials") {
		t.Fatalf("refused = %+v", b.Refused)
	}
	if len(b.Sources) != 1 || b.Sources[0].Ref != project {
		t.Fatalf("given = %+v", b.Sources)
	}
}

func TestResolveSaysAMissingPathAndRefusesTheWrongShape(t *testing.T) {
	root := realDir(t)
	f := filepath.Join(root, "notes.md")
	if err := os.WriteFile(f, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	s, _ := newStore(t)
	p := placeWith(t, s, "P", Context{Sources: []Source{
		{ID: "gone", Kind: SourceFolder, Ref: filepath.Join(root, "unplugged"), AddedBy: AddedByYou},
		{ID: "shape", Kind: SourceFolder, Ref: f, AddedBy: AddedByYou},
		{ID: "rel", Kind: SourceFile, Ref: "notes.md", AddedBy: AddedByYou},
		{ID: "self", Kind: SourceChat, Ref: "c", AddedBy: AddedByAI},
		{ID: "other", Kind: SourceChat, Ref: "0123abcd", AddedBy: AddedByYou},
		{ID: "esc", Kind: SourceChat, Ref: "../x", AddedBy: AddedByYou},
	}}, Policy{})
	file(t, s, "c", p)
	pol := SourcePolicy{Deny: []string{}, ChatTitle: func(id string) string { return "Title of " + id }}
	b := snap(t, s).Resolve("c", ResolveOptions{Sources: pol})
	status := map[string]string{}
	for _, u := range append(append(b.Sources, b.Refused...), b.Trimmed...) {
		status[u.From[0].SourceID] = string(u.Status) + ":" + u.Label
	}
	want := map[string]string{
		"gone": "missing:", "shape": "refused:", "rel": "refused:",
		"self": "refused:", "other": "ok:Title of 0123abcd", "esc": "refused:",
	}
	for id, w := range want {
		if status[id] != w {
			t.Errorf("%s = %q, want %q", id, status[id], w)
		}
	}
	if b.Counts.Sources != 2 {
		t.Fatalf("the chip counts what the chat is given (missing is still named): %+v", b.Counts)
	}
}

func TestWithSourceIsANoopForTheSameRef(t *testing.T) {
	dir := realDir(t)
	a, err := NewSource(SourceFolder, dir, AddedByYou, noDeny)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := NewSource(SourceFolder, dir+"/", AddedByAI, noDeny)
	c, added := WithSource(Context{Instructions: "keep"}, a)
	if !added || len(c.Sources) != 1 || c.Instructions != "keep" {
		t.Fatalf("first = %+v %v", c, added)
	}
	c2, added := WithSource(c, b)
	if added || len(c2.Sources) != 1 {
		t.Fatalf("second = %+v %v", c2, added)
	}
	// The result is storable as-is.
	s, _ := newStore(t)
	p := mk(t, s, "P")
	ok(t)(s.SetContext(p.ID, c2))
}
