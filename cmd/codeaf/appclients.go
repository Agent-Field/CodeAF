package main

import (
	"context"
	"crypto/ed25519"
	"errors"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/clientproto"
	"github.com/Agent-Field/codeaf/internal/clientrun"
	"github.com/Agent-Field/codeaf/internal/devname"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/dirwatch"
	"github.com/Agent-Field/codeaf/internal/furrowbin"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

// appClientOperationTimeout bounds individual pause/unpair or directory lookups
// initiated on behalf of local client management.
const appClientOperationTimeout = 30 * time.Second

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
			r.Delta(v)
		case appwire.EntryAdded:
			if v.Entry.Msg != nil {
				r.ClearStream(v.Entry.Agent)
			}
		case appwire.TurnDone:
			r.ClearStream(v.Agent)
		case appwire.TurnRetrying:
			r.ClearStream(v.Agent)
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

// phoneRunnerUnpairer adapts a live phone runner to tui3.PhoneUnpairer so
// removing a phone in /devices or `codeaf devices revoke` notifies the runner
// in the same process.
type phoneRunnerUnpairer struct {
	r *clientrun.Runner
}

func (u phoneRunnerUnpairer) Unpair(ctx context.Context, id string) error {
	return u.r.Unpair(ctx, id)
}

type appClientDirectory struct {
	directory.Client
	b   *appBackend
	key []byte
}

func (d appClientDirectory) List(ctx context.Context) (directory.Listing, error) {
	listing, err := d.Client.List(ctx)
	if err == nil && d.b != nil && len(d.key) > 0 {
		d.b.rememberClientNames(listing.Devices, d.key)
	}
	return listing, err
}

type appClientWatchDirectory struct {
	appClientDirectory
}

func (d appClientWatchDirectory) Watch(ctx context.Context) (dirwatch.Stream, error) {
	return d.Client.(directory.Watcher).Watch(ctx)
}

type appClientServingDirectory struct {
	appClientDirectory
}

func (d appClientServingDirectory) Watch(ctx context.Context) (dirwatch.Stream, error) {
	if w, ok := d.Client.(directory.Watcher); ok {
		return w.Watch(ctx)
	}
	return d.Client.(directory.ServingWatcher).WatchServing(ctx)
}

func (d appClientServingDirectory) WatchServing(ctx context.Context) (dirwatch.Stream, error) {
	return d.Client.(directory.ServingWatcher).WatchServing(ctx)
}

func wrapClientDirectory(inner directory.Client, b *appBackend, key []byte) directory.Client {
	if inner == nil {
		return nil
	}
	base := appClientDirectory{Client: inner, b: b, key: key}
	if _, ok := inner.(directory.ServingWatcher); ok {
		return appClientServingDirectory{appClientDirectory: base}
	}
	if _, ok := inner.(directory.Watcher); ok {
		return appClientWatchDirectory{appClientDirectory: base}
	}
	return base
}

// startPhoneRunner opens and runs the phone runner when sync is on and this
// computer may talk for a phone (either its identity is shared or at least one
// phone is approved on this solo computer). When sync is off, no identity
// exists, or the identity is solo with no approved phone, it does nothing and
// makes no request.
func (b *appBackend) startPhoneRunner() {
	b.ensurePhoneApprovalWatch()
	dir := b.relayHome()
	if !syncsetup.PhoneMayTalk(dir) {
		return
	}
	if _, err := b.clients(); err != nil {
		b.emit.Emit(appwire.EventLog, appwire.Log{Level: "warn", Message: "paired clients could not start"})
	}
}

func (b *appBackend) ensurePhoneApprovalWatch() {
	b.clientInit.Lock()
	defer b.clientInit.Unlock()
	if b.clientPhonesStop != nil {
		return
	}
	select {
	case <-b.left:
		return
	default:
	}
	stopListener := syncsetup.OnPhonesChange(func(changedHome string) {
		if changedHome != b.relayHome() {
			return
		}
		if syncsetup.PhoneMayTalk(changedHome) {
			b.startPhoneRunner()
			if r := b.clientRunner.Load(); r != nil {
				r.Wake()
			}
		} else if r := b.clientRunner.Load(); r != nil {
			r.Changed()
		}
	})
	// A PHONE APPROVED BY ANOTHER PROCESS, such as `codeaf pair approve` in a terminal
	// while this app is open, is recorded on disk and no listener here hears it, so the
	// gate is looked at on a slow timer as well.
	ctx, cancel := context.WithCancel(context.Background())
	ticker := time.NewTicker(phoneGatePollEvery)
	guard.Go("app/phone-gate", func() {
		defer ticker.Stop()
		watchPhoneGate(ctx, b.relayHome(), ticker.C, func() {
			b.startPhoneRunner()
			if r := b.clientRunner.Load(); r != nil {
				r.Wake()
			}
		})
	})
	b.clientPhonesStop = func() { stopListener(); cancel() }
}

// phoneGatePollEvery is how often a host looks at the phone gate for a change another
// process made, because the gate lives on disk and a listener hears only this process.
const phoneGatePollEvery = 5 * time.Second

// watchPhoneGate calls changed each time the phone gate for home goes from closed to
// open, on each tick, until ctx ends. The gate opens when a phone is approved on a
// computer that had gone quiet, and the runner must not wait out a long backoff to
// serve it.
func watchPhoneGate(ctx context.Context, home string, ticks <-chan time.Time, changed func()) {
	open := syncsetup.PhoneMayTalk(home)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticks:
			now := syncsetup.PhoneMayTalk(home)
			if now && !open {
				changed()
			}
			open = now
		}
	}
}

