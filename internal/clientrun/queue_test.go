package clientrun

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/nacl/box"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/clientproto"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/directory/directorytest"
	"github.com/Agent-Field/codeaf/internal/filelock"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/reqsign"
)

// scriptedEngine extends the test rig's fake appwire.Engine with scripted
// handlers for all six phone methods (including appwire.Talker for answerGuard
// batch checks) and optional transient/dispatch error hooks.
type scriptedEngine struct {
	*engine
	appwire.Talker

	chatsDir     string
	listAsksErr  error
	listTasksErr error

	createFn      func(appwire.CreateAsk) (appwire.Agent, error)
	sendFn        func(id, text, clientID string) (uint64, error)
	stopFn        func(id string) error
	answerFn      func(agent, id, option, text string) error
	answerWithFn  func(agent, id string, answer appwire.AskAnswer) error
	inboxAnswerFn func(id, option, text, why, scope string) (time.Time, error)
	taskActFn     func(id, action, option, text string) error

	creates          []appwire.CreateAsk
	createdIDs       []string
	sends            []sentMsg
	stops            []string
	answered         []answeredAsk
	beforeResolveLog [][]string
	inboxAnswers     []string
	startedTasks     []string
}

var _ appwire.Talker = (*scriptedEngine)(nil)

type sentMsg struct {
	ID       string
	Text     string
	ClientID string
}

type answeredAsk struct {
	Agent  string
	ID     string
	Option string
	Text   string
}

func newScriptedEngine(base *engine) *scriptedEngine {
	return &scriptedEngine{engine: base}
}

func (e *scriptedEngine) ListAsks(agent string) ([]appwire.Ask, error) {
	if e.listAsksErr != nil {
		return nil, e.listAsksErr
	}
	return e.engine.ListAsks(agent)
}

func (e *scriptedEngine) ListTasks(agent string) ([]appwire.Task, error) {
	if e.listTasksErr != nil {
		return nil, e.listTasksErr
	}
	return e.engine.ListTasks(agent)
}

func (e *scriptedEngine) CreateAgent(ask appwire.CreateAsk) (appwire.Agent, error) {
	e.creates = append(e.creates, ask)
	if e.createFn != nil {
		return e.createFn(ask)
	}
	e.engine.created++
	id := fmt.Sprintf("01JCREATE0000000000000%04d", len(e.createdIDs)+1)
	e.createdIDs = append(e.createdIDs, id)
	a := appwire.Agent{ID: id}
	root := e.chatsDir
	if root == "" {
		root, _ = os.MkdirTemp("", "codeaf-test-chats-*")
		e.chatsDir = root
	}
	folder := filepath.Join(root, id)
	if ask.BeforeMint != nil {
		if err := ask.BeforeMint(filepath.Join(folder, "transcript.jsonl")); err != nil {
			return appwire.Agent{}, err
		}
	}
	if err := os.MkdirAll(folder, 0700); err != nil {
		return appwire.Agent{}, err
	}
	if ask.BeforeStart != nil {
		if err := ask.BeforeStart(a); err != nil {
			return appwire.Agent{}, err
		}
	}
	return a, nil
}

func (e *scriptedEngine) Send(id, text, clientID string) (uint64, error) {
	if e.sendFn != nil {
		return e.sendFn(id, text, clientID)
	}
	e.sends = append(e.sends, sentMsg{ID: id, Text: text, ClientID: clientID})
	return uint64(len(e.sends)), nil
}

func (e *scriptedEngine) Stop(id string) error {
	if e.stopFn != nil {
		return e.stopFn(id)
	}
	e.stops = append(e.stops, id)
	return nil
}

func (e *scriptedEngine) AnswerAsk(agent, id, option, text string) error {
	if e.answerFn != nil {
		return e.answerFn(agent, id, option, text)
	}
	e.engine.answers++
	e.answered = append(e.answered, answeredAsk{Agent: agent, ID: id, Option: option, Text: text})
	return nil
}

func (e *scriptedEngine) AnswerAskWith(agent, id string, answer appwire.AskAnswer) error {
	if e.answerWithFn != nil {
		return e.answerWithFn(agent, id, answer)
	}
	if answer.BeforeResolve != nil {
		asks, err := e.ListAsks(agent)
		if err != nil {
			return err
		}
		resolved := resolveAskBatchForTest(asks, agent, id, answer)
		ids := make([]string, 0, len(resolved))
		for _, a := range resolved {
			ids = append(ids, a.ID)
		}
		e.beforeResolveLog = append(e.beforeResolveLog, ids)
		if err := answer.BeforeResolve(resolved); err != nil {
			return err
		}
	}
	return e.AnswerAsk(agent, id, answer.Option, answer.Text)
}

func resolveAskBatchForTest(asks []appwire.Ask, agent, id string, answer appwire.AskAnswer) []appwire.Ask {
	chatAsks := make([]appwire.Ask, 0, len(asks))
	for _, a := range asks {
		if a.Agent == "" || a.Agent == agent {
			chatAsks = append(chatAsks, a)
		}
	}
	targetID := id
	if answer.BatchItem != "" && !answer.Later && !answer.YouDecide {
		targetID = answer.BatchItem
	}
	idx := slices.IndexFunc(chatAsks, func(a appwire.Ask) bool { return a.ID == targetID })
	if idx < 0 {
		return nil
	}
	selected := chatAsks[idx]
	if answer.BatchItem == "" && !answer.Later && !answer.YouDecide && answer.Option != appwire.OptionAlways && len(selected.Batch) > 1 {
		var batch []appwire.Ask
		for _, item := range selected.Batch {
			if sIdx := slices.IndexFunc(chatAsks, func(a appwire.Ask) bool { return a.ID == item.ID }); sIdx >= 0 {
				batch = append(batch, chatAsks[sIdx])
			}
		}
		if len(batch) > 0 {
			return batch
		}
	}
	return []appwire.Ask{selected}
}

func (e *scriptedEngine) AnswerInbox(id, option, text, why, scope string) (time.Time, error) {
	if e.inboxAnswerFn != nil {
		return e.inboxAnswerFn(id, option, text, why, scope)
	}
	e.engine.inboxAnswers++
	e.inboxAnswers = append(e.inboxAnswers, id)
	return time.Unix(1700000000, 0), nil
}

func (e *scriptedEngine) UndoInbox(string) error        { return nil }
func (e *scriptedEngine) LaterInbox(string, bool) error { return nil }
func (e *scriptedEngine) OpenInbox(string) (string, string, error) {
	return chat, "", nil
}

func (e *scriptedEngine) GetTask(id string) (appwire.TaskPage, error) {
	return appwire.TaskPage{}, nil
}

func (e *scriptedEngine) ActOnTask(id, action, option, text string) error {
	if e.taskActFn != nil {
		return e.taskActFn(id, action, option, text)
	}
	e.engine.taskStarts++
	e.startedTasks = append(e.startedTasks, id+":"+action)
	return nil
}

func (g *rig) useScriptedEngine(t *testing.T) *scriptedEngine {
	t.Helper()
	se := newScriptedEngine(g.e)
	se.chatsDir = t.TempDir()
	g.r.cfg.Engine = se
	return se
}

func (g *rig) executorBoxPub(t *testing.T) [boxKeyBytes]byte {
	t.Helper()
	pub, err := g.r.executorPublicKey()
	if err != nil {
		t.Fatal(err)
	}
	return pub
}

func (g *rig) queueCall(t *testing.T, number int64, method, target string, params any, faceID bool, lifetime time.Duration) clientproto.CallPlain {
	t.Helper()
	return g.queueCallFrom(t, g.client, g.executorBoxPub(t), number, method, target, params, faceID, lifetime)
}

func (g *rig) queueCallFrom(t *testing.T, phone signer, boxPub [boxKeyBytes]byte, number int64, method, target string, params any, faceID bool, lifetime time.Duration) clientproto.CallPlain {
	t.Helper()
	var rawParams json.RawMessage
	switch v := params.(type) {
	case json.RawMessage:
		rawParams = v
	case string:
		rawParams = json.RawMessage(v)
	default:
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		rawParams = b
	}
	at := g.clock.Now().UnixMilli()
	if lifetime == 0 {
		if method == clientproto.MethodCreate {
			lifetime = clientproto.CreateLifetime
		} else {
			lifetime = clientproto.CallLifetime
		}
	}
	plain := clientproto.CallPlain{
		V:        clientproto.CallVersion,
		Client:   phone.dev.ID(),
		Number:   number,
		At:       at,
		NotAfter: at + lifetime.Milliseconds(),
		Target:   target,
		Method:   method,
		Params:   rawParams,
		FaceID:   faceID,
	}
	sealed, err := clientproto.SealCall(plain, &boxPub, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	g.request(t, phone, "POST", "/v1/clients/"+phone.dev.ID()+"/queue", map[string]any{
		"number": number,
		"at":     at,
		"method": method,
		"target": target,
		"sealed": sealed,
	}, nil, 204)
	return plain
}

func (g *rig) readOutcomesSlot(t *testing.T, writer string) []clientproto.Outcome {
	t.Helper()
	p := g.readBoxSlot(t, "outcomes", writer, 0)
	outcomes, err := clientproto.DecodeOutcomes(p.Payload)
	if err != nil {
		t.Fatalf("DecodeOutcomes: %v (payload=%s)", err, p.Payload)
	}
	return outcomes
}

func (g *rig) queueLen(t *testing.T, clientID string) int {
	t.Helper()
	var q struct {
		Entries []json.RawMessage `json:"entries"`
	}
	g.request(t, g.s, "GET", "/v1/clients/"+clientID+"/queue", nil, &q, 200)
	return len(q.Entries)
}

// TestExecutorCardCreatesKeyOnceAndDoesNotFightRunningRunner verifies Step 3:
//  1. Calling ExecutorCard on a home with no state creates the executor key
//     once and a second call returns the exact same BoxPub without overwriting
//     other state fields.
//  2. With a runner open on the same home, ExecutorCard returns that runner's
//     key and writes nothing (runner.json bytes are identical before and after).
//  3. When the runner lock is held and runner.json has no key yet, ExecutorCard
//     answers the plain error ErrNotReady ("this computer is not ready to serve a phone yet").
//  4. The returned card verifies with Card.Verify under the identity key.
//  5. A name that fails clientproto.ValidCardName is rejected with an error.
func TestExecutorCardCreatesKeyOnceAndDoesNotFightRunningRunner(t *testing.T) {
	home := t.TempDir()
	id, err := identity.Ensure(home)
	if err != nil {
		t.Fatal(err)
	}
	devPub, devPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.UnixMilli(1700000123456)
	devCert := identity.IssueCert(id, devPub, now)

	// A name that fails ValidCardName is an error before any state file is created.
	for _, badName := range []string{"", "bad\nname", strings.Repeat("a", clientproto.MaxCardNameRunes+1)} {
		if _, err := ExecutorCard(home, devPriv, devCert, badName, now); err == nil {
			t.Fatalf("ExecutorCard accepted invalid card name %q", badName)
		}
	}
	if _, err := os.Stat(StatePath(home)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed ValidCardName touched runner.json: %v", err)
	}

	// First call on a home with no state creates the key and returns a valid card.
	card1, err := ExecutorCard(home, devPriv, devCert, "desk-1", now)
	if err != nil {
		t.Fatalf("first ExecutorCard: %v", err)
	}
	if err := card1.Verify(id.PublicKey()); err != nil {
		t.Fatalf("Card.Verify failed: %v", err)
	}
	if card1.Writer != devCert.DeviceID() || card1.Name != "desk-1" || card1.At != now.UnixMilli() {
		t.Fatalf("card1 = %+v", card1)
	}

	// Second call on the same home returns the exact same BoxPub.
	card2, err := ExecutorCard(home, devPriv, devCert, "desk-2", now.Add(time.Second))
	if err != nil {
		t.Fatalf("second ExecutorCard: %v", err)
	}
	if err := card2.Verify(id.PublicKey()); err != nil {
		t.Fatalf("card2.Verify failed: %v", err)
	}
	if !bytes.Equal(card1.BoxPub, card2.BoxPub) {
		t.Fatalf("BoxPub changed between calls: %x != %x", card1.BoxPub, card2.BoxPub)
	}

	// With a live runner open on g.state, ExecutorCard returns that runner's key
	// and writes nothing to runner.json (compare file bytes before and after).
	g := newRig(t)
	beforeBytes, err := os.ReadFile(StatePath(g.state))
	if err != nil {
		t.Fatal(err)
	}
	wantPub := g.executorBoxPub(t)
	runnerDevCert := identity.IssueCert(g.id, devPub, g.clock.Now())
	runnerCard, err := ExecutorCard(g.state, devPriv, runnerDevCert, "desk", g.clock.Now())
	if err != nil {
		t.Fatalf("ExecutorCard with open runner: %v", err)
	}
	afterBytes, err := os.ReadFile(StatePath(g.state))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(beforeBytes, afterBytes) {
		t.Fatalf("ExecutorCard wrote to runner.json while runner held lock:\nbefore=%s\nafter=%s", beforeBytes, afterBytes)
	}
	if !bytes.Equal(runnerCard.BoxPub, wantPub[:]) {
		t.Fatalf("ExecutorCard BoxPub = %x, want runner key %x", runnerCard.BoxPub, wantPub)
	}
	if err := runnerCard.Verify(g.id.PublicKey()); err != nil {
		t.Fatalf("runnerCard.Verify: %v", err)
	}

	// When the lock is held by a live holder and runner.json has no key yet,
	// ExecutorCard returns the plain sentence ErrNotReady.
	lockedHome := t.TempDir()
	if err := os.MkdirAll(filepath.Dir(StatePath(lockedHome)), 0700); err != nil {
		t.Fatal(err)
	}
	lockFile, err := os.OpenFile(StatePath(lockedHome)+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer lockFile.Close()
	if err := filelock.Lock(lockFile, true, true); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = filelock.Unlock(lockFile) }()
	_, err = ExecutorCard(lockedHome, devPriv, devCert, "desk", now)
	if err == nil || err.Error() != "this computer is not ready to serve a phone yet" {
		t.Fatalf("ExecutorCard on locked uninitialized home = %v, want %q", err, ErrNotReady)
	}
}

// TestS1TargetedCreateRunsOnlyOnTargetedComputer verifies that an agents.create
// call targeted at computer A makes exactly one chat on A, and the same queue
// seen by computer B (both via the real relay and via a lying relay that lists
// A's entry to B) makes none on B (contract S1).
func TestS1TargetedCreateRunsOnlyOnTargetedComputer(t *testing.T) {
	g := newRig(t)
	seA := g.useScriptedEngine(t)
	g.enroll(t, []string{"agents.list"}, clientproto.MethodCreate)

	// Set up computer B on the same identity.
	homeB := t.TempDir()
	if _, err := identity.Adopt(homeB, g.id, false); err != nil {
		t.Fatal(err)
	}
	devB, err := identity.Device(homeB)
	if err != nil {
		t.Fatal(err)
	}
	signerB := signer{g.id, devB}
	dirB := directory.NewHTTP(g.url, reqsign.SignFor(signerB, g.clock.Now), g.srv.Client())
	if err := dirB.PutDevice(context.Background(), devB.ID(), directory.Device{V: 1}); err != nil {
		t.Fatal(err)
	}
	seB := newScriptedEngine(&engine{})
	runnerB, err := Open(Config{
		Home:      t.TempDir(),
		Relay:     g.url,
		Name:      "laptop-b",
		Identity:  g.id.PublicKey(),
		Signer:    signerB,
		Directory: dirB,
		Engine:    seB,
		Log:       noopLog,
		Now:       g.clock.Now,
		HTTP:      g.srv.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runnerB.Close() })

	// Queue a create targeted at computer A.
	g.queueCall(t, 1, clientproto.MethodCreate, g.s.dev.ID(), map[string]string{
		"client_id": "c0ffee00-0000-4000-8000-000000000001",
	}, false, clientproto.CreateLifetime)

	// Capture the raw queue listing for A so we can also show it directly to B.
	var rawQueue struct {
		Entries []json.RawMessage `json:"entries"`
	}
	g.request(t, g.s, "GET", "/v1/clients/"+g.client.dev.ID()+"/queue", nil, &rawQueue, 200)
	if len(rawQueue.Entries) != 1 {
		t.Fatalf("expected 1 queued entry for A, got %d", len(rawQueue.Entries))
	}

	// Computer B steps against the real relay first: makes 0 chats.
	if err := runnerB.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(seB.creates) != 0 {
		t.Fatalf("computer B created %d chats from A's targeted create", len(seB.creates))
	}

	// Even if a lying relay serves A's exact queue entry to B BEFORE A runs it,
	// B skips it (no outcome, no receipt, no watermark, no acknowledgement) and
	// A's own run of that entry is unaffected.
	_ = runnerB.Close()
	runnerB2, err := Open(Config{
		Home:      t.TempDir(),
		Relay:     g.url,
		Name:      "laptop-b",
		Identity:  g.id.PublicKey(),
		Signer:    signerB,
		Directory: dirB,
		Engine:    seB,
		Log:       noopLog,
		Now:       g.clock.Now,
		HTTP: &http.Client{Transport: transport(func(req *http.Request) (*http.Response, error) {
			if req.Method == "GET" && strings.HasSuffix(req.URL.Path, "/queue") {
				body, _ := json.Marshal(rawQueue)
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(body))}, nil
			}
			return http.DefaultTransport.RoundTrip(req)
		})},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runnerB2.Close() })
	if err := runnerB2.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(seB.creates) != 0 {
		t.Fatalf("computer B created chat when shown A's entry: %d", len(seB.creates))
	}
	outcomesB := g.readOutcomesSlot(t, devB.ID())
	if len(outcomesB) != 0 {
		t.Fatalf("computer B published outcome for A's entry: %+v, want []", outcomesB)
	}
	if len(runnerB2.Receipts()) != 0 {
		t.Fatalf("computer B recorded receipt for A's entry: %+v", runnerB2.Receipts())
	}
	if g.queueLen(t, g.client.dev.ID()) != 1 {
		t.Fatalf("computer B acknowledged A's entry; queueLen = %d, want 1", g.queueLen(t, g.client.dev.ID()))
	}

	// Computer A steps: creates exactly 1 chat here, does not pass client_id to
	// the engine, and publishes outcome done with the created chat ID.
	if err := g.r.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(seA.creates) != 1 || len(seA.createdIDs) != 1 {
		t.Fatalf("computer A creates = %d, want 1", len(seA.creates))
	}
	if seA.creates[0].ClientID != "" {
		t.Fatalf("runner leaked phone client_id %q to engine CreateAgent", seA.creates[0].ClientID)
	}
	createdID := seA.createdIDs[0]
	outcomesA := g.readOutcomesSlot(t, g.s.dev.ID())
	if len(outcomesA) != 1 || outcomesA[0] != (clientproto.Outcome{Number: 1, Status: clientproto.OutcomeDone, Agent: createdID}) {
		t.Fatalf("outcomes A = %+v", outcomesA)
	}
}

