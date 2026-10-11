package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

var remoteJobDoors = []string{"Pause", "SetStage", "AddStage", "SetCap", "SetEffort", "Edit", "Refresh", "Talk", "Shape", "Sync", "AskAuthor", "BankStages", "Open"}

func assertRemoteJobDoorsNil(t *testing.T, seam factory.Seam) {
	t.Helper()
	for _, name := range remoteJobDoors {
		if !reflect.ValueOf(seam).FieldByName(name).IsNil() {
			t.Errorf("%s acts on this machine's floor for an unheld remote job", name)
		}
	}
}

func TestFactoryRemoteAttachmentHidesJobDoorsInBothLaunches(t *testing.T) {
	st := attachLab(t)
	body, _ := json.Marshal(factoryAttachment{URL: "https://factory.example"})
	if err := writeFactoryPrivate(factoryAttachPath(), body); err != nil {
		t.Fatal(err)
	}
	launch, _ := factoryLaunchSeam(st, t.TempDir(), "")
	attached, _ := factoryAttachedSeam("")
	for _, seam := range []factory.Seam{launch, attached} {
		assertRemoteJobDoorsNil(t, seam)
		assertRemoteJobDoorsNil(t, seam.Job(1))
		if seam.Launch == nil || seam.Hold == nil || seam.SetRepos == nil || seam.Clone == nil {
			t.Fatal("the remote guard removed a wired or repository door")
		}
	}
	if it, err := st.Get(1); err != nil || it.Title != "attach fixture" {
		t.Fatalf("local job changed: %+v, %v", it, err)
	}
}

func TestFactoryRemoteJobDoorsUseOnlyTheLiveWorkerMirror(t *testing.T) {
	st := attachLab(t)
	mirror, err := store.Open(filepath.Join(st.Root(), "workers", strconv.Itoa(os.Getpid())))
	if err != nil {
		t.Fatal(err)
	}
	it, _ := st.Get(1)
	it.Title = "remote job"
	if err := mirror.Save(it); err != nil {
		t.Fatal(err)
	}
	base := "https://factory.example"
	held := api.WorkerLease{ID: "lease_remote", Job: 1, Worker: "worker_here", ExpiresAt: time.Now().Add(time.Hour)}
	keep := factoryLocalLease(mirror)
	keep(base, held)
	c := &factoryConnection{at: factoryTarget{url: base}, seam: factory.Seam{
		Load: func() (factory.Snapshot, error) { return factory.Snapshot{Items: []factory.Item{it}}, nil },
	}}
	seam := c.compose(factorySeam(st, t.TempDir(), ""), st, "", "")
	if _, err := seam.Load(); err != nil {
		t.Fatal(err)
	}
	assertRemoteJobDoorsNil(t, seam.Job(2))
	cap := seam.Job(1).SetCap
	if cap == nil {
		t.Fatal("a live worker mirror has no cap door")
	}
	if err := cap(1, 12); err != nil {
		t.Fatal(err)
	}
	local, _ := st.Get(1)
	worker, _ := mirror.Get(1)
	if local.Cap == 12 || worker.Cap != 12 {
		t.Fatalf("cap went to the wrong floor: local %v, worker %v", local.Cap, worker.Cap)
	}
	for _, bad := range []factoryLocalHold{
		{Base: "https://other.example", Lease: held},
		{Base: base, Lease: api.WorkerLease{ID: held.ID, Job: 1, Worker: held.Worker, ExpiresAt: time.Unix(1, 0)}},
	} {
		keep(bad.Base, bad.Lease)
		if err := cap(1, 20); err == nil {
			t.Fatal("a captured cap door accepted a foreign or expired lease")
		}
		if _, err := seam.Load(); err != nil {
			t.Fatal(err)
		}
		assertRemoteJobDoorsNil(t, seam.Job(1))
	}
	keep(base, held)
	if _, err := seam.Load(); err != nil {
		t.Fatal(err)
	}
	cap = seam.Job(1).SetCap
	keep(base, api.WorkerLease{Job: 1})
	if err := cap(1, 30); err == nil {
		t.Fatal("a door captured before lease release still wrote the mirror")
	}
	if _, err := seam.Load(); err != nil {
		t.Fatal(err)
	}
	assertRemoteJobDoorsNil(t, seam.Job(1))
}
