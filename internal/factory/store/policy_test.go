package store

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory/acl"
)

// Contract A1, C1. A revision is shared across store instances, and a side file has no effect.
func TestOrgPolicyPersistenceAndRevision(t *testing.T) {
	root := t.TempDir()
	reader, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	writer, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	p, err := reader.OrgPolicy()
	if err != nil || !reflect.DeepEqual(p, acl.DefaultPolicy()) {
		t.Fatalf("initial policy = %+v, %v", p, err)
	}
	if err := os.WriteFile(filepath.Join(root, "org-policy.json"), []byte(`{"rev":900}`), 0o600); err != nil {
		t.Fatal(err)
	}
	p.Operator.SeesPrivate = true
	saved, err := writer.SavePolicy(p, 0)
	if err != nil || saved.Rev != 1 {
		t.Fatalf("save = %+v, %v", saved, err)
	}
	got, err := reader.OrgPolicy()
	if err != nil || !reflect.DeepEqual(got, saved) {
		t.Fatalf("read = %+v, %v", got, err)
	}
	got.Roles[0].Verbs[0] = "other"
	got, err = reader.OrgPolicy()
	if err != nil || got.Roles[0].Verbs[0] != acl.VerbView {
		t.Fatalf("a caller changed the cached revision: %+v, %v", got, err)
	}
	var stale *ErrPolicyStale
	if _, err := writer.SavePolicy(p, 0); !errors.As(err, &stale) || stale.Rev != 1 {
		t.Fatalf("stale save = %v", err)
	}
	db, err := writer.wsDB(false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE org_policy SET doc = '{', rev = 2`); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		got, err = reader.OrgPolicy()
		if err != nil || got.Rev != 2 || got.Operator.SeesPrivate {
			t.Fatalf("corrupt policy = %+v, %v", got, err)
		}
	}
}

// Contract C2, D2. Role removal cannot strand a live grant or a policy default.
func TestOrgPolicyRejectsRemovingRolesInUse(t *testing.T) {
	st, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	p := acl.DefaultPolicy()
	p.Roles = append(p.Roles, acl.Role{Name: "runner", Verbs: []string{acl.VerbView, acl.VerbRun}})
	saved, err := st.SavePolicy(p, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddGrant(acl.Grant{Subject: acl.Subject{Kind: "person", ID: "reader"},
		Resource: acl.Resource{Kind: "workstream", ID: "work-1"}, Role: "runner"}); err != nil {
		t.Fatal(err)
	}
	var invalid *acl.ValidationError
	if _, err := st.SavePolicy(acl.DefaultPolicy(), saved.Rev); !errors.As(err, &invalid) {
		t.Fatalf("removing an in-use role = %v", err)
	}
}
