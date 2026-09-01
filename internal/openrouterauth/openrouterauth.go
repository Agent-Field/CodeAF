// Package openrouterauth connects a local aforge profile to OpenRouter without
// asking the person to make and copy an API key by hand.
package openrouterauth

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/guard"
)

const (
	defaultAuthURL     = "https://openrouter.ai/auth"
	defaultExchangeURL = "https://openrouter.ai/api/v1/auth/keys"
	maxExchangeBody    = 1 << 20
)

// Options holds the replaceable edges of an OpenRouter connection. Its zero
// value is the shipped service; tests replace the endpoints, entropy and
// listener without sending a browser or a credential outside the process.
type Options struct {
	AuthURL     string
	ExchangeURL string
	HTTPClient  *http.Client
	Random      io.Reader
	Listen      func(network, address string) (net.Listener, error)
}

// Flow is one browser connection in progress. The loopback listener is already
// standing when [Begin] returns, so URL may be opened immediately and Wait may
// safely run on another goroutine.
type Flow struct {
	url      string
	server   *http.Server
	listener net.Listener
	client   *http.Client
	exchange string
	verifier string

	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
	once   sync.Once
	// callbackOnce lets exactly one browser return spend the one-time code. A
	// refresh racing the original response must not make two exchanges whose
	// success and refusal then race to become the flow's answer.
	callbackOnce sync.Once

	mu  sync.Mutex
	key string
	err error
}

// Begin starts an S256 PKCE connection on an arbitrary loopback port.
//
// OpenRouter explicitly accepts localhost callbacks on any port, so there is
// no fixed socket to contend for and no public redirect service in the middle.
// The callback path is random as well: the verifier protects the exchanged
// code, and the unguessable path keeps an unrelated local request from ending
// the person's attempt early.
func Begin(ctx context.Context, options Options) (*Flow, error) {
	authURL := strings.TrimSpace(options.AuthURL)
	if authURL == "" {
		authURL = defaultAuthURL
	}
	exchangeURL := strings.TrimSpace(options.ExchangeURL)
	if exchangeURL == "" {
		exchangeURL = defaultExchangeURL
	}
	if _, err := url.ParseRequestURI(authURL); err != nil {
		return nil, fmt.Errorf("connect OpenRouter: invalid sign-in address: %w", err)
	}
	if _, err := url.ParseRequestURI(exchangeURL); err != nil {
		return nil, fmt.Errorf("connect OpenRouter: invalid exchange address: %w", err)
	}

	random := options.Random
	if random == nil {
		random = rand.Reader
	}
	verifierBytes := make([]byte, 32)
	pathBytes := make([]byte, 18)
	if _, err := io.ReadFull(random, verifierBytes); err != nil {
		return nil, fmt.Errorf("connect OpenRouter: make proof key: %w", err)
	}
	if _, err := io.ReadFull(random, pathBytes); err != nil {
		return nil, fmt.Errorf("connect OpenRouter: make callback: %w", err)
	}
	verifier := base64.RawURLEncoding.EncodeToString(verifierBytes)
	callbackPath := "/openrouter/" + base64.RawURLEncoding.EncodeToString(pathBytes)

	listen := options.Listen
	if listen == nil {
		listen = net.Listen
	}
	listener, err := listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("connect OpenRouter: open the browser return address: %w", err)
	}
	port, err := listenerPort(listener.Addr())
	if err != nil {
		_ = listener.Close()
		return nil, fmt.Errorf("connect OpenRouter: read the browser return address: %w", err)
	}
	callbackURL := fmt.Sprintf("http://127.0.0.1:%d%s", port, callbackPath)

	parsed, _ := url.Parse(authURL)
	query := parsed.Query()
	challenge := sha256.Sum256([]byte(verifier))
	query.Set("callback_url", callbackURL)
	query.Set("code_challenge", base64.RawURLEncoding.EncodeToString(challenge[:]))
	query.Set("code_challenge_method", "S256")
	parsed.RawQuery = query.Encode()

	client := options.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	runContext, cancel := context.WithCancel(context.WithoutCancel(ctx))
	flow := &Flow{
		url: parsed.String(), listener: listener, client: client,
		exchange: exchangeURL, verifier: verifier,
		ctx: runContext, cancel: cancel, done: make(chan struct{}),
	}
	mux := http.NewServeMux()
	mux.HandleFunc(callbackPath, flow.callback)
	flow.server = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       5 * time.Second,
	}
	guard.Go("openrouterauth/callback", func() {
		serveErr := flow.server.Serve(listener)
		if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) && !errors.Is(serveErr, net.ErrClosed) {
			flow.finish("", fmt.Errorf("connect OpenRouter: browser return: %w", serveErr))
		}
	})
	return flow, nil
}

