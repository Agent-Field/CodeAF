package appwire

import "github.com/Agent-Field/codeaf/internal/identity"

// PairedClient is one locally enrolled scoped client in Settings.
type PairedClient struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Methods []string `json:"methods"`
	Reads   []string `json:"reads"`
	Paused  bool     `json:"paused"`
}

// PairClientParams contains public keys only. The engine issues the certificate
// and registers its restricted scope before returning the public grant.
type PairClientParams struct {
	SigningKey []byte   `json:"signing_key"`
	BoxKey     []byte   `json:"box_key"`
	Methods    []string `json:"methods"`
	Reads      []string `json:"reads"`
	Name       string   `json:"name"`
}

// ClientGrant carries public connection and encryption information, never the
// identity's signing seed, cell key, dedup secret or vault.
type ClientGrant struct {
	Cert     identity.Cert `json:"cert"`
	Identity []byte        `json:"identity"`
	Relay    string        `json:"relay"`
	Writer   string        `json:"writer"`
	BoxKey   []byte        `json:"box_key"`
}

// PairedClientManager is optional; engines without it do not advertise the
// feature and refuse these methods rather than exposing a broken control.
type PairedClientManager interface {
	ListPairedClients() ([]PairedClient, error)
	PairClient(PairClientParams) (ClientGrant, error)
	PausePairedClient(string, bool) error
	UnpairClient(string) error
}

func pairedClientManager(e Engine) (PairedClientManager, error) {
	manager, ok := e.(PairedClientManager)
	if !ok {
		return nil, Errorf(CodeUnknownMethod, "this engine does not serve paired clients")
	}
	return manager, nil
}

func init() {
	methods["clients.list"] = typed(func(e Engine, _ struct{}) (*struct {
		Clients []PairedClient `json:"clients"`
	}, error) { manager, err := pairedClientManager(e); if err != nil {
		return nil, err
	}; list, err := manager.ListPairedClients(); if err != nil {
		return nil, err
	}; return &struct {
		Clients []PairedClient `json:"clients"`
	}{nonNil(list)}, nil })
	methods["clients.pair"] = typed(func(e Engine, p PairClientParams) (*ClientGrant, error) {
		manager, err := pairedClientManager(e)
		if err != nil {
			return nil, err
		}
		if len(p.SigningKey) != 32 || len(p.BoxKey) != 32 {
			return nil, Errorf(CodeBadParams, "client signing and encryption keys must each be 32 bytes")
		}
		grant, err := manager.PairClient(p)
		return &grant, err
	})
	methods["clients.pause"] = typed(func(e Engine, p struct {
		ID     string `json:"id"`
		Paused *bool  `json:"paused"`
	}) (*Empty, error) { manager, err := pairedClientManager(e); if err != nil {
		return nil, err
	}; if p.ID == "" || p.Paused == nil {
		return nil, Errorf(CodeBadParams, "clients.pause needs an id and paused flag")
	}; return done(manager.PausePairedClient(p.ID, *p.Paused)) })
	methods["clients.unpair"] = typed(func(e Engine, p IDParams) (*Empty, error) {
		manager, err := pairedClientManager(e)
		if err != nil {
			return nil, err
		}
		if p.ID == "" {
			return nil, Errorf(CodeBadParams, "clients.unpair needs an id")
		}
		return done(manager.UnpairClient(p.ID))
	})
}
