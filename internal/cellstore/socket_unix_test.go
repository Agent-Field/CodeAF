//go:build !windows

package cellstore

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestU1LongHomesStillUseOneManagedDaemon proves U1/U4: a valid long home
// must not force seals into an unmanaged CLI daemon or exceed AF_UNIX limits.
func TestU1LongHomesStillUseOneManagedDaemon(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir()+strings.Repeat("/nested", 30))
	first := socketFor("/engine/furrow")
	if len(first) >= maxSocketBytes || first != socketFor("/engine/furrow") {
		t.Fatalf("unstable or long address: %s", first)
	}
	t.Setenv("CODEAF_HOME", t.TempDir()+strings.Repeat("/nested", 30))
	if first == socketFor("/engine/furrow") {
		t.Fatal("different homes shared a private socket")
	}
}

// TestR8UnixDeepTempSocketBudget proves F2 when both home and temp are long.
func TestR8UnixDeepTempSocketBudget(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", "")
	base := t.TempDir()
	for range 20 {
		base = filepath.Join(base, "subdir")
	}
	if err := os.MkdirAll(base, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMPDIR", base)
	t.Setenv("CODEAF_HOME", filepath.Join(base, "home"))
	first := socketFor("/engine/furrow-a")
	if len(first) >= 100 || first != socketFor("/engine/furrow-a") {
		t.Fatalf("F2: unstable or over-budget socket: %q (%d bytes)", first, len(first))
	}
	if first == socketFor("/engine/furrow-b") {
		t.Fatal("F2: short fallback lost build identity")
	}
	t.Setenv("CODEAF_HOME", filepath.Join(base, "second-home"))
	if first == socketFor("/engine/furrow-a") {
		t.Fatal("F2: short fallback lost home identity")
	}
}
