package livedoor

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
)

// ErrNotRunning reports that no live process currently holds the requested chat.
var ErrNotRunning = errors.New("livedoor: nobody is running that chat")

const (
	// defaultPresenceTTL bounds how long one presence directory walk is reused
	// across channel open requests before the disk is consulted again.
	defaultPresenceTTL = 2 * time.Second

	// maxChatNameLen caps a chat folder name so oversized inputs are rejected
	// before touching presence or the filesystem.
	maxChatNameLen = 128
)

var errNothingHolding = errors.New("engine host: nothing is holding this workspace")

// Holder describes the engine host process holding one workspace's chats, as the
// engine-host package reports it.
type Holder struct {
	Self     remote.HostSelf
	Answered bool
}

// DaemonDoors is how a resolver reaches the engine host that holds a workspace
// without this package importing the engine-host package, which imports the
// sync package that the live door is started from. The program's own entry
// point installs the real ones once at start; a resolver whose doors were never
// installed finds no daemon and answers from the registry alone.
type DaemonDoors struct {
	// Inspect asks the host holding workspace who it is, as the engine-host
	// package does (including filling in a pid an older host does not state).
	Inspect func(workspace string) (Holder, error)
	// Dial opens one connection to that host and never starts one.
	Dial func(workspace string) (net.Conn, error)
}

var (
	daemonDoorsMu sync.RWMutex
	daemonDoors   DaemonDoors
)

// SetDaemonDoors installs the doors a resolver uses to reach an engine host. It
// is meant to be called once, before any resolver runs.
func SetDaemonDoors(d DaemonDoors) {
	daemonDoorsMu.Lock()
	daemonDoors = d
	daemonDoorsMu.Unlock()
}

// DaemonDoorsInstalled says whether both doors are in place, so the program can
// pin that its entry point wired them: without them a chat held by a daemon is
// never found, and nothing else would say so.
func DaemonDoorsInstalled() bool {
	d := installedDaemonDoors()
	return d.Inspect != nil && d.Dial != nil
}

func installedDaemonDoors() DaemonDoors {
	daemonDoorsMu.RLock()
	defer daemonDoorsMu.RUnlock()
	return daemonDoors
}

// Route describes the holder process that answered for a chat and carries the
// transcript path and workspace needed to rewrite the viewer's initial hello.
type Route struct {
	Kind      string
	PID       int
	Session   string
	Workspace string
	Build     string
}

// Resolver locates the OS process holding an already-running chat and dials its
// local attach socket without ever booting or spawning a conversation.
type Resolver interface {
	Resolve(ctx context.Context, chat string) (net.Conn, Route, error)
}

// ResolverOptions configures the seams used by a Resolver so unit tests can
// substitute presence, flock, daemon inspection, and transport dependencies.
type ResolverOptions struct {
	Presence    func(exclude string) []session.SessionPresence
	InUse       func(path string) bool
	Inspect     func(workspace string) (Holder, error)
	DialDaemon  func(workspace string) (net.Conn, error)
	RegistryDir string
	Transport   Transport
	Clock       Clock
	PresenceTTL time.Duration
}

type presenceFlight struct {
	done chan struct{}
	rows []session.SessionPresence
}

type resolver struct {
	presence    func(exclude string) []session.SessionPresence
	inUse       func(path string) bool
	inspect     func(workspace string) (Holder, error)
	dialDaemon  func(workspace string) (net.Conn, error)
	registryDir string
	transport   Transport
	clock       Clock
	presenceTTL time.Duration

	mu       sync.Mutex
	cached   []session.SessionPresence
	cachedAt time.Time
	hasCache bool
	flight   *presenceFlight
}

// NewResolver constructs a Resolver rooted at homeDir, filling any unset option
// with its production default.
func NewResolver(homeDir string, opt ResolverOptions) Resolver {
	homeDir = strings.TrimSpace(homeDir)
	if homeDir == "" {
		homeDir = home.Dir()
	}
	if opt.Presence == nil {
		opt.Presence = session.HomePresence
	}
	if opt.InUse == nil {
		opt.InUse = session.InUse
	}
	if opt.Inspect == nil {
		opt.Inspect = func(workspace string) (Holder, error) {
			if inspect := installedDaemonDoors().Inspect; inspect != nil {
				return inspect(workspace)
			}
			return Holder{}, errNothingHolding
		}
	}
	if opt.DialDaemon == nil {
		opt.DialDaemon = func(workspace string) (net.Conn, error) {
			if dial := installedDaemonDoors().Dial; dial != nil {
				return dial(workspace)
			}
			return nil, errNothingHolding
		}
	}
	if strings.TrimSpace(opt.RegistryDir) == "" {
		opt.RegistryDir = filepath.Join(homeDir, "v3", "live")
	}
	if opt.Transport == nil {
		opt.Transport = UnixTransport()
	}
	if opt.Clock == nil {
		opt.Clock = RealClock()
	}
	if opt.PresenceTTL <= 0 {
		opt.PresenceTTL = defaultPresenceTTL
	}
	return &resolver{
		presence:    opt.Presence,
		inUse:       opt.InUse,
		inspect:     opt.Inspect,
		dialDaemon:  opt.DialDaemon,
		registryDir: opt.RegistryDir,
		transport:   opt.Transport,
		clock:       opt.Clock,
		presenceTTL: opt.PresenceTTL,
	}
}

