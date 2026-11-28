package appwire

// PairedClient is one enrolled scoped client in Settings. A phone is never
// listed with a key or a secret: only its device id, name, methods, reads, and
// paused flag.
type PairedClient struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Methods []string `json:"methods"`
	Reads   []string `json:"reads"`
	Paused  bool     `json:"paused"`
}

// PairedClientManager is optional; engines without it do not advertise the
// clients.manage feature and refuse these methods rather than exposing a broken
// control. Pairing itself goes through link approval, not an appwire method.
type PairedClientManager interface {
	ListPairedClients() ([]PairedClient, error)
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
	}, error) {
		manager, err := pairedClientManager(e)
		if err != nil {
			return nil, err
		}
		list, err := manager.ListPairedClients()
		if err != nil {
			return nil, err
		}
		for i := range list {
			list[i].Methods = nonNil(list[i].Methods)
			list[i].Reads = nonNil(list[i].Reads)
		}
		return &struct {
			Clients []PairedClient `json:"clients"`
		}{nonNil(list)}, nil
	})
	methods["clients.pause"] = typed(func(e Engine, p struct {
		ID     string `json:"id"`
		Paused *bool  `json:"paused"`
	}) (*Empty, error) {
		manager, err := pairedClientManager(e)
		if err != nil {
			return nil, err
		}
		if p.ID == "" || p.Paused == nil {
			return nil, Errorf(CodeBadParams, "clients.pause needs an id and paused flag")
		}
		return done(manager.PausePairedClient(p.ID, *p.Paused))
	})
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
