package keys

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

func scanFixture(t *testing.T) string {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("root reads files whatever their mode")
	}
	root := t.TempDir()
	for rel, mode := range map[string]os.FileMode{"odd.bin": 0, "fine.txt": 0o644, "locked/a.txt": 0o644, ".env": 0} {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		os.Chmod(p, mode)
	}
	os.Chmod(filepath.Join(root, "locked"), 0)
	t.Cleanup(func() { os.Chmod(filepath.Join(root, "locked"), 0o755) })
	return root
}

func TestScannerNamesPathsItCannotRead(t *testing.T) {
	root := scanFixture(t)
	var got []Finding
	got = append(got, Scanner{}.Walk(root)...)
	sort.Slice(got, func(i, j int) bool { return got[i].Path < got[j].Path })
	want := []Finding{{".env", RuleDotenv}, {"locked", RuleUnreadable}, {"odd.bin", RuleUnreadable}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("findings %+v, want %+v", got, want)
	}
}

func TestUnreadableIsNeverNotedClean(t *testing.T) {
	root := scanFixture(t)
	s := Scanner{Ledger: NewLedger()}
	s.Walk(root)
	if again := s.Paths(root, []string{"odd.bin"}); len(again) != 1 || again[0].Rule != RuleUnreadable {
		t.Fatalf("second scan %+v: an unreadable file was remembered as clean", again)
	}
}