func (b *appBackend) clientRunnerConfig(s *syncsetup.Sync, dir directory.Client, onPaused func(string)) clientrun.Config {
	if dir == nil {
		dir = s.PhoneDirectory()
	}
	name := devname.Name(s.Home)
	if !clientproto.ValidCardName(name) {
		name = clientrun.DefaultCardName
	}
	wrapped := wrapClientDirectory(dir, b, directory.MetadataKey(s.Identity.CellKey()))
	return clientrun.Config{
		Home:           s.Home,
		Relay:          s.Relay,
		Name:           name,
		Identity:       s.Identity.PublicKey(),
		Signer:         appClientSigner{s.Identity, s.Device},
		Directory:      wrapped,
		Engine:         b,
		HTTP:           s.PhoneHTTP(),
		Now:            b.now,
		Log:            b.logClientCall,
		LeaseCreated:   b.leaseClientChat,
		LocalChats:     func() ([]string, bool) { return b.phoneLocalChats(s.Home) },
		OnPausedChange: onPaused,
	}
}

// phoneLocalChats answers which chats this computer holds while it is the only
// computer of its identity, for the phone runner (contract P13). A solo computer
// publishes no directory cell, so the directory cannot say which chats are its
// own and, with no other computer, there is nothing to arbitrate. It is NOT solo
// the moment MayTalk holds, and then the directory decides again, so the answer
// is read at each decision and never kept.
//
// The chats are the saved ones a person has spoken in, plus any this process is
// running or opening, whose first words may not be on disk yet: a chat a phone
// just made has no words until its first message arrives. Archived chats, a
// factory's own conversations and hosted views are not the phone's to reach.
func (b *appBackend) phoneLocalChats(home string) ([]string, bool) {
	if syncsetup.MayTalk(home) {
		return nil, false
	}
	archived := map[string]bool{}
	_ = b.state.read(func(f *appStateFile) {
		for id := range f.Archived {
			archived[id] = true
		}
	})
	seen := map[string]bool{}
	var ids []string
	for _, row := range session.ReadWorld(session.PlacesRoot()).Sessions() {
		if appChatStaging(row.Dir) || row.DeletionPending || factoryOwnedConversation(row.Transcript) || archived[row.ID] {
			continue
		}
		seen[row.ID] = true
		ids = append(ids, row.ID)
	}
	b.locked(func() {
		for id, a := range b.agents {
			if seen[id] || archived[id] || a.view.FactoryOwned || a.hosted != nil || a.liveOnly() || factoryOwnedConversation(a.transcript) {
				continue
			}
			if a.live != nil || a.opening {
				ids = append(ids, id)
			}
		}
	})
	return ids, true
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
	r, err := clientrun.Open(b.clientRunnerConfig(s, nil, nil))
	if err != nil {
		return nil, err
	}
	b.startOpenedClientRunnerLocked(r)
	return r, nil
}

