package appapi

import (
	"context"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

func TestAppsServicesAreBoundToOneServer(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	first := New("test", Options{Context: ctx})
	second := New("test", Options{Context: ctx})
	defer first.Close()
	defer second.Close()
	all := &svc.Services{}
	if first.AppsServices() != nil || second.AppsServices() != nil {
		t.Fatal("unexpected services")
	}
	if first.SetAppsServices(all) != all || first.AppsServices() != all {
		t.Fatal("handle was not retained")
	}
	if second.AppsServices() != nil {
		t.Fatal("services leaked across servers")
	}
	replacement := &svc.Services{}
	first.SetAppsServices(replacement)
	if first.AppsServices() != replacement {
		t.Fatal("handle was not replaced")
	}
	first.SetAppsServices(nil)
	if first.AppsServices() != nil {
		t.Fatal("handle was not cleared")
	}
}
