//go:build !windows

package bus

import (
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"
)

func sequenceIDs() func() string {
	var mu sync.Mutex
	next := 0
	return func() string {
		mu.Lock()
		defer mu.Unlock()
		next++
		return fmt.Sprintf("evt_%d", next)
	}
}

func TestPublishOrderUnsubscribeAndPanicIsolation(t *testing.T) {
	b := New(
		Context{Directory: "/repo", Project: "p", Workspace: "w"},
		WithIDGenerator(sequenceIDs()),
	)
	def := Define("test.order", nil)
	var got []string
	b.SubscribeCallback(def, func(Payload) { got = append(got, "typed-1") })
	b.SubscribeCallback(def, func(Payload) { panic("subscriber failed") })
	unsubscribe := b.SubscribeCallback(def, func(Payload) { got = append(got, "typed-3") })
	b.SubscribeAllCallback(func(Payload) { got = append(got, "all-1") })

	b.Publish(def, map[string]any{"x": float64(1)}, PublishOptions{ID: "fixed"})
	want := []string{"typed-1", "typed-3", "all-1"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("delivery order: %v, want %v", got, want)
	}
	unsubscribe()
	unsubscribe()
	got = nil
	b.Publish(def, nil)
	want = []string{"typed-1", "all-1"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("after unsubscribe: %v, want %v", got, want)
	}
}

func TestSnapshotSemanticsForMutationDuringPublish(t *testing.T) {
	b := New(Context{}, WithIDGenerator(sequenceIDs()))
	def := Define("test.snapshot", nil)
	var got []string
	var unsubscribeSecond func()
	b.SubscribeCallback(def, func(Payload) {
		got = append(got, "first")
		unsubscribeSecond()
		b.SubscribeCallback(def, func(Payload) { got = append(got, "late") })
	})
	unsubscribeSecond = b.SubscribeCallback(def, func(Payload) { got = append(got, "second") })
	b.Publish(def, nil)
	if want := []string{"first", "second"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("first publish: %v", got)
	}
	got = nil
	b.Publish(def, nil)
	if want := []string{"first", "late"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("second publish: %v", got)
	}
}

func TestDisposeOnlyNotifiesWildcardAndClosesStreams(t *testing.T) {
	b := New(Context{Directory: "/d"}, WithIDGenerator(sequenceIDs()))
	var typed []Payload
	var all []Payload
	b.SubscribeCallback(InstanceDisposed, func(event Payload) { typed = append(typed, event) })
	b.SubscribeAllCallback(func(event Payload) { all = append(all, event) })
	stream := b.SubscribeAll()

	b.Dispose()
	if len(typed) != 0 {
		t.Fatalf("typed disposed subscriber was called: %v", typed)
	}
	if len(all) != 1 || all[0].Type != InstanceDisposed.Type {
		t.Fatalf("wildcard disposed events: %v", all)
	}
	properties := all[0].Properties.(map[string]any)
	if properties["directory"] != "/d" {
		t.Fatalf("disposed properties: %v", properties)
	}
	select {
	case event := <-stream.C:
		if event.Type != InstanceDisposed.Type {
			t.Fatalf("stream event: %+v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for disposal event")
	}
	select {
	case _, ok := <-stream.C:
		if ok {
			t.Fatal("stream remained open")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for stream close")
	}
	b.Publish(InstanceDisposed, nil)
	if len(all) != 1 {
		t.Fatalf("publish after dispose delivered: %v", all)
	}
}

func TestConcurrentPublishIsSafeAndComplete(t *testing.T) {
	b := New(Context{}, WithIDGenerator(sequenceIDs()))
	def := Define("test.concurrent", nil)
	var mu sync.Mutex
	count := 0
	b.SubscribeCallback(def, func(Payload) {
		mu.Lock()
		count++
		mu.Unlock()
	})
	var wg sync.WaitGroup
	for range 100 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			b.Publish(def, nil)
		}()
	}
	wg.Wait()
	if count != 100 {
		t.Fatalf("count = %d", count)
	}
}
