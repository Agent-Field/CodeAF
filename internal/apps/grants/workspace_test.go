package grants

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

func TestWorkspaceRoleChecks(t *testing.T) {
	for _, member := range []string{"mona", "raj", "jm"} {
		for _, method := range []string{"list", "add", "who", "impact", "remove"} {
			t.Run(member+"/"+method, func(t *testing.T) {
				s, f, _, _ := fixture(t, "acme")
				c, err := s.AddWorkspace(testCtx, mona, bridge.AddWorkspaceConnectionParams{Service: "stripe", Who: "all"})
				if err != nil {
					t.Fatal(err)
				}
				caller := svc.Caller{Member: member, Role: svc.RoleViewer}
				switch method {
				case "list":
					out, listErr := s.Workspace(testCtx, caller)
					err = listErr
					if err == nil && (len(out.Connections) != 1 || out.CanEdit != (member != "jm")) {
						t.Fatalf("list=%v", out)
					}
				case "add":
					_, err = s.AddWorkspace(testCtx, caller, bridge.AddWorkspaceConnectionParams{Service: "gmail", Who: "all"})
				case "who":
					_, err = s.SetWho(testCtx, caller, bridge.SetWhoParams{ID: c.ID, Who: "chosen", Apps: []string{"crm"}})
				case "impact":
					_, err = s.RemovalImpact(testCtx, caller, c.ID)
				case "remove":
					err = s.RemoveWorkspace(testCtx, caller, c.ID)
				}
				if member == "jm" && method != "list" {
					assertReason(t, err, bridge.CodeDenied, bridge.ReasonAdminOnly)
					if f.signIns != 1 || len(f.signedOut) != 0 {
						t.Fatal("denied operation changed credentials")
					}
				} else if err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

func TestWorkspaceWhoImpactAndRemoval(t *testing.T) {
	for _, who := range []string{"all", "chosen"} {
		t.Run(who, func(t *testing.T) {
			s, f, audit, hub := fixture(t, "acme")
			params := bridge.AddWorkspaceConnectionParams{Service: "stripe", Who: who}
			if who == "chosen" {
				params.Apps = []string{"crm", "support"}
			}
			c, err := s.AddWorkspace(testCtx, mona, params)
			if err != nil {
				t.Fatal(err)
			}
			if c.Account != "acme live" || len(c.UsedBy) != 2 {
				t.Fatalf("connection=%v", c)
			}
			s.all.Runs = fakeRuns{rows: []bridge.RunState{
				{StartedAt: testTime.Add(-time.Hour).Format(time.RFC3339)},
				{StartedAt: testTime.Add(-2 * time.Hour).Format(time.RFC3339)},
				{StartedAt: testTime.Add(-24 * time.Hour).Format(time.RFC3339)},
				{StartedAt: testTime.Add(-7 * 24 * time.Hour).Format(time.RFC3339)},
				{StartedAt: testTime.Add(time.Hour).Format(time.RFC3339)},
				{StartedAt: testTime.Format(time.RFC3339), Fake: true},
				{StartedAt: "invalid"},
			}}
			impact, err := s.RemovalImpact(testCtx, mona, c.ID)
			if err != nil {
				t.Fatal(err)
			}
			if len(impact.Apps) != 2 || impact.Apps[0].For != "read invoices for won deals" || impact.Apps[1].For != "refunds" || impact.Apps[0].RunsThisWeek != 3 {
				t.Fatalf("impact=%v", impact)
			}
			c, err = s.SetWho(testCtx, mona, bridge.SetWhoParams{ID: c.ID, Who: "chosen", Apps: []string{"support"}})
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(c.Apps, []string{"support"}) || len(c.UsedBy) != 1 {
				t.Fatal("chosen apps not replaced")
			}
			_, err = s.Resolve(testCtx, appScope("crm"), "stripe")
			assertReason(t, err, bridge.CodeDenied, bridge.ReasonNotAllowedForApp)
			c, err = s.SetWho(testCtx, mona, bridge.SetWhoParams{ID: c.ID, Who: "all"})
			if err != nil || len(c.Apps) != 0 || len(c.UsedBy) != 2 {
				t.Fatal("all did not clear chosen list")
			}
			if err = s.RemoveWorkspace(testCtx, mona, c.ID); err != nil {
				t.Fatal(err)
			}
			if len(f.signedOut) != 1 || f.signedOut[0] != c.ID {
				t.Fatal("remove did not sign out")
			}
			out, err := s.Workspace(testCtx, mona)
			if err != nil || len(out.Connections) != 0 {
				t.Fatal("removed connection listed")
			}
			need, err := s.Needs(testCtx, appScope("crm"))
			if err != nil {
				t.Fatal(err)
			}
			if need.Connections[2].Service != "stripe" || need.Connections[2].Status != "ask_admin" {
				t.Fatalf("needs=%v", need)
			}
			_, err = s.Resolve(testCtx, appScope("crm"), "stripe")
			assertReason(t, err, bridge.CodeDenied, bridge.ReasonNotAllowedForApp)
			if len(audit.lines) != 4 || audit.lines[0].Action != "connection.add" || audit.lines[1].Action != "connection.who" || audit.lines[3].Activity != "Mona removed Stripe · used by 2 apps" {
				t.Fatalf("audit=%v", audit.lines)
			}
			if len(hub.events) != 4 {
				t.Fatalf("workspace events=%d", len(hub.events))
			}
		})
	}
}

func TestWorkspaceValidation(t *testing.T) {
	for _, tt := range []struct {
		name, who string
		apps      []string
		code      bridge.ErrorCode
	}{
		{name: "unknown who", who: "some", code: bridge.CodeInvalid},
		{name: "empty app", who: "chosen", apps: []string{""}, code: bridge.CodeInvalid},
		{name: "unknown app", who: "chosen", apps: []string{"unknown"}, code: bridge.CodeNotFound},
		{name: "all plus apps", who: "all", apps: []string{"crm"}, code: bridge.CodeInvalid},
		{name: "chosen no apps", who: "chosen"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s, f, _, _ := fixture(t, "acme")
			_, err := s.AddWorkspace(testCtx, mona, bridge.AddWorkspaceConnectionParams{Service: "stripe", Who: tt.who, Apps: tt.apps})
			if tt.code != "" {
				assertReason(t, err, tt.code, "")
				if f.signIns != 0 {
					t.Fatal("invalid request began auth")
				}
			} else if err != nil {
				t.Fatal(err)
			}
		})
	}
	s, _, _, _ := fixture(t, "acme")
	c, err := s.AddWorkspace(testCtx, mona, bridge.AddWorkspaceConnectionParams{Service: "stripe", Who: "all"})
	if err != nil {
		t.Fatal(err)
	}

	_, err = s.SetWho(testCtx, mona, bridge.SetWhoParams{ID: c.ID, Who: "no"})
	assertReason(t, err, bridge.CodeInvalid, "")
	_, err = s.SetWho(testCtx, mona, bridge.SetWhoParams{ID: "missing", Who: "all"})
	assertReason(t, err, bridge.CodeNotFound, "")
	_, err = s.RemovalImpact(testCtx, mona, "missing")
	assertReason(t, err, bridge.CodeNotFound, "")
	err = s.RemoveWorkspace(testCtx, mona, "missing")
	assertReason(t, err, bridge.CodeNotFound, "")
}

func TestNeedsCounts(t *testing.T) {
	for _, tt := range []struct {
		name, space               string
		role                      svc.Role
		setup                     string
		gmailStatus, stripeStatus string
		n, of                     int
	}{
		{name: "workspace not yet", space: "acme", role: svc.RoleEditor, gmailStatus: "not_yet", stripeStatus: "ask_admin", n: 0, of: 4},
		{name: "three allowed", space: "acme", role: svc.RoleEditor, setup: "three", gmailStatus: "allowed", stripeStatus: "ask_admin", n: 3, of: 4},
		{name: "user no count", space: "acme", role: svc.RoleUser, setup: "three", gmailStatus: "allowed", stripeStatus: "ask_admin"},
		{name: "expired grant", space: "acme", role: svc.RoleEditor, setup: "expired", gmailStatus: "expired", stripeStatus: "ask_admin", n: 1, of: 4},
		{name: "shared account", space: "acme", role: svc.RoleEditor, setup: "workspace", gmailStatus: "not_yet", stripeStatus: "set_by_admin", n: 0, of: 4},
		{name: "chosen other app", space: "acme", role: svc.RoleEditor, setup: "chosen", gmailStatus: "not_yet", stripeStatus: "ask_admin", n: 0, of: 4},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s, _, _, _ := fixture(t, tt.space)
			switch tt.setup {
			case "three":
				for _, member := range []string{"mona", "raj", "jm"} {
					allow(t, s, member, "crm", "gmail", "token-"+member)
				}
			case "expired":
				c := bridge.GrantCredential{Kind: "grant", Token: "token", ExpiresAt: testTime.Add(time.Hour).Format(time.RFC3339)}
				if _, err := s.Allow(testCtx, appScope("crm"), bridge.AllowParams{Service: "gmail", Answer: "allow", Credential: &c}); err != nil {
					t.Fatal(err)
				}
				s.now = func() time.Time { return testTime.Add(2 * time.Hour) }
			case "workspace", "chosen":
				p := bridge.AddWorkspaceConnectionParams{Service: "stripe", Who: "all"}
				if tt.setup == "chosen" {
					p.Who = "chosen"
					p.Apps = []string{"support"}
				}
				if _, err := s.AddWorkspace(testCtx, mona, p); err != nil {
					t.Fatal(err)
				}
			}
			scope := appScope("crm")
			scope.Caller.Role = tt.role
			out, err := s.Needs(testCtx, scope)
			if err != nil {
				t.Fatal(err)
			}
			if len(out.Connections) != 3 {
				t.Fatalf("needs=%v", out)
			}
			gmail, stripe := out.Connections[1], out.Connections[2]
			if gmail.Service != "gmail" || gmail.Level != "personal" || gmail.Label != "as each person" || gmail.Status != tt.gmailStatus || stripe.Level != "workspace" || stripe.Label != "acme workspace" || stripe.Status != tt.stripeStatus {
				t.Fatalf("needs=%v", out)
			}
			if tt.role == svc.RoleEditor {
				if gmail.Allowed == nil || gmail.Allowed.N != tt.n || gmail.Allowed.Of != tt.of {
					t.Fatalf("allowed=%v", gmail.Allowed)
				}
			} else if gmail.Allowed != nil {
				t.Fatal("User received editor counts")
			}
		})
	}
}

func TestPersonalDefaults(t *testing.T) {
	s := New(svc.Config{Root: t.TempDir() + "/apps"}, &svc.Services{})
	defer s.Close()
	if s.space() != "personal" || s.roleOf(svc.Caller{Member: "anyone"}) != "owner" || s.admin(svc.Caller{}) != nil {
		t.Fatal("Personal seam defaults changed")
	}
	// The absent spaces writer must be harmless and must not create an audit file.
	if err := s.audit(context.Background(), mona, "crm", "connector.allow", "gmail", "allow", ""); err != nil {
		t.Fatal(err)
	}
}

func TestWorkspaceResolveSelectsEligibleAccount(t *testing.T) {
	s, f, _, _ := fixture(t, "acme")
	for _, app := range []string{"support", "crm"} {
		c, err := s.AddWorkspace(testCtx, mona, bridge.AddWorkspaceConnectionParams{Service: "gmail", Who: "chosen", Apps: []string{app}})
		if err != nil {
			t.Fatal(err)
		}
		f.workspace[c.ID] = bridge.GrantCredential{Kind: "grant", Token: app + "-account"}
	}
	for _, app := range []string{"support", "crm"} {
		cred, err := s.Resolve(testCtx, appScope(app), "gmail")
		if err != nil || cred.Token != app+"-account" {
			t.Fatal("did not select eligible workspace account")
		}
	}
}
