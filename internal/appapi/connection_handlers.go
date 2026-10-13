package appapi

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var connectionContractRoutes = []contractRoute{
	{RouteContract: RouteContract{Method: "GET", Path: "/app/v1/accounts", Response: "AccountsResponse"}, handle: (*Server).accountHandler},
	{RouteContract: RouteContract{Method: "PATCH", Path: "/app/v1/accounts/{id}", Request: "ServiceAccessRequest", Response: "ConnectedService"}, handle: (*Server).accountHandler},
	{RouteContract: RouteContract{Method: "PATCH", Path: "/app/v1/accounts/{id}/actions/{aid}", Request: "ServiceAccessRequest", Response: "ConnectedService"}, handle: (*Server).accountHandler},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/accounts/{id}/connect", Request: "EmptyRequest", Response: "ServiceConnectResponse"}, handle: (*Server).accountHandler},
	{RouteContract: RouteContract{Method: "GET", Path: "/app/v1/accounts/{id}/connect", Response: "AccountConnectResponse"}, handle: (*Server).accountHandler},
	{RouteContract: RouteContract{Method: "DELETE", Path: "/app/v1/accounts/{id}", Response: "ActionResponse"}, handle: (*Server).accountHandler},
	{RouteContract: RouteContract{Method: "GET", Path: "/app/v1/mcp", Response: "McpServersResponse"}, handle: (*Server).mcpHandler},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/mcp", Request: "AddMcpServerRequest", Response: "McpServer"}, handle: (*Server).mcpHandler},
	{RouteContract: RouteContract{Method: "PATCH", Path: "/app/v1/mcp/{id}", Request: "ServiceAccessRequest", Response: "McpServer"}, handle: (*Server).mcpHandler},
	{RouteContract: RouteContract{Method: "DELETE", Path: "/app/v1/mcp/{id}", Response: "ActionResponse"}, handle: (*Server).mcpHandler},
}

func serviceAccess(value any) serviceAccessValue {
	text, _ := value.(string)
	switch serviceAccessValue(text) {
	case ServiceYes, ServiceOff:
		return serviceAccessValue(text)
	}
	return ServiceAsk
}

// Account identity comes only from the launch pipe, never from terminal credentials.
// Browser sign-in writes its completed grant to that same memory-only source.
func (s *Server) account(id string, values map[string]any) (ConnectedService, bool) {
	row := ConnectedService{ID: id, State: "disconnected", Detail: "Not connected", Access: ServiceOff, Actions: []ServiceAction{}}
	switch id {
	case "github":
		row.Name, row.ConnectURL = "GitHub", "https://github.com/settings/tokens"
	case "google":
		row.Name, row.ConnectURL = "Google", "https://accounts.google.com"
	case "slack":
		row.Name, row.ConnectURL = "Slack", "https://slack.com/signin"
	default:
		return row, false
	}
	if s.options.Credentials != nil {
		if token, _ := s.options.Credentials.Key(id); token != "" {
			row.State, row.Detail, row.Access = "connected", "Connected", serviceAccess(values["accounts."+id])
		}
	}
	manager := s.accountManager()
	if manager == nil {
		return row, true
	}
	if id != "github" && row.State == "connected" {
		if !manager.Connected(id) {
			row.State, row.Detail, row.Access = "revoked", "Connect again", ServiceOff
		}
		for _, status := range manager.Services() {
			if status.ID == id && status.Account != "" {
				row.Detail = status.Account
			}
		}
	}
	s.accounts.Lock()
	if flow := s.accounts.flows[id]; flow != nil {
		row.ConnectURL = flow.URL()
	}
	s.accounts.Unlock()
	for _, action := range manager.Capabilities(id) {
		access := serviceAccess(values["accounts."+id+".actions."+action.ID])
		if action.Acts {
			access = ServiceAsk
		}
		row.Actions = append(row.Actions, ServiceAction{ID: action.ID, Label: action.Phrase, Scope: accountActionScope(id, action.ID), Access: access, ActingAsYou: action.Acts})
	}
	return row, true
}

