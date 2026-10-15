package main

// ── codeaf app: PROVIDERS AND KEYS ──────────────────────────────────────────
//
// KEYS NEVER TOUCH DISK IN THE APP'S PROCESS. The app keeps them in the macOS
// keychain and hands each one over at every launch with keys.set; this process
// holds them in memory ([config.AppCredentials]), where every key reader in
// the engine looks first, and the profile writer refuses to put one on disk.
//
// keys.check is EXACTLY ONE request to the provider, with no redirect
// followed and ten seconds to answer, and a rejection is said in the
// provider's own words with its own status code. A browser sign-in exists
// where the engine already has one: OpenRouter's.

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/modelsource"
	"github.com/Agent-Field/codeaf/internal/openrouterauth"
	"github.com/Agent-Field/codeaf/internal/redact"
	"github.com/Agent-Field/codeaf/internal/trace"
)

// appKeyCheckWait is how long keys.check waits for the provider: the probe
// ceiling every connect shares, because a person is watching this one too.
const appKeyCheckWait = modelsource.ProbeTimeout

// appKeyAnswerLimit bounds how much of a provider's answer is read.
const appKeyAnswerLimit = 4 << 20

// appSignInWait is how long a browser sign-in is waited for.
const appSignInWait = 10 * time.Minute

// appProvider is one service the app can connect, with the address its key is
// proven against.
type appProvider struct {
	id, name string
	check    string
	signIn   bool
}

// appProviders is every service a pasted key connects, OpenRouter first. The
// Codex sign-in, a local Ollama and a custom endpoint are not here: the first
// keeps its tokens on disk, and the other two are addresses rather than keys.
func appProviders() []appProvider {
	providers := []appProvider{{
		id: modelsource.DefaultID, name: modelsource.DefaultSource("").Name,
		check: strings.TrimRight(config.DefaultBaseURL, "/") + "/models/user", signIn: true,
	}}
	for _, source := range modelsource.Vendored() {
		if source.ID == "codex" || source.KeyOptional || modelsource.IsCustomID(source.ID) {
			continue
		}
		address := source.Address
		if address == "" && len(source.Regions) > 0 {
			address = source.Regions[0].Address
		}
		if address == "" {
			continue
		}
		providers = append(providers, appProvider{id: source.ID, name: source.Name, check: strings.TrimRight(address, "/") + "/models"})
	}
	return providers
}

// appProviderNamed finds one provider by id.
func appProviderNamed(id string) (appProvider, bool) {
	for _, p := range appProviders() {
		if p.id == id {
			return p, true
		}
	}
	return appProvider{}, false
}

// appKeyHint is the last four characters of a key, which is all of it a
// person is ever shown again.
func appKeyHint(key string) string {
	runes := []rune(strings.TrimSpace(key))
	if len(runes) <= 4 {
		return ""
	}
	return "…" + string(runes[len(runes)-4:])
}

// providerView is one provider as the app sees it: connected when a turn on it
// would find a key, wherever that key came from.
func (b *appBackend) providerView(p appProvider, sources modelsource.Set) appwire.Provider {
	view := appwire.Provider{ID: p.id, Name: p.name, SignIn: p.signIn}
	key := ""
	if service, ok := sources.ByID(p.id); ok {
		key = service.Key
	}
	if key != "" {
		view.SignedIn, view.KeyHint = true, appKeyHint(key)
	}
	return view
}

// ListProviders answers every provider and whether it is connected.
func (b *appBackend) ListProviders() ([]appwire.Provider, error) {
	profile := config.ProfileDir()
	sources := config.ResolveSources(profile, config.APIKeyAt(profile), config.DefaultBaseURL)
	providers := appProviders()
	views := make([]appwire.Provider, 0, len(providers))
	for _, p := range providers {
		views = append(views, b.providerView(p, sources))
	}
	return views, nil
}

// providerChanged says provider.changed for one provider, with key carried
// only when this process made the key itself and the app has never seen it.
func (b *appBackend) providerChanged(id, key string) {
	p, ok := appProviderNamed(id)
	if !ok {
		return
	}
	profile := config.ProfileDir()
	sources := config.ResolveSources(profile, config.APIKeyAt(profile), config.DefaultBaseURL)
	b.emit.Emit(appwire.EventProviderChanged, appwire.ProviderChanged{Provider: b.providerView(p, sources), Key: key})
}

// CheckKey proves a key with one request and keeps nothing.
func (b *appBackend) CheckKey(provider, key string) (appwire.KeyCheck, error) {
	p, ok := appProviderNamed(provider)
	if !ok {
		return appwire.KeyCheck{}, appwire.Errorf(appwire.CodeBadParams, "codeaf cannot connect %s with a key", provider)
	}
	trace.Secret(key)
	redact.Register(key)
	address := p.check
	if b.keyAddress != nil {
		address = b.keyAddress(p.id)
	}
	ctx, cancel := context.WithTimeout(context.Background(), appKeyCheckWait)
	defer cancel()
	return appCheckKey(ctx, b.keyClient, address, p.id, key), nil
}

