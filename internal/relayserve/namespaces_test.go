package relayserve

import (
	"os"
	"testing"
	"time"
)

// No id that is not "id_" plus 32 lowercase hex may become a path.
func TestNamespacesRefuseIdsThatAreNotIdentities(t *testing.T) {
	root := t.TempDir()
	n := newNamespaces(root, time.Now)
	defer n.Close()
	for _, id := range []string{
		"", "../escape", "id_../../etc", "id_" + "0123456789abcdef0123456789abcde", // one short
		"id_" + "0123456789ABCDEF0123456789abcdef", "id_0123456789abcdef0123456789abcdef/x", "/abs",
	} {
		if _, err := n.directory(id); err == nil {
			t.Errorf("directory(%q) opened", id)
		}
		if _, err := n.blobs(id); err == nil {
			t.Errorf("blobs(%q) opened", id)
		}
	}
	if entries, _ := os.ReadDir(root); len(entries) != 0 {
		t.Fatalf("a refused id created %v", entries)
	}
}
