package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/clientrun"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/reqsign"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

// phoneFixtureEnv is the switch a normal app leaves unset. With it, the app
// serves testing.open_ask and a loopback step URL. Without it, that method is
// absent and no step server is opened.
const phoneFixtureEnv = "CODEAF_PHONE_FIXTURE"

// phoneHost is the app the fixture step server calls. It is set for the life
// of one `codeaf app` process.
var phoneHost atomic.Pointer[appBackend]

// phoneFixturePending is set for the one conversation OpenPhoneFixture is
// about to open, and consumed when that conversation is assembled.
var phoneFixturePending atomic.Bool

// phoneFixtureAgent is that conversation's id, once CreateAgent has minted it.
var phoneFixtureAgent atomic.Value // string

// phoneFixtureErr is why the fixture ask did not open. Empty means none yet.
var phoneFixtureErr atomic.Value // string

// phoneFixtureReady is the ask once ListAsks can see it.
var phoneFixtureReady atomic.Value // appwire.PhoneFixtureAsk

// phoneFixtureMu keeps two callers from opening two fixture conversations.
var phoneFixtureMu sync.Mutex

func phoneFixtureEnabled() bool { return os.Getenv(phoneFixtureEnv) == "1" }

// phoneFixtureKeyless lets the fixture app open its process with no provider
// key. A normal app leaves it false and still refuses to open without one.
var phoneFixtureKeyless atomic.Bool

func phoneFixtureFail(err error) {
	if err == nil {
		return
	}
	phoneFixtureErr.Store(err.Error())
	fmt.Fprintf(os.Stderr, "phone fixture: %s\n", err)
}

func phoneSockPath() string { return filepath.Join(home.Dir(), "app", "phone.sock") }

// startPhoneHost opens the local pairing socket for every app. The fixture
// step server and the automatic ask open only when the fixture switch is on.
func startPhoneHost(b *appBackend, openAsk bool) error {
	phoneHost.Store(b)
	if err := listenPhonePair(b); err != nil {
		return err
	}
	if !phoneFixtureEnabled() {
		return nil
	}
	if err := servePhoneFixture(b); err != nil {
		return err
	}
	if openAsk {
		guard.Go("app/phone-fixture", func() {
			if _, err := b.OpenPhoneFixture(); err != nil {
				phoneFixtureFail(err)
			}
		})
	}
	return nil
}

// listenPhonePair is the trusted local door `codeaf phone pair` dials. A
// second process cannot open the client runner itself: the runner's lock is
// exclusive, and the running app already holds it once a client is paired.
func listenPhonePair(b *appBackend) error {
	dir := filepath.Join(home.Dir(), "app")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	path := phoneSockPath()
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	ln, err := net.Listen("unix", path)
	if err != nil {
		return err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = ln.Close()
		return err
	}
	guard.Go("app/phone-pair", func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			guard.Go("app/phone-pair-conn", func() { handlePhonePair(conn, b) })
		}
	})
	return nil
}

func handlePhonePair(conn net.Conn, b *appBackend) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(45 * time.Second))
	var params appwire.PairClientParams
	if err := json.NewDecoder(conn).Decode(&params); err != nil {
		writePhonePair(conn, appwire.ClientGrant{}, err)
		return
	}
	// Pairing needs an identity on this machine. A fresh home has none until
	// something asks, and the phone grant is that something.
	if _, err := identity.Ensure(home.Dir()); err != nil {
		writePhonePair(conn, appwire.ClientGrant{}, err)
		return
	}
	grant, err := b.PairClient(params)
	writePhonePair(conn, grant, err)
}

func writePhonePair(w io.Writer, grant appwire.ClientGrant, err error) {
	out := struct {
		Grant appwire.ClientGrant `json:"grant"`
		Error string              `json:"error,omitempty"`
	}{Grant: grant}
	if err != nil {
		out.Error = err.Error()
	}
	_ = json.NewEncoder(w).Encode(out)
}

