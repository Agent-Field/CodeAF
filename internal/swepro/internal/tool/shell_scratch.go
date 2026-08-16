package tool

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/processgroup"
)

const (
	shellScratchMarker     = ".codeaf-scratch.json"
	defaultScratchRoot     = "/tmp/codeaf-scratch"
	defaultScratchTTLHours = 24
)

type shellScratchOwner struct {
	PID       int    `json:"pid"`
	Hostname  string `json:"hostname"`
	StartedAt int64  `json:"startedAt"`
}

var shellScratchSweep sync.Once

var shellScratchUsers = struct {
	sync.Mutex
	counts map[string]int
}{counts: map[string]int{}}

func scratchRoot() string {
	if root := os.Getenv("CODEAF_SCRATCH_ROOT"); root != "" {
		return root
	}
	return defaultScratchRoot
}

func scratchDir(sessionID string) string {
	return filepath.Join(scratchRoot(), sessionID)
}

func ensureShellScratch(sessionID string) {
	shellScratchSweep.Do(sweepShellScratch)
	dir := scratchDir(sessionID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	hostname, _ := os.Hostname()
	marker, _ := json.Marshal(shellScratchOwner{
		PID: os.Getpid(), Hostname: hostname, StartedAt: time.Now().UnixMilli(),
	})
	file, err := os.OpenFile(filepath.Join(dir, shellScratchMarker), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return
	}
	_, _ = file.Write(marker)
	_ = file.Close()
}

// AcquireShellScratch registers one active user of a session's private build
// caches. The returned release removes the cache after the last user exits.
func AcquireShellScratch(sessionID string) func() {
	if sessionID == "" {
		return func() {}
	}
	shellScratchUsers.Lock()
	shellScratchUsers.counts[sessionID]++
	shellScratchUsers.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() { TeardownShellScratch(sessionID) })
	}
}

// TeardownShellScratch releases one leaf's private build caches. Shared-cache
// mode never creates these directories, so removal remains a best-effort no-op.
func TeardownShellScratch(sessionID string) {
	if sessionID == "" {
		return
	}
	shellScratchUsers.Lock()
	defer shellScratchUsers.Unlock()
	users := shellScratchUsers.counts[sessionID]
	if users > 1 {
		shellScratchUsers.counts[sessionID] = users - 1
		return
	}
	delete(shellScratchUsers.counts, sessionID)
	_ = os.RemoveAll(scratchDir(sessionID))
}

func sweepShellScratch() {
	entries, err := os.ReadDir(scratchRoot())
	if err != nil {
		return
	}
	hostname, _ := os.Hostname()
	now := time.Now()
	ttl := time.Duration(defaultScratchTTLHours) * time.Hour
	if raw := os.Getenv("CODEAF_SCRATCH_TTL_H"); raw != "" {
		if hours, err := strconv.ParseFloat(raw, 64); err == nil && hours > 0 {
			ttl = time.Duration(hours * float64(time.Hour))
		}
	}
	type keptScratch struct {
		dir     string
		owner   *shellScratchOwner
		started time.Time
	}
	kept := []keptScratch{}
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "ses_") {
			continue
		}
		dir := filepath.Join(scratchRoot(), entry.Name())
		info, err := entry.Info()
		if err != nil {
			continue
		}
		owner := readShellScratchOwner(dir)
		orphan := false
		if owner != nil && owner.Hostname == hostname {
			orphan = !shellScratchPIDAlive(owner.PID)
		} else {
			started := info.ModTime()
			if owner != nil && owner.StartedAt > 0 {
				started = time.UnixMilli(owner.StartedAt)
			}
			orphan = now.Sub(started) > ttl
		}
		if orphan {
			_ = os.RemoveAll(dir)
		} else {
			started := info.ModTime()
			if owner != nil && owner.StartedAt > 0 {
				started = time.UnixMilli(owner.StartedAt)
			}
			kept = append(kept, keptScratch{dir: dir, owner: owner, started: started})
		}
	}
	rawCap := os.Getenv("CODEAF_SCRATCH_MAX_GB")
	capGB, err := strconv.ParseFloat(rawCap, 64)
	if err != nil || capGB <= 0 {
		return
	}
	capBytes := int64(capGB * 1024 * 1024 * 1024)
	sizes := make(map[string]int64, len(kept))
	var total int64
	for _, item := range kept {
		sizes[item.dir] = shellScratchDirSize(item.dir)
		total += sizes[item.dir]
	}
	if total <= capBytes {
		return
	}
	sort.SliceStable(kept, func(i, j int) bool { return kept[i].started.Before(kept[j].started) })
	for _, item := range kept {
		if total <= capBytes {
			break
		}
		if item.owner != nil && item.owner.Hostname == hostname && shellScratchPIDAlive(item.owner.PID) {
			continue
		}
		if os.RemoveAll(item.dir) == nil {
			total -= sizes[item.dir]
		}
	}
}

