package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/credits"
)

const balanceKeyA, balanceKeyB = "sk-or-balance-key-a1a1a1", "sk-or-balance-key-b2b2b2"

// balanceFake is the provider, with no socket: it records who asked with which
// key and answers by path.
type balanceFake struct {
	mu    sync.Mutex
	asks  []string
	keys  []string
	reply func(r *http.Request) (*http.Response, error)
}

func (f *balanceFake) RoundTrip(r *http.Request) (*http.Response, error) {
	f.mu.Lock()
	f.asks = append(f.asks, r.URL.Path)
	f.keys = append(f.keys, strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	reply := f.reply
	f.mu.Unlock()
	return reply(r)
}

func (f *balanceFake) count() int { f.mu.Lock(); defer f.mu.Unlock(); return len(f.asks) }

func balanceBody(status int, body string) (*http.Response, error) {
	return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
}

func healthy(r *http.Request) (*http.Response, error) {
	if strings.HasSuffix(r.URL.Path, "/key") {
		return balanceBody(200, `{"data":{"limit_remaining":4}}`)
	}
	return balanceBody(200, `{"data":{"total_credits":10,"total_usage":2.5}}`)
}

type balanceClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *balanceClock) now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *balanceClock) add(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

// balanceBackend is a backend with a key for OpenRouter and the fake for a
// network, answering on a clock the test moves.
func balanceBackend(t *testing.T, reply func(*http.Request) (*http.Response, error)) (*appBackend, *balanceFake, *balanceClock) {
	t.Helper()
	t.Setenv(config.ProfileDirEnv, t.TempDir())
	for _, name := range []string{"OPENROUTER_API_KEY", "OPENAI_API_KEY", "DEEPSEEK_API_KEY"} {
		t.Setenv(name, "")
	}
	restore := config.UseAppCredentials(config.NewAppCredentials())
	t.Cleanup(restore)
	backend := newFakeBackend(newAppRecorder(), nil)
	t.Cleanup(func() { backend.Close() })
	fake := &balanceFake{reply: reply}
	clock := &balanceClock{t: time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)}
	ctx, stop := context.WithCancel(context.Background())
	backend.locked(func() {
		backend.balances = &appBalances{
			book: credits.NewBook(ctx, clock.now), stop: stop, ctx: ctx,
			client: &http.Client{Transport: fake}, address: func(string) string { return "https://balance.invalid/api/v1" },
		}
	})
	if err := backend.SetKey("openrouter", balanceKeyA); err != nil {
		t.Fatal(err)
	}
	return backend, fake, clock
}

func askBalance(t *testing.T, b *appBackend, provider string) appwire.ProviderBalance {
	t.Helper()
	r, err := b.ProviderBalance(appwire.ProviderBalanceAsk{Provider: provider})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(r)
	if len(raw) > appwire.ProviderBalanceReplyMax {
		t.Fatalf("reply is %d bytes", len(raw))
	}
	for _, secret := range []string{balanceKeyA, balanceKeyB, "SECRET-BODY", "balance.invalid"} {
		if strings.Contains(string(raw), secret) {
			t.Fatalf("the reply carries %q: %s", secret, raw)
		}
	}
	return r
}

func TestBalanceKnownCarriesBothFiguresAndTheLesser(t *testing.T) {
	b, fake, _ := balanceBackend(t, healthy)
	r := askBalance(t, b, "openrouter")
	if r.Status != appwire.BalanceKnown || !r.Supported || r.Name != "OpenRouter" || r.Stale || r.ErrorKind != "" {
		t.Fatalf("%+v", r)
	}
	if *r.AccountRemainingUSD != 7.5 || *r.KeyLimitRemainingUSD != 4 || *r.RemainingUSD != 4 {
		t.Fatalf("%v %v %v", *r.AccountRemainingUSD, *r.KeyLimitRemainingUSD, *r.RemainingUSD)
	}
	if r.LastSuccessAt != "2026-10-15T12:00:00Z" || r.LastErrorAt != "" || fake.count() != 2 {
		t.Fatalf("%+v asks %d", r, fake.count())
	}
	if fake.keys[0] != balanceKeyA {
		t.Fatal("the request did not carry the connected key")
	}
}

