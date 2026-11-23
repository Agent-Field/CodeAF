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

func TestModelIdentityTransportsOneModelLockAndChosenIntent(t *testing.T) {
	a := &identityAgent{fakeAgent: &fakeAgent{model: "vendor/preferred"}, identity: session.ModelIdentity{Epoch: "live", Mode: "fixed", Chosen: "automatic", LockedReason: "one-model launch", Revision: 1}}
	loop, err := Loopback(Hello{Version: Version}, Options{Boot: func(Hello) (*Engine, error) { return &Engine{Agent: a}, nil }})
	if err != nil {
		t.Fatal(err)
	}
	defer loop.Close()
	if got := loop.Client.Agent().ModelIdentity(); got.Mode != "fixed" || got.Chosen != "automatic" || got.LockedReason != "one-model launch" {
		t.Fatalf("lock facts lost: %+v", got)
	}
}

func (a *identityAgent) SetConversationModels(pair session.ConversationModels) ([]session.ModelSwitch, error) {
	a.model = pair.Main
	a.identity.Models = &pair
	a.identity.Selected = pair.Main
	a.identity.Revision++
	return []session.ModelSwitch{{ID: "remote-pick", Text: "Main model changed."}}, nil
}
func TestConversationModelsCrossRemoteWithoutChangingActiveIdentity(t *testing.T) {
	a := &identityAgent{fakeAgent: &fakeAgent{model: "vendor/old"}, identity: session.ModelIdentity{Epoch: "live", Active: "vendor/old", Selected: "vendor/old", Revision: 1}}
	loop, err := Loopback(Hello{Version: Version}, Options{Boot: func(Hello) (*Engine, error) { return &Engine{Agent: a}, nil }})
	if err != nil {
		t.Fatal(err)
	}
	defer loop.Close()
	notes, err := loop.Client.Agent().SetConversationModels(session.ConversationModels{Main: "vendor/new", Fallback: "backup/new"})
	if err != nil {
		t.Fatal(err)
	}
	state := loop.Client.Agent().ModelIdentity()
	if len(notes) != 1 || state.Models == nil || state.Models.Main != "vendor/new" || state.Active != "vendor/old" {
		t.Fatalf("selection: %+v %+v", state, notes)
	}
}