func shellScratchDirSize(dir string) int64 {
	var total int64
	_ = filepath.WalkDir(dir, func(_ string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return nil
		}
		if info, infoErr := entry.Info(); infoErr == nil {
			total += info.Size()
		}
		return nil
	})
	return total
}

func readShellScratchOwner(dir string) *shellScratchOwner {
	data, err := os.ReadFile(filepath.Join(dir, shellScratchMarker))
	if err != nil {
		return nil
	}
	var owner shellScratchOwner
	if json.Unmarshal(data, &owner) != nil || owner.PID == 0 {
		return nil
	}
	return &owner
}

func shellScratchPIDAlive(pid int) bool {
	if runtime.GOOS == "windows" {
		return true
	}
	return processgroup.ProcessAlive(pid)
}

// shellEnvironment is the environment one shell tool call runs in.
//
// SHARING IS THE DEFAULT AND PRIVACY IS THE OPT-OUT, which is the reverse of
// how this started. Toolchain caches are content-addressed: the same module at
// the same version is the same bytes for every session, so a private copy per
// session buys no isolation at all and costs a full re-download each time. The
// measured price of the old default (audit-notes/headless-regression-audit.md
// §10) was 329MB of one dependency pulled per session, concurrent sessions each
// pulling it again, and a 5GB root filesystem at 100% mid-run — which the
// scheduler's own disk guard then read as a resource pause it could never
// leave. CODEAF_SHARED_BUILD_CACHE=0 restores per-session caches for the case
// that genuinely needs them.
//
// This reading deliberately diverges from internal/config/env.go's boolModes
// entry for the same name, which is a frozen parity port of the upstream
// TypeScript closure and is pinned by fixtures. Nothing on this path consults
// that table — the scratch layer reads the variable itself — and the parity
// artifact is left exactly as imported.
func shellEnvironment(sessionID string) []string {
	environment := append([]string(nil), os.Environ()...)
	if os.Getenv("CODEAF_SHARED_BUILD_CACHE") != "0" || sessionID == "" {
		return environment
	}
	ensureShellScratch(sessionID)
	leaf := scratchDir(sessionID)
	defaults := map[string]string{
		"CARGO_TARGET_DIR": filepath.Join(leaf, "cargo"),
		"GOCACHE":          filepath.Join(leaf, "go-build"),
		"GOMODCACHE":       filepath.Join(leaf, "go-mod"),
		"npm_config_cache": filepath.Join(leaf, "npm"),
		"PIP_CACHE_DIR":    filepath.Join(leaf, "pip"),
	}
	for _, name := range []string{"CARGO_TARGET_DIR", "GOCACHE", "GOMODCACHE", "npm_config_cache", "PIP_CACHE_DIR"} {
		if _, exists := os.LookupEnv(name); !exists {
			environment = append(environment, name+"="+defaults[name])
		}
	}
	return environment
}
