package appapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	factoryapi "github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/filelock"
	"github.com/Agent-Field/codeaf/internal/guard"
)

// FactoryRegistry keeps connection addresses on disk and credentials in memory.
// The binary host supplies the solo service and the elected worker's power door.
type FactoryRegistry struct {
	mu               sync.Mutex
	mountMu          sync.RWMutex
	Home, URL, Token string
	Credentials      *config.AppCredentials
	Solo             func(context.Context) (http.Handler, error)
	Changed          func()
	Power            func(context.Context, bool) error
	Tasks            FactoryTaskSource
	Client           *http.Client
	server           *Server
	handler          http.Handler
	cards            map[string]*factoryCard
	closed           bool
	seen             map[string]Factory
	sourceTokens     map[string]string
}
type factoryConnection struct {
	ID   string `json:"id"`
	URL  string `json:"url"`
	Mode string `json:"mode"`
}
type factoryConnections struct {
	URL       string              `json:"url,omitempty"`
	Factories []factoryConnection `json:"factories"`
}

// FactoryTaskSource lets the task resource keep ownership of its state and
// cancellation policy while the factory lane owns the outward job creation.
type FactoryTaskSource interface {
	FactoryTask(context.Context, string) (TaskResponse, error)
	HandedToFactory(context.Context, string, SourceRef) error
}
type factoryCard struct {
	Complete bool
	HandOffCard
	Agent       string
	Task        string
	Version     uint64
	TaskVersion uint64
	TaskState   string
	Result      *HandOffResponse
	Expires     time.Time
}

func factoryID(address string) string {
	sum := sha256.Sum256([]byte(address))
	return "factory_" + hex.EncodeToString(sum[:8])
}
func factoryOrigin(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") || (u.Scheme != "https" && u.Scheme != "http") {
		return "", agentError(422, "bad_input", "use a factory HTTPS origin without a path or credentials")
	}
	ip := net.ParseIP(u.Hostname())
	if u.Scheme == "http" && u.Hostname() != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return "", agentError(422, "bad_input", "team factories require HTTPS")
	}
	return u.Scheme + "://" + strings.ToLower(u.Host), nil
}
func (f *FactoryRegistry) connections(change func(*factoryConnections) error) ([]factoryConnection, error) {
	path := filepath.Join(f.Home, "v3", "factory", "attach.json")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	lock, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	defer lock.Close()
	if err = filelock.Lock(lock, true, false); err != nil {
		return nil, err
	}
	defer filelock.Unlock(lock)
	var saved factoryConnections
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err == nil && json.Unmarshal(data, &saved) != nil {
		return nil, errors.New("invalid factory connections")
	}
	if len(saved.Factories) == 0 && saved.URL != "" {
		if address, e := factoryOrigin(saved.URL); e == nil {
			saved.Factories = append(saved.Factories, factoryConnection{factoryID(address), address, "team"})
		}
	}
	if change != nil {
		if err = change(&saved); err != nil {
			return nil, err
		}
		saved.URL = ""
		for _, row := range saved.Factories {
			if row.Mode == "team" {
				saved.URL = row.URL
				break
			}
		}
		data, err = json.Marshal(saved)
		if err != nil {
			return nil, err
		}
		tmp, err := os.CreateTemp(filepath.Dir(path), ".attach-*")
		if err != nil {
			return nil, err
		}
		defer os.Remove(tmp.Name())
		if err = tmp.Chmod(0600); err == nil {
			_, err = tmp.Write(append(data, '\n'))
		}
		closeErr := tmp.Close()
		if err == nil {
			err = closeErr
		}
		if err == nil {
			err = os.Rename(tmp.Name(), path)
		}
		if err != nil {
			return nil, err
		}
	}
	return saved.Factories, nil
}
func (f *FactoryRegistry) source(row factoryConnection) *FactoryInboxSource {
	token := f.Token
	address := f.URL
	if row.Mode == "team" {
		address = row.URL
		token = ""
		if f.Credentials != nil {
			token = f.Credentials.FactorySession(address)
		}
	}
	return &FactoryInboxSource{ID: row.ID, URL: address, Token: token, Client: f.Client}
}
func (f *FactoryRegistry) view(ctx context.Context, row factoryConnection) Factory {
	out := Factory{ID: row.ID, URL: row.URL, Mode: row.Mode, Name: "Factory", State: "unreachable"}
	source := f.source(row)
	if row.Mode == "solo" {
		out.URL = f.URL
		out.Name = "This machine"
	}
	if row.Mode == "team" && source.Token == "" {
		out.State = "signed_out"
		return out
	}
	var me factoryapi.Me
	if err := source.request(ctx, "GET", "/v1/me", nil, &me); err != nil {
		var failure *agentReplyError
		if errors.As(err, &failure) && failure.status == 401 {
			out.State = "signed_out"
		}
		return out
	}
	out.Name = me.Factory.Name
	out.Operator = me.Operator
	out.State = "ok"
	now := time.Now().UTC()
	out.LastSeen = &now
	return out
}

