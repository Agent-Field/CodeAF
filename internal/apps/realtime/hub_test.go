package realtime

import (
	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"sync"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

func TestHubTopicsAndPrefixes(t *testing.T) {
	hub := New(svc.Config{}, nil)
	exact, stopExact := hub.Subscribe("app/crm/records/deal")
	prefix, stopPrefix := hub.Subscribe("app/crm/*")
	defer stopPrefix()
	hub.Publish("app/crm/records/deal", 1)
	hub.Publish("app/other/records/deal", 2)
	if got := <-exact; got.Seq != 1 || got.Data != 1 {
		t.Fatalf("exact got %+v", got)
	}
	if got := <-prefix; got.Data != 1 {
		t.Fatalf("prefix got %+v", got)
	}
	stopExact()
	stopExact()
	if len(prefix) != 0 {
		t.Fatal("other app leaked into prefix")
	}
}

func TestSlowSubscriberGetsAnErrorAndIsRemoved(t *testing.T) {
	hub := New(svc.Config{}, nil)
	slow, cancel := hub.Subscribe("app/crm/records/*")
	fast, stopFast := hub.Subscribe("app/crm/records/deal")
	defer stopFast()
	for i := 0; i <= subscriberBuffer; i++ {
		hub.Publish("app/crm/records/deal", i)
		if event := <-fast; event.Data != i || event.Seq != int64(i+1) {
			t.Fatal(event)
		}
	}
	terminal := <-slow
	err, ok := terminal.Data.(*bridge.Error)
	if !ok || err.Code != bridge.CodeConflict {
		t.Fatalf("terminal = %+v", terminal)
	}
	if _, open := <-slow; open {
		t.Fatal("slow subscriber remained open")
	}
	cancel()
	cancel()
	hub.mu.Lock()
	remaining := len(hub.subs)
	hub.mu.Unlock()
	if remaining != 1 {
		t.Fatalf("subscribers = %d", remaining)
	}
}

func TestPublishAndCancelAreSafeTogether(t *testing.T) {
	hub := New(svc.Config{}, nil)
	var wg sync.WaitGroup
	for range 100 {
		events, cancel := hub.Subscribe("app/crm/*")
		wg.Add(2)
		go func() {
			defer wg.Done()
			for range 100 {
				hub.Publish("app/crm/state", "state")
			}
		}()
		go func() {
			defer wg.Done()
			cancel()
			for range events {
			}
		}()
	}
	wg.Wait()
	hub.mu.Lock()
	remaining := len(hub.subs)
	hub.mu.Unlock()
	if remaining != 0 {
		t.Fatalf("subscribers = %d", remaining)
	}
}

func TestTopicMatchingAndSequence(t *testing.T) {
	for _, tt := range []struct {
		pattern, topic string
		want           bool
	}{
		{"app/crm/records/*", "app/crm/records/deal", true},
		{"app/crm/*", "app/crm/agent/thread", true},
		{"app/crm/*", "app/crm2/state", false},
		{"app/crm/*", "app/crm", false},
		{"app/crm/state", "app/crm/state", true},
		{"app/crm/state", "app/crm/build", false},
	} {
		t.Run(tt.pattern+tt.topic, func(t *testing.T) {
			if matches(tt.pattern, tt.topic) != tt.want {
				t.Fatal(tt)
			}
		})
	}
	hub := New(svc.Config{}, nil)
	events, cancel := hub.Subscribe("app/crm/*")
	defer cancel()
	hub.Publish("app/crm/state", 1)
	hub.Publish("app/crm/build", 2)
	hub.Publish("app/crm/state", 3)
	for _, want := range []int64{1, 1, 2} {
		if got := (<-events).Seq; got != want {
			t.Fatalf("seq=%d want %d", got, want)
		}
	}
}
