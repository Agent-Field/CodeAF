package session

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/service"
	"github.com/Agent-Field/codeaf/internal/service/servicetest"
)

func TestCloseForDeletionStopsKeptServices(t *testing.T) {
	rt := servicetest.New()
	sup, err := service.Open(service.Options{Dir: t.TempDir(), Runtime: rt})
	if err != nil {
		t.Fatal(err)
	}
	defer sup.Close()
	agent := keepAgent(t, &scriptedCompleter{}, sup, "deleted-owner")
	row, err := sup.Keep(agent.keepOwner(), service.Spec{Name: "server", Command: "server", Dir: t.TempDir(), Health: service.Health{Port: 4011}}, service.PolicyAlways, service.Provenance{})
	if err != nil {
		t.Fatal(err)
	}
	if err := agent.CloseForDeletion(); err != nil {
		t.Fatal(err)
	}
	if got, _ := sup.Get(agent.keepOwner(), row.ID); got.Running {
		t.Fatalf("orphan after shared deletion close: %+v", got)
	}
}