func TestBalanceNeverFabricatesAnAmountTheProviderDidNotSend(t *testing.T) {
	b, _, _ := balanceBackend(t, func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/key") {
			return balanceBody(200, `{"data":{"limit_remaining":null}}`)
		}
		return balanceBody(403, "SECRET-BODY")
	})
	r := askBalance(t, b, "openrouter")
	if r.Status != appwire.BalanceUnavailable || r.ErrorKind != "not_reported" || r.RemainingUSD != nil || r.AccountRemainingUSD != nil || r.KeyLimitRemainingUSD != nil {
		t.Fatalf("%+v", r)
	}
	b2, _, _ := balanceBackend(t, func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/key") {
			return balanceBody(200, `{"data":{"limit_remaining":0.75}}`)
		}
		return balanceBody(404, "")
	})
	r = askBalance(t, b2, "openrouter")
	if r.Status != appwire.BalanceKnown || r.AccountRemainingUSD != nil || *r.RemainingUSD != 0.75 {
		t.Fatalf("%+v", r)
	}
}

func TestBalanceNegativeIsHonestAndInvalidIsInvalid(t *testing.T) {
	b, _, _ := balanceBackend(t, func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/key") {
			return balanceBody(200, `{"data":{}}`)
		}
		return balanceBody(200, `{"data":{"total_credits":1,"total_usage":3.25}}`)
	})
	if r := askBalance(t, b, "openrouter"); *r.RemainingUSD != -2.25 || r.Status != appwire.BalanceKnown {
		t.Fatalf("%+v", r)
	}
	b2, _, _ := balanceBackend(t, func(r *http.Request) (*http.Response, error) {
		return balanceBody(200, `{"data": SECRET-BODY`)
	})
	r := askBalance(t, b2, "openrouter")
	if r.Status != appwire.BalanceInvalid || r.ErrorKind != "bad_response" || r.RemainingUSD != nil || r.LastErrorAt == "" {
		t.Fatalf("%+v", r)
	}
}

func TestBalanceOtherProvidersAreUnsupportedWithoutARequest(t *testing.T) {
	b, fake, _ := balanceBackend(t, healthy)
	if err := b.SetKey("deepseek", "sk-deepseek-balance-9f9f9f"); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"deepseek", "codex", "custom", "custom-openrouter", "openrouter.ai", "nowhere"} {
		r := askBalance(t, b, id)
		if r.Supported || r.Status != appwire.BalanceUnavailable || r.ErrorKind != "unsupported" || r.RemainingUSD != nil {
			t.Fatalf("%s: %+v", id, r)
		}
	}
	if fake.count() != 0 {
		t.Fatalf("%d requests for providers that report nothing", fake.count())
	}
}

func TestBalanceWithoutAKeyIsNotConnectedAndAsksNothing(t *testing.T) {
	b, fake, _ := balanceBackend(t, healthy)
	askBalance(t, b, "openrouter")
	before := fake.count()
	if err := b.RemoveKey("openrouter"); err != nil {
		t.Fatal(err)
	}
	r := askBalance(t, b, "openrouter")
	if r.Status != appwire.BalanceUnavailable || r.ErrorKind != "not_connected" || !r.Supported || r.RemainingUSD != nil || fake.count() != before {
		t.Fatalf("%+v", r)
	}
	if b.balances.book.Len() != 0 {
		t.Fatal("a disconnected provider's balance is still held")
	}
}

func TestBalanceIsAskedOnceAMinutePerAccountAndSaysWhenNext(t *testing.T) {
	b, fake, clock := balanceBackend(t, healthy)
	askBalance(t, b, "openrouter")
	clock.add(45 * time.Second)
	r := askBalance(t, b, "openrouter")
	if fake.count() != 2 || r.RetryAfterMS != 15000 || r.Status != appwire.BalanceKnown {
		t.Fatalf("asks %d %+v", fake.count(), r)
	}
	clock.add(15 * time.Second)
	if r := askBalance(t, b, "openrouter"); fake.count() != 4 || r.RetryAfterMS != 60000 {
		t.Fatalf("asks %d %+v", fake.count(), r)
	}
}

func TestBalanceConcurrentAsksShareOneRequest(t *testing.T) {
	release := make(chan struct{})
	b, fake, _ := balanceBackend(t, func(r *http.Request) (*http.Response, error) {
		<-release
		return healthy(r)
	})
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if r := askBalance(t, b, "openrouter"); r.Status != appwire.BalanceKnown {
				t.Errorf("%+v", r)
			}
		}()
	}
	for fake.count() == 0 {
		time.Sleep(time.Millisecond)
	}
	time.Sleep(30 * time.Millisecond)
	close(release)
	wg.Wait()
	if fake.count() != 2 {
		t.Fatalf("%d requests: the key row and the credits row, once", fake.count())
	}
}

