package vaultsync

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/keys"
)

// TestK3WindowsWithheldFileKeepsRecordedMode proves K3/K4: an incoming
// withheld file's bytes can change on NTFS without replacing its recorded Unix
// mode. An unchanged arrival is not recorded as an edit; new files get 0644.
func TestK3WindowsWithheldFileKeepsRecordedMode(t *testing.T) {
	r := newRig(t)
	m := r.machine()
	root := t.TempDir()
	c := Carried{ID: "incoming", Scope: "test", Name: ".env", Medium: fileMedium{root: root, path: filepath.Join(root, ".env")}}
	original := encodeFile(0755, []byte("before\r\n"))
	at := time.Unix(1000, 0)
	must(t, m.vault.PutAt(c.ID, keys.Entry{Name: c.Name, Scope: c.Scope, Value: original}, at))
	must(t, c.restore(m.vault))
	must(t, c.capture(m.vault))
	if got := m.value(t, c.ID); got != original {
		t.Fatalf("K3 unchanged incoming metadata changed: %s", got)
	}
	must(t, os.WriteFile(filepath.Join(root, ".env"), []byte("after\r\n"), 0600))
	must(t, c.capture(m.vault))
	mode, data, err := decodeFile(m.value(t, c.ID))
	must(t, err)
	if mode != 0755 || string(data) != "after\r\n" {
		t.Fatalf("K3 edit mode %o bytes %q", mode, data)
	}
	fresh := Carried{ID: "new", Scope: "test", Name: "new.env", Medium: fileMedium{root: root, path: filepath.Join(root, "new.env")}}
	must(t, os.WriteFile(filepath.Join(root, "new.env"), []byte("new"), 0600))
	must(t, fresh.capture(m.vault))
	mode, _, err = decodeFile(m.value(t, fresh.ID))
	must(t, err)
	if mode != 0644 {
		t.Fatalf("K3 new file mode %o", mode)
	}
}
