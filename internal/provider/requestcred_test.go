package provider

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// The keys here are this file's own literals and the service is an in-process
// handler: nothing leaves the test.

const (
	requestCredDomain = "crew-credential\x00openrouter"
	requestCredKeyA   = "fixture-request-cred-a"
	requestCredKeyB   = "fixture-request-cred-b"
	requestCredOwn    = "fixture-request-cred-own"
)

// requestCredClient is a direct adapter on handler with key A, whose retry
// wait passes no time, and the bearer of every request handler received.
func requestCredClient(t *testing.T, handler func(attempt int, bearer string, w http.ResponseWriter)) (*Client, func() []string) {
	t.Helper()
	oldLimiter := sharedLimiter
	sharedLimiter = newAdaptiveLimiter()
	t.Cleanup(func() { sharedLimiter = oldLimiter })
	var mu sync.Mutex
	var bearers []string
	client, err := NewClient(Config{
		APIKey: requestCredKeyA, BaseURL: "https://direct.example/v1", Model: "glm-5.3-flash", Direct: true,
		HTTPClient: handlerClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			bearer := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			mu.Lock()
			bearers = append(bearers, bearer)
			attempt := len(bearers)
			mu.Unlock()
			handler(attempt, bearer, w)
		})),
	})
	if err != nil {
		t.Fatal(err)
	}
	client.wait = func(context.Context, time.Duration) error { return nil }
	return client, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), bearers...)
	}
}

// EVERY ATTEMPT IS TOLD THE BEARER ITS OWN HEADER CARRIED. The first attempt
// leaves with key A and is paced; while it is in flight the live adapter is
// handed key B; the dispatcher's next attempt is refused. The observer is told
// one digest per attempt, in order, each the digest of the bearer that
// attempt's header actually carried — so the last one told is B's, the key the
// refused request was sent with, never A's from when the call began.
func TestEachAttemptIsToldTheBearerItsHeaderCarried(t *testing.T) {
	var client *Client
	client, received := requestCredClient(t, func(attempt int, _ string, w http.ResponseWriter) {
		if attempt == 1 {
			if err := client.SetAPIKey(requestCredKeyB); err != nil {
				t.Error(err)
			}
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"message":"too many requests"}`))
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"No auth credentials found","code":401}}`))
	})
	var told []RequestCredential
	ctx := client.ObserveRequestCredential(context.Background(), requestCredDomain, func(seen RequestCredential) {
		told = append(told, seen)
	})
	if _, err := client.CompleteWithMessages(ctx, userMessages("hello")); err == nil {
		t.Fatal("the refused attempt answered")
	}
	bearers := received()
	if len(bearers) != 2 || bearers[0] != requestCredKeyA || bearers[1] != requestCredKeyB {
		t.Fatalf("the service received %d requests; the fixture did not rotate between them", len(bearers))
	}
	if len(told) != len(bearers) {
		t.Fatalf("told %d attempts for %d requests", len(told), len(bearers))
	}
	for i, bearer := range bearers {
		if want := CredentialDigest(requestCredDomain, bearer); told[i].Digest != want || told[i].Override {
			t.Fatalf("attempt %d was told %+v, want the digest of its own header %q", i+1, told[i], want)
		}
	}
	if last := told[len(told)-1].Digest; last == CredentialDigest(requestCredDomain, requestCredKeyA) || last == "" {
		t.Fatalf("the refused attempt was told %q, the key the call began with", last)
	}
}

// A BEARER THE REQUEST NAMED ITSELF IS SAID TO BE ITS OWN, and a request
// another adapter writes under the same context is not reported at all.
func TestAnOverrideIsMarkedAndAnotherAdapterIsNotReported(t *testing.T) {
	ok := func(_ int, _ string, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"ok"}}]}`))
	}
	client, received := requestCredClient(t, ok)
	other, _ := requestCredClient(t, ok)
	var told []RequestCredential
	ctx := client.ObserveRequestCredential(context.Background(), requestCredDomain, func(seen RequestCredential) {
		told = append(told, seen)
	})
	if _, err := client.CompleteWithMessages(ctx, userMessages("hello"), ai.WithAPIKey(requestCredOwn)); err != nil {
		t.Fatal(err)
	}
	if bearers := received(); len(bearers) != 1 || bearers[0] != requestCredOwn {
		t.Fatal("the request did not carry its own bearer")
	}
	if len(told) != 1 || !told[0].Override || told[0].Digest != CredentialDigest(requestCredDomain, requestCredOwn) {
		t.Fatalf("the override was told %+v", told)
	}
	if _, err := other.CompleteWithMessages(ctx, userMessages("hello")); err != nil {
		t.Fatal(err)
	}
	if len(told) != 1 {
		t.Fatalf("another adapter's request was reported: %+v", told)
	}
	if CredentialDigest("", requestCredKeyA) != "" || CredentialDigest(requestCredDomain, "") != "" {
		t.Fatal("a digest with no domain or no secret named something")
	}
}
