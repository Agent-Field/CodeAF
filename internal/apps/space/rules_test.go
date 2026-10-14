package space

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

func TestRoleRules(t *testing.T) {
	last := "You're the last Owner. Make someone else Owner first, or delete acme in General."
	only := "only an Owner can make or unmake an Owner"
	for _, tt := range []struct {
		name        string
		actor, from svc.WorkspaceRole
		to          svc.WorkspaceRole
		owners      int
		want        string
	}{
		{"owner promotes", svc.WorkspaceOwner, svc.WorkspaceMember, svc.WorkspaceOwner, 1, ""},
		{"admin cannot make owner", svc.WorkspaceAdmin, svc.WorkspaceMember, svc.WorkspaceOwner, 1, only},
		{"admin cannot unmake owner", svc.WorkspaceAdmin, svc.WorkspaceOwner, svc.WorkspaceAdmin, 1, only},
		{"last owner stays", svc.WorkspaceOwner, svc.WorkspaceOwner, svc.WorkspaceAdmin, 1, last},
		{"admin sets member", svc.WorkspaceAdmin, svc.WorkspaceMember, svc.WorkspaceAdmin, 1, ""},
		{"owner cannot invite another owner", svc.WorkspaceOwner, "", svc.WorkspaceOwner, 1, "invite someone as Admin or Member"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var err error
			if tt.name == "owner cannot invite another owner" {
				err = canInvite(tt.actor, tt.to)
			} else {
				err = canSetRole(tt.actor, tt.from, tt.to, tt.owners, "acme")
			}
			if tt.want == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || err.Error() == "" || !contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestInviteLinkRoundTrip(t *testing.T) {
	link := buildInvite("http://127.0.0.1:8443", "inv_ab", "cdcd")
	host, id, secret, err := parseInvite(link)
	if err != nil || host != "127.0.0.1:8443" || id != "inv_ab" || secret != "cdcd" {
		t.Fatalf("parse %s = %s %s %s %v", link, host, id, secret, err)
	}
	if originFor(host) != "http://127.0.0.1:8443" {
		t.Fatal(originFor(host))
	}
	if originFor("acme.codeaf.dev") != "https://acme.codeaf.dev" {
		t.Fatal(originFor("acme.codeaf.dev"))
	}
	_, _, _, err = parseInvite("https://example.com/nope")
	var be *bridge.Error
	if err == nil || !asBridge(err, &be) || be.Code != bridge.CodeInvalid {
		t.Fatal(err)
	}
}

func contains(got, want string) bool {
	return len(got) >= len(want) && (got == want || len(want) == 0 || (len(got) > 0 && (got == "conflict: "+want || got == "denied: "+want || got == "invalid: "+want || got[len(got)-len(want):] == want)))
}

func asBridge(err error, dst **bridge.Error) bool {
	if err == nil {
		return false
	}
	be, ok := err.(*bridge.Error)
	if ok {
		*dst = be
	}
	return ok
}
