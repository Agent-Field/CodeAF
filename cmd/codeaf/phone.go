package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/clientrun"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/filelock"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/leave"
	"github.com/Agent-Field/codeaf/internal/provider"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/store"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

const (
	phoneUsageLine             = "usage: codeaf phone serve [--home dir] [--accept-older-list]"
	phoneServeStartLine        = "serving your phone on this computer. ctrl+c stops it."
	phoneAlreadyServingLine    = "codeaf app is open on this computer and is already serving your phone."
	phoneQuietLine             = "no phone is paired with this computer yet. Approve one first, then run this again."
	phoneAcceptedOlderListLine = "accepted the older device list. Phones paired before the reset must pair again."
)

const phoneUsage = `  codeaf phone serve [--home dir] [--accept-older-list]
      keep this computer ready to start chats and answer from your phone`

// phoneDirectoryOverride replaces the directory client in tests so V3 and V5
// can run through the command's own entry function against a fake directory.
var phoneDirectoryOverride directory.Client

type phoneServeFlags struct {
	set             *flag.FlagSet
	home            *string
	acceptOlderList *bool
}

func newPhoneServeFlags() phoneServeFlags {
	set := commandFlags("phone serve")
	return phoneServeFlags{
		set:             set,
		home:            set.String("home", "", "codeaf's state folder for this host, instead of CODEAF_HOME or ~/.codeaf"),
		acceptOlderList: set.Bool("accept-older-list", false, "accept an older device list after a relay reset"),
	}
}

func runPhone(args []string) error {
	return runPhoneContext(context.Background(), args, os.Stderr)
}

func runPhoneContext(parent context.Context, args []string, out io.Writer) error {
	if out == nil {
		out = os.Stderr
	}
	if len(args) == 0 {
		return wrongCall(phoneUsageLine)
	}
	if args[0] != "serve" {
		if askedForHelp(args) {
			writeCommandUsage(usageOut, newPhoneServeFlags().set)
			return exitHelped
		}
		return wrongCall(phoneUsageLine)
	}
	names := newPhoneServeFlags()
	if err := parseCommandFlags(names.set, reorder(names.set, args[1:])); err != nil {
		return err
	}
	if names.set.NArg() > 0 {
		return wrongCall(phoneUsageLine)
	}
	restoreHome, homeErr := useAppHome(*names.home)
	if homeErr != nil {
		return homeErr
	}
	defer restoreHome()

	// Refuse immediately when another runner or the desktop app holds this home.
	if appHomeBusy(home.Dir()) || runnerLockBusy(home.Dir()) {
		return reportPhoneError(out, errors.New(phoneAlreadyServingLine))
	}
	s, err := syncOf()
	if err != nil {
		if errors.Is(err, syncsetup.ErrNoIdentity) && identity.Solo(home.Dir()) {
			return reportPhoneError(out, errors.New(phoneQuietLine))
		}
		return reportPhoneError(out, err)
	}
	if !syncsetup.PhoneMayTalk(s.Home) {
		return reportPhoneError(out, errors.New(phoneQuietLine))
	}

	inheritLoginPath()
	ctx, leaving := context.WithCancel(parent)
	cellstore.SetAppDaemonLifetime(ctx)
	defer leaving()
	defer leave.On(leaving, nil)()

	server := appwire.NewServer(io.Discard)
	backend := newAppBackend(server, appOpenInProcess)
	backend.leaving = leaving
	var calmReceipts func()
	backend.hurryReceipts = func() { calmReceipts = provider.HurryReceipts() }
	defer func() {
		if calmReceipts != nil {
			calmReceipts()
		}
	}()
	backend.useSyncDrives(syncDrives)
	defer func() {
		flushCtx, cancel := context.WithTimeout(context.Background(), cellstore.SealWithin)
		defer cancel()
		recordFinalSaveFailure(flushFinalSaves(flushCtx))
	}()
	defer backend.watchSpendRows()()
	defer backend.watchAgentSpend()()
	backend.memoryStore = func() (*store.Store, error) {
		proc, err := openAppEngineProcess()
		if err != nil {
			return nil, err
		}
		if proc.Memory == nil {
			return nil, errors.New("this engine has no saved memory store")
		}
		return proc.Memory, nil
	}
	backend.searchWords = func(terms string, limit int) ([]store.ConversationHit, error) {
		proc, err := openAppEngineProcess()
		if err != nil || proc.Search == nil {
			return nil, errors.New("conversation search is unavailable")
		}
		return proc.Search.SearchConversations(terms, limit)
	}
	services := openAppServices()
	appServiceDoor.Store(services)
	appClientToolsBackend.Store(backend)
	backend.attachServices(services)
	defer func() {
		_ = backend.Close()
		if backend.assembled.Load() {
			session.CloseUsage()
			if proc := openedEngineProcess(); proc != nil {
				proc.quitReceiptWait.Store(int64(appQuitReceiptWait))
			}
			closeEngineProcess()
		}
	}()

	var dir directory.Client
	if phoneDirectoryOverride != nil {
		dir = phoneDirectoryOverride
	}
	// The reason is printed from the runner's own change hook, once per change, so a
	// round that pauses serving can never be missed by a read that raced it.
	var pausedMu sync.Mutex
	var lastPaused string
	printPaused := func(reason string) {
		paused := reason
		pausedMu.Lock()
		defer pausedMu.Unlock()
		if paused == "" {
			lastPaused = ""
			return
		}
		if paused != lastPaused {
			lastPaused = paused
			fmt.Fprintln(out, paused)
		}
	}
	r, err := clientrun.Open(backend.clientRunnerConfig(s, dir, printPaused))
	if err != nil {
		if errors.Is(err, clientrun.ErrLocked) {
			return reportPhoneError(out, errors.New(phoneAlreadyServingLine))
		}
		return reportPhoneError(out, err)
	}
	runCtx, runCancel := context.WithCancel(ctx)
	done := make(chan struct{})
	func() {
		backend.clientInit.Lock()
		defer backend.clientInit.Unlock()
		backend.clientCancel = runCancel
		backend.clientDone = done
		backend.clientRunner.Store(r)
		setActivePhoneUnpairer(phoneRunnerUnpairer{r: r})
	}()

	if *names.acceptOlderList {
		if err := r.AcceptOlderList(ctx); err != nil {
			close(done)
			backend.stopClients()
			return reportPhoneError(out, err)
		}
		fmt.Fprintln(out, phoneAcceptedOlderListLine)
	}
	fmt.Fprintln(out, phoneServeStartLine)

	gateTicker := time.NewTicker(phoneGatePollEvery)
	guard.Go("phone/gate", func() {
		defer gateTicker.Stop()
		watchPhoneGate(runCtx, s.Home, gateTicker.C, r.Wake)
	})

	guard.Go("phone/clients", func() {
		defer close(done)
		stop := syncsetup.WatchPublishes(r.Changed)
		defer stop()
		r.Run(runCtx)
	})

	<-ctx.Done()
	backend.stopClients()
	return nil
}

func reportPhoneError(out io.Writer, err error) error {
	if err != nil && out != nil && out != os.Stderr {
		fmt.Fprintln(out, err.Error())
	}
	return err
}

func appHomeBusy(dir string) bool {
	return lockFileBusy(filepath.Join(dir, "app.lock"))
}

func runnerLockBusy(dir string) bool {
	return lockFileBusy(clientrun.StatePath(dir) + ".lock")
}

func lockFileBusy(path string) bool {
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return false
	}
	defer f.Close()
	if err := filelock.Lock(f, true, true); err != nil {
		return filelock.IsBusy(err)
	}
	_ = filelock.Unlock(f)
	return false
}
