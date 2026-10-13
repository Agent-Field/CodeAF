package caps

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

func TestConnectTopicPermissions(t *testing.T) {
	for _, method := range []string{"sub", "unsub"} {
		for _, tt := range []struct {
			name, topic, app, member string
			role                     svc.Role
			code                     bridge.ErrorCode
		}{
			{"own allow", "connect/mona/allow", "", "mona", "", "allowed"},
			{"own waits", "connect/mona/waits", "", "mona", "", "allowed"},
			{"workspace member", "connect/workspace", "", "mona", "", "allowed"},
			{"workspace guest", "connect/workspace", "", "", "", bridge.CodeDenied},
			{"another allow", "connect/raj/allow", "", "mona", "", bridge.CodeDenied},
			{"another waits", "connect/raj/waits", "", "mona", "", bridge.CodeDenied},
			{"member wildcard", "connect/*/allow", "", "mona", "", bridge.CodeDenied},
			{"allow suffix", "connect/mona/allow/other", "", "mona", "", bridge.CodeInvalid},
			{"workspace suffix", "connect/workspace/other", "", "mona", "", bridge.CodeInvalid},
			{"fakes editor", "app/crm/fakes", "crm", "mona", svc.RoleEditor, "allowed"},
			{"fakes user", "app/crm/fakes", "crm", "mona", svc.RoleUser, "allowed"},
			{"fakes viewer", "app/crm/fakes", "crm", "mona", svc.RoleViewer, "allowed"},
			{"fakes without app role", "app/crm/fakes", "crm", "mona", "", bridge.CodeDenied},
			{"fakes wrong app", "app/crm/fakes", "other", "mona", svc.RoleEditor, bridge.CodeInvalid},
			{"fakes suffix", "app/crm/fakes/other", "crm", "mona", svc.RoleEditor, bridge.CodeInvalid},
		} {
			t.Run(method+"/"+tt.name, func(t *testing.T) {
				checker := New(svc.Config{Root: t.TempDir()}, nil)
				scope := svc.Scope{App: tt.app, Caller: svc.Caller{Member: tt.member, Role: tt.role}}
				raw, _ := json.Marshal(map[string]string{"topic": tt.topic})
				if err := checker.Allow(context.Background(), scope, method, raw); errorCode(err) != tt.code {
					t.Fatalf("permission=%v, want %s", err, tt.code)
				}
			})
		}
	}
}

func TestConnectExportDenyRule(t *testing.T) {
	for _, tt := range []struct {
		name, app string
		caller    svc.Caller
	}{
		{"app editor", "crm", svc.Caller{Member: svc.Owner, Role: svc.RoleEditor}},
		{"app viewer", "crm", svc.Caller{Member: svc.Owner, Role: svc.RoleViewer}},
		{"agent", "", svc.Caller{Member: svc.Owner, Agent: true}},
		{"other member", "", svc.Caller{Member: "mona"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			checker := New(svc.Config{Root: t.TempDir()}, nil)
			err := checker.Allow(context.Background(), svc.Scope{App: tt.app, Caller: tt.caller}, bridge.MethodConnectCredentialExport, nil)
			if errorCode(err) != bridge.CodeDenied {
				t.Fatalf("export permission=%v", err)
			}
		})
	}
}
