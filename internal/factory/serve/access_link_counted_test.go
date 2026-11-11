package serve

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// COUNTED IS SAID FOR A HUMAN ROW ONLY, AND TRUE ONLY FOR A LINK WITH ITS OWN
// LIMIT: a legacy row reads max_uses 1 either way, so this is the one place a
// client can tell what revoking it will do to the people already in.
func TestLinkRowsSayWhetherTheyAreCountedForHumanRowsOnly(t *testing.T) {
	f, _ := linkFixture(t)
	counted, _ := f.makeLink(t, api.CreateAccessLink{MaxUses: 3})
	if counted.Counted == nil || !*counted.Counted {
		t.Fatalf("a new link is counted: %+v", counted)
	}
	old := `{"tokens":[
	 {"access_link":true,"id":"tok_legacy","token":"legacy-open","created":"2026-10-01T00:00:00Z","expires":"2026-10-30T00:00:00Z"},
	 {"access_link":true,"operator":true,"id":"tok_operator","token":"claim","created":"2026-10-01T00:00:00Z"},
	 {"id":"tok_script","token":"script-key","created":"2026-10-01T00:00:00Z"}]}`
	if err := os.WriteFile(f.cfg.TokensPath, []byte(old), 0600); err != nil {
		t.Fatal(err)
	}
	raw := f.request(t, "GET", "/v1/access/links", "operator-secret", nil, 200, nil)
	var doc struct {
		Links []map[string]json.RawMessage `json:"links"`
	}
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, row := range doc.Links {
		var id string
		_ = json.Unmarshal(row["id"], &id)
		got[id] = string(row["counted"])
	}
	if got["tok_legacy"] != "false" {
		t.Fatalf("legacy link: counted = %q", got["tok_legacy"])
	}
	if got["tok_operator"] != "" || got["tok_script"] != "" {
		t.Fatalf("a row that is not a person's invitation says counted: %v", got)
	}
}

// THE FACTORY SAYS WHAT IT IS CONFIGURED TO DO, SO NO CLIENT PROBES GITHUB.
func TestMeSaysWhetherGitHubSignInAndInvitationsAreAvailable(t *testing.T) {
	f, _ := linkFixture(t)
	read := func() api.Me {
		var me api.Me
		raw := f.request(t, "GET", "/v1/me", "operator-secret", nil, 200, &me)
		if me.GitHubSignInAvailable != strings.Contains(raw, `"github_sign_in_available":true`) {
			t.Fatalf("wire and type disagree: %s", raw)
		}
		return me
	}
	if me := read(); !me.GitHubSignInAvailable || !me.InviteAvailable {
		t.Fatalf("a configured factory: %+v", me)
	}
	tokensPath := f.h.config.TokensPath
	f.h.config.TokensPath = ""
	if me := read(); !me.GitHubSignInAvailable || me.InviteAvailable {
		t.Fatalf("no tokens file: %+v", me)
	}
	f.h.config.TokensPath = tokensPath
	f.h.config.GitHubClientID = ""
	raw := f.request(t, "GET", "/v1/me", "operator-secret", nil, 200, nil)
	if strings.Contains(raw, "github_sign_in_available") || strings.Contains(raw, "invite_available") {
		t.Fatalf("an unconfigured factory states availability: %s", raw)
	}
}
