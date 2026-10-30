package main

import (
	"net/http"
	"path/filepath"
	"runtime"
	"time"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/pair"
	"github.com/Agent-Field/codeaf/internal/reqsign"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
	"github.com/Agent-Field/codeaf/internal/taskdoor"
	"github.com/Agent-Field/codeaf/internal/taskplace"
)

// v3TaskComputers builds no client while this home is quiet, unpaired or on an
// unsupported platform. A nil door also keeps the proposal argument absent.
func v3TaskComputers(profile string) session.TaskComputers {
	if runtime.GOOS == "windows" || syncsetup.Quiet(profile) {
		return nil
	}
	route := syncsetup.Resolve(profile)
	if !route.On() {
		return nil
	}
	client, id, err := homeDirectory(profile, pair.Mailbox{URL: route.URL})
	if err != nil {
		return nil
	}
	device, err := identity.LoadDevice(profile)
	if err != nil {
		return nil
	}
	engine := cellstore.EngineFor("")
	engine.DataRoot = filepath.Join(profile, "v3", "stores")
	engine.Guard.Home = profile
	store := blobstore.NewHTTP(route.URL, reqsign.SignFor(homeSigner{id, device}, time.Now), &http.Client{Timeout: time.Minute})
	copies := taskdoor.CopyDeps{Root: filepath.Join(profile, "v3", "taskcopies"), Service: route.URL, Identity: id, Device: device, Engine: engine, Store: store}
	return &taskplace.Computers{Directory: client, Copies: copies, Home: profile}
}
