package enginehost

import (
	"bytes"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
	"testing"
)

// C10 requires relay changes and off to change host compatibility.
func TestC10SyncURLChangesMustChangeHostEnvironment(t *testing.T) {
	t.Setenv(config.ProfileDirEnv, t.TempDir())
	workspace := t.TempDir()
	salt := make([]byte, 32)
	t.Setenv("CODEAF_SYNC_URL", "http://127.0.0.1:41001")
	initial := EnvironmentMAC(salt, workspace)
	for _, target := range []string{"http://127.0.0.1:41002", "off"} {
		t.Setenv("CODEAF_SYNC_URL", target)
		if bytes.Equal(initial, EnvironmentMAC(salt, workspace)) {
			t.Errorf("changing CODEAF_SYNC_URL to %q leaves the host environment fingerprint unchanged", target)
		}
	}
}

// C10 resolves the saved pairing address before comparing, so equal relay
// choices stay compatible regardless of which source named them.
func TestC10HostFingerprintUsesResolvedSyncSetting(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CODEAF_HOME", dir)
	t.Setenv(config.ProfileDirEnv, t.TempDir())
	t.Setenv("CODEAF_SYNC_URL", "http://127.0.0.1:41001")
	workspace, salt := t.TempDir(), make([]byte, 32)
	initial := EnvironmentMAC(salt, workspace)
	if err := syncsetup.SaveRelayURL(dir, "http://127.0.0.1:41001"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEAF_SYNC_URL", "")
	if !bytes.Equal(initial, EnvironmentMAC(salt, workspace)) {
		t.Fatal("the same saved relay changed compatibility")
	}
	if err := syncsetup.SaveRelayURL(dir, "http://127.0.0.1:41002"); err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(initial, EnvironmentMAC(salt, workspace)) {
		t.Fatal("a changed saved relay left compatibility unchanged")
	}
	t.Setenv("CODEAF_SYNC_URL", "OFF")
	off := EnvironmentMAC(salt, workspace)
	t.Setenv("CODEAF_SYNC_URL", "off")
	if !bytes.Equal(off, EnvironmentMAC(salt, workspace)) {
		t.Fatal("equivalent off spellings changed compatibility")
	}
}
