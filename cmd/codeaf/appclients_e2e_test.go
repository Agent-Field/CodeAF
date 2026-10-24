package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/clientrun"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/relayserve"
	"github.com/Agent-Field/codeaf/internal/reqsign"
	"github.com/Agent-Field/codeaf/internal/session"
	"golang.org/x/crypto/nacl/box"
)

// scopedSigner intentionally holds no root identity secrets, cell key or vault.
type scopedSigner struct {
	pub ed25519.PublicKey
	dev identity.Dev
}

func (s scopedSigner) IdentityKey() ed25519.PublicKey { return s.pub }
func (s scopedSigner) Cert() identity.Cert            { return s.dev.Cert }
func (s scopedSigner) Sign(raw []byte) []byte         { return s.dev.Sign(raw) }

func clientWire(t *testing.T, base string, s reqsign.Signer, method, path string, body, out any) {
	t.Helper()
	var raw []byte
	var err error
	if body != nil {
		raw, err = json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
	}
	req, err := http.NewRequest(method, base+path, bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	reqsign.Sign(req, raw, s, time.Now())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 && resp.StatusCode != 204 {
		t.Fatalf("%s %s: %d %s", method, path, resp.StatusCode, data)
	}
	if out != nil {
		if err := json.Unmarshal(data, out); err != nil {
			t.Fatal(err)
		}
	}
}

// The whole backend runs in process. Only the conversation's model-facing
// channel is scripted; pairing, signatures, slots, queue, answers and history
// cross their real public doors without a provider key or a simulator.
func TestScopedClientReadsAnswersAndAcksThroughTheAppBackend(t *testing.T) {
	root := t.TempDir()
	t.Setenv(home.EnvVar, root)
	t.Setenv("CODEAF_PROFILE_DIR", "")
	t.Setenv("CODEAF_SYNC_URL", "off")
	c, err := cell.CreateIn(filepath.Join(root, "sessions"), cell.Options{Class: cell.HostBound})
	if err != nil {
		t.Fatal(err)
	}
	transcript, err := c.Path(cell.TranscriptPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(transcript, nil, 0600); err != nil {
		t.Fatal(err)
	}
	conv := &fakeConversation{}
	conv.onAnswer = func(a session.Answer) {
		q := session.Question{Kind: a.Kind, ID: a.ID}
		conv.raise(session.Event{Kind: session.EventQuestionAnswered, Question: &q, Answer: &a})
	}
	rec := newAppRecorder()
	backend := newAppBackend(rec, func(ask appOpenAsk) (*appConn, error) {
		return &appConn{Conv: conv, Workspace: ask.Workspace, Transcript: transcript, Release: func() {}}, nil
	})
	defer backend.Close()
	backend.mint = func(string, ...func(string) error) (string, error) { return transcript, nil }
	agent, err := mustCreate(t, backend, appwire.CreateAsk{Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if agent.ID != c.ID {
		t.Fatalf("app chat id = %q; directory lease uses %q", agent.ID, c.ID)
	}

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
	full := appClientSigner{id, dev}
	dir := directory.NewHTTP(srv.URL, reqsign.SignFor(full, time.Now), srv.Client())
	ctx := context.Background()
	if err := dir.PutDevice(ctx, dev.ID(), directory.Device{V: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := dir.Create(ctx, c.ID, directory.CellInit{Head: strings.Repeat("1", 64), Class: "chat"}); err != nil {
		t.Fatal(err)
	}
	runner, err := clientrun.Open(clientrun.Config{Home: root, Relay: srv.URL, Signer: full, Directory: dir, Engine: backend, Log: backend.logClientCall, HTTP: srv.Client()})
	if err != nil {
		t.Fatal(err)
	}
	backend.clientRunner.Store(runner)
	seed, err := identity.NewSeed()
	if err != nil {
		t.Fatal(err)
	}
	pub, priv, err := box.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	params, _ := json.Marshal(appwire.PairClientParams{SigningKey: seed.Public(), BoxKey: pub[:], Methods: []string{"asks.answer"}, Reads: []string{"asks.list"}})
	result, err := appwire.Call(backend, "clients.pair", params)
	if err != nil {
		t.Fatal(err)
	}
	grant := result.(*appwire.ClientGrant)
	clientDev, err := seed.Join(grant.Cert)
	if err != nil {
		t.Fatal(err)
	}
	client := scopedSigner{grant.Identity, clientDev}
	question := session.Question{ID: 1, Kind: session.QuestionConsent, Head: "Apply the selected change?", Stakes: session.StakesIrreversible, Options: session.AnswerOptions(session.QuestionConsent)}
	conv.raise(session.Event{Kind: session.EventQuestion, Question: &question})
	rec.waitFor(t, 5*time.Second, appAskEvent(appwire.EventAskOpened))
	if err := runner.Step(ctx); err != nil {
		t.Fatal(err)
	}
	path := "/v1/clients/" + clientDev.ID()
	var slot struct{ Sealed []byte }
	clientWire(t, srv.URL, client, "GET", path+"/slots/"+clientrun.SlotName("asks.list", dev.ID()), nil, &slot)
	plain, ok := box.OpenAnonymous(nil, slot.Sealed, pub, priv)
	if !ok {
		t.Fatal("client cannot open slot")
	}
	var envelopes []struct {
		Writer string
		At     time.Time
		Agent  string
		Result appwire.AsksResult
	}
	if err := json.Unmarshal(plain, &envelopes); err != nil {
		t.Fatal(err)
	}
	if len(envelopes) != 1 || envelopes[0].Agent != agent.ID || envelopes[0].Writer != dev.ID() || envelopes[0].At.IsZero() || len(envelopes[0].Result.Asks) != 1 {
		t.Fatalf("slot envelope: %s", plain)
	}
	ask := envelopes[0].Result.Asks[0]
	call, _ := json.Marshal(clientrun.Call{Method: "asks.answer", Params: json.RawMessage(`{"agent":"` + agent.ID + `","id":"` + ask.ID + `","option":"allow_once"}`), FaceID: true})
	sealed, err := box.SealAnonymous(nil, call, (*[32]byte)(grant.BoxKey), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	clientWire(t, srv.URL, client, "POST", path+"/queue", map[string]any{"number": 1, "at": time.Now().UnixMilli(), "method": "asks.answer", "target": dev.ID(), "sealed": sealed}, nil)
	if err := runner.Step(ctx); err != nil {
		t.Fatal(err)
	}
	rec.waitFor(t, 5*time.Second, appAskEvent(appwire.EventAskClosed))
	var queue struct{ Entries []json.RawMessage }
	clientWire(t, srv.URL, full, "GET", path+"/queue", nil, &queue)
	if len(queue.Entries) != 0 {
		t.Fatal("answer was not acknowledged")
	}
	conv.mu.Lock()
	answers := append([]session.Answer(nil), conv.answers...)
	conv.mu.Unlock()
	if len(answers) != 1 || answers[0].Key != "1" {
		t.Fatalf("engine answers: %+v", answers)
	}
	entries, _, err := backend.Entries(agent.ID, "", 200)
	if err != nil {
		t.Fatal(err)
	}
	logged := false
	for _, entry := range entries {
		if strings.Contains(entry.Text, clientDev.ID()) && strings.Contains(entry.Text, "asks.answer") && !entry.At.IsZero() {
			logged = true
		}
	}
	if !logged {
		t.Fatalf("chat has no who/when/method log: %+v", entries)
	}
	if err := runner.Step(ctx); err != nil {
		t.Fatal(err)
	}
	clientWire(t, srv.URL, client, "GET", path+"/slots/"+clientrun.SlotName("asks.list", dev.ID()), nil, &slot)
	plain, ok = box.OpenAnonymous(nil, slot.Sealed, pub, priv)
	if !ok {
		t.Fatal("cannot open updated slot")
	}
	if err := json.Unmarshal(plain, &envelopes); err != nil {
		t.Fatal(err)
	}
	if len(envelopes) != 1 || envelopes[0].Agent != agent.ID || len(envelopes[0].Result.Asks) != 0 {
		t.Fatalf("answered ask remains in slot: %s", plain)
	}
	// The final unpair retains chat receipts, but a later startup must not
	// open sync or complain that sync is off merely to read those receipts.
	if _, err := appwire.Call(backend, "clients.unpair", json.RawMessage(`{"id":"`+clientDev.ID()+`"}`)); err != nil {
		t.Fatal(err)
	}
	if err := runner.Close(); err != nil {
		t.Fatal(err)
	}
	nextRec := newAppRecorder()
	next := newAppBackend(nextRec, nil)
	defer next.Close()
	next.startPairedClients()
	list, err := appwire.Call(next, "clients.list", nil)
	if err != nil {
		t.Fatal(err)
	}
	listed, _ := json.Marshal(list)
	if string(listed) != `{"clients":[]}` {
		t.Fatalf("unpaired startup lists %s", listed)
	}
	nextRec.mu.Lock()
	defer nextRec.mu.Unlock()
	for i, event := range nextRec.events {
		if event == appwire.EventLog {
			t.Fatalf("unpaired startup tried to open sync: %+v", nextRec.data[i])
		}
	}

}

func TestQueuedFaceIDCoversTheResolvedConsentSet(t *testing.T) {
	for _, scenario := range []struct {
		name            string
		batchItem, late bool
	}{{"batch", false, false}, {"batch_item", true, false}, {"sibling_during_claim", false, true}} {
		t.Run(scenario.name, func(t *testing.T) {
			root := t.TempDir()
			t.Setenv(home.EnvVar, root)
			t.Setenv("CODEAF_PROFILE_DIR", "")
			t.Setenv("CODEAF_SYNC_URL", "off")
			c, err := cell.CreateIn(filepath.Join(session.PlacesRoot(), "phone-test"), cell.Options{Class: cell.HostBound})
			if err != nil {
				t.Fatal(err)
			}
			transcript, err := c.Path(cell.TranscriptPath)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(transcript, nil, 0600); err != nil {
				t.Fatal(err)
			}
			conv := &fakeConversation{}
			conv.onAnswer = func(a session.Answer) {
				q := session.Question{Kind: a.Kind, ID: a.ID}
				conv.raise(session.Event{Kind: session.EventQuestionAnswered, Question: &q, Answer: &a})
			}
			rec := newAppRecorder()
			backend := newAppBackend(rec, func(ask appOpenAsk) (*appConn, error) {
				return &appConn{Conv: conv, Workspace: ask.Workspace, Transcript: transcript, Release: func() {}}, nil
			})
			defer backend.Close()
			backend.mint = func(string, ...func(string) error) (string, error) { return transcript, nil }
			agent, err := mustCreate(t, backend, appwire.CreateAsk{Workspace: t.TempDir()})
			if err != nil {
				t.Fatal(err)
			}
			if agent.ID != c.ID {
				t.Fatalf("app chat id = %q; directory lease uses %q", agent.ID, c.ID)
			}

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
			full := appClientSigner{id, dev}
			dir := directory.NewHTTP(srv.URL, reqsign.SignFor(full, time.Now), srv.Client())
			ctx := context.Background()
			if err := dir.PutDevice(ctx, dev.ID(), directory.Device{V: 1}); err != nil {
				t.Fatal(err)
			}
			if _, err := dir.Create(ctx, c.ID, directory.CellInit{Head: strings.Repeat("1", 64), Class: "chat"}); err != nil {
				t.Fatal(err)
			}
			runner, err := clientrun.Open(clientrun.Config{Home: root, Relay: srv.URL, Signer: full, Directory: dir, Engine: backend, Log: backend.logClientCall, HTTP: srv.Client()})
			if err != nil {
				t.Fatal(err)
			}
			backend.clientRunner.Store(runner)
			seed, err := identity.NewSeed()
			if err != nil {
				t.Fatal(err)
			}
			pub, priv, err := box.GenerateKey(rand.Reader)
			if err != nil {
				t.Fatal(err)
			}
			params, _ := json.Marshal(appwire.PairClientParams{SigningKey: seed.Public(), BoxKey: pub[:], Methods: []string{"asks.answer"}, Reads: []string{"asks.list"}})
			result, err := appwire.Call(backend, "clients.pair", params)
			if err != nil {
				t.Fatal(err)
			}
			grant := result.(*appwire.ClientGrant)
			clientDev, err := seed.Join(grant.Cert)
			if err != nil {
				t.Fatal(err)
			}
			client := scopedSigner{grant.Identity, clientDev}

			_ = priv
			raise := func(i int, stakes session.Stakes) {
				q := session.Question{ID: uint64(i + 1), Kind: session.QuestionConsent, Head: []string{"ls", "rm -rf"}[i], Stakes: stakes, Batch: "step-1", Options: session.AnswerOptions(session.QuestionConsent)}
				conv.raise(session.Event{Kind: session.EventQuestion, Question: &q})
				rec.waitFor(t, 5*time.Second, func(name string, data any) bool {
					e, ok := data.(appwire.AskOpened)
					return name == appwire.EventAskOpened && ok && e.Ask.ID == fmt.Sprintf("consent:%d", i+1)
				})
			}
			raise(0, session.StakesCostly)
			if !scenario.late {
				raise(1, session.StakesIrreversible)
			}
			if scenario.late {
				srv.Client().Transport = appClientTransport(func(req *http.Request) (*http.Response, error) {
					resp, err := http.DefaultTransport.RoundTrip(req)
					if err == nil && req.Method == "POST" && strings.HasSuffix(req.URL.Path, "/claim") {
						raise(1, session.StakesIrreversible)
					}
					return resp, err
				})
			}
			answerParams := map[string]any{"agent": agent.ID, "id": "consent:1", "option": "allow_once"}
			if scenario.batchItem {
				answerParams["batch_item"] = "consent:2"
			}
			rawParams, _ := json.Marshal(answerParams)
			payload, _ := json.Marshal(clientrun.Call{Method: "asks.answer", Params: rawParams})
			sealed, err := box.SealAnonymous(nil, payload, (*[32]byte)(grant.BoxKey), rand.Reader)
			if err != nil {
				t.Fatal(err)
			}
			clientWire(t, srv.URL, client, "POST", "/v1/clients/"+clientDev.ID()+"/queue", map[string]any{"number": 1, "at": time.Now().UnixMilli(), "method": "asks.answer", "target": dev.ID(), "sealed": sealed}, nil)
			err = runner.Step(ctx)
			if err == nil || !strings.Contains(err.Error(), "Face ID") {
				t.Errorf("consent set accepted without Face ID: %v", err)
			}
			conv.mu.Lock()
			answers := len(conv.answers)
			conv.mu.Unlock()
			if answers != 0 {
				t.Errorf("resolved %d consents without Face ID", answers)
			}
			entries, _, err := backend.Entries(agent.ID, "", 200)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, e := range entries {
				if strings.Contains(e.Text, "Face ID") && strings.Contains(e.Text, clientDev.ID()) {
					found = true
				}
			}
			if !found {
				t.Error("chat has no paired-client Face ID refusal")
			}
		})
	}
}

type appClientTransport func(*http.Request) (*http.Response, error)

func (f appClientTransport) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }
