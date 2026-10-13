package appapi

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/connect"
)

type accountState struct {
	sync.Mutex
	manager *connect.Manager
	flows   map[string]*connect.Flow
	issued  map[string]string
	closed  bool
}

// NewAccountManager is shared by the settings routes and app agent launches.
// App mode never opens the terminal's credentials.json or writes a new one.
func NewAccountManager(profile string, source *config.AppCredentials) *connect.Manager {
	credentials := map[string]connect.ClientCredential{}
	id, secret := config.GoogleOAuthClientAt(profile)
	credentials["google"] = connect.ClientCredential{ID: id, Secret: secret}
	credentials["slack"] = connect.ClientCredential{ID: config.SlackOAuthClientAt(profile), Public: true}
	manager := connect.NewAppManager(profile, credentials, source)
	manager.SetAppToolServers(func() []connect.AppToolServer {
		rows, err := config.AppMCPServersAt(profile)
		if err != nil {
			return nil
		}
		out := []connect.AppToolServer{}
		for _, row := range rows {
			out = append(out, connect.AppToolServer{ID: row.ID, Name: row.Name, Address: row.Address})
		}
		return out
	})
	manager.SetAppPolicy(func(service, capability string) (connect.CapabilityState, bool) {
		values, _, err := config.AppSettingsAt(profile)
		if err != nil {
			return connect.StateOff, true
		}
		access := serviceAccess(values["accounts."+service])
		action, ok := values["accounts."+service+".actions."+capability]
		if access == ServiceOff {
			return connect.StateOff, true
		}
		if ok {
			access = serviceAccess(action)
		}
		switch access {
		case ServiceYes:
			return connect.StateYes, true
		case ServiceOff:
			return connect.StateOff, true
		default:
			return connect.StateAsk, true
		}
	})
	return manager
}
func (s *Server) accountManager() *connect.Manager {
	s.accounts.Lock()
	defer s.accounts.Unlock()
	if s.accounts.manager == nil && s.options.Credentials != nil {
		s.accounts.manager = NewAccountManager(s.options.ProfileDir, s.options.Credentials)
		s.accounts.flows = map[string]*connect.Flow{}
		s.accounts.issued = map[string]string{}
	}
	return s.accounts.manager
}
func (s *Server) beginAccountConnect(ctx context.Context, id string) (string, error) {
	manager := s.accountManager()
	if manager == nil {
		return "", agentError(503, "not_allowed", "the app's account keys are unavailable")
	}
	s.accounts.Lock()
	defer s.accounts.Unlock()
	if old := s.accounts.flows[id]; old != nil {
		old.Cancel()
		delete(s.accounts.flows, id)
	}
	if s.accounts.closed {
		return "", agentError(503, "not_allowed", "the engine is stopping")
	}
	delete(s.accounts.issued, id)
	key, _ := s.options.Credentials.Key(id)
	s.options.Credentials.Set(id, key)
	flow, err := manager.BeginAuth(ctx, id, "")
	if err != nil {
		return "", agentError(409, "not_allowed", "this service cannot start browser sign-in")
	}
	s.accounts.flows[id] = flow
	parent := s.inbox.ctx
	wait, cancel := context.WithTimeout(parent, 5*time.Minute)
	s.watchers.Add(1)
	go func() {
		defer s.watchers.Done()
		defer cancel()
		_, err := flow.Wait(wait)
		s.accounts.Lock()
		current := s.accounts.flows[id] == flow
		if current {
			if err == nil {
				grant, _ := s.options.Credentials.Key(id)
				s.accounts.issued[id] = grant
			}
			delete(s.accounts.flows, id)
		}
		s.accounts.Unlock()
		if current && err == nil {
			s.settingsChanged(map[string]json.RawMessage{"accounts." + id: nil})
		}
	}()
	return flow.URL(), nil
}
func (s *Server) cancelAccountConnect(id string) {
	s.accounts.Lock()
	defer s.accounts.Unlock()
	delete(s.accounts.issued, id)
	if flow := s.accounts.flows[id]; flow != nil {
		flow.Cancel()
		delete(s.accounts.flows, id)
	}
}
func (s *Server) closeAccounts() {
	s.accounts.Lock()
	defer s.accounts.Unlock()
	s.accounts.closed = true
	s.accounts.issued = nil
	for id, flow := range s.accounts.flows {
		flow.Cancel()
		delete(s.accounts.flows, id)
	}
}

// A completed grant is handed back once for Keychain; launch keys are never exported by polling.
func (s *Server) pollAccountConnect(row ConnectedService) AccountConnectResponse {
	s.accounts.Lock()
	defer s.accounts.Unlock()
	out := AccountConnectResponse{ConnectedService: row, Credential: s.accounts.issued[row.ID]}
	delete(s.accounts.issued, row.ID)
	return out
}
