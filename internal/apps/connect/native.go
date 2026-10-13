package connect

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
	"github.com/Agent-Field/codeaf/internal/config"
	native "github.com/Agent-Field/codeaf/internal/connect"
	"golang.org/x/oauth2"
)

// ManagerProvider binds a resolved connection to its isolated token store.
// Workspace credentials must never fall back to the server owner's personal account.
type ManagerProvider interface {
	ConnectorManager(context.Context, svc.Credential) (*native.Manager, error)
}

type nativeClients struct{ s *Service }

func (n *nativeClients) manager(ctx context.Context, c svc.Credential) (*native.Manager, error) {
	if provider, ok := n.s.all.Connect.Grants.(ManagerProvider); ok {
		return provider.ConnectorManager(ctx, c)
	}
	if c.Level != "personal" || (c.Member != "" && c.Member != svc.Owner) {
		return nil, bridge.Errorf(bridge.CodeInvalid, "connection credential is unavailable")
	}
	scope, _ := ctx.Value(scopeKey{}).(svc.Scope)
	if scope.App == "" || n.s.all.Store == nil {
		return nil, bridge.Errorf(bridge.CodeInvalid, "connection credential is unavailable")
	}
	bundle, err := n.s.all.Store.Definition(ctx, scope.App)
	if err != nil {
		return nil, err
	}
	if bundle == nil || bundle.App == nil || bundle.App.Space != "personal" {
		return nil, bridge.Errorf(bridge.CodeInvalid, "connection credential is unavailable")
	}
	home := filepath.Dir(n.s.cfg.Root)
	id, secret := config.GoogleOAuthClientAt(home)
	credentials := map[string]native.ClientCredential{"google": {ID: id, Secret: secret}, "slack": {ID: config.SlackOAuthClientAt(home), Public: true}}
	if source := config.AppCredentialSource(); source != nil {
		manager := native.NewAppManager(home, credentials, source)
		manager.SetAppToolServers(func() []native.AppToolServer {
			rows, _ := config.AppMCPServersAt(home)
			out := make([]native.AppToolServer, 0, len(rows))
			for _, row := range rows {
				out = append(out, native.AppToolServer{ID: row.ID, Name: row.Name, Address: row.Address})
			}
			return out
		})
		return manager, nil
	}
	return native.NewManager(home, credentials)
}
func (n *nativeClients) client(ctx context.Context, c svc.Credential, service string) (*http.Client, error) {
	manager, err := n.manager(ctx, c)
	if err != nil {
		return nil, err
	}
	return manager.Client(ctx, service)
}
func text(args map[string]any, key string) string { value, _ := args[key].(string); return value }
func number(args map[string]any, key string) int {
	switch value := args[key].(type) {
	case int:
		return value
	case float64:
		return int(value)
	}
	return 0
}
func noVerb(verb string) error { return bridge.Errorf(bridge.CodeInvalid, "no adapter for "+verb) }

func expiredError(err error) bool {
	if err == nil {
		return false
	}
	var response *oauth2.RetrieveError
	if errors.As(err, &response) && response.ErrorCode == "invalid_grant" {
		return true
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "has to be connected again") || strings.Contains(message, "not connected") || strings.Contains(message, "slack connection has expired") || strings.Contains(message, "401 unauthorized:")
}
