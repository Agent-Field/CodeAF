package store

import (
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func seedServiceNode(t *testing.T, graph *Store, id string, serviceIntent bool) {
	t.Helper()
	err := graph.Splice(RootID, Subtree{Nodes: []NodeSpec{{ID: id, Brief: "run app", Stage: 1}}},
		Provenance{Origin: OriginUser, SessionID: "services", Intent: "run the app", ServiceIntent: serviceIntent})
	if err != nil {
		t.Fatal(err)
	}
}

func serviceFixture(id, name, node string, pid int) Service {
	return Service{
		ID: id, Name: name, Command: "npm run dev", Dir: "/tmp/work", LogPath: "/tmp/work/.aforge/jobs/1.log",
		Health: ServiceHealth{Kind: ServiceHealthPort, Value: "5173"}, PID: pid,
		StartedAt: time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC), Status: ServiceRunning,
		Provenance: ServiceProvenance{OriginJobID: 1, LeafNodeID: node},
	}
}

func TestServiceNameUniquenessAmongLiveServices(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "services.db"))
	seedServiceNode(t, graph, "leaf-a", true)
	seedServiceNode(t, graph, "leaf-b", true)
	first, err := graph.PromoteService(serviceFixture("svc-a", "dev-server", "leaf-a", 101))
	if err != nil {
		t.Fatal(err)
	}
	duplicate := serviceFixture("svc-b", "DEV-SERVER", "leaf-b", 102)
	if _, err := graph.PromoteService(duplicate); !errors.Is(err, ErrInvalid) {
		t.Fatalf("duplicate promotion err = %v, want ErrInvalid", err)
	}
	if err := graph.StopService(first.ID, "done"); err != nil {
		t.Fatal(err)
	}
	if _, err := graph.PromoteService(duplicate); err != nil {
		t.Fatalf("stopped name was not reusable: %v", err)
	}
}

func TestServiceLifecycleRebuild(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "services-rebuild.db"))
	seedServiceNode(t, graph, "leaf", true)
	service, err := graph.PromoteService(serviceFixture("svc", "dev-server", "leaf", 201))
	if err != nil {
		t.Fatal(err)
	}
	if err := graph.AdoptService(service.ID); err != nil {
		t.Fatal(err)
	}
	if err := graph.SetServiceAutoRestart(service.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := graph.FailService(service.ID, "port closed", 1); err != nil {
		t.Fatal(err)
	}
	if err := graph.RestartService(service.ID, 202, service.StartedAt.Add(time.Minute), 2); err != nil {
		t.Fatal(err)
	}
	if err := graph.RestService(service.ID, "cap", 3); err != nil {
		t.Fatal(err)
	}
	before, found, err := graph.Service(service.ID)
	if err != nil || !found {
		t.Fatalf("service before rebuild = %+v found=%v err=%v", before, found, err)
	}
	if err := graph.Rebuild(); err != nil {
		t.Fatal(err)
	}
	after, found, err := graph.Service(service.ID)
	if err != nil || !found {
		t.Fatalf("service after rebuild = %+v found=%v err=%v", after, found, err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("rebuilt service mismatch\nbefore: %+v\nafter:  %+v", before, after)
	}
	node, _, _ := graph.Node("leaf")
	if !node.Provenance.ServiceIntent {
		t.Fatal("Rebuild lost service-intent provenance")
	}
}