// BindFactories mounts the native factory on the existing authenticated listener.
// Restoring an attachment also restores its person-scoped Inbox producer.
func (s *Server) BindFactories(f *FactoryRegistry) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.server != nil {
		return errors.New("factory registry already bound")
	}
	f.server = s
	f.cards = map[string]*factoryCard{}
	f.seen = map[string]Factory{}
	f.sourceTokens = map[string]string{}
	s.options.Factories = f
	s.Handle("/v1/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mountMu.RLock()
		handler := f.handler
		f.mountMu.RUnlock()
		if handler == nil {
			fail(w, 404, "no_factory", "start a solo factory on this machine")
			return
		}
		handler.ServeHTTP(w, r)
	}))
	rows, err := f.connections(nil)
	if err != nil {
		return err
	}
	for _, row := range rows {
		if row.Mode == "solo" {
			if f.Solo == nil {
				return errors.New("solo factory host unavailable")
			}
			var handler http.Handler
			handler, err = f.Solo(context.Background())
			f.setHandler(handler)
			if err != nil {
				return err
			}
		}
		s.SetInboxSource(row.ID, f.inboxSource(row))
	}
	if f.Changed != nil {
		f.Changed()
	}
	guard.Go("app/factory connections", func() { f.watch(s.inbox.ctx) })
	return nil
}
func (f *FactoryRegistry) publish(row factoryConnection, state string) {
	_, _ = f.server.events.Publish(EventFactoryChanged, FactoryChangedEvent{row.ID, state})
}
func (f *FactoryRegistry) list(ctx context.Context) (FactoriesResponse, error) {
	rows, err := f.connections(nil)
	out := FactoriesResponse{}
	for _, row := range rows {
		fresh := f.view(ctx, row)
		if fresh.LastSeen == nil {
			fresh.LastSeen = f.seen[row.ID].LastSeen
		}
		out = append(out, fresh)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, err
}

func (f *FactoryRegistry) setHandler(handler http.Handler) {
	f.mountMu.Lock()
	f.handler = handler
	f.mountMu.Unlock()
}

// RefreshFactories also catches a stdin handoff arriving after hello. A token
// replacement restarts the factory stream without ever persisting the token.
func (s *Server) RefreshFactories(ctx context.Context) error {
	f := s.options.Factories
	if f == nil {
		return nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return nil
	}
	rows, err := f.connections(nil)
	if err != nil {
		return err
	}
	present := map[string]bool{}
	for _, row := range rows {
		present[row.ID] = true
		source := f.source(row)
		signature := source.URL + "\x00" + source.Token
		if f.sourceTokens[row.ID] != signature {
			s.SetInboxSource(row.ID, &factoryEventSource{source})
			f.sourceTokens[row.ID] = signature
		}
		fresh := f.view(ctx, row)
		old, ok := f.seen[row.ID]
		if fresh.LastSeen == nil {
			fresh.LastSeen = old.LastSeen
		}
		if !ok || old.State != fresh.State || old.Name != fresh.Name || old.Operator != fresh.Operator {
			f.publish(row, fresh.State)
		}
		f.seen[row.ID] = fresh
	}
	for id := range f.seen {
		if !present[id] {
			delete(f.seen, id)
			delete(f.sourceTokens, id)
			s.SetInboxSource(id, nil)
		}
	}
	return nil
}
func (f *FactoryRegistry) watch(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = f.server.RefreshFactories(ctx)
		}
	}
}
func (s *Server) closeFactories() {
	if f := s.options.Factories; f != nil {
		f.mu.Lock()
		f.closed = true
		f.mu.Unlock()
	}
}