func (r *resolver) Resolve(ctx context.Context, chat string) (net.Conn, Route, error) {
	if !validChatName(chat) {
		return nil, Route{}, ErrNotRunning
	}
	rows := r.readPresence()
	for _, row := range rows {
		place := session.Place{Dir: row.Dir}
		if place.ID() != chat {
			continue
		}
		transcript := place.Transcript()
		if transcript == "" || row.PID <= 0 || !r.inUse(transcript) {
			continue
		}
		if ws := strings.TrimSpace(row.Workspace); ws != "" {
			if holder, err := r.inspect(ws); err == nil && holder.Self.PID == row.PID {
				conn, err := r.dialDaemon(ws)
				if err != nil || conn == nil {
					return nil, Route{}, ErrNotRunning
				}
				build := strings.TrimSpace(row.Build)
				if build == "" {
					build = strings.TrimSpace(holder.Self.Revision)
				}
				if build == "" {
					build = strings.TrimSpace(holder.Self.Build)
				}
				return conn, Route{
					Kind:      "daemon",
					PID:       row.PID,
					Session:   transcript,
					Workspace: row.Workspace,
					Build:     build,
				}, nil
			}
		}
		conn, regBuild, err := r.dialRegistry(ctx, row.PID)
		if err != nil {
			return nil, Route{}, ErrNotRunning
		}
		build := strings.TrimSpace(regBuild)
		if build == "" {
			build = strings.TrimSpace(row.Build)
		}
		return conn, Route{
			Kind:      "registry",
			PID:       row.PID,
			Session:   transcript,
			Workspace: row.Workspace,
			Build:     build,
		}, nil
	}
	return nil, Route{}, ErrNotRunning
}

func (r *resolver) readPresence() []session.SessionPresence {
	r.mu.Lock()
	now := r.clock.Now()
	if r.hasCache && !now.Before(r.cachedAt) && now.Sub(r.cachedAt) < r.presenceTTL {
		rows := r.cached
		r.mu.Unlock()
		return rows
	}
	if f := r.flight; f != nil {
		r.mu.Unlock()
		<-f.done
		return f.rows
	}
	f := &presenceFlight{done: make(chan struct{})}
	r.flight = f
	r.mu.Unlock()

	rows := r.presence("")

	r.mu.Lock()
	f.rows = rows
	r.cached = rows
	r.cachedAt = r.clock.Now()
	r.hasCache = true
	r.flight = nil
	close(f.done)
	r.mu.Unlock()
	return rows
}

func (r *resolver) dialRegistry(ctx context.Context, pid int) (net.Conn, string, error) {
	path := filepath.Join(r.registryDir, strconv.Itoa(pid)+".json")
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, "", ErrNotRunning
	}
	var entry registryEntry
	if err := json.Unmarshal(raw, &entry); err != nil || entry.PID != pid {
		return nil, "", ErrNotRunning
	}
	sock := strings.TrimSpace(entry.Socket)
	if sock == "" {
		return nil, "", ErrNotRunning
	}
	if !filepath.IsAbs(sock) {
		if !validRelativeSocketName(sock) {
			return nil, "", ErrNotRunning
		}
		sock = filepath.Join(r.registryDir, sock)
	}
	conn, err := r.transport.Dial(ctx, sock)
	if err != nil || conn == nil {
		return nil, "", ErrNotRunning
	}
	return conn, entry.Build, nil
}

func validChatName(chat string) bool {
	if chat == "" || len(chat) > maxChatNameLen {
		return false
	}
	if chat == "." || chat == ".." {
		return false
	}
	if strings.TrimSpace(chat) != chat {
		return false
	}
	if strings.ContainsAny(chat, "/\\\x00") {
		return false
	}
	return filepath.Base(chat) == chat
}

func validRelativeSocketName(name string) bool {
	if name == "" || name == "." || name == ".." {
		return false
	}
	if strings.ContainsAny(name, "/\\\x00") {
		return false
	}
	return filepath.Base(name) == name
}
