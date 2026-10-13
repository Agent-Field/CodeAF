package connect

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/oauth2"
)

// AppToolServer is public endpoint metadata; tokens stay in the app's existing key source.
type AppToolServer struct{ ID, Name, Address string }

// SetAppToolServers resolves the saved list on each belt rebuild, including additions and removals.
func (m *Manager) SetAppToolServers(servers func() []AppToolServer) { m.appServers = servers }
func (m *Manager) currentPlugs() []Plug {
	if m.appServers == nil {
		return m.plugs
	}
	plugs := append([]Plug(nil), m.plugs...)
	for _, row := range m.appServers() {
		id := "mcp:" + row.ID
		RegisterGenericCapabilities(id, nil)
		plugs = append(plugs, &toolServer{app: true, address: row.Address, service: Service{ID: id, Name: row.Name, Address: row.Address, Auth: AuthBrowser}})
	}
	sortPlugs(plugs)
	return plugs
}

// Saved app servers can be anonymous. Their tools still have to answer before anything is armed.
func (m *Manager) openAppMCP(ctx context.Context, plug *toolServer) (*mcp.ClientSession, error) {
	client := &http.Client{Timeout: mcpCallTimeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	raw, _ := m.store.app.Key(plug.service.ID)
	if strings.TrimSpace(raw) != "" {
		entry, _, err := m.store.get(plug.service.ID)
		if err != nil || entry.Keys == nil || !entry.Keys.Valid() {
			return nil, fmt.Errorf("%s is not connected", plug.service.Name)
		}
		client.Transport = &oauth2.Transport{Source: bearerSource{base: oauth2.StaticTokenSource(entry.Keys)}}
	}
	return mcpClient().Connect(ctx, &mcp.StreamableClientTransport{Endpoint: plug.address, HTTPClient: client, DisableStandaloneSSE: true}, nil)
}
