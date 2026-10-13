package apps

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appapi"
	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
	"github.com/coder/websocket"
)

func TestBridgePersonalSpaces(t *testing.T) {
	c, all := dialPersonalSpaces(t)
	entry, err := all.Audit.Append(c.ctx, bridge.AuditEntry{Member: svc.Owner, Action: "records.update", App: "crm", Decision: "allow"})
	if err != nil {
		t.Fatal(err)
	}
	var info bridge.SpaceInfo
	if err := c.call("", bridge.MethodSpaceInfo, bridge.Empty{}, &info); err != nil {
		t.Fatal(err)
	}
	if info.ID != svc.PersonalSpace || info.Name != "Personal" || info.Kind != "personal" || info.Me.ID != svc.Owner || info.Me.Role != string(svc.WorkspaceOwner) || info.Apps != 1 {
		t.Fatalf("space.info = %+v", info)
	}
	var audit bridge.AuditListResult
	if err := c.call("", bridge.MethodAuditList, bridge.AuditListParams{App: "crm", Limit: 1}, &audit); err != nil {
		t.Fatal(err)
	}
	if len(audit.Entries) != 1 || audit.Entries[0].Seq != entry.Seq || !audit.Entries[0].At.Equal(entry.At) || audit.Entries[0].Action != entry.Action || audit.Next != 0 {
		t.Fatalf("audit.list = %+v", audit)
	}
	for _, topic := range []string{bridge.TopicSpaceMembers, bridge.TopicSpaceActivity, bridge.TopicSpaceWaiting, bridge.TopicRoles("crm"), bridge.TopicSignoff("crm")} {
		id := c.send(bridge.Envelope{Type: bridge.TypeSub, Topic: topic})
		if reply := c.read(); reply.ID != id || reply.Type != bridge.TypeResult {
			t.Fatalf("subscribe %s = %+v", topic, reply)
		}
	}
}

func dialPersonalSpaces(t *testing.T) (*client, *svc.Services) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	api := appapi.New(testToken, appapi.Options{Context: ctx})
	t.Cleanup(api.Close)
	all := Wire(ctx, t.TempDir())
	Mount(ctx, api, all)
	server := httptest.NewServer(api.Handler())
	t.Cleanup(server.Close)
	url := "ws" + strings.TrimPrefix(server.URL, "http") + bridge.Path
	conn, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{Subprotocols: []string{bridge.Subprotocol, bridge.BearerProtocol + testToken}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.CloseNow() })
	return &client{t: t, ctx: ctx, conn: conn}, all
}
