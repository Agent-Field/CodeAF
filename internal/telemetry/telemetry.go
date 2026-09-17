// Package telemetry implements codeaf's anonymous usage counting exactly as
// .telemetry-contract.md spells it: four typed events, an allowlisted property
// set, a local spool, and one deadline-bounded flush. It is a library only —
// nothing in cmd/codeaf, internal/session or internal/config reads it yet; the
// wiring is a later job.
//
// The law of the package is the contract at the repository root. When this file
// and the contract disagree, the contract wins and this package is the defect.
package telemetry

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/buildinfo"
	"github.com/Agent-Field/codeaf/internal/env"
	"github.com/Agent-Field/codeaf/internal/home"
)

// DefaultEndpoint is the relay the contract names. CODEAF_TELEMETRY_ENDPOINT
// moves it; set to empty it turns telemetry off entirely.
const DefaultEndpoint = "https://agentfield.ai/api/oss/codeaf/telemetry"

// schemaVersion is the wire version the contract fixes. Bumping it is a
// contract change, not a code change.
const schemaVersion = 1

// OptOut enumerates the reasons the ladder can answer with. The empty value
// means telemetry would run.
const (
	OnReason         = ""
	OffEnv           = "CODEAF_TELEMETRY=off"
	OffDoNotTrack    = "DO_NOT_TRACK is set"
	OffConfig        = "config telemetry = off"
	OffEmptyEndpoint = "CODEAF_TELEMETRY_ENDPOINT is empty"
	OffBuild         = "dirty or unstamped build"
	OffTest          = "running under go test"
)

// truthy reads a foreign variable the way the contract means "when truthy":
// present, non-empty, and not one of the negative spellings.
func truthy(name string) bool {
	value := env.Value(name)
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "0", "false", "off":
		return false
	}
	return true
}

// offReason is the whole opt-out ladder in one pure function so tests can
// reach every rung without rebuilding the binary. The rungs are ordered: a
// person's own switch wins over everything, the build's honesty wins over the
// runtime, and a test binary is never a source of truth about usage.
func offReason(configTelemetryOff, underTest, dirtyOrUnstamped bool) string {
	if codeaf := strings.ToLower(strings.TrimSpace(env.Get("CODEAF_TELEMETRY"))); codeaf == "off" || codeaf == "0" || codeaf == "false" {
		return OffEnv
	}
	if value := strings.TrimSpace(env.Value("DO_NOT_TRACK")); value == "1" || strings.EqualFold(value, "true") {
		return OffDoNotTrack
	}
	if configTelemetryOff {
		return OffConfig
	}
	if value, set := env.Lookup("CODEAF_TELEMETRY_ENDPOINT"); set && strings.TrimSpace(value) == "" {
		return OffEmptyEndpoint
	}
	if dirtyOrUnstamped {
		return OffBuild
	}
	if underTest {
		return OffTest
	}
	return OnReason
}

// dirtyOrUnstampedBuild reports whether this binary cannot name its own
// source. A build without a stamped revision or carrying uncommitted edits has
// no stable version to report, so it does not report at all.
func dirtyOrUnstampedBuild() bool {
	revision := buildinfo.Revision()
	if revision == "" {
		return true
	}
	// Identity spells a source with no slashes in it and falls back to
	// "source/dirty/moment" when it has none; a dirty build is visible there
	// without internal/buildinfo growing a second accessor.
	return strings.Contains(buildinfo.Identity(), "/true/")
}

// underGoTest reports whether this process is a test binary, where telemetry
// is always off.
func underGoTest() bool { return testing.Testing() }

// Enabled reports whether telemetry would run, given the config value the
// caller read (true means the config turns it off). Nothing is spooled or
// sent when this answers false.
func Enabled(configTelemetryOff bool) bool {
	return OffReason(configTelemetryOff) == OnReason
}

// OffReason is Enabled with the why, for a status command. It returns "" when
// telemetry would run, otherwise one stable phrase naming the rung that
// switched it off.
func OffReason(configTelemetryOff bool) string {
	return offReason(configTelemetryOff, underGoTest(), dirtyOrUnstampedBuild())
}

// Endpoint names where events go: the contract relay unless the override says
// otherwise.
func Endpoint() string {
	if value := strings.TrimSpace(env.Get("CODEAF_TELEMETRY_ENDPOINT")); value != "" {
		return value
	}
	return DefaultEndpoint
}

// osName buckets runtime.GOOS the way the contract enumerates it.
func osName() string {
	switch runtime.GOOS {
	case "darwin", "linux", "windows":
		return runtime.GOOS
	}
	return "other"
}

// archName buckets runtime.GOARCH the same way.
func archName() string {
	switch runtime.GOARCH {
	case "amd64", "arm64":
		return runtime.GOARCH
	}
	return "other"
}

// ciVariables are the foreign variables that mean this run is on a CI
// machine. They are read through the dynamic env door because none of them is
// a codeaf name.
var ciVariables = []string{"CI", "GITHUB_ACTIONS", "GITLAB_CI", "BUILDKITE", "CIRCLECI", "JENKINS_URL"}