// servePhoneFixture is the loopback the phone already posts to after an
// answer. It exists only while the fixture switch is on, and it binds to
// this machine only.
func servePhoneFixture(b *appBackend) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/ready", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(phoneReady(b))
	})
	mux.HandleFunc("/step", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method", http.StatusMethodNotAllowed)
			return
		}
		n, err := phoneStep(r.Context(), b)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"entries":%d}`, n)
	})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	guard.Go("app/phone-step", func() { _ = http.Serve(ln, mux) })
	fmt.Fprintf(os.Stderr, "PHONE_READY http://%s\n", ln.Addr().String())
	return nil
}

func phoneReady(b *appBackend) appwire.PhoneFixtureAsk {
	id, _ := phoneFixtureAgent.Load().(string)
	out := appwire.PhoneFixtureAsk{Agent: id, Title: session.PhoneFixtureHead}
	if id == "" {
		return out
	}
	asks, err := b.ListAsks(id)
	if err != nil || len(asks) == 0 {
		return out
	}
	out.Ask = asks[0].ID
	out.Title = asks[0].Title
	if len(asks[0].Options) > 0 {
		out.Option = asks[0].Options[0].ID
	}
	return out
}

func phoneStep(ctx context.Context, b *appBackend) (int, error) {
	if runner := b.clientRunner.Load(); runner != nil {
		if err := runner.Step(ctx); err != nil {
			return 0, err
		}
	}
	return phoneQueueEntries(ctx, b)
}

func phoneQueueEntries(ctx context.Context, b *appBackend) (int, error) {
	runner := b.clientRunner.Load()
	if runner == nil {
		return 0, nil
	}
	clients := runner.Clients()
	if len(clients) == 0 {
		return 0, nil
	}
	s, err := syncOf()
	if err != nil {
		return 0, err
	}
	signer := appClientSigner{s.Identity, s.Device}
	n := 0
	for _, c := range clients {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(s.Relay, "/")+"/v1/clients/"+c.ID+"/queue", nil)
		if err != nil {
			return 0, err
		}
		reqsign.Sign(req, nil, signer, time.Now())
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return 0, err
		}
		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			return 0, readErr
		}
		if resp.StatusCode == http.StatusNotFound {
			continue
		}
		if resp.StatusCode != http.StatusOK {
			return 0, fmt.Errorf("queue list %d %s", resp.StatusCode, body)
		}
		var listed struct {
			Entries []json.RawMessage `json:"entries"`
		}
		if err := json.Unmarshal(body, &listed); err != nil {
			return 0, err
		}
		n += len(listed.Entries)
	}
	return n, nil
}

// OpenPhoneFixture opens one real conversation and parks the phone's ask on
// it. The method is unknown unless the fixture switch is on, so a normal app
// has no such verb.
func (b *appBackend) OpenPhoneFixture() (appwire.PhoneFixtureAsk, error) {
	if !phoneFixtureEnabled() {
		return appwire.PhoneFixtureAsk{}, appwire.Errorf(appwire.CodeUnknownMethod, "this engine does not serve testing.open_ask")
	}
	phoneFixtureMu.Lock()
	defer phoneFixtureMu.Unlock()
	if ready, ok := phoneFixtureReady.Load().(appwire.PhoneFixtureAsk); ok && ready.Ask != "" {
		return ready, nil
	}
	workspace := filepath.Join(home.Dir(), "phone-workspace")
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		return appwire.PhoneFixtureAsk{}, err
	}
	phoneFixtureErr.Store("")
	phoneFixturePending.Store(true)
	view, err := b.CreateAgent(appwire.CreateAsk{Workspace: workspace})
	if err != nil {
		phoneFixturePending.Store(false)
		return appwire.PhoneFixtureAsk{}, err
	}
	phoneFixtureAgent.Store(view.ID)
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		if msg, _ := phoneFixtureErr.Load().(string); msg != "" {
			return appwire.PhoneFixtureAsk{}, errors.New(msg)
		}
		asks, err := b.ListAsks(view.ID)
		if err == nil {
			for _, ask := range asks {
				if ask.Title != session.PhoneFixtureHead {
					continue
				}
				ready := appwire.PhoneFixtureAsk{Agent: view.ID, Ask: ask.ID, Title: ask.Title}
				if len(ask.Options) > 0 {
					ready.Option = ask.Options[0].ID
				}
				phoneFixtureReady.Store(ready)
				return ready, nil
			}
		}
		a := b.agent(view.ID)
		var state string
		var opening bool
		b.locked(func() {
			state = a.view.State
			opening = a.opening
		})
		if state == appwire.StateStopped && !opening {
			if msg, _ := phoneFixtureErr.Load().(string); msg != "" {
				return appwire.PhoneFixtureAsk{}, errors.New(msg)
			}
			return appwire.PhoneFixtureAsk{}, errors.New("the fixture conversation did not open")
		}
		time.Sleep(50 * time.Millisecond)
	}
	return appwire.PhoneFixtureAsk{}, errors.New("the fixture ask did not open")
}

// phoneFixtureOnBoot parks the ask inside the conversation's own open, before
// the app replays open questions onto the ask list.
func phoneFixtureOnBoot(agent *session.Agent, transcript string) {
	if agent == nil || !phoneFixturePending.CompareAndSwap(true, false) {
		return
	}
	if err := parkPhoneFixture(agent, transcript); err != nil {
		phoneFixtureFail(err)
	}
}

func parkPhoneFixture(agent *session.Agent, transcript string) error {
	if !cell.Available() {
		return errors.New(cell.UnavailableReason())
	}
	if relay := syncsetup.Resolve(home.Dir()); !relay.On() {
		return errors.New("sync is off: set CODEAF_SYNC_URL to your sync address")
	}
	if _, err := identity.Ensure(home.Dir()); err != nil {
		return err
	}
	dev, err := identity.Device(home.Dir())
	if err != nil {
		return err
	}
	s, err := syncOf()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := s.Dir.PutDevice(ctx, dev.ID(), directory.Device{V: 1}); err != nil {
		return err
	}
	cellID := appAgentID(transcript)
	if cellID == "" || cellID == "." {
		return errors.New("fixture conversation has no id")
	}
	fence, err := fixtureCell(ctx, s.Dir, dev.ID(), cellID)
	if err != nil {
		return err
	}
	// A directory lease lasts 90s. The phone's simulator takes longer than
	// that to launch, and a later round then publishes an empty list in place
	// of the ask. Renew the lease for as long as this app runs.
	guard.Go("phone/fixture-lease", func() { keepFixtureLease(s.Dir, cellID, fence) })
	return agent.ParkFixtureConsent()
}

// fixtureCell opens the chat cell and returns the fence this device holds.
func fixtureCell(ctx context.Context, dir directory.Client, device, cellID string) (uint64, error) {
	view, err := dir.Create(ctx, cellID, directory.CellInit{Head: strings.Repeat("1", 64), Class: "chat"})
	if err == nil {
		return view.Cell.Lease.Fence, nil
	}
	if !errors.Is(err, directory.ErrExists) {
		return 0, err
	}
	listing, err := dir.List(ctx)
	if err != nil {
		return 0, err
	}
	held := listing.Cells[cellID]
	if held.Lease.Device == device && held.Lease.Fence != 0 && held.Lease.Expires > listing.Now {
		return held.Lease.Fence, nil
	}
	taken, err := dir.Acquire(ctx, cellID, directory.AcquireOpts{})
	if err != nil {
		return 0, err
	}
	return taken.Cell.Lease.Fence, nil
}

// keepFixtureLease renews the fixture chat before the directory drops it.
// One beat lands immediately so the first expiry cannot fall inside the
// launch wait, and the rest land three times inside a lease.
func keepFixtureLease(dir directory.Client, cellID string, fence uint64) {
	beat := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		view, err := dir.Heartbeat(ctx, cellID, directory.Beat{Fence: fence})
		if err != nil {
			fmt.Fprintf(os.Stderr, "phone fixture: %s\n", err)
			return
		}
		if view.Cell.Lease.Fence != 0 {
			fence = view.Cell.Lease.Fence
		}
	}
	beat()
	ticker := time.NewTicker(directory.LeaseTTL / 3)
	defer ticker.Stop()
	for range ticker.C {
		beat()
	}
}

// runPhone is `codeaf phone pair`. It is absent from the usage text for the
// same reason relay is: the running app is the door, and this command only
// carries a grant across the local socket.
func runPhone(args []string) error {
	if len(args) != 1 || args[0] != "pair" {
		return fmt.Errorf("usage: codeaf phone pair")
	}
	var in struct {
		SigningKey []byte   `json:"signing_key"`
		BoxKey     []byte   `json:"box_key"`
		Methods    []string `json:"methods"`
		Reads      []string `json:"reads"`
		Name       string   `json:"name"`
	}
	if err := json.NewDecoder(bufio.NewReader(os.Stdin)).Decode(&in); err != nil {
		return fmt.Errorf("codeaf phone pair: read the client's public keys on stdin: %w", err)
	}
	if len(in.Methods) == 0 {
		in.Methods = clientrun.PhoneMethods()
	}
	if len(in.Reads) == 0 {
		in.Reads = clientrun.PhoneReads()
	}
	if strings.TrimSpace(in.Name) == "" {
		in.Name = "phone"
	}
	conn, err := net.DialTimeout("unix", phoneSockPath(), 2*time.Second)
	if err != nil {
		return fmt.Errorf("codeaf app is not running")
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(45 * time.Second))
	if err := json.NewEncoder(conn).Encode(appwire.PairClientParams{
		SigningKey: in.SigningKey, BoxKey: in.BoxKey, Methods: in.Methods, Reads: in.Reads, Name: in.Name,
	}); err != nil {
		return err
	}
	var resp struct {
		Grant appwire.ClientGrant `json:"grant"`
		Error string              `json:"error"`
	}
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return err
	}
	if resp.Error != "" {
		return errors.New(resp.Error)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false)
	return enc.Encode(resp.Grant)
}
