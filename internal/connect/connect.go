// STUB(connect): replaced by the engine branch on merge.

// Package connect is the door between a session and the accounts a person
// already has somewhere else — Google first, and whatever follows it.
//
// It owns three things and nothing else: WHICH services this build knows about,
// WHETHER one of them is connected on this machine, and an authorized
// [net/http.Client] for one that is. The reading of an account — a mailbox
// search, one message, a stretch of a calendar — is the small family of helpers
// at the bottom, each of which bounds its own answer, because a session that had
// to bound them would be a session that had to know what a mailbox is.
//
// THIS FILE IS A STUB. Every signature here is the real one and every body is
// the honest refusal of a build that cannot do it yet: nothing pretends to
// connect, nothing pretends to read, and the session on the other side reads
// those refusals exactly as it will read a network that is down.
package connect

import (
	"context"
	"errors"
	"net/http"
	"strings"
)

// ClientCredential is the pair a person registers with a service once — an
// application id and its secret — and the thing this package cannot invent. A
// build handed none for a service simply cannot offer it.
type ClientCredential struct{ ID, Secret string }

// Service is one connectable account as the person meets it: the id the tools
// name it by, the name they read, one line about what connecting it buys, and
// the access it will ask for.
type Service struct {
	ID, Name, Blurb string
	Scopes          []string
}

// Status is a Service plus what is true of it on this machine right now.
// Account is the address the person is connected as, and it is EMPTY WHENEVER
// Connected is false — an account name for an account nobody connected is a
// sentence no surface should be able to draw.
type Status struct {
	Service
	Connected bool
	Account   string
}

// Manager is one profile's whole connection state: the services this build
// knows, the credentials it was handed, and whatever is stored on disk for the
// ones already connected.
type Manager struct {
	profileDir string
	creds      map[string]ClientCredential
}

// NewManager builds the manager for one profile directory. It performs no
// network request and touches no account: everything it does happens the first
// time somebody asks for a service.
func NewManager(profileDir string, creds map[string]ClientCredential) (*Manager, error) {
	if strings.TrimSpace(profileDir) == "" {
		return nil, errors.New("connect: a profile directory is required")
	}
	copied := make(map[string]ClientCredential, len(creds))
	for id, credential := range creds {
		copied[id] = credential
	}
	return &Manager{profileDir: profileDir, creds: copied}, nil
}

// Services lists every service this build can offer, connected or not, in a
// stable order. A service whose credential this build was not handed is left
// out: a row that could never be connected is a row that only wastes a person's
// attention.
func (m *Manager) Services() []Status {
	var services []Status
	for _, service := range known {
		if _, held := m.creds[service.ID]; !held {
			continue
		}
		services = append(services, Status{Service: service})
	}
	return services
}

// Connected reports whether one service is ready to be used.
func (m *Manager) Connected(id string) bool {
	for _, status := range m.Services() {
		if status.ID == id && status.Connected {
			return true
		}
	}
	return false
}

// BeginAuth starts connecting one service and hands back the [Flow] the surface
// walks the person through.
func (m *Manager) BeginAuth(ctx context.Context, id string) (*Flow, error) {
	return nil, errNotBuilt
}

// Flow is one connection in progress: the page the person opens, and the wait
// for them to finish with it.
type Flow struct{}

// URL is the page the person opens to say yes. It is empty on a flow that never
// started.
func (f *Flow) URL() string { return "" }

// Wait blocks until the person has finished, the attempt has failed, or ctx
// ends. The Status it returns is the connected one, account and all.
func (f *Flow) Wait(ctx context.Context) (Status, error) { return Status{}, errNotBuilt }

// Client is an HTTP client that carries this profile's authorization for one
// service, refreshing it when it has to. It is the only way anything outside
// this package reaches an account.
func (m *Manager) Client(ctx context.Context, id string) (*http.Client, error) {
	return nil, errNotBuilt
}

// Disconnect forgets one service on this machine.
func (m *Manager) Disconnect(id string) error { return errNotBuilt }

// GmailSearch answers one mailbox query as a numbered list of messages, bounded
// to max rows and to a size a transcript can carry.
func GmailSearch(ctx context.Context, client *http.Client, query string, max int) (string, error) {
	return "", errNotBuilt
}

// GmailRead returns one message as text: who it is from, when, the subject, and
// the body with the markup stripped and the length bounded.
func GmailRead(ctx context.Context, client *http.Client, id string) (string, error) {
	return "", errNotBuilt
}

// CalendarList returns the events between two days, inclusive, as one line
// each. The dates are plain YYYY-MM-DD.
func CalendarList(ctx context.Context, client *http.Client, from, to string) (string, error) {
	return "", errNotBuilt
}

// known is the catalog: what this build could offer a person who has the
// credential for it.
var known = []Service{{
	ID:    "google",
	Name:  "Google",
	Blurb: "search and read your mail, and look at your calendar",
	Scopes: []string{
		"https://www.googleapis.com/auth/gmail.readonly",
		"https://www.googleapis.com/auth/calendar.readonly",
	},
}}

// errNotBuilt is what every hand of this stub answers. The words are the ones a
// person may end up reading, so they say what is true — this build cannot do it
// — rather than naming a package nobody outside this tree has heard of.
var errNotBuilt = errors.New("connecting accounts is not built into this copy of aforge yet")
