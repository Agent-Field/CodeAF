package main

import (
	"context"
	"crypto/ed25519"
	"errors"
	"os"
	"strconv"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/clientrun"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

// appClientEmitter schedules fresh snapshots after the engine has published
// its changes. The empty case is one pointer load with no goroutine or timer.
type appClientEmitter struct {
	b    *appBackend
	next appwire.Emitter
}

func (e appClientEmitter) Emit(name string, data any) {
	// Update the phone's partial before notifying either reader. The ordinary
	// app event remains unchanged and is still delivered through its outbox.
	r := e.b.clientRunner.Load()
	if r != nil {
		switch v := data.(type) {
		case appwire.TurnDelta:
			e.b.clientStream.delta(v, r.Changed)
		case appwire.EntryAdded:
			if v.Entry.Msg != nil {
				e.b.clientStream.clear(v.Entry.Agent)
			}
		case appwire.TurnDone:
			e.b.clientStream.clear(v.Agent)
		case appwire.TurnRetrying:
			e.b.clientStream.clear(v.Agent)
		}
	}
	e.next.Emit(name, data)
	if r == nil {
		return
	}
	switch name {
	case appwire.EventEntryAdded, appwire.EventTurnDone, appwire.EventTurnRetrying,
		appwire.EventAskOpened, appwire.EventAskClosed, appwire.EventTaskChanged,
		appwire.EventAgentChanged, appwire.EventSpendChanged:
		r.Changed()
	}
}

type appClientSigner struct {
	id  identity.Identity
	dev identity.Dev
}

func (s appClientSigner) IdentityKey() ed25519.PublicKey { return s.id.PublicKey() }
func (s appClientSigner) Cert() identity.Cert            { return s.dev.Cert }
func (s appClientSigner) Sign(p []byte) []byte           { return s.dev.Sign(p) }

// startPairedClients preserves the unpaired startup: it does not open sync,
// make keys, or start a timer when no local client record exists.
func (b *appBackend) startPairedClients() {
	if _, err := os.Stat(clientrun.StatePath(home.Dir())); err != nil {
		return
	}
	local, err := clientrun.Open(clientrun.Config{Home: home.Dir(), Log: b.logClientCall})
	if err != nil {
		b.emit.Emit(appwire.EventLog, appwire.Log{Level: "warn", Message: "paired client records could not be read"})
		return
	}
	if len(local.Clients()) == 0 {
		_ = local.Close()
		return
	}
	_ = local.Close()
	if _, err := b.clients(); err != nil {
		b.emit.Emit(appwire.EventLog, appwire.Log{Level: "warn", Message: "paired clients could not start"})
	}
}

func (b *appBackend) clients() (*clientrun.Runner, error) {
	b.clientInit.Lock()
	defer b.clientInit.Unlock()
	select {
	case <-b.left:
		return nil, appwire.Errorf(appwire.CodeClosed, "codeaf is shutting down")
	default:
	}
	if r := b.clientRunner.Load(); r != nil {
		return r, nil
	}
	s, err := syncOf()
	if err != nil {
		return nil, err
	}
	r, err := clientrun.Open(clientrun.Config{Home: s.Home, Relay: s.Relay, Signer: appClientSigner{s.Identity, s.Device}, Directory: s.Dir, Engine: appClientEngine{b}, Now: b.now, Log: b.logClientCall, LeaseCreated: b.leaseClientChat})
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	b.clientCancel = cancel
	b.clientDone = make(chan struct{})
	b.clientRunner.Store(r)
	guard.Go("app/clients", func() {
		defer close(b.clientDone)
		stop := syncsetup.WatchPublishes(r.Changed)
		defer stop()
		r.Run(ctx)
	})
	return r, nil
}

// logClientCall keeps chat history independently of replay watermarks, so
// acknowledgement and unpair can discard runner receipts without losing lines.
func (b *appBackend) logClientCall(receipt clientrun.Receipt) error {
	if receipt.Agent != "" {
		// A reserved creation may have died before its folder existed, or
		// before best-effort session metadata was written. Its durable path
		// lets us recover the latter without inventing a nonexistent chat.
		if receipt.Transcript != "" {
			folder, ok := session.FolderOf(receipt.Transcript)
			if !ok {
				return appwire.Errorf(appwire.CodeBadParams, "client receipt has no chat folder")
			}
			if _, err := os.Stat(folder); os.IsNotExist(err) {
				return nil
			} else if err != nil {
				return err
			}
			a := b.agent(receipt.Agent)
			b.locked(func() {
				if a.transcript == "" {
					a.transcript = receipt.Transcript
				}
			})
		}
		a, err := b.findCrewOwner(receipt.Agent)
		if err != nil {
			return err
		}
		entry := appClientEntry(receipt)
		if err := b.keepAppEntry(a, entry, ""); err != nil {
			return err
		}
		b.emit.Emit(appwire.EventEntryAdded, appwire.EntryAdded{Entry: appClientEntry(receipt)})
	}
	return nil
}
func appClientEntry(receipt clientrun.Receipt) appwire.Entry {
	at, _ := time.Parse(time.RFC3339Nano, receipt.At)
	state := "your call"
	switch receipt.Status {
	case "started":
		state = "running"
	case "done":
		state = "done"
	case "failed":
		state = "incomplete"
	}
	suffix := ""
	if receipt.Reason != "" {
		suffix = " · " + receipt.Reason
	}
	if receipt.SkippedFrom > 0 {
		suffix = " · skipped missing calls " + strconv.FormatInt(receipt.SkippedFrom, 10) + "–" + strconv.FormatInt(receipt.SkippedThrough, 10)
	}
	id := "client-" + receipt.Client + "-" + strconv.FormatInt(receipt.Number, 10)
	id += "-" + receipt.Status
	return appwire.Entry{ID: id, Agent: receipt.Agent, Kind: appwire.EntryNotice, At: at, Text: "Paired client " + receipt.Client + " · " + receipt.Method + " · " + state + suffix}
}
func (b *appBackend) ListPairedClients() ([]appwire.PairedClient, error) {
	r := b.clientRunner.Load()
	if r == nil {
		return []appwire.PairedClient{}, nil
	}
	out := make([]appwire.PairedClient, 0)
	for _, c := range r.Clients() {
		out = append(out, appwire.PairedClient{ID: c.ID, Name: c.Name, Methods: c.Methods, Reads: c.Reads, Paused: c.Paused})
	}
	return out, nil
}
func (b *appBackend) PairClient(p appwire.PairClientParams) (appwire.ClientGrant, error) {
	r, err := b.clients()
	if err != nil {
		return appwire.ClientGrant{}, err
	}
	id, err := identity.Load(home.Dir())
	if err != nil {
		return appwire.ClientGrant{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	grant, err := r.Pair(ctx, clientrun.Pairing{Cert: identity.IssueCert(id, p.SigningKey, b.now()), BoxKey: p.BoxKey, Methods: p.Methods, Reads: p.Reads, Name: p.Name})
	return appwire.ClientGrant{Cert: grant.Cert, Identity: grant.Identity, Relay: grant.Relay, Writer: grant.Writer, BoxKey: grant.BoxKey}, err
}
func (b *appBackend) PausePairedClient(id string, paused bool) error {
	r := b.clientRunner.Load()
	if r == nil {
		return appwire.Errorf(appwire.CodeNotFound, "paired client not found")
	}
	return r.Pause(id, paused)
}
func (b *appBackend) UnpairClient(id string) error {
	r := b.clientRunner.Load()
	if r == nil {
		return appwire.Errorf(appwire.CodeNotFound, "paired client not found")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return r.Unpair(ctx, id)
}

// stopClients joins the executor before any engine it may call is closed.
func (b *appBackend) stopClients() {
	b.clientStream.stop()
	done := func() chan struct{} {
		b.clientInit.Lock()
		defer b.clientInit.Unlock()
		if b.clientCancel != nil {
			b.clientCancel()
		}
		return b.clientDone
	}()
	if done != nil {
		<-done
	}
	if r := b.clientRunner.Load(); r != nil {
		_ = r.Close()
	}
}

// leaseClientChat seals and uploads the initial chat before queued creation
// releases its first turn. The ordinary drive then renews this real head's
// lease; no placeholder head can strand a phone-created conversation.
func (b *appBackend) leaseClientChat(ctx context.Context, agent appwire.Agent) error {
	a := b.agent(agent.ID)
	var transcript string
	b.locked(func() { transcript = a.transcript })
	folder, ok := session.FolderOf(transcript)
	if !ok {
		return errors.New("created chat has no cell folder")
	}
	c, err := cell.OpenAt(folder, agent.ID)
	if err != nil {
		return err
	}
	eng := cellstore.Engine{Workspace: agent.Workspace}
	if _, err = eng.Seal(ctx, c, cellstore.TurnInfo{}); err != nil {
		return err
	}
	s, err := syncOf()
	if err != nil {
		return err
	}
	if err = s.PublishCopy(ctx, eng, c, "", agent.Title); err != nil {
		return err
	}
	_, err = s.Dir.Acquire(ctx, agent.ID, directory.AcquireOpts{})
	return err
}
