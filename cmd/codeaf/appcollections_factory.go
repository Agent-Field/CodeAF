package main

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/workspace"
)

const (
	factoryReadTimeout    = 3 * time.Second
	factoryQueueFresh     = 10 * time.Second
	factoryQueueCacheMax  = 32
	factoryMissingJobsMax = 20
	factoryReadParallel   = 8
)

// collectionFactory belongs to one backend so cached queue reads cannot cross profiles.
type collectionFactory struct {
	queue   func(context.Context, string) (api.Queue, error)
	job     func(context.Context, string, int) (api.Job, error)
	cacheMu sync.Mutex
	cache   map[string]factoryQueueRead
	flights map[string]chan struct{}
}

type factoryJob struct {
	Factory string
	Number  int
}

type factoryQueueRead struct {
	at   time.Time
	jobs map[int]api.Job
	err  error
}

// factoryQueue shares successful and failed reads briefly, including an
// in-flight read, so concurrent list/get calls cannot multiply Factory work.
func (b *appBackend) factoryQueue(ctx context.Context, factory string) (entry factoryQueueRead) {
	state := &b.collectionFactory
	entry, flight, readQueue := state.factoryQueueStart(factory, b.now())
	if flight == nil {
		return entry
	}
	if !readQueue {
		select {
		case <-flight:
			return b.factoryQueue(ctx, factory)
		case <-ctx.Done():
			return factoryQueueRead{err: ctx.Err()}
		}
	}
	// Complete ownership even when the guarded reader panics. Only a completed
	// read replaces the unavailable entry, so a fault cannot publish success.
	entry = factoryQueueRead{err: errors.New("the Factory queue read did not complete")}
	defer func() {
		entry.at = b.now()
		state.factoryQueueFinish(factory, flight, entry)
	}()
	read := state.queue
	if read == nil {
		read = b.FactoryQueue
	}
	queue, err := read(ctx, factory)
	jobs := map[int]api.Job{}
	for _, j := range queue.Jobs {
		jobs[j.ID] = j
	}
	entry = factoryQueueRead{err: err, jobs: jobs}
	return entry
}

func (state *collectionFactory) factoryQueueStart(factory string, now time.Time) (factoryQueueRead, chan struct{}, bool) {
	state.cacheMu.Lock()
	defer state.cacheMu.Unlock()
	if entry, ok := state.cache[factory]; ok && now.Sub(entry.at) < factoryQueueFresh {
		return entry, nil, false
	}
	if flight := state.flights[factory]; flight != nil {
		return factoryQueueRead{}, flight, false
	}
	if state.flights == nil {
		state.flights = map[string]chan struct{}{}
	}
	flight := make(chan struct{})
	state.flights[factory] = flight
	return factoryQueueRead{}, flight, true
}

func (state *collectionFactory) factoryQueueFinish(factory string, flight chan struct{}, entry factoryQueueRead) {
	state.cacheMu.Lock()
	defer state.cacheMu.Unlock()
	if state.cache == nil {
		state.cache = map[string]factoryQueueRead{}
	}
	if len(state.cache) >= factoryQueueCacheMax {
		oldest := ""
		var at time.Time
		for id, v := range state.cache {
			if oldest == "" || v.at.Before(at) {
				oldest, at = id, v.at
			}
		}
		delete(state.cache, oldest)
	}
	state.cache[factory] = entry
	delete(state.flights, factory)
	close(flight)
}

type factoryJobReading struct {
	job      api.Job
	err      error
	notFound bool
}

// factoryJobReads gives each factory one concurrent queue read. Missing
// jobs share a global fallback allowance and the same overall deadline.
func (b *appBackend) factoryJobReads(jobs []factoryJob) map[factoryJob]factoryJobReading {
	out := map[factoryJob]factoryJobReading{}
	grouped := map[string][]factoryJob{}
	for _, j := range jobs {
		if _, seen := out[j]; !seen {
			out[j] = factoryJobReading{err: context.DeadlineExceeded}
			grouped[j.Factory] = append(grouped[j.Factory], j)
		}
	}
	if len(grouped) == 0 {
		return out
	}
	ctx, cancel := context.WithTimeout(context.Background(), factoryReadTimeout)
	defer cancel()
	var wg sync.WaitGroup
	var mu sync.Mutex
	missing := 0
	factoryMissingAllowed := func() bool {
		mu.Lock()
		defer mu.Unlock()
		if missing >= factoryMissingJobsMax {
			return false
		}
		missing++
		return true
	}
	factoryRememberRead := func(j factoryJob, read factoryJobReading) {
		mu.Lock()
		defer mu.Unlock()
		out[j] = read
	}
	slots := make(chan struct{}, factoryReadParallel)
	for factory, members := range grouped {
		wg.Add(1)
		guard.Go("app/collection-factory", func() {
			defer wg.Done()
			select {
			case slots <- struct{}{}:
				defer func() { <-slots }()
			case <-ctx.Done():
				return
			}
			queue := b.factoryQueue(ctx, factory)
			for _, j := range members {
				job, ok := queue.jobs[j.Number]
				err := queue.err
				if err == nil && !ok {
					allowed := factoryMissingAllowed()
					switch {
					case !allowed:
						err = errors.New("the missing-job read allowance was reached")
					case ctx.Err() != nil:
						err = ctx.Err()
					default:
						read := b.collectionFactory.job
						if read == nil {
							read = b.FactoryJob
						}
						job, err = read(ctx, j.Factory, j.Number)
						if err == nil && job.ID != j.Number {
							err = errors.New("the Factory returned another job")
						}
					}
				}
				var wire *appwire.Error
				notFound := queue.err == nil && errors.As(err, &wire) && wire.Code == appwire.CodeNotFound
				factoryRememberRead(j, factoryJobReading{job: job, err: err, notFound: notFound})
			}
		})
	}
	wg.Wait()
	return out
}

// Collection resolution uses the same bounded batch reader, with each job
// asked once even when it appears through several nested collections.
func (r *collectionResolver) prepareFactoryRefs(members []workspace.Member) {
	jobs := []factoryJob{}
	refs := map[factoryJob]workspace.Ref{}
	for _, member := range members {
		if member.Kind != workspace.FactoryJobKind {
			continue
		}
		number, _ := strconv.Atoi(member.ID)
		job := factoryJob{Factory: member.Factory, Number: number}
		jobs = append(jobs, job)
		refs[job] = member.Ref
	}
	r.factoryTargets = map[workspace.Ref]appwire.Target{}
	for job, read := range r.b.factoryJobReads(jobs) {
		var target appwire.Target
		if read.err != nil {
			if read.notFound {
				target = appwire.Target{State: appwire.TargetDeleted}
			} else {
				target = appUnknown("the Factory", read.err)
				target.Status = "unknown"
			}
		} else {
			target = appwire.Target{State: appwire.TargetLive, Title: read.job.Title, Status: read.job.State, Repo: read.job.Repo}
		}
		r.factoryTargets[refs[job]] = target
	}
}

func (r *collectionResolver) prepareFactoryTree(level workspace.Level) {
	members := []workspace.Member{}
	var collect func(workspace.Level)
	collect = func(level workspace.Level) {
		for _, node := range level.Members {
			if node.Kind == workspace.FactoryJobKind {
				members = append(members, node.Member)
			}
			if node.Children != nil {
				collect(*node.Children)
			}
		}
	}
	collect(level)
	r.prepareFactoryRefs(members)
}