// TestS2CreateIdempotencyRedeliveryLostClaimCrashAndDuplicateClientID verifies
// that:
//  1. Redelivery after a failed DELETE makes at most one chat and does not
//     duplicate the outcome.
//  2. A lost claim reply recovers by repeating the claim with the same token
//     from the saved claiming receipt and makes at most one chat.
//  3. A crash after saving the claiming receipt makes at most one chat.
//  4. A crash after saving the started receipt (both after BeforeStart and
//     immediately after started) never dispatches CreateAgent again and makes
//     at most one chat.
//  5. A second create with the same client_id under a new queue number finishes
//     done with the original chat and creates nothing, bounded to 256 entries
//     per phone and cleared on revoke (contract S2).
func TestS2CreateIdempotencyRedeliveryLostClaimCrashAndDuplicateClientID(t *testing.T) {
	t.Run("redelivery_and_duplicate_client_id", func(t *testing.T) {
		g := newRig(t)
		se := g.useScriptedEngine(t)
		g.enroll(t, []string{"agents.list"}, clientproto.MethodCreate)

		const cid = "c0ffee00-0000-4000-8000-000000000001"
		g.queueCall(t, 1, clientproto.MethodCreate, g.s.dev.ID(), map[string]string{"client_id": cid}, false, clientproto.CreateLifetime)

		// Fail DELETE on round 1 so entry #1 is redelivered on round 2.
		failDelete := true
		g.reopen(t, transport(func(req *http.Request) (*http.Response, error) {
			if failDelete && req.Method == "DELETE" && strings.Contains(req.URL.Path, "/queue/") {
				return nil, errors.New("delete reply lost")
			}
			return http.DefaultTransport.RoundTrip(req)
		}), noopLog)
		se = g.useScriptedEngine(t)

		if err := g.r.Step(context.Background()); err == nil {
			t.Fatal("expected DELETE failure on round 1")
		}
		if len(se.creates) != 1 {
			t.Fatalf("creates after round 1 = %d, want 1", len(se.creates))
		}
		firstChat := se.createdIDs[0]

		// Round 2 re-receives entry #1, acknowledges it without re-running or
		// duplicating the outcome.
		failDelete = false
		if err := g.r.Step(context.Background()); err != nil {
			t.Fatal(err)
		}
		if len(se.creates) != 1 || g.queueLen(t, g.client.dev.ID()) != 0 {
			t.Fatalf("after redelivery: creates=%d queueLen=%d", len(se.creates), g.queueLen(t, g.client.dev.ID()))
		}
		outcomes := g.readOutcomesSlot(t, g.s.dev.ID())
		if len(outcomes) != 1 || outcomes[0] != (clientproto.Outcome{Number: 1, Status: clientproto.OutcomeDone, Agent: firstChat}) {
			t.Fatalf("outcomes after redelivery = %+v", outcomes)
		}

		// Queue entry #2 with the SAME client_id under a new queue number: must
		// finish done with firstChat and create nothing, even across a restart.
		g.reopen(t, nil, noopLog)
		se = g.useScriptedEngine(t)
		g.queueCall(t, 2, clientproto.MethodCreate, g.s.dev.ID(), map[string]string{"client_id": cid}, false, clientproto.CreateLifetime)
		if err := g.r.Step(context.Background()); err != nil {
			t.Fatal(err)
		}
		if len(se.creates) != 0 {
			t.Fatalf("duplicate client_id created %d new chats, want 0", len(se.creates))
		}
		outcomes = g.readOutcomesSlot(t, g.s.dev.ID())
		want := []clientproto.Outcome{
			{Number: 1, Status: clientproto.OutcomeDone, Agent: firstChat},
			{Number: 2, Status: clientproto.OutcomeDone, Agent: firstChat},
		}
		if !slices.Equal(outcomes, want) {
			t.Fatalf("outcomes after duplicate client_id = %+v, want %+v", outcomes, want)
		}
	})

	t.Run("lost_claim_reply_and_crash_after_claiming", func(t *testing.T) {
		g := newRig(t)
		g.enroll(t, []string{"agents.list"}, clientproto.MethodCreate)
		g.queueCall(t, 1, clientproto.MethodCreate, g.s.dev.ID(), map[string]string{"client_id": "cid-claim"}, false, clientproto.CreateLifetime)

		// Round 1 lets POST .../claim execute on the relay, snapshots runner.json
		// (which holds the claiming receipt), and drops the HTTP response.
		var claimTokens []string
		dropFirstClaim := true
		g.reopen(t, transport(func(req *http.Request) (*http.Response, error) {
			if req.Method == "POST" && strings.HasSuffix(req.URL.Path, "/claim") {
				body, _ := io.ReadAll(req.Body)
				req.Body = io.NopCloser(bytes.NewReader(body))
				var payload struct {
					Token string `json:"token"`
				}
				_ = json.Unmarshal(body, &payload)
				claimTokens = append(claimTokens, payload.Token)
				resp, err := http.DefaultTransport.RoundTrip(req)
				if dropFirstClaim {
					dropFirstClaim = false
					if resp != nil {
						resp.Body.Close()
					}
					return nil, errors.New("lost claim reply after relay committed claim")
				}
				return resp, err
			}
			return http.DefaultTransport.RoundTrip(req)
		}), noopLog)
		se := g.useScriptedEngine(t)

		if err := g.r.Step(context.Background()); err == nil {
			t.Fatal("expected lost claim reply error")
		}
		if len(se.creates) != 0 {
			t.Fatalf("created chat before claim succeeded: %d", len(se.creates))
		}

		// Simulate a crash after claiming by reopening from disk; the next round
		// must repeat the claim with the exact same 32-byte hex token and create 1 chat.
		g.reopen(t, transport(func(req *http.Request) (*http.Response, error) {
			if req.Method == "POST" && strings.HasSuffix(req.URL.Path, "/claim") {
				body, _ := io.ReadAll(req.Body)
				req.Body = io.NopCloser(bytes.NewReader(body))
				var payload struct {
					Token string `json:"token"`
				}
				_ = json.Unmarshal(body, &payload)
				claimTokens = append(claimTokens, payload.Token)
			}
			return http.DefaultTransport.RoundTrip(req)
		}), noopLog)
		se = g.useScriptedEngine(t)
		if err := g.r.Step(context.Background()); err != nil {
			t.Fatalf("recovery after lost claim reply failed: %v", err)
		}
		if len(claimTokens) != 2 || len(claimTokens[0]) != clientproto.ClaimTokenBytes*2 || claimTokens[0] != claimTokens[1] {
			t.Fatalf("claim tokens = %v; want two identical 64-hex-char tokens", claimTokens)
		}
		if len(se.creates) != 1 {
			t.Fatalf("creates after claim recovery = %d, want 1", len(se.creates))
		}
	})

	t.Run("crash_after_started", func(t *testing.T) {
		g := newRig(t)
		g.enroll(t, []string{"agents.list"}, clientproto.MethodCreate)
		g.queueCall(t, 1, clientproto.MethodCreate, g.s.dev.ID(), map[string]string{"client_id": "cid-started"}, false, clientproto.CreateLifetime)

		// Round 1 runs the engine's CreateAgent once (creating the chat folder and
		// saving the started receipt + create mapping via BeforeMint/BeforeStart),
		// snapshots the disk state while the receipt is at status "started", and
		// simulates a process crash before DELETE acknowledges the queue entry.
		snapHome := t.TempDir()
		g.reopen(t, transport(func(req *http.Request) (*http.Response, error) {
			if req.Method == "DELETE" && strings.Contains(req.URL.Path, "/queue/") {
				return nil, errors.New("crash before delete")
			}
			return http.DefaultTransport.RoundTrip(req)
		}), noopLog)
		se := g.useScriptedEngine(t)
		chatsDir := se.chatsDir
		totalCreates := 0
		const createdChatID = "01JCREATEDAFTERSTARTED0001"
		se.createFn = func(ask appwire.CreateAsk) (appwire.Agent, error) {
			totalCreates++
			folder := filepath.Join(chatsDir, createdChatID)
			if err := ask.BeforeMint(filepath.Join(folder, "transcript.jsonl")); err != nil {
				return appwire.Agent{}, err
			}
			if err := os.MkdirAll(folder, 0700); err != nil {
				return appwire.Agent{}, err
			}
			a := appwire.Agent{ID: createdChatID}
			if err := ask.BeforeStart(a); err != nil {
				return appwire.Agent{}, err
			}
			for _, fn := range []func(string) string{StatePath, PinsPath, TombstonesPath} {
				if raw, err := os.ReadFile(fn(g.state)); err == nil {
					dst := fn(snapHome)
					_ = os.MkdirAll(filepath.Dir(dst), 0700)
					_ = os.WriteFile(dst, raw, 0600)
				}
			}
			return a, nil
		}
		if err := g.r.Step(context.Background()); err == nil {
			t.Fatal("expected crash before delete in round 1")
		}
		if totalCreates != 1 {
			t.Fatalf("engine CreateAgent calls in round 1 = %d, want 1", totalCreates)
		}

		// Reopen from snapHome (where the receipt on disk is at status "started"
		// and the created chat folder exists) and run recovery Step.
		_ = g.r.Close()
		g.state = snapHome
		g.reopen(t, nil, noopLog)
		se2 := g.useScriptedEngine(t)
		se2.chatsDir = chatsDir
		se2.createFn = func(ask appwire.CreateAsk) (appwire.Agent, error) {
			totalCreates++
			return appwire.Agent{ID: "unexpected-second-create"}, nil
		}
		if recs := g.r.Receipts(); len(recs) != 1 || recs[0].Status != "started" {
			t.Fatalf("receipts on disk before recovery = %+v, want started", recs)
		}
		if err := g.r.Step(context.Background()); err != nil {
			t.Fatalf("recovery Step after crash_after_started: %v", err)
		}
		if totalCreates != 1 {
			t.Fatalf("engine saw %d CreateAgent calls across crash and recovery, want 1", totalCreates)
		}
		if g.queueLen(t, g.client.dev.ID()) != 0 {
			t.Fatal("recovered started receipt was not acknowledged")
		}
		outcomes := g.readOutcomesSlot(t, g.s.dev.ID())
		want := []clientproto.Outcome{{Number: 1, Status: clientproto.OutcomeDone, Agent: createdChatID}}
		if !slices.Equal(outcomes, want) {
			t.Fatalf("outcomes after started recovery = %+v, want %+v", outcomes, want)
		}
	})

	t.Run("create_map_bounded_to_256_and_cleared_on_revoke", func(t *testing.T) {
		g := newRig(t)
		g.useScriptedEngine(t)
		g.enroll(t, []string{"agents.list"}, clientproto.MethodCreate)
		if err := g.r.Step(context.Background()); err != nil {
			t.Fatal(err)
		}
		g.r.mu.Lock()
		for i := 1; i <= clientproto.MaxClientCreates+10; i++ {
			g.r.recordCreatedChatLocked(g.client.dev.ID(), fmt.Sprintf("cid-%d", i), fmt.Sprintf("chat-%d", i))
		}
		gotLen := len(g.r.state.Creates[g.client.dev.ID()])
		g.r.mu.Unlock()
		if gotLen != clientproto.MaxClientCreates {
			t.Fatalf("creates map len = %d, want %d", gotLen, clientproto.MaxClientCreates)
		}
		if _, ok := g.r.createdChat(g.client.dev.ID(), "cid-10"); ok {
			t.Fatal("oldest create entry past 256 was not evicted")
		}
		if agent, ok := g.r.createdChat(g.client.dev.ID(), fmt.Sprintf("cid-%d", clientproto.MaxClientCreates+10)); !ok || agent == "" {
			t.Fatal("newest create entry was not kept")
		}
		if err := g.r.Unpair(context.Background(), g.client.dev.ID()); err != nil {
			t.Fatal(err)
		}
		if _, ok := g.r.createdChat(g.client.dev.ID(), fmt.Sprintf("cid-%d", clientproto.MaxClientCreates+10)); ok {
			t.Fatal("revoke did not delete phone's create map entries")
		}
	})
}

// TestS3WatermarkRejectsReplaysGapsAndRePairedPhone verifies that:
//  1. Gaps in queue numbers do not stop a later number (watermark, not
//     contiguity), and the old skip_through field is gone.
//  2. Any number at or below the watermark is acknowledged as a replay, runs
//     nothing, and publishes nothing new.
//  3. After a phone is removed and re-paired, captured calls from the old
//     phone cannot run again (contract S3).
func TestS3WatermarkRejectsReplaysGapsAndRePairedPhone(t *testing.T) {
	g := newRig(t)
	se := g.useScriptedEngine(t)
	g.enroll(t, []string{"asks.list"}, clientproto.MethodCreate, clientproto.MethodSend, clientproto.MethodStop)
	if err := g.r.Step(context.Background()); err != nil {
		t.Fatal(err)
	}

	// Queue entries 1, 2, 3 at the relay, but serve only entries 1 and 3 to this
	// computer (simulating entry 2 having been targeted at another computer).
	g.queueCall(t, 1, clientproto.MethodSend, g.s.dev.ID(), map[string]string{
		"id": chat, "text": "msg-1", "client_id": "c1",
	}, false, 0)
	g.queueCall(t, 2, clientproto.MethodSend, g.s.dev.ID(), map[string]string{
		"id": chat, "text": "msg-2-other-computer", "client_id": "c2",
	}, false, 0)
	g.queueCall(t, 3, clientproto.MethodSend, g.s.dev.ID(), map[string]string{
		"id": chat, "text": "msg-3-after-gap", "client_id": "c3",
	}, false, 0)

	var fullQueue struct {
		Entries []json.RawMessage `json:"entries"`
	}
	g.request(t, g.s, "GET", "/v1/clients/"+g.client.dev.ID()+"/queue", nil, &fullQueue, 200)
	if len(fullQueue.Entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(fullQueue.Entries))
	}

	// Serve [entry 1, entry 3] (a gap at number 2!). Both must run without any skip_through.
	var servedEntries []json.RawMessage
	servedEntries = append(servedEntries, fullQueue.Entries[0], fullQueue.Entries[2])
	g.reopen(t, transport(func(req *http.Request) (*http.Response, error) {
		if req.Method == "GET" && strings.HasSuffix(req.URL.Path, "/queue") {
			body, _ := json.Marshal(map[string]any{"entries": servedEntries})
			return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(body))}, nil
		}
		return http.DefaultTransport.RoundTrip(req)
	}), noopLog)
	se = g.useScriptedEngine(t)

	if err := g.r.Step(context.Background()); err != nil {
		t.Fatalf("gap in queue numbers failed: %v", err)
	}
	if len(se.sends) != 2 || se.sends[0].Text != "msg-1" || se.sends[1].Text != "msg-3-after-gap" {
		t.Fatalf("sends across gap = %+v, want [msg-1, msg-3-after-gap]", se.sends)
	}

	// Now replay entry 1 and entry 2 (both <= watermark 3) and entry 3 (== watermark 3).
	// None may run, and no new outcome may be recorded or published.
	servedEntries = []json.RawMessage{fullQueue.Entries[1], fullQueue.Entries[2]}
	outcomesBefore := g.readOutcomesSlot(t, g.s.dev.ID())
	var outcomePuts int
	g.reopen(t, transport(func(req *http.Request) (*http.Response, error) {
		if req.Method == "GET" && strings.HasSuffix(req.URL.Path, "/queue") {
			body, _ := json.Marshal(map[string]any{"entries": servedEntries})
			return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(body))}, nil
		}
		if req.Method == "PUT" && strings.Contains(req.URL.Path, "/slots/outcomes.") {
			outcomePuts++
		}
		return http.DefaultTransport.RoundTrip(req)
	}), noopLog)
	se = g.useScriptedEngine(t)
	// First round after reopen seeds in-memory hashes; then test replay in round 2.
	servedEntries = []json.RawMessage{}
	if err := g.r.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	outcomePuts = 0
	servedEntries = []json.RawMessage{fullQueue.Entries[1], fullQueue.Entries[2]}
	if err := g.r.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(se.sends) != 0 {
		t.Fatalf("entries at or below watermark ran again: %+v", se.sends)
	}
	if outcomePuts != 0 {
		t.Fatalf("replay at or below watermark published %d outcome slot PUTs, want 0", outcomePuts)
	}
	outcomesAfter := g.readOutcomesSlot(t, g.s.dev.ID())
	if !slices.Equal(outcomesBefore, outcomesAfter) {
		t.Fatalf("outcomes changed on replay: %+v -> %+v", outcomesBefore, outcomesAfter)
	}

	// Verify that Receipt no longer has SkippedFrom or SkippedThrough fields, and
	// that a call carrying skip_through in its sealed plaintext is rejected as bad_call.
	rt := reflect.TypeFor[Receipt]()
	if _, ok := rt.FieldByName("SkippedFrom"); ok {
		t.Fatal("Receipt still has SkippedFrom field")
	}
	if _, ok := rt.FieldByName("SkippedThrough"); ok {
		t.Fatal("Receipt still has SkippedThrough field")
	}

	// Verify that entry 2 (same phone key, number 2 <= watermark 3, never run)
	// has a 100% valid proof so ONLY the watermark can reject it.
	var entry2 Queued
	if err := json.Unmarshal(fullQueue.Entries[1], &entry2); err != nil {
		t.Fatal(err)
	}
	clients := g.r.Clients()
	if len(clients) != 1 {
		t.Fatalf("Clients() = %+v, want 1 enrolled phone", clients)
	}
	if err := g.r.verify(clients[0], entry2); err != nil {
		t.Fatalf("entry 2 failed proof verification (must be valid proof so only watermark rejects): %v", err)
	}

	// Also mint a brand-new call signed by the SAME phone key at number 2 (<= watermark 3),
	// verify that its proof checks out, and verify that the watermark rejects it even after
	// a relay store reset + AcceptOlderList re-enrollment of the same phone key.
	freshAt := g.clock.Now().UnixMilli()
	execPub := g.executorBoxPub(t)
	freshPlain2 := clientproto.CallPlain{
		V:        clientproto.CallVersion,
		Client:   g.client.dev.ID(),
		Number:   2,
		At:       freshAt,
		NotAfter: freshAt + clientproto.CallLifetime.Milliseconds(),
		Target:   g.s.dev.ID(),
		Method:   clientproto.MethodSend,
		Params:   json.RawMessage(fmt.Sprintf(`{"id":%q,"text":"fresh-at-num-2","client_id":"c-fresh-2"}`, chat)),
	}
	freshSealed2, err := clientproto.SealCall(freshPlain2, &execPub, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	freshBody2, err := json.Marshal(map[string]any{
		"number": 2,
		"at":     freshAt,
		"method": clientproto.MethodSend,
		"target": g.s.dev.ID(),
		"sealed": freshSealed2,
	})
	if err != nil {
		t.Fatal(err)
	}
	req2, err := http.NewRequest("POST", g.url+"/v1/clients/"+g.client.dev.ID()+"/queue", bytes.NewReader(freshBody2))
	if err != nil {
		t.Fatal(err)
	}
	reqsign.Sign(req2, freshBody2, g.client, g.clock.Now())
	freshQueued2 := Queued{
		Number:      2,
		At:          freshAt,
		Method:      clientproto.MethodSend,
		Target:      g.s.dev.ID(),
		Sealed:      freshSealed2,
		Signature:   req2.Header.Get(reqsign.HeaderSig),
		Cert:        req2.Header.Get(reqsign.HeaderCert),
		Identity:    req2.Header.Get(reqsign.HeaderIdentity),
		RequestTime: req2.Header.Get(reqsign.HeaderTime),
		RequestURI:  req2.URL.RequestURI(),
		Body:        freshBody2,
	}
	if err := g.r.verify(clients[0], freshQueued2); err != nil {
		t.Fatalf("freshQueued2 proof did not verify: %v", err)
	}
	freshQueued2JSON, _ := json.Marshal(freshQueued2)
	servedEntries = []json.RawMessage{freshQueued2JSON}
	if err := g.r.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(se.sends) != 0 {
		t.Fatalf("freshly signed call at number <= watermark ran: %+v", se.sends)
	}

	// Remove (unpair) the phone and pair a new phone; replaying the old phone's
	// entry does not run under either the old tombstoned phone or the new phone.
	g.reopen(t, nil, noopLog)
	se = g.useScriptedEngine(t)
	if err := g.r.Unpair(context.Background(), g.client.dev.ID()); err != nil {
		t.Fatal(err)
	}
	seed2, err := identity.NewSeed()
	if err != nil {
		t.Fatal(err)
	}
	dev2, err := seed2.Join(identity.IssueCert(g.id, seed2.Public(), g.clock.Now()))
	if err != nil {
		t.Fatal(err)
	}
	pub2, err := hex.DecodeString(dev2.Cert.Device)
	if err != nil {
		t.Fatal(err)
	}
	boxPub2, _, err := box.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	e2 := signEnrollment(g.id, clientproto.Enrollment{
		V:       1,
		Device:  dev2.ID(),
		SignPub: pub2,
		BoxPub:  boxPub2[:],
		Methods: []string{clientproto.MethodSend},
		Reads:   []string{"asks.list"},
		Ver:     1,
	})
	g.request(t, g.s, "PUT", "/v1/clients/"+dev2.ID()+"/enroll", e2, nil, 204)
	g.request(t, g.s, "PUT", "/v1/clients/"+dev2.ID(), map[string]any{
		"cert":    dev2.Cert,
		"methods": []string{clientproto.MethodSend},
		"name":    "",
	}, nil, 204)

	// Replay the old phone's entry in the new phone's queue listing.
	g.reopen(t, transport(func(req *http.Request) (*http.Response, error) {
		if req.Method == "GET" && req.URL.Path == "/v1/clients/"+dev2.ID()+"/queue" {
			body, _ := json.Marshal(map[string]any{"entries": []json.RawMessage{fullQueue.Entries[1]}})
			return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(body))}, nil
		}
		return http.DefaultTransport.RoundTrip(req)
	}), noopLog)
	se = g.useScriptedEngine(t)
	if err := g.r.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(se.sends) != 0 {
		t.Fatalf("old phone call ran after remove and re-pair: %+v", se.sends)
	}
}

