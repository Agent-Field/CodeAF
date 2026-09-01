package openrouterauth

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestBrowserTripUsesS256AndReturnsTheMintedKey(t *testing.T) {
	verifierBytes := bytes.Repeat([]byte{0x2a}, 32)
	pathBytes := bytes.Repeat([]byte{0x4b}, 18)
	verifier := base64.RawURLEncoding.EncodeToString(verifierBytes)

	exchange := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("exchange was %s with content type %q", request.Method, request.Header.Get("Content-Type"))
		}
		var body struct {
			Code                string `json:"code"`
			CodeVerifier        string `json:"code_verifier"`
			CodeChallengeMethod string `json:"code_challenge_method"`
		}
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.Code != "one-time-code" || body.CodeVerifier != verifier || body.CodeChallengeMethod != "S256" {
			t.Fatalf("exchange body = %+v", body)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `{"key":"sk-or-v1-from-browser"}`)
	}))
	defer exchange.Close()

	flow, err := Begin(t.Context(), Options{
		AuthURL: "https://openrouter.example/auth", ExchangeURL: exchange.URL,
		Random: bytes.NewReader(append(verifierBytes, pathBytes...)),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(flow.Cancel)

	signIn, err := url.Parse(flow.URL())
	if err != nil {
		t.Fatal(err)
	}
	if signIn.Scheme != "https" || signIn.Host != "openrouter.example" || signIn.Path != "/auth" {
		t.Fatalf("sign-in address = %s", signIn)
	}
	callback := signIn.Query().Get("callback_url")
	callbackURL, err := url.Parse(callback)
	if err != nil {
		t.Fatal(err)
	}
	if callbackURL.Scheme != "http" || callbackURL.Hostname() != "127.0.0.1" || !strings.HasPrefix(callbackURL.Path, "/openrouter/") {
		t.Fatalf("callback address = %s", callbackURL)
	}
	challenge := sha256.Sum256([]byte(verifier))
	if got, want := signIn.Query().Get("code_challenge"), base64.RawURLEncoding.EncodeToString(challenge[:]); got != want {
		t.Fatalf("challenge = %q, want %q", got, want)
	}
	if got := signIn.Query().Get("code_challenge_method"); got != "S256" {
		t.Fatalf("challenge method = %q", got)
	}

	response, err := http.Get(callback + "?code=one-time-code")
	if err != nil {
		t.Fatal(err)
	}
	page, _ := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK || !strings.Contains(string(page), "OpenRouter connected") {
		t.Fatalf("callback answered %s: %s", response.Status, page)
	}
	key, err := flow.Wait(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if key != "sk-or-v1-from-browser" {
		t.Fatalf("key = %q", key)
	}
	// Wait is idempotent; a second caller reads the settled result rather than
	// waiting on a channel whose only value has already been taken.
	if again, err := flow.Wait(t.Context()); err != nil || again != key {
		t.Fatalf("second wait = %q, %v", again, err)
	}
}

func TestARefusedExchangeFailsInTheBrowserAndTheTerminal(t *testing.T) {
	exchange := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		http.Error(writer, `{"error":"invalid code"}`, http.StatusForbidden)
	}))
	defer exchange.Close()
	flow, err := Begin(t.Context(), Options{ExchangeURL: exchange.URL})
	if err != nil {
		t.Fatal(err)
	}
	signIn, _ := url.Parse(flow.URL())
	callback := signIn.Query().Get("callback_url")
	response, err := http.Get(callback + "?code=wrong")
	if err != nil {
		t.Fatal(err)
	}
	page, _ := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if response.StatusCode != http.StatusBadGateway || !strings.Contains(string(page), "did not connect") {
		t.Fatalf("callback answered %s: %s", response.Status, page)
	}
	if key, err := flow.Wait(t.Context()); key != "" || err == nil || !strings.Contains(err.Error(), "403 Forbidden") {
		t.Fatalf("wait = %q, %v", key, err)
	}
}

func TestCancelEndsAWaitAndReleasesTheCallback(t *testing.T) {
	flow, err := Begin(context.Background(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	flow.Cancel()
	if key, err := flow.Wait(t.Context()); key != "" || err == nil || !strings.Contains(err.Error(), "cancelled") {
		t.Fatalf("wait after cancel = %q, %v", key, err)
	}
}
