package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"

	"github.com/Agent-Field/codeaf/internal/clientrun"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/relayserve"
	"github.com/Agent-Field/codeaf/internal/reqsign"
	"github.com/Agent-Field/codeaf/internal/session"
	"golang.org/x/crypto/nacl/box"
)

type dieAfterCreate struct{ appwire.Engine }

func (e dieAfterCreate) CreateAgent(p appwire.CreateAsk) (appwire.Agent, error) {
	a, err := e.Engine.CreateAgent(p)
	if err == nil {
		if err := os.WriteFile(filepath.Join(home.Dir(), "created-id"), []byte(a.ID), 0600); err != nil {
			panic(err)
		}
		os.Exit(73)
	}
	return a, err
}

func TestInterruptedClientCreationKeepsItsChatLine(t *testing.T) {
	if relayURL := os.Getenv("CODEAF_TEST_CREATION_RELAY"); relayURL != "" {
		root := os.Getenv("CODEAF_TEST_CREATION_HOME")
		t.Setenv(home.EnvVar, root)
		b := newAppBackend(newAppRecorder(), nil)
		if os.Getenv("CODEAF_TEST_CREATION_AT_MINT") != "" {
			mint := b.mint
			b.mint = func(workspace string, before ...func(string) error) (string, error) {
				if os.Getenv("CODEAF_TEST_CREATION_AT_MINT") == "before" {
					return mint(workspace, func(transcript string) error {
						for _, hook := range before {
							if err := hook(transcript); err != nil {
								return err
							}
						}
						if err := os.WriteFile(filepath.Join(home.Dir(), "created-id"), []byte(appAgentID(transcript)), 0600); err != nil {
							return err
						}
						os.Exit(73)
						return nil
					})
				}
				transcript, err := mint(workspace, before...)
				if err == nil {
					if err := os.WriteFile(filepath.Join(home.Dir(), "created-id"), []byte(appAgentID(transcript)), 0600); err != nil {
						panic(err)
					}
					os.Exit(73)
				}
				return transcript, err
			}
		}
		id, err := identity.Load(root)
		if err != nil {
			t.Fatal(err)
		}
		dev, err := identity.Device(root)
		if err != nil {
			t.Fatal(err)
		}
		signer := appClientSigner{id, dev}
		dir := directory.NewHTTP(relayURL, reqsign.SignFor(signer, time.Now), http.DefaultClient)
		r, err := clientrun.Open(clientrun.Config{Home: root, Relay: relayURL, Signer: signer, Directory: dir, Engine: dieAfterCreate{b}, Log: b.logClientCall})
		if err != nil {
			t.Fatal(err)
		}
		if err := r.Step(context.Background()); err != nil {
			t.Fatal(err)
		}
		t.Fatal("create did not reach crash point")
	}
	root := t.TempDir()
	t.Setenv(home.EnvVar, root)
	t.Setenv("CODEAF_PROFILE_DIR", "")
	t.Setenv("CODEAF_SYNC_URL", "off")
	relay := relayserve.New(relayserve.Config{Store: t.TempDir()})
	srv := httptest.NewServer(relay.Handler)
	defer relay.Close()
	defer srv.Close()
	id, err := identity.Ensure(root)
	if err != nil {
		t.Fatal(err)
	}
	dev, err := identity.Device(root)
	if err != nil {
		t.Fatal(err)
	}
	signer := appClientSigner{id, dev}
	dir := directory.NewHTTP(srv.URL, reqsign.SignFor(signer, time.Now), srv.Client())
	if err := dir.PutDevice(context.Background(), dev.ID(), directory.Device{V: 1}); err != nil {
		t.Fatal(err)
	}
	r, err := clientrun.Open(clientrun.Config{Home: root, Relay: srv.URL, Signer: signer, Directory: dir})
	if err != nil {
		t.Fatal(err)
	}
	seed, err := identity.NewSeed()
	if err != nil {
		t.Fatal(err)
	}
	pub, _, err := box.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	grant, err := r.Pair(context.Background(), clientrun.Pairing{Cert: identity.IssueCert(id, seed.Public(), time.Now()), BoxKey: pub[:], Methods: []string{"agents.create"}, Reads: []string{"asks.list"}})
	if err != nil {
		t.Fatal(err)
	}
	clientDev, err := seed.Join(grant.Cert)
	if err != nil {
		t.Fatal(err)
	}
	client := scopedSigner{grant.Identity, clientDev}
	raw, _ := json.Marshal(clientrun.Call{Method: "agents.create", Params: json.RawMessage(`{"text":"new work"}`)})
	sealed, err := box.SealAnonymous(nil, raw, (*[32]byte)(grant.BoxKey), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	clientWire(t, srv.URL, client, "POST", "/v1/clients/"+clientDev.ID()+"/queue", map[string]any{"number": 1, "at": time.Now().UnixMilli(), "method": "agents.create", "target": "any", "sealed": sealed}, nil)
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestInterruptedClientCreationKeepsItsChatLine$")
	cmd.Env = append(os.Environ(), home.EnvVar+"="+t.TempDir(), "CODEAF_TEST_CREATION_RELAY="+srv.URL, "CODEAF_TEST_CREATION_HOME="+root)
	out, err := cmd.CombinedOutput()
	if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 73 {
		t.Fatalf("crash point: %v\n%s", err, out)
	}
	b := newAppBackend(newAppRecorder(), nil)
	defer b.Close()
	r, err = clientrun.Open(clientrun.Config{Home: root, Relay: srv.URL, Signer: signer, Directory: dir, Engine: b, Log: b.logClientCall})
	if err != nil {
		t.Fatal(err)
	}
	b.clientRunner.Store(r)
	createdID, err := os.ReadFile(filepath.Join(root, "created-id"))
	if err != nil {
		t.Fatal(err)
	}
	agentID := string(createdID)
	receipts := r.Receipts()
	if len(receipts) != 1 || receipts[0].Status != "started" || receipts[0].Agent != agentID {
		t.Fatalf("created chat lost from started receipt: %+v", receipts)
	}
	absent := os.Getenv("CODEAF_TEST_CREATION_AT_MINT") == "before"
	if absent {
		folder, ok := session.FolderOf(receipts[0].Transcript)
		if !ok {
			t.Fatal("reserved chat lost its folder path")
		}
		if _, err := os.Stat(folder); !os.IsNotExist(err) {
			t.Fatalf("reserved-only chat created a folder: %v", err)
		}
	}
	// Read the durable chat through the desktop door after the process died.
	if !absent {
		entries, _, err := b.Entries(agentID, "", 200)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, entry := range entries {
			if strings.Contains(entry.Text, clientDev.ID()) && strings.Contains(entry.Text, "agents.create") {
				found = true
			}
		}
		if !found {
			t.Fatal("created chat has no paired-client line after crash")
		}
	}
	if err := r.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	var queue struct{ Entries []json.RawMessage }
	clientWire(t, srv.URL, signer, "GET", "/v1/clients/"+clientDev.ID()+"/queue", nil, &queue)
	if len(queue.Entries) != 0 {
		t.Fatal("uncertain creation was not acknowledged")
	}
}

func TestInterruptedClientMintKeepsItsChatLine(t *testing.T) {
	t.Setenv("CODEAF_TEST_CREATION_AT_MINT", "after")
	TestInterruptedClientCreationKeepsItsChatLine(t)
}

func TestInterruptedClientReservationDoesNotInventAChat(t *testing.T) {
	t.Setenv("CODEAF_TEST_CREATION_AT_MINT", "before")
	TestInterruptedClientCreationKeepsItsChatLine(t)
}