// TestS4QueuedCallPastNotAfterNeverRuns verifies that a queued call whose not_after
// has passed according to Config.Now never runs even when the relay still
// serves it, is acknowledged, and publishes expired / expired (contract S4).
func TestS4QueuedCallPastNotAfterNeverRuns(t *testing.T) {
	g := newRig(t)
	se := g.useScriptedEngine(t)
	g.enroll(t, []string{"asks.list"}, clientproto.MethodSend)
	if err := g.r.Step(context.Background()); err != nil {
		t.Fatal(err)
	}

	g.queueCall(t, 1, clientproto.MethodSend, g.s.dev.ID(), map[string]string{
		"id": chat, "text": "too-late", "client_id": "c-late",
	}, false, 10*time.Minute)

	// Advance Config.Now past the call's 10-minute not_after (while within the
	// relay's 24h retention so the relay still serves it).
	g.clock.Advance(11 * time.Minute)
	if err := g.r.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(se.sends) != 0 {
		t.Fatalf("expired call ran: %+v", se.sends)
	}
	if g.queueLen(t, g.client.dev.ID()) != 0 {
		t.Fatal("expired call was not acknowledged")
	}
	outcomes := g.readOutcomesSlot(t, g.s.dev.ID())
	want := []clientproto.Outcome{{Number: 1, Status: clientproto.OutcomeExpired, Reason: clientproto.ReasonExpired}}
	if !slices.Equal(outcomes, want) {
		t.Fatalf("outcomes = %+v, want %+v", outcomes, want)
	}
}

// TestS5QueuedCreateWhileOfflineRunsInsideHourAndExpiresAfter verifies that a
// create queued while the computer is down runs once when the computer returns
// inside clientproto.CreateLifetime (1 hour), and expires without creating a
// chat when the computer returns after the hour (contract S5).
func TestS5QueuedCreateWhileOfflineRunsInsideHourAndExpiresAfter(t *testing.T) {
	g := newRig(t)
	se := g.useScriptedEngine(t)
	g.enroll(t, []string{"agents.list"}, clientproto.MethodCreate)
	if err := g.r.Step(context.Background()); err != nil {
		t.Fatal(err)
	}

	// Computer is offline; phone queues create #1 at t = 0.
	g.queueCall(t, 1, clientproto.MethodCreate, g.s.dev.ID(), map[string]string{"client_id": "offline-30m"}, false, clientproto.CreateLifetime)
	// Computer returns after 30 minutes (inside the 1-hour CreateLifetime).
	g.clock.Advance(30 * time.Minute)
	if err := g.r.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(se.creates) != 1 {
		t.Fatalf("create inside the hour ran %d times, want 1", len(se.creates))
	}

	// Computer goes offline again; phone queues create #2.
	g.queueCall(t, 2, clientproto.MethodCreate, g.s.dev.ID(), map[string]string{"client_id": "offline-61m"}, false, clientproto.CreateLifetime)
	// Computer returns after 1 hour + 1 millisecond (past CreateLifetime).
	g.clock.Advance(clientproto.CreateLifetime + time.Millisecond)
	if err := g.r.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(se.creates) != 1 {
		t.Fatalf("create past the hour ran: total creates = %d, want 1", len(se.creates))
	}
	if g.queueLen(t, g.client.dev.ID()) != 0 {
		t.Fatal("expired create was not acknowledged")
	}
	outcomes := g.readOutcomesSlot(t, g.s.dev.ID())
	want := []clientproto.Outcome{
		{Number: 1, Status: clientproto.OutcomeDone, Agent: se.createdIDs[0]},
		{Number: 2, Status: clientproto.OutcomeExpired, Reason: clientproto.ReasonExpired},
	}
	if !slices.Equal(outcomes, want) {
		t.Fatalf("outcomes = %+v, want %+v", outcomes, want)
	}
}

// TestS6DeterministicRefusalsAreAckedRecordedPublishedAndNextEntryRuns verifies
// that each deterministic refusal (not_holder after a successful directory
// read, method_denied, needs_face_id, bad_target, bad_call from a bad seal or
// bad params, expired, and failed from a dispatched call error) is
// acknowledged, recorded, published in outcomes.<self>, and the NEXT entry from
// that phone still runs in the same drain round (contract S6).
func TestS6DeterministicRefusalsAreAckedRecordedPublishedAndNextEntryRuns(t *testing.T) {
	g := newRig(t)
	se := g.useScriptedEngine(t)
	g.e.asks = []appwire.Ask{{ID: "irreversible-ask", Agent: chat, Irreversible: true}}

	// Enroll the phone with create, send, answer, stop, inbox.answer, tasks.start
	// at the relay directory, while pinning an enrollment that omits agents.stop
	// so we can queue agents.stop at the relay and trigger method_denied at the runner.
	allMethods := clientproto.PhoneMethods()
	withoutStop := slices.DeleteFunc(slices.Clone(allMethods), func(m string) bool { return m == clientproto.MethodStop })
	pubBytes, err := hex.DecodeString(g.client.dev.Cert.Device)
	if err != nil {
		t.Fatal(err)
	}
	enroll := signEnrollment(g.id, clientproto.Enrollment{
		V:       1,
		Device:  g.client.dev.ID(),
		SignPub: pubBytes,
		BoxPub:  g.pub[:],
		Methods: withoutStop,
		Reads:   []string{"asks.list"},
		Ver:     1,
	})
	g.request(t, g.s, "PUT", "/v1/clients/"+g.client.dev.ID()+"/enroll", enroll, nil, 204)
	g.request(t, g.s, "PUT", "/v1/clients/"+g.client.dev.ID(), map[string]any{
		"cert":    g.client.Cert(),
		"methods": allMethods,
		"name":    "",
	}, nil, 204)
	if err := g.r.Step(context.Background()); err != nil {
		t.Fatal(err)
	}

	// 1. not_holder: send to unleased otherChat.
	g.queueCall(t, 1, clientproto.MethodSend, g.s.dev.ID(), map[string]string{
		"id": otherChat, "text": "no-lease", "client_id": "c1",
	}, false, 0)

	// 2. method_denied: agents.stop is not in the phone's signed Enrollment.
	g.queueCall(t, 2, clientproto.MethodStop, g.s.dev.ID(), map[string]string{
		"id": chat,
	}, false, 0)

	// 3. needs_face_id: irreversible ask without FaceID.
	g.queueCall(t, 3, clientproto.MethodAnswer, g.s.dev.ID(), map[string]string{
		"agent": chat, "id": "irreversible-ask", "option": "allow",
	}, false, 0)

	// 4. bad_target: target is "any" instead of this computer's device id.
	g.queueCall(t, 4, clientproto.MethodCreate, "any", map[string]string{
		"client_id": "c4",
	}, false, clientproto.CreateLifetime)

	// 5. bad_call (from a bad seal: sealed to a random key instead of this computer's executor key).
	wrongPub, _, err := box.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	g.queueCallFrom(t, g.client, *wrongPub, 5, clientproto.MethodCreate, g.s.dev.ID(), map[string]string{
		"client_id": "c5",
	}, false, clientproto.CreateLifetime)

	// 6. bad_call (from unknown parameter key in agents.create).
	g.queueCall(t, 6, clientproto.MethodCreate, g.s.dev.ID(), map[string]string{
		"client_id": "c6", "workspace": "/not-allowed",
	}, false, clientproto.CreateLifetime)

	// 7. expired: lifetime of 1 second, then advance clock by 2 seconds before queueing #8.
	g.queueCall(t, 7, clientproto.MethodSend, g.s.dev.ID(), map[string]string{
		"id": chat, "text": "expires-fast", "client_id": "c7",
	}, false, time.Second)
	g.clock.Advance(2 * time.Second)

	// 8. NEXT entry: valid agents.send on leased chat — must still run!
	g.queueCall(t, 8, clientproto.MethodSend, g.s.dev.ID(), map[string]string{
		"id": chat, "text": "runs-after-refusals", "client_id": "c8",
	}, false, 0)

	if err := g.r.Step(context.Background()); err != nil {
		t.Fatalf("Step with deterministic refusals returned error: %v", err)
	}
	if g.queueLen(t, g.client.dev.ID()) != 0 {
		t.Fatalf("queue still has %d entries after Step, want 0", g.queueLen(t, g.client.dev.ID()))
	}
	if len(se.sends) != 1 || se.sends[0].Text != "runs-after-refusals" {
		t.Fatalf("next valid entry did not run after deterministic refusals: sends=%+v", se.sends)
	}

	outcomes := g.readOutcomesSlot(t, g.s.dev.ID())
	want := []clientproto.Outcome{
		{Number: 1, Status: clientproto.OutcomeRefused, Reason: clientproto.ReasonNotHolder},
		{Number: 2, Status: clientproto.OutcomeRefused, Reason: clientproto.ReasonMethodDenied},
		{Number: 3, Status: clientproto.OutcomeRefused, Agent: chat, Reason: clientproto.ReasonNeedsFaceID},
		{Number: 4, Status: clientproto.OutcomeRefused, Reason: clientproto.ReasonBadTarget},
		{Number: 5, Status: clientproto.OutcomeRefused, Reason: clientproto.ReasonBadCall},
		{Number: 6, Status: clientproto.OutcomeRefused, Reason: clientproto.ReasonBadCall},
		{Number: 7, Status: clientproto.OutcomeExpired, Reason: clientproto.ReasonExpired},
		{Number: 8, Status: clientproto.OutcomeDone, Agent: chat},
	}
	if !slices.Equal(outcomes, want) {
		t.Fatalf("outcomes =\n%+v\nwant\n%+v", outcomes, want)
	}
}

// TestS7QueuedCallWithChangedOuterTargetIsRunByNobody verifies that an entry whose outer
// target was altered in transit (or signed with an outer target different from
// the inner sealed target) is executed by nobody (contract S7).
func TestS7QueuedCallWithChangedOuterTargetIsRunByNobody(t *testing.T) {
	g := newRig(t)
	seA := g.useScriptedEngine(t)
	g.enroll(t, []string{"asks.list"}, clientproto.MethodCreate)
	if err := g.r.Step(context.Background()); err != nil {
		t.Fatal(err)
	}

	// Set up computer B.
	homeB := t.TempDir()
	if _, err := identity.Adopt(homeB, g.id, false); err != nil {
		t.Fatal(err)
	}
	devB, err := identity.Device(homeB)
	if err != nil {
		t.Fatal(err)
	}
	signerB := signer{g.id, devB}
	dirB := directory.NewHTTP(g.url, reqsign.SignFor(signerB, g.clock.Now), g.srv.Client())
	if err := dirB.PutDevice(context.Background(), devB.ID(), directory.Device{V: 1}); err != nil {
		t.Fatal(err)
	}
	seB := newScriptedEngine(&engine{})
	runnerB, err := Open(Config{
		Home:      t.TempDir(),
		Relay:     g.url,
		Name:      "laptop-b",
		Identity:  g.id.PublicKey(),
		Signer:    signerB,
		Directory: dirB,
		Engine:    seB,
		Log:       noopLog,
		Now:       g.clock.Now,
		HTTP:      g.srv.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runnerB.Close() })
	if err := runnerB.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	pubB, err := runnerB.executorPublicKey()
	if err != nil {
		t.Fatal(err)
	}

	// Case 1: Inner sealed call targets computer A, but outer signed envelope targets computer B.
	at := g.clock.Now().UnixMilli()
	innerForA := clientproto.CallPlain{
		V:        clientproto.CallVersion,
		Client:   g.client.dev.ID(),
		Number:   1,
		At:       at,
		NotAfter: at + clientproto.CreateLifetime.Milliseconds(),
		Target:   g.s.dev.ID(),
		Method:   clientproto.MethodCreate,
		Params:   json.RawMessage(`{"client_id":"s7-mismatch"}`),
	}
	sealedToB, err := clientproto.SealCall(innerForA, &pubB, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	g.request(t, g.client, "POST", "/v1/clients/"+g.client.dev.ID()+"/queue", map[string]any{
		"number": 1,
		"at":     at,
		"method": clientproto.MethodCreate,
		"target": devB.ID(),
		"sealed": sealedToB,
	}, nil, 204)

	// Neither A nor B may execute it.
	if err := g.r.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := runnerB.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(seA.creates) != 0 || len(seB.creates) != 0 {
		t.Fatalf("mismatched outer target ran: A=%d B=%d", len(seA.creates), len(seB.creates))
	}
	outcomesB := g.readOutcomesSlot(t, devB.ID())
	if len(outcomesB) != 1 || outcomesB[0] != (clientproto.Outcome{Number: 1, Status: clientproto.OutcomeRefused, Reason: clientproto.ReasonBadCall}) {
		t.Fatalf("outcomes B = %+v, want bad_call", outcomesB)
	}

	// Case 2: Phone signs target=A, and the relay alters outer target to B in GET /queue.
	g.queueCall(t, 2, clientproto.MethodCreate, g.s.dev.ID(), map[string]string{"client_id": "s7-relay-tamper"}, false, clientproto.CreateLifetime)
	g.reopen(t, transport(func(req *http.Request) (*http.Response, error) {
		resp, err := http.DefaultTransport.RoundTrip(req)
		if err != nil || req.Method != "GET" || !strings.HasSuffix(req.URL.Path, "/queue") {
			return resp, err
		}
		defer resp.Body.Close()
		var q map[string][]map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&q); err != nil {
			return nil, err
		}
		for i := range q["entries"] {
			q["entries"][i]["target"] = devB.ID()
		}
		raw, _ := json.Marshal(q)
		resp.Body = io.NopCloser(bytes.NewReader(raw))
		return resp, nil
	}), noopLog)
	seA = g.useScriptedEngine(t)
	if err := g.r.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(seA.creates) != 0 {
		t.Fatalf("tampered outer target ran on A: %d", len(seA.creates))
	}
}

