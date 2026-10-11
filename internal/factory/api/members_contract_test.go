package api

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// Members, sessions and the empty-queue reason are additive. The fixture
// floor still encodes with no reason field, and Contract stays 1.
func TestMembersSessionsAndVisibilityStayContractOne(t *testing.T) {
	if Contract != 1 {
		t.Fatalf("contract = %d", Contract)
	}
	member, err := json.Marshal(Member{
		Handle: "priya", Name: "Priya Nair", AvatarURL: "https://avatars.githubusercontent.com/u/42",
		JoinedAt: at, LastSeen: at, Sessions: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	assertKeys(t, member, "handle", "name", "avatar_url", "joined_at", "last_seen", "sessions")

	session, err := json.Marshal(Session{ID: "session_fixture", Device: "this device", Created: at, LastSeen: at, Current: false})
	if err != nil {
		t.Fatal(err)
	}
	assertKeys(t, session, "id", "device", "created", "last_seen", "current")
	if !strings.Contains(string(session), `"current":false`) {
		t.Fatalf("current must stay present when false: %s", session)
	}

	raw, err := json.Marshal(FromSnapshot(factory.Fixture(at), "santosh"))
	if err != nil {
		t.Fatal(err)
	}
	var q Queue
	if err := json.Unmarshal(raw, &q); err != nil {
		t.Fatal(err)
	}
	if q.Reason != "" || len(q.Jobs) == 0 {
		t.Fatalf("fixture decode = reason %q, %d jobs", q.Reason, len(q.Jobs))
	}
	annotated := strings.TrimSuffix(strings.TrimSpace(string(raw)), "}") + `,"reason":"no_visible_repos"}`
	if err := json.Unmarshal([]byte(annotated), &q); err != nil {
		t.Fatal(err)
	}
	if q.Reason != ReasonNoVisibleRepos || len(q.Jobs) == 0 || q.Me != "santosh" {
		t.Fatalf("reason hid the floor: %+v", q.Reason)
	}
}

func assertKeys(t *testing.T, raw []byte, keys ...string) {
	t.Helper()
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	if len(fields) != len(keys) {
		t.Fatalf("fields = %s", raw)
	}
	for _, key := range keys {
		if _, ok := fields[key]; !ok {
			t.Fatalf("missing %s in %s", key, raw)
		}
	}
}
