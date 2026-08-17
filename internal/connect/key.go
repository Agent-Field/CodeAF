package connect

// The other way an account is connected: a key the person already holds.
//
// Google's way is a trip through a browser and a set of keys aforge renews for
// itself. This way has no trip, no renewal and no expiry — the person pastes a
// key once, it is written to the same file beside everything else, and it is
// exactly as good tomorrow as it was today. What differs between one such
// service and the next is one small fact — where the key rides on a request —
// and that fact comes from the catalog (catalog.go), not from code written per
// service.
//
// THE PACKAGE STILL NEVER LOGS A KEY. It is written to the store file and put
// on outgoing requests, and it appears in no error, no answer and no line of
// text this file builds.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"golang.org/x/oauth2"
)

// keyService is the half of a plug that only a key-connected service has. It is
// an OPTIONAL interface rather than a widening of [Plug]: google.go has no
// address, no key to attach and nothing to prove, and making it answer three
// questions that mean nothing to it would be paying for this feature in the one
// place that does not use it.
type keyService interface {
	Plug
	// read splits one answer from a person into the blank the address is
	// missing and the key itself.
	read(answer string) (blank string, key string, err error)
	// address is where this service answers, with the blank filled in.
	address(blank string) (string, error)
	// sign wraps a transport so that every request through it carries the
	// key the way this service wants it carried.
	sign(base http.RoundTripper, key string) http.RoundTripper
	// check proves a key against the cheap read the catalog names, or says
	// nothing when it names none.
	check(ctx context.Context, client *http.Client) error
}

var _ keyService = (*keyPlug)(nil)

func (p *keyPlug) Service() Service { return p.service }

// Endpoint and AuthCodeOptions are the browser trip's two questions, and a
// service connected with a key makes no browser trip. They answer with nothing,
// and [Manager.BeginAuth] refuses this kind of service before either is asked.
func (p *keyPlug) Endpoint() oauth2.Endpoint                { return oauth2.Endpoint{} }
func (p *keyPlug) AuthCodeOptions() []oauth2.AuthCodeOption { return nil }

// Account answers with nothing, always.
//
// THE EMPTINESS LAW, in the one place it is most tempting to break. A key says
// nothing about whose key it is, and the catalog's cheap check — where there is
// one — answers "yes this works", not "you are ana@example.com". A screen that
// rendered the service's own name, or the first characters of the key, next to
// the word "connected" would be inventing an identity out of nothing.
func (p *keyPlug) Account(context.Context, *http.Client) (string, error) { return "", nil }

// read splits one answer into the blank and the key.
//
// ── THE BLANK COMES FIRST AND THE KEY IS LAST ──
//
// Most services answer at one fixed address and want one thing: the key. A few
// answer at an address with the person's own workspace in it, and those want
// two: the workspace, a space, then the key. One space, in that order, and the
// service's own line (catalog.go's blurb) says so — it is the smallest thing a
// person can be asked to type, and it needs nothing from the screen that asks
// beyond a place to type it.
//
// A key never has a space in it, which is what makes the split safe.
func (p *keyPlug) read(answer string) (string, string, error) {
	parts := strings.Fields(answer)
	if p.blank.name == "" {
		if len(parts) != 1 {
			return "", "", fmt.Errorf("%s needs one key and nothing else", p.service.Name)
		}
		return "", parts[0], nil
	}
	if len(parts) != 2 {
		return "", "", fmt.Errorf("%s needs your %s and then the key, one space between them",
			p.service.Name, strings.ToLower(p.blank.label))
	}
	return parts[0], parts[1], nil
}

// address is where this service answers for this person.
func (p *keyPlug) address(blank string) (string, error) {
	if p.blank.name == "" {
		return p.base, nil
	}
	blank = strings.TrimSpace(blank)
	if blank == "" {
		return "", fmt.Errorf("%s needs your %s before it can be reached",
			p.service.Name, strings.ToLower(p.blank.label))
	}
	filled := fill(p.base, p.blank.name, url.PathEscape(blank))
	if !addressable(filled) {
		return "", fmt.Errorf("%s cannot be reached at that %s", p.service.Name, strings.ToLower(p.blank.label))
	}
	return filled, nil
}

func (p *keyPlug) sign(base http.RoundTripper, key string) http.RoundTripper {
	return signing{base: base, carry: p.carry, key: key}
}

// check makes the one read the catalog says proves a key, and says plainly that
// the service refused it.
//
// A SERVICE WITH NO SUCH READ IS BELIEVED. Almost none of them name one. The
// alternative — guessing at an address that might be free and might charge, and
// might delete something — is worse than letting the first real call be the one
// that says the key is wrong, which it will say in the service's own words.
func (p *keyPlug) check(ctx context.Context, client *http.Client) error {
	if p.probe.address == "" {
		return nil
	}
	method := p.probe.method
	if method == "" {
		method = http.MethodGet
	}
	request, err := http.NewRequestWithContext(ctx, method, p.probe.address, nil)
	if err != nil {
		return err
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes))
	if accepted(p.probe.accepts, response.StatusCode) {
		return nil
	}
	return fmt.Errorf("%s: %s", response.Status, apiMessage(body))
}

