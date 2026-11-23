package keys_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/keys"
)

func TestDeletedReturnsVaultReadError(t *testing.T) {
	home := t.TempDir()
	vault, err := keys.Open(home)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "vault.enc"), []byte("damaged vault"), 0600); err != nil {
		t.Fatal(err)
	}
	if gone, err := vault.Deleted("missing-slot"); err == nil || gone {
		t.Fatalf("Deleted = %v, %v; want false and a vault read error", gone, err)
	}
}
