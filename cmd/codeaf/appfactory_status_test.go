package main

import (
	"context"
	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/session"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

type appStatusConversation struct{ *fakeConversation }

func (c *appStatusConversation) ReadFactoryStatus(_ context.Context, text string) (string, bool, error) {
	return "Plan is waiting for the saved decision.", factory.StatusOnlyRequest(text), nil
}
func TestAppFactoryStatusLeavesForegroundAndQueueUntouched(t *testing.T) {
	for _, busy := range []bool{false, true} {
		t.Run(map[bool]string{false: "idle", true: "busy"}[busy], func(t *testing.T) {
			useLocalHome(t)
			rec := newAppRecorder()
			conv := &appStatusConversation{&fakeConversation{hold: make(chan struct{}), model: "selected-model"}}
			b := newFakeBackend(rec, func(ask appOpenAsk) (*appConn, error) {
				return &appConn{Conv: conv, Workspace: ask.Workspace, Transcript: ask.Transcript, Release: func() {}}, nil
			})
			b.mint = appMint
			b.state = &appState{path: filepath.Join(t.TempDir(), appStateName)}
			t.Cleanup(func() { close(conv.hold); b.Close() })
			agent, err := mustCreate(t, b, appwire.CreateAsk{Workspace: t.TempDir()})
			if err != nil {
				t.Fatal(err)
			}
			a, _ := b.find(agent.ID)
			live := b.liveOf(a)
			if busy {
				if _, err := b.Send(agent.ID, "Continue investigating", "original"); err != nil {
					t.Fatal(err)
				}
			}
			if busy {
				rec.waitFor(t, time.Second, func(event string, _ any) bool { return event == appwire.EventTurnStarted })
			}
			b.locked(func() { a.queue = append(a.queue, appQueued{id: "retained", text: "queued work"}) })
			var turn uint64
			var running bool
			var view appwire.Agent
			b.locked(func() { turn, running, view = live.turn, live.running, a.view })
			rec.mu.Lock()
			beforeEvents := len(rec.events)
			rec.mu.Unlock()
			if _, err := b.Send(agent.ID, "Everything ok here?", "status-send"); err != nil {
				t.Fatal(err)
			}
			if err := b.steer(agent.ID, "Any updates?", "status-steer", nil); err != nil {
				t.Fatal(err)
			}
			answer := addedSaying(t, rec, "Plan is waiting for the saved decision.")
			if answer.Turn != 0 || answer.NoticeKind != "factory_status" {
				t.Fatal("status claimed a foreground turn")
			}
			b.locked(func() {
				if live.turn != turn || live.running != running || len(a.queue) != 1 || !reflect.DeepEqual(a.view, view) {
					t.Error("status changed running work, queue or identity")
				}
			})
			conv.mu.Lock()
			submits, steers, interrupts := conv.submits, len(conv.steered), conv.interrupted
			conv.mu.Unlock()
			want := 0
			if busy {
				want = 1
			}
			if submits != want || steers != 0 || interrupts != 0 {
				t.Fatal("status started paid steering or cancelled work")
			}
			rec.mu.Lock()
			for _, e := range rec.events[beforeEvents:] {
				if e == appwire.EventTurnStarted || e == appwire.EventTurnDone {
					t.Error("status emitted a false turn lifecycle event")
				}
			}
			rec.mu.Unlock()
			page, _, err := b.Entries(agent.ID, "", 100)
			if err != nil {
				t.Fatal(err)
			}
			users, answers := 0, 0
			for _, e := range page {
				if e.ClientID == "status-send" || e.ClientID == "status-steer" {
					users++
				}
				if e.NoticeKind == "factory_status" {
					answers++
				}
			}
			if users != 2 || answers != 2 {
				t.Fatal("human-only exchange did not survive history reload")
			}
			if busy {
				conv.mu.Lock()
				conv.steer = make(chan session.Event)
				close(conv.steer)
				conv.mu.Unlock()
				if err := b.steer(agent.ID, "Use the existing helper", "ordinary", nil); err != nil {
					t.Fatal(err)
				}
				conv.mu.Lock()
				n := len(conv.steered)
				conv.mu.Unlock()
				if n != 1 {
					t.Fatal("ordinary correction lost steering")
				}
			}
		})
	}
}
