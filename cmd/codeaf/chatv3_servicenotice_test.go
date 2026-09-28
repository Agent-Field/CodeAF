package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/Agent-Field/codeaf/internal/catalog"
	"github.com/Agent-Field/codeaf/internal/modelsource"
)

func TestProviderNoticesUnsubscribeWithoutHoldingTheProcessLock(t *testing.T) {
	p := &v3Process{}
	calls := 0
	var remove func()
	remove = p.registerServiceNotice(func(string, string) { calls++; remove() })
	p.noteServiceModelsTo("service", "address")
	p.noteServiceModelsTo("service", "address")
	remove()
	if calls != 1 || len(p.serviceNotices) != 0 {
		t.Fatalf("calls=%d subscriptions=%d", calls, len(p.serviceNotices))
	}
}

func TestDefaultProviderRefreshNotifiesOnSuccessAndFailure(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	for _, fail := range []bool{false, true} {
		options := catalog.Options{BaseURL: "https://example.invalid/v1", Dir: t.TempDir(), HTTPClient: shelfRouter(`{"id":"openai/new","context_length":8192}`, nil)}
		launch := catalog.Load(t.Context(), options)
		if fail {
			options.HTTPClient = shelfRouter("", errors.New("offline"))
		}
		p := &v3Process{Shelf: newV3ModelShelf(launch, options)}
		p.Shelf.setSources(modelsource.NewSet(modelsource.Connected{Source: modelsource.DefaultSource(options.BaseURL), Address: options.BaseURL}))
		calls := 0
		remove := p.registerServiceNotice(func(source, address string) {
			calls++
			if source != modelsource.DefaultID || address != options.BaseURL {
				t.Errorf("wrong landing %q %q", source, address)
			}
		})
		p.refreshAllModels(t.Context())
		remove()
		if calls != 1 {
			t.Fatalf("failure=%v: got%d notices", fail, calls)
		}
		if (p.Shelf.fetchErrorFor(modelsource.DefaultID) != "") != fail {
			t.Fatal("default error state not delivered")
		}
	}
}

func TestProviderWarmReportsFailureBeforeTheNextProviderFinishes(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "refused", http.StatusForbidden) }))
	defer first.Close()
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		once.Do(func() { close(entered) })
		<-release
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"fresh"}]}`))
	}))
	defer second.Close()
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer unblock()
	options := catalog.Options{Dir: t.TempDir(), HTTPClient: shelfRouter(`{"id":"default"}`, nil)}
	shelf := newV3ModelShelf(catalog.Load(t.Context(), options), options)
	makeService := func(id, address string) modelsource.Connected {
		return modelsource.Connected{Source: modelsource.Source{ID: id, Written: id, Listing: modelsource.ListingModels}, Address: address, Key: "synthetic"}
	}
	shelf.setSources(modelsource.NewSet(makeService(modelsource.DefaultID, "https://example.invalid"), makeService("custom:first", first.URL), makeService("custom:second", second.URL)))
	notices := make(chan string, 2)
	p := &v3Process{Shelf: shelf}
	remove := p.registerServiceNotice(func(source, address string) { notices <- source })
	defer remove()
	done := make(chan struct{})
	go func() { p.warmEmptyProviders(context.Background()); close(done) }()
	<-entered
	select {
	case got := <-notices:
		if got != "custom:first" {
			t.Fatalf("first notice %q", got)
		}
	default:
		t.Fatal("failed provider waited for the next network request")
	}
	unblock()
	<-done
	if got := <-notices; got != "custom:second" {
		t.Fatalf("second notice %q", got)
	}
	if shelf.fetchErrorFor("custom:first") == "" {
		t.Fatal("failed provider's reason missing")
	}
}
