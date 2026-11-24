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

	mu     sync.Mutex
	closed bool
	// conns is every accepted connection, each with the chat its hello named
	// ("" until that hello has been resolved), which is what lets CloseChat end
	// one chat's viewers and no others.
	conns     map[net.Conn]string
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
		conns:    make(map[net.Conn]string),
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
		l.conns[conn] = ""
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
						// THE CONNECTION IS FILED UNDER ITS CHAT BEFORE THE CHAT IS
						// ASKED FOR, and unfiled again if the answer is no. The
						// holder lets a chat go (drops it from what open answers)
						// and only then calls CloseChat, so a hello that lands in
						// between is either filed in time to be closed or refused.
						l.file(conn, target)
						if sess, ok := l.open(target); ok && sess != nil {
							return sess, nil
						}
						l.file(conn, "")
					}
					return nil, errors.New("that conversation is not open here")
				},
			})
		})
	}
}

// file records which chat a connection opened, under the spelling CloseChat
// compares by.
func (l *Listener) file(conn net.Conn, chat string) {
	key := ""
	if chat != "" {
		key = chatKey(chat)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, ok := l.conns[conn]; ok {
		l.conns[conn] = key
	}
}

// CloseChat ends every viewer connection this listener has handed to chat, and
// only those, and says how many it ended. IT IS WHAT A HOLDER CALLS WHEN IT LETS
// A CHAT GO: a closed remote.Session tells its attached windows nothing and
// leaves their pipes open, while the live door's router reads the end of the
// holder's local connection as the holder having gone and ends the viewer's
// channel with `holder-gone`, the word every viewer already says for it. Without
// this a viewer of a chat the holder closed would sit on a view nobody feeds.
//
// The chat is compared by its physical path, so a symlinked spelling (the macOS
// temporary folder is one) names the same chat the holder's own spelling does.
func (l *Listener) CloseChat(chat string) int {
	key := chatKey(chat)
	if key == "" {
		return 0
	}
	l.mu.Lock()
	var doomed []net.Conn
	for c, k := range l.conns {
		if k == key {
			doomed = append(doomed, c)
		}
	}
	l.mu.Unlock()
	for _, c := range doomed {
		_ = c.Close()
	}
	return len(doomed)
}

// CloseAllChats ends every viewer connection this listener has handed to any
// chat, and says how many it ended. It is what turning remote control off does
// for the people already looking, beside the door's own refusal of anyone new:
// each viewer reads the end of the holder's connection as the holder having gone,
// exactly as it does for one chat (see [Listener.CloseChat]). A connection that
// has not yet opened a chat is not a viewer of one and is left to its own
// refusal.
func (l *Listener) CloseAllChats() int {
	l.mu.Lock()
	var doomed []net.Conn
	for c, k := range l.conns {
		if k != "" {
			doomed = append(doomed, c)
		}
	}
	l.mu.Unlock()
	for _, c := range doomed {
		_ = c.Close()
	}
	return len(doomed)
}

// chatKey is the one spelling of a transcript path the listener compares by: the
// cleaned path with every symlink resolved, falling back to the resolved folder
// for a file that does not exist yet.
func chatKey(transcript string) string {
	transcript = strings.TrimSpace(transcript)
	if transcript == "" {
		return ""
	}
	path := filepath.Clean(transcript)
	if physical, err := filepath.EvalSymlinks(path); err == nil {
		return physical
	}
	if dir, err := filepath.EvalSymlinks(filepath.Dir(path)); err == nil {
		return filepath.Join(dir, filepath.Base(path))
	}
	return path
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
