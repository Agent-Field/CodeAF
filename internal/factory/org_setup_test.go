package factory

import (
	"encoding/json"
	"strings"
	"testing"
)

// A CARD IS READ AS IT IS SAID: words trimmed, a team named only on an invite
// joins the card's teams, a team's people carry it onto their invites, and
// names a retry finds things by are never doubled.
func TestAnOrgSetupCardIsReadAndHeldToItsLimits(t *testing.T) {
	c, err := ParseOrgSetupCard(json.RawMessage(`{"workstreams":[{"name":"  Company ","people":["me","me"],"inside":[{"name":"Hiring"}]}],
		"teams":[{"name":"Engineering team","people":["Sam"]}],
		"invites":[{"name":"Sam"},{"name":"Lena","team":"Design"}],"by":"mallory"}`))
	if err != nil {
		t.Fatal(err)
	}
	if c.Workstreams[0].Name != "Company" || len(c.Workstreams[0].People) != 1 || c.By != "" || c.InsideCount() != 1 {
		t.Fatalf("read: %+v", c)
	}
	if c.Invites[0].Team != "Engineering team" || len(c.Teams) != 2 || c.Teams[1].Name != "Design" {
		t.Fatalf("teams: %+v invites: %+v", c.Teams, c.Invites)
	}
	for _, bad := range []struct{ card, says string }{
		{`{"workstreams":[]}`, "no workstreams and nobody to invite"},
		{`{"workstreams":[{"name":"A"},{"name":"a"}]}`, "two workstreams are called a"},
		{`{"workstreams":[{"name":"A","inside":[{"name":"B"},{"name":"b"}]}]}`, "two workstreams inside A"},
		{`{"workstreams":[{"name":"A"}],"invites":[{"name":"Sam","email":"not an address"}]}`, "not an email address"},
		{`{"workstreams":[{"name":"A"}],"invites":[{"name":"Sam"},{"name":"sam"}]}`, "invited twice"},
		{`{"workstreams":[{"name":"A"}],"invites":[{"name":"me"}]}`, "not invited"},
		{`{"workstreams":[{"name":"A","tasks":[]}]}`, "invalid arguments"},
	} {
		if _, err := ParseOrgSetupCard(json.RawMessage(bad.card)); err == nil || !strings.Contains(err.Error(), bad.says) {
			t.Errorf("%s: %v, want %q", bad.card, err, bad.says)
		}
	}
}