func listenerPort(address net.Addr) (int, error) {
	_, rawPort, err := net.SplitHostPort(address.String())
	if err != nil {
		return 0, err
	}
	var port int
	if _, err := fmt.Sscanf(rawPort, "%d", &port); err != nil || port < 1 {
		return 0, fmt.Errorf("invalid port %q", rawPort)
	}
	return port, nil
}

// URL is the OpenRouter sign-in address to open in the person's browser.
func (f *Flow) URL() string { return f.url }

// Wait blocks until the browser trip has exchanged its one-time code for a
// user-controlled API key, the caller leaves, or the flow is cancelled.
func (f *Flow) Wait(ctx context.Context) (string, error) {
	select {
	case <-f.done:
	case <-ctx.Done():
		f.finish("", fmt.Errorf("connect OpenRouter: %w", ctx.Err()))
		<-f.done
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.key, f.err
}

// Cancel releases the loopback listener and makes a waiting call return.
func (f *Flow) Cancel() {
	f.finish("", errors.New("connect OpenRouter: cancelled"))
}

func (f *Flow) callback(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	writer.Header().Set("Cache-Control", "no-store")
	claimed := false
	f.callbackOnce.Do(func() { claimed = true })
	if !claimed {
		writer.WriteHeader(http.StatusConflict)
		_, _ = io.WriteString(writer, failurePage)
		return
	}
	if refusal := strings.TrimSpace(request.URL.Query().Get("error")); refusal != "" {
		writer.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(writer, failurePage)
		f.finish("", errors.New("connect OpenRouter: sign-in was not completed"))
		return
	}
	code := strings.TrimSpace(request.URL.Query().Get("code"))
	if code == "" {
		writer.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(writer, failurePage)
		f.finish("", errors.New("connect OpenRouter: the browser returned without a code"))
		return
	}
	key, err := f.exchangeCode(code)
	if err != nil {
		writer.WriteHeader(http.StatusBadGateway)
		_, _ = io.WriteString(writer, failurePage)
		f.finish("", err)
		return
	}
	_, _ = io.WriteString(writer, successPage)
	f.finish(key, nil)
}

func (f *Flow) exchangeCode(code string) (string, error) {
	body, err := json.Marshal(map[string]string{
		"code": code, "code_verifier": f.verifier, "code_challenge_method": "S256",
	})
	if err != nil {
		return "", fmt.Errorf("connect OpenRouter: prepare exchange: %w", err)
	}
	request, err := http.NewRequestWithContext(f.ctx, http.MethodPost, f.exchange, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("connect OpenRouter: prepare exchange: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	response, err := f.client.Do(request)
	if err != nil {
		return "", fmt.Errorf("connect OpenRouter: exchange the browser code: %w", err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxExchangeBody))
	if err != nil {
		return "", fmt.Errorf("connect OpenRouter: read the exchange: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", fmt.Errorf("connect OpenRouter: OpenRouter refused the exchange (%s)", response.Status)
	}
	var answer struct {
		Key string `json:"key"`
	}
	if err := json.Unmarshal(raw, &answer); err != nil {
		return "", fmt.Errorf("connect OpenRouter: read the exchange: %w", err)
	}
	key := strings.TrimSpace(answer.Key)
	if !strings.HasPrefix(key, "sk-") || strings.ContainsAny(key, " \t\r\n") {
		return "", errors.New("connect OpenRouter: OpenRouter returned no usable key")
	}
	return key, nil
}

func (f *Flow) finish(key string, err error) {
	f.once.Do(func() {
		f.cancel()
		_ = f.listener.Close()
		f.mu.Lock()
		f.key, f.err = key, err
		f.mu.Unlock()
		close(f.done)
	})
}

const successPage = `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>OpenRouter connected</title></head><body><main><h1>OpenRouter connected.</h1><p>You can close this tab and return to aforge.</p></main></body></html>`

const failurePage = `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>OpenRouter did not connect</title></head><body><main><h1>OpenRouter did not connect.</h1><p>Return to aforge and try again.</p></main></body></html>`
