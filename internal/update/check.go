package update

import (
	"context"
	"encoding/json"
	"errors"
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

	// PrelaunchLookup is the longest a launch waits on the release API BEFORE
	// the chat opens, while a person is looking at an empty terminal. It is half
	// the in-chat check's budget ([CheckTimeout]) because that check runs behind
	// a drawn frame and this one runs in front of it. A cached answer costs no
	// wait at all, so this is paid at most once a day on a stable build.
	PrelaunchLookup = 1500 * time.Millisecond
	// prelaunchMissQuiet is how long a launch that got no answer stops waiting
	// for one. A laptop on a network that drops the request silently would
	// otherwise pay [PrelaunchLookup] on every single launch; the chat's own
	// check behind the first frame still runs and still refreshes the cache.
	prelaunchMissQuiet = time.Hour
	prelaunchMissName  = "update-check.miss.json"
)

// Available is the release comparison shown at launch or by --check.
type Available struct {
	Latest           string
	Running          string
	LatestPublished  time.Time
	RunningPublished time.Time
	Curl             string
	// Pinned names the release a person pinned this executable at
	// ([PinnedRelease]), or "". It is read only when a newer release is out,
	// because that is the only time a pin changes what a launch does.
	Pinned string
	// Automatic is [AutomaticFor] asked about the release and the executable
	// the check was given: whether this build and this file are ever installed
	// without anybody asking. It is decided here, beside the request and off
	// any frame, because it resolves the file's links; both launch roads read
	// it rather than asking again.
	Automatic bool
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
	return "codeaf " + a.Latest + " is out · you have " + a.Running + " · /update installs it for the next launch · or: " + curl
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
	// Prelaunch marks the look a launch takes BEFORE the chat opens, with a
	// person waiting on it. After a request that got no answer it makes none for
	// [prelaunchMissQuiet] and answers from the cache alone, and it writes that
	// miss down when its own request gets none. The caller bounds the wait with
	// its context ([PrelaunchLookup]).
	Prelaunch bool
}

// prelaunchMiss is the one fact the miss file holds.
type prelaunchMiss struct {
	MissedAt time.Time `json:"missed_at"`
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
	cached, cachedOK := loadCheckCache(path)
	cachedForRunning := cachedOK && cached.Running == running
	if cachedForRunning {
		age := now().Sub(cached.CheckedAt)
		cachedForRunning = age >= 0 && age < lifetime
	}
	// A DEV OR STAGING ANSWER IS ONLY AS GOOD AS ITS MOMENT. That is the one
	// channel whose comparison orders two BUILDS BY THE MOMENT rather than by
	// the tag, so a cache a pre-PR codeaf wrote — or any answer that arrived
	// without a timestamp — cannot order a same-day pair at all. It is
	// REFRESHED rather than reused whole: an entry with no fact behind it must
	// not decide the offer for the rest of its lifetime. The stable and rc
	// channels order by the tag itself, so a missing moment costs them nothing
	// and they keep their cache untouched.
	needsMoment := (channel == "dev" || channel == "staging") && cached.LatestPublished.IsZero()
	// Every answer leaves through here, so a newer release always arrives with
	// the pin that decides what a launch may do about it.
	finish := func(answer Available) (Available, bool) {
		newer := answer.Newer()
		if newer {
			answer.Pinned = PinnedRelease(options.Executable)
			answer.Automatic = AutomaticFor(answer.Running, answer.Latest, options.Executable)
		}
		return answer, newer
	}
	fromCache := func() (Available, bool) {
		if !cachedForRunning {
			return Available{}, false
		}
		return finish(Available{
			Latest: cached.Latest, Running: running, Curl: curl,
			LatestPublished: cached.LatestPublished, RunningPublished: cached.RunningPublished,
		})
	}
	if cachedForRunning && !needsMoment {
		return fromCache()
	}
	if options.Client == nil {
		return Available{}, false
	}
	// A LAUNCH THAT GOT NO ANSWER AN HOUR AGO DOES NOT WAIT AGAIN. The cache
	// still answers if it can; only the request is skipped.
	missPath := config.ProfilePath(options.ProfileDir, prelaunchMissName)
	if options.Prelaunch && missedRecently(missPath, now()) {
		return fromCache()
	}
	release, err := options.Client.Check(ctx, Choice{Channel: channel, Running: running})
	if err != nil {
		// A PERSON WHO PRESSED CTRL+C IS NOT A NETWORK THAT FAILED, so only a
		// request that ran out of time or was refused is written down.
		if options.Prelaunch && !errors.Is(ctx.Err(), context.Canceled) {
			_ = writeProfileJSON(missPath, ".update-check-miss-*.json", prelaunchMiss{MissedAt: now()})
		}
		// THE REFRESH IS BEST-EFFORT. A release the API cannot answer for must
		// not turn a known offer into no offer at all: a usable cache is still
		// the answer, moment or no moment, and the installer resolves the
		// moment it needs when it writes the record.
		return fromCache()
	}
	if options.Prelaunch {
		_ = os.Remove(missPath)
	}
	_ = saveCheckCache(path, checkCache{
		CheckedAt: now(), Latest: release.Tag, Running: running,
		LatestPublished: release.PublishedAt, RunningPublished: release.RunningPublishedAt,
	})
	return finish(Available{
		Latest: release.Tag, Running: running, Curl: curl,
		LatestPublished: release.PublishedAt, RunningPublished: release.RunningPublishedAt,
	})
}

// missedRecently reports whether a launch's request got no answer within the
// last [prelaunchMissQuiet]. A missing, unreadable or future-dated file is no
// miss, so a broken file can only ever cost one more wait.
func missedRecently(path string, now time.Time) bool {
	raw, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var miss prelaunchMiss
	if json.Unmarshal(raw, &miss) != nil || miss.MissedAt.IsZero() {
		return false
	}
	age := now.Sub(miss.MissedAt)
	return age >= 0 && age < prelaunchMissQuiet
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
	return writeProfileJSON(path, ".update-check-*.json", cached)
}

// writeProfileJSON lands one small JSON file beside config.json with a
// temporary file and a rename, so a reader never sees half of it.
func writeProfileJSON(path, pattern string, value any) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), pattern)
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