// accepted reads the answers the catalog counts as yes, falling back to the two
// every well-behaved service uses when it says nothing.
func accepted(codes []int, status int) bool {
	if len(codes) == 0 {
		return status == http.StatusOK || status == http.StatusNoContent
	}
	for _, code := range codes {
		if code == status {
			return true
		}
	}
	return false
}

// ── signing ─────────────────────────────────────────────────────────────────

// signing puts the key on every request that goes through it.
type signing struct {
	base  http.RoundTripper
	carry carry
	key   string
}

// RoundTrip attaches the key the way this service wants it.
//
// The request is COPIED first. A round tripper is handed a request it does not
// own, and one that writes a header into the caller's request has changed a
// value somebody else may still be holding.
func (s signing) RoundTrip(request *http.Request) (*http.Response, error) {
	out := request.Clone(request.Context())
	switch s.carry.kind {
	case carryHeader:
		value := s.key
		if s.carry.prefix != "" {
			value = s.carry.prefix + " " + s.key
		}
		out.Header.Set(s.carry.name, value)
	case carryQuery:
		query := out.URL.Query()
		query.Set(s.carry.name, s.key)
		out.URL.RawQuery = query.Encode()
	case carryBasic:
		out.SetBasicAuth(s.carry.pair(s.key))
	}
	base := s.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(out)
}

// pair turns the one key a person gave into the two halves the older name-and-
// password shape wants.
//
// A KEY WITH A COLON IN IT IS ALREADY BOTH HALVES — that is how every service
// of this kind writes the pair down, and a person who was told "use api:yourkey"
// should be able to paste exactly that. A key without one goes in as the name
// with nothing as the password, which is what the documentation of nearly every
// such service says to do.
func (c carry) pair(key string) (string, string) {
	if c.format != "" {
		key = strings.Replace(c.format, "%s", key, 1)
	}
	if name, password, found := strings.Cut(key, ":"); found {
		return name, password
	}
	return key, ""
}

// ── connecting ──────────────────────────────────────────────────────────────

// ConnectKey connects one service with a key the person already holds.
//
// It is [Manager.BeginAuth] and [Flow.Wait] in a single call, because there is
// no browser to wait for and nothing in the middle to report on: the person has
// already done the only step there is by the time this is called.
//
// NOTHING IS WRITTEN DOWN UNTIL THE KEY IS BELIEVED. Where the catalog names a
// cheap read, the key makes it first and a refusal fails here, with the
// service's own sentence, and stores nothing — the alternative is a screen that
// says "connected" over a key that was mistyped, and a failure an hour later in
// the middle of somebody's work.
func (m *Manager) ConnectKey(ctx context.Context, id string, key string) (Status, error) {
	plug, err := m.plug(id)
	if err != nil {
		return Status{}, err
	}
	service := plug.Service()
	holder, ok := plug.(keyService)
	if !ok {
		return Status{}, fmt.Errorf("%s is connected in a browser, not with a key", service.Name)
	}
	if strings.TrimSpace(key) == "" {
		return Status{}, fmt.Errorf("%s needs a key", service.Name)
	}
	blank, key, err := holder.read(key)
	if err != nil {
		return Status{}, err
	}
	address, err := holder.address(blank)
	if err != nil {
		return Status{}, err
	}
	entry := stored{Auth: AuthKey, Key: key, Blank: blank}
	if err := holder.check(ctx, m.keyClient(holder, entry)); err != nil {
		return Status{}, fmt.Errorf("%s did not accept that key: %w", service.Name, err)
	}
	if err := m.store.put(service.ID, entry); err != nil {
		return Status{}, fmt.Errorf("connect %s: %w", service.Name, err)
	}
	service.Address = address
	// Account stays empty. See [keyPlug.Account].
	return Status{Service: service, Connected: true}, nil
}

// keyClient is the account-bearing client for one key entry.
func (m *Manager) keyClient(p keyService, entry stored) *http.Client {
	return &http.Client{
		Transport: p.sign(http.DefaultTransport, entry.Key),
		Timeout:   clientTimeout,
	}
}

// located is where one plug answers for one stored connection, or the empty
// string for a service that has no single address of its own.
func located(p Plug, entry stored) (string, error) {
	holder, ok := p.(keyService)
	if !ok {
		return "", nil
	}
	return holder.address(entry.Blank)
}

// ── the raw call ────────────────────────────────────────────────────────────

