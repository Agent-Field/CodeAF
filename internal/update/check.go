package update

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	internalenv "github.com/Agent-Field/codeaf/internal/env"
)

const cacheLifetime = 24 * time.Hour

// Available is the stable release comparison shown at launch or by --check.
type Available struct {
	Latest  string
	Running string
}

// Newer reports whether the selected stable release supersedes this build.
func (a Available) Newer() bool {
	latest, latestOK := ParseStable(a.Latest)
	switch Kind(a.Running) {
	case "stable":
		running, ok := ParseStable(a.Running)
		return latestOK && ok && latest.Compare(running) > 0
	case "rc":
		running, ok := ParseRC(a.Running)
		return latestOK && ok && latest.Compare(running.Base) >= 0
	default:
		return false
	}
}

// Notice is the one launch line carrying both update roads.
func (a Available) Notice() string {
	if !a.Newer() {
		return ""
	}
	return "codeaf " + a.Latest + " is out · you have " + a.Running + " · /update installs it and restarts · or: " + CurlCommand
}

type checkCache struct {
	CheckedAt time.Time `json:"checked_at"`
	Latest    string    `json:"latest"`
	Running   string    `json:"running"`
}

// CheckOptions supplies the launch policy's disk, clock, and release client.
type CheckOptions struct {
	Running    string
	ProfileDir string
	Client     *Client
	Now        func() time.Time
	Disabled   bool
}

// CheckLaunch silently checks the newest stable release when this build is a
// stable or release-candidate build. Every failure is absence at this surface.
func CheckLaunch(ctx context.Context, options CheckOptions) (Available, bool) {
	running := strings.TrimSpace(options.Running)
	if options.Disabled || internalenv.Get(NoUpdateCheckEnv) == "1" || (Kind(running) != "stable" && Kind(running) != "rc") {
		return Available{}, false
	}
	now := time.Now
	if options.Now != nil {
		now = options.Now
	}
	path := config.ProfilePath(options.ProfileDir, "update-check.json")
	if cached, ok := loadCheckCache(path); ok && cached.Running == running {
		age := now().Sub(cached.CheckedAt)
		if age >= 0 && age < cacheLifetime {
			answer := Available{Latest: cached.Latest, Running: running}
			return answer, answer.Newer()
		}
	}
	if options.Client == nil {
		return Available{}, false
	}
	release, err := options.Client.Select(ctx, Choice{Channel: "stable"})
	if err != nil {
		return Available{}, false
	}
	answer := Available{Latest: release.Tag, Running: running}
	_ = saveCheckCache(path, checkCache{CheckedAt: now(), Latest: release.Tag, Running: running})
	return answer, answer.Newer()
}

func loadCheckCache(path string) (checkCache, bool) {
	if strings.TrimSpace(path) == "" {
		return checkCache{}, false
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return checkCache{}, false
	}
	var cached checkCache
	if json.Unmarshal(raw, &cached) != nil || cached.CheckedAt.IsZero() || cached.Latest == "" || cached.Running == "" {
		return checkCache{}, false
	}
	return cached, true
}

func saveCheckCache(path string, cached checkCache) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	raw, err := json.Marshal(cached)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".update-check-*.json")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	keep := false
	defer func() {
		if !keep {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(append(raw, '\n')); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return err
	}
	keep = true
	return nil
}
