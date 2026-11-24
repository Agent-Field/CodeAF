package store

import (
	"regexp"
	"slices"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/acl"
)

// Contract B1, B2, F1. Registrations and every grant row survive reopening the database.
func TestGrantRegistryPersists(t *testing.T) {
	root := t.TempDir()
	st, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	ref := acl.Resource{Kind: "chat", ID: "chat-1"}
	person := acl.Subject{Kind: "person", ID: "reader"}
	row := Resource{Resource: ref, Parent: acl.Resource{Kind: "workstream", ID: "work-1"},
		Owner: acl.Subject{Kind: "person", ID: "writer"}, Visibility: acl.Owner, Label: "Project chat", CreatedAt: now}
	if err := st.PutResource(row); err != nil {
		t.Fatal(err)
	}
	expired := now.Add(-time.Second)
	added, err := st.AddGrant(acl.Grant{Subject: person, Role: "view", Resource: ref, ExpiresAt: &expired,
		GrantedBy: acl.Principal{Kind: "agent", ID: "assistant", Sponsor: "writer"}})
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^g_[0-9A-Za-z]{26}$`).MatchString(added.ID) {
		t.Fatalf("grant id = %q", added.ID)
	}
	st, err = Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if got, found, err := st.Resource(ref.Kind, ref.ID); err != nil || !found || got != row {
		t.Fatalf("resource = %+v, %v, %v", got, found, err)
	}
	on, err := st.GrantsOn(ref.Kind, ref.ID)
	if err != nil || len(on) != 1 || on[0].ID != added.ID || on[0].Live(now) || on[0].GrantedBy != added.GrantedBy {
		t.Fatalf("grants = %+v, %v", on, err)
	}
	many, err := st.GrantsOnMany([]acl.Resource{ref, {Kind: "canvas", ID: "irrelevant"}})
	if err != nil || len(many[ref]) != 1 {
		t.Fatalf("batch = %+v, %v", many, err)
	}
	subject, err := st.GrantsOfSubject(person.Kind, person.ID)
	if err != nil || len(subject) != 1 {
		t.Fatalf("subject grants = %+v, %v", subject, err)
	}
	refs, err := st.GrantedResources()
	if err != nil || !slices.Equal(refs, []acl.Resource{ref}) {
		t.Fatalf("granted resources = %+v, %v", refs, err)
	}
	if err := st.DeleteResource(ref.Kind, ref.ID); err != nil {
		t.Fatal(err)
	}
	if on, err := st.GrantsOn(ref.Kind, ref.ID); err != nil || len(on) != 0 {
		t.Fatalf("grants after deletion = %+v, %v", on, err)
	}
}

// Contract C4. The initial grant and chat registration succeed or fail together.
func TestResourceRegistrationRollsBackWithGrant(t *testing.T) {
	st, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ref := acl.Resource{Kind: "chat", ID: "chat-1"}
	row := Resource{Resource: ref, CreatedAt: time.Now(), Visibility: acl.Org}
	forbidden := acl.Grant{Subject: acl.Subject{Kind: "agent", ID: "assistant"}, Role: "view"}
	if err := st.RegisterResource(row, &forbidden, 0, nil); err == nil {
		t.Fatal("stored a grant to an agent")
	}
	if _, found, err := st.Resource(ref.Kind, ref.ID); err != nil || found {
		t.Fatalf("registration persisted despite failure: found = %v, err = %v", found, err)
	}
}

// Contract F1. A chat is registered inside its workstream and goes with it,
// grants included, so no label is left showing for a conversation that is gone.
func TestRemovingAWorkstreamRemovesTheChatsRegisteredInIt(t *testing.T) {
	st, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.EditWorkstreams(func(all factory.OrgWorkstreams) ([]string, error) {
		all["ws_a"] = factory.OrgWorkstream{Name: "A"}
		all["ws_b"] = factory.OrgWorkstream{Name: "B"}
		return []string{"ws_a", "ws_b"}, nil
	}); err != nil {
		t.Fatal(err)
	}
	inA := acl.Resource{Kind: "chat", ID: "chat-a"}
	inB := acl.Resource{Kind: "chat", ID: "chat-b"}
	for _, row := range []struct {
		ref, parent acl.Resource
	}{{inA, acl.Resource{Kind: "workstream", ID: "ws_a"}}, {inB, acl.Resource{Kind: "workstream", ID: "ws_b"}}} {
		if err := st.PutResource(Resource{Resource: row.ref, Parent: row.parent, Visibility: acl.Org, CreatedAt: time.Now()}); err != nil {
			t.Fatal(err)
		}
		if _, err := st.AddGrant(acl.Grant{Subject: acl.Subject{Kind: "org", ID: "*"}, Role: "view", Resource: row.ref}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := st.EditWorkstreams(func(all factory.OrgWorkstreams) ([]string, error) {
		delete(all, "ws_a")
		return []string{"ws_a"}, nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, found, err := st.Resource(inA.Kind, inA.ID); err != nil || found {
		t.Fatalf("the removed workstream's chat is still registered: found = %v, err = %v", found, err)
	}
	if on, err := st.GrantsOn(inA.Kind, inA.ID); err != nil || len(on) != 0 {
		t.Fatalf("the removed workstream's chat keeps grants: %+v, %v", on, err)
	}
	if _, found, err := st.Resource(inB.Kind, inB.ID); err != nil || !found {
		t.Fatalf("another workstream's chat was removed: found = %v, err = %v", found, err)
	}
	if on, err := st.GrantsOn(inB.Kind, inB.ID); err != nil || len(on) != 1 {
		t.Fatalf("another workstream's chat lost its grant: %+v, %v", on, err)
	}
}