// TestS8TransplantedSealedBodyFromAnotherPhoneIsBadCall verifies that a sealed
// call plaintext from Phone 1 copied into Phone 2's signed outer envelope is
// refused as bad_call and never executed (contract S8).
func TestS8TransplantedSealedBodyFromAnotherPhoneIsBadCall(t *testing.T) {
	g := newRig(t)
	se := g.useScriptedEngine(t)
	g.enroll(t, []string{"asks.list"}, clientproto.MethodCreate)

	// Enroll Phone 2 on the same identity.
	seed2, err := identity.NewSeed()
	if err != nil {
		t.Fatal(err)
	}
	dev2, err := seed2.Join(identity.IssueCert(g.id, seed2.Public(), g.clock.Now()))
	if err != nil {
		t.Fatal(err)
	}
	pub2, err := hex.DecodeString(dev2.Cert.Device)
	if err != nil {
		t.Fatal(err)
	}
	boxPub2, boxPriv2, err := box.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	phone2 := signer{g.id, dev2}
	e2 := signEnrollment(g.id, clientproto.Enrollment{
		V:       1,
		Device:  dev2.ID(),
		SignPub: pub2,
		BoxPub:  boxPub2[:],
		Methods: []string{clientproto.MethodCreate},
		Reads:   []string{"asks.list"},
		Ver:     1,
	})
	g.request(t, g.s, "PUT", "/v1/clients/"+dev2.ID()+"/enroll", e2, nil, 204)
	g.request(t, g.s, "PUT", "/v1/clients/"+dev2.ID(), map[string]any{
		"cert":    dev2.Cert,
		"methods": []string{clientproto.MethodCreate},
		"name":    "",
	}, nil, 204)
	if err := g.r.Step(context.Background()); err != nil {
		t.Fatal(err)
	}

	// Phone 1 seals a call naming Phone 1 as inner Client; Phone 2 re-wraps those
	// exact sealed bytes in Phone 2's signed outer envelope and posts to Phone 2's queue.
	at := g.clock.Now().UnixMilli()
	execPub := g.executorBoxPub(t)
	phone1Plain := clientproto.CallPlain{
		V:        clientproto.CallVersion,
		Client:   g.client.dev.ID(),
		Number:   1,
		At:       at,
		NotAfter: at + clientproto.CreateLifetime.Milliseconds(),
		Target:   g.s.dev.ID(),
		Method:   clientproto.MethodCreate,
		Params:   json.RawMessage(`{"client_id":"transplanted"}`),
	}
	sealedByPhone1, err := clientproto.SealCall(phone1Plain, &execPub, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	g.request(t, phone2, "POST", "/v1/clients/"+phone2.dev.ID()+"/queue", map[string]any{
		"number": 1,
		"at":     at,
		"method": clientproto.MethodCreate,
		"target": g.s.dev.ID(),
		"sealed": sealedByPhone1,
	}, nil, 204)

	if err := g.r.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(se.creates) != 0 {
		t.Fatalf("transplanted ciphertext executed: %d creates", len(se.creates))
	}
	// Open Phone 2's outcome slot and verify it is refused / bad_call.
	var slot struct {
		Sealed []byte `json:"sealed"`
	}
	g.request(t, phone2, "GET", "/v1/clients/"+phone2.dev.ID()+"/slots/"+SlotName("outcomes", g.s.dev.ID()), nil, &slot, 200)
	p, err := clientproto.OpenSlot(slot.Sealed, &execPub, boxPriv2)
	if err != nil {
		t.Fatal(err)
	}
	outcomes2, err := clientproto.DecodeOutcomes(p.Payload)
	if err != nil {
		t.Fatal(err)
	}
	want := []clientproto.Outcome{{Number: 1, Status: clientproto.OutcomeRefused, Reason: clientproto.ReasonBadCall}}
	if !slices.Equal(outcomes2, want) {
		t.Fatalf("phone 2 outcomes = %+v, want %+v", outcomes2, want)
	}
}

// TestS9PhoneMethodOutsideEnrollmentIsMethodDenied verifies that a method outside
// the phone's signed enrollment or outside clientproto.PhoneMethods() is
// refused with method_denied and never executed (contract S9).
func TestS9PhoneMethodOutsideEnrollmentIsMethodDenied(t *testing.T) {
	g := newRig(t)
	se := g.useScriptedEngine(t)

	// Enroll the phone ONLY for agents.create in the signed Enrollment, while
	// registering agents.send and settings.get on the relay device row so the
	// relay lets the phone queue them.
	pubBytes, err := hex.DecodeString(g.client.dev.Cert.Device)
	if err != nil {
		t.Fatal(err)
	}
	enroll := signEnrollment(g.id, clientproto.Enrollment{
		V:       1,
		Device:  g.client.dev.ID(),
		SignPub: pubBytes,
		BoxPub:  g.pub[:],
		Methods: []string{clientproto.MethodCreate},
		Reads:   []string{"asks.list"},
		Ver:     1,
	})
	g.request(t, g.s, "PUT", "/v1/clients/"+g.client.dev.ID()+"/enroll", enroll, nil, 204)
	g.request(t, g.s, "PUT", "/v1/clients/"+g.client.dev.ID(), map[string]any{
		"cert":    g.client.Cert(),
		"methods": []string{clientproto.MethodCreate, clientproto.MethodSend, "settings.get"},
		"name":    "",
	}, nil, 204)
	if err := g.r.Step(context.Background()); err != nil {
		t.Fatal(err)
	}

	// #1: agents.send is in PhoneMethods() but outside this phone's Enrollment.Methods.
	g.queueCall(t, 1, clientproto.MethodSend, g.s.dev.ID(), map[string]string{
		"id": chat, "text": "denied", "client_id": "c1",
	}, false, 0)
	// #2: settings.get is outside clientproto.PhoneMethods().
	g.queueCall(t, 2, "settings.get", g.s.dev.ID(), map[string]string{}, false, 0)

	if err := g.r.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(se.sends) != 0 {
		t.Fatalf("denied method executed: %+v", se.sends)
	}
	outcomes := g.readOutcomesSlot(t, g.s.dev.ID())
	want := []clientproto.Outcome{
		{Number: 1, Status: clientproto.OutcomeRefused, Reason: clientproto.ReasonMethodDenied},
		{Number: 2, Status: clientproto.OutcomeRefused, Reason: clientproto.ReasonMethodDenied},
	}
	if !slices.Equal(outcomes, want) {
		t.Fatalf("outcomes = %+v, want %+v", outcomes, want)
	}
}

// TestS10TransientFailuresLeaveEntryQueuedKeepOrderApplyBackoffAndExpireAfterNotAfter
// verifies that each transient failure (failed directory read, unreachable
// relay, failed claim round trip or claim conflict, save error, and engine
// error while reading asks or tasks before the call started) leaves the entry
// queued, unacknowledged, and unrun with no outcome, keeps queue order so
// entry #2 never jumps ahead of entry #1, applies the runner's backoff, and
// turns into a deterministic expired outcome once not_after passes (contract S10).
func TestS10TransientFailuresLeaveEntryQueuedKeepOrderApplyBackoffAndExpireAfterNotAfter(t *testing.T) {
	g := newRig(t)
	se := g.useScriptedEngine(t)
	g.enroll(t, []string{"agents.list"}, clientproto.MethodAnswer, clientproto.MethodTaskStart, clientproto.MethodSend)
	if err := g.r.Step(context.Background()); err != nil {
		t.Fatal(err)
	}

	// Queue entry #1 (asks.answer) and entry #2 (agents.send).
	g.e.asks = []appwire.Ask{{ID: "ask-1", Agent: chat}}
	g.queueCall(t, 1, clientproto.MethodAnswer, g.s.dev.ID(), map[string]string{
		"agent": chat, "id": "ask-1", "option": "allow",
	}, false, time.Hour)
	g.queueCall(t, 2, clientproto.MethodSend, g.s.dev.ID(), map[string]string{
		"id": chat, "text": "must-stay-behind-1", "client_id": "c2",
	}, false, time.Hour)

	assertUnackedAndUnrun := func(what string) {
		t.Helper()
		if len(se.answered) != 0 || len(se.sends) != 0 {
			t.Fatalf("%s: ran call during transient failure: answered=%+v sends=%+v", what, se.answered, se.sends)
		}
		if got := g.queueLen(t, g.client.dev.ID()); got != 2 {
			t.Fatalf("%s: queueLen = %d, want 2 (transient failure must not ack)", what, got)
		}
		if len(g.r.Outcomes(g.client.dev.ID())) != 0 {
			t.Fatalf("%s: recorded outcome for transient failure: %+v", what, g.r.Outcomes(g.client.dev.ID()))
		}
	}

	// 1. Engine error while reading asks before the call started.
	se.listAsksErr = errors.New("temporary disk busy reading asks")
	if err := g.r.Step(context.Background()); err == nil {
		t.Fatal("expected engine ListAsks error")
	}
	assertUnackedAndUnrun("engine ListAsks error")

	// Verify Runner.Serve applies exponential backoff (10s, 20s) when a transient queue error occurs.
	durations := make(chan time.Duration, 4)
	ticks := make(chan time.Time)
	if err := g.r.Close(); err != nil {
		t.Fatal(err)
	}
	rBackoff, err := Open(Config{
		Home:      g.state,
		Relay:     g.url,
		Name:      "desk",
		Identity:  g.id.PublicKey(),
		Signer:    g.s,
		Directory: noWatchDirClient{Client: g.dir},
		Engine:    se,
		Log:       noopLog,
		Now:       g.clock.Now,
		HTTP:      g.srv.Client(),
		After: func(d time.Duration) <-chan time.Time {
			durations <- d
			return ticks
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	g.r = rBackoff
	// The ceiling only guards against a hang: the rounds themselves run on the fake clock, and a
	// loaded box under the race detector can take longer than a few real seconds to reach them.
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	done := make(chan struct{})
	go func() {
		defer close(done)
		g.r.Serve(ctx)
	}()
	for _, want := range []time.Duration{10 * time.Second, 20 * time.Second} {
		select {
		case got := <-durations:
			if got != want {
				cancel()
				<-done
				t.Fatalf("transient drain backoff = %v, want %v", got, want)
			}
		case <-ctx.Done():
			cancel()
			<-done
			t.Fatal("timed out waiting for transient drain backoff")
		}
		if want == 10*time.Second {
			ticks <- g.clock.Now()
		}
	}
	cancel()
	<-done
	assertUnackedAndUnrun("after backoff rounds")
	se.listAsksErr = nil

	// 2. Save error (make <home>/clients read-only so writeAtomic fails on claiming save).
	g.reopen(t, nil, noopLog)
	se = g.useScriptedEngine(t)
	clientsDir := filepath.Dir(StatePath(g.state))
	if err := os.Chmod(clientsDir, 0500); err != nil {
		t.Fatal(err)
	}
	saveStepErr := g.r.Step(context.Background())
	_ = os.Chmod(clientsDir, 0700)
	if saveStepErr == nil {
		t.Fatal("expected save error when clients dir is read-only")
	}
	assertUnackedAndUnrun("save error")

	// 3. Unreachable relay on GET /queue.
	g.reopen(t, transport(func(req *http.Request) (*http.Response, error) {
		if req.Method == "GET" && strings.HasSuffix(req.URL.Path, "/queue") {
			return nil, errors.New("relay unreachable")
		}
		return http.DefaultTransport.RoundTrip(req)
	}), noopLog)
	se = g.useScriptedEngine(t)
	if err := g.r.Step(context.Background()); err == nil {
		t.Fatal("expected unreachable relay error")
	}
	assertUnackedAndUnrun("unreachable relay")

	// 4. Failed claim round trip / claim conflict (409).
	g.reopen(t, transport(func(req *http.Request) (*http.Response, error) {
		if req.Method == "POST" && strings.HasSuffix(req.URL.Path, "/claim") {
			return &http.Response{
				StatusCode: 409,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{"error":"claimed"}`)),
			}, nil
		}
		return http.DefaultTransport.RoundTrip(req)
	}), noopLog)
	se = g.useScriptedEngine(t)
	if err := g.r.Step(context.Background()); err == nil {
		t.Fatal("expected claim conflict error")
	}
	assertUnackedAndUnrun("claim conflict")

	// 5. Failed directory read during post-claim re-check.
	dirCalls := 0
	flakyDir := &flakyDirClient{Client: g.dir, failOnCall: 2, calls: &dirCalls}
	if err := g.r.Close(); err != nil {
		t.Fatal(err)
	}
	rFlakyDir, err := Open(Config{
		Home:      g.state,
		Relay:     g.url,
		Name:      "desk",
		Identity:  g.id.PublicKey(),
		Signer:    g.s,
		Directory: flakyDir,
		Engine:    se,
		Log:       noopLog,
		Now:       g.clock.Now,
		HTTP:      g.srv.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	g.r = rFlakyDir
	if err := g.r.Step(context.Background()); err == nil {
		t.Fatal("expected post-claim directory read failure")
	}
	assertUnackedAndUnrun("post-claim directory read failure")

	// When transient failures clear inside not_after, entries 1 and 2 run in order.
	g.reopen(t, nil, noopLog)
	se = g.useScriptedEngine(t)
	if err := g.r.Step(context.Background()); err != nil {
		t.Fatalf("Step after transient failures cleared: %v", err)
	}
	if len(se.answered) != 1 || len(se.sends) != 1 || se.sends[0].Text != "must-stay-behind-1" {
		t.Fatalf("entries did not run in order after recovery: answered=%+v sends=%+v", se.answered, se.sends)
	}

	// Now test a transient failure on tasks.start (ListTasks engine error) that
	// persists until after not_after passes: the entry becomes expired, is
	// acknowledged, and entry #4 behind it runs!
	g.queueCall(t, 3, clientproto.MethodTaskStart, g.s.dev.ID(), map[string]string{
		"id": chat + ":1",
	}, false, 5*time.Minute)
	g.queueCall(t, 4, clientproto.MethodSend, g.s.dev.ID(), map[string]string{
		"id": chat, "text": "runs-after-3-expires", "client_id": "c4",
	}, false, time.Hour)

	se.listTasksErr = errors.New("task store locked")
	if err := g.r.Step(context.Background()); err == nil {
		t.Fatal("expected ListTasks transient error")
	}
	if len(se.startedTasks) != 0 || len(se.sends) != 1 {
		t.Fatal("entry 3 or 4 ran during ListTasks failure")
	}

	// Advance clock past entry #3's 5-minute not_after (while entry #4's 1-hour
	// not_after is still valid) and keep this computer's lease on chat active.
	// Entry #3 expires in Step 3 before checkAsk, is acknowledged, and entry #4 runs!
	g.clock.Advance(6 * time.Minute)
	if _, err := g.dir.Acquire(context.Background(), chat, directory.AcquireOpts{}); err != nil {
		t.Fatal(err)
	}
	if err := g.r.Step(context.Background()); err != nil {
		t.Fatalf("Step after entry 3 expired: %v", err)
	}
	if len(se.startedTasks) != 0 || len(se.sends) != 2 || se.sends[1].Text != "runs-after-3-expires" {
		t.Fatalf("entry 4 did not run after entry 3 expired: started=%+v sends=%+v", se.startedTasks, se.sends)
	}
	outcomes := g.readOutcomesSlot(t, g.s.dev.ID())
	want := []clientproto.Outcome{
		{Number: 1, Status: clientproto.OutcomeDone, Agent: chat},
		{Number: 2, Status: clientproto.OutcomeDone, Agent: chat},
		{Number: 3, Status: clientproto.OutcomeExpired, Reason: clientproto.ReasonExpired},
		{Number: 4, Status: clientproto.OutcomeDone, Agent: chat},
	}
	if !slices.Equal(outcomes, want) {
		t.Fatalf("outcomes = %+v, want %+v", outcomes, want)
	}
}

type flakyDirClient struct {
	directory.Client
	failOnCall int
	calls      *int
}

func (f *flakyDirClient) List(ctx context.Context) (directory.Listing, error) {
	*f.calls++
	if *f.calls == f.failOnCall {
		return directory.Listing{}, directory.ErrUnreachable
	}
	return f.Client.List(ctx)
}

// TestS11EveryAcknowledgedCallHasOneOutcomeAndSlotKeepsNewest32 verifies that:
//  1. Every acknowledged call across all 6 phone methods (agents.create,
//     agents.send, asks.answer, agents.stop, inbox.answer, tasks.start) plus
//     dispatched-call failures (refused / failed) gets exactly one outcome with
//     the right number, status, agent, and reason, published to outcomes.<self>
//     before DELETE acknowledges the entry.
//  2. A bad proof is acknowledged with DELETE, keeps no receipt, and publishes
//     NO outcome.
//  3. When more than 32 calls finish, the outcome slot keeps the newest 32
//     (contract S11).
func TestS11EveryAcknowledgedCallHasOneOutcomeAndSlotKeepsNewest32(t *testing.T) {
	g := newRig(t)
	se := g.useScriptedEngine(t)
	g.e.asks = []appwire.Ask{{ID: "ask-ok", Agent: chat}}
	g.enroll(t, []string{"asks.list"}, clientproto.PhoneMethods()...)
	if err := g.r.Step(context.Background()); err != nil {
		t.Fatal(err)
	}

	// Verify outcome slot PUT happens BEFORE DELETE on the queue entry.
	var order []string
	g.reopen(t, transport(func(req *http.Request) (*http.Response, error) {
		if req.Method == "PUT" && strings.Contains(req.URL.Path, "/slots/outcomes.") {
			order = append(order, "PUT outcomes")
		}
		if req.Method == "DELETE" && strings.Contains(req.URL.Path, "/queue/") {
			order = append(order, "DELETE queue")
		}
		return http.DefaultTransport.RoundTrip(req)
	}), noopLog)
	se = g.useScriptedEngine(t)

	// 1. agents.create -> done, agent = created chat
	g.queueCall(t, 1, clientproto.MethodCreate, g.s.dev.ID(), map[string]string{"client_id": "s11-create"}, false, clientproto.CreateLifetime)
	// 2. agents.send -> done, agent = chat
	g.queueCall(t, 2, clientproto.MethodSend, g.s.dev.ID(), map[string]string{"id": chat, "text": "hi", "client_id": "s11-send"}, false, 0)
	// 3. asks.answer -> done, agent = chat
	g.queueCall(t, 3, clientproto.MethodAnswer, g.s.dev.ID(), map[string]string{"agent": chat, "id": "ask-ok", "option": "yes"}, false, 0)
	// 4. agents.stop -> done, agent = chat
	g.queueCall(t, 4, clientproto.MethodStop, g.s.dev.ID(), map[string]string{"id": chat}, false, 0)
	// 5. inbox.answer -> done, agent = chat
	g.queueCall(t, 5, clientproto.MethodInboxAnswer, g.s.dev.ID(), map[string]string{"id": chat + "/ask-ok", "option": "yes"}, false, 0)
	// 6. tasks.start -> done, agent = chat
	g.queueCall(t, 6, clientproto.MethodTaskStart, g.s.dev.ID(), map[string]string{"id": chat + ":1"}, false, 0)
	// 7. dispatched call error -> receipt failed, outcome refused / failed, agent = chat, never retried
	g.queueCall(t, 7, clientproto.MethodStop, g.s.dev.ID(), map[string]string{"id": chat}, false, 0)
	stopCalls := 0
	se.stopFn = func(id string) error {
		stopCalls++
		if stopCalls == 2 {
			return errors.New("stop failed in engine")
		}
		return nil
	}

	if err := g.r.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(order) < 2 || order[0] != "PUT outcomes" || order[1] != "DELETE queue" {
		t.Fatalf("outcome slot must be published before DELETE; order=%v", order)
	}
	createdID := se.createdIDs[0]
	outcomes := g.readOutcomesSlot(t, g.s.dev.ID())
	want := []clientproto.Outcome{
		{Number: 1, Status: clientproto.OutcomeDone, Agent: createdID},
		{Number: 2, Status: clientproto.OutcomeDone, Agent: chat},
		{Number: 3, Status: clientproto.OutcomeDone, Agent: chat},
		{Number: 4, Status: clientproto.OutcomeDone, Agent: chat},
		{Number: 5, Status: clientproto.OutcomeDone, Agent: chat},
		{Number: 6, Status: clientproto.OutcomeDone, Agent: chat},
		{Number: 7, Status: clientproto.OutcomeRefused, Agent: chat, Reason: clientproto.ReasonFailed},
	}
	if !slices.Equal(outcomes, want) {
		t.Fatalf("outcomes =\n%+v\nwant\n%+v", outcomes, want)
	}

	// Inject a bad proof at number 99 (tampered signature from relay) followed by
	// valid calls 8..36 (total 35 valid calls: 1..7 and 8..35):
	// the bad proof must be acknowledged (DELETE), keep no receipt, and produce
	// NO outcome, while the slot keeps the newest 32 (numbers 4..35).
	se.stopFn = nil
	for n := int64(8); n <= 35; n++ {
		g.queueCall(t, n, clientproto.MethodStop, g.s.dev.ID(), map[string]string{"id": chat}, false, 0)
	}
	injectBadProof := true
	var badProofDeleted bool
	g.reopen(t, transport(func(req *http.Request) (*http.Response, error) {
		if req.Method == "DELETE" && strings.HasSuffix(req.URL.Path, "/queue/99") {
			badProofDeleted = true
			return &http.Response{StatusCode: 204, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(""))}, nil
		}
		resp, err := http.DefaultTransport.RoundTrip(req)
		if err != nil || !injectBadProof || req.Method != "GET" || !strings.HasSuffix(req.URL.Path, "/queue") {
			return resp, err
		}
		injectBadProof = false
		defer resp.Body.Close()
		var q struct {
			Entries []map[string]any `json:"entries"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&q); err != nil {
			return nil, err
		}
		if len(q.Entries) > 0 {
			bad := map[string]any{}
			for k, v := range q.Entries[0] {
				bad[k] = v
			}
			bad["number"] = 7
			bad["signature"] = "tampered-signature"
			// Put the bad proof first with number 7, or at the end with number 99.
			bad["number"] = 99
			q.Entries = append(q.Entries, bad)
		}
		raw, _ := json.Marshal(q)
		resp.Body = io.NopCloser(bytes.NewReader(raw))
		return resp, nil
	}), noopLog)
	se = g.useScriptedEngine(t)

	if err := g.r.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !badProofDeleted {
		t.Fatal("bad proof entry 99 was not acknowledged with DELETE")
	}
	for _, rec := range g.r.Receipts() {
		if rec.Number == 99 {
			t.Fatalf("bad proof entry 99 left a receipt: %+v", rec)
		}
	}
	outcomes = g.readOutcomesSlot(t, g.s.dev.ID())
	if len(outcomes) != clientproto.MaxOutcomes {
		t.Fatalf("outcomes len = %d, want %d", len(outcomes), clientproto.MaxOutcomes)
	}
	if outcomes[0].Number != 4 || outcomes[len(outcomes)-1].Number != 35 {
		t.Fatalf("outcomes range = %d..%d, want 4..35 (bad proof 99 must have no outcome)", outcomes[0].Number, outcomes[len(outcomes)-1].Number)
	}
}

// TestV1AndV2QueueNeverDrainsPausedRevokedTombstonedOrBeforeDirectoryRead
// verifies that:
//  1. V1: a paused, revoked, or tombstoned phone is never claimed for and its
//     queued calls never run.
//  2. V2: nothing is drained in a round whose directory read failed.
func TestV1AndV2QueueNeverDrainsPausedRevokedTombstonedOrBeforeDirectoryRead(t *testing.T) {
	g := newRig(t)
	se := g.useScriptedEngine(t)
	g.enroll(t, []string{"asks.list"}, clientproto.MethodSend)
	if err := g.r.Step(context.Background()); err != nil {
		t.Fatal(err)
	}

	g.queueCall(t, 1, clientproto.MethodSend, g.s.dev.ID(), map[string]string{
		"id": chat, "text": "while-paused", "client_id": "c1",
	}, false, 0)

	// V1: Pause the phone locally; Step must make zero queue or claim requests and run nothing.
	if err := g.r.Pause(g.client.dev.ID(), true); err != nil {
		t.Fatal(err)
	}
	g.clearRequests()
	if err := g.r.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(se.sends) != 0 {
		t.Fatalf("paused phone call ran: %+v", se.sends)
	}
	for _, req := range g.recordedRequests() {
		if strings.Contains(req, "/queue") {
			t.Fatalf("paused phone touched queue route: %q", req)
		}
	}

	// V2: Unpause, but make Directory.List fail; Step must make zero queue or claim requests.
	if err := g.r.Pause(g.client.dev.ID(), false); err != nil {
		t.Fatal(err)
	}
	failDir := &failingDirClient{Client: g.dir, fail: true}
	g.r.cfg.Directory = failDir
	g.clearRequests()
	if err := g.r.Step(context.Background()); err == nil {
		t.Fatal("Step succeeded when Directory.List failed")
	}
	if len(se.sends) != 0 {
		t.Fatal("call ran when Directory.List failed")
	}
	for _, req := range g.recordedRequests() {
		if strings.Contains(req, "/queue") {
			t.Fatalf("failed directory read touched queue route: %q", req)
		}
	}

	// V1: Revoke the phone while offline, then restore Directory.List: the runner
	// tombstones the phone from the directory read and never drains or claims its queue.
	if err := g.dir.Revoke(context.Background(), g.client.dev.ID()); err != nil {
		t.Fatal(err)
	}
	failDir.fail = false
	g.clearRequests()
	if err := g.r.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(se.sends) != 0 {
		t.Fatal("revoked phone call ran")
	}
	for _, req := range g.recordedRequests() {
		if strings.Contains(req, "/queue") {
			t.Fatalf("revoked/tombstoned phone touched queue route: %q", req)
		}
	}
}

// TestQueueBoundsRejectOversizedEntryAndExcessEntriesWithoutAck verifies Step 2
// item 13 and D6:
//  1. A queue listing containing an entry larger than clientproto.MaxQueueBytes
//     errors before running or acknowledging any entry.
//  2. A queue listing with more than clientproto.MaxClientQueue valid entries
//     runs and acknowledges the first clientproto.MaxClientQueue (128) entries
//     in round 1 and leaves the remaining entries for round 2 so a long queue
//     is never wedged.
func TestQueueBoundsRejectOversizedEntryAndExcessEntriesWithoutAck(t *testing.T) {
	g := newRig(t)
	g.useScriptedEngine(t)
	g.enroll(t, []string{"asks.list"}, clientproto.MethodSend)
	if err := g.r.Step(context.Background()); err != nil {
		t.Fatal(err)
	}

	var acked []int64
	var servedEntries []Queued
	g.reopen(t, transport(func(req *http.Request) (*http.Response, error) {
		if req.Method == "DELETE" && strings.Contains(req.URL.Path, "/queue/") {
			idx := strings.LastIndex(req.URL.Path, "/")
			if n, err := strconv.ParseInt(req.URL.Path[idx+1:], 10, 64); err == nil {
				acked = append(acked, n)
				servedEntries = slices.DeleteFunc(servedEntries, func(q Queued) bool { return q.Number == n })
			}
			return &http.Response{StatusCode: 204, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(""))}, nil
		}
		if req.Method == "POST" && strings.HasSuffix(req.URL.Path, "/claim") {
			return &http.Response{StatusCode: 204, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(""))}, nil
		}
		if req.Method == "GET" && strings.HasSuffix(req.URL.Path, "/queue") {
			raw, _ := json.Marshal(map[string]any{"entries": servedEntries})
			return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(raw))}, nil
		}
		return http.DefaultTransport.RoundTrip(req)
	}), noopLog)
	se := g.useScriptedEngine(t)

	makeSignedEntry := func(num int64, text string) Queued {
		t.Helper()
		at := g.clock.Now().UnixMilli()
		pub := g.executorBoxPub(t)
		plain := clientproto.CallPlain{
			V:        clientproto.CallVersion,
			Client:   g.client.dev.ID(),
			Number:   num,
			At:       at,
			NotAfter: at + clientproto.CallLifetime.Milliseconds(),
			Target:   g.s.dev.ID(),
			Method:   clientproto.MethodSend,
			Params:   json.RawMessage(fmt.Sprintf(`{"id":%q,"text":%q,"client_id":"c-%d"}`, chat, text, num)),
		}
		sealed, err := clientproto.SealCall(plain, &pub, rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		body, err := json.Marshal(map[string]any{
			"number": num,
			"at":     at,
			"method": clientproto.MethodSend,
			"target": g.s.dev.ID(),
			"sealed": sealed,
		})
		if err != nil {
			t.Fatal(err)
		}
		req, err := http.NewRequest("POST", g.url+"/v1/clients/"+g.client.dev.ID()+"/queue", bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		reqsign.Sign(req, body, g.client, g.clock.Now())
		return Queued{
			Number:      num,
			At:          at,
			Method:      clientproto.MethodSend,
			Target:      g.s.dev.ID(),
			Sealed:      sealed,
			Signature:   req.Header.Get(reqsign.HeaderSig),
			Cert:        req.Header.Get(reqsign.HeaderCert),
			Identity:    req.Header.Get(reqsign.HeaderIdentity),
			RequestTime: req.Header.Get(reqsign.HeaderTime),
			RequestURI:  req.URL.RequestURI(),
			Body:        body,
		}
	}

	// 1. Entry with sealed bytes larger than clientproto.MaxQueueBytes: fails
	// before executing or acknowledging even a valid earlier entry in the same response.
	servedEntries = []Queued{
		makeSignedEntry(1, "valid-before-oversized"),
		{
			Number: 2,
			At:     g.clock.Now().UnixMilli(),
			Method: clientproto.MethodSend,
			Target: g.s.dev.ID(),
			Sealed: bytes.Repeat([]byte{'a'}, clientproto.MaxQueueBytes+1),
		},
	}
	if err := g.r.Step(context.Background()); err == nil {
		t.Fatal("accepted queue entry exceeding MaxQueueBytes")
	}
	if len(acked) != 0 || len(se.sends) != 0 {
		t.Fatalf("oversized queue listing ran=%d or acked=%v, want none", len(se.sends), acked)
	}

	// 2. More than clientproto.MaxClientQueue entries (130 entries): round 1 runs
	// and acknowledges exactly 1..128; round 2 runs and acknowledges 129..130.
	const totalEntries = clientproto.MaxClientQueue + 2
	servedEntries = make([]Queued, totalEntries)
	for i := range servedEntries {
		servedEntries[i] = makeSignedEntry(int64(i+1), fmt.Sprintf("msg-%d", i+1))
	}
	if err := g.r.Step(context.Background()); err != nil {
		t.Fatalf("round 1 with %d entries returned error: %v", totalEntries, err)
	}
	wantRound1 := make([]int64, clientproto.MaxClientQueue)
	for i := range wantRound1 {
		wantRound1[i] = int64(i + 1)
	}
	if !slices.Equal(acked, wantRound1) || len(se.sends) != clientproto.MaxClientQueue {
		t.Fatalf("round 1 acked len=%d sends=%d, want 1..%d", len(acked), len(se.sends), clientproto.MaxClientQueue)
	}

	// Round 2 drains the remaining 2 entries (129 and 130).
	acked = nil
	if err := g.r.Step(context.Background()); err != nil {
		t.Fatalf("round 2 draining remaining entries failed: %v", err)
	}
	wantRound2 := []int64{int64(clientproto.MaxClientQueue + 1), int64(clientproto.MaxClientQueue + 2)}
	if !slices.Equal(acked, wantRound2) || len(se.sends) != totalEntries {
		t.Fatalf("round 2 acked=%v sends=%d, want %v and %d", acked, len(se.sends), wantRound2, totalEntries)
	}
}

// TestD1ProofAuthenticityUsesSignedRequestTimeNotComputerClock verifies D1:
//  1. A computer whose clock is an hour BEHIND the relay still verifies and
//     runs a fresh call.
//  2. A call whose at is 10 hours older than its RequestTime (composed offline
//     and uploaded later) verifies and runs.
//  3. An entry whose at is more than the relay's forward allowance
//     (reqsign.Skew) past its RequestTime (or more than clientproto.MaxAge
//     older than its RequestTime) is dropped as never authored (acknowledged,
//     no receipt, no outcome).
//  4. A tampered RequestTime fails the HTTP signature and is dropped as never
//     authored.
func TestD1ProofAuthenticityUsesSignedRequestTimeNotComputerClock(t *testing.T) {
	t.Run("computer_clock_behind_relay_runs_fresh_call", func(t *testing.T) {
		g := newRig(t)
		se := g.useScriptedEngine(t)
		g.enroll(t, []string{"asks.list"}, clientproto.MethodSend)
		if err := g.r.Step(context.Background()); err != nil {
			t.Fatal(err)
		}

		// Advance the relay clock by 2 hours while the computer's clock is 1 hour
		// behind the relay, and keep this computer's lease on chat active.
		g.clock.Advance(2 * time.Hour)
		if _, err := g.dir.Acquire(context.Background(), chat, directory.AcquireOpts{}); err != nil {
			t.Fatal(err)
		}
		g.queueCall(t, 1, clientproto.MethodSend, g.s.dev.ID(), map[string]string{
			"id": chat, "text": "from-relay-ahead", "client_id": "c-behind",
		}, false, 0)

		computerNow := func() time.Time { return g.clock.Now().Add(-time.Hour) }
		if err := g.r.Close(); err != nil {
			t.Fatal(err)
		}
		// Sign runner HTTP requests near the relay clock while the runner's own
		// Config.Now is 1 hour behind the relay.
		rBehind, err := Open(Config{
			Home:      g.state,
			Relay:     g.url,
			Name:      "desk",
			Identity:  g.id.PublicKey(),
			Signer:    g.s,
			Directory: g.dir,
			Engine:    se,
			Log:       noopLog,
			Now:       computerNow,
			HTTP: &http.Client{Transport: transport(func(req *http.Request) (*http.Response, error) {
				var body []byte
				if req.Body != nil {
					body, _ = io.ReadAll(req.Body)
					req.Body = io.NopCloser(bytes.NewReader(body))
				}
				reqsign.Sign(req, body, g.s, g.clock.Now())
				return http.DefaultTransport.RoundTrip(req)
			})},
		})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = rBehind.Close() })
		g.r = rBehind

		if err := g.r.Step(context.Background()); err != nil {
			t.Fatalf("Step with computer clock 1h behind relay: %v", err)
		}
		if len(se.sends) != 1 || se.sends[0].Text != "from-relay-ahead" {
			t.Fatalf("call did not run when computer clock was 1h behind relay: sends=%+v", se.sends)
		}
		outcomes := g.readOutcomesSlot(t, g.s.dev.ID())
		want := []clientproto.Outcome{{Number: 1, Status: clientproto.OutcomeDone, Agent: chat}}
		if !slices.Equal(outcomes, want) {
			t.Fatalf("outcomes = %+v, want %+v", outcomes, want)
		}
	})

	t.Run("offline_call_ten_hours_older_than_request_time_verifies_and_runs", func(t *testing.T) {
		g := newRig(t)
		se := g.useScriptedEngine(t)
		g.enroll(t, []string{"asks.list"}, clientproto.MethodSend)
		if err := g.r.Step(context.Background()); err != nil {
			t.Fatal(err)
		}

		// Phone composes the call offline at T_compose, then uploads it 10 hours
		// later at T_upload = T_compose + 10h.
		composeAt := g.clock.Now().UnixMilli()
		g.clock.Advance(10 * time.Hour)
		if _, err := g.dir.Acquire(context.Background(), chat, directory.AcquireOpts{}); err != nil {
			t.Fatal(err)
		}
		pub := g.executorBoxPub(t)
		plain := clientproto.CallPlain{
			V:        clientproto.CallVersion,
			Client:   g.client.dev.ID(),
			Number:   1,
			At:       composeAt,
			NotAfter: composeAt + clientproto.CallLifetime.Milliseconds(),
			Target:   g.s.dev.ID(),
			Method:   clientproto.MethodSend,
			Params:   json.RawMessage(fmt.Sprintf(`{"id":%q,"text":"composed-10h-ago","client_id":"c-offline"}`, chat)),
		}
		sealed, err := clientproto.SealCall(plain, &pub, rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		g.request(t, g.client, "POST", "/v1/clients/"+g.client.dev.ID()+"/queue", map[string]any{
			"number": 1,
			"at":     composeAt,
			"method": clientproto.MethodSend,
			"target": g.s.dev.ID(),
			"sealed": sealed,
		}, nil, 204)

		if err := g.r.Step(context.Background()); err != nil {
			t.Fatalf("Step with 10h offline-composed call: %v", err)
		}
		if len(se.sends) != 1 || se.sends[0].Text != "composed-10h-ago" {
			t.Fatalf("10h offline-composed call did not run: sends=%+v", se.sends)
		}
		outcomes := g.readOutcomesSlot(t, g.s.dev.ID())
		want := []clientproto.Outcome{{Number: 1, Status: clientproto.OutcomeDone, Agent: chat}}
		if !slices.Equal(outcomes, want) {
			t.Fatalf("outcomes = %+v, want %+v", outcomes, want)
		}
	})

	t.Run("at_past_relay_forward_allowance_and_tampered_request_time_are_dropped", func(t *testing.T) {
		g := newRig(t)
		se := g.useScriptedEngine(t)
		g.enroll(t, []string{"asks.list"}, clientproto.MethodSend)
		if err := g.r.Step(context.Background()); err != nil {
			t.Fatal(err)
		}

		reqTime := g.clock.Now()
		pub := g.executorBoxPub(t)
		buildSignedEntry := func(num int64, callAt int64, signAt time.Time, overrideReqTime string) Queued {
			t.Helper()
			plain := clientproto.CallPlain{
				V:        clientproto.CallVersion,
				Client:   g.client.dev.ID(),
				Number:   num,
				At:       callAt,
				NotAfter: callAt + clientproto.CallLifetime.Milliseconds(),
				Target:   g.s.dev.ID(),
				Method:   clientproto.MethodSend,
				Params:   json.RawMessage(fmt.Sprintf(`{"id":%q,"text":"bad-%d","client_id":"c-%d"}`, chat, num, num)),
			}
			sealed, err := clientproto.SealCall(plain, &pub, rand.Reader)
			if err != nil {
				t.Fatal(err)
			}
			body, err := json.Marshal(map[string]any{
				"number": num,
				"at":     callAt,
				"method": clientproto.MethodSend,
				"target": g.s.dev.ID(),
				"sealed": sealed,
			})
			if err != nil {
				t.Fatal(err)
			}
			req, err := http.NewRequest("POST", g.url+"/v1/clients/"+g.client.dev.ID()+"/queue", bytes.NewReader(body))
			if err != nil {
				t.Fatal(err)
			}
			reqsign.Sign(req, body, g.client, signAt)
			rt := req.Header.Get(reqsign.HeaderTime)
			if overrideReqTime != "" {
				rt = overrideReqTime
			}
			return Queued{
				Number:      num,
				At:          callAt,
				Method:      clientproto.MethodSend,
				Target:      g.s.dev.ID(),
				Sealed:      sealed,
				Signature:   req.Header.Get(reqsign.HeaderSig),
				Cert:        req.Header.Get(reqsign.HeaderCert),
				Identity:    req.Header.Get(reqsign.HeaderIdentity),
				RequestTime: rt,
				RequestURI:  req.URL.RequestURI(),
				Body:        body,
			}
		}

		// Entry 1: at is 1ms past reqTime + reqsign.Skew (even though computer clock
		// will be advanced 1 hour ahead of at, verify must evaluate against RequestTime!).
		tooFarAheadAt := reqTime.UnixMilli() + reqsign.Skew.Milliseconds() + 1
		e1 := buildSignedEntry(1, tooFarAheadAt, reqTime, "")

		// Entry 2: at is valid, but RequestTime was tampered with (+1ms).
		e2 := buildSignedEntry(2, reqTime.UnixMilli(), reqTime, strconv.FormatInt(reqTime.UnixMilli()+1, 10))

		// Advance computer clock by 30 minutes (so computer clock is well ahead of tooFarAheadAt)
		// and refresh the lease on chat.
		g.clock.Advance(30 * time.Minute)
		if _, err := g.dir.Acquire(context.Background(), chat, directory.AcquireOpts{}); err != nil {
			t.Fatal(err)
		}

		var deleted []int64
		served := []Queued{e1, e2}
		g.reopen(t, transport(func(req *http.Request) (*http.Response, error) {
			if req.Method == "DELETE" && strings.Contains(req.URL.Path, "/queue/") {
				idx := strings.LastIndex(req.URL.Path, "/")
				if n, err := strconv.ParseInt(req.URL.Path[idx+1:], 10, 64); err == nil {
					deleted = append(deleted, n)
				}
				return &http.Response{StatusCode: 204, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(""))}, nil
			}
			if req.Method == "GET" && strings.HasSuffix(req.URL.Path, "/queue") {
				raw, _ := json.Marshal(map[string]any{"entries": served})
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(raw))}, nil
			}
			return http.DefaultTransport.RoundTrip(req)
		}), noopLog)
		se = g.useScriptedEngine(t)

		if err := g.r.Step(context.Background()); err != nil {
			t.Fatal(err)
		}
		if len(se.sends) != 0 {
			t.Fatalf("unauthentic entries ran: %+v", se.sends)
		}
		if !slices.Equal(deleted, []int64{1, 2}) {
			t.Fatalf("deleted = %v, want [1 2]", deleted)
		}
		if len(g.r.Receipts()) != 0 {
			t.Fatalf("bad proof left receipts: %+v", g.r.Receipts())
		}
		if outcomes := g.readOutcomesSlot(t, g.s.dev.ID()); len(outcomes) != 0 {
			t.Fatalf("bad proof published outcomes: %+v", outcomes)
		}
	})
}

