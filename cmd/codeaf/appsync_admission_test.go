package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cell/celltest"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/relayserve"
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

type appAdmissionCapture struct {
	entered chan struct{}
	release chan struct{}
}

func (s appAdmissionCapture) Seal(ctx context.Context, _ cell.Cell, _ cellstore.TurnInfo) (cellstore.Sealed, error) {
	close(s.entered)
	select {
	case <-s.release:
		return cellstore.Sealed{}, nil
	case <-ctx.Done():
		return cellstore.Sealed{}, ctx.Err()
	}
}

// C6 and the admission amendment: a real paired app send reaches its session
// while background startup or the previous capture is held open. Admission
// neither joins that work nor adds a relay request.
func TestC6AppSendDoesNotWaitForSyncStartupOrCapture(t *testing.T) {
	for _, boundary := range []string{"startup", "capture"} {
		t.Run(boundary, func(t *testing.T) {
			celltest.RequireEngine(t)
			svc := relayserve.New(relayserve.Config{Store: t.TempDir()})
			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			var requests atomic.Int64
			var first sync.Once
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if boundary == "startup" {
					first.Do(func() { close(entered) })
					select {
					case <-release:
					case <-r.Context().Done():
						return
					}
				}
				svc.Handler.ServeHTTP(w, r)
			}))
			t.Cleanup(func() { srv.Close(); _ = svc.Close() })
			b, rec, model, workspace := appSyncBackend(t)
			t.Setenv(syncsetup.URLVar, srv.URL)
			t.Setenv(syncsetup.IntervalVar, "600000")
			if _, err := identity.Ensure(home.Dir()); err != nil {
				t.Fatal(err)
			}
			blockModel, modelEntered, releaseModel := appSyncHoldReply(t, model)
			blockModel()
			t.Cleanup(unblock)
			agent, err := mustCreate(t, b, appwire.CreateAsk{Workspace: workspace})
			if err != nil {
				t.Fatal(err)
			}
			dir, _ := session.FolderOf(appTranscriptOf(b, agent.ID))
			c, err := cell.OpenAt(dir, filepath.Base(dir))
			if err != nil {
				t.Fatal(err)
			}
			if boundary == "startup" {
				select {
				case <-entered:
				case <-time.After(10 * time.Second):
					t.Fatal("background startup never reached the relay")
				}
			} else {
				b.drives.mu.Lock()
				drive := b.drives.drive[c.ID]
				b.drives.mu.Unlock()
				if drive == nil {
					t.Fatal("the paired chat has no drive")
				}
				appSyncWait(t, "background drive startup", func() bool { return drive.started() != nil })
				capture := appAdmissionCapture{entered: entered, release: release}
				recorder, err := cellstore.NewRecorder(nil, capture, c, filepath.Join(t.TempDir(), "capture.wal"), cellstore.Options{Async: true})
				if err != nil {
					t.Fatal(err)
				}
				recorder.Warm(context.Background())
				select {
				case <-entered:
				case <-time.After(10 * time.Second):
					t.Fatal("background capture never started")
				}
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				if err := cellstore.Captured(ctx, c); err == nil {
					t.Fatal("the capture fixture did not hold the real boundary")
				}
				t.Cleanup(func() {
					unblock()
					if err := cellstore.Captured(context.Background(), c); err != nil {
						t.Error(err)
					}
				})
			}
			before := requests.Load()
			type sent struct {
				turn uint64
				err  error
			}
			admitted := make(chan sent, 1)
			go func() {
				turn, err := b.Send(agent.ID, "keep working here", "cached-admission")
				admitted <- sent{turn: turn, err: err}
			}()
			var turn uint64
			select {
			case result := <-admitted:
				if result.err != nil {
					t.Fatal(result.err)
				}
				turn = result.turn
			case <-time.After(2 * time.Second):
				t.Fatal("send waited for " + boundary)
			}
			select {
			case <-modelEntered:
			case <-time.After(10 * time.Second):
				t.Fatal("the admitted send never reached the real session model")
			}
			if got := requests.Load(); got != before {
				t.Fatalf("admission added relay work: before %d, after %d", before, got)
			}
			unblock()
			releaseModel()
			appTurnDoneFor(t, rec, turn)
		})
	}
}

