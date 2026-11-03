package main

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/lane"
)

func TestR2_M1_WorkstreamWakeResolvesToStandingRole(t *testing.T) {
	if got := roleOf("workstream-wake"); got != lane.RoleStanding {
		t.Fatalf("workstream-wake resolved to %q, want %q", got, lane.RoleStanding)
	}
}
