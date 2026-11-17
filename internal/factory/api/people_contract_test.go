package api

import (
	"encoding/json"
	"github.com/Agent-Field/codeaf/internal/factory"
	"testing"
)

func TestPeopleAndOwnersStayContractOne(t *testing.T) {
	if Contract != 2 {
		t.Fatalf("contract = %d", Contract)
	}
	q := FromSnapshot(factory.Fixture(at), "santosh")
	if q.People["priya"].Name != "Priya Nair" || q.People["priya"].AvatarURL == "" || len(q.Jobs[1].Owners) != 1 || q.Jobs[1].Owners[0] != "priya" {
		t.Fatalf("fixture people = %+v owners %v", q.People, q.Jobs[1].Owners)
	}
	for _, handle := range []string{"olu", "mei"} {
		if _, ok := q.People[handle]; !ok {
			t.Errorf("queue omitted %s", handle)
		}
	}
	raw, err := json.Marshal(Person{Handle: "priya", Role: "member", GitHubPerson: factory.GitHubPerson{PersonSummary: q.People["priya"], HTMLURL: "https://github.com/priya"}, LastSeen: &at, Owns: []Ownership{{Repo: "acme/api", Path: "/api/"}}, AssignedNow: 3, WaitingOn: 2, Questions: 1, Machines: []Machine{}})
	if err != nil {
		t.Fatal(err)
	}
	assertKeys(t, raw, "factory", "handle", "name", "avatar_url", "html_url", "role", "last_seen", "owns", "assigned_now", "waiting_on", "questions", "machines", "login", "you", "operator", "status", "runners")
	var decoded Person
	if err := json.Unmarshal(raw, &decoded); err != nil || decoded.AssignedNow != 3 || decoded.WaitingOn != 2 || decoded.Role != "member" {
		t.Fatalf("card = %+v %v", decoded, err)
	}
}
