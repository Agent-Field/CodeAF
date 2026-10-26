package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cell/celltest"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/relayserve"
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/reqsign"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
	"github.com/Agent-Field/codeaf/internal/tui3"
)

func appSyncBackend(t *testing.T) (*appBackend, *appRecorder, *recordingModel, string) {
	t.Helper()
	model, workspace := wakeEnvironment(t, resolvedTempDir(t))
	t.Setenv("CODEAF_ENGINE_DAEMON", "0")
	previous := syncDrives
	syncDrives = &driveBook{}
	rec := newAppRecorder()
	b := newAppBackend(rec, appOpenInProcess)
	b.useSyncDrives(syncDrives)
	t.Cleanup(func() {
		_ = b.Close()
		syncDrives.closeAll()
		syncDrives = previous
	})
	return b, rec, model, workspace
}

func appSyncWait(t *testing.T, what string, check func() bool) {
	t.Helper()
	until := time.Now().Add(30 * time.Second)
	for !check() {
		if time.Now().After(until) {
			t.Fatal("timed out waiting for " + what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func appSyncRelay(t *testing.T) *httptest.Server {
	t.Helper()
	svc := relayserve.New(relayserve.Config{Store: t.TempDir()})
	srv := httptest.NewServer(svc.Handler)
	t.Cleanup(func() { srv.Close(); _ = svc.Close() })
	return srv
}

func appSyncOwn(t *testing.T) *syncsetup.Sync {
	t.Helper()
	s, ok, err := syncsetup.Open(home.Dir())
	if err != nil || !ok {
		t.Fatalf("sync open = %v, %v", ok, err)
	}
	return s
}

type appSyncSigner struct{ sync *syncsetup.Sync }

func (s appSyncSigner) IdentityKey() ed25519.PublicKey { return s.sync.Identity.PublicKey() }
func (s appSyncSigner) Cert() identity.Cert            { return s.sync.Device.Cert }
func (s appSyncSigner) Sign(msg []byte) []byte         { return s.sync.Device.Sign(msg) }

func appSyncHoldReply(t *testing.T, model *recordingModel) (func(), <-chan struct{}, func()) {
	t.Helper()
	var block atomic.Bool
	started, release := make(chan struct{}), make(chan struct{})
	var startedOnce, releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(unblock)
	original := model.server.Config.Handler
	model.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if block.Load() && strings.HasSuffix(r.URL.Path, "/chat/completions") {
			raw, _ := io.ReadAll(r.Body)
			r.Body = io.NopCloser(bytes.NewReader(raw))
			var request recordedRequest
			_ = json.Unmarshal(raw, &request)
			if request.Stream {
				startedOnce.Do(func() { close(started) })
				select {
				case <-release:
				case <-r.Context().Done():
					return
				}
			}
		}
		original.ServeHTTP(w, r)
	})
	return func() { block.Store(true) }, started, unblock
}

// C1/C2: a real drive loses its real relay lease while a real app turn runs.
// The wire refuses every new user-work door, keeps queued words and staged
// attachments, and the journal contains neither the notices nor refused work.
func TestC1C2AppViewerRefusesWorkBeforeSubmission(t *testing.T) {
	celltest.RequireEngine(t)
	srv := appSyncRelay(t)
	b, rec, model, workspace := appSyncBackend(t)
	t.Setenv(syncsetup.URLVar, srv.URL)
	t.Setenv(syncsetup.IntervalVar, "50")
	if _, err := identity.Ensure(home.Dir()); err != nil {
		t.Fatal(err)
	}
	block, started, unblock := appSyncHoldReply(t, model)
	var modelCalls atomic.Int32
	handler := model.server.Config.Handler
	model.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/chat/completions") {
			raw, _ := io.ReadAll(r.Body)
			r.Body = io.NopCloser(bytes.NewReader(raw))
			var request recordedRequest
			_ = json.Unmarshal(raw, &request)
			if request.Stream {
				modelCalls.Add(1)
			}
		}
		handler.ServeHTTP(w, r)
	})
	agent, err := mustCreate(t, b, appwire.CreateAsk{Workspace: workspace})
	if err != nil {
		t.Fatal(err)
	}
	turn, err := b.Send(agent.ID, "first", "first")
	if err != nil {
		t.Fatal(err)
	}
	appTurnDoneFor(t, rec, turn)
	a := b.agent(agent.ID)
	place := v3PlaceOf(appTranscriptOf(b, agent.ID), workspace)
	s := appSyncOwn(t)
	appSyncWait(t, "the first published turn", func() bool {
		v, err := s.Dir.Cell(context.Background(), place.ID())
		return err == nil && v.Cell.Head != ""
	})
	block()
	turn, err = b.Send(agent.ID, "keep working", "running")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(30 * time.Second):
		t.Fatal("the real turn never reached the scripted model")
	}
	queued, err := b.Queue(agent.ID, "keep this queued draft", "queued")
	if err != nil || len(queued) != 1 {
		t.Fatalf("queue = %+v, %v", queued, err)
	}
	// Go through the real remote client, bypassing the app's admission. This
	// message is accepted before the move and must be refused when drained.
	client, ok := b.liveOf(a).conn.Conv.(*remote.Agent)
	if !ok {
		t.Fatal("the real session has no remote client")
	}
	follow, err := client.FollowUp("refused remote queued draft")
	if err != nil {
		t.Fatal(err)
	}
	tasksBefore, err := client.AppTasks()
	if err != nil {
		t.Fatal(err)
	}
	replayedAsk := taskAsk(agent.ID, "existing-viewer-receipt", "already admitted")
	prior := appActionClaim{V: appActionClaimV, ClientID: replayedAsk.ClientID, Agent: agent.ID,
		Action: replayedAsk.Action, Digest: appActionDigest(replayedAsk), State: appActionDone, At: time.Now().UTC()}
	if made, err := b.actionClaims().create(prior); err != nil || !made {
		t.Fatalf("seed existing receipt = %v, %v", made, err)
	}
	chip, err := b.AddAttachment(agent.ID, appwire.Attachment{Kind: appwire.AttachPaste}, strings.Repeat("keep this attachment\n", appPasteLines+1))
	if err != nil {
		t.Fatal(err)
	}
	otherHome := t.TempDir()
	if _, err := identity.Adopt(otherHome, s.Identity, false); err != nil {
		t.Fatal(err)
	}
	other, ok, err := syncsetup.Open(otherHome)
	if err != nil || !ok {
		t.Fatalf("other computer = %v, %v", ok, err)
	}
	if err := syncsetup.PutOwnDevice(context.Background(), other.Dir, other.Identity, other.Device, "laptop"); err != nil {
		t.Fatal(err)
	}
	if _, err := other.Dir.Acquire(context.Background(), place.ID(), directory.AcquireOpts{Force: true}); err != nil {
		t.Fatal(err)
	}
	// This is the same drive the real seat holds; checking it closes the gap
	// without waiting for the relay's watch or heartbeat to arrive.
	b.drives.mu.Lock()
	drive := b.drives.drive[place.ID()]
	b.drives.mu.Unlock()
	if drive == nil || drive.Gate() == nil {
		t.Fatal("the real drive did not become a viewer")
	}
	line := chatlist.Superseded("laptop")
	beforeRefusals := modelCalls.Load()
	entryWith(rec, t, func(e appwire.Entry) bool {
		return e.Agent == agent.ID && e.NoticeKind == appwire.NoticeMoved && e.Text == line && !e.Quiet
	})
	wire := newActionWire(t, b)
	if !slices.Contains(wire.hello, appwire.FeatureChatsLocation) || !slices.Contains(wire.hello, appwire.FeatureSyncStatus) {
		t.Fatalf("hello = %v", wire.hello)
	}
	for _, method := range []string{"agents.send", "agents.steer", "agents.queue", "agents.retry"} {
		params := map[string]any{"id": agent.ID, "text": "refused draft", "client_id": "refused"}
		if method == "agents.send" {
			params["attachments"] = []string{chip.ID}
		}
		_, refusal := wire.call(method, params)
		if refusal == nil || refusal.Code != appwire.CodeConflict || refusal.Message != line {
			t.Fatalf("%s refusal = %+v", method, refusal)
		}
		raw, _ := json.Marshal(refusal.Detail)
		var detail appwire.HeldElsewhere
		if err := json.Unmarshal(raw, &detail); err != nil || detail.Kind != "held_elsewhere" || detail.Holder == nil || detail.Holder.ID != other.Device.ID() || detail.Holder.Name != "laptop" {
			t.Fatalf("%s detail = %s, %v", method, raw, err)
		}
	}
	// R2-1: a new task or program must leave no action intent on a viewer.
	for _, ask := range []appwire.ActionAsk{
		taskAsk(agent.ID, "viewer-task", "refused task draft"),
		programAsk(agent.ID, "viewer-program", "senior-dev", "refused program draft"),
	} {
		_, refusal := wire.act(ask)
		if refusal == nil || refusal.Code != appwire.CodeConflict || refusal.Message != line {
			t.Fatalf("%s refusal = %+v", ask.Action, refusal)
		}
		raw, _ := json.Marshal(refusal.Detail)
		var detail appwire.HeldElsewhere
		if err := json.Unmarshal(raw, &detail); err != nil || detail.Kind != "held_elsewhere" || detail.Holder == nil || detail.Holder.ID != other.Device.ID() {
			t.Fatalf("%s detail = %s, %v", ask.Action, raw, err)
		}
		if _, found, err := b.actionClaims().read(ask.ClientID); err != nil || found {
			t.Fatalf("refused action recorded an intent: %v, %v", found, err)
		}
	}
	for name, work := range map[string]func() error{
		"quick promotion": func() error { _, err := b.PromoteQuick(agent.ID + ":1"); return err },
		"move to task":    func() error { _, err := b.MoveToTask(agent.ID, turn); return err },
		"task retry":      func() error { return b.RetryTask(agent.ID + ":1") },
		"task note":       func() error { _, err := b.NoteTask(agent.ID+":1", "refused task note", "viewer-note"); return err },
		"task action":     func() error { return b.ActOnTask(agent.ID+":1", "start", "", "") },
		"crew proposal":   func() error { return b.CrewPropose(agent.ID) },
		"ask answer":      func() error { return b.AnswerAsk(agent.ID, "ask:1", "", "refused answer") },
	} {
		var refusal *appwire.Error
		if err := work(); !errors.As(err, &refusal) || refusal.Code != appwire.CodeConflict || refusal.Message != line {
			t.Fatalf("app %s refusal = %v", name, err)
		}
	}
	if receipt, refusal := wire.act(replayedAsk); refusal != nil || !receipt.Replayed || receipt.State != appwire.ActionDone {
		t.Fatalf("viewer could not replay a receipt: %+v, %v", receipt, refusal)
	}
	// R2-2: remote work doors reach the session without going through b.Send.
	for name, send := range map[string]func() error{
		"submit": func() error { _, err := client.Submit(context.Background(), "refused remote draft"); return err },
		"marked submit": func() error {
			_, err := client.SubmitMessage(context.Background(), session.Submission{Text: "refused remote marked draft", ClientID: "viewer-marked"})
			return err
		},
		"composed submit": func() error {
			_, _, err := client.SubmitComposed(context.Background(), session.Submission{Text: "refused remote composed draft", ClientID: "viewer-composed"})
			return err
		},
		"steer":     func() error { _, err := client.Steer("refused remote steer"); return err },
		"follow-up": func() error { _, err := client.FollowUp("refused remote follow-up"); return err },
		"task": func() error {
			_, _, _, err := client.StartTask(context.Background(), "refused remote task", true)
			return err
		},
		"program": func() error {
			_, _, _, err := client.StartDelegate(context.Background(), "senior-dev", "refused remote program")
			return err
		},
		"move": client.MoveTurnToTask,
		"answer": func() error {
			return client.ResolveQuestion(session.Answer{Kind: session.QuestionAsk, ID: 1, Change: "refused remote answer"})
		},
	} {
		if err := send(); err == nil || err.Error() != line {
			t.Fatalf("remote %s refusal = %v", name, err)
		}
	}
	if tasksAfter, err := client.AppTasks(); err != nil || len(tasksAfter) != len(tasksBefore) {
		t.Fatalf("viewer admitted a task: before %+v, after %+v, %v", tasksBefore, tasksAfter, err)
	}
	// A joined host may have no app-owned drive or cached location. Its
	// preflight and typed refusal still come from the remote session's gate.
	var savedDrives *driveBook
	var savedLocation *appwire.Location
	b.locked(func() {
		savedDrives, savedLocation = b.drives, a.view.Location
		b.drives, a.view.Location = nil, nil
	})
	_, remoteRefusal := wire.call("agents.send", map[string]any{"id": agent.ID, "text": "refused joined-host draft"})
	b.locked(func() { b.drives, a.view.Location = savedDrives, savedLocation })
	if remoteRefusal == nil || remoteRefusal.Code != appwire.CodeConflict || remoteRefusal.Message != line {
		t.Fatalf("joined-host refusal = %+v", remoteRefusal)
	}
	if got := modelCalls.Load(); got != beforeRefusals {
		t.Fatalf("viewer started model calls: before %d, after %d", beforeRefusals, got)
	}
	// Editing and reordering only maintain the queue; neither starts work.
	if _, err := b.EditQueued(agent.ID, queued[0].ID, "keep this edited draft"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.MoveQueued(agent.ID, queued[0].ID, 0); err != nil {
		t.Fatal(err)
	}
	if _, _, err := b.Entries(agent.ID, "", appwire.MaxEntries); err != nil {
		t.Fatalf("viewer history refused: %v", err)
	}
	if _, err := b.OpenAgent(agent.ID); err != nil {
		t.Fatalf("viewer open refused: %v", err)
	}
	unblock()
	appTurnDoneFor(t, rec, turn)
	select {
	case ev, open := <-follow:
		if !open || ev.Kind != session.EventError || ev.Err == nil || ev.Err.Error() != line {
			t.Fatalf("remote queued follow-up refusal = %+v, open %v", ev, open)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("remote queued follow-up did not report the viewer refusal")
	}
	if got := modelCalls.Load(); got != beforeRefusals {
		t.Fatalf("queued viewer message started a model call: before %d, after %d", beforeRefusals, got)
	}
	b.sendQueued(a, b.liveOf(a))
	if got := b.queued(a); len(got) != 1 || got[0].Text != "keep this edited draft" {
		t.Fatalf("viewer consumed queued draft: %+v", got)
	}
	b.locked(func() {
		if _, kept := a.attach[chip.ID]; !kept {
			t.Error("viewer refusal consumed the staged attachment")
		}
	})
	for _, request := range appModelRequests(model) {
		if strings.Contains(request.allText(), "refused ") || strings.Contains(request.allText(), "keep this edited draft") {
			t.Fatal("refused work reached the model")
		}
	}
	raw, err := os.ReadFile(appTranscriptOf(b, agent.ID))
	if err != nil || bytes.Contains(raw, []byte(line)) || bytes.Contains(raw, []byte("refused ")) || bytes.Contains(raw, []byte("keep this edited draft")) {
		t.Fatalf("viewer wrote to the journal: %v", err)
	}
	listed, err := b.ListAgents(workspace)
	if err != nil {
		t.Fatal(err)
	}
	for _, view := range listed {
		if view.ID == agent.ID {
			if view.Location == nil || view.Location.Here {
				t.Fatalf("listed viewer lost location: %+v", view)
			}
			return
		}
	}
	t.Fatalf("the open viewer was not listed: %+v", listed)
}

// C2: every drive sentence reaches the real app backend's event surface once;
// this table injects copy at the sink. C1 above supplies the real-drive path.
func TestC2AppSyncSentencesAreVisibleLocalNotices(t *testing.T) {
	b, rec, _, workspace := appSyncBackend(t)
	t.Setenv(syncsetup.URLVar, "off")
	agent, err := mustCreate(t, b, appwire.CreateAsk{Workspace: workspace})
	if err != nil {
		t.Fatal(err)
	}
	place := v3PlaceOf(appTranscriptOf(b, agent.ID), workspace)
	c := cell.Cell{ID: place.ID(), Root: place.Dir}
	lines := []string{
		chatlist.KeptEdits(2, "tower"), chatlist.RelayFull(5 << 30), chatlist.Removed,
		chatlist.ReplacedGone, chatlist.Replaced, chatlist.ClockOff, chatlist.SlowDown,
		chatlist.TooManyNew, chatlist.TooManyNewIn(time.Hour),
		"could not record which turns reached your other devices: disk full",
	}
	for _, line := range lines {
		b.drives.notices.say(c, line)
		b.drives.notices.say(c, line)
		entryWith(rec, t, func(e appwire.Entry) bool { return e.Text == line && e.NoticeKind == appwire.NoticeSync && !e.Quiet })
	}
	counts := map[string]int{}
	rec.mu.Lock()
	for _, data := range rec.data {
		if added, ok := data.(appwire.EntryAdded); ok && added.Entry.NoticeKind == appwire.NoticeSync {
			counts[added.Entry.Text]++
		}
	}
	rec.mu.Unlock()
	raw, err := os.ReadFile(appTranscriptOf(b, agent.ID))
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range lines {
		if counts[line] != 1 || bytes.Contains(raw, []byte(line)) {
			t.Fatalf("sentence count %d, or journaled: %q", counts[line], line)
		}
	}
}

func TestC2AppSyncStartupFactsWaitForTheOpenedChat(t *testing.T) {
	b, rec, _, workspace := appSyncBackend(t)
	t.Setenv(syncsetup.URLVar, "off")
	transcript, err := appMint(workspace)
	if err != nil {
		t.Fatal(err)
	}
	dir, _ := session.FolderOf(transcript)
	c := cell.Cell{Root: dir, ID: filepath.Base(dir)}
	line := chatlist.Superseded("laptop")
	b.drives.notices.viewer(c, syncsetup.Viewer{ID: "other", Name: "laptop", Line: line})
	b.drives.notices.say(c, line)
	b.mint = func(string) (string, error) { return transcript, nil }
	agent, err := mustCreate(t, b, appwire.CreateAsk{Workspace: workspace})
	if err != nil {
		t.Fatal(err)
	}
	entryWith(rec, t, func(e appwire.Entry) bool {
		return e.Agent == agent.ID && e.NoticeKind == appwire.NoticeMoved && e.Text == line
	})
	view, err := b.OpenAgent(agent.ID)
	if err != nil || view.Location == nil || view.Location.Holder == nil || view.Location.Holder.ID != "other" {
		t.Fatalf("startup location lost before hold: %+v, %v", view, err)
	}
	// A later successful drive clears the durable view fact and announces it.
	b.drives.notices.driving(c)
	view, err = b.OpenAgent(agent.ID)
	if err != nil || view.Location != nil {
		t.Fatalf("location did not clear: %+v, %v", view, err)
	}
}

func TestC2AppStartupRefusalReachesItsChat(t *testing.T) {
	celltest.RequireEngine(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"err":"revoked"}`)
	}))
	t.Cleanup(srv.Close)
	b, rec, _, workspace := appSyncBackend(t)
	t.Setenv(syncsetup.URLVar, srv.URL)
	if _, err := identity.Ensure(home.Dir()); err != nil {
		t.Fatal(err)
	}
	agent, err := mustCreate(t, b, appwire.CreateAsk{Workspace: workspace})
	if err != nil {
		t.Fatal(err)
	}
	entryWith(rec, t, func(e appwire.Entry) bool {
		return e.Agent == agent.ID && e.Text == chatlist.Removed && e.NoticeKind == appwire.NoticeSync && !e.Quiet
	})
	raw, err := os.ReadFile(appTranscriptOf(b, agent.ID))
	if err != nil || bytes.Contains(raw, []byte(chatlist.Removed)) {
		t.Fatalf("startup refusal was journaled: %v", err)
	}
}

// C3: a real hosted drive owes the disclosure, sync.get leaves the marker
// alone, and only the acknowledged id marks it. The hosted wire is redirected
// to the in-process relay, so this fixture cannot reach the hosted service.
func TestC3AppHostedDisclosureWaitsForAcknowledgement(t *testing.T) {
	for _, viaTerminal := range []bool{false, true} {
		t.Run(map[bool]string{false: "desktop acknowledges", true: "terminal still tells"}[viaTerminal], func(t *testing.T) {
			celltest.RequireEngine(t)
			srv := appSyncRelay(t)
			b, rec, _, workspace := appSyncBackend(t)
			t.Setenv(syncsetup.URLVar, "")
			if _, err := identity.Ensure(home.Dir()); err != nil {
				t.Fatal(err)
			}
			own := appSyncOwn(t)
			hosted, _ := url.Parse(syncsetup.HostedRelayURL)
			local, _ := url.Parse(srv.URL)
			previous := http.DefaultTransport
			http.DefaultTransport = roundTripper(func(r *http.Request) (*http.Response, error) {
				if r.URL.Host == hosted.Host {
					clone := r.Clone(r.Context())
					u := *r.URL
					u.Scheme, u.Host, u.Path = local.Scheme, local.Host, strings.TrimPrefix(u.Path, hosted.Path)
					clone.URL = &u
					var body []byte
					if r.Body != nil {
						var err error
						body, err = io.ReadAll(r.Body)
						if err != nil {
							return nil, err
						}
						clone.Body = io.NopCloser(bytes.NewReader(body))
					}
					// The path is signed, so this transport fixture signs the
					// redirected path with the same real device before sending it.
					reqsign.Sign(clone, body, appSyncSigner{sync: own}, time.Now())
					return previous.RoundTrip(clone)
				}
				return previous.RoundTrip(r)
			})
			t.Cleanup(func() { http.DefaultTransport = previous })
			agent, err := mustCreate(t, b, appwire.CreateAsk{Workspace: workspace})
			if err != nil {
				t.Fatal(err)
			}
			a := b.agent(agent.ID)
			a.gate.Lock()
			err = b.workAllowed(a)
			a.gate.Unlock()
			if err != nil {
				t.Fatalf("real hosted drive did not start through the local relay: %v", err)
			}
			dir, _ := session.FolderOf(appTranscriptOf(b, agent.ID))
			b.drives.mu.Lock()
			drive := b.drives.drive[filepath.Base(dir)]
			b.drives.mu.Unlock()
			if drive == nil || drive.started() == nil {
				t.Fatal("hosted startup did not install a real drive")
			}
			i := rec.waitFor(t, 30*time.Second, func(event string, data any) bool {
				changed, ok := data.(appwire.SyncChanged)
				return event == appwire.EventSyncChanged && ok && changed.Sync.Disclosure != nil
			})
			rec.mu.Lock()
			disclosure := *rec.data[i].(appwire.SyncChanged).Sync.Disclosure
			rec.mu.Unlock()
			if disclosure.Text != syncsetup.FirstRunLine {
				t.Fatalf("disclosure = %q", disclosure.Text)
			}
			wire := newActionWire(t, b)
			for range 2 {
				raw, err := wire.call("sync.get", struct{}{})
				var result appwire.SyncResult
				if err != nil || json.Unmarshal(raw, &result) != nil || result.Sync.Mode != appwire.SyncOn || result.Sync.Disclosure == nil || *result.Sync.Disclosure != disclosure {
					t.Fatalf("sync.get = %s, %v", raw, err)
				}
			}
			marker := filepath.Join(home.Dir(), "sync-told")
			if _, err := os.Stat(marker); !os.IsNotExist(err) {
				t.Fatalf("drive or read marked disclosure told: %v", err)
			}
			if _, err := wire.call("sync.acknowledge", appwire.IDParams{ID: "unknown"}); err == nil || err.Code != appwire.CodeNotFound {
				t.Fatalf("unknown acknowledgement = %v", err)
			}
			if viaTerminal {
				line, first := (&syncsetup.Sync{Home: home.Dir(), Hosted: true}).FirstRun()
				if !first || line != disclosure.Text {
					t.Fatalf("unacknowledged desktop disclosure never reached the terminal: %q, %v", line, first)
				}
				_, _ = b.SyncStatus()
			} else {
				if _, err := wire.call("sync.acknowledge", appwire.IDParams{ID: disclosure.ID}); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := os.Stat(marker); err != nil {
				t.Fatalf("acknowledgement did not write marker: %v", err)
			}
			if _, err := wire.call("sync.acknowledge", appwire.IDParams{ID: disclosure.ID}); err == nil || err.Code != appwire.CodeNotFound {
				t.Fatalf("repeated acknowledgement = %v", err)
			}
			rec.waitFor(t, time.Second, func(event string, data any) bool {
				changed, ok := data.(appwire.SyncChanged)
				return event == appwire.EventSyncChanged && ok && changed.Sync.Mode == appwire.SyncOn && changed.Sync.Disclosure == nil
			})
			if _, owed := (&syncsetup.Sync{Home: home.Dir(), Hosted: true}).FirstRun(); owed {
				t.Fatal("terminal repeated the acknowledged disclosure")
			}
		})
	}
}

// C4: the file request travels through a real session's standing lane. The
// journal lock becomes available, and later app opens and sends cannot race
// the terminal to reclaim it.
func TestC4AppAnswersLocalTerminalTakeover(t *testing.T) {
	t.Cleanup(cell.UseAvailabilityInTests(false))
	b, rec, _, workspace := appSyncBackend(t)
	t.Setenv(syncsetup.URLVar, "off")
	agent, err := mustCreate(t, b, appwire.CreateAsk{Workspace: workspace})
	if err != nil {
		t.Fatal(err)
	}
	turn, err := b.Send(agent.ID, "keep this conversation", "first")
	if err != nil {
		t.Fatal(err)
	}
	appTurnDoneFor(t, rec, turn)
	transcript := appTranscriptOf(b, agent.ID)
	if !session.InUse(transcript) {
		t.Fatal("fixture did not hold the journal")
	}
	if err := session.AskTakeover(v3PlaceOf(transcript, workspace).Dir); err != nil {
		t.Fatal(err)
	}
	entryWith(rec, t, func(e appwire.Entry) bool {
		return e.NoticeKind == appwire.NoticeMoved && e.Text == session.TakeoverWord && !e.Quiet
	})
	appSyncWait(t, "the terminal's journal lock", func() bool { return !session.InUse(transcript) })
	if _, err := b.OpenAgent(agent.ID); err == nil || !strings.Contains(err.Error(), appWindowConflict) {
		t.Fatalf("open after takeover = %v", err)
	}
	if _, err := b.Send(agent.ID, "do not reopen", "later"); err == nil || !strings.Contains(err.Error(), appWindowConflict) {
		t.Fatalf("send after takeover = %v", err)
	}
	// A terminal's ordinary opener can now hold the actual conversation.
	terminal, err := appOpenInProcess(appOpenAsk{Workspace: workspace, Transcript: transcript})
	if err != nil {
		t.Fatalf("terminal could not open after takeover: %v", err)
	}
	terminal.Release()
	raw, _ := os.ReadFile(transcript)
	if bytes.Contains(raw, []byte(session.TakeoverWord)) || bytes.Contains(raw, []byte("do not reopen")) {
		t.Fatal("the move notice or refused work was journaled")
	}
}

func TestC4AppTakeoverStopsAnInFlightReply(t *testing.T) {
	t.Cleanup(cell.UseAvailabilityInTests(false))
	b, rec, model, workspace := appSyncBackend(t)
	t.Setenv(syncsetup.URLVar, "off")
	block, started, _ := appSyncHoldReply(t, model)
	agent, err := mustCreate(t, b, appwire.CreateAsk{Workspace: workspace})
	if err != nil {
		t.Fatal(err)
	}
	block()
	if _, err := b.Send(agent.ID, "keep the unfinished answer", "running"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(30 * time.Second):
		t.Fatal("reply never started")
	}
	transcript := appTranscriptOf(b, agent.ID)
	if err := session.AskTakeover(v3PlaceOf(transcript, workspace).Dir); err != nil {
		t.Fatal(err)
	}
	entryWith(rec, t, func(e appwire.Entry) bool {
		return e.NoticeKind == appwire.NoticeMoved && e.Text == session.TakeoverWord
	})
	appSyncWait(t, "takeover to release the running conversation", func() bool { return !session.InUse(transcript) })
	raw, err := os.ReadFile(transcript)
	if err != nil || !bytes.Contains(raw, []byte("keep the unfinished answer")) || !bytes.Contains(raw, []byte(string(session.StopByTakeover))) {
		t.Fatalf("takeover did not keep the interrupted turn with its stop cause: %v", err)
	}
}

// C5: actual incomplete WAL records reach appConnOf through the real remote
// welcome, then become visible notices when the app holds the conversation.
func TestC5AppNamesCrashInterruptedCommands(t *testing.T) {
	celltest.RequireEngine(t)
	b, rec, _, workspace := appSyncBackend(t)
	t.Setenv(syncsetup.URLVar, "off")
	transcript, err := appMint(workspace)
	if err != nil {
		t.Fatal(err)
	}
	place := v3PlaceOf(transcript, workspace)
	c, err := cell.OpenAt(place.Dir, place.ID())
	if err != nil {
		t.Fatal(err)
	}
	wal, _, err := cellstore.OpenWAL((cellstore.Engine{}).WALPath(c))
	if err != nil {
		t.Fatal(err)
	}
	for i, tool := range []string{"bash", "edit"} {
		if err := wal.Begin(cellstore.Intent{Tool: tool, ArgsHash: tool, Started: int64(i + 1), SideEffect: "external", Brief: "unfinished"}); err != nil {
			t.Fatal(err)
		}
	}
	a := b.agent(appAgentID(transcript))
	b.locked(func() { a.transcript, a.view.Workspace = transcript, workspace })
	if _, err := b.OpenAgent(a.view.ID); err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, tool := range []string{"bash", "edit"} {
		line := tool + ": unfinished — stopped when codeaf was killed — not run again. Ask me to run it again if you still want it."
		entry := entryWith(rec, t, func(e appwire.Entry) bool {
			return e.NoticeKind == appwire.NoticeInterrupted && e.Text == line && !e.Quiet
		})
		ids[entry.ID] = true
	}
	if len(ids) != 2 {
		t.Fatal("interrupted commands shared an entry id")
	}
	conn := b.liveOf(a).conn
	b.openingNotices(a, conn)
	count := 0
	rec.mu.Lock()
	for _, data := range rec.data {
		if added, ok := data.(appwire.EntryAdded); ok && added.Entry.NoticeKind == appwire.NoticeInterrupted {
			count++
		}
	}
	rec.mu.Unlock()
	if count != 2 {
		t.Fatalf("repeated welcome duplicated warnings: %d", count)
	}
}

// C6: a pure status read and a drive start with no identity send no request.
// Off takes precedence even on a paired home; unavailable names the engine.
func TestC6AppSyncQuietOffAndUnavailableStayLocal(t *testing.T) {
	t.Cleanup(cell.UseAvailabilityInTests(true))
	var requests atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.Error(w, "unexpected relay request", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	b, _, _, _ := appSyncBackend(t)
	t.Setenv(syncsetup.URLVar, srv.URL)
	wire := newActionWire(t, b)
	read := func(want, reason string) {
		t.Helper()
		raw, err := wire.call("sync.get", struct{}{})
		var result appwire.SyncResult
		if err != nil || json.Unmarshal(raw, &result) != nil || result.Sync.Mode != want || result.Sync.Reason != reason || result.Sync.Disclosure != nil {
			t.Fatalf("sync.get = %s, %v; want %s, %q", raw, err, want, reason)
		}
	}
	read(appwire.SyncQuiet, "")
	if d, err := startDriveWith(cell.Cell{}, cellstore.Engine{}, func(error) {}, b.drives.notices); d != nil || !errors.Is(err, syncsetup.ErrNoIdentity) {
		t.Fatalf("unpaired drive = %v, %v", d, err)
	}
	if _, err := identity.Load(home.Dir()); !errors.Is(err, identity.ErrNone) {
		t.Fatal("status created an identity")
	}
	if _, err := identity.Ensure(home.Dir()); err != nil {
		t.Fatal(err)
	}
	t.Setenv(syncsetup.URLVar, "off")
	read(appwire.SyncOff, chatlist.SyncOff)
	if d, err := startDriveWith(cell.Cell{}, cellstore.Engine{}, func(error) {}, b.drives.notices); d != nil || err != nil {
		t.Fatalf("off drive = %v, %v", d, err)
	}
	t.Setenv(syncsetup.URLVar, srv.URL)
	restore := cell.UseAvailabilityInTests(false)
	read(appwire.SyncUnavailable, "moving chats needs a newer furrow engine than this build carries")
	restore()
	if requests.Load() != 0 {
		t.Fatalf("local status or quiet/off startup made %d relay requests", requests.Load())
	}
}

// C6: an ordinary terminal drive's startup refusal still reaches the shared
// surfaceNotices desk and is rendered by the terminal's real notice loop.
func TestC6TerminalDriveStillSaysThroughSurfaceNotices(t *testing.T) {
	cfg, c := doorChat(t)
	prior := surfaceNotices
	surfaceNotices = tui3.NewNotices()
	t.Cleanup(func() { surfaceNotices = prior })
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"err":"revoked"}`)
	}))
	t.Cleanup(srv.Close)
	t.Setenv(syncsetup.URLVar, srv.URL)
	if _, err := identity.Ensure(home.Dir()); err != nil {
		t.Fatal(err)
	}
	if drive, err := startDrive(c, cellstore.EngineFor(cfg.Place.Workspace), func(error) {}); drive != nil || err == nil {
		t.Fatalf("revoked terminal drive = %v, %v", drive, err)
	}
	// Wiring while off adds only the desk and avoids unrelated home reads.
	t.Setenv(syncsetup.URLVar, "off")
	input, writer := io.Pipe()
	t.Cleanup(func() { _ = writer.Close() })
	frame := &surfaceFrameWriter{ready: make(chan struct{})}
	options := tui3.Options{Agent: &quietAgent{}, Workspace: cfg.Place.Workspace, ProfileDir: t.TempDir(), Input: input, Output: frame, Width: 160, Height: 30}
	wireSync(&options)
	if options.Notices != surfaceNotices {
		t.Fatal("terminal lost the shared notice desk")
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	finished := make(chan error, 1)
	go func() { finished <- runSurfaceProgram(ctx, options) }()
	appSyncWait(t, "the terminal notice", func() bool { return strings.Contains(frame.String(), "this device was removed") })
	cancel()
	select {
	case err := <-finished:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("terminal notice surface did not leave")
	}
}
