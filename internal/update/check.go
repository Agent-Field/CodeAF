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

const (
	cacheLifetime        = 24 * time.Hour
	channelCacheLifetime = time.Hour
)

// Available is the release comparison shown at launch or by --check.
type Available struct {
	Latest           string
	Running          string
	LatestPublished  time.Time
	RunningPublished time.Time
	Curl             string
}

// Newer reports whether the selected release supersedes this build.
func (a Available) Newer() bool {
	latest, latestOK := ParseStable(a.Latest)
	switch Kind(a.Running) {
	case "stable":
		running, ok := ParseStable(a.Running)
		return latestOK && ok && latest.Compare(running) > 0
	case "rc":
		running, ok := ParseRC(a.Running)
		return latestOK && ok && latest.Compare(running.Base) >= 0
	case "dev", "staging":
		comparison, ok := CompareChannelBuilds(a.Running, a.RunningPublished, a.Latest, a.LatestPublished)
		return ok && comparison < 0
	default:
		return false
	}
}

// Ahead reports whether this build is newer than the release selected from its
// own channel. Source builds and incomparable channels are never called ahead.
func (a Available) Ahead() bool {
	if Kind(a.Running) == "dev" || Kind(a.Running) == "staging" {
		comparison, ok := CompareChannelBuilds(a.Running, a.RunningPublished, a.Latest, a.LatestPublished)
		return ok && comparison > 0
	}
	comparison, ok := CompareSemverTags(a.Running, a.Latest)
	return ok && comparison > 0
}

// Notice is the one launch line carrying both update roads.
func (a Available) Notice() string {
	if !a.Newer() {
		return ""
	}
	curl := a.Curl
	if curl == "" {
		curl = CurlCommand
	}
	return "codeaf " + a.Latest + " is out · you have " + a.Running + " · /update installs it and restarts · or: " + curl
}

type checkCache struct {
	CheckedAt        time.Time `json:"checked_at"`
	Latest           string    `json:"latest"`
	Running          string    `json:"running"`
	LatestPublished  time.Time `json:"latest_published,omitempty"`
	RunningPublished time.Time `json:"running_published,omitempty"`
}

// CheckOptions supplies the launch policy's disk, clock, and release client.
type CheckOptions struct {
	Running    string
	ProfileDir string
	Client     *Client
	Now        func() time.Time
	Disabled   bool
	Executable string
}

// CheckLaunch silently checks the release channel this build follows. Every
// failure is absence at this surface.
func CheckLaunch(ctx context.Context, options CheckOptions) (Available, bool) {
	running := strings.TrimSpace(options.Running)
	kind := Kind(running)
	if options.Disabled || internalenv.Get(NoUpdateCheckEnv) == "1" || kind == "other" {
		return Available{}, false
	}
	// THE LINE UNDER THE NOTICE REINSTALLS WHAT THE NOTICE IS ABOUT. Both the
	// release this asks for and the road it offers come from the one channel
	// this build follows, so a release candidate — which is told about the
	// stable release ahead of it — is handed the stable line, not an rc one.
	channel := FollowedChannel(running)
	cacheName, lifetime := "update-check.json", cacheLifetime
	if channel == "dev" || channel == "staging" {
		cacheName = "update-check." + channel + ".json"
		lifetime = channelCacheLifetime
	}
	curl := CurlLine(options.Executable, channel)
	now := time.Now
	if options.Now != nil {
		now = options.Now
	}
	path := config.ProfilePath(options.ProfileDir, cacheName)
	if cached, ok := loadCheckCache(path); ok && cached.Running == running {
		age := now().Sub(cached.CheckedAt)
		if age >= 0 && age < lifetime {
			answer := Available{
				Latest: cached.Latest, Running: running, Curl: curl,
				LatestPublished: cached.LatestPublished, RunningPublished: cached.RunningPublished,
			}
			return answer, answer.Newer()
		}
	}
	if options.Client == nil {
		return Available{}, false
	}
	release, err := options.Client.Check(ctx, Choice{Channel: channel, Running: running})
	if err != nil {
		return Available{}, false
	}
	answer := Available{
		Latest: release.Tag, Running: running, Curl: curl,
		LatestPublished: release.PublishedAt, RunningPublished: release.RunningPublishedAt,
	}
	_ = saveCheckCache(path, checkCache{
		CheckedAt: now(), Latest: release.Tag, Running: running,
		LatestPublished: release.PublishedAt, RunningPublished: release.RunningPublishedAt,
	})
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
