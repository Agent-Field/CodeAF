package main

// ── codeaf app: PROVIDERS AND KEYS ──────────────────────────────────────────
//
// KEYS NEVER TOUCH DISK IN THE APP'S PROCESS. The app keeps them in the macOS
// keychain and hands each one over at every launch with keys.set; this process
// holds them in memory ([config.AppCredentials]), where every key reader in
// the engine looks first, and the profile writer refuses to put one on disk.
//
// EVERY PROVIDER THE TERMINAL CONNECTS, CONNECTED THE WAY THE TERMINAL CONNECTS
// IT. The list is the terminal's first-run list in its order
// ([tui3.ProviderCatalog]), and each provider says what connecting it asks
// for: a key (OpenRouter's with a browser sign-in beside it; Z.ai's, Moonshot's
// and Qwen's after one of their regions), the ChatGPT sign-in and nothing else,
// or an address with a key only if the service wants one (Ollama, a custom
// endpoint). A key is proved by the terminal's own requests in the terminal's
// own order ([config.ProveService]) — the listing, the one-token call where
// there is no listing, every billing road where there is more than one — and
// what that proof found beyond the key (the region, the road the key opened,
// whether the service lists its models, the address) is not a secret, so
// keys.set keeps it in the profile the terminal reads ([config.KeepAppSource]).
//
// OPENROUTER'S CHECK IS EXACTLY ONE REQUEST, with no redirect followed and ten
// seconds to answer, and a rejection is said in the provider's own words with
// its own status code. Its browser sign-in produces a key the app keeps.
//
// THE CHATGPT SIGN-IN IS THE ONE EXCEPTION, accepted by the owner: its tokens
// rotate on every refresh, so they stay in the profile exactly as the terminal
// keeps them ([config.ConnectCodex]), its row carries no key, and no key
// reaches the app for it. It is an account sign-in, with an attempt and a
// state of its own the window reads (appaccount_auth.go), and keys.remove
// signs it out the way the terminal's disconnect does.

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
	"github.com/Agent-Field/codeaf/internal/tui3"
)

// appKeyCheckWait is how long keys.check waits for OpenRouter: the probe
// ceiling every connect shares, because a person is watching this one too.
const appKeyCheckWait = modelsource.ProbeTimeout

// appConnectWait bounds a whole proof of any other provider. Each request in
// it has the probe ceiling of its own, and the longest proof the terminal makes
// is three of them: a listing and the one-token call that stands in for it, or
// a call on each of two billing roads and the listing of the one that answered.
const appConnectWait = 3 * modelsource.ProbeTimeout

// appKeyAnswerLimit bounds how much of a provider's answer is read.
const appKeyAnswerLimit = 4 << 20

// appSignInWait is how long a browser sign-in is waited for.
const appSignInWait = 10 * time.Minute

// The words keys.check answers in status when there is no status code to
// give: the service could not be reached, codeaf refused the key's shape before
// asking, or every billing road refused for money.
const (
	appCheckUnreachable = "unreachable"
	appCheckShape       = "shape"
	appCheckPayment     = "payment"
)

// appProvider is one service the app can connect: what connecting it asks
// for, the description it is connected from, and for OpenRouter the address
// its key is proven against.
type appProvider struct {
	id, name string
	kind     string
	signIn   bool
	check    string
	source   modelsource.Source
}

// appSignIn is OpenRouter's browser sign-in waiting for the person. The
// ChatGPT one has a slot of its own ([appAccountAuth]).
type appSignIn interface {
	Cancel()
}

// appSources is every service description the app connects from, which a
// test replaces with ones at its own stub addresses.
func (b *appBackend) appSources() []modelsource.Source {
	if b.vendored != nil {
		return b.vendored()
	}
	return modelsource.Vendored()
}

// appProviders is every provider the terminal's first-run screen offers, in
// its order, and then every further custom endpoint the profile holds — the
// terminal can connect several, and the app lists each by the name it was
// connected as so it can be seen and removed.
func (b *appBackend) appProviders() []appProvider {
	catalog := tui3.ProviderCatalog(b.appSources())
	providers := make([]appProvider, 0, len(catalog)+1)
	var custom modelsource.Source
	for _, source := range catalog {
		if source.ID == modelsource.CustomID {
			custom = source
		}
		providers = append(providers, appProviderOf(source))
	}
	if custom.ID == "" {
		return providers
	}
	for _, row := range config.PersistedSources(config.ProfileDir()) {
		id := strings.ToLower(strings.TrimSpace(row.ID))
		if !modelsource.IsCustomID(id) || id == modelsource.CustomID {
			continue
		}
		instance := custom
		instance.ID, instance.Written = id, strings.TrimSpace(row.Written)
		p := appProviderOf(instance)
		p.name = firstNonEmptyString(instance.Written, id)
		providers = append(providers, p)
	}
	return providers
}