func (b *appBackend) startOpenedClientRunnerLocked(r *clientrun.Runner) {
	ctx, cancel := context.WithCancel(context.Background())
	b.clientCancel = cancel
	b.clientDone = make(chan struct{})
	b.clientRunner.Store(r)
	activePhoneUnpairer = phoneRunnerUnpairer{r: r}
	guard.Go("app/clients", func() {
		defer close(b.clientDone)
		stop := syncsetup.WatchPublishes(r.Changed)
		defer stop()
		r.Run(ctx)
	})
}

func (b *appBackend) rememberClientNames(devices map[string]directory.Device, key []byte) {
	if len(devices) == 0 || len(key) == 0 {
		return
	}
	b.clientNamesMu.Lock()
	defer b.clientNamesMu.Unlock()
	if b.clientNames == nil {
		b.clientNames = map[string]string{}
	}
	for id, dev := range devices {
		if name, err := directory.OpenName(key, dev.Name); err == nil && strings.TrimSpace(name) != "" {
			b.clientNames[id] = devname.Shown(strings.TrimSpace(name))
		}
	}
}

func (b *appBackend) lookupClientName(clientID string) string {
	b.clientNamesMu.Lock()
	defer b.clientNamesMu.Unlock()
	return b.clientNames[clientID]
}

func (b *appBackend) clientPhoneName(clientID string) string {
	if clientID == "" {
		return "Your phone"
	}
	if name := b.lookupClientName(clientID); name != "" {
		return name
	}
	dir := b.relayHome()
	if id, err := identity.Load(dir); err == nil {
		key := directory.MetadataKey(id.CellKey())
		if b.devices != nil {
			if dev, err := identity.LoadDevice(dir); err == nil {
				snap := b.devices.watch.Snapshot(id, dev)
				b.rememberClientNames(snap.Devices, key)
				if name := b.lookupClientName(clientID); name != "" {
					return name
				}
			}
		}
		if syncsetup.PhoneMayTalk(dir) {
			if s, err := syncOf(); err == nil {
				ctx, cancel := context.WithTimeout(context.Background(), appClientOperationTimeout)
				listing, lerr := s.PhoneDirectory().List(ctx)
				cancel()
				if lerr == nil {
					b.rememberClientNames(listing.Devices, key)
					if name := b.lookupClientName(clientID); name != "" {
						return name
					}
				}
			}
		}
	}
	return "Your phone"
}

// logClientCall keeps chat history independently of replay watermarks, so
// acknowledgement and unpair can discard runner receipts without losing lines.
func (b *appBackend) logClientCall(receipt clientrun.Receipt) error {
	if receipt.Agent == "" {
		return nil
	}
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
	entry, ok := appClientEntry(b.clientPhoneName(receipt.Client), receipt)
	if !ok {
		return nil
	}
	a, endRead, err := b.findCrewOwner(receipt.Agent)
	if err != nil {
		return err
	}
	defer endRead()
	if err := b.keepAppEntry(a, entry, ""); err != nil {
		return err
	}
	b.emit.Emit(appwire.EventEntryAdded, appwire.EntryAdded{Entry: entry})
	return nil
}