// TestD2AskOwnershipNotFoundAndFaceIDBatchGuards verifies D2:
//  1. An order: and a crewprop: id (or any ask id not in asks.list(agent) for
//     the named chat) are refused bad_call for asks.answer and inbox.answer,
//     and the engine is never called.
//  2. An appwire CodeNotFound error from asks.list or ListTasks for a chat this
//     computer's directory listing says it holds is refused deterministically as
//     not_holder (not a transient failure), and the NEXT entry runs.
//  3. An irreversible ask or task without face_id is refused needs_face_id for
//     asks.answer, inbox.answer, and tasks.start, and runs with face_id.
//  4. A batch asks.answer whose sibling is irreversible is refused needs_face_id
//     through AskAnswer.BeforeResolve on an appwire.Talker engine (and runs with
//     face_id), and inbox.answer on a chat with an open irreversible sibling ask
//     refuses conservatively without face_id and runs with face_id.
func TestD2AskOwnershipNotFoundAndFaceIDBatchGuards(t *testing.T) {
	t.Run("order_and_crewprop_refused_bad_call_without_calling_engine", func(t *testing.T) {
		g := newRig(t)
		se := g.useScriptedEngine(t)
		// Open asks on chat do not include order:1 or crewprop:1 (even if an ask
		// with that ID exists under another chat).
		g.e.asks = []appwire.Ask{
			{ID: "real-ask", Agent: chat},
			{ID: "other-chat-ask", Agent: otherChat},
		}
		g.enroll(t, []string{"asks.list"}, clientproto.MethodAnswer, clientproto.MethodInboxAnswer)
		if err := g.r.Step(context.Background()); err != nil {
			t.Fatal(err)
		}

		// 1. asks.answer with order:1
		g.queueCall(t, 1, clientproto.MethodAnswer, g.s.dev.ID(), map[string]string{
			"agent": chat, "id": "order:1", "option": "run",
		}, true, 0)
		// 2. asks.answer with crewprop:1
		g.queueCall(t, 2, clientproto.MethodAnswer, g.s.dev.ID(), map[string]string{
			"agent": chat, "id": "crewprop:1", "option": "approve",
		}, true, 0)
		// 3. asks.answer naming an ask from otherChat
		g.queueCall(t, 3, clientproto.MethodAnswer, g.s.dev.ID(), map[string]string{
			"agent": chat, "id": "other-chat-ask", "option": "yes",
		}, true, 0)
		// 4. inbox.answer with order:1
		g.queueCall(t, 4, clientproto.MethodInboxAnswer, g.s.dev.ID(), map[string]string{
			"id": chat + "/order:1", "option": "run",
		}, true, 0)
		// 5. inbox.answer with crewprop:1
		g.queueCall(t, 5, clientproto.MethodInboxAnswer, g.s.dev.ID(), map[string]string{
			"id": chat + "/crewprop:1", "option": "approve",
		}, true, 0)

		if err := g.r.Step(context.Background()); err != nil {
			t.Fatal(err)
		}
		if len(se.answered) != 0 || len(se.inboxAnswers) != 0 || len(se.beforeResolveLog) != 0 {
			t.Fatalf("engine was called for foreign/unknown ask IDs: answered=%+v inbox=%+v beforeResolve=%+v", se.answered, se.inboxAnswers, se.beforeResolveLog)
		}
		outcomes := g.readOutcomesSlot(t, g.s.dev.ID())
		want := []clientproto.Outcome{
			{Number: 1, Status: clientproto.OutcomeRefused, Agent: chat, Reason: clientproto.ReasonBadCall},
			{Number: 2, Status: clientproto.OutcomeRefused, Agent: chat, Reason: clientproto.ReasonBadCall},
			{Number: 3, Status: clientproto.OutcomeRefused, Agent: chat, Reason: clientproto.ReasonBadCall},
			{Number: 4, Status: clientproto.OutcomeRefused, Agent: chat, Reason: clientproto.ReasonBadCall},
			{Number: 5, Status: clientproto.OutcomeRefused, Agent: chat, Reason: clientproto.ReasonBadCall},
		}
		if !slices.Equal(outcomes, want) {
			t.Fatalf("outcomes =\n%+v\nwant\n%+v", outcomes, want)
		}
	})

	t.Run("not_found_from_asks_list_and_list_tasks_is_not_holder_and_next_entry_runs", func(t *testing.T) {
		g := newRig(t)
		se := g.useScriptedEngine(t)
		g.enroll(t, []string{"agents.list"}, clientproto.MethodAnswer, clientproto.MethodTaskStart, clientproto.MethodSend)
		if err := g.r.Step(context.Background()); err != nil {
			t.Fatal(err)
		}

		se.listAsksErr = appwire.Errorf(appwire.CodeNotFound, "chat %s is not on this engine", chat)
		se.listTasksErr = appwire.Errorf(appwire.CodeNotFound, "chat %s is not on this engine", chat)

		g.queueCall(t, 1, clientproto.MethodAnswer, g.s.dev.ID(), map[string]string{
			"agent": chat, "id": "ask-1", "option": "yes",
		}, false, 0)
		g.queueCall(t, 2, clientproto.MethodTaskStart, g.s.dev.ID(), map[string]string{
			"id": chat + ":1",
		}, false, 0)
		g.queueCall(t, 3, clientproto.MethodSend, g.s.dev.ID(), map[string]string{
			"id": chat, "text": "next-entry-runs", "client_id": "c3",
		}, false, 0)

		if err := g.r.Step(context.Background()); err != nil {
			t.Fatalf("Step returned transient error on CodeNotFound: %v", err)
		}
		if len(se.sends) != 1 || se.sends[0].Text != "next-entry-runs" {
			t.Fatalf("next entry did not run after CodeNotFound not_holder: sends=%+v", se.sends)
		}
		outcomes := g.readOutcomesSlot(t, g.s.dev.ID())
		want := []clientproto.Outcome{
			{Number: 1, Status: clientproto.OutcomeRefused, Agent: chat, Reason: clientproto.ReasonNotHolder},
			{Number: 2, Status: clientproto.OutcomeRefused, Agent: chat, Reason: clientproto.ReasonNotHolder},
			{Number: 3, Status: clientproto.OutcomeDone, Agent: chat},
		}
		if !slices.Equal(outcomes, want) {
			t.Fatalf("outcomes =\n%+v\nwant\n%+v", outcomes, want)
		}
	})

	t.Run("irreversible_ask_and_task_and_batch_sibling_face_id_guards", func(t *testing.T) {
		g := newRig(t)
		se := g.useScriptedEngine(t)
		g.e.taskIrreversible = true
		g.e.asks = []appwire.Ask{
			{ID: "ask-irrev", Agent: chat, Irreversible: true},
		}
		g.enroll(t, []string{"asks.list"}, clientproto.MethodAnswer, clientproto.MethodInboxAnswer, clientproto.MethodTaskStart)
		if err := g.r.Step(context.Background()); err != nil {
			t.Fatal(err)
		}

		// 1..3: irreversible ask/task without face_id for asks.answer, inbox.answer, tasks.start -> needs_face_id
		g.queueCall(t, 1, clientproto.MethodAnswer, g.s.dev.ID(), map[string]string{
			"agent": chat, "id": "ask-irrev", "option": "allow",
		}, false, 0)
		g.queueCall(t, 2, clientproto.MethodInboxAnswer, g.s.dev.ID(), map[string]string{
			"id": chat + "/ask-irrev", "option": "allow",
		}, false, 0)
		g.queueCall(t, 3, clientproto.MethodTaskStart, g.s.dev.ID(), map[string]string{
			"id": chat + ":1",
		}, false, 0)

		// 4..6: same three calls WITH face_id: true -> all three run!
		g.queueCall(t, 4, clientproto.MethodAnswer, g.s.dev.ID(), map[string]string{
			"agent": chat, "id": "ask-irrev", "option": "allow",
		}, true, 0)
		g.queueCall(t, 5, clientproto.MethodInboxAnswer, g.s.dev.ID(), map[string]string{
			"id": chat + "/ask-irrev", "option": "allow",
		}, true, 0)
		g.queueCall(t, 6, clientproto.MethodTaskStart, g.s.dev.ID(), map[string]string{
			"id": chat + ":1",
		}, true, 0)

		if err := g.r.Step(context.Background()); err != nil {
			t.Fatal(err)
		}
		if len(se.answered) != 1 || len(se.inboxAnswers) != 1 || len(se.startedTasks) != 1 {
			t.Fatalf("expected 1 run each with face_id: answered=%d inbox=%d tasks=%d", len(se.answered), len(se.inboxAnswers), len(se.startedTasks))
		}

		// Now set up a batch of two asks on chat: ask-batch-rev (reversible) and
		// ask-batch-irrev (irreversible sibling).
		batchItems := []appwire.BatchItem{{ID: "ask-batch-rev"}, {ID: "ask-batch-irrev"}}
		g.e.asks = []appwire.Ask{
			{ID: "ask-batch-rev", Agent: chat, Irreversible: false, Batch: batchItems},
			{ID: "ask-batch-irrev", Agent: chat, Irreversible: true, Batch: batchItems},
		}
		se.beforeResolveLog = nil

		// 7: asks.answer naming the reversible ask-batch-rev without face_id ->
		// preflight passes on ask-batch-rev, then answerGuard's BeforeResolve sees
		// ask-batch-irrev in the resolved batch and refuses needs_face_id!
		g.queueCall(t, 7, clientproto.MethodAnswer, g.s.dev.ID(), map[string]string{
			"agent": chat, "id": "ask-batch-rev", "option": "allow_once",
		}, false, 0)
		// 8: inbox.answer naming ask-batch-rev without face_id -> refused
		// conservatively as needs_face_id because ask-batch-irrev is open on chat!
		g.queueCall(t, 8, clientproto.MethodInboxAnswer, g.s.dev.ID(), map[string]string{
			"id": chat + "/ask-batch-rev", "option": "allow_once",
		}, false, 0)
		// 9: batch asks.answer with face_id: true -> BeforeResolve allows it and it runs!
		g.queueCall(t, 9, clientproto.MethodAnswer, g.s.dev.ID(), map[string]string{
			"agent": chat, "id": "ask-batch-rev", "option": "allow_once",
		}, true, 0)
		// 10: inbox.answer with face_id: true -> runs!
		g.queueCall(t, 10, clientproto.MethodInboxAnswer, g.s.dev.ID(), map[string]string{
			"id": chat + "/ask-batch-rev", "option": "allow_once",
		}, true, 0)

		if err := g.r.Step(context.Background()); err != nil {
			t.Fatal(err)
		}
		// Verify BeforeResolve was called twice (for #7 and #9) with both batch items.
		if len(se.beforeResolveLog) != 2 || !slices.Equal(se.beforeResolveLog[0], []string{"ask-batch-rev", "ask-batch-irrev"}) {
			t.Fatalf("BeforeResolve log = %+v, want two calls with [ask-batch-rev ask-batch-irrev]", se.beforeResolveLog)
		}
		if len(se.answered) != 2 || len(se.inboxAnswers) != 2 {
			t.Fatalf("after batch calls: answered=%d inbox=%d, want 2 each", len(se.answered), len(se.inboxAnswers))
		}
		outcomes := g.readOutcomesSlot(t, g.s.dev.ID())
		want := []clientproto.Outcome{
			{Number: 1, Status: clientproto.OutcomeRefused, Agent: chat, Reason: clientproto.ReasonNeedsFaceID},
			{Number: 2, Status: clientproto.OutcomeRefused, Agent: chat, Reason: clientproto.ReasonNeedsFaceID},
			{Number: 3, Status: clientproto.OutcomeRefused, Agent: chat, Reason: clientproto.ReasonNeedsFaceID},
			{Number: 4, Status: clientproto.OutcomeDone, Agent: chat},
			{Number: 5, Status: clientproto.OutcomeDone, Agent: chat},
			{Number: 6, Status: clientproto.OutcomeDone, Agent: chat},
			{Number: 7, Status: clientproto.OutcomeRefused, Agent: chat, Reason: clientproto.ReasonNeedsFaceID},
			{Number: 8, Status: clientproto.OutcomeRefused, Agent: chat, Reason: clientproto.ReasonNeedsFaceID},
			{Number: 9, Status: clientproto.OutcomeDone, Agent: chat},
			{Number: 10, Status: clientproto.OutcomeDone, Agent: chat},
		}
		if !slices.Equal(outcomes, want) {
			t.Fatalf("outcomes =\n%+v\nwant\n%+v", outcomes, want)
		}
	})
}