// C6: unpaired homes, explicit sync off, and sessions with no sync gate still
// accept ordinary sends through the real app/session path without relay work.
func TestC6AppSendWithoutAViewerStaysLocal(t *testing.T) {
	for _, mode := range []string{"unpaired", "off", "ungated"} {
		t.Run(mode, func(t *testing.T) {
			if mode == "ungated" {
				t.Cleanup(cell.UseAvailabilityInTests(false))
			} else {
				celltest.RequireEngine(t)
			}
			var requests atomic.Int64
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				http.Error(w, "unexpected relay request", http.StatusInternalServerError)
			}))
			t.Cleanup(srv.Close)
			b, rec, _, workspace := appSyncBackend(t)
			t.Setenv(syncsetup.URLVar, srv.URL)
			if mode == "off" {
				if _, err := identity.Ensure(home.Dir()); err != nil {
					t.Fatal(err)
				}
				t.Setenv(syncsetup.URLVar, "off")
			}
			agent, err := mustCreate(t, b, appwire.CreateAsk{Workspace: workspace})
			if err != nil {
				t.Fatal(err)
			}
			turn, err := b.Send(agent.ID, "ordinary message", "local-admission")
			if err != nil {
				t.Fatal(err)
			}
			appTurnDoneFor(t, rec, turn)
			if err := b.Close(); err != nil {
				t.Fatal(err)
			}
			if requests.Load() != 0 {
				t.Fatalf("%s send made %d relay requests", mode, requests.Load())
			}
		})
	}
}

// C1's answer amendment: the app accepts an existing card, including You
// decide, after recording a move. A following new message still refuses.
func TestC1AppViewerAcceptsPendingAnswers(t *testing.T) {
	for _, door := range []string{"answer", "scope", "wire", "you decide"} {
		t.Run(door, func(t *testing.T) {
			conv := &fakeConversation{}
			b, rec, agent := workBackend(t, conv)
			q := session.Question{Kind: session.QuestionAsk, ID: 9, Ask: session.AskChoice, Head: "Which approach?",
				Options: []session.AnswerOption{{Key: "yes", Label: "Use it"}, {Key: "no", Label: "Leave it"}},
				Pick:    &session.Pick{Key: "yes"},
			}
			conv.raise(session.Event{Kind: session.EventQuestion, Question: &q})
			i := rec.waitFor(t, 5*time.Second, appAskEvent(appwire.EventAskOpened))
			rec.mu.Lock()
			ask := rec.data[i].(appwire.AskOpened).Ask
			rec.mu.Unlock()
			b.located(b.agent(agent.ID), &appwire.Location{Here: false, Line: "laptop continued this chat"})
			var err error
			switch door {
			case "answer":
				err = b.AnswerAsk(agent.ID, ask.ID, ask.Options[0].ID, "")
			case "scope":
				err = b.answerAskScope(agent.ID, ask.ID, ask.Options[0].ID, "", "")
			case "wire":
				wire := newActionWire(t, b)
				if _, refusal := wire.call("asks.answer", map[string]any{"agent": agent.ID, "id": ask.ID, "option": ask.Options[0].ID}); refusal != nil {
					err = refusal
				}
			case "you decide":
				err = b.AnswerAskWith(agent.ID, ask.ID, appwire.AskAnswer{YouDecide: true})
			}
			if err != nil {
				t.Fatal(err)
			}
			conv.mu.Lock()
			answers := append([]session.Answer(nil), conv.answers...)
			conv.mu.Unlock()
			if len(answers) != 1 || answers[0].ID != q.ID || answers[0].FirstKey() != "yes" {
				t.Fatalf("answer = %+v", answers)
			}
			if _, err := b.Send(agent.ID, "new work", ""); wireCode(err) != appwire.CodeConflict {
				t.Fatalf("new work after answer = %v", err)
			}
		})
	}
}

