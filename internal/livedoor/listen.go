package livedoor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/buildinfo"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/remote"
)

const staleDialTimeout = 100 * time.Millisecond

type registryEntry struct {
	PID    int    `json:"pid"`
	Socket string `json:"socket"`
	Build  string `json:"build"`
}

// Listener accepts local holder connections for desk and in-process sessions
// and publishes the holder's PID and relative socket name under <home>/v3/live.
type Listener struct {
	ln       net.Listener
	open     func(transcript string) (*remote.Session, bool)
	jsonPath string
	sockPath string

	mu        sync.Mutex
	closed    bool
	conns     map[net.Conn]struct{}
	wg        sync.WaitGroup
	closeOnce sync.Once
}

// Listen binds <home>/v3/live/<pid>.sock, sweeps stale registry files of dead
// processes in that directory, writes <home>/v3/live/<pid>.json atomically with
// mode 0600, and serves accepted connections with remote.ServeAttach.
func Listen(homeDir string, open func(transcript string) (*remote.Session, bool), tr Transport) (*Listener, error) {
	homeDir = strings.TrimSpace(homeDir)
	if homeDir == "" {
		homeDir = home.Dir()
	}
	if tr == nil {
		tr = UnixTransport()
	}
	pid := os.Getpid()
	dir := filepath.Join(homeDir, "v3", "live")
	sockName := strconv.Itoa(pid) + ".sock"
	sockPath := filepath.Join(dir, sockName)
	jsonPath := filepath.Join(dir, strconv.Itoa(pid)+".json")

	if err := checkUnixSocketPath(sockPath); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("livedoor: create live registry dir: %w", err)
	}
	_ = os.Chmod(dir, 0o700)

	sweepStaleRegistry(dir, pid, tr)
	_ = os.Remove(sockPath)

	ln, err := tr.Listen(sockPath)
	if err != nil {
		return nil, err
	}
	_ = os.Chmod(sockPath, 0o600)

	entry := registryEntry{
		PID:    pid,
		Socket: sockName,
		Build:  currentBuildString(),
	}
	if err := writeRegistryAtomic(dir, jsonPath, entry); err != nil {
		_ = ln.Close()
		_ = os.Remove(sockPath)
		return nil, err
	}

	l := &Listener{
		ln:       ln,
		open:     open,
		jsonPath: jsonPath,
		sockPath: sockPath,
		conns:    make(map[net.Conn]struct{}),
	}
	l.wg.Add(1)
	guard.Go("livedoor.listen.accept", l.acceptLoop)
	return l, nil
}

// Path returns the path of the registry JSON file written by this Listener.
func (l *Listener) Path() string {
	return l.jsonPath
}

// Close stops accepting connections, closes any active served connections, and
// removes both the registry JSON file and the unix socket file.
func (l *Listener) Close() error {
	l.closeOnce.Do(func() {
		l.mu.Lock()
		l.closed = true
		conns := make([]net.Conn, 0, len(l.conns))
		for c := range l.conns {
			conns = append(conns, c)
		}
		l.mu.Unlock()

		_ = l.ln.Close()
		for _, c := range conns {
			_ = c.Close()
		}
		l.wg.Wait()
		_ = os.Remove(l.jsonPath)
		_ = os.Remove(l.sockPath)
	})
	return nil
}

func (l *Listener) acceptLoop() {
	defer l.wg.Done()
	for {
		conn, err := l.ln.Accept()
		if err != nil {
			return
		}
		l.mu.Lock()
		if l.closed {
			l.mu.Unlock()
			_ = conn.Close()
			return
		}
		l.conns[conn] = struct{}{}
		l.wg.Add(1)
		l.mu.Unlock()

		guard.Go("livedoor.listen.conn", func() {
			defer func() {
				l.mu.Lock()
				delete(l.conns, conn)
				l.mu.Unlock()
				_ = conn.Close()
				l.wg.Done()
			}()
			_ = remote.ServeAttach(conn, conn, remote.AttachOptions{
				Open: func(hello remote.Hello) (*remote.Session, error) {
					if hello.New {
						return nil, errors.New("that conversation is not open here")
					}
					target := strings.TrimSpace(hello.Session)
					if target != "" && l.open != nil {
						if sess, ok := l.open(target); ok && sess != nil {
							return sess, nil
						}
					}
					return nil, errors.New("that conversation is not open here")
				},
			})
		})
	}
}

func writeRegistryAtomic(dir, jsonPath string, entry registryEntry) error {
	raw, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".live-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	_ = tmp.Chmod(0o600)
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, jsonPath); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	_ = os.Chmod(jsonPath, 0o600)
	return nil
}

// sweepStaleRegistry removes any <pid>.json file in dir belonging to another
// process whose socket no longer accepts a connection.
func sweepStaleRegistry(dir string, selfPID int, tr Transport) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, d := range entries {
		if d.IsDir() {
			continue
		}
		name := d.Name()
		if !strings.HasSuffix(name, ".json") || strings.HasPrefix(name, ".") {
			continue
		}
		stem := strings.TrimSuffix(name, ".json")
		filePID, err := strconv.Atoi(stem)
		if err != nil || filePID == selfPID {
			continue
		}
		jsonFile := filepath.Join(dir, name)
		raw, err := os.ReadFile(jsonFile)
		if err != nil {
			continue
		}
		var reg registryEntry
		if err := json.Unmarshal(raw, &reg); err != nil || reg.PID <= 0 || !validRelativeSocketName(strings.TrimSpace(reg.Socket)) {
			_ = os.Remove(jsonFile)
			continue
		}
		sockFile := filepath.Join(dir, strings.TrimSpace(reg.Socket))
		ctx, cancel := context.WithTimeout(context.Background(), staleDialTimeout)
		conn, dialErr := tr.Dial(ctx, sockFile)
		cancel()
		if dialErr != nil {
			// A DIAL THAT TIMED OUT IS A BUSY HOLDER, NOT A DEAD ONE. Only a
			// refusal or a missing socket proves nobody is listening, and
			// removing a live holder's file would make its chats unreachable
			// until it restarted.
			if errors.Is(dialErr, context.DeadlineExceeded) || errors.Is(dialErr, os.ErrDeadlineExceeded) {
				continue
			}
			_ = os.Remove(jsonFile)
			_ = os.Remove(sockFile)
			continue
		}
		_ = conn.Close()
	}
}

// currentBuildString is the build stamp the presence file and the whois
// answer already carry, read once, so a registry file names the same build the
// rest of the product says.
var currentBuildString = sync.OnceValue(func() string {
	if b := strings.TrimSpace(buildinfo.String()); b != "" && !strings.ContainsAny(b, `/\`) {
		return b
	}
	return "dev"
})
