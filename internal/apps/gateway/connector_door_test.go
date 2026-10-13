package gateway

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/connect"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

func TestConnectorActivityMethodsUseDoor(t *testing.T) {
	ctx := context.Background()
	all := &svc.Services{Connect: stubConnectServices()}
	door := connect.New(svc.Config{Root: filepath.Join(t.TempDir(), "apps")}, all)
	defer door.Close()
	all.Connectors = door
	scope := ownerScope("crm")
	scope.Data = svc.DataSample
	if _, err := door.Call(connect.WithInvocation(ctx, "run-1", "read"), scope, "gmail.read", map[string]any{"id": "message"}); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct{ method, params string }{{bridge.MethodConnectFakes, `{"run_id":"run-1"}`}, {bridge.MethodConnectWaits, `{}`}} {
		t.Run(tt.method, func(t *testing.T) {
			result, err := methods[tt.method].run(ctx, all, scope, json.RawMessage(tt.params))
			if err != nil {
				t.Fatal(err)
			}
			if tt.method == bridge.MethodConnectFakes {
				calls := result.(bridge.ConnectFakesResult).Calls
				if len(calls) != 1 || calls[0].RunID != "run-1" {
					t.Fatalf("result=%+v", result)
				}
			} else if len(result.(bridge.ConnectWaitsResult).Waits) != 0 {
				t.Fatalf("result=%+v", result)
			}
		})
	}
}
