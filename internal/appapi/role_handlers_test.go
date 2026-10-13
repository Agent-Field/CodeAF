package appapi

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/teams"
)

func makeTestRole(t *testing.T, f *agentFixture) CrewRole {
	t.Helper()
	var role CrewRole
	crewTestRead(t, f.request(t, "POST", "/app/v1/roles", `{"name":"Review","instructions":"Review diffs","model":"test/model","effort":"high"}`), 201, &role)
	return role
}
func TestRolesDurableCopiesEditsAndMembershipLaw(t *testing.T) {
	f := newAgentFixture(t)
	role := makeTestRole(t, f)
	crews := []Crew{}
	for _, name := range []string{"Release", "Pricing"} {
		manager := crewTestAgent(t, f, name+" manager")
		helper := crewTestAgent(t, f, name+" helper")
		crews = append(crews, crewTestMake(t, f, name, manager, helper))
		crewTestRead(t, f.request(t, "POST", "/app/v1/crews/"+crews[len(crews)-1].ID+"/members", fmt.Sprintf(`{"role_id":%q}`, role.ID)), 201, nil)
	}
	var rows RolesResponse
	crewTestRead(t, f.request(t, "GET", "/app/v1/roles?q=Review", ""), 200, &rows)
	if len(rows.Roles) != 1 || len(rows.Roles[0].Copies) != 2 || rows.Roles[0].Copies[0].MemberID == rows.Roles[0].Copies[1].MemberID {
		t.Fatal(rows)
	}
	copy := rows.Roles[0].Copies[0]
	crewTestRead(t, f.request(t, "POST", "/app/v1/crews/"+copy.CrewID+"/members", fmt.Sprintf(`{"role_id":%q}`, role.ID)), 409, nil)
	firstAgent := readAgent(t, f.request(t, "GET", "/app/v1/agents/"+copy.MemberID, ""), 200)
	path := "/app/v1/roles/" + role.ID
	crewTestRead(t, f.request(t, "PATCH", path, `{"instructions":"Flag migrations","name":"Migration review","model":"test/new","effort":"low","expect":{"version":1}}`), 200, &role)
	if len(role.Copies) != 2 || role.Instructions != "Flag migrations" {
		t.Fatal(role)
	}
	for _, copy := range role.Copies {
		log, err := teams.ReadTraffic(f.home, copy.CrewID, "", 0)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, e := range log {
			if e.ToKey != "" && strings.Contains(e.Text, "Flag migrations") {
				found = true
			}
		}
		if !found {
			t.Fatal("copy did not receive its next-step instructions")
		}
		agent := readAgent(t, f.request(t, "GET", "/app/v1/agents/"+copy.MemberID, ""), 200)
		if agent.Model != "test/new" || agent.Effort != "low" {
			t.Fatal(agent)
		}
	}
	after := readAgent(t, f.request(t, "GET", "/app/v1/agents/"+copy.MemberID, ""), 200)
	if after.ID != firstAgent.ID {
		t.Fatal("role edit replaced a conversation")
	}
	crewTestRead(t, f.request(t, "PATCH", path, `{"instructions":"Stale","expect":{"version":1}}`), 409, nil)
	store := storedCrewRoles{home: f.home}
	saved, err := store.ReadCrewRole(context.Background(), role.ID)
	if err != nil || saved.Instructions != "Flag migrations" {
		t.Fatalf("saved=%+v err=%v", saved, err)
	}
	crewTestRead(t, f.request(t, "POST", "/app/v1/crews/"+copy.CrewID+"/members/"+copy.MemberID+"/move", fmt.Sprintf(`{"crew_id":%q}`, role.Copies[1].CrewID)), 409, nil)
}
func TestRoleValidationSecurityAndPOSTReplay(t *testing.T) {
	f := newAgentFixture(t)
	for _, body := range []string{`null`, `{}`, `{"name":null}`, `{"unknown":1}`, `{} {}`, `{"name":"Review","instructions":"x","model":"x","effort":"max"}`} {
		crewTestRead(t, f.request(t, "POST", "/app/v1/roles", body), 422, nil)
	}
	crewTestRead(t, f.request(t, "PATCH", "/app/v1/roles/missing", `{}`), 404, nil)
	crewTestRead(t, request(t, f.server, "POST", "/app/v1/roles", `{}`, nil, "127.0.0.1:4000"), 401, nil)
	headers := bearer()
	headers.Set("Idempotency-Key", "make-role")
	body := `{"name":"Review","instructions":"x","model":"test/model"}`
	first := request(t, f.server, "POST", "/app/v1/roles", body, headers, "127.0.0.1:4000")
	second := request(t, f.server, "POST", "/app/v1/roles", body, headers, "127.0.0.1:4000")
	crewTestRead(t, first, 201, nil)
	crewTestRead(t, second, 201, nil)
	if first.Body.String() != second.Body.String() {
		t.Fatal("role POST duplicated on replay")
	}
	var list RolesResponse
	crewTestRead(t, f.request(t, "GET", "/app/v1/roles", ""), 200, &list)
	if len(list.Roles) != 1 || list.Roles[0].Effort != "medium" {
		t.Fatal(list)
	}
	crewTestRead(t, request(t, f.server, "POST", "/app/v1/roles", `{}`, headers, "127.0.0.1:4000"), 409, nil)
}