// requestMethods is every verb this door opens, and it is a closed list on
// purpose: a method nobody named is a method nobody thought about.
var requestMethods = map[string]bool{
	http.MethodGet:    true,
	http.MethodPost:   true,
	http.MethodPut:    true,
	http.MethodPatch:  true,
	http.MethodDelete: true,
}

// ServiceRequest makes one authenticated call against a service's own address
// and hands back what it said, bounded.
//
// ── WHY THIS IS ONE TOOL AND NOT A HUNDRED ──
//
// There are hundreds of services behind a key and no two of them agree on
// anything: what a contact is, how a page is asked for, what a date looks like.
// A hand-written pair of helpers per service is hundreds of files nobody can
// keep true, and a generic "list the objects" would be a promise the catalog
// cannot keep — it knows where a service lives and how a key rides on a
// request, and nothing whatever about what the service holds. So this is the
// honest shape: the address is ours, the key is ours, the path is the model's,
// and the service's own documentation is the schema.
//
// THE PATH IS RELATIVE, ALWAYS. An absolute address here would send the
// person's key to a host nobody vouched for, which is the one thing a signed
// client must never be talked into.
func ServiceRequest(ctx context.Context, client *http.Client, service Service, method, path, query, body string) (string, error) {
	if client == nil {
		return "", errors.New("no connected account for this request")
	}
	base := strings.TrimRight(strings.TrimSpace(service.Address), "/")
	if base == "" {
		return "", fmt.Errorf("%s has no address to call", service.Name)
	}
	method = strings.ToUpper(strings.TrimSpace(method))
	if method == "" {
		method = http.MethodGet
	}
	if !requestMethods[method] {
		return "", fmt.Errorf("%s is not a method this can use (GET, POST, PUT, PATCH, DELETE)", method)
	}
	path = strings.TrimSpace(path)
	if strings.Contains(path, "://") || strings.HasPrefix(path, "//") {
		return "", errors.New("the path is relative to the service's own address, not a whole address of its own")
	}
	if path != "" && !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	address := base + path
	if query = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(query), "?")); query != "" {
		values, err := url.ParseQuery(query)
		if err != nil {
			return "", fmt.Errorf("that query cannot be read: %w", err)
		}
		if strings.Contains(address, "?") {
			address += "&" + values.Encode()
		} else {
			address += "?" + values.Encode()
		}
	}

	var reader io.Reader
	body = strings.TrimSpace(body)
	if body != "" {
		reader = strings.NewReader(body)
	}
	request, err := http.NewRequestWithContext(ctx, method, address, reader)
	if err != nil {
		return "", err
	}
	if body != "" {
		if json.Valid([]byte(body)) {
			request.Header.Set("Content-Type", "application/json")
		} else {
			request.Header.Set("Content-Type", "text/plain; charset=utf-8")
		}
	}
	request.Header.Set("Accept", "application/json")

	response, err := client.Do(request)
	if err != nil {
		return "", err
	}
	defer func() { _ = response.Body.Close() }()
	answer, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes))
	if err != nil {
		return "", err
	}
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return "", fmt.Errorf("%s: %s", response.Status, apiMessage(answer))
	}
	return bound(renderAnswer(service.Name, method, path, response.Status, answer)), nil
}

// renderAnswer is what the caller reads: one line saying what was asked and
// what came back, then the answer itself.
//
// JSON is laid out rather than left on one line. It costs a few characters and
// it is the difference between an answer a reader can follow and a wall.
func renderAnswer(name, method, path, status string, body []byte) string {
	head := name + " · " + method + " " + path + " · " + status
	text := strings.TrimSpace(string(body))
	if text == "" {
		// THE EMPTINESS LAW: a service that said nothing said nothing, and
		// an invented "{}" would be a fact nobody sent.
		return head + "\n\nIt answered with nothing."
	}
	if json.Valid(body) {
		var laid any
		if err := json.Unmarshal(body, &laid); err == nil {
			if pretty, err := json.MarshalIndent(laid, "", "  "); err == nil {
				text = string(pretty)
			}
		}
	}
	return head + "\n\n" + text
}

// Request is [ServiceRequest] with the account looked up: the one door the rest
// of the program uses, so that nothing outside this package has to know where a
// service lives or how its key rides.
func (m *Manager) Request(ctx context.Context, id, method, path, query, body string) (string, error) {
	plug, err := m.plug(id)
	if err != nil {
		return "", err
	}
	service := plug.Service()
	entry, ok := m.standing(plug)
	if !ok {
		return "", fmt.Errorf("%s is not connected", service.Name)
	}
	address, err := located(plug, entry)
	if err != nil {
		return "", err
	}
	service.Address = address
	client, err := m.Client(ctx, id)
	if err != nil {
		return "", err
	}
	return ServiceRequest(ctx, client, service, method, path, query, body)
}
