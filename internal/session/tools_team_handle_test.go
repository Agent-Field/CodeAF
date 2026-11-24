package session

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/teams"
)

// The manager is told the handle range the store accepts, from the same
// constants, so a typed limit change reaches team_start's schema.
func TestTeamStartSchemaNamesTheHandleLimits(t *testing.T) {
	var schema struct {
		Properties map[string]struct {
			Description string `json:"description"`
		} `json:"properties"`
	}
	if err := json.Unmarshal([]byte(teamStartSchema), &schema); err != nil {
		t.Fatalf("team_start's schema is not JSON: %v", err)
	}
	want := fmt.Sprintf("%d to %d lowercase letters, digits or hyphens", teams.HandleMin, teams.HandleMax)
	if got := schema.Properties["handle"].Description; !strings.HasPrefix(got, want) {
		t.Fatalf("handle description = %q, want it to start %q", got, want)
	}
}