// TestD3CreateMapCleanedOnFailedCreateWithoutFolderAndRecoveredByFolderExistence
// verifies D3:
//  1. A create whose engine calls BeforeMint (reserve) and then aborts before
//     making a folder removes the (client_id -> reserved chat) mapping in the
//     same save that records the failed outcome, so a second create with the
//     same client_id creates a real chat.
//  2. A crash after the folder exists on disk but before the final save
//     recovers in recoverStarted as done with that chat, and a retry with the
//     same client_id creates none.
//  3. A crash after reserve (BeforeMint) with no folder on disk recovers in
//     recoverStarted as refused / failed with the mapping removed, and a retry
//     with the same client_id creates a chat.
func TestD3CreateMapCleanedOnFailedCreateWithoutFolderAndRecoveredByFolderExistence(t *testing.T) {
	t.Run("engine_aborts_before_folder_then_retry_creates", func(t *testing.T) {
		g := newRig(t)
		se := g.useScriptedEngine(t)
		g.enroll(t, []string{"agents.list"}, clientproto.MethodCreate)
		if err := g.r.Step(context.Background()); err != nil {
			t.Fatal(err)
		}

		const cid = "cid-abort-no-folder"
		abortFirst := true
		se.createFn = func(ask appwire.CreateAsk) (appwire.Agent, error) {
			if abortFirst {
				abortFirst = false
				ghostFolder := filepath.Join(se.chatsDir, "01JGHOST000000000000000001")
				if err := ask.BeforeMint(filepath.Join(ghostFolder, "transcript.jsonl")); err != nil {
					return appwire.Agent{}, err
				}
				return appwire.Agent{}, errors.New("provider key missing before folder creation")
			}
			id := "01JREALCREATE0000000000001"
			folder := filepath.Join(se.chatsDir, id)
			if err := ask.BeforeMint(filepath.Join(folder, "transcript.jsonl")); err != nil {
				return appwire.Agent{}, err
			}
			if err := os.MkdirAll(folder, 0700); err != nil {
				return appwire.Agent{}, err
			}
			a := appwire.Agent{ID: id}
			if err := ask.BeforeStart(a); err != nil {
				return appwire.Agent{}, err
			}
			se.createdIDs = append(se.createdIDs, id)
			return a, nil
		}

		// Call #1 aborts after BeforeMint without creating the folder.
		g.queueCall(t, 1, clientproto.MethodCreate, g.s.dev.ID(), map[string]string{"client_id": cid}, false, clientproto.CreateLifetime)
		if err := g.r.Step(context.Background()); err != nil {
			t.Fatal(err)
		}
		if _, ok := g.r.createdChat(g.client.dev.ID(), cid); ok {
			t.Fatal("failed create without folder left ghost chat in create map")
		}

		// Call #2 with the SAME client_id must create a real chat (1 chat created).
		g.queueCall(t, 2, clientproto.MethodCreate, g.s.dev.ID(), map[string]string{"client_id": cid}, false, clientproto.CreateLifetime)
		if err := g.r.Step(context.Background()); err != nil {
			t.Fatal(err)
		}
		if len(se.createdIDs) != 1 || se.createdIDs[0] != "01JREALCREATE0000000000001" {
			t.Fatalf("createdIDs = %+v, want [01JREALCREATE0000000000001]", se.createdIDs)
		}
		outcomes := g.readOutcomesSlot(t, g.s.dev.ID())
		want := []clientproto.Outcome{
			{Number: 1, Status: clientproto.OutcomeRefused, Reason: clientproto.ReasonFailed},
			{Number: 2, Status: clientproto.OutcomeDone, Agent: "01JREALCREATE0000000000001"},
		}
		if !slices.Equal(outcomes, want) {
			t.Fatalf("outcomes =\n%+v\nwant\n%+v", outcomes, want)
		}
	})

	t.Run("crash_after_folder_exists_finishes_done_and_retry_creates_none", func(t *testing.T) {
		g := newRig(t)
		se := g.useScriptedEngine(t)
		chatsDir := se.chatsDir
		g.enroll(t, []string{"agents.list"}, clientproto.MethodCreate)
		if err := g.r.Step(context.Background()); err != nil {
			t.Fatal(err)
		}

		const cid = "cid-crash-with-folder"
		const existingID = "01JFOLDEREXISTS00000000001"
		snapHome := t.TempDir()
		g.reopen(t, transport(func(req *http.Request) (*http.Response, error) {
			if req.Method == "DELETE" && strings.Contains(req.URL.Path, "/queue/") {
				return nil, errors.New("crash before delete")
			}
			return http.DefaultTransport.RoundTrip(req)
		}), noopLog)
		se = g.useScriptedEngine(t)
		se.chatsDir = chatsDir
		se.createFn = func(ask appwire.CreateAsk) (appwire.Agent, error) {
			folder := filepath.Join(chatsDir, existingID)
			if err := ask.BeforeMint(filepath.Join(folder, "transcript.jsonl")); err != nil {
				return appwire.Agent{}, err
			}
			if err := os.MkdirAll(folder, 0700); err != nil {
				return appwire.Agent{}, err
			}
			for _, fn := range []func(string) string{StatePath, PinsPath, TombstonesPath} {
				if raw, err := os.ReadFile(fn(g.state)); err == nil {
					dst := fn(snapHome)
					_ = os.MkdirAll(filepath.Dir(dst), 0700)
					_ = os.WriteFile(dst, raw, 0600)
				}
			}
			return appwire.Agent{}, errors.New("crash after folder created")
		}

		g.queueCall(t, 1, clientproto.MethodCreate, g.s.dev.ID(), map[string]string{"client_id": cid}, false, clientproto.CreateLifetime)
		_ = g.r.Step(context.Background())

		// Recover from snapHome (receipt at "started", folder exists on disk), and
		// also queue retry #2 with the same client_id.
		_ = g.r.Close()
		g.state = snapHome
		g.reopen(t, nil, noopLog)
		se = g.useScriptedEngine(t)
		se.chatsDir = chatsDir
		g.queueCall(t, 2, clientproto.MethodCreate, g.s.dev.ID(), map[string]string{"client_id": cid}, false, clientproto.CreateLifetime)

		if err := g.r.Step(context.Background()); err != nil {
			t.Fatal(err)
		}
		if len(se.creates) != 0 {
			t.Fatalf("retry created %d chats when folder already existed, want 0", len(se.creates))
		}
		outcomes := g.readOutcomesSlot(t, g.s.dev.ID())
		want := []clientproto.Outcome{
			{Number: 1, Status: clientproto.OutcomeDone, Agent: existingID},
			{Number: 2, Status: clientproto.OutcomeDone, Agent: existingID},
		}
		if !slices.Equal(outcomes, want) {
			t.Fatalf("outcomes =\n%+v\nwant\n%+v", outcomes, want)
		}
	})

	t.Run("crash_after_reserve_with_no_folder_recovers_failed_and_retry_creates", func(t *testing.T) {
		g := newRig(t)
		se := g.useScriptedEngine(t)
		chatsDir := se.chatsDir
		g.enroll(t, []string{"agents.list"}, clientproto.MethodCreate)
		if err := g.r.Step(context.Background()); err != nil {
			t.Fatal(err)
		}

		const cid = "cid-crash-no-folder"
		const reservedID = "01JRESERVEDNOFOLDER0000001"
		snapHome := t.TempDir()
		g.reopen(t, transport(func(req *http.Request) (*http.Response, error) {
			if req.Method == "DELETE" && strings.Contains(req.URL.Path, "/queue/") {
				return nil, errors.New("crash before delete")
			}
			return http.DefaultTransport.RoundTrip(req)
		}), noopLog)
		se = g.useScriptedEngine(t)
		se.chatsDir = chatsDir
		se.createFn = func(ask appwire.CreateAsk) (appwire.Agent, error) {
			folder := filepath.Join(chatsDir, reservedID)
			if err := ask.BeforeMint(filepath.Join(folder, "transcript.jsonl")); err != nil {
				return appwire.Agent{}, err
			}
			// Snapshot runner.json BEFORE any folder is created on disk!
			for _, fn := range []func(string) string{StatePath, PinsPath, TombstonesPath} {
				if raw, err := os.ReadFile(fn(g.state)); err == nil {
					dst := fn(snapHome)
					_ = os.MkdirAll(filepath.Dir(dst), 0700)
					_ = os.WriteFile(dst, raw, 0600)
				}
			}
			return appwire.Agent{}, errors.New("crash right after reserve with no folder")
		}

		g.queueCall(t, 1, clientproto.MethodCreate, g.s.dev.ID(), map[string]string{"client_id": cid}, false, clientproto.CreateLifetime)
		_ = g.r.Step(context.Background())

		// Recover from snapHome (receipt at "started", NO folder on disk), and
		// queue retry #2 with the same client_id: #1 must recover as refused/failed
		// with mapping removed, and #2 must create a chat!
		_ = g.r.Close()
		g.state = snapHome
		g.reopen(t, nil, noopLog)
		se = g.useScriptedEngine(t)
		se.chatsDir = chatsDir
		g.queueCall(t, 2, clientproto.MethodCreate, g.s.dev.ID(), map[string]string{"client_id": cid}, false, clientproto.CreateLifetime)

		if err := g.r.Step(context.Background()); err != nil {
			t.Fatal(err)
		}
		if len(se.creates) != 1 || len(se.createdIDs) != 1 {
			t.Fatalf("retry after reserve-only crash created %d chats, want 1", len(se.creates))
		}
		outcomes := g.readOutcomesSlot(t, g.s.dev.ID())
		want := []clientproto.Outcome{
			{Number: 1, Status: clientproto.OutcomeRefused, Reason: clientproto.ReasonFailed},
			{Number: 2, Status: clientproto.OutcomeDone, Agent: se.createdIDs[0]},
		}
		if !slices.Equal(outcomes, want) {
			t.Fatalf("outcomes =\n%+v\nwant\n%+v", outcomes, want)
		}
	})
}

