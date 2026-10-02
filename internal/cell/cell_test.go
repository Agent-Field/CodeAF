package cell

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestCreateOpenRoundTrip(t *testing.T) {
	home := t.TempDir()
	base := &Base{Remote: "git@example.com:a/b.git", SHA: "6e1f0c2a"}
	c, err := Create(home, Options{Class: Sandboxed, Base: base})
	if err != nil {
		t.Fatal(err)
	}
	if !ValidID(c.ID) {
		t.Fatalf("bad id %q", c.ID)
	}
	got, err := Open(home, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Meta().Class != Sandboxed || *got.Meta().Base != *base || got.Meta().V != 1 {
		t.Fatalf("meta = %+v", got.Meta())
	}
	for _, rel := range []string{EnvPath, MetaPath} {
		p, _ := got.Path(rel)
		if _, err := os.Stat(p); err != nil {
			t.Errorf("%s: %v", rel, err)
		}
	}
}

func TestPathRejects(t *testing.T) {
	c := Cell{Root: "/cells/x"}
	for _, tc := range []struct {
		rel string
		ok  bool
	}{
		{".cell/meta.json", true},
		{"a/../b", true},
		{"", false},
		{"/etc/passwd", false},
		{"..", false},
		{"../other", false},
		{"a/../../other", false},
	} {
		_, err := c.Path(tc.rel)
		if (err == nil) != tc.ok {
			t.Errorf("Path(%q) err=%v, want ok=%v", tc.rel, err, tc.ok)
		}
	}
}

func TestOpenRejectsBadID(t *testing.T) {
	for _, id := range []string{"", "../x", "short", strings.Repeat("Z", 26)} {
		if _, err := Open(t.TempDir(), id); err == nil {
			t.Errorf("Open(%q) succeeded", id)
		}
	}
}

func TestCreateRejectsBadMeta(t *testing.T) {
	for _, o := range []Options{{}, {Class: "weird"}, {Class: FilesOnly, KeyID: "nothex"}} {
		if _, err := Create(t.TempDir(), o); err == nil {
			t.Errorf("Create(%+v) succeeded", o)
		}
	}
}

func TestWriteMetaAtomic(t *testing.T) {
	home := t.TempDir()
	c, err := Create(home, Options{Class: FilesOnly})
	if err != nil {
		t.Fatal(err)
	}
	m := c.Meta()
	m.Class = HostBound
	if err := c.WriteMeta(m); err != nil {
		t.Fatal(err)
	}
	if err := c.WriteMeta(Meta{}); err == nil {
		t.Fatal("invalid meta written")
	}
	got, _ := Open(home, c.ID)
	if got.Meta().Class != HostBound {
		t.Fatalf("class = %s", got.Meta().Class)
	}
	dir := filepath.Dir(got.abs(MetaPath))
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("leftover %s", e.Name())
		}
	}
}

func TestMetaHasNoAbsolutePaths(t *testing.T) {
	home := t.TempDir()
	c, err := Create(home, Options{Class: FilesOnly})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(c.abs(MetaPath))
	if strings.Contains(string(raw), home) || regexp.MustCompile(`"/[^"]`).Match(raw) {
		t.Fatalf("absolute path in meta: %s", raw)
	}
}

func TestIDsSortByTime(t *testing.T) {
	a, _ := NewID(time.UnixMilli(1_000_000))
	b, _ := NewID(time.UnixMilli(2_000_000))
	if !ValidID(a) || !ValidID(b) || a >= b {
		t.Fatalf("ids %s %s", a, b)
	}
}

func TestEnabled(t *testing.T) {
	for v, want := range map[string]bool{"": true, "0": false, "1": true} {
		t.Setenv(EnvVar, v)
		if Enabled() != want {
			t.Errorf("%q: want %v", v, want)
		}
	}
}
