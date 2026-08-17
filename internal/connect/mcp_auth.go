package connect

import (
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/int128/listener"
	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
	"golang.org/x/oauth2"
)

// The sign-in for a service that brings its own tools.
//
// ── IT IS THE SAME TRIP, ARRANGED BY THE SERVICE INSTEAD OF BY US ──
//
// Google's trip (auth.go) is made of facts written down in this package: where
// the sign-in page is, where the exchange goes, who aforge is. A tool server
// publishes all three, in a shape everybody has agreed on, so the trip is the
// same trip and the difference is only that the first thing aforge does is ASK:
//
//	1. the service says where the description of its sign-in lives
//	   (RFC 9728 — protected resource metadata),
//	2. that description names the sign-in, which describes itself
//	   (RFC 8414 — authorization server metadata),
//	3. aforge introduces itself there and is issued an identity
//	   (RFC 7591 — dynamic client registration), kept for next time,
//	4. the browser trip runs with a proof key and the service's own name
//	   attached (PKCE and RFC 8707), on the same loopback addresses and behind
//	   the same page as every other connection this program makes.
//
// The first three steps come from the tool-server SDK's oauthex package, which
// implements those documents and their security checks. What is written here is
// the order they go in, the loopback half of step four, and what is kept
// afterwards.
//
// ── THE ONE HONEST FAILURE ──
//
// A service whose sign-in will not let a program introduce itself cannot be
// connected this way at all, and there is nothing a person can do about it from
// here. That is said in one sentence, in their words, at the moment they ask —
// not as a refusal from somebody else's server three redirects later.

const (
	// mcpAskTimeout bounds each of the small asks that precede the browser
	// trip. They are ordinary web requests to a service that is either up or
	// not, and none of them is worth hanging a screen on.
	mcpAskTimeout = 30 * time.Second
	// mcpClientName is how aforge introduces itself. It is shown to the person
	// on the service's own permission screen, so it is the product's name and
	// nothing more technical.
	mcpClientName = "aforge"
)