func TestBalanceKeepsLastGoodThroughATransientFailureButNotARejectedKey(t *testing.T) {
	var fail atomicStatus
	b, _, clock := balanceBackend(t, func(r *http.Request) (*http.Response, error) {
		switch fail.get() {
		case 0:
			return healthy(r)
		case 503:
			return balanceBody(503, "SECRET-BODY")
		}
		return balanceBody(401, `{"error":{"message":"SECRET-BODY"}}`)
	})
	askBalance(t, b, "openrouter")
	fail.set(503)
	clock.add(time.Minute)
	r := askBalance(t, b, "openrouter")
	if r.Status != appwire.BalanceUnreachable || r.ErrorKind != "provider_error" || !r.Stale || *r.RemainingUSD != 4 || r.LastSuccessAt == "" || r.LastErrorAt == "" {
		t.Fatalf("%+v", r)
	}
	fail.set(401)
	clock.add(time.Minute)
	r = askBalance(t, b, "openrouter")
	if r.Status != appwire.BalanceUnauthorized || r.ErrorKind != "key_rejected" || r.RemainingUSD != nil || r.Stale || r.Message == "" {
		t.Fatalf("%+v", r)
	}
	fail.set(503)
	clock.add(time.Minute)
	if r := askBalance(t, b, "openrouter"); r.RemainingUSD != nil {
		t.Fatalf("a rejected key's old figures came back: %+v", r)
	}
}

type atomicStatus struct {
	mu sync.Mutex
	v  int
}

func (a *atomicStatus) get() int  { a.mu.Lock(); defer a.mu.Unlock(); return a.v }
func (a *atomicStatus) set(v int) { a.mu.Lock(); a.v = v; a.mu.Unlock() }

func TestBalanceExpiredKeyIsUnauthorizedWithNoFigures(t *testing.T) {
	b, _, _ := balanceBackend(t, func(*http.Request) (*http.Response, error) {
		return balanceBody(401, `{"error":{"message":"API key expired."}}`)
	})
	r := askBalance(t, b, "openrouter")
	if r.Status != appwire.BalanceUnauthorized || r.ErrorKind != "key_expired" || r.RemainingUSD != nil || r.LastSuccessAt == "" {
		t.Fatalf("%+v", r)
	}
}

func TestBalanceAChangedKeyIsAskedAtOnceAndNeverShowsTheOldAccount(t *testing.T) {
	b, fake, _ := balanceBackend(t, func(r *http.Request) (*http.Response, error) {
		if strings.Contains(r.Header.Get("Authorization"), "b2b2") {
			return balanceBody(500, "")
		}
		return healthy(r)
	})
	if r := askBalance(t, b, "openrouter"); *r.RemainingUSD != 4 {
		t.Fatalf("%+v", r)
	}
	if err := b.SetKey("openrouter", balanceKeyB); err != nil {
		t.Fatal(err)
	}
	// No time has passed, and the minute does not protect a new account.
	r := askBalance(t, b, "openrouter")
	if fake.keys[len(fake.keys)-1] != balanceKeyB || r.RemainingUSD != nil || r.Stale || r.LastSuccessAt != "" || r.Status != appwire.BalanceUnreachable {
		t.Fatalf("%+v keys %v", r, fake.keys)
	}
	if b.balances.book.Len() != 1 {
		t.Fatal("two accounts held for one provider")
	}
}

func TestBalanceRedirectIsNeverFollowedWithTheKey(t *testing.T) {
	b, fake, _ := balanceBackend(t, func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 302, Header: http.Header{"Location": {"https://elsewhere.invalid/key"}}, Body: io.NopCloser(strings.NewReader(""))}, nil
	})
	r := askBalance(t, b, "openrouter")
	if fake.count() != 1 || r.Status != appwire.BalanceInvalid || r.ErrorKind != "unexpected_status" {
		t.Fatalf("asks %v %+v", fake.asks, r)
	}
}

func TestBalanceCloseEndsARequestInFlight(t *testing.T) {
	started := make(chan struct{})
	b, _, _ := balanceBackend(t, func(r *http.Request) (*http.Response, error) {
		close(started)
		<-r.Context().Done()
		return nil, r.Context().Err()
	})
	done := make(chan error, 1)
	go func() {
		_, err := b.ProviderBalance(appwire.ProviderBalanceAsk{Provider: "openrouter"})
		done <- err
	}()
	<-started
	b.Close()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("a closed backend answered a balance")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Close left a balance request running")
	}
}
