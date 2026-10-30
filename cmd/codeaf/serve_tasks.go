package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/pair"
	"github.com/Agent-Field/codeaf/internal/reqsign"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
	"github.com/Agent-Field/codeaf/internal/taskdoor"
)

// serveTasks shares the machine door's lifetime, relay and output. Its own
// cancellation joins followers while deliberately leaving their children alive.
func serveTasks(ctx context.Context, service string) func() {
	profile := home.Dir()
	if syncsetup.Quiet(profile) {
		return func() {}
	}
	if runtime.GOOS == "windows" {
		fmt.Fprintln(os.Stdout, taskdoor.UnsupportedComputerLine)
		return func() {}
	}
	config, ok, err := serveTasksConfig(profile, service)
	if err != nil {
		fmt.Fprintln(os.Stderr, "could not take tasks from your paired computers:", err)
		return func() {}
	}
	if !ok {
		return func() {}
	}
	config.Say = func(line string) { fmt.Fprintln(os.Stdout, line) }
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := taskdoor.Serve(ctx, config); err != nil && ctx.Err() == nil {
			fmt.Fprintln(os.Stderr, "could not take tasks from your paired computers:", err)
		}
	}()
	return func() { cancel(); <-done }
}

func serveTasksConfig(profile, service string) (taskdoor.Config, bool, error) {
	absolute, err := filepath.Abs(profile)
	if err != nil {
		return taskdoor.Config{}, false, err
	}
	profile = absolute
	if runtime.GOOS == "windows" || syncsetup.Quiet(profile) {
		return taskdoor.Config{}, false, nil
	}
	client, id, err := homeDirectory(profile, pair.Mailbox{URL: service})
	if err != nil {
		return taskdoor.Config{}, false, err
	}
	device, err := identity.LoadDevice(profile)
	if err != nil {
		return taskdoor.Config{}, false, err
	}
	engine := cellstore.EngineFor("")
	engine.DataRoot = filepath.Join(profile, "v3", "stores")
	engine.Guard.Home = profile
	store := blobstore.NewHTTP(service, reqsign.SignFor(homeSigner{id, device}, time.Now), &http.Client{})
	copies := taskdoor.CopyDeps{Root: filepath.Join(profile, "v3", "taskcopies"), Service: service, Identity: id, Device: device, Engine: engine, Store: store}
	return taskdoor.Config{Service: service, Identity: id, Device: device, Home: profile, Root: filepath.Join(profile, "v3", "taskdoor"),
		Roster: func(ctx context.Context) (map[string]bool, error) {
			listing, err := client.List(ctx)
			if err != nil {
				return nil, err
			}
			active := make(map[string]bool, len(listing.Devices))
			for id, device := range listing.Devices {
				active[id] = !device.Revoked
			}
			return active, nil
		}, Executor: taskdoor.NewExecutor(taskdoor.ExecutorConfig{Copies: copies, Ledger: filepath.Join(profile, "v3", "usage.jsonl")})}, true, nil
}
