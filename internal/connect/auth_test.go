package connect

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

// onFreePort keeps a test off the fixed addresses the shipped program prefers,
// which another process on the machine may already hold.
func onFreePort(t *testing.T) {
	t.Helper()
	saved := localServerAddresses
	localServerAddresses = []string{"127.0.0.1:0"}
	t.Cleanup(func() { localServerAddresses = saved })
}

// loopback rewrites an address to the numeric loopback host, so that a test
// cannot fail over the name "localhost" resolving to an address family the
// listener is not on.
func loopback(t *testing.T, raw string) *url.URL {
	t.Helper()
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse %q: %v", raw, err)
	}
	parsed.Host = "127.0.0.1:" + parsed.Port()
	return parsed
}

// TestBeginAuthAndWait walks the whole round-trip: the address is ready before
// anyone waits, the outgoing request carries a proof key and asks for a durable
// grant, the browser's return lands on the loopback listener, and what comes
// back is a connection written to the store with the account's own address on
// it.
func TestBeginAuthAndWait(t *testing.T) {
	onFreePort(t)

	var mu sync.Mutex
	var exchange url.Values
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse exchange: %v", err)
		}
		mu.Lock()
		exchange = r.Form
		mu.Unlock()
		writeJSON(t, w, map[string]any{
			"access_token":  "access-one",
			"refresh_token": "refresh-one",
			"token_type":    "Bearer",
			"expires_in":    3600,
		})
	})
	mux.HandleFunc("/gmail/v1/users/me/profile", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer access-one" {
			t.Errorf("the identity probe must act for the new account, got %q", got)
		}
		writeJSON(t, w, map[string]any{"emailAddress": "me@example.com"})
	})
	fakeService(t, mux)

	manager, _ := testManager(t)
	ctx := context.Background()

	flow, err := manager.BeginAuth(ctx, "google", "")
	if err != nil {
		t.Fatalf("BeginAuth: %v", err)
	}
	defer flow.Cancel()
	if flow.URL() == "" {
		t.Fatal("the address must be ready before anyone waits on the flow")
	}

	// Follow the one hop the loopback address makes, but stop there so the
	// outgoing request can be read.
	stopAtRedirect := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	hop, err := stopAtRedirect.Get(loopback(t, flow.URL()).String())
	if err != nil {
		t.Fatalf("open the address: %v", err)
	}
	_ = hop.Body.Close()
	if hop.StatusCode != http.StatusFound {
		t.Fatalf("the address must send the browser on, got %s", hop.Status)
	}
	outgoing := loopback(t, hop.Header.Get("Location"))
	if !strings.HasSuffix(outgoing.Path, "/auth") {
		t.Fatalf("the browser must be sent to the service, got %s", outgoing)
	}

	query := outgoing.Query()
	challenge := query.Get("code_challenge")
	if challenge == "" || query.Get("code_challenge_method") != "S256" {
		t.Errorf("the request must carry a hashed proof key, got %v", query)
	}
	if query.Get("access_type") != "offline" {
		t.Errorf("the request must ask for a durable grant, got %q", query.Get("access_type"))
	}
	if query.Get("prompt") != "consent" {
		t.Errorf("the request must force a fresh grant, got %q", query.Get("prompt"))
	}
	for _, scope := range (google{}).Service().Scopes {
		if !strings.Contains(query.Get("scope"), scope) {
			t.Errorf("the request must ask for %s, got %q", scope, query.Get("scope"))
		}
	}
	state := query.Get("state")
	if state == "" {
		t.Error("the request must carry a state parameter")
	}
	returnTo := loopback(t, query.Get("redirect_uri"))
	if returnTo.Hostname() != "127.0.0.1" {
		t.Errorf("the browser must come back to this machine, got %s", returnTo)
	}

	// Play the browser's part: come back to the loopback listener with a
	// code, exactly as the service would.
	back := *returnTo
	back.RawQuery = url.Values{"code": {"the-code"}, "state": {state}}.Encode()
	page, err := http.Get(back.String())
	if err != nil {
		t.Fatalf("return to the loopback address: %v", err)
	}
	body, _ := io.ReadAll(page.Body)
	_ = page.Body.Close()
	if page.StatusCode != http.StatusOK {
		t.Fatalf("the return must be accepted, got %s", page.Status)
	}
	if !strings.Contains(string(body), "Connected.") || !strings.Contains(string(body), "close this tab") {
		t.Errorf("the page must tell the person they are done, got %q", body)
	}

	status, err := flow.Wait(ctx)
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if !status.Connected || status.Account != "me@example.com" || status.ID != "google" {
		t.Errorf("Wait: got %+v", status)
	}

	mu.Lock()
	verifier := exchange.Get("code_verifier")
	code := exchange.Get("code")
	mu.Unlock()
	if code != "the-code" {
		t.Errorf("the exchange must carry the code the browser brought back, got %q", code)
	}
	if verifier == "" {
		t.Fatal("the exchange must carry the proof key")
	}
	sum := sha256.Sum256([]byte(verifier))
	if got := base64.RawURLEncoding.EncodeToString(sum[:]); got != challenge {
		t.Errorf("the proof key does not match what was sent out: %q vs %q", got, challenge)
	}

	if !manager.Connected("google") {
		t.Error("the connection must be on disk once Wait has returned")
	}
	entry, ok, err := manager.store.get("google")
	if err != nil || !ok {
		t.Fatalf("store after Wait: ok=%v err=%v", ok, err)
	}
	if entry.Keys.RefreshToken != "refresh-one" || entry.Account != "me@example.com" {
		t.Errorf("store after Wait: account=%q", entry.Account)
	}

	// A second Wait answers with what the first one found rather than
	// starting anything new.
	again, err := flow.Wait(ctx)
	if err != nil || again.Account != status.Account || !again.Connected {
		t.Errorf("Wait twice: got %+v, %v", again, err)
	}
}

