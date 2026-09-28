package tui3

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"testing"

	"github.com/Agent-Field/codeaf/internal/modelsource"
)

func TestProviderLandingsKeepEveryConcurrentPair(t *testing.T) {
	var desk serviceLands
	var writers sync.WaitGroup
	for i := 0; i < 16; i++ {
		writers.Add(1)
		go func(i int) {
			defer writers.Done()
			for j := 0; j < 64; j++ {
				desk.put(fmt.Sprint(i), fmt.Sprint(j))
				runtime.Gosched()
			}
		}(i)
	}
	done := make(chan struct{})
	go func() { writers.Wait(); close(done) }()
	seen := map[[2]string]bool{}
	for {
		for _, pair := range desk.take() {
			seen[pair] = true
		}
		select {
		case <-done:
			for _, pair := range desk.take() {
				seen[pair] = true
			}
			if len(seen) != 16*64 {
				t.Fatalf("retained %d pairs, want1024", len(seen))
			}
			return
		default:
			runtime.Gosched()
		}
	}
}

func TestProviderSubscriptionRestocksTheOpenPickerAndCloses(t *testing.T) {
	door := &fakeRefresh{}
	a := refreshApp(t, door)
	typeLine(t, a, "/model")
	var notify func(string, string)
	removed := false
	closeNotice := listenForServiceModels(a, func(tell func(string, string)) func() {
		notify = tell
		return func() { removed = true }
	})
	defer closeNotice()
	door.list = []Model{{ID: "openai/newly-listed"}}
	notify(modelsource.DefaultID, "https://example.invalid/v1")
	msg := a.landedBell.waitRing()()
	a.Update(msg)
	if !a.pick.open || len(a.pick.all) != 2 || a.pick.all[0].ID != "openai/newly-listed" {
		t.Fatalf("picker did not restock: %+v", a.pick.all)
	}
	closeNotice()
	notify("late", "late")
	if !removed || len(a.serviceLands.take()) != 0 {
		t.Fatal("closed surface retained a callback or late landing")
	}
	if msg := a.landedBell.waitRing()(); msg != nil {
		t.Fatalf("closed bell answered %T", msg)
	}
}

func TestAllProviderRefreshRestocksWithoutASubscription(t *testing.T) {
	door := &fakeRefresh{}
	a := refreshApp(t, door)
	a.refreshAllModels = func(context.Context) { door.list = []Model{{ID: "openai/refreshed"}} }
	typeLine(t, a, "/model")
	fetch := pressRefresh(a)
	if fetch == nil {
		t.Fatal("refresh unavailable")
	}
	a.Update(fetch())
	if a.modelsFetching || !a.pick.open || len(a.pick.all) != 2 || a.pick.all[0].ID != "openai/refreshed" {
		t.Fatalf("all-provider completion left stale picker: %+v", a.pick.all)
	}
}