// usageContext buckets where codeaf is running: a CI machine, a container, or
// a person's own machine. CI wins over container when both are true, because
// CI runners are themselves containers and the coarser bucket is the useful
// one.
func usageContext() string {
	for _, name := range ciVariables {
		if truthy(name) {
			return "ci"
		}
	}
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return "container"
	}
	if truthy("KUBERNETES_SERVICE_HOST") {
		return "container"
	}
	return "local"
}

// installMethod reads the one fact the installer writes about how codeaf
// arrived, and answers "unknown" whenever it cannot vouch for a word.
func installMethod() string {
	body, err := os.ReadFile(home.Join("telemetry", "install.json"))
	if err != nil {
		return "unknown"
	}
	var record struct {
		InstallMethod string `json:"install_method"`
	}
	if json.Unmarshal(body, &record) != nil {
		return "unknown"
	}
	switch record.InstallMethod {
	case "script", "source":
		return record.InstallMethod
	}
	return "unknown"
}

// channelFor names the release channel a build revision came from, using the
// same tag shapes cmd/codeaf-release cuts: vX.Y.Z, vX.Y.Z-rc.N,
// dev-YYYYMMDD-hex12 and staging-YYYYMMDD-hex12. Anything else is unknown.
func channelFor(revision string) string {
	switch {
	case strings.Contains(revision, "-rc."):
		if isNumericDotted(strings.TrimPrefix(strings.TrimSuffix(revision, revision[strings.LastIndex(revision, "-rc."):]), "v")) {
			return "rc"
		}
		return "unknown"
	case isStableTag(revision):
		return "stable"
	case isChannelTag(revision, "dev-"):
		return "dev"
	case isChannelTag(revision, "staging-"):
		return "staging"
	}
	return "unknown"
}

// isStableTag matches vX.Y.Z with three numeric parts and nothing else.
func isStableTag(revision string) bool {
	if !strings.HasPrefix(revision, "v") {
		return false
	}
	return isNumericDotted(revision[1:])
}

func isNumericDotted(value string) bool {
	parts := strings.Split(value, ".")
	if len(parts) != 3 {
		return false
	}
	for _, part := range parts {
		if part == "" {
			return false
		}
		for _, r := range part {
			if r < '0' || r > '9' {
				return false
			}
		}
	}
	return true
}

// isChannelTag matches <prefix>YYYYMMDD-<12 lowercase hex chars>, the shape
// every dev and staging tag carries.
func isChannelTag(revision, prefix string) bool {
	rest, ok := strings.CutPrefix(revision, prefix)
	if !ok || len(rest) != 21 || rest[8] != '-' {
		return false
	}
	for _, r := range rest[:8] {
		if r < '0' || r > '9' {
			return false
		}
	}
	_, err := hex.DecodeString(rest[9:])
	return err == nil
}

// commonProps are the properties every event carries. version is capped at 64
// characters because the contract caps it and a truncated tag still sorts.
func commonProps() map[string]any {
	version := buildinfo.Revision()
	if version == "" {
		version = "unknown"
	}
	if len(version) > 64 {
		version = version[:64]
	}
	return map[string]any{
		"codeaf_version": version,
		"channel":        channelFor(version),
		"os":             osName(),
		"arch":           archName(),
		"usage_context":  usageContext(),
		"install_method": installMethod(),
	}
}

// hashHex is sha256 over one string, spelled once because three different
// properties are hashes and none of them may drift into a different recipe.
func hashHex(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

// telemetryDir is the directory under the state root that holds everything
// this package writes. It is reached through internal/home so CODEAF_HOME
// moves it.
func telemetryDir() string { return home.Join("telemetry") }

// ensureDir creates the telemetry directory with its contract mode. The parent
// state root usually exists; MkdirAll only stamps the mode on what it creates.
func ensureDir() error {
	return os.MkdirAll(telemetryDir(), dirMode)
}

// writeFilePrivate writes one file at 0600 after making sure its directory is
// 0700, which is every file this package writes.
func writeFilePrivate(path string, body []byte) error {
	if err := ensureDir(); err != nil {
		return err
	}
	return os.WriteFile(path, body, 0o600)
}

// writeAtomicPrivate rewrites path through a sibling temp file so a crash
// mid-write never leaves a half file where an id or a marker should be.
func writeAtomicPrivate(path string, body []byte) error {
	if err := ensureDir(); err != nil {
		return err
	}
	temp := path + ".tmp"
	if err := os.WriteFile(temp, body, 0o600); err != nil {
		return err
	}
	return os.Rename(temp, path)
}

// fileExists is os.Stat with the answer the callers want.
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// roundTime is the RFC3339 UTC stamp every event carries.
func roundTime(t time.Time) string { return t.UTC().Format(time.RFC3339) }

// ageOf parses an event_time back into an age, answering "ancient" for a stamp
// it cannot read — an unparseable line is never worth sending.
func ageOf(stamp string) time.Duration {
	parsed, err := time.Parse(time.RFC3339, stamp)
	if err != nil {
		return 365 * 24 * time.Hour
	}
	return time.Since(parsed)
}

// dirMode is asserted by tests so a permissions drift fails loudly here rather
// than leaking anything in production.
const (
	dirMode  os.FileMode = 0o700
	fileMode os.FileMode = 0o600
)
