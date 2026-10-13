package connect

import (
	"context"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
	native "github.com/Agent-Field/codeaf/internal/connect"
	"golang.org/x/oauth2"
)

type managerGrants struct {
	svc.Grants
	manager *native.Manager
	seen    svc.Credential
}

func (g *managerGrants) ConnectorManager(_ context.Context, c svc.Credential) (*native.Manager, error) {
	g.seen = c
	return g.manager, nil
}

type keysStub map[string]string

func (k keysStub) Key(service string) (string, bool) { value, ok := k[service]; return value, ok }
func (k keysStub) Set(service, value string)         { k[service] = value }

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestNativeAdaptersUseResolvedCredentialAndClient(t *testing.T) {
	cases := []struct {
		tool, path, method, reply string
		args                      map[string]any
	}{
		{"gmail.read", "/messages/message", "GET", `{"id":"message","snippet":"Hello","payload":{"headers":[{"name":"Subject","value":"Team plan"}]}}`, map[string]any{"id": "message"}},
		{"gmail.search", "/messages", "GET", `{"messages":[]}`, map[string]any{"query": "team", "max": 3}},
		{"gmail.send", "/messages/send", "POST", `{"id":"message"}`, map[string]any{"to": "priya@example.test", "subject": "Team plan", "body": "Hello"}},
		{"calendar.list", "/events", "GET", `{"items":[]}`, map[string]any{"from": "2026-10-13", "to": "2026-10-14"}},
		{"calendar.create", "/events", "POST", `{"id":"event"}`, map[string]any{"title": "Team plan", "start": "2026-10-13T12:00:00Z", "end": "2026-10-13T13:00:00Z"}},
		{"slack.search", "/search.messages", "POST", `{"ok":true,"messages":{"matches":[]}}`, map[string]any{"query": "team"}},
		{"slack.read", "/conversations.replies", "POST", `{"ok":true,"messages":[]}`, map[string]any{"channel": "team", "ts": "123"}},
		{"slack.list", "/conversations.list", "POST", `{"ok":true,"channels":[]}`, nil},
		{"slack.post", "/chat.postMessage", "POST", `{"ok":true,"ts":"123"}`, map[string]any{"channel": "team", "text": "Hello"}},
	}
	for _, tt := range cases {
		t.Run(tt.tool, func(t *testing.T) {
			root := t.TempDir()
			g := &managerGrants{manager: native.NewAppManager(root, nil, keysStub{"google": "fixture-key", "slack": "fixture-key"})}
			all := &svc.Services{Connect: svc.ConnectServices{Grants: g}}
			s := New(svc.Config{Root: filepath.Join(root, "apps")}, all)
			defer s.Close()
			count := 0
			transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
				count++
				if !strings.HasSuffix(r.URL.Path, tt.path) || r.Method != tt.method || r.Header.Get("Authorization") != "Bearer fixture-key" {
					t.Fatalf("unexpected adapter request: %s %s", r.Method, r.URL.Path)
				}
				return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(tt.reply)), Request: r}, nil
			})
			ctx := context.WithValue(context.Background(), oauth2.HTTPClient, &http.Client{Transport: transport})
			service, verb, _ := strings.Cut(tt.tool, ".")
			c := svc.Credential{ID: "workspace-connection", Service: service, Account: "support@example.test", Level: "workspace"}
			reply, err := s.adapters[service].Call(ctx, c, verb, tt.args)
			if err != nil || reply == nil || count != 1 || g.seen != c {
				t.Fatalf("reply=%v err=%v calls=%d credential=%+v", reply, err, count, g.seen)
			}
		})
	}
}
func TestWorkspaceNeverFallsBackToPersonalKeys(t *testing.T) {
	for _, c := range []svc.Credential{{Level: "workspace", ID: "workspace"}, {Level: "personal", Member: "another-member"}} {
		t.Run(c.Level+"/"+c.Member, func(t *testing.T) {
			s, _, _, _, _ := testDoor(t)
			_, err := (&nativeClients{s: s}).manager(context.Background(), c)
			if err == nil {
				t.Fatal("unresolved token store reached personal account")
			}
		})
	}
}

type definitionStore struct {
	svc.Store
	space string
}

func (s *definitionStore) Definition(context.Context, string) (*spec.Bundle, error) {
	return &spec.Bundle{App: &spec.App{Space: s.space}}, nil
}
func TestWorkspacePersonalGrantNeverFallsBackToOwnerKeys(t *testing.T) {
	s, _, _, all, _ := testDoor(t)
	all.Store = &definitionStore{space: "acme"}
	ctx := context.WithValue(context.Background(), scopeKey{}, testScope(svc.DataReal))
	_, err := (&nativeClients{s: s}).manager(ctx, svc.Credential{Level: "personal", Member: svc.Owner})
	if err == nil {
		t.Fatal("workspace personal grant reached the server owner's keys")
	}
}
