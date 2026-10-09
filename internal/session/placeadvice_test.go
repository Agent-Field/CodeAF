package session

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/placegraph"
	"github.com/Agent-Field/codeaf/internal/roles"
)

// BOTH PLACE ROLES ARE REGISTERED, AND ON THE CHEAP TIER. Registration is what
// turns the desktop's two rows live, and LOW is the tier a person-approved
// offer belongs on.
func TestThePlaceRolesAreRegisteredLow(t *testing.T) {
	for _, role := range []roles.Role{roles.RolePlaceFile, roles.RolePlaceSuggest} {
		tier, ok := roles.TierOf(role)
		if !ok || tier != roles.TierLow {
			t.Errorf("%s resolves on %q (registered %v), want %q", role, tier, ok, roles.TierLow)
		}
	}
}

// THE SEAM IS FOR PLACES ONLY. Any other role, an empty question and a closed
// conversation are refused before a single request is made.
func TestAskPlacesRefusesWithoutACall(t *testing.T) {
	client := &scriptedCompleter{}
	agent, _ := newTestAgent(t, client, func(c *Config) { c.RolesSource = nameSettings() })
	ctx := context.Background()
	for name, req := range map[string]placegraph.ModelRequest{
		"another role": {Role: roles.RoleTitle, System: "s", User: "u"},
		"no role":      {System: "s", User: "u"},
		"no system":    {Role: roles.RolePlaceFile, User: "u"},
		"no question":  {Role: roles.RolePlaceSuggest, System: "s", User: "  "},
	} {
		if got, err := agent.AskPlaces(ctx, req); err == nil || got != (PlacesAnswer{}) {
			t.Errorf("%s: answered %+v, %v", name, got, err)
		}
	}
	if err := agent.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := agent.AskPlaces(ctx, placegraph.ModelRequest{Role: roles.RolePlaceFile, System: "s", User: "u"}); err == nil {
		t.Error("a closed conversation answered a places question")
	}
	if client.requests() != 0 {
		t.Fatalf("%d requests were made for questions that were refused", client.requests())
	}
}

// ONE CALL ON THE ROLE'S OWN TIER, BILLED OFF THE TURN, and the model that
// answered comes back with the raw answer, so the desktop can say and price
// what was asked. The journal's line names the role it was asked for.
func TestAskPlacesAsksTheRoleOnceAndNamesTheModel(t *testing.T) {
	for _, role := range []roles.Role{roles.RolePlaceFile, roles.RolePlaceSuggest} {
		t.Run(string(role), func(t *testing.T) {
			client := &scriptedCompleter{steps: []step{func(context.Context, []ai.Message) (*ai.Response, error) {
				return textResponse(`{"place":"p1"}`), nil
			}}}
			journal := filepath.Join(t.TempDir(), "session.jsonl")
			agent, _ := newTestAgent(t, client, func(c *Config) {
				c.RolesSource = nameSettings()
				c.SessionFile = journal
			})
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			got, err := agent.AskPlaces(ctx, placegraph.ModelRequest{Role: role, System: "the rules", User: "the chat"})
			if err != nil {
				t.Fatal(err)
			}
			if got.Text != `{"place":"p1"}` || got.Model != "cheap/model" {
				t.Fatalf("answered %+v", got)
			}
			if client.requests() != 1 || client.model(0) != "cheap/model" {
				t.Fatalf("%d requests, first on %q", client.requests(), client.model(0))
			}
			sent := client.seen[0]
			if len(sent) != 2 || sent[0].Role != "system" || sent[1].Role != "user" ||
				!strings.Contains(messageText(sent[0]), "the rules") || !strings.Contains(messageText(sent[1]), "the chat") {
				t.Fatalf("the question went out as %+v", sent)
			}
			if usage := agent.Usage(); usage.Calls != 1 || usage.Turns != 0 {
				t.Fatalf("usage %+v, want one call charged to no turn", usage)
			}
			if err := agent.Close(); err != nil {
				t.Fatal(err)
			}
			var tags []string
			for _, line := range readLines(t, journal) {
				var entry sessionEntry
				if json.Unmarshal([]byte(line), &entry) == nil && entry.Usage != nil && entry.Usage.Role != "" {
					tags = append(tags, entry.Usage.Role)
				}
			}
			if len(tags) != 1 || tags[0] != string(role) {
				t.Fatalf("the journal's usage lines name %v, want one %q", tags, role)
			}
		})
	}
}

// A RUNAWAY ANSWER IS CUT, and cut on a rune boundary so it is still text.
func TestAPlacesAnswerIsCappedOnARuneBoundary(t *testing.T) {
	long := strings.Repeat("é", placesAnswerCap)
	got := capPlacesAnswer(long)
	if len(got) > placesAnswerCap || !strings.HasPrefix(long, got) || !utf8.ValidString(got) {
		t.Fatalf("capped to %d bytes, valid %v", len(got), utf8.ValidString(got))
	}
	if capPlacesAnswer("p1") != "p1" {
		t.Fatal("a short answer was changed")
	}
}
