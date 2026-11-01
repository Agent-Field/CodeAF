package main

import (
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"

	"github.com/Agent-Field/codeaf/internal/devname"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/session"
)

// DESIGN 1 requires the ordinary chat launch to find the paired identity in
// the same home that serve uses, without a separate profile override.
func TestConversationTaskComputersUseTheDefaultHomeWhenTheProfileIsUnset(t *testing.T) {
	_ = pairedTaskDefaultHome(t)
	computers := v3TaskComputers("")
	if computers == nil || computers.ThisComputer() != "chat computer" {
		t.Fatal("the normal profile hides paired task computers")
	}
}

// DESIGN 1 also applies over the ordinary engine connection; disabling an
// unrelated run lane must not remove the task placement that /task --on uses.
func TestConnectedConversationsKeepPairedTaskPlacement(t *testing.T) {
	profile := pairedTaskDefaultHome(t)
	cfg, _ := v3Shape(session.Config{ProfileDir: profile}, v3LanesOverWire())
	if cfg.TaskComputers == nil || cfg.TaskComputers.ThisComputer() != "chat computer" {
		t.Fatal("the connected conversation hides paired task placement")
	}
}

func pairedTaskDefaultHome(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("paired tasks require Linux or macOS")
	}
	profile := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEAF_HOME", profile)
	t.Setenv("CODEAF_PROFILE_DIR", "")
	t.Setenv("CODEAF_TELEMETRY", "off")
	t.Setenv("DO_NOT_TRACK", "1")
	server := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(server.Close)
	t.Setenv("CODEAF_SYNC_URL", server.URL)
	if _, err := identity.Ensure(profile); err != nil {
		t.Fatal(err)
	}
	if _, err := devname.Set(profile, "chat computer"); err != nil {
		t.Fatal(err)
	}
	return profile
}
