package codexauth

import (
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

	"github.com/Agent-Field/codeaf/internal/guard"
)

const (
	authorizePath   = "/oauth/authorize"
	tokenPath       = "/oauth/token"
	callbackPath    = "/auth/callback"
	callbackScope   = "openid profile email offline_access"
	maxExchangeBody = 1 << 20
)

// Options holds every replaceable edge of a Codex browser sign-in and its
// later backend calls. The zero value is the shipped service.
type Options struct {
	Issuer     string
	Backend    string
	HTTPClient *http.Client
	Random     io.Reader
	Listen     func(network, address string) (net.Listener, error)
	Now        func() time.Time
	SessionID  string
}

func (o Options) issuer() string {
	if value := strings.TrimRight(strings.TrimSpace(o.Issuer), "/"); value != "" {
		return value
	}
	return Issuer()
}

func (o Options) backend() string {
	if value := strings.TrimRight(strings.TrimSpace(o.Backend), "/"); value != "" {
		return value
	}
	return Backend()
}

func (o Options) client() *http.Client {
	if o.HTTPClient != nil {
		return o.HTTPClient
	}
	return &http.Client{Timeout: 30 * time.Second}
}

func (o Options) now() time.Time {
	if o.Now != nil {
		return o.Now()
	}
	return time.Now()
}

// Flow is one browser connection whose fixed loopback listener is already
// standing when Begin returns.
type Flow struct {
	url         string
	server      *http.Server
	listener    net.Listener
	client      *http.Client
	tokenURL    string
	redirectURI string
	verifier    string
	state       string
	now         func() time.Time

	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
	once   sync.Once
	claim  sync.Once
	mu     sync.Mutex
	tokens Tokens
	err    error
}

// Begin starts the exact S256 loopback flow registered for the Codex CLI,
// trying its primary port and then its one fallback.
func Begin(ctx context.Context, options Options) (*Flow, error) {
	issuer := options.issuer()
	parsedIssuer, err := url.ParseRequestURI(issuer)
	if err != nil || parsedIssuer.Scheme == "" || parsedIssuer.Host == "" {
		return nil, errors.New("connect Codex: the sign-in address is invalid")
	}
	random := options.Random
	if random == nil {
		random = rand.Reader
	}
	proof := make([]byte, 32)
	stateBytes := make([]byte, 32)
	if _, err := io.ReadFull(random, proof); err != nil {
		return nil, fmt.Errorf("connect Codex: make proof key: %w", err)
	}
	if _, err := io.ReadFull(random, stateBytes); err != nil {
		return nil, fmt.Errorf("connect Codex: make state: %w", err)
	}
	listen := options.Listen
	if listen == nil {
		listen = net.Listen
	}
	var listener net.Listener
	port := 0
	for _, candidate := range []int{1455, 1457} {
		listener, err = listen("tcp", fmt.Sprintf("127.0.0.1:%d", candidate))
		if err == nil {
			port = candidate
			break
		}
	}
	if listener == nil {
		return nil, errors.New("connect Codex: both browser return ports are busy · finish or cancel the other sign-in and try again")
	}
	redirectURI := fmt.Sprintf("http://localhost:%d%s", port, callbackPath)
	verifier := base64.RawURLEncoding.EncodeToString(proof)
	challenge := sha256.Sum256([]byte(verifier))
	state := base64.RawURLEncoding.EncodeToString(stateBytes)
	authorize, _ := url.Parse(issuer + authorizePath)
	query := url.Values{}
	query.Set("response_type", "code")
	query.Set("client_id", ClientID)
	query.Set("redirect_uri", redirectURI)
	query.Set("scope", callbackScope)
	query.Set("code_challenge", base64.RawURLEncoding.EncodeToString(challenge[:]))
	query.Set("code_challenge_method", "S256")
	query.Set("state", state)
	query.Set("id_token_add_organizations", "true")
	query.Set("codex_cli_simplified_flow", "true")
	query.Set("originator", Originator)
	authorize.RawQuery = query.Encode()
	runContext, cancel := context.WithCancel(context.WithoutCancel(ctx))
	flow := &Flow{
		url: authorize.String(), listener: listener, client: options.client(),
		tokenURL: issuer + tokenPath, redirectURI: redirectURI, verifier: verifier,
		state: state, now: options.now, ctx: runContext, cancel: cancel,
		done: make(chan struct{}),
	}
	mux := http.NewServeMux()
	mux.HandleFunc(callbackPath, flow.callback)
	flow.server = &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 5 * time.Second}
	guard.Go("codexauth/callback", func() {
		serveErr := flow.server.Serve(listener)
		if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) && !errors.Is(serveErr, net.ErrClosed) {
			flow.finish(Tokens{}, fmt.Errorf("connect Codex: browser return: %w", serveErr))
		}
	})
	return flow, nil
}

