//go:build !windows

package app

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/seniordev/netpolicy"
)

func TestPolicyDisabledTools(t *testing.T) {
	if names := policyDisabledTools(netpolicy.Policy{Mode: netpolicy.ModeAllow}); names != nil {
		t.Fatalf("allow mode should disable nothing, got %v", names)
	}
	names := policyDisabledTools(netpolicy.Policy{Mode: netpolicy.ModeOff})
	if len(names) != 2 || names[0] != "webfetch" || names[1] != "websearch" {
		t.Fatalf("off mode should hide both web tools, got %v", names)
	}
}
