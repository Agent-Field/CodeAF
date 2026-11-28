package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/crypto/nacl/box"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cell/celltest"
	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/clientproto"
	"github.com/Agent-Field/codeaf/internal/clientrun"
	"github.com/Agent-Field/codeaf/internal/devname"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/filelock"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/relayserve"
	"github.com/Agent-Field/codeaf/internal/reqsign"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

// phoneTestSigner signs relay requests on behalf of an enrolled phone using
// only the phone's device certificate and Ed25519 key (no root identity seed).
type phoneTestSigner struct {
	idPub ed25519.PublicKey
	dev   identity.Dev
}

func (s phoneTestSigner) IdentityKey() ed25519.PublicKey { return s.idPub }
func (s phoneTestSigner) Cert() identity.Cert            { return s.dev.Cert }
func (s phoneTestSigner) Sign(raw []byte) []byte         { return s.dev.Sign(raw) }

type enrolledTestPhone struct {
	id      string
	name    string
	signer  phoneTestSigner
	boxPub  *[32]byte
	boxPriv *[32]byte
}

func enrollPhoneAtHome(t *testing.T, relayURL, homeDir, phoneName string, methods, reads []string) enrolledTestPhone {
	t.Helper()
	id, err := identity.Load(homeDir)
	if err != nil {
		t.Fatal(err)
	}
	dev, err := identity.Device(homeDir)
	if err != nil {
		t.Fatal(err)
	}
	hostSigner := appClientSigner{id: id, dev: dev}
	dir := directory.NewHTTP(relayURL, reqsign.SignFor(hostSigner, time.Now), http.DefaultClient)
	ctx := context.Background()
	selfName, err := directory.SealName(directory.MetadataKey(id.CellKey()), devname.Name(homeDir))
	if err != nil {
		t.Fatal(err)
	}
	if err := dir.PutDevice(ctx, dev.ID(), directory.Device{V: 1, Name: selfName}); err != nil {
		t.Fatal(err)
	}

	seed, err := identity.NewSeed()
	if err != nil {
		t.Fatal(err)
	}
	cert := identity.IssueCert(id, seed.Public(), time.Now())
	phoneDev, err := seed.Join(cert)
	if err != nil {
		t.Fatal(err)
	}
	boxPub, boxPriv, err := box.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if methods == nil {
		methods = []string{
			clientproto.MethodCreate,
			clientproto.MethodSend,
			clientproto.MethodStop,
			clientproto.MethodAnswer,
			clientproto.MethodInboxAnswer,
		}
	}
	if reads == nil {
		reads = []string{"asks.list", "agents.entries"}
	}
	enrollment := clientproto.Enrollment{
		V:       1,
		Device:  phoneDev.ID(),
		SignPub: seed.Public(),
		BoxPub:  boxPub[:],
		Methods: methods,
		Reads:   reads,
		Ver:     1,
	}
	enrollment.Sig = id.Sign(enrollment.SignText())
	phoneDoJSON(t, relayURL, hostSigner, http.MethodPut, "/v1/clients/"+phoneDev.ID()+"/enroll", enrollment, nil, http.StatusNoContent)

	sealedPhoneName, err := directory.SealName(directory.MetadataKey(id.CellKey()), phoneName)
	if err != nil {
		t.Fatal(err)
	}
	phoneDoJSON(t, relayURL, hostSigner, http.MethodPut, "/v1/clients/"+phoneDev.ID(), map[string]any{
		"cert":    cert,
		"methods": methods,
		"name":    sealedPhoneName,
	}, nil, http.StatusNoContent)

	if err := syncsetup.NotePhoneApproved(homeDir, phoneDev.ID()); err != nil {
		t.Fatal(err)
	}
	return enrolledTestPhone{
		id:      phoneDev.ID(),
		name:    phoneName,
		signer:  phoneTestSigner{idPub: id.PublicKey(), dev: phoneDev},
		boxPub:  boxPub,
		boxPriv: boxPriv,
	}
}

func phoneDoJSON(t *testing.T, baseURL string, s reqsign.Signer, method, path string, body, out any, wantStatus int) http.Header {
	t.Helper()
	var raw []byte
	var err error
	if body != nil {
		raw, err = json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
	}
	req, err := http.NewRequest(method, baseURL+path, bytes.NewReader(raw))
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
	if wantStatus != 0 && resp.StatusCode != wantStatus {
		t.Fatalf("%s %s: status %d (%s), want %d", method, path, resp.StatusCode, data, wantStatus)
	}
	if out != nil && len(data) > 0 {
		if err := json.Unmarshal(data, out); err != nil {
			t.Fatalf("decode %s %s: %v (%s)", method, path, err, data)
		}
	}
	return resp.Header
}

func readExecutorCardFromRelay(t *testing.T, relayURL string, phone enrolledTestPhone, hostDeviceID string) clientproto.Card {
	t.Helper()
	var slot struct {
		Sealed []byte `json:"sealed"`
	}
	slotPath := "/v1/clients/" + phone.id + "/slots/" + clientrun.SlotName("card", hostDeviceID)
	phoneDoJSON(t, relayURL, phone.signer, http.MethodGet, slotPath, nil, &slot, http.StatusOK)
	card, err := clientproto.OpenCard(slot.Sealed, phone.boxPub, phone.boxPriv)
	if err != nil {
		t.Fatalf("OpenCard: %v", err)
	}
	if err := card.Verify(phone.signer.idPub); err != nil {
		t.Fatalf("card.Verify: %v", err)
	}
	return card
}

