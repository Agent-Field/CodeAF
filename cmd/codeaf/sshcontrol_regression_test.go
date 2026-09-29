package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/enginehost"
)

func TestSSHControlPathFitsOpenSSHTemporarySuffix(t *testing.T) {
	const hashLength = 40
	const temporarySuffix = 17
	prefix, err := os.MkdirTemp("/tmp", "s")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(prefix) })
	rootLength := enginehost.SocketLimit - len("/v3/ssh/ctl-") - hashLength
	root := filepath.Join(prefix, strings.Repeat("a", rootLength-len(prefix)-1))
	t.Setenv("CODEAF_HOME", root)
	if got := sshControlPath(); got != "" {
		t.Fatalf("control path %q was accepted without room for OpenSSH's %d-byte temporary suffix", got, temporarySuffix)
	}
}