func appClientEntry(phoneName string, receipt clientrun.Receipt) (appwire.Entry, bool) {
	phoneName = strings.TrimSpace(phoneName)
	if phoneName == "" {
		phoneName = "Your phone"
	}
	var text string
	switch receipt.Status {
	case "claiming":
		return appwire.Entry{}, false
	case "started":
		// A create records its notice once its folder exists (BeforeStart or
		// crash recovery after mint); every other method logs only when finished.
		if receipt.Method != clientproto.MethodCreate {
			return appwire.Entry{}, false
		}
		text = phoneName + " started this chat."
	case clientproto.OutcomeDone:
		switch receipt.Method {
		case clientproto.MethodCreate:
			text = phoneName + " started this chat."
		case clientproto.MethodStop:
			text = phoneName + " stopped this chat."
		case clientproto.MethodAnswer, clientproto.MethodInboxAnswer:
			text = phoneName + " answered a question."
		case clientproto.MethodSend:
			// A send adds no notice because the message is the person's own entry.
			return appwire.Entry{}, false
		default:
			return appwire.Entry{}, false
		}
	case clientproto.OutcomeRefused, clientproto.OutcomeExpired, "failed":
		text = phoneName + " asked for something that could not be done."
	default:
		return appwire.Entry{}, false
	}
	at, _ := time.Parse(time.RFC3339Nano, receipt.At)
	id := "client-" + receipt.Client + "-" + strconv.FormatInt(receipt.Number, 10)
	return appwire.Entry{
		ID:    id,
		Agent: receipt.Agent,
		Kind:  appwire.EntryNotice,
		At:    at,
		Text:  text,
	}, true
}

func (b *appBackend) ListPairedClients() ([]appwire.PairedClient, error) {
	r := b.clientRunner.Load()
	if r == nil {
		return []appwire.PairedClient{}, nil
	}
	clients := r.Clients()
	out := make([]appwire.PairedClient, 0, len(clients))
	for _, c := range clients {
		out = append(out, appwire.PairedClient{
			ID:      c.ID,
			Name:    b.clientPhoneName(c.ID),
			Methods: c.Methods,
			Reads:   c.Reads,
			Paused:  c.Paused,
		})
	}
	return out, nil
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
	ctx, cancel := context.WithTimeout(context.Background(), appClientOperationTimeout)
	defer cancel()
	err := r.Unpair(ctx, id)
	if err == nil || errors.Is(err, clientrun.ErrClientNotFound) {
		// The runner has revoked and forgotten the phone, or never knew or already forgot
		// it; either way a solo computer stops serving phones, and goes quiet, once the
		// last recorded one is gone (contract P13).
		if forgetErr := syncsetup.ForgetPhone(b.relayHome(), id); forgetErr != nil && err == nil {
			return forgetErr
		}
	}
	return err
}

// stopClients joins the executor before any engine it may call is closed.
func (b *appBackend) stopClients() {
	done := func() chan struct{} {
		b.clientInit.Lock()
		defer b.clientInit.Unlock()
		if b.clientPhonesStop != nil {
			b.clientPhonesStop()
			b.clientPhonesStop = nil
		}
		if b.clientCancel != nil {
			b.clientCancel()
		}
		return b.clientDone
	}()
	if done != nil {
		<-done
	}
	if r := b.clientRunner.Swap(nil); r != nil {
		if activePhoneUnpairer == (phoneRunnerUnpairer{r: r}) {
			activePhoneUnpairer = nil
		}
		_ = r.Close()
	}
}

// leaseClientChat seals and uploads the initial chat before queued creation
// releases its first turn. The ordinary drive then renews this real head's
// lease; no placeholder head can strand a phone-created conversation.
func (b *appBackend) leaseClientChat(ctx context.Context, agent appwire.Agent) error {
	s, err := syncOf()
	if err != nil {
		return err
	}
	if !syncsetup.MayTalk(s.Home) {
		return nil
	}
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
	if !furrowbin.CellsAvailable() {
		if _, err := s.Dir.Create(ctx, agent.ID, directory.CellInit{Class: string(cell.HostBound)}); err != nil && !errors.Is(err, directory.ErrExists) {
			return err
		}
		_, err = s.Dir.Acquire(ctx, agent.ID, directory.AcquireOpts{})
		return err
	}
	eng := cellstore.Engine{Workspace: agent.Workspace}
	if _, err = eng.Seal(ctx, c, cellstore.TurnInfo{}); err != nil {
		return err
	}
	if err = s.PublishCopy(ctx, eng, c, "", agent.Title); err != nil {
		return err
	}
	_, err = s.Dir.Acquire(ctx, agent.ID, directory.AcquireOpts{})
	return err
}
