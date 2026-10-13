package realtime

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

func TestHubTopicsAndPrefixes(t *testing.T) {
	hub := New(svc.Config{}, nil)
	exact, stopExact := hub.Subscribe("app/crm/records/deal")
	prefix, stopPrefix := hub.Subscribe("app/crm/*")
	defer stopPrefix()
	hub.Publish("app/crm/records/deal", 1)
	hub.Publish("app/other/records/deal", 2)
	if got := <-exact; got.Seq != 1 || got.Data != 1 {
		t.Fatalf("exact got %+v", got)
	}
	if got := <-prefix; got.Data != 1 {
		t.Fatalf("prefix got %+v", got)
	}
	stopExact()
	stopExact()
	if len(prefix) != 0 {
		t.Fatal("other app leaked into prefix")
	}
}
