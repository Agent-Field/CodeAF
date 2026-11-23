package vaultsync

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/keys"
)

// THE VAULT CARRIES SECRETS, NOT FILES OF ANY SIZE (keys.MaxSlotValue). A chat
// whose folder was the codeaf home withheld graph.db, a git pack and three
// binaries, and carrying them made a 3.3 GB vault that cost 22 seconds of CPU
// per chat flush. A withheld file too large for a slot stays on its own
// computer; a small one beside it still travels; and nothing deletes either.
func TestAWithheldFileTooLargeForTheVaultStaysWhereItIs(t *testing.T) {
	r := newRig(t)
	a, b := r.machine(), r.machine()
	wa, wb := a.chatFolder(t), b.chatFolder(t)
	big := strings.Repeat("x", keys.MaxSlotValue) // its slot value would be a third larger
	old := time.Now().Add(-time.Hour)
	wa.save(t, "graph.db", big, 0o600, old)
	wa.save(t, ".env", "A=1\n", 0o600, old)
	wb.save(t, "graph.db", "b's own\n", 0o600, old)

	carry(t, a, b)
	wb.want(t, ".env", "A=1\n", 0o600)
	wb.want(t, "graph.db", "b's own\n", 0o600)
	if _, err := a.vault.Get("home:file:chat/graph.db"); err == nil {
		t.Fatal("a file larger than a vault slot was carried into the vault")
	}
	if gone, _ := a.vault.Deleted("home:file:chat/graph.db"); gone {
		t.Fatal("a file too large to carry was tombstoned, which would delete it on the other computer")
	}

	// And back the other way: b's push and a's pull leave a's large file alone.
	must(t, b.Push(ctx))
	must(t, a.Pull(ctx))
	wa.want(t, "graph.db", big, 0o600)
	wa.want(t, ".env", "A=1\n", 0o600)
}
