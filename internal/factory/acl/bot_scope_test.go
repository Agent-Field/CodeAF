package acl

import (
	"slices"
	"testing"
	"time"
)

func TestContractC6bOnlyNarrowsTheCurrentAgentAndSponsorVerbs(t *testing.T) {
	p := DefaultPolicy()
	p.Agents.Scopes = map[string]string{"bot:bt_example": "edit"}
	item := Item{OwnerKind: "person", OwnerID: "sponsor"}
	caller := Caller{Handle: "sponsor", Member: true, Agent: &Agent{ID: "bot:bt_example", Sponsor: "sponsor"}}
	caller.Agent.Only = []string{VerbView, VerbShare}
	if got := p.Verbs(caller, item, time.Time{}).List(); !slices.Equal(got, []string{VerbView}) {
		t.Fatalf("verbs = %v, want view", got)
	}
	if got := p.RoleOf(caller, item, time.Time{}); got != "view" {
		t.Fatalf("role = %q, want view", got)
	}
	caller.Agent.Only = []string{}
	if got := p.RoleOf(caller, item, time.Time{}); got != "" {
		t.Fatalf("role = %q for an empty limit", got)
	}
	caller.Agent.Only = nil
	if !p.Allowed(caller, item, VerbEdit, time.Time{}) {
		t.Fatal("unspecified limit removed the existing edit permission")
	}
	item.OwnerID = "another-person"
	if got := p.Verbs(caller, item, time.Time{}).List(); len(got) != 0 {
		t.Fatalf("verbs = %v after the sponsor loses access", got)
	}
}
