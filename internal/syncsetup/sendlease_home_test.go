package syncsetup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSendLeaseI1EmptyHomeCannotWriteRelativeHold(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	s := &Sync{}
	for name, action := range map[string]func() error{
		"read":   func() error { _, _, err := s.SendLease("chat"); return err },
		"record": func() error { return s.recordSendLease("chat", sendLease{Cell: "chat", Fence: 1}) },
		"forget": func() error { return s.forgetSendLease("chat", sendLease{Cell: "chat", Fence: 1}) },
	} {
		if err := action(); err == nil || !strings.Contains(err.Error(), "home") {
			t.Errorf("%s with empty home = %v, want a home error", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "v3")); !os.IsNotExist(err) {
		t.Fatalf("empty home created a relative sync directory: %v", err)
	}
}