// appProviderOf is one description as the app connects it. The kind is read
// off the same facts the terminal's connect flow branches on.
func appProviderOf(source modelsource.Source) appProvider {
	p := appProvider{id: source.ID, name: source.Name, kind: appwire.ProviderKey, source: source}
	switch {
	case source.ID == modelsource.DefaultID:
		p.signIn = true
		p.check = strings.TrimRight(config.DefaultBaseURL, "/") + "/models/user"
	case strings.EqualFold(source.ID, "codex"):
		p.kind, p.signIn = appwire.ProviderBrowser, true
	case source.AddressOwned || modelsource.IsCustomID(source.ID):
		p.kind = appwire.ProviderAddress
	}
	return p
}

// appProviderNamed finds one provider by id.
func (b *appBackend) appProviderNamed(id string) (appProvider, bool) {
	id = strings.ToLower(strings.TrimSpace(id))
	for _, p := range b.appProviders() {
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

// appPersistedRows is the profile's service rows by id.
func appPersistedRows() map[string]config.PersistedSource {
	rows := make(map[string]config.PersistedSource)
	for _, row := range config.PersistedSources(config.ProfileDir()) {
		rows[strings.ToLower(strings.TrimSpace(row.ID))] = row
	}
	return rows
}

// appSourcesNow is the profile's services as a turn would find them now.
func appSourcesNow() modelsource.Set {
	profile := config.ProfileDir()
	return config.ResolveSources(profile, config.APIKeyAt(profile), config.DefaultBaseURL)
}

// providerView is one provider as the app sees it: connected when a turn on it
// would find what it needs — a key, wherever that key came from; a sign-in;
// an address, with a key if it wants one.
func (b *appBackend) providerView(p appProvider, sources modelsource.Set, rows map[string]config.PersistedSource) appwire.Provider {
	view := appwire.Provider{ID: p.id, Name: p.name, SignIn: p.signIn, Kind: p.kind}
	service, found := sources.ByID(p.id)
	key := ""
	if found {
		key = service.Key
	}
	switch p.kind {
	case appwire.ProviderBrowser:
		// The sign-in's key is a word naming where its tokens live, never a
		// key a person could recognise, so no hint is drawn from it.
		view.SignedIn = found && key != ""
		// THE ONE BROWSER SIGN-IN IS AN ACCOUNT, and both its fields are said
		// here, from one place: kind for a reader of the connect shapes,
		// auth_kind and the sign-in's own state for a reader of account
		// sign-ins.
		view.AuthKind = "account"
		b.accountAuthView(&view)
	case appwire.ProviderAddress:
		view.KeyOptional = true
		view.Address = strings.TrimRight(strings.TrimSpace(p.source.Address), "/")
		if found && strings.TrimSpace(service.Address) != "" {
			view.Address = service.Address
			view.SignedIn = service.HasCredentials()
			// A CONNECTED CUSTOM ENDPOINT IS CALLED WHAT IT WAS CONNECTED AS,
			// as the terminal's /connect calls it; the unconnected row keeps the
			// name the manual spells.
			if modelsource.IsCustomID(p.id) {
				view.Name = firstNonEmptyString(strings.TrimSpace(service.Source.Written), view.Name)
			}
		}
		view.KeyHint = appKeyHint(key)
	default:
		if key != "" {
			view.SignedIn, view.KeyHint = true, appKeyHint(key)
		}
		for _, region := range p.source.Regions {
			view.Regions = append(view.Regions, appwire.ProviderRegion{ID: region.ID, Name: region.Name})
		}
		if view.SignedIn {
			view.Region = strings.TrimSpace(rows[p.id].Region)
			if len(p.source.Doors) > 1 {
				view.Plan = service.Door.Name
			}
		}
	}
	return view
}

// ListProviders answers every provider and whether it is connected.
func (b *appBackend) ListProviders() ([]appwire.Provider, error) {
	sources, rows := appSourcesNow(), appPersistedRows()
	providers := b.appProviders()
	views := make([]appwire.Provider, 0, len(providers))
	for _, p := range providers {
		views = append(views, b.providerView(p, sources, rows))
	}
	return views, nil
}

// providerConnected says whether any provider is connected, by exactly the rule
// providers.list answers signed_in by, so models.list and the person's list of
// providers never disagree about whether anything can answer.
func (b *appBackend) providerConnected() bool {
	providers, _ := b.ListProviders()
	for _, p := range providers {
		if p.SignedIn {
			return true
		}
	}
	return false
}

// providerChanged says provider.changed for one provider, with key carried
// only when this process made the key itself and the app has never seen it.
func (b *appBackend) providerChanged(p appProvider, key string) {
	view := b.providerView(p, appSourcesNow(), appPersistedRows())
	b.emit.Emit(appwire.EventProviderChanged, appwire.ProviderChanged{Provider: view, Key: key})
}

// providerIDChanged is [appBackend.providerChanged] for a provider named by
// its id, which is how the account sign-in names the one it signs in to; an id
// that names no provider says nothing.
func (b *appBackend) providerIDChanged(id, key string) {
	if p, ok := b.appProviderNamed(id); ok {
		b.providerChanged(p, key)
	}
}

// CheckKey proves a key and keeps nothing: the two-field question.
func (b *appBackend) CheckKey(provider, key string) (appwire.KeyCheck, error) {
	return b.CheckProvider(appwire.KeyParams{Provider: provider, Key: key})
}

// CheckProvider proves a provider can be connected as asked and keeps
// nothing. OpenRouter's key is one request; every other provider is proved by
// the terminal's own requests, with the region or address the person gave.
func (b *appBackend) CheckProvider(params appwire.KeyParams) (appwire.KeyCheck, error) {
	p, ok := b.appProviderNamed(params.Provider)
	if !ok {
		return appwire.KeyCheck{}, appwire.Errorf(appwire.CodeBadParams, "codeaf cannot connect %s with a key", params.Provider)
	}
	key := strings.TrimSpace(params.Key)
	if key != "" {
		trace.Secret(key)
		redact.Register(key)
	}
	if err := appKeyAsked(p, key); err != nil {
		return appwire.KeyCheck{}, err
	}
	if p.id == modelsource.DefaultID {
		address := p.check
		if b.keyAddress != nil {
			address = b.keyAddress(p.id)
		}
		ctx, cancel := context.WithTimeout(context.Background(), appKeyCheckWait)
		defer cancel()
		return appCheckKey(ctx, b.keyClient, address, p.id, key), nil
	}
	row, source, err := appServiceRow(config.ProfileDir(), p, params.Region, params.Address, key)
	if err != nil {
		return appwire.KeyCheck{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), appConnectWait)
	defer cancel()
	outcome, proved, keep := config.ProveService(ctx, row, source, key)
	return appProvedCheck(p, source, proved, outcome, keep), nil
}

// appKeyAsked refuses a key where the provider takes none, and a missing key
// where it needs one, before anything is asked of the network.
func appKeyAsked(p appProvider, key string) error {
	switch {
	case p.kind == appwire.ProviderBrowser:
		return appwire.Errorf(appwire.CodeBadParams, "%s signs in through the browser and takes no key", p.name)
	case p.kind == appwire.ProviderKey && key == "":
		return appwire.Errorf(appwire.CodeBadParams, "%s needs a key", p.name)
	}
	return nil
}

// appServiceRow is the row a provider would be kept as, assembled from what
// the profile already holds for it and the person's answers: the region, the
// address. It carries no key; the key is handed to the proof beside it.
func appServiceRow(profile string, p appProvider, region, address, key string) (config.PersistedSource, modelsource.Source, error) {
	source := p.source
	row := config.PersistedSource{ID: p.id, Written: source.Written}
	held := false
	for _, existing := range config.PersistedSources(profile) {
		if strings.EqualFold(strings.TrimSpace(existing.ID), p.id) {
			row, held = existing, true
			break
		}
	}
	row.Key, row.KeyEnv = "", ""
	if region = strings.TrimSpace(region); region != "" {
		known := false
		for _, r := range source.Regions {
			known = known || strings.EqualFold(r.ID, region)
		}
		if !known {
			return row, source, appwire.Errorf(appwire.CodeBadParams, "%s has no region %s", p.name, region)
		}
		row.Region = strings.ToLower(region)
	}
	if p.kind == appwire.ProviderAddress {
		address = strings.TrimRight(firstNonEmptyString(strings.TrimSpace(address), row.Address, source.Address), "/")
		if address == "" {
			return row, source, appwire.Errorf(appwire.CodeBadParams, "%s needs the address it answers at", p.name)
		}
		// The terminal's own two refusals for an address, in its words.
		parsed, err := url.Parse(address)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			return row, source, appwire.Errorf(appwire.CodeBadParams, "that is not a base URL")
		}
		if parsed.Scheme != "http" && parsed.Scheme != "https" {
			return row, source, appwire.Errorf(appwire.CodeBadParams, "use an http or https base URL")
		}
		if modelsource.IsCustomID(p.id) && !held {
			// A NEW CUSTOM ENDPOINT IS MINTED THE ONE WAY THE TERMINAL MINTS
			// ONE, named for its host ([config.PrepareCustomSource]).
			row = config.PrepareCustomSource(profile, address, "")
		}
		row.Address = address
		// A custom endpoint proved with no key is one that answers with none,
		// which is the per-instance fact the terminal records the same way.
		row.KeyOptional = key == ""
	}
	row, source = config.PrepareServiceRow(profile, row, source, nil)
	return row, source, nil
}

// appProvedCheck is what a proof learned, said the way keys.check says it: a
// pass with the models a turn can use and the connection to keep, or a
// refusal in the provider's own words.
func appProvedCheck(p appProvider, source modelsource.Source, row config.PersistedSource, outcome modelsource.Outcome, keep bool) appwire.KeyCheck {
	if !keep {
		switch outcome.Kind {
		case modelsource.OutcomeRefused:
			check := appwire.KeyCheck{Message: redact.Secrets(config.ConnectionDetailWords(outcome.VendorSaid, 0))}
			if outcome.Status != 0 {
				check.Status = outcome.Status
				if check.Message == "" {
					check.Message = http.StatusText(outcome.Status)
				}
			}
			return check
		case modelsource.OutcomeUnanswered:
			return appwire.KeyCheck{Status: appCheckUnreachable, Message: "No answer · couldn't reach " + appServiceHost(source, row)}
		case modelsource.OutcomeWrongShape:
			return appwire.KeyCheck{Status: appCheckShape, Message: config.ConnectionOutcomeWord(p.name, outcome)}
		default:
			return appwire.KeyCheck{Status: appCheckPayment, Message: redact.Secrets(config.ConnectionOutcomeWord(p.name, outcome))}
		}
	}
	connection := &appwire.Connection{Region: row.Region, Door: row.Door, Listed: row.Listed}
	if p.kind == appwire.ProviderAddress {
		connection.Address = row.Address
	}
	if len(source.Doors) > 1 {
		connection.Plan = outcome.Door.Name
	}
	check := appwire.KeyCheck{OK: true, Models: appProvedModels(source, outcome), Connection: connection}
	if outcome.Kind == modelsource.OutcomeAccountCannotPay || outcome.PlanPaused {
		check.Message = redact.Secrets(config.ConnectionOutcomeWord(p.name, outcome))
	}
	return check
}

// appProvedModels is the models a proof found, spelled the way models.list
// spells them so the one a person picks routes to this provider: what the
// provider listed, the plan's own models, or — for a provider that lists none
// — the one model the terminal would move a conversation onto.
func appProvedModels(source modelsource.Source, outcome modelsource.Outcome) []appwire.Model {
	ids := outcome.ModelIDs
	if len(ids) == 0 {
		if preferred := source.PreferredModel(outcome.Door, nil); preferred != "" {
			ids = []string{preferred}
		}
	}
	connected := modelsource.Connected{Source: source}
	group := strings.ToLower(strings.TrimSpace(source.Written))
	name := source.Name
	if modelsource.IsCustomID(source.ID) || name == "" {
		name = strings.TrimSpace(source.Written)
	}
	models := make([]appwire.Model, 0, len(ids))
	for _, id := range ids {
		if id = strings.TrimSpace(id); id != "" {
			models = append(models, appwire.Model{ID: connected.Qualify(id), Name: id, Provider: group, ProviderName: name})
		}
	}
	return models
}

// appServiceHost is the host a proof was sent to, for the line that says it
// never answered.
func appServiceHost(source modelsource.Source, row config.PersistedSource) string {
	address := row.Address
	for _, region := range source.Regions {
		if address == "" && strings.EqualFold(region.ID, row.Region) {
			address = region.Address
		}
	}
	if address == "" && len(source.Doors) > 0 {
		address = source.Doors[0].Address
	}
	return modelsource.AddressHost(firstNonEmptyString(address, source.Address))
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
		unreachable.Status = appCheckUnreachable
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

// SetKey holds a provider's key for this launch: the two-field hand-over.
func (b *appBackend) SetKey(provider, key string) error {
	return b.SetProvider(appwire.KeyParams{Provider: provider, Key: key})
}

// SetProvider holds a provider's key for this launch and makes it live in
// every conversation the process holds, and keeps the connection keys.check
// proved — its region, the road its key opened, its address — in the profile.
// A launch's hand-over carries no connection and keeps what is already there.
func (b *appBackend) SetProvider(params appwire.KeyParams) error {
	p, ok := b.appProviderNamed(params.Provider)
	if !ok {
		return appwire.Errorf(appwire.CodeBadParams, "codeaf cannot connect %s with a key", params.Provider)
	}
	key := strings.TrimSpace(params.Key)
	if err := appKeyAsked(p, key); err != nil {
		return err
	}
	if p.id != modelsource.DefaultID {
		if err := b.keepConnection(p, params, key); err != nil {
			return err
		}
	}
	if key != "" {
		b.keys.Set(p.id, key)
	} else {
		// A provider connected with no key is connected with none, so a key
		// held for it earlier this launch does not quietly come along.
		b.keys.Remove(p.id)
	}
	b.keysMoved(p.id)
	b.providerChanged(p, "")
	b.warmProviderModels()
	return nil
}

// keepConnection writes the provider's row when the hand-over says something
// the profile should keep: a region, a billing road, whether it lists models,
// an address. An address provider handed no key and held by no row is kept at
// its usual address, because with no key its row is the whole of what connects
// it; one handed a key is connected by the key alone, as at every launch, so a
// launch never writes back a row the terminal took away.
func (b *appBackend) keepConnection(p appProvider, params appwire.KeyParams, key string) error {
	profile := config.ProfileDir()
	var connection appwire.Connection
	if params.Connection != nil {
		connection = *params.Connection
	}
	region := firstNonEmptyString(strings.TrimSpace(connection.Region), params.Region)
	address := firstNonEmptyString(strings.TrimSpace(connection.Address), params.Address)
	door := strings.TrimSpace(connection.Door)
	_, held := appPersistedRows()[p.id]
	said := region != "" || door != "" || connection.Listed != nil || address != ""
	if !said && (p.kind != appwire.ProviderAddress || held || key != "") {
		return nil
	}
	row, source, err := appServiceRow(profile, p, region, address, key)
	if err != nil {
		return err
	}
	if door != "" {
		known := false
		for _, d := range source.Doors {
			known = known || strings.EqualFold(d.ID, door)
		}
		if !known {
			return appwire.Errorf(appwire.CodeBadParams, "%s has no plan %s", p.name, door)
		}
		row.Door = strings.ToLower(door)
	}
	if connection.Listed != nil {
		listed := *connection.Listed
		row.Listed = &listed
	}
	if err := config.KeepAppSource(profile, row); err != nil {
		return appEngineError(err)
	}
	return nil
}

// RemoveKey disconnects a provider. A key is let go for the rest of this
// launch, and a key in the profile or the shell does not quietly take its
// place; an address or a sign-in is a connection in the profile, and removing
// it is the terminal's disconnect, which takes the row — and the sign-in's
// tokens — with it. The ChatGPT account is signed out: a sign-in of it still
// waiting for the browser is cancelled first, and one being saved answers busy
// ([appBackend.endAccountSignIn]).
func (b *appBackend) RemoveKey(provider string) error {
	p, ok := b.appProviderNamed(provider)
	if !ok {
		return appwire.Errorf(appwire.CodeBadParams, "codeaf has no provider %s", provider)
	}
	if p.kind == appwire.ProviderBrowser {
		if err := b.endAccountSignIn(); err != nil {
			return err
		}
	}
	if p.kind != appwire.ProviderKey {
		if err := config.DisconnectService(config.ProfileDir(), p.id); err != nil {
			return appEngineError(err)
		}
	}
	if p.kind != appwire.ProviderBrowser {
		// THE SIGN-IN IS NOT A HELD KEY, and marking it removed in memory would
		// hide the next sign-in of this same launch behind the removal.
		b.keys.Remove(p.id)
	}
	b.keysMoved(p.id)
	b.providerChanged(p, "")
	return nil
}

// keysMoved makes the keys live in the engine's process half, if it is open:
// one that opens later reads them as it opens.
//
// IT ASKS WHETHER THE PROCESS HALF IS OPEN, NEVER WHETHER A CONVERSATION WAS
// BUILT. It used to return early until one had been, and to read the memo when
// one had, so a key handed over after an open had failed for the want of it
// reached nothing, and every later call answered with the old missing-key
// error. A failed open is now tried again by the next caller
// ([openAppEngineProcess]) and reads this key then; an open one is updated
// here, whoever opened it.
func (b *appBackend) keysMoved(provider string) {
	proc := openedEngineProcess()
	if proc == nil {
		return
	}
	if provider == modelsource.DefaultID {
		key, _ := b.keys.Key(provider)
		_ = proc.setAPIKey(key)
	}
	proc.refreshModelSources()
}

// warmProviderModels fetches, in the background, the model list of every
// connected provider this process has no list for yet — most often the one
// just connected — through the terminal's own door ([v3ModelShelf.warmAll]),
// so models.list offers its models as the terminal's picker would. It is
// seated on the profile's errand tracker, so a close waits for it.
func (b *appBackend) warmProviderModels() {
	if !b.assembled.Load() || b.isClosing() {
		return
	}
	// The process half is already open here (assembled), so this reads it
	// rather than opening one: a key handed over after a failed open is the
	// app opener's business ([openAppEngineProcess]), not a background warm's.
	proc := openedEngineProcess()
	if proc == nil || proc.Shelf == nil {
		return
	}
	done := make(chan struct{})
	b.locked(func() { b.providerWarm = done })
	poolErrandGoCtx(proc.ProfileDir, "app/provider-models", func(ctx context.Context) {
		defer close(done)
		proc.Shelf.warmAll(ctx, true, proc.noteServiceModels)
	})
}

// SignIn starts a browser sign-in and answers the address to open. OpenRouter's
// produces a key, held like a pasted one and handed to the app once, on
// provider.changed, for the app to keep; the ChatGPT sign-in is the account
// sign-in (appaccount_auth.go), whose tokens stay in the profile and whose
// provider.changed carries no key.
func (b *appBackend) SignIn(provider string) (string, error) {
	p, ok := b.appProviderNamed(provider)
	if !ok || !p.signIn {
		return "", appwire.Errorf(appwire.CodeBadParams, "%s has no browser sign-in; paste a key instead", provider)
	}
	if p.kind == appwire.ProviderBrowser {
		return b.accountSignIn()
	}
	ctx, cancel := context.WithTimeout(context.Background(), appSignInWait)
	flow, err := openrouterauth.Begin(ctx, openrouterauth.Options{})
	if err != nil {
		cancel()
		return "", appEngineError(err)
	}
	b.replaceSignIn(flow)
	guard.Go("app/sign-in", func() {
		defer cancel()
		key, err := flow.Wait(ctx)
		b.signInEnded(flow)
		if err != nil || strings.TrimSpace(key) == "" || b.isClosing() {
			return
		}
		b.keys.Set(p.id, key)
		b.keysMoved(p.id)
		b.providerChanged(p, key)
		b.warmProviderModels()
	})
	return flow.URL(), nil
}

// replaceSignIn makes one sign-in the one waiting, and cancels the one it
// replaces: a person who starts a second has given up on the first.
func (b *appBackend) replaceSignIn(flow appSignIn) {
	var previous appSignIn
	b.locked(func() { previous, b.signIn = b.signIn, flow })
	if previous != nil {
		previous.Cancel()
	}
}

// signInEnded forgets a sign-in that is over, unless another replaced it.
func (b *appBackend) signInEnded(flow appSignIn) {
	b.locked(func() {
		if b.signIn == flow {
			b.signIn = nil
		}
	})
}
