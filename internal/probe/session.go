package probe

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Manager owns the probe root: its tmux socket and its session registry.
type Manager struct {
	Root       string
	SocketPath string
	registry   *registry
}

type registry struct {
	Root     string                 `json:"root"`
	Socket   string                 `json:"socket"`
	Build    *BuildIdentity         `json:"build,omitempty"`
	Sessions map[string]*SessionRec `json:"sessions"`
}

type SessionRec struct {
	ID        string    `json:"id"`
	TmuxSess  string    `json:"tmux_session"`
	Socket    string    `json:"socket"`
	Profile   string    `json:"profile"`
	Binary    string    `json:"binary"`
	Home      string    `json:"home"`
	Width     int       `json:"width"`
	Height    int       `json:"height"`
	Revision  int       `json:"revision"`
	StartedAt time.Time `json:"started_at"`
}

var errNoSession = errors.New("no such probe session")

// ErrWaitTimeout marks a bounded wait that did not reach its condition in
// time. It is reported on the wire as TIMEOUT, never BAD_REQUEST: the request
// itself was well-formed.
var ErrWaitTimeout = errors.New("wait timeout")

func Open(root string) (*Manager, error) {
	if root == "" {
		return nil, fmt.Errorf("probe root name required")
	}
	if strings.ContainsAny(root, `/\`) {
		return nil, fmt.Errorf("probe root must be a plain name")
	}
	base := ProbeBase
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		base = filepath.Join(home, ".codeaf-probe")
	}
	rroot := filepath.Join(base, root)
	m := &Manager{Root: rroot, SocketPath: socketPath(rroot)}
	if err := os.MkdirAll(filepath.Join(rroot, "homes"), 0o700); err != nil {
		return nil, err
	}
	// The -S socket must live in a directory that already exists.
	if err := os.MkdirAll(filepath.Dir(m.SocketPath), 0o700); err != nil {
		return nil, err
	}
	m.registry = &registry{Root: rroot, Socket: m.SocketPath, Sessions: map[string]*SessionRec{}}
	if b, err := os.ReadFile(filepath.Join(rroot, "registry.json")); err == nil {
		if err := json.Unmarshal(b, m.registry); err != nil {
			return nil, fmt.Errorf("registry corrupt: %w", err)
		}
	}
	if m.registry.Sessions == nil {
		m.registry.Sessions = map[string]*SessionRec{}
	}
	m.recoverStale()
	return m, m.save()
}

func (m *Manager) save() error {
	b, err := json.MarshalIndent(m.registry, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(m.Root, "registry.json"), b, 0o600)
}

func (m *Manager) recoverStale() {
	live := map[string]bool{}
	if out, err := m.tmux("list-sessions", "-F", "#{session_name}"); err == nil {
		for _, name := range strings.Split(strings.TrimSpace(out), "\n") {
			live[name] = true
		}
	}
	for id, rec := range m.registry.Sessions {
		if !live[rec.TmuxSess] {
			delete(m.registry.Sessions, id)
		}
	}
}

// ProbeBase overrides where probe roots live. Empty means ~/.codeaf-probe.
// Tests set it to a short temp path because a macOS unix socket path must
// stay under ~104 bytes.
var ProbeBase string

func socketPath(root string) string {
	return filepath.Join(root, "tmux.sock")
}

// tmux runs a tmux command against this root's dedicated socket. TMUX_TMPDIR
// is pinned to the probe root so the socket lands under it.
func (m *Manager) tmux(args ...string) (string, error) {
	cmd := exec.Command("tmux", append([]string{"-S", m.SocketPath}, args...)...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func SanitizeEnv(home string, optIn map[string]string) []string {
	env := []string{
		"HOME=" + home,
		"PATH=" + os.Getenv("PATH"),
		"TERM=xterm-256color",
		"LANG=en_US.UTF-8",
		"SHELL=/bin/sh",
		"CODEAF_TELEMETRY=off",
	}
	for k, v := range optIn {
		if !secretish(k) {
			env = append(env, k+"="+v)
		}
	}
	return env
}

func secretish(key string) bool {
	u := strings.ToUpper(key)
	return strings.HasSuffix(u, "API_KEY") ||
		strings.Contains(u, "TOKEN") || strings.Contains(u, "SECRET") ||
		strings.Contains(u, "PASSWD") || strings.Contains(u, "PASSWORD") ||
		strings.Contains(u, "CREDENTIAL")
}

func Redact(s string) string {
	for _, part := range strings.Fields(s) {
		if i := strings.Index(part, "="); i > 0 && secretish(part[:i]) {
			s = strings.Replace(s, part, part[:i]+"=***", 1)
		}
	}
	return s
}

func (m *Manager) Start(sessionID, binary, profile string, args []string, optIn map[string]string, dims Dims) (StartData, error) {
	if sessionID == "" || binary == "" {
		return StartData{}, fmt.Errorf("session_id and binary required: %w", errNoSession)
	}
	if _, exists := m.registry.Sessions[sessionID]; exists {
		if _, err := m.tmux("has-session", "-t", m.tmuxName(sessionID)); err == nil {
			return StartData{}, fmt.Errorf("session %q already exists", sessionID)
		}
		delete(m.registry.Sessions, sessionID)
	}
	if dims.Width <= 0 {
		dims.Width = 170
	}
	if dims.Height <= 0 {
		dims.Height = 50
	}
	home := filepath.Join(m.Root, "homes", sessionID)
	if err := os.MkdirAll(home, 0o700); err != nil {
		return StartData{}, err
	}
	name := m.tmuxName(sessionID)
	cmdline := append([]string{binary}, args...)
	quoted := make([]string, len(cmdline))
	for i, a := range cmdline {
		quoted[i] = "'" + strings.ReplaceAll(a, "'", "'\\''") + "'"
	}
	env := strings.Join(SanitizeEnv(home, optIn), " ")
	shell := fmt.Sprintf("cd '%s' && %s %s; sleep 3600", home, env, strings.Join(quoted, " "))
	if _, err := m.tmux("new-session", "-d", "-s", name,
		"-x", strconv.Itoa(dims.Width), "-y", strconv.Itoa(dims.Height), shell); err != nil {
		return StartData{}, fmt.Errorf("tmux new-session: %v", trimErr(err))
	}
	rec := &SessionRec{
		ID: sessionID, TmuxSess: name, Socket: m.SocketPath, Profile: profile,
		Binary: binary, Home: home, Width: dims.Width, Height: dims.Height,
		Revision: 0, StartedAt: time.Now().UTC(),
	}
	m.registry.Sessions[sessionID] = rec
	if err := m.save(); err != nil {
		return StartData{}, err
	}
	return StartData{SessionID: sessionID, Socket: m.SocketPath, Profile: profile, Dims: dims}, nil
}

func (m *Manager) tmuxName(id string) string { return "probe-" + id }

func (m *Manager) rec(id string) (*SessionRec, error) {
	rec, ok := m.registry.Sessions[id]
	if !ok {
		return nil, fmt.Errorf("session %q: %w", id, errNoSession)
	}
	return rec, nil
}

func (m *Manager) Observe(sessionID string) (ObserveData, error) {
	rec, err := m.rec(sessionID)
	if err != nil {
		return ObserveData{}, err
	}
	snap, err := m.tmux("capture-pane", "-p", "-t", rec.TmuxSess)
	if err != nil {
		return ObserveData{}, fmt.Errorf("capture-pane: %v", trimErr(err))
	}
	cur, _ := m.tmux("display-message", "-p", "-t", rec.TmuxSess, "#{cursor_x} #{cursor_y}")
	var c Cursor
	fmt.Sscanf(strings.TrimSpace(cur), "%d %d", &c.X, &c.Y)
	return ObserveData{
		Revision:  rec.Revision,
		Snapshot:  strings.TrimSuffix(snap, "\n"),
		Cursor:    c,
		Processes: m.processes(rec),
		TS:        time.Now().UTC().Format(time.RFC3339),
	}, nil
}

func (m *Manager) processes(rec *SessionRec) []string {
	pidOut, err := m.tmux("display-message", "-p", "-t", rec.TmuxSess, "#{pane_pid}")
	if err != nil {
		return nil
	}
	pid := strings.TrimSpace(pidOut)
	var out []string
	if p, err := exec.Command("ps", "-o", "pid=,comm=", "-p", pid).Output(); err == nil {
		out = append(out, strings.TrimSpace(string(p)))
	}
	for _, k := range childPIDs(pid) {
		if p, err := exec.Command("ps", "-o", "pid=,comm=", "-p", k).Output(); err == nil {
			out = append(out, strings.TrimSpace(string(p)))
		}
	}
	return out
}

func childPIDs(pid string) []string {
	out, err := exec.Command("pgrep", "-P", pid).Output()
	if err != nil {
		return nil
	}
	return strings.Fields(string(out))
}

func (m *Manager) Act(sessionID string, req ActRequest) (ActData, error) {
	rec, err := m.rec(sessionID)
	if err != nil {
		return ActData{}, err
	}
	data := ActData{RevisionBefore: rec.Revision}
	if req.ExpectRevision != nil && *req.ExpectRevision != rec.Revision {
		data.Stale = true
		return data, nil
	}
	// A wait is a modifier, not a mutation: an act may carry exactly one of
	// text/keys/resize (or none, wait-only) plus an optional bounded wait,
	// which is what makes act atomic act+wait+observe.
	nMut := 0
	if req.Text != "" {
		nMut++
	}
	if req.Keys != "" {
		nMut++
	}
	if req.Resize != nil {
		nMut++
	}
	if nMut > 1 {
		return data, fmt.Errorf("act takes exactly one of text/keys/resize (wait is a modifier)")
	}
	switch {
	case req.Text != "":
		if _, err := m.tmux("send-keys", "-t", rec.TmuxSess, "-l", req.Text); err != nil {
			return data, fmt.Errorf("send-keys: %v", trimErr(err))
		}
	case req.Keys != "":
		if _, err := m.tmux("send-keys", "-t", rec.TmuxSess, req.Keys); err != nil {
			return data, fmt.Errorf("send-keys: %v", trimErr(err))
		}
	case req.Resize != nil:
		if req.Resize.Width <= 0 || req.Resize.Height <= 0 {
			return data, fmt.Errorf("resize width/height must be positive")
		}
		if _, err := m.tmux("resize-window", "-t", rec.TmuxSess,
			"-x", strconv.Itoa(req.Resize.Width), "-y", strconv.Itoa(req.Resize.Height)); err != nil {
			return data, fmt.Errorf("resize-window: %v", trimErr(err))
		}
		rec.Width, rec.Height = req.Resize.Width, req.Resize.Height
	}
	if req.Wait != nil {
		if _, err := m.waitQuiet(rec, req.Wait.QuietMs, req.Wait.TimeoutMs); err != nil {
			// The act itself went through; only the bounded wait timed out.
			// That is a TIMEOUT, not a bad request — callers must be able to
			// tell the difference (errors.Is).
			return data, fmt.Errorf("%w: wait did not reach quiet within timeout", ErrWaitTimeout)
		}
	}
	rec.Revision++
	if err := m.save(); err != nil {
		return data, err
	}
	data.Accepted = true
	data.RevisionAfter = rec.Revision
	return data, nil
}

func (m *Manager) waitQuiet(rec *SessionRec, quietMs, timeoutMs int) (bool, error) {
	if quietMs <= 0 {
		quietMs = 300
	}
	if timeoutMs <= 0 {
		timeoutMs = 10000
	}
	if timeoutMs < quietMs {
		timeoutMs = quietMs
	}
	deadline := time.Now().Add(time.Duration(timeoutMs) * time.Millisecond)
	var quietSince time.Time
	var last string
	for time.Now().Before(deadline) {
		snap, err := m.tmux("capture-pane", "-p", "-t", rec.TmuxSess)
		if err != nil {
			return false, trimErr(err)
		}
		if snap == last {
			if quietSince.IsZero() {
				quietSince = time.Now()
			}
			if time.Since(quietSince) >= time.Duration(quietMs)*time.Millisecond {
				return true, nil
			}
		} else {
			last = snap
			quietSince = time.Time{}
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false, errors.New("timeout")
}

func (m *Manager) SetBuild(b BuildIdentity) (PrepareData, error) {
	data := PrepareData{Build: b}
	if m.registry.Build != nil && sameBuild(*m.registry.Build, b) {
		data.Reused = true
		return data, nil
	}
	m.registry.Build = &b
	return data, m.save()
}

func sameBuild(a, b BuildIdentity) bool {
	if a.SHA != b.SHA || a.Dirty != b.Dirty || a.GoVersion != b.GoVersion ||
		a.Binary != b.Binary || len(a.Flags) != len(b.Flags) {
		return false
	}
	for i := range a.Flags {
		if a.Flags[i] != b.Flags[i] {
			return false
		}
	}
	return true
}

func (m *Manager) Finish(sessionID string) error {
	rec, err := m.rec(sessionID)
	if err != nil {
		return err
	}
	_, _ = m.tmux("kill-session", "-t", rec.TmuxSess)
	os.RemoveAll(rec.Home)
	delete(m.registry.Sessions, sessionID)
	return m.save()
}

func (m *Manager) KillServer() error {
	if filepath.Base(m.SocketPath) != "tmux.sock" || !strings.HasPrefix(m.SocketPath, m.Root+string(os.PathSeparator)) {
		return fmt.Errorf("refusing to kill a foreign tmux server")
	}
	_, _ = m.tmux("kill-server")
	return nil
}

func trimErr(err error) error { return errors.New(strings.TrimSpace(err.Error())) }

// ObserveWithWait captures the pane after a bounded quiet wait, so an
// observe can settle before reading.
func (m *Manager) ObserveWithWait(sessionID string, w ActWait) (ObserveData, error) {
	rec, err := m.rec(sessionID)
	if err != nil {
		return ObserveData{}, err
	}
	if _, err := m.waitQuiet(rec, w.QuietMs, w.TimeoutMs); err != nil {
		// fall through: return the snapshot we have
		_ = err
	}
	return m.Observe(sessionID)
}

// Wait performs a bounded quiet wait and reports the truthful outcome:
// settled true when the screen reached the quiet window in time, false on
// timeout. The returned observe data is the snapshot at the wait's end.
func (m *Manager) Wait(sessionID string, w ActWait) (bool, ObserveData, error) {
	rec, err := m.rec(sessionID)
	if err != nil {
		return false, ObserveData{}, err
	}
	settled, err := m.waitQuiet(rec, w.QuietMs, w.TimeoutMs)
	// Always report the CURRENT revision, even on timeout: a caller that
	// timed out still needs the revision to expect on its next act.
	obs, oerr := m.Observe(sessionID)
	if err != nil {
		if oerr != nil {
			return false, ObserveData{}, nil // timeout, and the screen could not be read
		}
		return false, obs, nil // timeout: a truthful answer, not an error
	}
	if oerr != nil {
		return settled, ObserveData{}, oerr
	}
	return settled, obs, nil
}