// appCheckKey is the one request: an authenticated listing of the models the
// key reaches. OpenRouter's public listing proves nothing about a key, so its
// listing filtered to the key's own account is the one asked.
func appCheckKey(ctx context.Context, client *http.Client, address, provider, key string) appwire.KeyCheck {
	host := address
	if parsed, err := url.Parse(address); err == nil && parsed.Host != "" {
		host = parsed.Hostname()
	}
	unreachable := appwire.KeyCheck{Status: "timeout", Message: "No answer · couldn't reach " + host + " in 10s"}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return unreachable
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+key)
	if client == nil {
		client = http.DefaultClient
	}
	// A REDIRECT MUST NEVER TURN THIS ONE CALL INTO A KEY SENT ELSEWHERE.
	local := *client
	local.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := local.Do(request)
	if err != nil {
		var netErr net.Error
		if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &netErr) && netErr.Timeout()) {
			return unreachable
		}
		unreachable.Status = "unreachable"
		unreachable.Message = "No answer · couldn't reach " + host
		return unreachable
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, appKeyAnswerLimit+1))
	if err != nil || len(raw) > appKeyAnswerLimit {
		return appwire.KeyCheck{Status: response.StatusCode, Message: "the provider's answer could not be read"}
	}
	if response.StatusCode != http.StatusOK {
		return appwire.KeyCheck{Status: response.StatusCode, Message: redact.Secrets(appProviderWords(raw, response.StatusCode))}
	}
	var listing struct {
		Data []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &listing); err != nil || listing.Data == nil {
		return appwire.KeyCheck{Status: response.StatusCode, Message: "the provider answered, but not with a list of models"}
	}
	check := appwire.KeyCheck{OK: true, Models: []appwire.Model{}}
	for _, row := range listing.Data {
		id := strings.TrimSpace(row.ID)
		if id == "" {
			continue
		}
		name := strings.TrimSpace(row.Name)
		if name == "" {
			name = id
		}
		check.Models = append(check.Models, appwire.Model{ID: id, Name: name, Provider: provider})
	}
	return check
}

// appProviderWords are a rejection's own words, read from the shapes
// providers answer in, or the status's name when they said nothing readable.
func appProviderWords(raw []byte, status int) string {
	var body struct {
		Message string          `json:"message"`
		Error   json.RawMessage `json:"error"`
	}
	_ = json.Unmarshal(raw, &body)
	if len(body.Error) > 0 {
		var nested struct {
			Message string `json:"message"`
		}
		if json.Unmarshal(body.Error, &nested) == nil && nested.Message != "" {
			body.Message = nested.Message
		} else {
			var plain string
			if json.Unmarshal(body.Error, &plain) == nil && plain != "" {
				body.Message = plain
			}
		}
	}
	if words := strings.TrimSpace(body.Message); words != "" {
		return words
	}
	return http.StatusText(status)
}

// SetKey holds a provider's key for this launch and makes it live in every
// conversation the process holds.
func (b *appBackend) SetKey(provider, key string) error {
	if _, ok := appProviderNamed(provider); !ok {
		return appwire.Errorf(appwire.CodeBadParams, "codeaf cannot connect %s with a key", provider)
	}
	b.keys.Set(provider, key)
	b.keysMoved(provider)
	b.providerChanged(provider, "")
	return nil
}

// RemoveKey disconnects a provider for the rest of this launch, and a key in
// the profile or the shell does not quietly take its place.
func (b *appBackend) RemoveKey(provider string) error {
	if _, ok := appProviderNamed(provider); !ok {
		return appwire.Errorf(appwire.CodeBadParams, "codeaf has no provider %s", provider)
	}
	b.keys.Remove(provider)
	b.keysMoved(provider)
	b.providerChanged(provider, "")
	return nil
}

// keysMoved makes the keys live in the engine's process half, if it is open:
// one that opens later reads them as it opens.
func (b *appBackend) keysMoved(provider string) {
	if !b.assembled.Load() {
		return
	}
	proc, err := openEngineProcess()
	if err != nil || proc == nil {
		return
	}
	if provider == modelsource.DefaultID {
		key, _ := b.keys.Key(provider)
		_ = proc.setAPIKey(key)
	}
	proc.refreshModelSources()
}

// SignIn starts a browser sign-in and answers the address to open. The key it
// produces is held like a pasted one and handed to the app once, on
// provider.changed, for the keychain.
func (b *appBackend) SignIn(provider string) (string, error) {
	p, ok := appProviderNamed(provider)
	if !ok || !p.signIn {
		return "", appwire.Errorf(appwire.CodeBadParams, "%s has no browser sign-in; paste a key instead", provider)
	}
	ctx, cancel := context.WithTimeout(context.Background(), appSignInWait)
	flow, err := openrouterauth.Begin(ctx, openrouterauth.Options{})
	if err != nil {
		cancel()
		return "", appEngineError(err)
	}
	var previous *openrouterauth.Flow
	b.locked(func() { previous, b.signIn = b.signIn, flow })
	if previous != nil {
		previous.Cancel()
	}
	guard.Go("app/sign-in", func() {
		defer cancel()
		key, err := flow.Wait(ctx)
		b.locked(func() {
			if b.signIn == flow {
				b.signIn = nil
			}
		})
		if err != nil || strings.TrimSpace(key) == "" || b.isClosing() {
			return
		}
		b.keys.Set(p.id, key)
		b.keysMoved(p.id)
		b.providerChanged(p.id, key)
	})
	return flow.URL(), nil
}