// TestWaitToleratesAnUnknownAccount holds the emptiness law: the identity probe
// failing costs the label and nothing else.
func TestWaitToleratesAnUnknownAccount(t *testing.T) {
	onFreePort(t)

	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, map[string]any{
			"access_token": "access-one", "refresh_token": "refresh-one",
			"token_type": "Bearer", "expires_in": 3600,
		})
	})
	mux.HandleFunc("/gmail/v1/users/me/profile", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"message":"having a bad minute"}}`, http.StatusInternalServerError)
	})
	fakeService(t, mux)

	manager, _ := testManager(t)
	flow, err := manager.BeginAuth(context.Background(), "google", "")
	if err != nil {
		t.Fatalf("BeginAuth: %v", err)
	}
	defer flow.Cancel()
	drive(t, flow, "the-code")

	status, err := flow.Wait(context.Background())
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if !status.Connected {
		t.Error("a working connection must survive an unanswerable identity probe")
	}
	if status.Account != "" {
		t.Errorf("an unknown account renders as nothing, got %q", status.Account)
	}
}

// TestWaitReportsARefusal covers the person deciding not to grant anything.
func TestWaitReportsARefusal(t *testing.T) {
	onFreePort(t)
	fakeService(t, http.NewServeMux())

	manager, _ := testManager(t)
	flow, err := manager.BeginAuth(context.Background(), "google", "")
	if err != nil {
		t.Fatalf("BeginAuth: %v", err)
	}
	defer flow.Cancel()

	back := loopback(t, flow.URL())
	back.RawQuery = url.Values{"error": {"access_denied"}}.Encode()
	if response, err := http.Get(back.String()); err == nil {
		_ = response.Body.Close()
	}

	status, err := flow.Wait(context.Background())
	if err == nil {
		t.Fatal("a refusal must be an error")
	}
	if status.Connected {
		t.Error("a refusal must not read as connected")
	}
	if manager.Connected("google") {
		t.Error("a refusal must write nothing to the store")
	}
}

// TestWaitEndsWithTheContext proves the caller can walk away.
func TestWaitEndsWithTheContext(t *testing.T) {
	onFreePort(t)
	fakeService(t, http.NewServeMux())

	manager, _ := testManager(t)
	flow, err := manager.BeginAuth(context.Background(), "google", "")
	if err != nil {
		t.Fatalf("BeginAuth: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := flow.Wait(ctx); err == nil {
		t.Fatal("Wait must end when its context does")
	}
	if manager.Connected("google") {
		t.Error("an abandoned connection must write nothing")
	}
}

// TestBeginAuthOutlivesItsContext holds the law that the round-trip is not tied
// to the call that started it.
func TestBeginAuthOutlivesItsContext(t *testing.T) {
	onFreePort(t)

	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, map[string]any{
			"access_token": "a", "refresh_token": "r",
			"token_type": "Bearer", "expires_in": 3600,
		})
	})
	mux.HandleFunc("/gmail/v1/users/me/profile", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, map[string]any{"emailAddress": "me@example.com"})
	})
	fakeService(t, mux)

	manager, _ := testManager(t)
	starting, stop := context.WithCancel(context.Background())
	flow, err := manager.BeginAuth(starting, "google", "")
	if err != nil {
		t.Fatalf("BeginAuth: %v", err)
	}
	defer flow.Cancel()
	stop()

	drive(t, flow, "the-code")
	status, err := flow.Wait(context.Background())
	if err != nil {
		t.Fatalf("Wait after the starting context ended: %v", err)
	}
	if !status.Connected {
		t.Error("the round-trip must survive the context that started it")
	}
}

// drive plays the browser's part: follow the loopback address to the service,
// then come back to the listener with a code.
func drive(t *testing.T, flow *Flow, code string) {
	t.Helper()
	stopAtRedirect := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	hop, err := stopAtRedirect.Get(loopback(t, flow.URL()).String())
	if err != nil {
		t.Fatalf("open the address: %v", err)
	}
	_ = hop.Body.Close()
	outgoing := loopback(t, hop.Header.Get("Location"))
	back := loopback(t, outgoing.Query().Get("redirect_uri"))
	back.RawQuery = url.Values{
		"code":  {code},
		"state": {outgoing.Query().Get("state")},
	}.Encode()
	page, err := http.Get(back.String())
	if err != nil {
		t.Fatalf("return to the loopback address: %v", err)
	}
	_, _ = io.Copy(io.Discard, page.Body)
	_ = page.Body.Close()
}

// TestWaitRecordsWhatWasGranted holds the record kept beside the keys: the
// service's own word for what it agreed to, and — when it says nothing — what
// the sign-in asked for.
func TestWaitRecordsWhatWasGranted(t *testing.T) {
	for _, c := range []struct {
		name      string
		said      string
		want      []string
		connected bool
	}{
		{
			name:      "the service names a narrower set than was asked for",
			said:      "https://www.googleapis.com/auth/gmail.modify",
			want:      []string{"https://www.googleapis.com/auth/gmail.modify"},
			connected: false,
		},
		{
			name:      "the service says nothing",
			want:      wholeGrant(),
			connected: true,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			onFreePort(t)
			mux := http.NewServeMux()
			mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
				answer := map[string]any{
					"access_token": "access-one", "refresh_token": "refresh-one",
					"token_type": "Bearer", "expires_in": 3600,
				}
				if c.said != "" {
					answer["scope"] = c.said
				}
				writeJSON(t, w, answer)
			})
			mux.HandleFunc("/gmail/v1/users/me/profile", func(w http.ResponseWriter, r *http.Request) {
				writeJSON(t, w, map[string]any{"emailAddress": "me@example.com"})
			})
			fakeService(t, mux)

			manager, _ := testManager(t)
			flow, err := manager.BeginAuth(context.Background(), "google", "")
			if err != nil {
				t.Fatalf("BeginAuth: %v", err)
			}
			defer flow.Cancel()
			drive(t, flow, "the-code")
			if _, err := flow.Wait(context.Background()); err != nil {
				t.Fatalf("Wait: %v", err)
			}

			entry, ok, err := manager.store.get("google")
			if err != nil || !ok {
				t.Fatalf("store after Wait: ok=%v err=%v", ok, err)
			}
			if strings.Join(entry.Scopes, " ") != strings.Join(c.want, " ") {
				t.Errorf("recorded %v, want %v", entry.Scopes, c.want)
			}
			if got := manager.Connected("google"); got != c.connected {
				t.Errorf("Connected = %v, want %v", got, c.connected)
			}
		})
	}
}

// TestClientPersistsARotatedKey is the law that a renewal is written down: the
// service hands back a new long-lived key, and the store must have it before
// the process ends.
func TestClientPersistsARotatedKey(t *testing.T) {
	var mu sync.Mutex
	renewals := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		renewals++
		mu.Unlock()
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse renewal: %v", err)
		}
		if got := r.Form.Get("refresh_token"); got != "refresh-one" {
			t.Errorf("the renewal must present the stored key, got %q", got)
		}
		writeJSON(t, w, map[string]any{
			"access_token":  "access-two",
			"refresh_token": "refresh-two",
			"token_type":    "Bearer",
			"expires_in":    3600,
		})
	})
	mux.HandleFunc("/gmail/v1/users/me/profile", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer access-two" {
			t.Errorf("the request must carry the renewed key, got %q", got)
		}
		writeJSON(t, w, map[string]any{"emailAddress": "me@example.com"})
	})
	fakeService(t, mux)

	manager, _ := testManager(t)
	if err := manager.store.put("google", stored{
		Account: "me@example.com",
		Scopes:  wholeGrant(),
		Keys: &oauth2.Token{
			AccessToken:  "access-one",
			RefreshToken: "refresh-one",
			TokenType:    "Bearer",
			Expiry:       time.Now().Add(-time.Hour),
		},
	}); err != nil {
		t.Fatalf("put: %v", err)
	}

	ctx := context.Background()
	client, err := manager.Client(ctx, "google")
	if err != nil {
		t.Fatalf("Client: %v", err)
	}
	if _, err := (google{}).Account(ctx, client); err != nil {
		t.Fatalf("first request: %v", err)
	}

	entry, ok, err := manager.store.get("google")
	if err != nil || !ok {
		t.Fatalf("store after a renewal: ok=%v err=%v", ok, err)
	}
	if entry.Keys.AccessToken != "access-two" || entry.Keys.RefreshToken != "refresh-two" {
		t.Errorf("the rotated keys were not written down: %+v", entry.Keys)
	}
	if entry.Account != "me@example.com" {
		t.Errorf("a renewal must not lose the account, got %q", entry.Account)
	}
	// Nor the record of what the person agreed to: a renewal that dropped it
	// would send somebody back through a sign-in they already finished.
	if strings.Join(entry.Scopes, " ") != strings.Join(wholeGrant(), " ") {
		t.Errorf("a renewal must not lose the permissions, got %v", entry.Scopes)
	}

	// A second request reuses what is in hand.
	if _, err := (google{}).Account(ctx, client); err != nil {
		t.Fatalf("second request: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if renewals != 1 {
		t.Errorf("renewals: got %d, want 1", renewals)
	}
}

// TestClientCarriesTheKeyForward covers the common case of a service renewing
// without issuing a new long-lived key: the old one must survive, or the
// connection dies at the next restart.
func TestClientCarriesTheKeyForward(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, map[string]any{
			"access_token": "access-two",
			"token_type":   "Bearer",
			"expires_in":   3600,
		})
	})
	mux.HandleFunc("/gmail/v1/users/me/profile", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, map[string]any{"emailAddress": "me@example.com"})
	})
	fakeService(t, mux)

	manager, _ := testManager(t)
	if err := manager.store.put("google", stored{
		Account: "me@example.com",
		Scopes:  wholeGrant(),
		Keys: &oauth2.Token{
			AccessToken:  "access-one",
			RefreshToken: "refresh-one",
			TokenType:    "Bearer",
			Expiry:       time.Now().Add(-time.Hour),
		},
	}); err != nil {
		t.Fatalf("put: %v", err)
	}
	ctx := context.Background()
	client, err := manager.Client(ctx, "google")
	if err != nil {
		t.Fatalf("Client: %v", err)
	}
	if _, err := (google{}).Account(ctx, client); err != nil {
		t.Fatalf("request: %v", err)
	}
	entry, _, err := manager.store.get("google")
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	if entry.Keys.RefreshToken != "refresh-one" {
		t.Errorf("the long-lived key must survive a renewal that omitted one, got %q", entry.Keys.RefreshToken)
	}
	if entry.Keys.AccessToken != "access-two" {
		t.Errorf("the renewed key must be written down, got %q", entry.Keys.AccessToken)
	}
}

func TestSameKeys(t *testing.T) {
	moment := time.Now().Round(time.Second)
	one := &oauth2.Token{AccessToken: "a", RefreshToken: "r", Expiry: moment}
	same := &oauth2.Token{AccessToken: "a", RefreshToken: "r", Expiry: moment}
	other := &oauth2.Token{AccessToken: "a", RefreshToken: "r2", Expiry: moment}
	if !sameKeys(one, same) {
		t.Error("identical key sets must compare equal")
	}
	if sameKeys(one, other) {
		t.Error("a rotated key set must not compare equal")
	}
	if sameKeys(one, nil) || !sameKeys(nil, nil) {
		t.Error("nil must only equal nil")
	}
}