// refusedPage is what the person sees when they say no, or when the service
// does. It says the one thing they need, in their own words, exactly as
// [successPage] does.
const refusedPage = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Not connected</title>
<style>
  :root { color-scheme: light dark; }
  body {
    margin: 0; min-height: 100vh;
    display: flex; align-items: center; justify-content: center;
    font: 16px/1.6 ui-sans-serif, system-ui, -apple-system, "Segoe UI", sans-serif;
    background: #fafaf9; color: #1c1917;
  }
  main { text-align: center; padding: 2rem; }
  h1 { margin: 0 0 .35rem; font-size: 1.35rem; font-weight: 600; letter-spacing: -.01em; }
  p { margin: 0; color: #78716c; }
  @media (prefers-color-scheme: dark) {
    body { background: #1c1917; color: #fafaf9; }
    p { color: #a8a29e; }
  }
</style>
</head>
<body>
<main>
  <h1>Not connected.</h1>
  <p>You can close this tab and try again.</p>
</main>
</body>
</html>
`

// signIn is everything the asking turned up about how one service is signed in
// to: what it calls itself, who runs its sign-in, and where the three steps of
// that sign-in happen.
type signIn struct {
	// resource is the service's own name for itself, which travels with the
	// browser trip and the exchange so that the keys that come back are good
	// at this service and nowhere else (RFC 8707).
	resource string
	// issuer is the sign-in's name for itself.
	issuer string
	// authorize is where the person is sent, and token is where the exchange
	// and every later renewal go.
	authorize string
	token     string
	// register is where aforge introduces itself, empty when the sign-in does
	// not allow it.
	register string
	// scopes are the permissions the service says are worth asking for.
	scopes []string
	// style is how the sign-in wants aforge to identify itself on a request to
	// the token address.
	style oauth2.AuthStyle
	// stampsIssuer reports that the sign-in promises to name itself in the
	// answer it sends the browser back with (RFC 9207).
	stampsIssuer bool
}

// discoverSignIn asks one service how it is signed in to.
//
// THE ORDER OF THE ASKING IS THE SPECIFICATION'S, and each rung is the honest
// fallback for the rung above: the service's own refusal names its description;
// failing that, the two agreed addresses that description lives at; failing
// that, the older arrangement in which the service and its sign-in are the same
// place. A service that answers none of them cannot be connected, and says so.
func discoverSignIn(ctx context.Context, client *http.Client, address string) (signIn, error) {
	resource, servers, scopes := describedResource(ctx, client, address)
	if len(servers) == 0 {
		return signIn{}, fmt.Errorf("it did not say how it is signed in to")
	}

	meta, err := auth.GetAuthServerMetadata(ctx, servers[0], client)
	if err != nil {
		return signIn{}, fmt.Errorf("its sign-in could not be read: %w", err)
	}
	if meta == nil {
		// The older arrangement, from before a sign-in described itself: the
		// three addresses are where they have always been.
		root := strings.TrimSuffix(servers[0], "/")
		meta = &oauthex.AuthServerMeta{
			Issuer:                servers[0],
			AuthorizationEndpoint: root + "/authorize",
			TokenEndpoint:         root + "/token",
			RegistrationEndpoint:  root + "/register",
		}
	}
	// THE ASK IS WHAT THE SERVICE SAID IT NEEDS, AND NOTHING ELSE. A sign-in
	// lists every permission it can issue, for every program it serves; asking
	// for that list would put a permissions screen in front of the person that
	// bears no relation to what aforge is about to do. A service that says
	// nothing is asked for nothing, and answers with whatever it grants by
	// default.
	//
	// A connection that cannot be renewed is a connection that dies quietly an
	// hour after it is made, so the permission that keeps it alive is asked for
	// wherever the sign-in offers it.
	if slices.Contains(meta.ScopesSupported, "offline_access") && !slices.Contains(scopes, "offline_access") {
		scopes = append(append([]string(nil), scopes...), "offline_access")
	}
	return signIn{
		resource:     resource,
		issuer:       meta.Issuer,
		authorize:    meta.AuthorizationEndpoint,
		token:        meta.TokenEndpoint,
		register:     meta.RegistrationEndpoint,
		scopes:       scopes,
		style:        tokenStyle(meta.TokenEndpointAuthMethodsSupported),
		stampsIssuer: meta.AuthorizationResponseIssParameterSupported,
	}, nil
}

// describedResource finds the service's own description of itself: what it is
// called, who signs people in to it, and what may be asked for.
//
// Nothing here fails: a service that describes itself nowhere falls back to the
// older arrangement, and the caller learns of the trouble by being handed no
// sign-in at all.
func describedResource(ctx context.Context, client *http.Client, address string) (string, []string, []string) {
	for _, candidate := range describedAt(ctx, client, address) {
		found, err := oauthex.GetProtectedResourceMetadata(ctx, candidate.description, candidate.resource, client)
		if err != nil || found == nil || len(found.AuthorizationServers) == 0 {
			continue
		}
		return found.Resource, found.AuthorizationServers, found.ScopesSupported
	}
	// The older arrangement: the service's own root signs people in to it.
	parsed, err := url.Parse(address)
	if err != nil {
		return address, nil, nil
	}
	root := *parsed
	root.Path, root.RawQuery, root.Fragment = "", "", ""
	return address, []string{root.String()}, nil
}

// described is one place a service's description of itself may live, with the
// name that description must claim if it is to be believed.
type described struct {
	description string
	resource    string
}

// describedAt lists the places to look, best first: where the service itself
// said to look, then the two agreed addresses.
func describedAt(ctx context.Context, client *http.Client, address string) []described {
	var places []described
	if named := namedDescription(ctx, client, address); named != "" {
		places = append(places, described{description: named, resource: address})
	}
	parsed, err := url.Parse(address)
	if err != nil {
		return places
	}
	at := *parsed
	at.RawQuery, at.Fragment = "", ""
	// At the path of the service's own address.
	at.Path = "/.well-known/oauth-protected-resource/" + strings.TrimLeft(parsed.Path, "/")
	places = append(places, described{description: at.String(), resource: address})
	// At the root.
	root := *parsed
	root.Path, root.RawQuery, root.Fragment = "", "", ""
	at.Path = "/.well-known/oauth-protected-resource"
	places = append(places, described{description: at.String(), resource: root.String()})
	return places
}

// namedDescription asks the service directly and reads the refusal, which is
// where a service is meant to say where its description lives.
//
// A SERVICE THAT DOES NOT REFUSE IS NOT AN ERROR HERE. This is a hint and the
// two agreed addresses are the rule; anything unexpected — a service that is
// up, a service that is down, a refusal that says nothing — simply produces no
// hint and the asking carries on.
func namedDescription(ctx context.Context, client *http.Client, address string) string {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return ""
	}
	request.Header.Set("Accept", "application/json, text/event-stream")
	response, err := client.Do(request)
	if err != nil {
		return ""
	}
	defer func() { _ = response.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1<<16))
	if response.StatusCode != http.StatusUnauthorized {
		return ""
	}
	challenges, err := oauthex.ParseWWWAuthenticate(response.Header.Values("WWW-Authenticate"))
	if err != nil {
		return ""
	}
	for _, challenge := range challenges {
		if at := challenge.Params["resource_metadata"]; at != "" {
			return at
		}
	}
	return ""
}

// tokenStyle picks how aforge identifies itself when it asks for keys, from
// what the sign-in says it accepts. The newer way is preferred where both are
// offered, and a sign-in that says nothing is left to the stock guess.
func tokenStyle(methods []string) oauth2.AuthStyle {
	switch {
	case slices.Contains(methods, "client_secret_post"), slices.Contains(methods, "none"):
		return oauth2.AuthStyleInParams
	case slices.Contains(methods, "client_secret_basic"):
		return oauth2.AuthStyleInHeader
	}
	return oauth2.AuthStyleAutoDetect
}

// beginToolServer starts connecting one tool server and returns as soon as
// there is an address to send the person to.
//
// It keeps every promise [Manager.BeginAuth] makes — the listener is up before
// this returns, the round-trip outlives the context that started it, and no
// browser is opened here — and it keeps one more that the ordinary trip does not
// have to think about: THE ASKING HAPPENS BEFORE THE ADDRESS EXISTS. There is no
// sign-in page to name until the service has been asked where its sign-in is, so
// this call is on the network for as long as that takes, and ctx bounds it.
func (m *Manager) beginToolServer(ctx context.Context, plug *toolServer) (*Flow, error) {
	service := plug.service
	local, err := listener.New(localServerAddresses)
	if err != nil {
		return nil, fmt.Errorf("connect %s: %w", service.Name, err)
	}
	// The loopback address is both where the browser is sent first and where
	// the service sends it back, which is what makes [Flow.URL] one short
	// address a terminal can print.
	redirect := local.URL.String()

	runContext, cancel := context.WithCancel(context.WithoutCancel(ctx))
	flow := &Flow{
		manager: m,
		plug:    plug,
		cancel:  cancel,
		done:    make(chan flowResult, 1),
	}
	loopback := &mcpLoopback{answers: make(chan mcpAnswer, 1)}
	server := &http.Server{Handler: loopback, ReadHeaderTimeout: mcpAskTimeout}
	go func() { _ = server.Serve(local) }()
	go func() {
		<-runContext.Done()
		// Gracefully, so that a page still being written to the person's
		// browser is not cut off by the connection finishing behind it.
		closing, stop := context.WithTimeout(context.Background(), mcpAskTimeout)
		defer stop()
		if err := server.Shutdown(closing); err != nil {
			_ = server.Close()
		}
	}()

	ready := make(chan string, 1)
	go func() {
		status, err := m.connectToolServer(runContext, plug, redirect, loopback, ready)
		if err != nil {
			flow.finish(Status{Service: service}, fmt.Errorf("connect %s: %w", service.Name, err))
			return
		}
		flow.finish(status, nil)
	}()

	select {
	case flow.url = <-ready:
		return flow, nil
	case result := <-flow.done:
		cancel()
		if result.err != nil {
			return nil, result.err
		}
		return nil, fmt.Errorf("connect %s: the connection ended before it began", service.Name)
	case <-ctx.Done():
		cancel()
		return nil, ctx.Err()
	}
}

// connectToolServer is the whole trip, from the first question to the keys on
// disk. It runs on its own goroutine and reports through [Flow.finish].
func (m *Manager) connectToolServer(ctx context.Context, plug *toolServer, redirect string, loopback *mcpLoopback, ready chan<- string) (Status, error) {
	service := plug.service
	client := &http.Client{Timeout: mcpAskTimeout}

	found, err := discoverSignIn(ctx, client, plug.address)
	if err != nil {
		return Status{}, err
	}

	// The identity from last time is used again whenever it still fits, so
	// that reconnecting costs the service nothing and leaves no trail of
	// abandoned registrations in somebody's console.
	record, held := m.registrations().get(service.ID)
	if !held || !record.fits(plug.address, found.issuer, found.resource, redirect) {
		record, err = introduce(ctx, client, plug, found, redirect)
		if err != nil {
			return Status{}, err
		}
	}

	config := record.config()
	config.RedirectURL = redirect
	// The proof key is generated per connection and never leaves this process,
	// for auth.go's reason: an intercepted round-trip is worth nothing without
	// it.
	verifier := oauth2.GenerateVerifier()
	state := rand.Text()
	loopback.aim(config.AuthCodeURL(state,
		oauth2.S256ChallengeOption(verifier),
		oauth2.SetAuthURLParam("resource", found.resource),
	))
	ready <- redirect

	var answer mcpAnswer
	select {
	case answer = <-loopback.answers:
	case <-ctx.Done():
		return Status{}, ctx.Err()
	}
	if answer.err != nil {
		return Status{}, answer.err
	}
	if answer.state != state {
		// The answer came back from a trip nobody here started.
		return Status{}, fmt.Errorf("the answer did not match the request")
	}
	if err := checkIssuer(answer.issuer, found); err != nil {
		return Status{}, err
	}

	keys, err := config.Exchange(context.WithValue(ctx, oauth2.HTTPClient, client), answer.code,
		oauth2.VerifierOption(verifier),
		oauth2.SetAuthURLParam("resource", found.resource),
	)
	if err != nil {
		return Status{}, err
	}

	// The identity is written first: a failure between the two leaves aforge
	// knowing who it is and not yet connected, which the next attempt fixes
	// without asking the service for a second identity.
	if err := m.registrations().put(service.ID, record); err != nil {
		return Status{}, err
	}
	entry := stored{
		Auth:   authMCP,
		Keys:   keys,
		Scopes: granted(keys, found.scopes),
	}
	if err := m.store.put(service.ID, entry); err != nil {
		return Status{}, err
	}
	return Status{Service: service, Connected: true}, nil
}

// checkIssuer holds the sign-in to its word about naming itself in the answer.
//
// A NAME THAT IS WRONG IS ALWAYS FATAL, whether or not the sign-in promised to
// send one: the whole point of the name is to catch an answer that came back
// from a different sign-in than the one the person was sent to. A name that is
// missing is only fatal when it was promised, because a sign-in that never
// claimed to send one has broken nothing by not sending it.
func checkIssuer(named string, found signIn) error {
	if named == "" {
		if found.stampsIssuer {
			return fmt.Errorf("its sign-in did not name itself on the way back")
		}
		return nil
	}
	if !sameIssuer(named, found.issuer) {
		return fmt.Errorf("the answer came back from somewhere else")
	}
	return nil
}

// introduce registers aforge with one sign-in and returns the identity it was
// issued.
func introduce(ctx context.Context, client *http.Client, plug *toolServer, found signIn, redirect string) (mcpRegistration, error) {
	if strings.TrimSpace(found.register) == "" {
		// THE HONEST SENTENCE. This is a fact about the service, it is not
		// going to change today, and there is nothing here for a person to try
		// differently — so it is said plainly and without a suggestion.
		return mcpRegistration{}, fmt.Errorf("%s does not let a program introduce itself, so it cannot be connected here", plug.service.Name)
	}
	metadata := &oauthex.ClientRegistrationMetadata{
		RedirectURIs:    loopbacks(redirect),
		ClientName:      mcpClientName,
		GrantTypes:      []string{"authorization_code", "refresh_token"},
		ResponseTypes:   []string{"code"},
		ApplicationType: "native",
		Scope:           strings.Join(found.scopes, " "),
	}
	answer, err := oauthex.RegisterClient(ctx, found.register, metadata, client)
	if err != nil {
		return mcpRegistration{}, err
	}
	style := found.style
	if method := strings.TrimSpace(answer.TokenEndpointAuthMethod); method != "" {
		style = tokenStyle([]string{method})
	}
	return mcpRegistration{
		Server:       plug.address,
		Issuer:       found.issuer,
		Resource:     found.resource,
		ClientID:     answer.ClientID,
		ClientSecret: answer.ClientSecret,
		Authorize:    found.authorize,
		Token:        found.token,
		Style:        int(style),
		Redirects:    metadata.RedirectURIs,
		Scopes:       append([]string(nil), found.scopes...),
	}, nil
}

// loopbacks is every address this build may send a browser back to, the one in
// use first.
//
// ALL THREE ARE REGISTERED, NOT ONLY THE ONE IN HAND. The port depends on what
// else is running on the machine at connect time, and an identity registered
// with one port would be useless — and would have to be replaced with another —
// the day the first port was busy. A port picked at random cannot be foreseen
// and is not listed, which is the same limit auth.go's third rung has.
func loopbacks(inUse string) []string {
	addresses := []string{inUse}
	for _, candidate := range localServerAddresses {
		host, port, err := splitAddress(candidate)
		if err != nil || port == "0" || host == "" {
			continue
		}
		if at := "http://localhost:" + port; !slices.Contains(addresses, at) {
			addresses = append(addresses, at)
		}
	}
	return addresses
}

// splitAddress cuts a "host:port" pair the way the loopback list writes them.
func splitAddress(address string) (string, string, error) {
	host, port, found := strings.Cut(address, ":")
	if !found {
		return "", "", fmt.Errorf("no port in %q", address)
	}
	return host, port, nil
}

// mcpAnswer is what the browser eventually came back with.
type mcpAnswer struct {
	code   string
	state  string
	issuer string
	err    error
}

// mcpLoopback is the page on the loopback address: it sends the browser on to
// the service's sign-in, and it catches the answer that comes back.
//
// ONE ADDRESS DOES BOTH, told apart by whether there is an answer in the
// request. That is what lets [Flow.URL] be one short address a person can read
// off a terminal, and it is the same shape the ordinary trip has.
type mcpLoopback struct {
	answers chan mcpAnswer

	mu   sync.Mutex
	page string
}

// aim points the loopback page at the sign-in the person is to be sent to.
func (l *mcpLoopback) aim(page string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.page = page
}

func (l *mcpLoopback) target() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.page
}

func (l *mcpLoopback) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	code, refusal := query.Get("code"), query.Get("error")
	if code == "" && refusal == "" {
		target := l.target()
		if target == "" {
			http.NotFound(w, r)
			return
		}
		http.Redirect(w, r, target, http.StatusFound)
		return
	}
	// THE PAGE IS WRITTEN BEFORE THE ANSWER IS HANDED OVER. Whoever is waiting
	// on the answer finishes the connection and takes the listener down with
	// it; a browser that had not yet been given its page would be left with
	// half of one, on the one screen the person is actually looking at.
	if refusal != "" {
		// The service's own words about why, kept whole for the error and kept
		// out of the page, where they would be machinery in front of somebody
		// who only needs to know they can close the tab.
		if described := strings.TrimSpace(query.Get("error_description")); described != "" {
			refusal = described
		}
		writePage(w, refusedPage)
		l.answer(mcpAnswer{err: fmt.Errorf("%s", collapse(refusal))})
		return
	}
	writePage(w, successPage)
	l.answer(mcpAnswer{code: code, state: query.Get("state"), issuer: query.Get("iss")})
}

// answer hands the first answer to whoever is waiting and drops the rest: a
// browser that replays the address must not be able to start a second exchange.
func (l *mcpLoopback) answer(a mcpAnswer) {
	select {
	case l.answers <- a:
	default:
	}
}

// writePage renders one of the two pages a person may land on.
func writePage(w http.ResponseWriter, page string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = io.WriteString(w, page)
}

// finish records the outcome of a connection this package drove itself, in the
// one place [Flow] keeps it.
//
// It is [Flow.settle]'s twin for the trip that does not go through oauth2cli:
// the storing has already happened, in [Manager.connectToolServer], because
// what a tool-server connection has to write down is more than a key set. THE
// FIRST ANSWER WINS, so a person who pressed escape a moment ago keeps their
// cancellation rather than having it overwritten by a browser tab that finished
// afterwards.
func (f *Flow) finish(status Status, err error) {
	f.mu.Lock()
	if !f.settled {
		f.settled, f.status, f.err = true, status, err
	}
	f.mu.Unlock()
	f.cancel()
	select {
	case f.done <- flowResult{err: err}:
	default:
	}
}
