package serve

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory/acl"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// titledList is the hosted list as a client reads it, with each row kept as raw fields so a test can tell a title that
// is absent from one that is empty.
type titledList struct {
	Sessions []map[string]json.RawMessage `json:"sessions"`
	Shared   []map[string]json.RawMessage `json:"shared"`
	Hosting  map[string]json.RawMessage   `json:"hosting"`
}

// titleOf says what a row's title is, and whether the row has the field at all.
func titleOf(t *testing.T, rows []map[string]json.RawMessage, chat string) (title string, present bool) {
	t.Helper()
	for _, row := range rows {
		var id string
		if json.Unmarshal(row["id"], &id) != nil || id != chat {
			continue
		}
		raw, present := row["title"]
		if present && json.Unmarshal(raw, &title) != nil {
			t.Fatalf("the title of %s is not a string: %s", chat, raw)
		}
		return title, present
	}
	t.Fatalf("no row for %s among %d rows", chat, len(rows))
	return "", false
}

// Contract S1, S2 and S4: every row of the hosted list, the caller's own and shared, carries the chat's title as a
// string when the chat has one and has no title field at all when it does not. Listing asks the chats' host for rows
// and for nothing else, so no chat is opened, started or woken to get a name.
func TestS1S2_ListRowsCarryTheTitleWhenThereIsOneAndNoFieldWhenThereIsNot(t *testing.T) {
	rg := newHostedRig(t)
	named := rg.create(t, "abir")
	unnamed := rg.create(t, "abir")
	sharedNamed := rg.create(t, "santosh")
	sharedUnnamed := rg.create(t, "santosh")
	hostedGrant(t, rg, "santosh", sharedNamed, "abir", "view")
	hostedGrant(t, rg, "santosh", sharedUnnamed, "abir", "view")
	rg.host.setTitle(named, "Fix the login redirect")
	rg.host.setTitle(sharedNamed, "Plan the Q3 launch")

	before := rg.host.count()
	var list titledList
	rg.do(t, "abir", http.MethodGet, hostedSessionsPath, -1, nil, http.StatusOK, &list)

	for _, tc := range []struct {
		rows  []map[string]json.RawMessage
		chat  string
		title string
	}{
		{list.Sessions, named, "Fix the login redirect"},
		{list.Sessions, unnamed, ""},
		{list.Shared, sharedNamed, "Plan the Q3 launch"},
		{list.Shared, sharedUnnamed, ""},
	} {
		got, present := titleOf(t, tc.rows, tc.chat)
		if present != (tc.title != "") || got != tc.title {
			t.Errorf("the row for %s has title %q (field present %v), want %q", tc.chat, got, present, tc.title)
		}
	}
	for _, id := range []string{named, unnamed, sharedNamed, sharedUnnamed} {
		if opened := rg.host.wireRequests(id); opened != 0 {
			t.Errorf("listing opened a wire to %s %d times, which would have started it", id, opened)
		}
	}
	for _, req := range rg.host.requests()[before:] {
		if req.method != http.MethodGet || req.path != "/v1/sessions" {
			t.Errorf("listing made the call %s %s to the chats' host, and only the list is allowed", req.method, req.path)
		}
	}
}

// Contract S3: a title that changes in the chat is what the list says on its next read, with the chat never reopened.
// The organization's server holds no copy of its own to go stale: it reads the chats' host, which keeps the title
// from the chat's own wire (internal/sessionhost, TestS3).
func TestS3_ARenamedChatIsListedUnderItsNewTitleOnTheNextRead(t *testing.T) {
	rg := newHostedRig(t)
	chat := rg.create(t, "abir")
	for _, title := range []string{"Untangle the billing job", "Untangle the billing job, then ship it"} {
		rg.host.setTitle(chat, title)
		var list titledList
		rg.do(t, "abir", http.MethodGet, hostedSessionsPath, -1, nil, http.StatusOK, &list)
		if got, _ := titleOf(t, list.Sessions, chat); got != title {
			t.Fatalf("after the chat was renamed %q the list says %q", title, got)
		}
	}
	if opened := rg.host.wireRequests(chat); opened != 0 {
		t.Fatalf("seeing the new title opened the chat %d times", opened)
	}
}

// Contract S4: a title is visible only to someone who can already read the chat. The operator may be shown a chat's
// metadata (whose it is, when it was used) without being let into it, and what a chat is about is part of it, so that
// row, and the single-chat read behind it, carry no title. Someone the chat is shared with, who can read it, sees it.
func TestS4_AnOperatorsMetadataRowNeverCarriesTheTitle(t *testing.T) {
	rg := newHostedRig(t)
	shared := rg.create(t, "santosh")
	hostedGrant(t, rg, "santosh", shared, "abir", "view")
	rg.setAccessPolicy(t, func(p *acl.Policy) {
		p.Defaults[store.SessionKind] = acl.KindDefault{Visibility: acl.Owner}
	})
	private := rg.create(t, "santosh")
	rg.host.setTitle(shared, "Read by the people it was shared with")
	rg.host.setTitle(private, "Read by nobody but santosh")

	var theirs titledList
	rg.do(t, "abir", http.MethodGet, hostedSessionsPath, -1, nil, http.StatusOK, &theirs)
	if got, present := titleOf(t, theirs.Shared, shared); !present || got != "Read by the people it was shared with" {
		t.Errorf("a person the chat is shared with reads title %q (present %v)", got, present)
	}
	for _, row := range theirs.Shared {
		var id string
		_ = json.Unmarshal(row["id"], &id)
		if id == private {
			t.Errorf("a chat that was not shared appeared in someone's list: %v", row)
		}
	}

	var operators titledList
	rg.do(t, "owner", http.MethodGet, hostedSessionsPath, -1, nil, http.StatusOK, &operators)
	var access string
	if json.Unmarshal(operators.Shared[0]["access"], &access) != nil || access != "operator" {
		t.Fatalf("the operator's row was not an operator row: %v", operators.Shared)
	}
	for _, row := range operators.Shared {
		if _, present := row["title"]; present {
			t.Errorf("the operator's metadata row carries a title: %v", row)
		}
	}
	var one map[string]json.RawMessage
	rg.do(t, "owner", http.MethodGet, hostedSessionsPath+"/"+private, -1, nil, http.StatusOK, &one)
	if _, present := one["title"]; present {
		t.Errorf("the operator's metadata read of one chat carries a title: %v", one)
	}
	// The same read by a person who may read the chat does.
	rg.do(t, "abir", http.MethodGet, hostedSessionsPath+"/"+shared, -1, nil, http.StatusOK, &one)
	var title string
	if json.Unmarshal(one["title"], &title) != nil || title != "Read by the people it was shared with" {
		t.Errorf("the single-chat read for someone the chat is shared with carries title %q", title)
	}
}