// TestD5OutcomeAgentBoundedAnd32LargestRefusedCallsFitInOneSlot verifies D5:
//  1. 32 refused calls built from the largest accepted sealed call
//     (clientproto.MaxQueueBytes) leave Outcome.Agent empty when the agent does
//     not name a held or just-created chat within clientproto.MaxOutcomeAgent,
//     and all 32 outcomes flush in one slot well below clientproto.MaxSlotBytes.
//  2. Even 32 refused outcomes each carrying a max-length (MaxOutcomeAgent = 128)
//     held chat ID fit within one clientproto.MaxSlotBytes outcome slot.
func TestD5OutcomeAgentBoundedAnd32LargestRefusedCallsFitInOneSlot(t *testing.T) {
	g := newRig(t)
	g.useScriptedEngine(t)
	g.enroll(t, []string{"asks.list"}, clientproto.MethodSend)
	if err := g.r.Step(context.Background()); err != nil {
		t.Fatal(err)
	}

	// Find the largest agent ID string that produces a sealed call <= clientproto.MaxQueueBytes.
	pub := g.executorBoxPub(t)
	at := g.clock.Now().UnixMilli()
	bigAgent := strings.Repeat("x", 3500)
	for len(bigAgent) > 1000 {
		plain := clientproto.CallPlain{
			V:        clientproto.CallVersion,
			Client:   g.client.dev.ID(),
			Number:   1,
			At:       at,
			NotAfter: at + clientproto.CallLifetime.Milliseconds(),
			Target:   g.s.dev.ID(),
			Method:   clientproto.MethodSend,
			Params:   json.RawMessage(fmt.Sprintf(`{"id":%q,"text":"x","client_id":"c"}`, bigAgent)),
		}
		sealed, err := clientproto.SealCall(plain, &pub, rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		if len(sealed) <= clientproto.MaxQueueBytes {
			break
		}
		bigAgent = bigAgent[:len(bigAgent)-64]
	}

	for n := int64(1); n <= clientproto.MaxOutcomes; n++ {
		g.queueCall(t, n, clientproto.MethodSend, g.s.dev.ID(), map[string]string{
			"id": bigAgent, "text": "x", "client_id": "c",
		}, false, 0)
	}
	if err := g.r.Step(context.Background()); err != nil {
		t.Fatalf("Step with 32 largest refused calls failed: %v", err)
	}
	if g.queueLen(t, g.client.dev.ID()) != 0 {
		t.Fatalf("queueLen = %d, want 0", g.queueLen(t, g.client.dev.ID()))
	}
	var rawSlot struct {
		Sealed []byte `json:"sealed"`
	}
	g.request(t, g.client, "GET", "/v1/clients/"+g.client.dev.ID()+"/slots/"+SlotName("outcomes", g.s.dev.ID()), nil, &rawSlot, 200)
	if len(rawSlot.Sealed) > clientproto.MaxSlotBytes {
		t.Fatalf("sealed outcomes slot size = %d, exceeds MaxSlotBytes %d", len(rawSlot.Sealed), clientproto.MaxSlotBytes)
	}
	outcomes := g.readOutcomesSlot(t, g.s.dev.ID())
	if len(outcomes) != clientproto.MaxOutcomes {
		t.Fatalf("outcomes len = %d, want %d", len(outcomes), clientproto.MaxOutcomes)
	}
	for i, o := range outcomes {
		if o.Agent != "" || o.Status != clientproto.OutcomeRefused || o.Reason != clientproto.ReasonNotHolder {
			t.Fatalf("outcome[%d] = %+v, want empty Agent and not_holder", i, o)
		}
	}

	// Also verify 32 refused outcomes each carrying a MaxOutcomeAgent-byte Agent
	// fit in one MaxSlotBytes slot.
	maxAgent := strings.Repeat("a", clientproto.MaxOutcomeAgent)
	maxList := make([]clientproto.Outcome, clientproto.MaxOutcomes)
	for i := range maxList {
		maxList[i] = clientproto.Outcome{
			Number: int64(i + 1),
			Status: clientproto.OutcomeRefused,
			Agent:  maxAgent,
			Reason: clientproto.ReasonNeedsFaceID,
		}
	}
	payload, err := clientproto.EncodeOutcomes(maxList)
	if err != nil {
		t.Fatal(err)
	}
	if got := sealedSlotSize(SlotName("outcomes", g.s.dev.ID()), g.client.dev.ID(), g.s.dev.ID(), at, 1, payload); got > clientproto.MaxSlotBytes {
		t.Fatalf("32 MaxOutcomeAgent outcomes sealed size = %d > MaxSlotBytes %d", got, clientproto.MaxSlotBytes)
	}
}

// TestD6RevokeWhileDispatchInFlightDoesNotRecreateClientState verifies D6:
// when a phone is unpaired or tombstoned while a dispatch is in flight (with
// the fake engine blocked on a channel after BeforeMint and BeforeStart have
// run), keep and finishCall do not recreate the phone's receipt, outcome,
// watermark, or create-map state after the revoke.
func TestD6RevokeWhileDispatchInFlightDoesNotRecreateClientState(t *testing.T) {
	g := newRig(t)
	se := g.useScriptedEngine(t)
	g.enroll(t, []string{"asks.list"}, clientproto.MethodCreate)
	if err := g.r.Step(context.Background()); err != nil {
		t.Fatal(err)
	}

	inFlight := make(chan struct{})
	release := make(chan struct{})
	se.createFn = func(ask appwire.CreateAsk) (appwire.Agent, error) {
		id := "01JINFLIGHT000000000000001"
		folder := filepath.Join(se.chatsDir, id)
		if err := ask.BeforeMint(filepath.Join(folder, "transcript.jsonl")); err != nil {
			return appwire.Agent{}, err
		}
		if err := os.MkdirAll(folder, 0700); err != nil {
			return appwire.Agent{}, err
		}
		a := appwire.Agent{ID: id}
		if err := ask.BeforeStart(a); err != nil {
			return appwire.Agent{}, err
		}
		close(inFlight)
		<-release
		return a, nil
	}

	g.queueCall(t, 1, clientproto.MethodCreate, g.s.dev.ID(), map[string]string{"client_id": "cid-inflight"}, false, clientproto.CreateLifetime)
	stepDone := make(chan error, 1)
	go func() {
		stepDone <- g.r.Step(context.Background())
	}()

	<-inFlight
	if err := g.r.Unpair(context.Background(), g.client.dev.ID()); err != nil {
		close(release)
		t.Fatalf("Unpair while dispatch in flight: %v", err)
	}
	close(release)
	if err := <-stepDone; err != nil {
		t.Fatalf("Step after in-flight Unpair: %v", err)
	}

	clientID := g.client.dev.ID()
	if len(g.r.Receipts()) != 0 {
		t.Fatalf("Receipts() after in-flight revoke = %+v, want empty", g.r.Receipts())
	}
	if len(g.r.Outcomes(clientID)) != 0 {
		t.Fatalf("Outcomes() after in-flight revoke = %+v, want empty", g.r.Outcomes(clientID))
	}
	if _, ok := g.r.createdChat(clientID, "cid-inflight"); ok {
		t.Fatal("create map entry recreated after in-flight revoke")
	}
	g.r.mu.Lock()
	_, hasWM := g.r.state.Watermarks[clientID]
	_, hasCreates := g.r.state.Creates[clientID]
	_, hasOutcomes := g.r.state.Outcomes[clientID]
	g.r.mu.Unlock()
	if hasWM || hasCreates || hasOutcomes {
		t.Fatalf("in-memory state recreated for revoked client: wm=%v creates=%v outcomes=%v", hasWM, hasCreates, hasOutcomes)
	}
	raw, err := os.ReadFile(StatePath(g.state))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), clientID) {
		t.Fatalf("runner.json contains revoked client %s after in-flight dispatch finished: %s", clientID, raw)
	}
}

// TestD6MidRoundLeaseDisplacedRevokedAndPausedBetweenListAndClaim verifies D6:
//  1. When ANOTHER computer takes the chat's lease between the initial directory
//     list and the claim round trip, the call is refused / not_holder and never
//     run. (This computer releasing it is not displacement any more: a released
//     chat is this computer's own and is taken back first, which
//     TestASendToAChatThisComputerReleasedTakesItFirstAndRuns pins.)
//  2. When the phone is revoked between the initial directory list and the
//     claim round trip, the call is never run, never acknowledged as done, and
//     produces no outcome.
//  3. When the phone is paused between the initial directory list and the claim
//     round trip, the call is never run, not acknowledged, and produces no
//     outcome (and runs once unpaused).
func TestD6MidRoundLeaseDisplacedRevokedAndPausedBetweenListAndClaim(t *testing.T) {
	t.Run("lease_displaced_between_list_and_claim", func(t *testing.T) {
		g := newRig(t)
		g.enroll(t, []string{"asks.list"}, clientproto.MethodSend)
		if err := g.r.Step(context.Background()); err != nil {
			t.Fatal(err)
		}

		displaced := false
		other := g.otherDevice(t)
		g.reopen(t, transport(func(req *http.Request) (*http.Response, error) {
			if !displaced && req.Method == "POST" && strings.HasSuffix(req.URL.Path, "/claim") {
				displaced = true
				displace(t, other, chat)
			}
			return http.DefaultTransport.RoundTrip(req)
		}), noopLog)
		se := g.useScriptedEngine(t)

		g.queueCall(t, 1, clientproto.MethodSend, g.s.dev.ID(), map[string]string{
			"id": chat, "text": "displaced-during-claim", "client_id": "c-disp",
		}, false, 0)

		if err := g.r.Step(context.Background()); err != nil {
			t.Fatal(err)
		}
		if len(se.sends) != 0 {
			t.Fatalf("call ran after lease was displaced during claim: %+v", se.sends)
		}
		if g.queueLen(t, g.client.dev.ID()) != 0 {
			t.Fatal("displaced not_holder call was not acknowledged")
		}
		outcomes := g.readOutcomesSlot(t, g.s.dev.ID())
		want := []clientproto.Outcome{{Number: 1, Status: clientproto.OutcomeRefused, Reason: clientproto.ReasonNotHolder}}
		if !slices.Equal(outcomes, want) {
			t.Fatalf("outcomes = %+v, want %+v", outcomes, want)
		}
	})

	t.Run("phone_revoked_between_list_and_claim", func(t *testing.T) {
		g := newRig(t)
		g.enroll(t, []string{"asks.list"}, clientproto.MethodSend)
		if err := g.r.Step(context.Background()); err != nil {
			t.Fatal(err)
		}

		revoked := false
		var deletes int
		g.reopen(t, transport(func(req *http.Request) (*http.Response, error) {
			if req.Method == "DELETE" && strings.Contains(req.URL.Path, "/queue/") {
				deletes++
			}
			if !revoked && req.Method == "POST" && strings.HasSuffix(req.URL.Path, "/claim") {
				revoked = true
				resp, err := http.DefaultTransport.RoundTrip(req)
				if rErr := g.dir.Revoke(context.Background(), g.client.dev.ID()); rErr != nil {
					t.Errorf("Revoke during claim: %v", rErr)
				}
				return resp, err
			}
			return http.DefaultTransport.RoundTrip(req)
		}), noopLog)
		se := g.useScriptedEngine(t)

		g.queueCall(t, 1, clientproto.MethodSend, g.s.dev.ID(), map[string]string{
			"id": chat, "text": "revoked-during-claim", "client_id": "c-rev",
		}, false, 0)

		if err := g.r.Step(context.Background()); err != nil {
			t.Fatalf("Step when phone revoked during claim: %v", err)
		}
		if len(se.sends) != 0 {
			t.Fatalf("call ran after phone was revoked during claim: %+v", se.sends)
		}
		if deletes != 0 {
			t.Fatalf("runner sent %d queue DELETEs for revoked phone, want 0", deletes)
		}
		if len(g.r.Outcomes(g.client.dev.ID())) != 0 || len(g.r.Receipts()) != 0 {
			t.Fatalf("revoked phone left outcomes=%+v or receipts=%+v", g.r.Outcomes(g.client.dev.ID()), g.r.Receipts())
		}
	})

	t.Run("phone_paused_between_list_and_claim", func(t *testing.T) {
		g := newRig(t)
		g.enroll(t, []string{"asks.list"}, clientproto.MethodSend)
		if err := g.r.Step(context.Background()); err != nil {
			t.Fatal(err)
		}

		paused := false
		var deletes int
		g.reopen(t, transport(func(req *http.Request) (*http.Response, error) {
			if req.Method == "DELETE" && strings.Contains(req.URL.Path, "/queue/") {
				deletes++
			}
			if !paused && req.Method == "POST" && strings.HasSuffix(req.URL.Path, "/claim") {
				paused = true
				if err := g.r.Pause(g.client.dev.ID(), true); err != nil {
					t.Errorf("Pause during claim: %v", err)
				}
			}
			return http.DefaultTransport.RoundTrip(req)
		}), noopLog)
		se := g.useScriptedEngine(t)

		g.queueCall(t, 1, clientproto.MethodSend, g.s.dev.ID(), map[string]string{
			"id": chat, "text": "paused-during-claim", "client_id": "c-pause",
		}, false, 0)

		if err := g.r.Step(context.Background()); err != nil {
			t.Fatalf("Step when phone paused during claim: %v", err)
		}
		if len(se.sends) != 0 {
			t.Fatalf("call ran after phone was paused during claim: %+v", se.sends)
		}
		if deletes != 0 || g.queueLen(t, g.client.dev.ID()) != 1 {
			t.Fatalf("paused phone call was acknowledged: deletes=%d queueLen=%d", deletes, g.queueLen(t, g.client.dev.ID()))
		}
		if len(g.r.Outcomes(g.client.dev.ID())) != 0 {
			t.Fatalf("paused phone recorded outcome: %+v", g.r.Outcomes(g.client.dev.ID()))
		}

		// Unpausing lets the claimed call finish on the next Step.
		if err := g.r.Pause(g.client.dev.ID(), false); err != nil {
			t.Fatal(err)
		}
		if err := g.r.Step(context.Background()); err != nil {
			t.Fatal(err)
		}
		if len(se.sends) != 1 || se.sends[0].Text != "paused-during-claim" {
			t.Fatalf("call did not run after unpause: %+v", se.sends)
		}
	})
}

