package session_test

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/session"
)

func TestOwnershipPrototypeRequiresOptInAndAbsoluteProfile(t *testing.T) {
	absolute := t.TempDir()
	for _, check := range []struct {
		name, flag, profile string
		want                bool
	}{{"disabled", "0", absolute, false}, {"unset", "", absolute, false}, {"missing profile", "1", "", false}, {"relative profile", "1", "relative-profile", false}, {"explicit profile", "1", absolute, true}} {
		t.Run(check.name, func(t *testing.T) {
			t.Setenv("CODEAF_PROTOTYPE_WORKSPACE_OWNERSHIP", check.flag)
			t.Setenv("CODEAF_HOME", check.profile)
			if got := session.WorkspaceOwnershipPrototypeEnabled(); got != check.want {
				t.Fatalf("prototype enabled=%v want %v", got, check.want)
			}
		})
	}
}