func (s *Server) accountHandler(w http.ResponseWriter, r *http.Request) {
	s.profileHandler(s.idempotent(s.serveAccount))(w, r)
}
func (s *Server) serveAccount(w http.ResponseWriter, r *http.Request) {
	s.settingsState.Lock()
	defer s.settingsState.Unlock()
	values, _, err := config.AppSettingsAt(s.options.ProfileDir)
	if err != nil {
		fail(w, 500, "conflict", "could not read saved settings")
		return
	}
	id := r.PathValue("id")
	if id == "" {
		rows := AccountsResponse{}
		for _, id := range []string{"github", "google", "slack"} {
			row, _ := s.account(id, values)
			rows = append(rows, row)
		}
		write(w, 200, rows)
		return
	}
	row, ok := s.account(id, values)
	if !ok {
		fail(w, 404, "not_found", "service not found")
		return
	}
	if r.Method == "GET" {
		write(w, 200, s.pollAccountConnect(row))
		return
	}
	if r.Method == "POST" {
		var body EmptyRequest
		if !decodeFactory(w, r, &body) {
			return
		}
		address := row.ConnectURL
		if id != "github" {
			var err error
			address, err = s.beginAccountConnect(r.Context(), id)
			if err != nil {
				agentFailure(w, err)
				return
			}
		}
		write(w, 200, ServiceConnectResponse{address})
		return
	}
	patch := map[string]json.RawMessage{}
	if r.Method == "DELETE" {
		patch["accounts."+id] = json.RawMessage(`"off"`)
	} else {
		var body ServiceAccessRequest
		if !decodeFactory(w, r, &body) {
			return
		}
		if body.Access != ServiceYes && body.Access != ServiceAsk && body.Access != ServiceOff {
			fail(w, 422, "bad_input", "access must be yes, ask_first or off")
			return
		}
		if row.State != "connected" && body.Access != ServiceOff {
			fail(w, 409, "not_allowed", row.Name+" isn't connected")
			return
		}
		key := "accounts." + id
		if aid := r.PathValue("aid"); aid != "" {
			found := false
			for _, action := range row.Actions {
				if action.ID == aid {
					found = true
					if action.ActingAsYou && body.Access != ServiceAsk {
						fail(w, 422, "bad_input", "acting in your name always asks")
						return
					}
				}
			}
			if !found {
				fail(w, 404, "not_found", "action not found")
				return
			}
			key += ".actions." + aid
		}
		patch[key], _ = json.Marshal(body.Access)
	}
	if err := config.ApplyAppSettings(s.options.ProfileDir, patch, ""); err != nil {
		settingError(w, err)
		return
	}
	if r.Method == "DELETE" {
		s.cancelAccountConnect(id)
	}
	if r.Method == "DELETE" && s.options.Credentials != nil {
		s.options.Credentials.Set(id, "")
	}
	s.settingsChanged(patch)
	if r.Method == "DELETE" {
		write(w, 200, ActionResponse{OK: true})
		return
	}
	values, _, _ = config.AppSettingsAt(s.options.ProfileDir)
	row, _ = s.account(id, values)
	write(w, 200, row)
}