func newSoloRig(t *testing.T, localChats func() ([]string, bool), leaseCreated func(context.Context, appwire.Agent) error) *rig {
	t.Helper()
	clock := directorytest.NewFakeClock()
	relayStore := t.TempDir()
	g := &rig{
		clock:      clock,
		relayStore: relayStore,
		e:          &engine{entryBytes: 10},
		state:      t.TempDir(),
		reqCh:      make(chan string, 256),
	}
	g.startRelay(t)

	home := t.TempDir()
	id, err := identity.Ensure(home)
	if err != nil {
		t.Fatal(err)
	}
	dev, err := identity.Device(home)
	if err != nil {
		t.Fatal(err)
	}
	s := signer{id, dev}
	dir := directory.NewHTTP(g.url, reqsign.SignFor(s, clock.Now), g.srv.Client())
	// A solo computer registers only its own device row when approving a phone
	// and never publishes any cell records to the directory (contract P13).
	if err := dir.PutDevice(context.Background(), dev.ID(), directory.Device{V: 1}); err != nil {
		t.Fatal(err)
	}
	seed, err := identity.NewSeed()
	if err != nil {
		t.Fatal(err)
	}
	c, err := seed.Join(identity.IssueCert(id, seed.Public(), clock.Now()))
	if err != nil {
		t.Fatal(err)
	}
	pub, priv, err := box.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	g.id = id
	g.s = s
	g.client = signer{id, c}
	g.pub = pub
	g.priv = priv
	g.dir = dir

	r, err := Open(Config{
		Home:         g.state,
		Relay:        g.url,
		Name:         "desk",
		Identity:     id.PublicKey(),
		Signer:       s,
		Directory:    dir,
		Engine:       g.e,
		LocalChats:   localChats,
		LeaseCreated: leaseCreated,
		Log:          noopLog,
		Logf:         g.logf,
		Now:          clock.Now,
		HTTP:         g.srv.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	g.r = r
	return g
}

// TestP13SoloRunnerWithEmptyDirectoryPublishesEntriesAndRunsHeldChat verifies
// that a solo-mode runner (LocalChats returning (["chat-a"], true)) with an
// empty directory listing of cells publishes agents.entries for chat-a, runs
// agents.send to chat-a and finishes done, and refuses agents.send to chat-z
// (not local) with not_holder without calling the engine (contract P13).
func TestP13SoloRunnerWithEmptyDirectoryPublishesEntriesAndRunsHeldChat(t *testing.T) {
	g := newSoloRig(t, func() ([]string, bool) {
		return []string{"chat-a"}, true
	}, func(context.Context, appwire.Agent) error {
		t.Fatal("LeaseCreated must not be called in solo mode")
		return nil
	})
	se := g.useScriptedEngine(t)
	g.enroll(t, []string{"agents.entries"}, clientproto.MethodSend)

	listing, err := g.dir.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(listing.Cells) != 0 {
		t.Fatalf("directory cells = %+v, want empty in solo mode", listing.Cells)
	}

	g.queueCall(t, 1, clientproto.MethodSend, g.s.dev.ID(), map[string]string{
		"id": "chat-a", "text": "hello solo chat-a", "client_id": "c-solo-1",
	}, false, 0)
	g.queueCall(t, 2, clientproto.MethodSend, g.s.dev.ID(), map[string]string{
		"id": "chat-z", "text": "must not run on non-local chat-z", "client_id": "c-solo-2",
	}, false, 0)

	if err := g.r.Step(context.Background()); err != nil {
		t.Fatal(err)
	}

	// Verify agents.entries was published for chat-a despite the empty cell listing.
	var entriesSlot []struct {
		Agent  string                `json:"agent"`
		Result appwire.EntriesResult `json:"result"`
	}
	if err := json.Unmarshal(g.slot(t, "agents.entries"), &entriesSlot); err != nil {
		t.Fatal(err)
	}
	if len(entriesSlot) != 1 || entriesSlot[0].Agent != "chat-a" || len(entriesSlot[0].Result.Entries) == 0 {
		t.Fatalf("agents.entries slot = %+v, want 1 non-empty envelope for chat-a", entriesSlot)
	}

	// Verify the engine ran chat-a once and was never called for chat-z.
	wantSends := []sentMsg{{ID: "chat-a", Text: "hello solo chat-a", ClientID: "c-solo-1"}}
	if !slices.Equal(se.sends, wantSends) {
		t.Fatalf("engine sends = %+v, want %+v", se.sends, wantSends)
	}

	// Verify outcomes: #1 done for chat-a, #2 refused / not_holder (empty Agent for non-held chat).
	outcomes := g.readOutcomesSlot(t, g.s.dev.ID())
	wantOutcomes := []clientproto.Outcome{
		{Number: 1, Status: clientproto.OutcomeDone, Agent: "chat-a"},
		{Number: 2, Status: clientproto.OutcomeRefused, Reason: clientproto.ReasonNotHolder},
	}
	if !slices.Equal(outcomes, wantOutcomes) {
		t.Fatalf("outcomes = %+v, want %+v", outcomes, wantOutcomes)
	}
}

// TestP13SoloCreateSkipsLeaseCreatedWhileNonSoloCallsItOnce verifies that
// creating a chat in solo mode creates the chat, finishes done, and never calls
// LeaseCreated, whereas with LocalChats returning (nil, false) the same create
// calls LeaseCreated once (contract P13).
func TestP13SoloCreateSkipsLeaseCreatedWhileNonSoloCallsItOnce(t *testing.T) {
	solo := true
	var leased []string
	var g *rig
	g = newSoloRig(t, func() ([]string, bool) {
		if solo {
			return []string{"chat-a"}, true
		}
		return nil, false
	}, func(ctx context.Context, a appwire.Agent) error {
		leased = append(leased, a.ID)
		_, err := g.dir.Create(ctx, a.ID, directory.CellInit{Head: strings.Repeat("1", 64), Class: "chat"})
		return err
	})
	se := g.useScriptedEngine(t)
	g.enroll(t, []string{"agents.list"}, clientproto.MethodCreate)

	// Solo create: makes the chat, finishes done, LeaseCreated is never called.
	g.queueCall(t, 1, clientproto.MethodCreate, g.s.dev.ID(), map[string]string{
		"client_id": "cid-solo-create",
	}, false, clientproto.CreateLifetime)
	if err := g.r.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(se.createdIDs) != 1 {
		t.Fatalf("solo creates = %d, want 1", len(se.createdIDs))
	}
	soloChatID := se.createdIDs[0]
	if len(leased) != 0 {
		t.Fatalf("LeaseCreated called in solo mode: %v, want none", leased)
	}
	outcomes := g.readOutcomesSlot(t, g.s.dev.ID())
	wantOutcomes := []clientproto.Outcome{
		{Number: 1, Status: clientproto.OutcomeDone, Agent: soloChatID},
	}
	if !slices.Equal(outcomes, wantOutcomes) {
		t.Fatalf("outcomes after solo create = %+v, want %+v", outcomes, wantOutcomes)
	}

	// Non-solo create (LocalChats returns (nil, false)): calls LeaseCreated once.
	solo = false
	g.queueCall(t, 2, clientproto.MethodCreate, g.s.dev.ID(), map[string]string{
		"client_id": "cid-dir-create",
	}, false, clientproto.CreateLifetime)
	if err := g.r.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(se.createdIDs) != 2 {
		t.Fatalf("total creates = %d, want 2", len(se.createdIDs))
	}
	dirChatID := se.createdIDs[1]
	if !slices.Equal(leased, []string{dirChatID}) {
		t.Fatalf("LeaseCreated calls = %v, want [%s]", leased, dirChatID)
	}
	outcomes = g.readOutcomesSlot(t, g.s.dev.ID())
	wantOutcomes = []clientproto.Outcome{
		{Number: 1, Status: clientproto.OutcomeDone, Agent: soloChatID},
		{Number: 2, Status: clientproto.OutcomeDone, Agent: dirChatID},
	}
	if !slices.Equal(outcomes, wantOutcomes) {
		t.Fatalf("outcomes after non-solo create = %+v, want %+v", outcomes, wantOutcomes)
	}
}

// TestP13SoloChatMadeByAPhoneIsReachableBeforeTheHostKnowsIt verifies that a chat a
// phone created on a solo computer takes the phone's first message even though the
// host's own listing does not list the chat yet (it has no words until that message),
// and that a chat the phone did not create and the host does not list is still refused.
func TestP13SoloChatMadeByAPhoneIsReachableBeforeTheHostKnowsIt(t *testing.T) {
	g := newSoloRig(t, func() ([]string, bool) { return nil, true }, nil)
	se := g.useScriptedEngine(t)
	g.enroll(t, []string{"agents.list"}, clientproto.MethodCreate, clientproto.MethodSend)

	g.queueCall(t, 1, clientproto.MethodCreate, g.s.dev.ID(), map[string]string{
		"client_id": "cid-first-chat",
	}, false, clientproto.CreateLifetime)
	if err := g.r.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(se.createdIDs) != 1 {
		t.Fatalf("creates = %d, want 1", len(se.createdIDs))
	}
	made := se.createdIDs[0]

	g.queueCall(t, 2, clientproto.MethodSend, g.s.dev.ID(), map[string]string{
		"id": made, "text": "the first message", "client_id": "m1",
	}, false, time.Hour)
	g.queueCall(t, 3, clientproto.MethodSend, g.s.dev.ID(), map[string]string{
		"id": "01JNOTMADEBYTHEPHONE000000", "text": "must not run", "client_id": "m2",
	}, false, time.Hour)
	if err := g.r.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(se.sends) != 1 || se.sends[0].ID != made {
		t.Fatalf("engine sends = %+v, want exactly the one to the chat the phone made", se.sends)
	}
	got := g.readOutcomesSlot(t, g.s.dev.ID())
	want := []clientproto.Outcome{
		{Number: 1, Status: clientproto.OutcomeDone, Agent: made},
		{Number: 2, Status: clientproto.OutcomeDone, Agent: made},
		{Number: 3, Status: clientproto.OutcomeRefused, Reason: clientproto.ReasonNotHolder},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("outcomes = %+v, want %+v", got, want)
	}
}

// TestP13SwitchingFromSoloToDirectoryBetweenRoundsWithoutRestart verifies that
// when LocalChats flips from ok to not ok between rounds on the same running
// runner (a computer that just paired a second computer), the runner uses the
// directory from the next round on: a chat held only locally becomes not_holder
// again and a directory-held chat runs (contract P13).
func TestP13SwitchingFromSoloToDirectoryBetweenRoundsWithoutRestart(t *testing.T) {
	solo := true
	g := newSoloRig(t, func() ([]string, bool) {
		if solo {
			return []string{"chat-b", "chat-a"}, true
		}
		return nil, false
	}, nil)
	se := g.useScriptedEngine(t)
	// Place chat-dir in the directory leased by this computer, while chat-a and
	// chat-b exist only locally.
	if _, err := g.dir.Create(context.Background(), "chat-dir", directory.CellInit{Head: strings.Repeat("1", 64), Class: "chat"}); err != nil {
		t.Fatal(err)
	}
	g.enroll(t, []string{"agents.entries"}, clientproto.MethodSend)

	// Round 1 (solo == true): publishes sorted local chats ["chat-a", "chat-b"]
	// and runs a send to chat-a.
	g.queueCall(t, 1, clientproto.MethodSend, g.s.dev.ID(), map[string]string{
		"id": "chat-a", "text": "round-1-local", "client_id": "c-r1",
	}, false, 0)
	if err := g.r.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	var round1Entries []struct {
		Agent string `json:"agent"`
	}
	if err := json.Unmarshal(g.slot(t, "agents.entries"), &round1Entries); err != nil {
		t.Fatal(err)
	}
	if len(round1Entries) != 2 || round1Entries[0].Agent != "chat-a" || round1Entries[1].Agent != "chat-b" {
		t.Fatalf("round 1 agents.entries = %+v, want [chat-a chat-b]", round1Entries)
	}
	if len(se.sends) != 1 || se.sends[0].ID != "chat-a" {
		t.Fatalf("round 1 sends = %+v, want [chat-a]", se.sends)
	}

	// Flip LocalChats from ok to not ok between rounds without restarting the runner.
	solo = false
	g.clock.Advance(clientproto.WatchingFloor)
	g.queueCall(t, 2, clientproto.MethodSend, g.s.dev.ID(), map[string]string{
		"id": "chat-a", "text": "round-2-local-must-refuse", "client_id": "c-r2-local",
	}, false, 0)
	g.queueCall(t, 3, clientproto.MethodSend, g.s.dev.ID(), map[string]string{
		"id": "chat-dir", "text": "round-2-dir-must-run", "client_id": "c-r2-dir",
	}, false, 0)

	if err := g.r.Step(context.Background()); err != nil {
		t.Fatal(err)
	}

	// In round 2, chat-a is refused / not_holder without calling the engine,
	// while chat-dir runs and finishes done, and agents.entries switches to chat-dir.
	wantSends := []sentMsg{
		{ID: "chat-a", Text: "round-1-local", ClientID: "c-r1"},
		{ID: "chat-dir", Text: "round-2-dir-must-run", ClientID: "c-r2-dir"},
	}
	if !slices.Equal(se.sends, wantSends) {
		t.Fatalf("sends across switch = %+v, want %+v", se.sends, wantSends)
	}
	outcomes := g.readOutcomesSlot(t, g.s.dev.ID())
	wantOutcomes := []clientproto.Outcome{
		{Number: 1, Status: clientproto.OutcomeDone, Agent: "chat-a"},
		{Number: 2, Status: clientproto.OutcomeRefused, Reason: clientproto.ReasonNotHolder},
		{Number: 3, Status: clientproto.OutcomeDone, Agent: "chat-dir"},
	}
	if !slices.Equal(outcomes, wantOutcomes) {
		t.Fatalf("outcomes across switch = %+v, want %+v", outcomes, wantOutcomes)
	}
	var round2Entries []struct {
		Agent string `json:"agent"`
	}
	if err := json.Unmarshal(g.slot(t, "agents.entries"), &round2Entries); err != nil {
		t.Fatal(err)
	}
	if len(round2Entries) != 1 || round2Entries[0].Agent != "chat-dir" {
		t.Fatalf("round 2 agents.entries = %+v, want [chat-dir]", round2Entries)
	}
}

// TestP13RecoverStartedAfterCrashInSoloModeSkipsLeaseCreated verifies that
// recoverStarted after a crash in solo mode finishes done with the created chat
// without calling LeaseCreated (contract P13).
func TestP13RecoverStartedAfterCrashInSoloModeSkipsLeaseCreated(t *testing.T) {
	leaseCalls := 0
	localChats := func() ([]string, bool) {
		return []string{"chat-a"}, true
	}
	leaseStub := func(context.Context, appwire.Agent) error {
		leaseCalls++
		return nil
	}
	g := newSoloRig(t, localChats, leaseStub)
	se := g.useScriptedEngine(t)
	chatsDir := se.chatsDir
	g.enroll(t, []string{"agents.list"}, clientproto.MethodCreate)
	if err := g.r.Step(context.Background()); err != nil {
		t.Fatal(err)
	}

	const cid = "cid-solo-recover-started"
	const createdChatID = "01JSOLORECOVERSTARTED00001"
	snapHome := t.TempDir()

	if err := g.r.Close(); err != nil {
		t.Fatal(err)
	}
	r1, err := Open(Config{
		Home:         g.state,
		Relay:        g.url,
		Name:         "desk",
		Identity:     g.id.PublicKey(),
		Signer:       g.s,
		Directory:    g.dir,
		Engine:       se,
		LocalChats:   localChats,
		LeaseCreated: leaseStub,
		Log:          noopLog,
		Logf:         g.logf,
		Now:          g.clock.Now,
		HTTP: &http.Client{Transport: transport(func(req *http.Request) (*http.Response, error) {
			if req.Method == "DELETE" && strings.Contains(req.URL.Path, "/queue/") {
				return nil, errors.New("crash before delete")
			}
			return http.DefaultTransport.RoundTrip(req)
		})},
	})
	if err != nil {
		t.Fatal(err)
	}
	g.r = r1

	totalCreates := 0
	se.createFn = func(ask appwire.CreateAsk) (appwire.Agent, error) {
		totalCreates++
		folder := filepath.Join(chatsDir, createdChatID)
		if err := ask.BeforeMint(filepath.Join(folder, "transcript.jsonl")); err != nil {
			return appwire.Agent{}, err
		}
		if err := os.MkdirAll(folder, 0700); err != nil {
			return appwire.Agent{}, err
		}
		for _, fn := range []func(string) string{StatePath, PinsPath, TombstonesPath} {
			if raw, err := os.ReadFile(fn(g.state)); err == nil {
				dst := fn(snapHome)
				_ = os.MkdirAll(filepath.Dir(dst), 0700)
				_ = os.WriteFile(dst, raw, 0600)
			}
		}
		return appwire.Agent{}, errors.New("crash after folder created in solo mode")
	}

	g.queueCall(t, 1, clientproto.MethodCreate, g.s.dev.ID(), map[string]string{
		"client_id": cid,
	}, false, clientproto.CreateLifetime)
	_ = g.r.Step(context.Background())
	if totalCreates != 1 {
		t.Fatalf("totalCreates in round 1 = %d, want 1", totalCreates)
	}
	if leaseCalls != 0 {
		t.Fatalf("leaseCalls in round 1 = %d, want 0", leaseCalls)
	}

	// Reopen from snapHome (where receipt #1 is at status "started" and the
	// folder exists on disk) in solo mode and run recovery Step.
	if err := g.r.Close(); err != nil {
		t.Fatal(err)
	}
	g.state = snapHome
	se2 := newScriptedEngine(g.e)
	se2.chatsDir = chatsDir
	se2.createFn = func(ask appwire.CreateAsk) (appwire.Agent, error) {
		totalCreates++
		return appwire.Agent{ID: "unexpected-second-create"}, nil
	}
	r2, err := Open(Config{
		Home:         g.state,
		Relay:        g.url,
		Name:         "desk",
		Identity:     g.id.PublicKey(),
		Signer:       g.s,
		Directory:    g.dir,
		Engine:       se2,
		LocalChats:   localChats,
		LeaseCreated: leaseStub,
		Log:          noopLog,
		Logf:         g.logf,
		Now:          g.clock.Now,
		HTTP:         g.srv.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r2.Close() })
	g.r = r2

	if recs := g.r.Receipts(); len(recs) != 1 || recs[0].Status != "started" {
		t.Fatalf("receipts on disk before recovery = %+v, want started", recs)
	}
	if err := g.r.Step(context.Background()); err != nil {
		t.Fatalf("recovery Step in solo mode: %v", err)
	}
	if totalCreates != 1 {
		t.Fatalf("totalCreates across crash and recovery = %d, want 1", totalCreates)
	}
	if leaseCalls != 0 {
		t.Fatalf("LeaseCreated calls during solo recoverStarted = %d, want 0", leaseCalls)
	}
	if g.queueLen(t, g.client.dev.ID()) != 0 {
		t.Fatal("recovered started receipt in solo mode was not acknowledged")
	}
	outcomes := g.readOutcomesSlot(t, g.s.dev.ID())
	wantOutcomes := []clientproto.Outcome{
		{Number: 1, Status: clientproto.OutcomeDone, Agent: createdChatID},
	}
	if !slices.Equal(outcomes, wantOutcomes) {
		t.Fatalf("outcomes after solo recoverStarted = %+v, want %+v", outcomes, wantOutcomes)
	}
}

// acquireHook runs before this computer's take of a chat's lease reaches the
// directory, so a test can have another computer win the race.
type acquireHook struct {
	directory.Client
	before func(id string)
}

func (h acquireHook) Acquire(ctx context.Context, id string, o directory.AcquireOpts) (directory.CellView, error) {
	if h.before != nil {
		h.before(id)
	}
	return h.Client.Acquire(ctx, id, o)
}

// TestASendToAChatThisComputerReleasedTakesItFirstAndRuns pins what a phone's
// call to a chat in this computer's home that nobody holds does: the chat was
// released by this computer, so it is listed, and a message to it takes the
// lease with the conditional take and then runs. The lease is held by this
// computer afterwards, and the take went through the expected holder and fence.
func TestASendToAChatThisComputerReleasedTakesItFirstAndRuns(t *testing.T) {
	g := newRig(t)
	g.enroll(t, []string{"agents.list"}, clientproto.MethodSend)
	ctx := context.Background()
	if err := g.dir.Release(ctx, chat, 1); err != nil {
		t.Fatal(err)
	}
	se := g.useScriptedEngine(t)
	if err := g.r.Step(ctx); err != nil {
		t.Fatal(err)
	}
	if v, err := g.dir.Cell(ctx, chat); err != nil || v.Cell.Lease.Expires != 0 || v.Cell.Lease.Device != g.s.dev.ID() {
		t.Fatalf("setup: the chat's lease is %+v (%v), want released with this computer as the last holder", v.Cell.Lease, err)
	}

	var takes []directory.AcquireOpts
	g.r.cfg.Directory = acquireRecorder{Client: g.dir, got: &takes}
	g.queueCall(t, 1, clientproto.MethodSend, g.s.dev.ID(), map[string]string{
		"id": chat, "text": "hello after a restart", "client_id": "c-released",
	}, false, 0)
	if err := g.r.Step(ctx); err != nil {
		t.Fatal(err)
	}
	if len(se.sends) != 1 || se.sends[0].Text != "hello after a restart" {
		t.Fatalf("sends = %+v, want the one message to run", se.sends)
	}
	v, err := g.dir.Cell(ctx, chat)
	if err != nil {
		t.Fatal(err)
	}
	if v.Cell.Lease.Device != g.s.dev.ID() || v.Cell.Lease.Expires <= v.Now || v.Cell.Lease.Fence != 2 {
		t.Fatalf("the lease after the send is %+v (now %d), want held by this computer with the fence up by one", v.Cell.Lease, v.Now)
	}
	if len(takes) != 1 || takes[0].Expect == nil || takes[0].Expect.Device != g.s.dev.ID() || takes[0].Expect.Fence != 1 || !takes[0].RequireConditional {
		t.Fatalf("takes = %+v, want one conditional take naming this computer and fence 1", takes)
	}
	want := []clientproto.Outcome{{Number: 1, Status: clientproto.OutcomeDone, Agent: chat}}
	if got := g.readOutcomesSlot(t, g.s.dev.ID()); !slices.Equal(got, want) {
		t.Fatalf("outcomes = %+v, want %+v", got, want)
	}
}

// acquireRecorder records every take this computer makes.
type acquireRecorder struct {
	directory.Client
	got *[]directory.AcquireOpts
}

func (h acquireRecorder) Acquire(ctx context.Context, id string, o directory.AcquireOpts) (directory.CellView, error) {
	*h.got = append(*h.got, o)
	return h.Client.Acquire(ctx, id, o)
}

// TestASendToAChatAnotherComputerTookInBetweenIsRefusedNotHolder pins the race the
// conditional take exists for: the chat was released by this computer when the
// call was read, and another computer took it before this computer's take
// arrived. The take loses, the message runs nowhere, and the phone is told
// not_holder, exactly as for a chat that had moved before the call was read.
func TestASendToAChatAnotherComputerTookInBetweenIsRefusedNotHolder(t *testing.T) {
	g := newRig(t)
	g.enroll(t, []string{"agents.list"}, clientproto.MethodSend)
	ctx := context.Background()
	other := g.otherDevice(t)
	if err := g.dir.Release(ctx, chat, 1); err != nil {
		t.Fatal(err)
	}
	se := g.useScriptedEngine(t)
	if err := g.r.Step(ctx); err != nil {
		t.Fatal(err)
	}

	raced := false
	g.r.cfg.Directory = acquireHook{Client: g.dir, before: func(id string) {
		if raced {
			return
		}
		raced = true
		if _, err := other.Acquire(ctx, id, directory.AcquireOpts{}); err != nil {
			t.Errorf("the other computer's take: %v", err)
		}
	}}
	g.queueCall(t, 1, clientproto.MethodSend, g.s.dev.ID(), map[string]string{
		"id": chat, "text": "must not run", "client_id": "c-raced",
	}, false, 0)
	if err := g.r.Step(ctx); err != nil {
		t.Fatal(err)
	}
	if !raced {
		t.Fatal("this computer never tried to take the released chat")
	}
	if len(se.sends) != 0 {
		t.Fatalf("a message ran on a chat another computer took: %+v", se.sends)
	}
	if v, err := g.dir.Cell(ctx, chat); err != nil || v.Cell.Lease.Device == g.s.dev.ID() {
		t.Fatalf("the lease after the lost race is %+v (%v), want the other computer's", v.Cell.Lease, err)
	}
	if g.queueLen(t, g.client.dev.ID()) != 0 {
		t.Fatal("the refused call was not acknowledged")
	}
	want := []clientproto.Outcome{{Number: 1, Status: clientproto.OutcomeRefused, Reason: clientproto.ReasonNotHolder}}
	if got := g.readOutcomesSlot(t, g.s.dev.ID()); !slices.Equal(got, want) {
		t.Fatalf("outcomes = %+v, want %+v", got, want)
	}
}
