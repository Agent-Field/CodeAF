package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	factorygithub "github.com/Agent-Field/codeaf/internal/factory/github"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/processgroup"
)

// factoryLocalHold binds a mirror to a factory and a live lease. A matching
// job id on this machine's own floor is never evidence of a remote lease.
type factoryLocalHold struct {
	Base  string          `json:"base"`
	Lease api.WorkerLease `json:"lease"`
}

func factoryLocalHoldPath(mirror *store.Store, id int) string {
	return filepath.Join(mirror.Root(), "holds", strconv.Itoa(id)+".json")
}

// factoryLocalLease keeps receipts next to the worker's mirror, so another
// window on this machine can identify the run without knowing its secrets.
func factoryLocalLease(mirror *store.Store) func(string, api.WorkerLease) {
	return func(base string, held api.WorkerLease) {
		path := factoryLocalHoldPath(mirror, held.Job)
		if held.ID == "" {
			_ = os.Remove(path)
			return
		}
		body, err := json.Marshal(factoryLocalHold{Base: base, Lease: held})
		if err == nil {
			_ = writeFactoryPrivate(path, body)
		}
	}
}

type factoryJobDoors struct {
	hold factoryLocalHold
	seam factory.Seam
}

// compose refreshes the cached worker doors beside Load, OFF THE LOOP. The
// selectors only read that cache: an absent or expired receipt hides the key.
func (c *factoryConnection) compose(local factory.Seam, floor *store.Store, workspace, profileDir string) factory.Seam {
	var mu sync.Mutex
	jobs := map[int]factoryJobDoors{}
	replaceJobs := func(next map[int]factoryJobDoors) {
		mu.Lock()
		defer mu.Unlock()
		jobs = next
	}
	target := func() factoryTarget {
		c.mu.Lock()
		defer c.mu.Unlock()
		return c.at
	}
	remote := c.remote()
	if load := remote.Load; load != nil {
		remote.Load = func() (factory.Snapshot, error) {
			snap, err := load()
			next := map[int]factoryJobDoors{}
			if err == nil && !factoryLoopback(target().url) {
				next = factoryLocalJobs(floor, target().url, workspace, profileDir, snap, remote.Hold)
			}
			replaceJobs(next)
			return snap, err
		}
	}
	return api.ComposeJobs(remote, local, func() bool { return factoryLoopback(target().url) }, func(id int) factory.Seam {
		mu.Lock()
		defer mu.Unlock()
		job, ok := jobs[id]
		if !ok || job.hold.Base != target().url || !time.Now().Before(job.hold.Lease.ExpiresAt) {
			return factory.Seam{}
		}
		return job.seam
	})
}

// factoryLocalJobs accepts only live worker processes with a receipt from
// this factory and a mirror of the same job. Completed and killed workers
// cannot re-enable a door through files they left behind.
func factoryLocalJobs(floor *store.Store, base, workspace, profileDir string, snap factory.Snapshot, pause func(int, bool) error) map[int]factoryJobDoors {
	jobs := map[int]factoryJobDoors{}
	if floor == nil {
		return jobs
	}
	root := filepath.Join(floor.Root(), "workers")
	entries, _ := os.ReadDir(root)
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid <= 0 || !entry.IsDir() || !processgroup.ProcessAlive(pid) {
			continue
		}
		mirror, err := store.Open(filepath.Join(root, entry.Name()))
		if err != nil {
			continue
		}
		for _, it := range snap.Items {
			body, err := os.ReadFile(factoryLocalHoldPath(mirror, it.ID))
			var hold factoryLocalHold
			if err != nil || json.Unmarshal(body, &hold) != nil || hold.Base != base || hold.Lease.Job != it.ID || hold.Lease.ID == "" || hold.Lease.Worker == "" || !time.Now().Before(hold.Lease.ExpiresAt) {
				continue
			}
			if _, err := mirror.Get(it.ID); err != nil {
				continue
			}
			seam := factory.LocalSeam(mirror, time.Now(), factoryPipelineOption(),
				factory.WithRepoDirs(factoryRepoDirs(floor, workspace)),
				factory.WithTalk(talkMaker(floor, workspace, profileDir)),
				factory.WithRefetch(factorygithub.Refetcher(mirror, func(ctx context.Context) *factorygithub.Source {
					return factoryGitHub(ctx, floor, func(ctx context.Context) string {
						token, _ := factorygithub.TokenAt(ctx, factoryProfile(profileDir))
						return token
					})
				})))
			setFactoryProcess(func() {
				if factoryProcess.mirror == mirror.Root() {
					seam.Shape = factoryProcess.workerShape
				}
			})
			if pause != nil {
				seam.Pause = func(id int) error {
					it, err := mirror.Get(id)
					if err != nil {
						return err
					}
					return pause(id, it.Stream == nil || !it.Stream.Paused)
				}
			}
			seam = factoryGuardLocalJob(seam, mirror, pid, hold)
			jobs[it.ID] = factoryJobDoors{hold: hold, seam: seam}
		}
	}
	return jobs
}

// factoryGuardLocalJob checks the receipt again when a captured door runs.
// A lease put away after the last Load must never keep a writable mirror door.
func factoryGuardLocalJob(seam factory.Seam, mirror *store.Store, pid int, hold factoryLocalHold) factory.Seam {
	v := reflect.ValueOf(&seam).Elem()
	for i := 0; i < v.NumField(); i++ {
		if _, only := api.TUIOnly[v.Type().Field(i).Name]; !only || v.Field(i).IsNil() {
			continue
		}
		door := reflect.ValueOf(seam).Field(i)
		v.Field(i).Set(reflect.MakeFunc(door.Type(), func(args []reflect.Value) []reflect.Value {
			body, err := os.ReadFile(factoryLocalHoldPath(mirror, hold.Lease.Job))
			var now factoryLocalHold
			if err == nil && json.Unmarshal(body, &now) == nil && now.Base == hold.Base && now.Lease.ID == hold.Lease.ID && now.Lease.Job == hold.Lease.Job && now.Lease.Worker == hold.Lease.Worker && time.Now().Before(now.Lease.ExpiresAt) && processgroup.ProcessAlive(pid) {
				return door.Call(args)
			}
			out := make([]reflect.Value, door.Type().NumOut())
			for j := range out {
				out[j] = reflect.Zero(door.Type().Out(j))
			}
			out[len(out)-1] = reflect.ValueOf(errors.New("this job is no longer running on this machine"))
			return out
		}))
	}
	return seam
}