func (s *Server) mcpHandler(w http.ResponseWriter, r *http.Request) {
	s.profileHandler(s.idempotent(s.serveMcp))(w, r)
}
func (s *Server) serveMcp(w http.ResponseWriter, r *http.Request) {
	s.settingsState.Lock()
	rows, err := config.AppMCPServersAt(s.options.ProfileDir)
	if err != nil {
		s.settingsState.Unlock()
		fail(w, 500, "conflict", "could not read saved servers")
		return
	}
	id := r.PathValue("id")
	var row config.AppMCPServer
	if id != "" {
		found := false
		for _, old := range rows {
			if old.ID == id {
				row, found = old, true
			}
		}
		if !found {
			s.settingsState.Unlock()
			fail(w, 404, "not_found", "server not found")
			return
		}
	}
	if r.Method == "POST" {
		var body AddMcpServerRequest
		if !decodeFactory(w, r, &body) {
			s.settingsState.Unlock()
			return
		}
		row, err = config.AddAppMCPServer(s.options.ProfileDir, body.Address)
	} else if r.Method == "PATCH" {
		var body ServiceAccessRequest
		if !decodeFactory(w, r, &body) {
			s.settingsState.Unlock()
			return
		}
		patch := map[string]json.RawMessage{}
		patch["accounts.mcp:"+id], _ = json.Marshal(body.Access)
		err = config.ApplyAppSettings(s.options.ProfileDir, patch, "")
	} else if r.Method == "DELETE" {
		err = config.RemoveAppMCPServer(s.options.ProfileDir, id)
		if err == nil && s.options.Credentials != nil {
			s.options.Credentials.Set("mcp:"+id, "")
		}
	}
	values, _, readErr := config.AppSettingsAt(s.options.ProfileDir)
	if err == nil {
		err = readErr
	}
	if err == nil && r.Method != "GET" {
		s.settingsChanged(map[string]json.RawMessage{"accounts.mcp:" + row.ID: nil})
	}
	s.settingsState.Unlock()
	if err != nil {
		settingError(w, err)
		return
	}
	if r.Method == "DELETE" {
		write(w, 200, ActionResponse{OK: true})
		return
	}
	if r.Method != "GET" {
		status := 200
		if r.Method == "POST" {
			status = 201
		}
		write(w, status, s.mcpView(r.Context(), row, values))
		return
	}
	result := make(McpServersResponse, len(rows))
	// Four bounded probes keep a slow server from blocking every other connection.
	slots := make(chan struct{}, 4)
	var wg sync.WaitGroup
	for i, row := range rows {
		wg.Add(1)
		go func() {
			defer wg.Done()
			slots <- struct{}{}
			defer func() { <-slots }()
			result[i] = s.mcpView(r.Context(), row, values)
		}()
	}
	wg.Wait()
	write(w, 200, result)
}

// A tools list is the connection proof. A failed request never manufactures tools.
func (s *Server) mcpView(ctx context.Context, row config.AppMCPServer, values map[string]any) McpServer {
	out := McpServer{ID: row.ID, Name: row.Name, Address: row.Address, State: "not_answering", Access: serviceAccess(values["accounts.mcp:"+row.ID])}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	token := ""
	if s.options.Credentials != nil {
		token, _ = s.options.Credentials.Key("mcp:" + row.ID)
	}
	client := &http.Client{Transport: mcpCredentialTransport{base: http.DefaultTransport, token: token}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	session, err := mcp.NewClient(&mcp.Implementation{Name: "codeaf", Version: s.options.Version}, nil).Connect(ctx, &mcp.StreamableClientTransport{Endpoint: row.Address, HTTPClient: client, DisableStandaloneSSE: true}, nil)
	if err != nil {
		return out
	}
	defer session.Close()
	count := 0
	for tool, err := range session.Tools(ctx, nil) {
		if err != nil || count >= 4096 {
			return out
		}
		if tool != nil {
			count++
		}
	}
	out.State, out.Tools = "connected", count
	return out
}

type mcpCredentialTransport struct {
	base  http.RoundTripper
	token string
}

func (t mcpCredentialTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	copy := r.Clone(r.Context())
	if t.token != "" {
		copy.Header.Set("Authorization", "Bearer "+t.token)
	}
	return t.base.RoundTrip(copy)
}

// Scope names come from the native integrations, so the Settings page cannot promise Drive tools that are absent.
func accountActionScope(service, action string) string {
	scopes := map[string]string{
		"google.mail-read":      "https://www.googleapis.com/auth/gmail.modify",
		"google.mail-send":      "https://www.googleapis.com/auth/gmail.modify",
		"google.calendar-read":  "https://www.googleapis.com/auth/calendar.events",
		"google.calendar-write": "https://www.googleapis.com/auth/calendar.events",
		"slack.messages-read":   "channels:history · channels:read · search:read",
		"slack.messages-send":   "chat:write",
	}
	return scopes[service+"."+action]
}
