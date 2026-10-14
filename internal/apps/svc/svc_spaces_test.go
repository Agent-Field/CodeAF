package svc

import (
	"context"
	"testing"
)

func TestCallerHelpers(t *testing.T) {
	for _, tt := range []struct {
		name   string
		caller Caller
		admin  bool
		shape  bool
	}{
		{"personal", PersonalCaller(), true, true},
		{"admin user", Caller{WS: WorkspaceAdmin, Role: RoleUser}, true, false},
		{"member editor", Caller{WS: WorkspaceMember, Role: RoleEditor}, false, true},
		{"agent editor", Caller{WS: WorkspaceOwner, Role: RoleEditor, Agent: true}, true, false},
		{"unknown", Caller{}, false, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if tt.caller.IsAdmin() != tt.admin || tt.caller.CanShape() != tt.shape {
				t.Fatalf("IsAdmin=%v CanShape=%v", tt.caller.IsAdmin(), tt.caller.CanShape())
			}
		})
	}
	if got := PersonalCaller(); got.Member != Owner || got.Space != PersonalSpace || got.WS != WorkspaceOwner || got.Role != RoleEditor || got.Agent {
		t.Fatalf("PersonalCaller = %+v", got)
	}
}

func TestRoleAtLeast(t *testing.T) {
	roles := []Role{RoleViewer, RoleUser, RoleEditor, "", "unknown"}
	for i, role := range roles {
		for j, other := range roles {
			t.Run(string(role)+"/"+string(other), func(t *testing.T) {
				want := i < 3 && j < 3 && i >= j
				if got := role.AtLeast(other); got != want {
					t.Fatalf("%q.AtLeast(%q) = %v, want %v", role, other, got, want)
				}
			})
		}
	}
}

func TestCallerContext(t *testing.T) {
	ctx := context.Background()
	if _, ok := CallerFrom(ctx); ok {
		t.Fatal("empty context has a caller")
	}
	for _, caller := range []Caller{PersonalCaller(), {Member: "raj", Space: "acme", WS: WorkspaceMember}, {}} {
		got, ok := CallerFrom(WithCaller(ctx, caller))
		if !ok || got != caller {
			t.Fatalf("caller = %+v, %v, want %+v", got, ok, caller)
		}
	}
}
