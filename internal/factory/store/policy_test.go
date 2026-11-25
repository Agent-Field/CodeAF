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

// Contract: who may move a chat is owner-only until the organization says otherwise. A policy document stored before the
// policy had a `chats` section reads as owner-only; one that sets the rule reads as set; and a stored word that is not one
// of the two is not trusted, so the default applies.
func TestOrgPolicyStoredWithoutChatRulesReadsAsOwnerOnlyMove(t *testing.T) {
	const roles = `"roles":[{"name":"view","verbs":["view"]},{"name":"comment","verbs":["view","comment"]},` +
		`{"name":"edit","verbs":["view","comment","edit","run","steer","answer"]},` +
		`{"name":"full","verbs":["view","comment","edit","run","steer","answer","share","manage"]}],` +
		`"owner_role":"full","org_role":"view","defaults":{"chat":{"visibility":"private"}},"sharing":{"who":"share_verb"},` +
		`"operator":{"role":"view","may_govern":true},"agents":{"may_create":true,"default_scope":"full"}`
	st, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.SavePolicy(acl.DefaultPolicy(), 0); err != nil {
		t.Fatal(err)
	}
	db, err := st.wsDB(false)
	if err != nil {
		t.Fatal(err)
	}
	for i, tc := range []struct{ name, doc, want string }{
		{"stored before the section existed", `{` + roles + `}`, acl.MoveByOwner},
		{"set to manage", `{` + roles + `,"chats":{"move_by":"manage"}}`, acl.MoveByManage},
		{"set to owner", `{` + roles + `,"chats":{"move_by":"owner"}}`, acl.MoveByOwner},
		{"set to a word nobody defined", `{` + roles + `,"chats":{"move_by":"anyone"}}`, acl.MoveByOwner},
	} {
		if _, err := db.Exec(`UPDATE org_policy SET doc = ?, rev = ?`, tc.doc, 10+i); err != nil {
			t.Fatal(err)
		}
		got, err := st.OrgPolicy()
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if got.Rev != 10+i || got.Chats.MoveByFor() != tc.want {
			t.Errorf("%s: revision %d, who moves a chat = %q, want %q", tc.name, got.Rev, got.Chats.MoveByFor(), tc.want)
		}
	}
}

// Contract: who approves a tool call and who starts a chat that is not running keep today's behaviour until the
// organization says otherwise. A policy stored before the settings existed reads as owner approves and anyone who can see
// the chat starts it; one that sets them reads as set; and a stored word that is not one of the allowed words is not
// trusted, so the defaults apply.
func TestOrgPolicyStoredWithoutApprovalAndWakeRulesReadsAsToday(t *testing.T) {
	const roles = `"roles":[{"name":"view","verbs":["view"]},{"name":"comment","verbs":["view","comment"]},` +
		`{"name":"edit","verbs":["view","comment","edit","run","steer","answer"]},` +
		`{"name":"full","verbs":["view","comment","edit","run","steer","answer","share","manage"]}],` +
		`"owner_role":"full","org_role":"view","defaults":{"chat":{"visibility":"private"}},"sharing":{"who":"share_verb"},` +
		`"operator":{"role":"view","may_govern":true},"agents":{"may_create":true,"default_scope":"full"}`
	st, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.SavePolicy(acl.DefaultPolicy(), 0); err != nil {
		t.Fatal(err)
	}
	db, err := st.wsDB(false)
	if err != nil {
		t.Fatal(err)
	}
	for i, tc := range []struct{ name, doc, wantApproval, wantWake string }{
		{"stored before the settings existed", `{` + roles + `,"chats":{"move_by":"manage"}}`, acl.ApprovalsByOwner, acl.WakeByView},
		{"set", `{` + roles + `,"chats":{"approvals_by":"answer","wake_by":"run"}}`, acl.ApprovalsByAnswer, acl.WakeByRun},
		{"wake set to owner", `{` + roles + `,"chats":{"wake_by":"owner"}}`, acl.ApprovalsByOwner, acl.WakeByOwner},
		{"set to words nobody defined", `{` + roles + `,"chats":{"approvals_by":"anyone","wake_by":"anyone"}}`, acl.ApprovalsByOwner, acl.WakeByView},
	} {
		if _, err := db.Exec(`UPDATE org_policy SET doc = ?, rev = ?`, tc.doc, 20+i); err != nil {
			t.Fatal(err)
		}
		got, err := st.OrgPolicy()
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if got.Rev != 20+i || got.Chats.ApprovalsByFor() != tc.wantApproval || got.Chats.WakeByFor() != tc.wantWake {
			t.Errorf("%s: revision %d, approvals %q, wake %q, want %q and %q", tc.name, got.Rev, got.Chats.ApprovalsByFor(), got.Chats.WakeByFor(), tc.wantApproval, tc.wantWake)
		}
	}
}
