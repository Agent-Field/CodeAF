package main

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/cell/celltest"
	"github.com/Agent-Field/codeaf/internal/clientproto"
	"github.com/Agent-Field/codeaf/internal/clientrun"
	"github.com/Agent-Field/codeaf/internal/devname"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/relayserve"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

// TestAPhoneStartsAChatWhileRelayStorageIsOff states the contract in a person's
// words: with relay storage off, a phone can still start a chat on a computer.
// Storage off means the relay keeps no chat content; it does not mean a computer
// may not start a chat, because a phone finds a computer's chats through the
// lease that computer holds in the directory, and the lease is coordination.
//
// The relay is the real Go relay with storage off, and the create goes through the
// shipped lease step (leaseClientChat), not a stand-in. Two worlds are the same
// promise: a computer that has already heard the relay's answer asks nothing of
// the store at all, and one that has not is refused once and carries on.
func TestAPhoneStartsAChatWhileRelayStorageIsOff(t *testing.T) {
	for _, tc := range []struct {
		name string
		// learned is whether this computer already holds the relay's storage answer.
		learned bool
	}{
		{"the computer already knows storage is off", true},
		{"the computer learns it from the relay's first refusal", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			celltest.Available(t)
			t.Setenv("CODEAF_TELEMETRY", "off")
			t.Setenv("DO_NOT_TRACK", "1")
			homeDir := t.TempDir()
			t.Setenv(home.EnvVar, homeDir)
			t.Setenv("CODEAF_PROFILE_DIR", "")

			relay := relayserve.New(relayserve.Config{Store: t.TempDir(), StorageOff: true})
			// THE PROMISE IS ABOUT UPLOADS. A computer that knows storage is off
			// must never try to hand the store chat content; a read of the store's
			// own answer (its stats) by the desktop backend in the background is
			// not an upload, and whether one lands inside this test is a race. So
			// every store request is recorded and only the uploads are counted.
			var storeMu sync.Mutex
			var storeSeen []string
			storeWrites := func() (writes int, seen []string) {
				storeMu.Lock()
				defer storeMu.Unlock()
				for _, r := range storeSeen {
					// An upload is a frame posted to the store (blobstore's
					// pathFrames); the other POSTs there (has, locate, objects)
					// are reads.
					if r == http.MethodPost+" /v1/store/frames" {
						writes++
					}
				}
				return writes, slices.Clone(storeSeen)
			}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasPrefix(r.URL.Path, "/v1/store/") {
					storeMu.Lock()
					storeSeen = append(storeSeen, r.Method+" "+r.URL.Path)
					storeMu.Unlock()
				}
				relay.Handler.ServeHTTP(w, r)
			}))
			t.Cleanup(func() { srv.Close(); _ = relay.Close() })
			t.Setenv(syncsetup.URLVar, srv.URL)

			if _, err := identity.Ensure(homeDir); err != nil {
				t.Fatal(err)
			}
			if _, err := devname.Set(homeDir, "studio-mac"); err != nil {
				t.Fatal(err)
			}
			phone := enrollPhoneAtHome(t, srv.URL, homeDir, "iPhone", []string{clientproto.MethodCreate}, []string{"asks.list", "agents.entries"})

			rec := newAppRecorder()
			b := newAppBackend(rec, func(ask appOpenAsk) (*appConn, error) {
				conv := &fakeConversation{script: []session.Event{{Kind: session.EventTurnDone}}}
				return &appConn{Conv: conv, Workspace: ask.Workspace, Transcript: ask.Transcript, Release: func() {}}, nil
			})
			b.state = &appState{path: filepath.Join(t.TempDir(), appStateName)}
			b.profileDir = t.TempDir()
			defer b.Close()

			s, err := syncOf()
			if err != nil {
				t.Fatal(err)
			}
			if tc.learned {
				if err := syncsetup.SaveStorage(s.Home, s.Relay, s.Identity.ID(), "off"); err != nil {
					t.Fatal(err)
				}
			}
			// The runner is the shipped one, with the shipped lease step.
			r, err := clientrun.Open(b.clientRunnerConfig(s, nil, nil))
			if err != nil {
				t.Fatal(err)
			}
			b.clientRunner.Store(r)
			setActivePhoneUnpairer(phoneRunnerUnpairer{r: r})
			defer b.stopClients()

			ctx := t.Context()
			if err := r.Step(ctx); err != nil {
				t.Fatalf("initial Step: %v", err)
			}
			card := readExecutorCardFromRelay(t, srv.URL, phone, s.Device.ID())
			var execBoxPub [32]byte
			copy(execBoxPub[:], card.BoxPub)
			enqueuePhoneCall(t, srv.URL, phone, execBoxPub, 1, clientproto.MethodCreate, s.Device.ID(), map[string]string{"client_id": "create-cid-off"}, false)
			if err := r.Step(ctx); err != nil {
				t.Fatalf("Step create: %v", err)
			}

			var outSlot struct {
				Sealed []byte `json:"sealed"`
			}
			phoneDoJSON(t, srv.URL, phone.signer, http.MethodGet, "/v1/clients/"+phone.id+"/slots/"+clientrun.SlotName("outcomes", s.Device.ID()), nil, &outSlot, http.StatusOK)
			plain, err := clientproto.OpenSlot(outSlot.Sealed, &execBoxPub, phone.boxPriv)
			if err != nil {
				t.Fatalf("OpenSlot outcomes: %v", err)
			}
			outcomes, err := clientproto.DecodeOutcomes(plain.Payload)
			if err != nil || len(outcomes) != 1 || outcomes[0].Status != clientproto.OutcomeDone || outcomes[0].Agent == "" {
				t.Fatalf("with relay storage off, the create's outcome = %+v, %v; want done with a chat", outcomes, err)
			}
			chat := outcomes[0].Agent

			// The computer holds the chat: the directory names it, leased to this
			// computer, which is how the phone's later calls find it.
			listing, err := s.Dir.List(ctx)
			if err != nil {
				t.Fatal(err)
			}
			held, ok := listing.Cells[chat]
			if !ok || held.Lease.Device != s.Device.ID() || held.Lease.Expires <= listing.Now {
				t.Fatalf("directory record of %s = %+v (now %d), want leased to %s", chat, held, listing.Now, s.Device.ID())
			}
			// And the relay was handed nothing of the chat: no head it cannot store, and no name.
			if held.Head != "" || held.Title != "" || held.Folder != "" {
				t.Fatalf("with storage off the record of %s names content: head %q, title %q, folder %q", chat, held.Head, held.Title, held.Folder)
			}
			// A create that is delivered again, after a crash or a retry, holds the same
			// chat once: the second lease finds the record and only takes the lease back.
			if err := b.leaseClientChat(ctx, appwire.Agent{ID: chat}); err != nil {
				t.Fatalf("leasing the same chat a second time with storage off: %v", err)
			}
			if writes, seen := storeWrites(); tc.learned && writes != 0 {
				t.Fatalf("a computer that knows storage is off tried to upload to the store %d times (store requests: %v)", writes, seen)
			}

			// The chat is the engine's and the phone's start is written in it.
			entries, _, err := b.Entries(chat, "", 100)
			if err != nil {
				t.Fatal(err)
			}
			started := false
			for _, e := range entries {
				if e.Kind == appwire.EntryNotice && e.Text == "iPhone started this chat." {
					started = true
				}
			}
			if !started {
				t.Fatalf("the chat has no 'iPhone started this chat.' line: %+v", entries)
			}
		})
	}
}
