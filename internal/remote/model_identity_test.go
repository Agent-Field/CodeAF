package remote

import (
	"github.com/Agent-Field/codeaf/internal/session"
	"testing"
)

type identityAgent struct {
	*fakeAgent
	identity session.ModelIdentity
}

func (a *identityAgent) ModelIdentity() session.ModelIdentity { return a.identity }
func (a *identityAgent) SetAutomaticModels(enabled bool) error {
	a.identity.Mode = "fixed"
	if enabled {
		a.identity.Mode = "automatic"
	}
	a.identity.Revision++
	return nil
}
func TestModelIdentityAndExplicitPolicyCrossRemoteConnection(t *testing.T) {
	a := &identityAgent{fakeAgent: &fakeAgent{model: "vendor/preferred"}, identity: session.ModelIdentity{Epoch: "live", Active: "vendor/actual", Selected: "vendor/preferred", Mode: "automatic", Revision: 2}}
	loop, err := Loopback(Hello{Version: Version}, Options{Boot: func(Hello) (*Engine, error) { return &Engine{Agent: a}, nil }})
	if err != nil {
		t.Fatal(err)
	}
	defer loop.Close()
	remote := loop.Client.Agent()
	if got := remote.ModelIdentity(); got.Active != "vendor/actual" || got.Mode != "automatic" {
		t.Fatalf("welcome: %+v", got)
	}
	if err := remote.SetAutomaticModels(false); err != nil {
		t.Fatal(err)
	}
	if got := remote.ModelIdentity(); got.Mode != "fixed" || got.Active != "vendor/actual" {
		t.Fatalf("policy changed identity: %+v", got)
	}
}
