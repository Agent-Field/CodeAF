package config

import (
	"github.com/Agent-Field/codeaf/internal/router"
	"testing"
	"time"
)

// A missing connection is no evidence that a new credential can retry. This
// also protects a task's cached failure when source resolution is unavailable.
func TestAnUnknownCurrentCredentialCannotProveARefusalRepaired(t *testing.T) {
	dir := t.TempDir()
	failure := router.CrewRouteOutcome{Provider: "not-connected", Kind: "auth", Cred: "known-old-dispatch-print", At: time.Now().Add(-time.Minute)}
	if !CrewRefusalApplies(dir, failure) {
		t.Fatal("unknown/disconnected route was treated as a proven new credential")
	}
}