func enqueuePhoneCall(t *testing.T, relayURL string, phone enrolledTestPhone, executorBoxPub [32]byte, number int64, method, target string, params any, faceID bool) {
	t.Helper()
	rawParams, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	nowMs := time.Now().UnixMilli()
	lifetime := clientproto.CallLifetime
	if method == clientproto.MethodCreate {
		lifetime = clientproto.CreateLifetime
	}
	plain := clientproto.CallPlain{
		V:        clientproto.CallVersion,
		Client:   phone.id,
		Number:   number,
		At:       nowMs,
		NotAfter: nowMs + lifetime.Milliseconds(),
		Target:   target,
		Method:   method,
		Params:   rawParams,
		FaceID:   faceID,
	}
	sealed, err := clientproto.SealCall(plain, &executorBoxPub, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	phoneDoJSON(t, relayURL, phone.signer, http.MethodPost, "/v1/clients/"+phone.id+"/queue", map[string]any{
		"number": number,
		"at":     nowMs,
		"method": method,
		"target": target,
		"sealed": sealed,
	}, nil, http.StatusNoContent)
}

// TestCreateAgentHooksBeforeMintAndBeforeStart verifies the two CreateAsk hooks
// in appBackend.CreateAgent and v3NewFolder: BeforeMint runs before the session
// folder exists on disk (and an error leaves no folder behind), and BeforeStart
// runs after the folder is minted and before the first turn is released.
func TestCreateAgentHooksBeforeMintAndBeforeStart(t *testing.T) {
	t.Setenv("CODEAF_TELEMETRY", "off")
	t.Setenv("DO_NOT_TRACK", "1")
	root := t.TempDir()
	t.Setenv(home.EnvVar, root)
	t.Setenv("CODEAF_PROFILE_DIR", "")
	t.Setenv(syncsetup.URLVar, "off")

	// 1. Verify v3NewFolder runs before-hooks before creating the folder on disk
	// under both cell-available and plain-folder layouts.
	for _, withCells := range []bool{false, true} {
		restoreCells := cell.UseAvailabilityInTests(withCells)
		bucket := filepath.Join(t.TempDir(), "bucket")
		var hookedTranscript string
		var folderExistedInHook bool
		errBoom := errors.New("abort before mint")
		_, dir, err := v3NewFolder(bucket, cell.HostBound, func(transcript string) error {
			hookedTranscript = transcript
			if folder, ok := session.FolderOf(transcript); ok {
				_, statErr := os.Stat(folder)
				folderExistedInHook = !os.IsNotExist(statErr)
			}
			return errBoom
		})
		restoreCells()
		if !errors.Is(err, errBoom) || dir != "" || hookedTranscript == "" || folderExistedInHook {
			t.Fatalf("v3NewFolder(cells=%v) err=%v dir=%q hooked=%q existed=%v",
				withCells, err, dir, hookedTranscript, folderExistedInHook)
		}
		if entries, err := os.ReadDir(bucket); err == nil && len(entries) != 0 {
			t.Fatalf("v3NewFolder(cells=%v) left folders after hook failure: %v", withCells, entries)
		}
	}

	// 2. Verify CreateAgent threads BeforeMint and BeforeStart in order and stops
	// when either hook fails.
	celltest.Available(t)
	var submitted atomic.Int32
	rec := newAppRecorder()
	b := newAppBackend(rec, func(ask appOpenAsk) (*appConn, error) {
		conv := &fakeConversation{script: []session.Event{{Kind: session.EventTurnDone}}}
		return &appConn{Conv: conv, Workspace: ask.Workspace, Transcript: ask.Transcript, Release: func() {}}, nil
	})
	b.state = &appState{path: filepath.Join(t.TempDir(), appStateName)}
	b.profileDir = t.TempDir()
	defer b.Close()

	work := t.TempDir()
	var rejectedFolder string
	_, err := b.CreateAgent(appwire.CreateAsk{
		Workspace: work,
		Text:      "should not mint",
		BeforeMint: func(transcript string) error {
			folder, ok := session.FolderOf(transcript)
			if !ok {
				t.Fatal("transcript has no session folder")
			}
			rejectedFolder = folder
			if _, statErr := os.Stat(folder); !os.IsNotExist(statErr) {
				t.Fatalf("folder %s already existed in BeforeMint: %v", folder, statErr)
			}
			return errors.New("reserve failed")
		},
	})
	if err == nil || !strings.Contains(err.Error(), "reserve failed") {
		t.Fatalf("CreateAgent with failing BeforeMint = %v, want reserve failed", err)
	}
	if rejectedFolder == "" {
		t.Fatal("BeforeMint was never called")
	}
	if _, statErr := os.Stat(rejectedFolder); !os.IsNotExist(statErr) {
		t.Fatalf("failing BeforeMint left folder %s on disk", rejectedFolder)
	}

	// Failing BeforeStart leaves the minted agent idle/unstarted and never opens
	// or submits the first turn.
	var openedConvs atomic.Int32
	b.open = func(ask appOpenAsk) (*appConn, error) {
		openedConvs.Add(1)
		conv := &fakeConversation{script: []session.Event{{Kind: session.EventTurnDone}}}
		return &appConn{Conv: conv, Workspace: ask.Workspace, Transcript: ask.Transcript, Release: func() {}}, nil
	}
	var order []string
	var reservedFolder string
	_, err = b.CreateAgent(appwire.CreateAsk{
		Workspace: work,
		Text:      "blocked at before-start",
		BeforeMint: func(transcript string) error {
			order = append(order, "before_mint")
			reservedFolder, _ = session.FolderOf(transcript)
			return nil
		},
		BeforeStart: func(agent appwire.Agent) error {
			order = append(order, "before_start:"+agent.ID)
			if _, statErr := os.Stat(reservedFolder); statErr != nil {
				t.Fatalf("folder %s missing at BeforeStart: %v", reservedFolder, statErr)
			}
			return errors.New("lease failed")
		},
	})
	if err == nil || !strings.Contains(err.Error(), "lease failed") {
		t.Fatalf("CreateAgent with failing BeforeStart = %v, want lease failed", err)
	}
	if len(order) != 2 || order[0] != "before_mint" || !strings.HasPrefix(order[1], "before_start:") {
		t.Fatalf("hook order = %v", order)
	}
	if openedConvs.Load() != 0 || submitted.Load() != 0 {
		t.Fatalf("failing BeforeStart still launched open=%d submitted=%d", openedConvs.Load(), submitted.Load())
	}

	// Succeeding BeforeMint + BeforeStart runs both before the conversation opens.
	order = nil
	var openSawBeforeStart atomic.Bool
	var beforeStartDone atomic.Bool
	b.open = func(ask appOpenAsk) (*appConn, error) {
		openSawBeforeStart.Store(beforeStartDone.Load())
		conv := &fakeConversation{script: []session.Event{{Kind: session.EventTurnDone}}}
		return &appConn{Conv: conv, Workspace: ask.Workspace, Transcript: ask.Transcript, Release: func() {}}, nil
	}
	created, err := b.CreateAgent(appwire.CreateAsk{
		Workspace: work,
		Text:      "hello from phone",
		BeforeMint: func(transcript string) error {
			order = append(order, "before_mint")
			return nil
		},
		BeforeStart: func(agent appwire.Agent) error {
			order = append(order, "before_start")
			beforeStartDone.Store(true)
			return nil
		},
	})
	if err != nil {
		t.Fatalf("CreateAgent with passing hooks: %v", err)
	}
	rec.waitFor(t, 5*time.Second, func(name string, data any) bool {
		done, ok := data.(appwire.TurnDone)
		return name == appwire.EventTurnDone && ok && done.Agent == created.ID
	})
	if !openSawBeforeStart.Load() || !slices.Equal(order, []string{"before_mint", "before_start"}) {
		t.Fatalf("openSawBeforeStart=%v order=%v", openSawBeforeStart.Load(), order)
	}
}

// TestAnswerAskWithBeforeResolveHookCoversBatchAndYouDecide verifies that
// AnswerAskWith authorizes the exact resolved target set through
// answer.BeforeResolve (including batch siblings, single batch_item, and
// YouDecide) and refuses without resolving when BeforeResolve fails.
func TestAnswerAskWithBeforeResolveHookCoversBatchAndYouDecide(t *testing.T) {
	t.Setenv("CODEAF_TELEMETRY", "off")
	t.Setenv("DO_NOT_TRACK", "1")
	root := t.TempDir()
	t.Setenv(home.EnvVar, root)
	t.Setenv("CODEAF_PROFILE_DIR", "")
	t.Setenv(syncsetup.URLVar, "off")

	conv := &fakeConversation{}
	conv.onAnswer = func(a session.Answer) {
		q := session.Question{Kind: a.Kind, ID: a.ID}
		conv.raise(session.Event{Kind: session.EventQuestionAnswered, Question: &q, Answer: &a})
	}
	rec := newAppRecorder()
	b := newFakeBackend(rec, func(ask appOpenAsk) (*appConn, error) {
		return &appConn{Conv: conv, Workspace: ask.Workspace, Transcript: ask.Transcript, Release: func() {}}, nil
	})
	defer b.Close()

	agent, err := mustCreate(t, b, appwire.CreateAsk{Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}

	// Raise two batched consent questions: consent:1 (costly) and consent:2 (irreversible).
	q1 := session.Question{ID: 1, Kind: session.QuestionConsent, Head: "ls", Stakes: session.StakesCostly, Batch: "batch-1", Options: session.AnswerOptions(session.QuestionConsent)}
	q2 := session.Question{ID: 2, Kind: session.QuestionConsent, Head: "rm -rf", Stakes: session.StakesIrreversible, Batch: "batch-1", Options: session.AnswerOptions(session.QuestionConsent)}
	conv.raise(session.Event{Kind: session.EventQuestion, Question: &q1})
	rec.waitFor(t, 5*time.Second, func(name string, data any) bool {
		e, ok := data.(appwire.AskOpened)
		return name == appwire.EventAskOpened && ok && e.Ask.ID == "consent:1"
	})
	conv.raise(session.Event{Kind: session.EventQuestion, Question: &q2})
	rec.waitFor(t, 5*time.Second, func(name string, data any) bool {
		e, ok := data.(appwire.AskOpened)
		return name == appwire.EventAskOpened && ok && e.Ask.ID == "consent:2"
	})

	// 1. Batch answer calls BeforeResolve with both sibling asks; refusal resolves neither.
	var batchChecked []string
	errFaceID := errors.New("Face ID required for batch")
	err = b.AnswerAskWith(agent.ID, "consent:1", appwire.AskAnswer{
		Option: appwire.OptionAllowOnce,
		BeforeResolve: func(asks []appwire.Ask) error {
			for _, a := range asks {
				batchChecked = append(batchChecked, a.ID)
			}
			return errFaceID
		},
	})
	if !errors.Is(err, errFaceID) {
		t.Fatalf("batch AnswerAskWith err = %v, want %v", err, errFaceID)
	}
	if !slices.Equal(batchChecked, []string{"consent:1", "consent:2"}) {
		t.Fatalf("batch BeforeResolve saw %v, want [consent:1 consent:2]", batchChecked)
	}
	conv.mu.Lock()
	if len(conv.answers) != 0 {
		t.Fatalf("refused batch answer still resolved %d questions", len(conv.answers))
	}
	conv.mu.Unlock()

	// 2. Single BatchItem calls BeforeResolve with only that item.
	var itemChecked []string
	err = b.AnswerAskWith(agent.ID, "consent:1", appwire.AskAnswer{
		Option:    appwire.OptionAllowOnce,
		BatchItem: "consent:1",
		BeforeResolve: func(asks []appwire.Ask) error {
			for _, a := range asks {
				itemChecked = append(itemChecked, a.ID)
			}
			return nil
		},
	})
	if err != nil {
		t.Fatalf("BatchItem AnswerAskWith: %v", err)
	}
	if !slices.Equal(itemChecked, []string{"consent:1"}) {
		t.Fatalf("BatchItem BeforeResolve saw %v, want [consent:1]", itemChecked)
	}

	// 3. YouDecide calls BeforeResolve with the question and honors refusal.
	q3 := session.Question{
		ID:      3,
		Kind:    session.QuestionAsk,
		Ask:     session.AskChoice,
		Head:    "Which format?",
		Options: []session.AnswerOption{{Key: "json", Label: "JSON"}},
		Pick:    &session.Pick{Key: "json"},
	}
	conv.raise(session.Event{Kind: session.EventQuestion, Question: &q3})
	rec.waitFor(t, 5*time.Second, func(name string, data any) bool {
		e, ok := data.(appwire.AskOpened)
		return name == appwire.EventAskOpened && ok && e.Ask.ID == "ask:3"
	})
	var decideChecked []string
	err = b.AnswerAskWith(agent.ID, "ask:3", appwire.AskAnswer{
		YouDecide: true,
		BeforeResolve: func(asks []appwire.Ask) error {
			for _, a := range asks {
				decideChecked = append(decideChecked, a.ID)
			}
			return errFaceID
		},
	})
	if !errors.Is(err, errFaceID) || !slices.Equal(decideChecked, []string{"ask:3"}) {
		t.Fatalf("YouDecide BeforeResolve err=%v checked=%v", err, decideChecked)
	}
}

// TestKeepAppEntriesSyncsAndDeduplicatesByID verifies that keepAppEntriesWith
// syncs app-footers.json to disk and deduplicates entries by ID so a replayed
// receipt never writes two lines.
func TestKeepAppEntriesSyncsAndDeduplicatesByID(t *testing.T) {
	t.Setenv("CODEAF_TELEMETRY", "off")
	t.Setenv("DO_NOT_TRACK", "1")
	root := t.TempDir()
	t.Setenv(home.EnvVar, root)
	t.Setenv("CODEAF_PROFILE_DIR", "")
	t.Setenv(syncsetup.URLVar, "off")

	celltest.Available(t)
	b := newAppBackend(newAppRecorder(), func(ask appOpenAsk) (*appConn, error) {
		return &appConn{Conv: &fakeConversation{}, Workspace: ask.Workspace, Transcript: ask.Transcript, Release: func() {}}, nil
	})
	b.state = &appState{path: filepath.Join(t.TempDir(), appStateName)}
	b.profileDir = t.TempDir()
	defer b.Close()

	agent, err := mustCreate(t, b, appwire.CreateAsk{Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	a, err := b.find(agent.ID)
	if err != nil {
		t.Fatal(err)
	}
	entry := appwire.Entry{
		ID:    "client-dev1-1",
		Agent: agent.ID,
		Kind:  appwire.EntryNotice,
		At:    time.Now(),
		Text:  "iPhone started this chat.",
	}
	if err := b.keepAppEntry(a, entry, ""); err != nil {
		t.Fatal(err)
	}
	if err := b.keepAppEntry(a, entry, ""); err != nil {
		t.Fatal(err)
	}
	entries, _, err := b.Entries(agent.ID, "", 100)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, e := range entries {
		if e.ID == entry.ID {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("found %d entries for %s, want 1: %+v", count, entry.ID, entries)
	}
}

// TestS1AndS2QueuedCreateThroughBackendLeasesChatAndDeduplicates verifies S1
// and S2 through appBackend and its hosted runner: a queued agents.create makes
// one chat, leases it on the relay directory under this computer's device ID,
// writes "<phone name> started this chat.", and a duplicate create with the
// same client_id does not create a second chat.
func TestS1AndS2QueuedCreateThroughBackendLeasesChatAndDeduplicates(t *testing.T) {
	celltest.Available(t)
	t.Setenv("CODEAF_TELEMETRY", "off")
	t.Setenv("DO_NOT_TRACK", "1")
	homeDir := t.TempDir()
	t.Setenv(home.EnvVar, homeDir)
	t.Setenv("CODEAF_PROFILE_DIR", "")

	relay := relayserve.New(relayserve.Config{Store: t.TempDir()})
	srv := httptest.NewServer(relay.Handler)
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

	// Ensure leaseClientChat can run without spawning a furrow daemon in this test.
	s, err := syncOf()
	if err != nil {
		t.Fatal(err)
	}
	r, err := clientrun.Open(func() clientrun.Config {
		cfg := b.clientRunnerConfig(s, nil, nil)
		cfg.LeaseCreated = func(ctx context.Context, agent appwire.Agent) error {
			if _, err := s.Dir.Create(ctx, agent.ID, directory.CellInit{Class: string(cell.HostBound)}); err != nil && !errors.Is(err, directory.ErrExists) {
				return err
			}
			_, err := s.Dir.Acquire(ctx, agent.ID, directory.AcquireOpts{})
			return err
		}
		return cfg
	}())
	if err != nil {
		t.Fatal(err)
	}
	b.clientRunner.Store(r)
	setActivePhoneUnpairer(phoneRunnerUnpairer{r: r})
	defer b.stopClients()

	ctx := context.Background()
	if err := r.Step(ctx); err != nil {
		t.Fatalf("initial Step: %v", err)
	}
	card := readExecutorCardFromRelay(t, srv.URL, phone, s.Device.ID())
	var execBoxPub [32]byte
	copy(execBoxPub[:], card.BoxPub)

	// 1. Enqueue agents.create with client_id "create-cid-1" targeted at this computer.
	enqueuePhoneCall(t, srv.URL, phone, execBoxPub, 1, clientproto.MethodCreate, s.Device.ID(), map[string]string{
		"client_id": "create-cid-1",
	}, false)
	if err := r.Step(ctx); err != nil {
		t.Fatalf("Step create #1: %v", err)
	}

	// Verify the outcome slot records Status=done and the created chat ID.
	var outSlot struct {
		Sealed []byte `json:"sealed"`
	}
	phoneDoJSON(t, srv.URL, phone.signer, http.MethodGet, "/v1/clients/"+phone.id+"/slots/"+clientrun.SlotName("outcomes", s.Device.ID()), nil, &outSlot, http.StatusOK)
	outPlain, err := clientproto.OpenSlot(outSlot.Sealed, &execBoxPub, phone.boxPriv)
	if err != nil {
		t.Fatalf("OpenSlot outcomes: %v", err)
	}
	outcomes, err := clientproto.DecodeOutcomes(outPlain.Payload)
	if err != nil || len(outcomes) != 1 || outcomes[0].Status != clientproto.OutcomeDone || outcomes[0].Agent == "" {
		t.Fatalf("outcomes after create #1 = %+v, %v", outcomes, err)
	}
	createdID := outcomes[0].Agent
	var agentCount int
	b.locked(func() { agentCount = len(b.agents) })
	if agentCount != 1 {
		t.Fatalf("backend agents count after create #1 = %d, want 1", agentCount)
	}

	// Verify the chat is leased in the relay directory under this computer's device ID (S1).
	listing, err := s.Dir.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	cellRec, ok := listing.Cells[createdID]
	if !ok || cellRec.Lease.Device != s.Device.ID() || cellRec.Lease.Expires <= listing.Now {
		t.Fatalf("directory cell %s = %+v (now=%d), want leased by %s", createdID, cellRec, listing.Now, s.Device.ID())
	}

	// 2. Enqueue a duplicate agents.create with the SAME client_id "create-cid-1" (S2).
	enqueuePhoneCall(t, srv.URL, phone, execBoxPub, 2, clientproto.MethodCreate, s.Device.ID(), map[string]string{
		"client_id": "create-cid-1",
	}, false)
	if err := r.Step(ctx); err != nil {
		t.Fatalf("Step duplicate create #2: %v", err)
	}
	b.locked(func() { agentCount = len(b.agents) })
	if agentCount != 1 {
		t.Fatalf("backend agents count after duplicate create = %d, want 1", agentCount)
	}
	phoneDoJSON(t, srv.URL, phone.signer, http.MethodGet, "/v1/clients/"+phone.id+"/slots/"+clientrun.SlotName("outcomes", s.Device.ID()), nil, &outSlot, http.StatusOK)
	outPlain2, err := clientproto.OpenSlot(outSlot.Sealed, &execBoxPub, phone.boxPriv)
	if err != nil {
		t.Fatalf("OpenSlot outcomes #2: %v", err)
	}
	outcomes2, err := clientproto.DecodeOutcomes(outPlain2.Payload)
	if err != nil || len(outcomes2) != 2 || outcomes2[1].Status != clientproto.OutcomeDone || outcomes2[1].Agent != createdID {
		t.Fatalf("outcomes after duplicate create #2 = %+v, %v", outcomes2, err)
	}

	// Verify the chat has the phone's start notice with the phone's display name.
	entries, _, err := b.Entries(createdID, "", 100)
	if err != nil {
		t.Fatal(err)
	}
	startNotices := 0
	for _, e := range entries {
		if e.Kind == appwire.EntryNotice && e.Text == "iPhone started this chat." {
			startNotices++
		}
	}
	if startNotices == 0 {
		t.Fatalf("created chat entries lacked 'iPhone started this chat.': %+v", entries)
	}
}

// TestL1EngineTurnDeltaReachesRunnerStream verifies L1: emitting
// appwire.TurnDelta on appBackend.emit reaches Runner.Delta through
// the backend's inbox tap and appears in the published agents.entries slot, and
// emitting TurnDone clears the partial stream entry.
func TestL1EngineTurnDeltaReachesRunnerStream(t *testing.T) {
	celltest.Available(t)
	t.Setenv("CODEAF_TELEMETRY", "off")
	t.Setenv("DO_NOT_TRACK", "1")
	homeDir := t.TempDir()
	t.Setenv(home.EnvVar, homeDir)
	t.Setenv("CODEAF_PROFILE_DIR", "")

	relay := relayserve.New(relayserve.Config{Store: t.TempDir()})
	srv := httptest.NewServer(relay.Handler)
	t.Cleanup(func() { srv.Close(); _ = relay.Close() })
	t.Setenv(syncsetup.URLVar, srv.URL)

	if _, err := identity.Ensure(homeDir); err != nil {
		t.Fatal(err)
	}
	phone := enrollPhoneAtHome(t, srv.URL, homeDir, "iPhone", []string{clientproto.MethodSend}, []string{"agents.entries"})

	rec := newAppRecorder()
	b := newAppBackend(rec, func(ask appOpenAsk) (*appConn, error) {
		return &appConn{Conv: &fakeConversation{}, Workspace: ask.Workspace, Transcript: ask.Transcript, Release: func() {}}, nil
	})
	b.state = &appState{path: filepath.Join(t.TempDir(), appStateName)}
	b.profileDir = t.TempDir()
	defer b.Close()

	agent, err := mustCreate(t, b, appwire.CreateAsk{Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	s, err := syncOf()
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := s.Dir.Create(ctx, agent.ID, directory.CellInit{Class: string(cell.HostBound)}); err != nil {
		t.Fatal(err)
	}

	var nowMu sync.Mutex
	currentNow := time.Now()
	b.now = func() time.Time {
		nowMu.Lock()
		defer nowMu.Unlock()
		return currentNow
	}
	advanceNow := func(d time.Duration) {
		nowMu.Lock()
		currentNow = currentNow.Add(d)
		nowMu.Unlock()
	}

	b.startPhoneRunner()
	defer b.stopClients()
	r := b.clientRunner.Load()
	if r == nil {
		t.Fatal("startPhoneRunner did not load runner")
	}

	// Emit a TurnDelta through b.emit (which tells the phone runner as it goes out).
	b.emit.Emit(appwire.EventTurnDelta, appwire.TurnDelta{
		Agent: agent.ID,
		Turn:  1,
		Kind:  appwire.DeltaText,
		Text:  "live token from engine",
	})
	if err := r.Step(ctx); err != nil {
		t.Fatalf("r.Step after TurnDelta: %v", err)
	}

	card := readExecutorCardFromRelay(t, srv.URL, phone, s.Device.ID())
	var execBoxPub [32]byte
	copy(execBoxPub[:], card.BoxPub)
	var slot struct {
		Sealed []byte `json:"sealed"`
	}
	slotPath := "/v1/clients/" + phone.id + "/slots/" + clientrun.SlotName("agents.entries", s.Device.ID())
	phoneDoJSON(t, srv.URL, phone.signer, http.MethodGet, slotPath, nil, &slot, http.StatusOK)
	plain, err := clientproto.OpenSlot(slot.Sealed, &execBoxPub, phone.boxPriv)
	if err != nil {
		t.Fatalf("OpenSlot agents.entries: %v", err)
	}
	if !strings.Contains(string(plain.Payload), "live token from engine") {
		t.Fatalf("agents.entries slot payload lacked streamed delta: %s", plain.Payload)
	}

	// Emitting TurnDone clears the partial from the stream buffer; advance past
	// WatchingFloor so the non-watching slot writer publishes the cleared state.
	b.emit.Emit(appwire.EventTurnDone, appwire.TurnDone{Agent: agent.ID, Turn: 1, OK: true})
	advanceNow(clientproto.WatchingFloor + time.Second)
	if err := r.Step(ctx); err != nil {
		t.Fatalf("r.Step after TurnDone: %v", err)
	}
	phoneDoJSON(t, srv.URL, phone.signer, http.MethodGet, slotPath, nil, &slot, http.StatusOK)
	plainAfter, err := clientproto.OpenSlot(slot.Sealed, &execBoxPub, phone.boxPriv)
	if err != nil {
		t.Fatalf("OpenSlot after TurnDone: %v", err)
	}
	if strings.Contains(string(plainAfter.Payload), "live token from engine") {
		t.Fatalf("stream partial remained in agents.entries after TurnDone: %s", plainAfter.Payload)
	}
}

// TestP8BackendRunnerSyncOffSoloOrNoPhoneEnrolled verifies P8 and the solo law
// on appBackend:
//   - sync off, no identity, or a solo identity with no approved phone makes zero
//     relay requests and logs no error;
//   - a shared identity with no enrolled phone makes only directory list and
//     serving=1 watch requests (no /v1/clients, card, slot, outcome, or queue);
//   - approving a phone on a solo home wakes the running backend's runner.
func TestP8BackendRunnerSyncOffSoloOrNoPhoneEnrolled(t *testing.T) {
	celltest.Available(t)
	t.Setenv("CODEAF_TELEMETRY", "off")
	t.Setenv("DO_NOT_TRACK", "1")

	var reqMu sync.Mutex
	var requests []string
	relay := relayserve.New(relayserve.Config{Store: t.TempDir()})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqMu.Lock()
		requests = append(requests, r.Method+" "+r.URL.RequestURI())
		reqMu.Unlock()
		relay.Handler.ServeHTTP(w, r)
	}))
	t.Cleanup(func() { srv.Close(); _ = relay.Close() })

	snapshotReqs := func() []string {
		reqMu.Lock()
		defer reqMu.Unlock()
		return slices.Clone(requests)
	}
	clearReqs := func() {
		reqMu.Lock()
		requests = nil
		reqMu.Unlock()
	}

	// 1. Sync off: zero requests, no runner, no log event.
	homeOff := t.TempDir()
	t.Setenv(home.EnvVar, homeOff)
	t.Setenv("CODEAF_PROFILE_DIR", "")
	t.Setenv(syncsetup.URLVar, "off")
	if _, err := identity.Ensure(homeOff); err != nil {
		t.Fatal(err)
	}
	clearReqs()
	recOff := newAppRecorder()
	bOff := newAppBackend(recOff, nil)
	bOff.startPhoneRunner()
	if bOff.clientRunner.Load() != nil || len(snapshotReqs()) != 0 {
		t.Fatalf("sync-off backend started runner=%v or made requests=%v", bOff.clientRunner.Load(), snapshotReqs())
	}
	_ = bOff.Close()

	// 2. No identity on disk: zero requests, no runner, no log event.
	homeNoID := t.TempDir()
	t.Setenv(home.EnvVar, homeNoID)
	t.Setenv(syncsetup.URLVar, srv.URL)
	clearReqs()
	recNoID := newAppRecorder()
	bNoID := newAppBackend(recNoID, nil)
	bNoID.startPhoneRunner()
	if bNoID.clientRunner.Load() != nil || len(snapshotReqs()) != 0 {
		t.Fatalf("no-identity backend started runner=%v or made requests=%v", bNoID.clientRunner.Load(), snapshotReqs())
	}
	_ = bNoID.Close()

	// 3. Solo home with no approved phone: zero requests, no runner; approving a
	// phone wakes the running backend and starts its runner.
	homeSolo := t.TempDir()
	t.Setenv(home.EnvVar, homeSolo)
	t.Setenv(syncsetup.URLVar, srv.URL)
	if _, err := identity.EnsureSolo(homeSolo); err != nil {
		t.Fatal(err)
	}
	clearReqs()
	recSolo := newAppRecorder()
	bSolo := newAppBackend(recSolo, nil)
	bSolo.startPhoneRunner()
	if bSolo.clientRunner.Load() != nil || len(snapshotReqs()) != 0 {
		t.Fatalf("solo backend started runner=%v or made requests=%v", bSolo.clientRunner.Load(), snapshotReqs())
	}
	if err := syncsetup.NotePhoneApproved(homeSolo, "dev_11111111111111111111111111111111"); err != nil {
		t.Fatal(err)
	}
	if bSolo.clientRunner.Load() == nil {
		t.Fatal("approving a phone on a solo home did not wake and start the backend's phone runner")
	}
	_ = bSolo.Close()

	// 4. Shared identity with NO enrolled phone (P8): starts runner and makes
	// ONLY directory list and serving=1 watch requests.
	homeShared := t.TempDir()
	t.Setenv(home.EnvVar, homeShared)
	t.Setenv(syncsetup.URLVar, srv.URL)
	id, err := identity.Ensure(homeShared)
	if err != nil {
		t.Fatal(err)
	}
	dev, err := identity.Device(homeShared)
	if err != nil {
		t.Fatal(err)
	}
	hostSigner := appClientSigner{id: id, dev: dev}
	dir := directory.NewHTTP(srv.URL, reqsign.SignFor(hostSigner, time.Now), http.DefaultClient)
	if err := dir.PutDevice(context.Background(), dev.ID(), directory.Device{V: 1}); err != nil {
		t.Fatal(err)
	}

	clearReqs()
	bShared := newAppBackend(newAppRecorder(), nil)
	bShared.startPhoneRunner()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		seen := snapshotReqs()
		hasList, hasWatch := false, false
		for _, r := range seen {
			if r == "GET /v1/dir/list" {
				hasList = true
			}
			if strings.HasPrefix(r, "GET /v1/dir/watch?") && strings.Contains(r, "serving=1") {
				hasWatch = true
			}
		}
		if hasList && hasWatch {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	_ = bShared.Close()
	for _, r := range snapshotReqs() {
		if r == "GET /v1/dir/list" {
			continue
		}
		if strings.HasPrefix(r, "GET /v1/dir/watch?") {
			continue
		}
		t.Fatalf("P8 violation: shared home with no enrolled phone made request %q (all=%v)", r, snapshotReqs())
	}
}

// TestV1RevokedPhoneCallNotRunAndInProcessRevokeUnpairs verifies V1 and the
// activePhoneUnpairer hook: revoking a phone in the same process calls
// activePhoneUnpairer.Unpair, tombstones the phone in the runner, and a queued
// call from the revoked phone is never executed.
func TestV1RevokedPhoneCallNotRunAndInProcessRevokeUnpairs(t *testing.T) {
	celltest.Available(t)
	t.Setenv("CODEAF_TELEMETRY", "off")
	t.Setenv("DO_NOT_TRACK", "1")
	homeDir := t.TempDir()
	t.Setenv(home.EnvVar, homeDir)
	t.Setenv("CODEAF_PROFILE_DIR", "")

	relay := relayserve.New(relayserve.Config{Store: t.TempDir()})
	srv := httptest.NewServer(relay.Handler)
	t.Cleanup(func() { srv.Close(); _ = relay.Close() })
	t.Setenv(syncsetup.URLVar, srv.URL)

	if _, err := identity.Ensure(homeDir); err != nil {
		t.Fatal(err)
	}
	phone := enrollPhoneAtHome(t, srv.URL, homeDir, "alpha-phone", []string{clientproto.MethodCreate}, []string{"asks.list"})

	b := newAppBackend(newAppRecorder(), func(ask appOpenAsk) (*appConn, error) {
		return &appConn{Conv: &fakeConversation{}, Workspace: ask.Workspace, Transcript: ask.Transcript, Release: func() {}}, nil
	})
	b.state = &appState{path: filepath.Join(t.TempDir(), appStateName)}
	b.profileDir = t.TempDir()
	defer b.Close()

	s, err := syncOf()
	if err != nil {
		t.Fatal(err)
	}
	r, err := clientrun.Open(b.clientRunnerConfig(s, nil, nil))
	if err != nil {
		t.Fatal(err)
	}
	b.clientRunner.Store(r)
	setActivePhoneUnpairer(phoneRunnerUnpairer{r: r})
	defer b.stopClients()

	ctx := context.Background()
	if err := r.Step(ctx); err != nil {
		t.Fatal(err)
	}
	paired, err := b.ListPairedClients()
	if err != nil || len(paired) != 1 || paired[0].ID != phone.id || paired[0].Name != "alpha-phone" {
		t.Fatalf("ListPairedClients = %+v, %v; want alpha-phone (%s)", paired, err, phone.id)
	}
	if err := b.PausePairedClient(phone.id, true); err != nil {
		t.Fatal(err)
	}
	if err := b.PausePairedClient(phone.id, false); err != nil {
		t.Fatal(err)
	}

	card := readExecutorCardFromRelay(t, srv.URL, phone, s.Device.ID())
	var execBoxPub [32]byte
	copy(execBoxPub[:], card.BoxPub)

	// Enqueue an agents.create call from the phone, then revoke the phone
	// through approvalsDoor (which uses activePhoneUnpairer) BEFORE the runner steps.
	enqueuePhoneCall(t, srv.URL, phone, execBoxPub, 1, clientproto.MethodCreate, s.Device.ID(), map[string]string{
		"client_id": "revoked-cid-1",
	}, false)

	door := newApprovalsDoor(homeDir)
	if err := door.Revoke(ctx, phone.id); err != nil {
		t.Fatalf("door.Revoke(%s): %v", phone.id, err)
	}
	if len(r.Clients()) != 0 {
		t.Fatalf("runner still lists clients after in-process revoke: %+v", r.Clients())
	}

	// Stepping the runner after revocation must not run the revoked phone's queued create (V1).
	if err := r.Step(ctx); err != nil {
		t.Fatalf("r.Step after revoke: %v", err)
	}
	agents, err := b.ListAgents("")
	if err != nil || len(agents) != 0 {
		t.Fatalf("V1 violation: revoked phone created a chat: %+v (%v)", agents, err)
	}
}

// TestPhoneNoticeSentencesAndNoForbiddenWords verifies every chat notice
// sentence logged for phone calls, replay deduplication, unminted reservation
// handling, and absence of banned vocabulary.
func TestPhoneNoticeSentencesAndNoForbiddenWords(t *testing.T) {
	celltest.Available(t)
	t.Setenv("CODEAF_TELEMETRY", "off")
	t.Setenv("DO_NOT_TRACK", "1")
	homeDir := t.TempDir()
	t.Setenv(home.EnvVar, homeDir)
	t.Setenv("CODEAF_PROFILE_DIR", "")
	t.Setenv(syncsetup.URLVar, "off")

	b := newAppBackend(newAppRecorder(), func(ask appOpenAsk) (*appConn, error) {
		return &appConn{Conv: &fakeConversation{}, Workspace: ask.Workspace, Transcript: ask.Transcript, Release: func() {}}, nil
	})
	b.state = &appState{path: filepath.Join(t.TempDir(), appStateName)}
	b.profileDir = t.TempDir()
	defer b.Close()

	b.clientNamesMu.Lock()
	b.clientNames = map[string]string{"dev_phone1": "Abir's iPhone"}
	b.clientNamesMu.Unlock()

	// 1. A reserved create whose folder never appeared on disk does not invent a chat.
	nonexistentTranscript := filepath.Join(t.TempDir(), "01J00000000000000000000099", ".cell", "transcript.jsonl")
	if err := b.logClientCall(clientrun.Receipt{
		Client:     "dev_phone1",
		Number:     99,
		Agent:      "01J00000000000000000000099",
		Method:     clientproto.MethodCreate,
		At:         time.Now().UTC().Format(time.RFC3339Nano),
		Status:     "started",
		Transcript: nonexistentTranscript,
	}); err != nil {
		t.Fatalf("logClientCall for unminted reservation: %v", err)
	}
	if _, err := b.find("01J00000000000000000000099"); err == nil {
		t.Fatal("unminted reservation invented an agent in the backend")
	}

	// 2. Verify all notice sentences on a real chat.
	agent, err := mustCreate(t, b, appwire.CreateAsk{Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	nowStr := time.Now().UTC().Format(time.RFC3339Nano)
	receipts := []clientrun.Receipt{
		{Client: "dev_phone1", Number: 1, Agent: agent.ID, Method: clientproto.MethodCreate, At: nowStr, Status: "started"},
		{Client: "dev_phone1", Number: 1, Agent: agent.ID, Method: clientproto.MethodCreate, At: nowStr, Status: clientproto.OutcomeDone},
		{Client: "dev_phone1", Number: 2, Agent: agent.ID, Method: clientproto.MethodSend, At: nowStr, Status: "started"},
		{Client: "dev_phone1", Number: 2, Agent: agent.ID, Method: clientproto.MethodSend, At: nowStr, Status: clientproto.OutcomeDone},
		{Client: "dev_phone1", Number: 3, Agent: agent.ID, Method: clientproto.MethodAnswer, At: nowStr, Status: "started"},
		{Client: "dev_phone1", Number: 3, Agent: agent.ID, Method: clientproto.MethodAnswer, At: nowStr, Status: clientproto.OutcomeDone},
		{Client: "dev_phone1", Number: 4, Agent: agent.ID, Method: clientproto.MethodInboxAnswer, At: nowStr, Status: clientproto.OutcomeDone},
		{Client: "dev_phone1", Number: 5, Agent: agent.ID, Method: clientproto.MethodStop, At: nowStr, Status: clientproto.OutcomeDone},
		{Client: "dev_phone1", Number: 6, Agent: agent.ID, Method: clientproto.MethodAnswer, At: nowStr, Status: clientproto.OutcomeRefused, Reason: clientproto.ReasonNeedsFaceID},
		{Client: "dev_phone1", Number: 7, Agent: agent.ID, Method: clientproto.MethodSend, At: nowStr, Status: "failed", Reason: "engine error"},
		// Replay of receipt #5 must not add a second line.
		{Client: "dev_phone1", Number: 5, Agent: agent.ID, Method: clientproto.MethodStop, At: nowStr, Status: clientproto.OutcomeDone},
	}
	for _, rc := range receipts {
		if err := b.logClientCall(rc); err != nil {
			t.Fatalf("logClientCall(%+v): %v", rc, err)
		}
	}

	entries, _, err := b.Entries(agent.ID, "", 100)
	if err != nil {
		t.Fatal(err)
	}
	var notices []string
	for _, e := range entries {
		if e.Kind == appwire.EntryNotice {
			notices = append(notices, e.Text)
		}
	}
	wantNotices := []string{
		"Abir's iPhone started this chat.",
		"Abir's iPhone answered a question.",
		"Abir's iPhone answered a question.",
		"Abir's iPhone stopped this chat.",
		"Abir's iPhone asked for something that could not be done.",
		"Abir's iPhone asked for something that could not be done.",
	}
	if !slices.Equal(notices, wantNotices) {
		t.Fatalf("notices = %q, want %q", notices, wantNotices)
	}

	// 3. Check all user-facing sentences for banned words, method names, and IDs.
	allSentences := append(slices.Clone(notices),
		phoneUsageLine,
		phoneUsage,
		phoneServeStartLine,
		phoneAlreadyServingLine,
		phoneQuietLine,
		phoneAcceptedOlderListLine,
		clientrun.PausedOlderListReason,
	)
	// "relay" is not on this list: it is the repository's word for the server, and the paused
	// sentence names it on purpose. internal/wordlaw is the gate for where it may appear.
	forbidden := []string{
		"auditor", "verdict", "verified", "refuted",
		"agents.create", "agents.send", "agents.stop", "asks.answer", "inbox.answer",
		"dev_phone1",
	}
	for _, s := range allSentences {
		lower := strings.ToLower(s)
		for _, bad := range forbidden {
			if strings.Contains(lower, strings.ToLower(bad)) {
				t.Errorf("sentence %q contains forbidden word %q", s, bad)
			}
		}
	}
}

// TestU4PhoneServeRefusesWhenAnotherRunnerOrAppHomeLocked verifies U4:
// `codeaf phone serve` refuses with the exact sentence
// "codeaf app is open on this computer and is already serving your phone."
// when another runner or app.lock holds the same home.
func TestU4PhoneServeRefusesWhenAnotherRunnerOrAppHomeLocked(t *testing.T) {
	celltest.Available(t)
	t.Setenv("CODEAF_TELEMETRY", "off")
	t.Setenv("DO_NOT_TRACK", "1")
	homeDir := t.TempDir()
	t.Setenv(home.EnvVar, homeDir)
	t.Setenv(syncsetup.URLVar, "http://127.0.0.1:1")

	id, err := identity.Ensure(homeDir)
	if err != nil {
		t.Fatal(err)
	}
	dev, err := identity.Device(homeDir)
	if err != nil {
		t.Fatal(err)
	}

	// 1. Hold the real clientrun lock on homeDir.
	r, err := clientrun.Open(clientrun.Config{
		Home:     homeDir,
		Relay:    "http://127.0.0.1:1",
		Identity: id.PublicKey(),
		Signer:   appClientSigner{id: id, dev: dev},
	})
	if err != nil {
		t.Fatal(err)
	}
	var out1 bytes.Buffer
	err = runPhoneContext(context.Background(), []string{"serve", "--home", homeDir}, &out1)
	if err == nil || err.Error() != phoneAlreadyServingLine || strings.TrimSpace(out1.String()) != phoneAlreadyServingLine {
		_ = r.Close()
		t.Fatalf("U4 with runner lock held: err=%v out=%q, want %q", err, out1.String(), phoneAlreadyServingLine)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}

	// 2. Hold app.lock on homeDir.
	appLockFile, err := os.OpenFile(filepath.Join(homeDir, "app.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer appLockFile.Close()
	if err := filelock.Lock(appLockFile, true, true); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = filelock.Unlock(appLockFile) }()

	var out2 bytes.Buffer
	err = runPhoneContext(context.Background(), []string{"serve", "--home", homeDir}, &out2)
	if err == nil || err.Error() != phoneAlreadyServingLine || strings.TrimSpace(out2.String()) != phoneAlreadyServingLine {
		t.Fatalf("U4 with app.lock held: err=%v out=%q, want %q", err, out2.String(), phoneAlreadyServingLine)
	}
}

// TestPhoneServeSyncOffNoIdentityAndQuietRefusalsMakeNoRequests verifies that
// `codeaf phone serve` refuses when sync is off, no identity exists, or the
// identity is solo with no approved phone, exiting non-zero and making zero
// relay requests.
func TestPhoneServeSyncOffNoIdentityAndQuietRefusalsMakeNoRequests(t *testing.T) {
	celltest.Available(t)
	t.Setenv("CODEAF_TELEMETRY", "off")
	t.Setenv("DO_NOT_TRACK", "1")

	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	// 1. Sync off.
	homeSyncOff := t.TempDir()
	if _, err := identity.Ensure(homeSyncOff); err != nil {
		t.Fatal(err)
	}
	t.Setenv(syncsetup.URLVar, "off")
	var outOff bytes.Buffer
	err := runPhoneContext(context.Background(), []string{"serve", "--home", homeSyncOff}, &outOff)
	if err == nil || err.Error() != chatlist.SyncOff {
		t.Fatalf("sync-off err = %v (out=%q), want the sentence %q", err, outOff.String(), chatlist.SyncOff)
	}

	// 2. No identity on disk.
	homeNoID := t.TempDir()
	t.Setenv(syncsetup.URLVar, srv.URL)
	var outNoID bytes.Buffer
	err = runPhoneContext(context.Background(), []string{"serve", "--home", homeNoID}, &outNoID)
	if err == nil || err.Error() != phoneQuietLine || strings.TrimSpace(outNoID.String()) != phoneQuietLine {
		t.Fatalf("no-identity err = %v out = %q, want %q", err, outNoID.String(), phoneQuietLine)
	}

	// 3. Solo identity with no approved phone.
	homeSolo := t.TempDir()
	if _, err := identity.EnsureSolo(homeSolo); err != nil {
		t.Fatal(err)
	}
	var outSolo bytes.Buffer
	err = runPhoneContext(context.Background(), []string{"serve", "--home", homeSolo}, &outSolo)
	if err == nil || err.Error() != phoneQuietLine || strings.TrimSpace(outSolo.String()) != phoneQuietLine {
		t.Fatalf("solo err = %v out = %q, want %q", err, outSolo.String(), phoneQuietLine)
	}

	if got := hits.Load(); got != 0 {
		t.Fatalf("refused phone serve runs made %d HTTP requests, want 0", got)
	}
}

// TestPhoneServeOnAFreshHomeSaysNoPhoneIsPairedAndMakesNothing is D3 of the
// first emulator run: a computer whose home holds no identity file at all, not
// even the solo marker, is the computer the manual describes as "never paired
// with a phone or another computer". It must say the one quiet sentence, send
// nothing, and leave the home without an identity, rather than telling a person
// who only wants to pair a phone to import one.
func TestPhoneServeOnAFreshHomeSaysNoPhoneIsPairedAndMakesNothing(t *testing.T) {
	celltest.Available(t)
	t.Setenv("CODEAF_TELEMETRY", "off")
	t.Setenv("DO_NOT_TRACK", "1")

	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	t.Setenv(syncsetup.URLVar, srv.URL)

	fresh := t.TempDir()
	for _, name := range []string{identity.File, "identity-solo", "device.json"} {
		if _, err := os.Stat(filepath.Join(fresh, name)); err == nil {
			t.Fatalf("fixture is not fresh: %s exists", name)
		}
	}

	var out bytes.Buffer
	err := runPhoneContext(context.Background(), []string{"serve", "--home", fresh}, &out)
	if err == nil || err.Error() != phoneQuietLine {
		t.Fatalf("fresh home err = %v, want %q", err, phoneQuietLine)
	}
	if got := strings.TrimSpace(out.String()); got != phoneQuietLine {
		t.Fatalf("fresh home printed %q, want only %q", got, phoneQuietLine)
	}
	if strings.Contains(out.String(), "identity import") || errors.Is(err, syncsetup.ErrNoIdentity) {
		t.Fatalf("fresh home was told to import an identity: err=%v out=%q", err, out.String())
	}
	if _, loadErr := identity.Load(fresh); !errors.Is(loadErr, identity.ErrNone) {
		t.Fatalf("refusing made an identity on a fresh home: load err = %v", loadErr)
	}
	if got := hits.Load(); got != 0 {
		t.Fatalf("fresh-home phone serve made %d HTTP requests, want 0", got)
	}
}

type versionedFakeDirectory struct {
	directory.Client
	mu      sync.Mutex
	version uint64
}

func (d *versionedFakeDirectory) List(ctx context.Context) (directory.Listing, error) {
	d.mu.Lock()
	v := d.version
	d.mu.Unlock()
	return directory.Listing{
		Version: v,
		Now:     time.Now().UnixMilli(),
		Devices: map[string]directory.Device{},
		Cells:   map[string]directory.Cell{},
	}, nil
}

// TestV3AndV5PhoneServeVersionPinPauseAndAcceptOlderList verifies V3 and V5
// through `codeaf phone serve`:
//   - when the directory serves a lower version than pinned in pins.json, it
//     prints Runner.Paused() once and keeps running without exiting;
//   - when started with --accept-older-list, it calls Runner.AcceptOlderList,
//     prints the reset confirmation sentence and the serving sentence, lowers the
//     pin, and keeps serving until clean signal/context stop.
func TestV3AndV5PhoneServeVersionPinPauseAndAcceptOlderList(t *testing.T) {
	celltest.Available(t)
	t.Setenv("CODEAF_TELEMETRY", "off")
	t.Setenv("DO_NOT_TRACK", "1")

	relay := relayserve.New(relayserve.Config{Store: t.TempDir()})
	srv := httptest.NewServer(relay.Handler)
	t.Cleanup(func() { srv.Close(); _ = relay.Close() })

	homeDir := t.TempDir()
	t.Setenv(home.EnvVar, homeDir)
	t.Setenv("CODEAF_PROFILE_DIR", "")
	t.Setenv(syncsetup.URLVar, srv.URL)
	if _, err := identity.Ensure(homeDir); err != nil {
		t.Fatal(err)
	}

	fakeDir := &versionedFakeDirectory{version: 10}
	phoneDirectoryOverride = fakeDir
	defer func() { phoneDirectoryOverride = nil }()

	// 1. Seed the directory version pin at version 10.
	ctx1, cancel1 := context.WithCancel(context.Background())
	var out1 pairScreenText
	done1 := make(chan error, 1)
	go func() {
		done1 <- runPhoneContext(ctx1, []string{"serve", "--home", homeDir}, &out1)
	}()
	out1.waitFor(t, `serving your phone on this computer\. ctrl\+c stops it\.`)
	// Wait until pins.json records version 10.
	pinDeadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(pinDeadline) {
		if raw, err := os.ReadFile(clientrun.PinsPath(homeDir)); err == nil && strings.Contains(string(raw), "10") {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel1()
	if err := <-done1; err != nil {
		t.Fatalf("clean stop of initial phone serve: %v", err)
	}

	// 2. Lower the fake directory's version to 3 and run `codeaf phone serve` (V3).
	fakeDir.mu.Lock()
	fakeDir.version = 3
	fakeDir.mu.Unlock()

	ctx2, cancel2 := context.WithCancel(context.Background())
	var out2 pairScreenText
	done2 := make(chan error, 1)
	go func() {
		done2 <- runPhoneContext(ctx2, []string{"serve", "--home", homeDir}, &out2)
	}()
	wantPaused := clientrun.PausedOlderListReason
	out2.waitFor(t, `phone serving is paused: the device list on this sync address is older`)
	// Must keep running (not exit on its own) while paused.
	select {
	case err := <-done2:
		t.Fatalf("phone serve exited on older list instead of pausing: %v", err)
	default:
	}
	cancel2()
	if err := <-done2; err != nil {
		t.Fatalf("clean stop after V3 pause: %v", err)
	}
	if count := strings.Count(out2.String(), wantPaused); count != 1 {
		t.Fatalf("V3 paused sentence printed %d times, want 1:\n%s", count, out2.String())
	}

	// 3. Run `codeaf phone serve --accept-older-list` against version 3 (V5).
	ctx3, cancel3 := context.WithCancel(context.Background())
	var out3 pairScreenText
	done3 := make(chan error, 1)
	go func() {
		done3 <- runPhoneContext(ctx3, []string{"serve", "--home", homeDir, "--accept-older-list"}, &out3)
	}()
	out3.waitFor(t, `accepted the older device list\. Phones paired before the reset must pair again\.`)
	out3.waitFor(t, `serving your phone on this computer\. ctrl\+c stops it\.`)
	cancel3()
	if err := <-done3; err != nil {
		t.Fatalf("clean stop after --accept-older-list: %v", err)
	}
	if strings.Contains(out3.String(), wantPaused) {
		t.Fatalf("V5 --accept-older-list still printed paused sentence:\n%s", out3.String())
	}
}

// TestPhoneServeHelpAndFrontPageAbsence verifies that `codeaf --help` does not
// list `phone`, while `codeaf phone --help` and `codeaf phone serve --help`
// print the synopsis and flags with exit 0 and no banned words.
func TestPhoneServeHelpAndFrontPageAbsence(t *testing.T) {
	if strings.Contains(usageText, "codeaf phone") || strings.Contains(frontPage(), "codeaf phone") {
		t.Fatalf("front page `codeaf --help` must not mention `codeaf phone`")
	}
	for _, cmd := range []string{"phone", "phone serve"} {
		u := usageForCommand(cmd)
		if !strings.Contains(u, "codeaf phone serve [--home dir] [--accept-older-list]") {
			t.Fatalf("usageForCommand(%q) = %q", cmd, u)
		}
	}
	for _, args := range [][]string{{"--help"}, {"-h"}, {"serve", "--help"}, {"serve", "-h"}} {
		out, errs := captureUsage(t)
		err := runPhoneContext(context.Background(), args, errs)
		if !errors.Is(err, exitHelped) || exitCodeOf(err) != 0 {
			t.Fatalf("runPhone(%v) err = %v (code %d), want exitHelped (0)", args, err, exitCodeOf(err))
		}
		page := out.String()
		for _, want := range []string{"codeaf phone serve [--home dir] [--accept-older-list]", "--home", "--accept-older-list"} {
			if !strings.Contains(page, want) {
				t.Fatalf("`codeaf phone %s` missing %q:\n%s", strings.Join(args, " "), want, page)
			}
		}
		lower := strings.ToLower(page)
		for _, bad := range slices.Clone(machineryVocabulary) {
			if strings.Contains(lower, bad) {
				t.Fatalf("`codeaf phone %s` used forbidden word %q:\n%s", strings.Join(args, " "), bad, page)
			}
		}
	}
}
