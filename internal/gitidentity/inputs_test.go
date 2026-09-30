package gitidentity

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// AN INPUT IS LEFT ALONE WHILE IT HOLDS THE BYTES IT WAS COPIED IN WITH, when it
// is gone, and before its fingerprint was taken; a changed one is not, and a
// path that was never an input is never left alone in this sense. The list
// survives the disk, whatever bytes a path holds.
func TestAnInputIsLeftAloneOnlyWhileItIsAsItWasCopied(t *testing.T) {
	root := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, name)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("parser.py", "half\n")
	write("credentials.json", "secret\n")
	write("odd/na\nme.txt", "odd\n")
	in := Inputs{"unfinished": ""}
	for _, name := range []string{"parser.py", "credentials.json", "odd/na\nme.txt"} {
		in[name], _ = Fingerprint(filepath.Join(root, name))
	}
	file := filepath.Join(t.TempDir(), "inputs")
	if err := WriteInputs(file, in); err != nil {
		t.Fatal(err)
	}
	read, err := ReadInputs(file)
	if err != nil || len(read) != len(in) || read["odd/na\nme.txt"] != in["odd/na\nme.txt"] {
		t.Fatalf("the list read back as %v (%v)", read, err)
	}
	write("parser.py", "finished\n")
	if err := os.Remove(filepath.Join(root, "credentials.json")); err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]bool{
		"parser.py": false, "credentials.json": true, "odd/na\nme.txt": true, "unfinished": true, "made.go": false,
	} {
		if got := read.LeftAlone(root, path); got != want {
			t.Fatalf("LeftAlone(%q) = %v, want %v", path, got, want)
		}
	}
	if changed := read.Changed(root); !slices.Equal(changed, []string{"parser.py"}) {
		t.Fatalf("changed = %q, want parser.py alone", changed)
	}
	if none, err := ReadInputs(""); err != nil || none != nil {
		t.Fatalf("no list read as %v (%v)", none, err)
	}
}