// C1's answer amendment through a real drive, session, remote server and app:
// moving while consent waits still accepts the answer, and the permitted
// command is refused by the tool gate before it can write its file.
func TestC1AppViewerAcceptsRealPendingConsent(t *testing.T) {
	for _, door := range []string{"appwire", "remote legacy", "remote question"} {
		t.Run(door, func(t *testing.T) {
			celltest.RequireEngine(t)
			srv := appSyncRelay(t)
			b, rec, model, workspace := appSyncBackend(t)
			t.Setenv(syncsetup.URLVar, srv.URL)
			t.Setenv(syncsetup.IntervalVar, "50")
			if _, err := identity.Ensure(home.Dir()); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(home.Dir(), "config.json"), []byte(`{"tools.approvalMode":"prompt"}`), 0600); err != nil {
				t.Fatal(err)
			}
			agent, err := mustCreate(t, b, appwire.CreateAsk{Workspace: workspace})
			if err != nil {
				t.Fatal(err)
			}
			turn, err := b.Send(agent.ID, "first", "first")
			if err != nil {
				t.Fatal(err)
			}
			appTurnDoneFor(t, rec, turn)
			place := v3PlaceOf(appTranscriptOf(b, agent.ID), workspace)
			own := appSyncOwn(t)
			appSyncWait(t, "the first published turn", func() bool {
				v, err := own.Dir.Cell(context.Background(), place.ID())
				return err == nil && v.Cell.Head != ""
			})
			commandModel := appProcessModel(t, `{"command":"printf ran > viewer-command.txt"}`)
			t.Cleanup(commandModel.Close)
			model.server.Config.Handler = commandModel.Config.Handler
			turn, err = b.Send(agent.ID, "run the command", "pending-consent")
			if err != nil {
				t.Fatal(err)
			}
			i := rec.waitFor(t, 10*time.Second, appAskEvent(appwire.EventAskOpened))
			rec.mu.Lock()
			ask := rec.data[i].(appwire.AskOpened).Ask
			rec.mu.Unlock()
			otherHome := t.TempDir()
			if _, err := identity.Adopt(otherHome, own.Identity, false); err != nil {
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
			b.drives.mu.Lock()
			drive := b.drives.drive[place.ID()]
			b.drives.mu.Unlock()
			if drive == nil || drive.Gate() == nil {
				t.Fatal("the pending conversation did not become a viewer")
			}
			a := b.agent(agent.ID)
			client := b.liveOf(a).conn.Conv.(*remote.Agent)
			switch door {
			case "appwire":
				wire := newActionWire(t, b)
				if _, err := wire.call("asks.answer", map[string]any{"agent": agent.ID, "id": ask.ID, "option": appwire.OptionAllowOnce}); err != nil {
					t.Fatal(err)
				}
			case "remote legacy", "remote question":
				var q session.Question
				b.locked(func() { q = a.live.questions[ask.ID] })
				if q.Kind != session.QuestionConsent {
					t.Fatalf("pending question = %+v", q)
				}
				if door == "remote legacy" {
					client.ResolveConsent(q.ID, true)
				} else if err := client.ResolveQuestion(session.Answer{Kind: q.Kind, ID: q.ID, Key: "1"}); err != nil {
					t.Fatal(err)
				}
			}
			appTurnDoneFor(t, rec, turn)
			if _, err := os.Stat(filepath.Join(workspace, "viewer-command.txt")); !os.IsNotExist(err) {
				t.Fatalf("the permitted command escaped the tool gate: %v", err)
			}
			if err := client.CheckWork(); err == nil {
				t.Fatal("answering cleared the session's viewer fact")
			}
		})
	}
}

// The admission amendment: checking an unopened chat has no opener side
// effect and does not require a conversation record to exist on disk.
func TestC1AppAdmissionDoesNotOpenAChat(t *testing.T) {
	var opens int
	b := newFakeBackend(newAppRecorder(), func(appOpenAsk) (*appConn, error) {
		opens++
		return nil, appwire.Errorf(appwire.CodeEngine, "admission opened a chat")
	})
	t.Cleanup(func() { _ = b.Close() })
	a := &appAgent{view: appwire.Agent{ID: "unopened"}}
	a.gate.Lock()
	err := b.workAllowed(a)
	a.gate.Unlock()
	if err != nil || opens != 0 || a.live != nil {
		t.Fatalf("unopened admission = %v, opens = %d, live = %v", err, opens, a.live)
	}
}

// C1's answer amendment includes the task card's start action when the
// proposal is already pending; starting unrelated work remains refused.
func TestC1AppViewerCanAnswerAnOfferedTask(t *testing.T) {
	conv := &fakeConversation{}
	b, _, agent := workBackend(t, conv)
	id := appTaskID(agent.ID, 9)
	b.locked(func() {
		a := b.agents[agent.ID]
		a.live.tasks[id] = appwire.Task{ID: id, Proposed: true}
	})
	b.located(b.agent(agent.ID), &appwire.Location{Here: false, Line: "laptop continued this chat"})
	if err := b.ActOnTask(id, "start", "", ""); err != nil {
		t.Fatal(err)
	}
	conv.mu.Lock()
	intents := append([]session.AppTaskIntent(nil), conv.intents...)
	conv.mu.Unlock()
	if len(intents) != 1 || intents[0].ID != "9" || intents[0].Action != "start" {
		t.Fatalf("proposal answer = %+v", intents)
	}
	if err := b.ActOnTask(appTaskID(agent.ID, 10), "start", "", ""); wireCode(err) != appwire.CodeConflict {
		t.Fatalf("unrelated task start = %v", err)
	}
}
