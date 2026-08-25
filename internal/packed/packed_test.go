package packed

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// folder writes a small corpus with the shapes that matter: a nested
// directory, and the dot- and underscore-prefixed names go:embed leaves out.
func folder(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "corpus")
	for name, body := range map[string]string{
		"one.md":           "# One\n\nthe first page\n",
		"two.md":           "# Two\n\nthe second page\n",
		"deep/three.md":    "# Three\n\nunder a folder\n",
		".hidden.md":       "not embedded",
		"_draft.md":        "not embedded",
		"_scratch/four.md": "not embedded",
	} {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	return root
}

func TestRoundTripIsByteIdentical(t *testing.T) {
	root := folder(t)
	archive, err := Pack(root)
	if err != nil {
		t.Fatalf("pack: %v", err)
	}
	if err := Verify(archive, root); err != nil {
		t.Fatalf("verify: %v", err)
	}

	packed := New(archive)
	want := []string{"corpus/deep/three.md", "corpus/one.md", "corpus/two.md"}
	got := packed.Names()
	if len(got) != len(want) {
		t.Fatalf("names = %v, want %v", got, want)
	}
	for i, name := range want {
		if got[i] != name {
			t.Fatalf("names = %v, want %v", got, want)
		}
	}
	for _, name := range want {
		data, err := packed.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		source, err := os.ReadFile(filepath.Join(filepath.Dir(root), filepath.FromSlash(name)))
		if err != nil {
			t.Fatalf("read source %s: %v", name, err)
		}
		if string(data) != string(source) {
			t.Errorf("%s = %q, want %q", name, data, source)
		}
	}
}

func TestPackIsDeterministic(t *testing.T) {
	root := folder(t)
	first, err := Pack(root)
	if err != nil {
		t.Fatalf("pack: %v", err)
	}
	second, err := Pack(root)
	if err != nil {
		t.Fatalf("pack again: %v", err)
	}
	if string(first) != string(second) {
		t.Error("packing the same folder twice produced different archives")
	}
}

func TestVerifyCatchesAStaleArchive(t *testing.T) {
	root := folder(t)
	archive, err := Pack(root)
	if err != nil {
		t.Fatalf("pack: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "one.md"), []byte("edited"), 0o644); err != nil {
		t.Fatalf("edit: %v", err)
	}
	if err := Verify(archive, root); err == nil {
		t.Error("Verify accepted an archive older than the folder")
	}
	if err := os.WriteFile(filepath.Join(root, "five.md"), []byte("new"), 0o644); err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := Verify(archive, root); err == nil {
		t.Error("Verify accepted an archive missing a new page")
	}
}

func TestReadFileRejectsWhatIsNotThere(t *testing.T) {
	root := folder(t)
	archive, err := Pack(root)
	if err != nil {
		t.Fatalf("pack: %v", err)
	}
	packed := New(archive)
	for _, name := range []string{"", "corpus", "corpus/deep", "corpus/.hidden.md", "corpus/_draft.md", "../go.mod"} {
		if _, err := packed.ReadFile(name); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("ReadFile(%q) error = %v, want fs.ErrNotExist", name, err)
		}
	}
}

func TestGlobMatchesTheEmbedPatterns(t *testing.T) {
	root := folder(t)
	archive, err := Pack(root)
	if err != nil {
		t.Fatalf("pack: %v", err)
	}
	packed := New(archive)
	matched, err := packed.Glob("corpus/*.md")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(matched) != 2 || matched[0] != "corpus/one.md" || matched[1] != "corpus/two.md" {
		t.Errorf("Glob = %v, want the two top-level pages", matched)
	}
	if _, err := packed.Glob("corpus/[.md"); err == nil {
		t.Error("Glob accepted a malformed pattern")
	}
}
