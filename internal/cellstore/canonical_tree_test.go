package cellstore

import (
	"os"
	"path/filepath"
	"testing"
)

// linkedRoot is a real folder and a symbolic link to it, both made by the test,
// so the alias is there on every platform and not only where the temporary
// folder happens to be reached through one (macOS /var).
func linkedRoot(t *testing.T) (real, link string) {
	t.Helper()
	base := t.TempDir()
	real = filepath.Join(base, "real")
	if err := os.Mkdir(real, 0o700); err != nil {
		t.Fatal(err)
	}
	link = filepath.Join(base, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("this filesystem cannot make a symbolic link: %v", err)
	}
	resolved, err := filepath.EvalSymlinks(real)
	if err != nil {
		t.Fatal(err)
	}
	return resolved, link
}

// A folder that does not exist yet resolves the ancestors it has, so the same
// folder keeps one spelling before and after it is created: a binding recorded
// before creation must not read as another folder after it.
func TestCanonicalTreeResolvesAncestorOfMissingWorkspace(t *testing.T) {
	real, link := linkedRoot(t)
	missing := filepath.Join(link, "work", "nested")
	want := filepath.Join(real, "work", "nested")
	if got := canonicalTree(missing); got != want {
		t.Fatalf("absent folder under a link: got %q, want %q", got, want)
	}
	if err := os.MkdirAll(missing, 0o700); err != nil {
		t.Fatal(err)
	}
	if got := canonicalTree(missing); got != want {
		t.Fatalf("the same folder once created: got %q, want %q", got, want)
	}
	if got := canonicalTree(filepath.Join(real, "other")); got != filepath.Join(real, "other") {
		t.Fatalf("an absent folder under a real path changed spelling: %q", got)
	}
}

// A path through a file is not a missing folder: it keeps its absolute
// spelling rather than being resolved past the file.
func TestCanonicalTreeKeepsAPathThroughAFile(t *testing.T) {
	real, link := linkedRoot(t)
	if err := os.WriteFile(filepath.Join(real, "file"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	through := filepath.Join(link, "file", "child")
	if got := canonicalTree(through); got != through {
		t.Fatalf("a path through a file: got %q, want it unchanged %q", got, through)
	}
}

// A chat reached through a link that saves only its own folder still owns its
// work/ before that folder exists; a folder outside the chat stays foreign.
func TestForeignFolderKeepsMissingWorkUnderSymlinkedChat(t *testing.T) {
	real, link := linkedRoot(t)
	useHome(t, t.TempDir())
	root := filepath.Join(link, "chat")
	if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(filepath.Dir(OwnFolderPath))), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(OwnFolderPath)), []byte("own\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if ForeignFolder(root, filepath.Join(root, "work")) {
		t.Fatal("the chat's own absent work/ is called foreign")
	}
	if ForeignFolder(filepath.Join(real, "chat"), filepath.Join(root, "work")) {
		t.Fatal("the chat's own absent work/, named through the link, is called foreign from the real spelling")
	}
	outside := filepath.Join(real, "elsewhere")
	if err := os.Mkdir(outside, 0o700); err != nil {
		t.Fatal(err)
	}
	if !ForeignFolder(root, outside) {
		t.Fatal("a folder outside the chat is not called foreign")
	}
}

// A link whose target is missing is not a missing folder: a path through it
// keeps its spelled, absolute form, so a chat's work/ that is a dangling link
// to somewhere else stays foreign when the chat is named through an alias.
func TestCanonicalTreeDoesNotResolvePastADanglingLink(t *testing.T) {
	real, link := linkedRoot(t)
	chat := filepath.Join(real, "chat")
	if err := os.MkdirAll(filepath.Join(chat, filepath.FromSlash(filepath.Dir(OwnFolderPath))), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(chat, filepath.FromSlash(OwnFolderPath)), []byte("own\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(t.TempDir(), "absent"), filepath.Join(chat, "work")); err != nil {
		t.Skipf("this filesystem cannot make a symbolic link: %v", err)
	}
	for _, spelled := range []string{filepath.Join(link, "chat", "work"), filepath.Join(link, "chat", "work", "deeper")} {
		if got := canonicalTree(spelled); got != spelled {
			t.Errorf("a path through a dangling link: got %q, want it unchanged %q", got, spelled)
		}
	}
	useHome(t, t.TempDir())
	if !ForeignFolder(filepath.Join(link, "chat"), filepath.Join(link, "chat", "work")) {
		t.Error("a dangling work/ link named through the alias is not called foreign")
	}
}
