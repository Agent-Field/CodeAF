//go:build !windows

package cellstore

import (
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
