package session

import (
	"path/filepath"
	"testing"
)

func TestPathCodecRoundTripsAgainstAnotherMachine(t *testing.T) {
	sealed := codecFor("/a/session", "/a/ws")
	ref, ok := sealed.encode("/a/ws/sub/dir")
	if !ok || ref != "workspace:sub/dir" {
		t.Fatalf("encode = %q, %v", ref, ok)
	}
	moved := codecFor("/b/session", "/b/elsewhere/ws")
	if got, ok := moved.resolve(ref); !ok || got != filepath.FromSlash("/b/elsewhere/ws/sub/dir") {
		t.Errorf("resolve = %q, %v", got, ok)
	}
}

func TestPathCodecPrefersTheSmallestRemainder(t *testing.T) {
	c := codecFor("/h/ws/.sessions/s1", "/h/ws")
	if ref, _ := c.encode("/h/ws/.sessions/s1/trees/a"); ref != "session:trees/a" {
		t.Errorf("session path spelled %q", ref)
	}
}

func TestPathCodecRefusesWhatHasNoBase(t *testing.T) {
	c := codecFor("/a/session", "/a/ws")
	if ref, ok := c.encode("/mnt/data/x"); ok {
		t.Errorf("a path under no base was spelled %q", ref)
	}
	if got, ok := c.resolve("nowhere:x"); ok {
		t.Errorf("an unknown base resolved to %q", got)
	}
}

func TestPathCodecKeepsPathsAnOlderBuildWrote(t *testing.T) {
	if got, ok := codecFor("/a/s", "/a/ws").resolve("/legacy/abs"); !ok || got != "/legacy/abs" {
		t.Errorf("resolve = %q, %v", got, ok)
	}
}