// URL is the one sign-in address the person opens.
func (f *Flow) URL() string { return f.url }

// Wait returns the token set after the browser has returned once.
func (f *Flow) Wait(ctx context.Context) (Tokens, error) {
	select {
	case <-f.done:
	case <-ctx.Done():
		f.finish(Tokens{}, fmt.Errorf("connect Codex: %w", ctx.Err()))
		<-f.done
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.tokens, f.err
}

// Cancel releases the fixed loopback listener. It is safe after Wait.
func (f *Flow) Cancel() {
	f.finish(Tokens{}, errors.New("connect Codex: cancelled"))
	f.cancel()
	_ = f.listener.Close()
}

func (f *Flow) callback(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	writer.Header().Set("Cache-Control", "no-store")
	claimed := false
	f.claim.Do(func() { claimed = true })
	if !claimed {
		writer.WriteHeader(http.StatusConflict)
		_, _ = io.WriteString(writer, failurePage)
		return
	}
	if request.URL.Query().Get("state") != f.state {
		writer.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(writer, failurePage)
		f.finish(Tokens{}, errors.New("connect Codex: the browser returned for a different sign-in"))
		return
	}
	if strings.TrimSpace(request.URL.Query().Get("error")) != "" {
		writer.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(writer, failurePage)
		f.finish(Tokens{}, errors.New("connect Codex: sign-in was not completed"))
		return
	}
	code := strings.TrimSpace(request.URL.Query().Get("code"))
	if code == "" {
		writer.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(writer, failurePage)
		f.finish(Tokens{}, errors.New("connect Codex: the browser returned without a code"))
		return
	}
	tokens, err := f.exchangeCode(code)
	if err != nil {
		writer.WriteHeader(http.StatusBadGateway)
		_, _ = io.WriteString(writer, failurePage)
		f.finish(Tokens{}, err)
		return
	}
	_, _ = io.WriteString(writer, successPage)
	f.finish(tokens, nil)
}

func (f *Flow) exchangeCode(code string) (Tokens, error) {
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {ClientID},
		"code":          {code},
		"redirect_uri":  {f.redirectURI},
		"code_verifier": {f.verifier},
	}
	request, err := http.NewRequestWithContext(f.ctx, http.MethodPost, f.tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return Tokens{}, fmt.Errorf("connect Codex: prepare exchange: %w", err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	response, err := f.client.Do(request)
	if err != nil {
		return Tokens{}, fmt.Errorf("connect Codex: exchange the browser code: %w", err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxExchangeBody))
	if err != nil {
		return Tokens{}, fmt.Errorf("connect Codex: read the exchange: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return Tokens{}, errors.New("connect Codex: OpenAI refused the exchange")
	}
	var answer struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		IDToken      string `json:"id_token"`
	}
	if json.Unmarshal(raw, &answer) != nil || strings.TrimSpace(answer.AccessToken) == "" || strings.TrimSpace(answer.RefreshToken) == "" || strings.TrimSpace(answer.IDToken) == "" {
		return Tokens{}, errors.New("connect Codex: OpenAI returned no usable sign-in")
	}
	idClaims, err := claimsFrom(answer.IDToken)
	if err != nil {
		return Tokens{}, errors.New("connect Codex: OpenAI returned unreadable account details")
	}
	accessClaims, _ := claimsFrom(answer.AccessToken)
	expiresAt := time.Unix(accessClaims.Exp, 0)
	if accessClaims.Exp == 0 {
		expiresAt = time.Time{}
	}
	tokens := Tokens{
		AccessToken: answer.AccessToken, RefreshToken: answer.RefreshToken, IDToken: answer.IDToken,
		AccountID: idClaims.Auth.AccountID, Email: idClaims.Email, Plan: idClaims.Auth.Plan,
		ExpiresAt: expiresAt, LastRefresh: f.now().UTC(),
	}
	register(tokens)
	return tokens, nil
}

func (f *Flow) finish(tokens Tokens, err error) {
	f.once.Do(func() {
		f.mu.Lock()
		f.tokens, f.err = tokens, err
		f.mu.Unlock()
		close(f.done)
	})
}

const successPage = `<!doctype html><html lang="en"><head><meta charset="utf-8"><title>Codex connected</title></head><body><main><h1>Codex connected.</h1><p>You can close this tab and return to codeaf.</p></main></body></html>`
const failurePage = `<!doctype html><html lang="en"><head><meta charset="utf-8"><title>Codex did not connect</title></head><body><main><h1>Codex did not connect.</h1><p>Return to codeaf and try again.</p></main></body></html>`
