package update

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
)

// The auto updater's durable memory: one small file in the profile holding the
// release a person dismissed, the release that failed and how often, and the
// backoff after that failure. An unreadable file reads as empty state, because
// the worst case of forgetting is one repeated offer.
const (
	// AutoGrace is how long a launch offer waits before the automatic answer.
	// Nothing is blocked while it runs.
	AutoGrace = 10 * time.Second
	// maxAutoFailures stops automatic retries of one release. Manual /update
	// ignores it.
	maxAutoFailures = 3
	autoRetryBase   = 6 * time.Hour
	autoRetryCap    = 72 * time.Hour

	autoStateName = "update-state.json"
)

// AutoState is what the background updater remembers between launches.
type AutoState struct {
	// DismissedRelease is the tag a person said no to, by chord or by command.
	DismissedRelease string    `json:"dismissed_release,omitempty"`
	DismissedAt      time.Time `json:"dismissed_at,omitempty"`
	// FailedRelease, Failures and LastAttemptAt bound automatic retries.
	FailedRelease string    `json:"failed_release,omitempty"`
	Failures      int       `json:"failures,omitempty"`
	LastAttemptAt time.Time `json:"last_attempt_at,omitempty"`
	LastError     string    `json:"last_error,omitempty"`
}

// Dismissed reports whether this exact release was answered "not now".
func (s AutoState) Dismissed(tag string) bool {
	tag = strings.TrimSpace(tag)
	return tag != "" && tag == strings.TrimSpace(s.DismissedRelease)
}

// Blocked reports whether automatic installation of this release has been tried
// too many times already.
func (s AutoState) Blocked(tag string) bool {
	tag = strings.TrimSpace(tag)
	return tag != "" && tag == strings.TrimSpace(s.FailedRelease) && s.Failures >= maxAutoFailures
}

// RetryDue reports whether a failed release may be tried again now.
func (s AutoState) RetryDue(tag string, now time.Time) bool {
	tag = strings.TrimSpace(tag)
	if tag == "" || tag != strings.TrimSpace(s.FailedRelease) || s.Failures == 0 {
		return true
	}
	if s.LastAttemptAt.IsZero() {
		return true
	}
	return !now.Before(s.LastAttemptAt.Add(autoBackoff(s.Failures)))
}

// autoBackoff is the quiet period after a failed automatic install.
func autoBackoff(failures int) time.Duration {
	if failures < 1 {
		return 0
	}
	backoff := autoRetryBase
	for i := 1; i < failures; i++ {
		backoff *= 2
	}
	if backoff > autoRetryCap {
		return autoRetryCap
	}
	return backoff
}

// ShouldOffer reports whether the automatic updater may offer this release now:
// not dismissed, attempts not exhausted, and any backoff passed. A manual door
// ignores all three.
func ShouldOffer(state AutoState, tag string, now time.Time) bool {
	tag = strings.TrimSpace(tag)
	if tag == "" {
		return false
	}
	return !state.Dismissed(tag) && !state.Blocked(tag) && state.RetryDue(tag, now)
}

// AutoStatePath names the coordinator's file in the profile.
func AutoStatePath(profileDir string) string {
	return config.ProfilePath(profileDir, autoStateName)
}

// LoadAutoState reads the coordinator's file; a missing or corrupt one is empty.
func LoadAutoState(profileDir string) AutoState {
	raw, err := os.ReadFile(AutoStatePath(profileDir))
	if err != nil {
		return AutoState{}
	}
	var state AutoState
	if json.Unmarshal(raw, &state) != nil {
		return AutoState{}
	}
	return state
}

// SaveAutoState writes the coordinator's file atomically.
func SaveAutoState(profileDir string, state AutoState) error {
	path := AutoStatePath(profileDir)
	if strings.TrimSpace(path) == "" {
		return nil
	}
	raw, err := json.Marshal(state)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".update-state-*.json")
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

// DismissRelease records "not now" for one exact release.
func DismissRelease(profileDir, tag string) error {
	tag = strings.TrimSpace(tag)
	if tag == "" {
		return errors.New("cannot dismiss an unnamed release")
	}
	state := LoadAutoState(profileDir)
	state.DismissedRelease = tag
	state.DismissedAt = time.Now()
	return SaveAutoState(profileDir, state)
}

// AutoFailure records one failed automatic install of a release.
func AutoFailure(profileDir, tag string, err error) AutoState {
	tag = strings.TrimSpace(tag)
	state := LoadAutoState(profileDir)
	if strings.TrimSpace(state.FailedRelease) != tag {
		state.FailedRelease = tag
		state.Failures = 0
	}
	state.Failures++
	state.LastAttemptAt = time.Now()
	state.LastError = ""
	if err != nil {
		state.LastError = err.Error()
	}
	_ = SaveAutoState(profileDir, state)
	return state
}

// AutoSuccess clears the failure belonging to a release now installed.
func AutoSuccess(profileDir string) AutoState {
	state := LoadAutoState(profileDir)
	state.FailedRelease = ""
	state.Failures = 0
	state.LastAttemptAt = time.Time{}
	state.LastError = ""
	_ = SaveAutoState(profileDir, state)
	return state
}

// ── ONE INSTALLER PER EXECUTABLE ────────────────────────────────────────────
//
// The lock is keyed on the executable target, not on a profile: two profiles
// can share one codeaf file and it is the file that must not be replaced twice
// at once. It is an OS advisory lock, so a process that dies releases it and
// nothing has to be taken over by age.
//
// The lock file also carries a record of the last successful install: the tag,
// the sha256 of the bytes written and when the release was published. The
// sha256 is what makes the record TRUSTWORTHY — it is believed only while the
// file still hashes to it, so a binary replaced by curl, a package manager or a
// person reads as unknown rather than as some older tag.

// ErrInstallInFlight means another process holds the target's lock.
var ErrInstallInFlight = errors.New("another codeaf is already installing an update")

// RefusalError is a typed refusal: this target will not be replaced in place,
// and Reason is the sentence to show. It is not a download or write failure, so
// it is never retried and never counted against a release's automatic attempts.
type RefusalError struct {
	Reason string
}

func (e *RefusalError) Error() string { return e.Reason }

// InstallLock is a held lock on one executable target.
type InstallLock struct {
	target string
	file   *os.File
}

type installRecord struct {
	Tag         string    `json:"tag,omitempty"`
	SHA256      string    `json:"sha256,omitempty"`
	PublishedAt time.Time `json:"published_at,omitempty"`
	InstalledAt time.Time `json:"installed_at,omitempty"`
}

// canonicalTarget resolves a target to the ABSOLUTE, LINK-FREE path whose file
// the installer will actually replace, so that two windows naming one
// executable through different spellings — a relative path, a symlinked
// folder, `..` — hold ONE lock and serialize against each other. It is
// [ExecutableTarget]'s answer, applied again here because the installer also
// runs for a target a caller handed in directly (`codeaf update`, a test).
func canonicalTarget(target string) string {
	target = strings.TrimSpace(target)
	if target == "" {
		return ""
	}
	absolute, err := filepath.Abs(target)
	if err != nil {
		absolute = filepath.Clean(target)
	}
	absolute = filepath.Clean(absolute)
	// THE WHOLE PATH FIRST: an executable that is already there is resolved
	// exactly as [ExecutableTarget] resolves it, which is the file the
	// replacement will land on.
	if resolved, err := filepath.EvalSymlinks(absolute); err == nil {
		return filepath.Clean(resolved)
	}
	// THE DIRECTORY OTHERWISE: a target that does not exist yet cannot be
	// resolved itself, but its folder can, and two aliases of one folder must
	// still point at one lock file.
	dir, base := filepath.Split(absolute)
	if resolved, err := filepath.EvalSymlinks(filepath.Clean(dir)); err == nil {
		return filepath.Join(resolved, base)
	}
	return absolute
}

// installLockPath names the lock-and-record file for a target. It sits beside
// the executable so a target directory that is not writable (which would fail
// the install anyway) cannot be the reason two installers run; the file is left
// in place after release, because unlinking it would let a fresh opener create
// a second inode and hold a second lock.
func installLockPath(target string) string {
	return canonicalTarget(target) + ".install.lock"
}

// TryTargetLock claims the lock for one executable target, returning
// ErrInstallInFlight when another process holds it. The target is canonicalized
// first, so an alias of a file already held answers in flight rather than
// opening a second lock file beside a path nothing else names.
func TryTargetLock(target string) (*InstallLock, error) {
	target = canonicalTarget(target)
	if target == "" {
		return nil, errors.New("the running executable path is empty")
	}
	file, err := os.OpenFile(installLockPath(target), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if !lockFileExclusive(file) {
		_ = file.Close()
		return nil, ErrInstallInFlight
	}
	return &InstallLock{target: target, file: file}, nil
}

// maxInstallRecordBytes bounds the record read: the file holds one small JSON
// object and nothing else, so a corrupted or replaced file is not read whole
// into memory.
const maxInstallRecordBytes = 4 << 10

// Record reads the last install this target recorded, and reports false unless
// the record still matches the file on disk. A tag written by bytes that are no
// longer there is not a fact about the file.
//
// THE BYTES COME THROUGH THE HELD HANDLE, and the read does not move its cursor
// (io.NewSectionReader). A second handle would be a second look-up of a name
// another process may be replacing, and on Windows it would be unreadable to the
// process that locked it: LockFileEx's own documentation says that a process
// which locks a region with one handle — which is what [lockFileExclusive]
// does — cannot access that region through a second handle until it unlocks it
// (learn.microsoft.com/windows/win32/api/fileapi/nf-fileapi-lockfileex). A record
// written by the lock holder would then be the one thing it could not read, so
// the record is read from the handle that holds the lock.
func (l *InstallLock) Record() (installRecord, bool) {
	if l == nil || l.file == nil {
		return installRecord{}, false
	}
	raw, err := io.ReadAll(io.NewSectionReader(l.file, 0, maxInstallRecordBytes))
	if err != nil {
		return installRecord{}, false
	}
	var record installRecord
	if json.Unmarshal(raw, &record) != nil || record.Tag == "" || record.SHA256 == "" {
		return installRecord{}, false
	}
	actual, err := sha256File(l.target)
	if err != nil || !strings.EqualFold(actual, record.SHA256) {
		return installRecord{}, false
	}
	return record, true
}

// RecordInstalled writes what this target now holds. The caller holds the lock
// and has just written those bytes.
func (l *InstallLock) RecordInstalled(tag, digest string, published time.Time) {
	if l == nil || l.file == nil {
		return
	}
	record := installRecord{
		Tag: strings.TrimSpace(tag), SHA256: strings.ToLower(strings.TrimSpace(digest)),
		PublishedAt: published.UTC(), InstalledAt: time.Now().UTC(),
	}
	raw, err := json.Marshal(record)
	if err != nil {
		return
	}
	if _, err := l.file.Seek(0, 0); err != nil {
		return
	}
	if err := l.file.Truncate(0); err != nil {
		return
	}
	if _, err := l.file.Write(append(raw, '\n')); err != nil {
		return
	}
	_ = l.file.Sync()
}

// Release drops the lock. It is safe on a nil lock and safe to call twice.
func (l *InstallLock) Release() {
	if l == nil || l.file == nil {
		return
	}
	unlockFile(l.file)
	_ = l.file.Close()
	l.file = nil
}

func sha256File(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	sum := sha256.New()
	if _, err := io.Copy(sum, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(sum.Sum(nil)), nil
}

// recordSupersedes reports whether the build on disk is newer than the release
// this process resolved. It uses PUBLISHED ordering where the facts exist — the
// same CompareChannelBuilds the launch check uses — and falls back to the tag
// grammar only when it must; two tags it cannot order are not guessed at.
//
// TWO SAME-DAY CHANNEL BUILDS WITH AN UNKNOWN MOMENT ARE ONE OF THE TAGS IT
// CANNOT ORDER. The date inside them is equal and says nothing about which came
// second, so the permissive answer — proceed — would step a file another
// window advanced back to a stale same-day dev or staging build. The build on
// disk therefore wins; a person who means to roll back names the tag, which
// sets [InstallOptions.AllowDowngrade] and never comes through here for an
// answer.
func recordSupersedes(record installRecord, release Release) bool {
	installed, candidate := strings.TrimSpace(record.Tag), strings.TrimSpace(release.Tag)
	if installed == "" || candidate == "" {
		return false
	}
	if installed == candidate {
		return true
	}
	installedChannel, installedDate, installedOK := ParseChannel(installed)
	candidateChannel, candidateDate, candidateOK := ParseChannel(candidate)
	if installedOK && candidateOK && installedChannel == candidateChannel {
		if !record.PublishedAt.IsZero() && !release.PublishedAt.IsZero() {
			comparison, ok := CompareChannelBuilds(installed, record.PublishedAt, candidate, release.PublishedAt)
			return ok && comparison > 0
		}
		if installedDate != candidateDate {
			// THE DATE IS A FACT WHEN THEY DIFFER: the older-dated build is the
			// older one whatever the clock said on the day.
			return installedDate > candidateDate
		}
		// SAME DAY, AND ONE OF THE TWO MOMENTS IS UNKNOWN. There is no fact
		// left to order them with, and guessing would be permissive in exactly
		// the case this refusal exists for ([codeupdate.Choice.Published] is
		// how the pinned road hands the moment over, so a correct install
		// carries it).
		return true
	}
	if comparison, ok := CompareSemverTags(installed, candidate); ok {
		return comparison > 0
	}
	return false
}

// InstallRefusal names why this target may not be replaced in place — a package
// manager owns it, or its directory is not writable — or "" when an ordinary
// install may proceed. The curl line is the road the refusal offers instead.
//
// THE TARGET IS CANONICALIZED FIRST, exactly as [Install] canonicalizes it: a
// launcher that hands in a symlink (or a folder reached through one) must answer
// about the file the replacement would land on, or a managed install answers
// "ordinary" while the installer refuses it a moment later.
func InstallRefusal(target, curl string) string {
	target = canonicalTarget(target)
	if manager := PackageManaged(target); manager != "" {
		return "this codeaf is managed by " + manager + " · update it there, or install a release with: " + curl
	}
	if dir := filepath.Dir(target); dir != "" && !dirWritable(dir) {
		return "this codeaf is in a folder this account cannot write · reinstall it as that owner, or with: " + curl
	}
	return ""
}

// PackageManaged names the package manager that owns a path, or "" when the
// path is an ordinary install this program may replace. A brew, distro or nix
// install must go through its manager, not through an in-place replacement that
// the next upgrade would quietly undo.
func PackageManaged(target string) string {
	path := filepath.ToSlash(filepath.Clean(strings.TrimSpace(target)))
	if path == "" || path == "/" {
		return ""
	}
	prefixes := []struct{ prefix, manager string }{
		{"/opt/homebrew/", "Homebrew"},
		{"/usr/local/Cellar/", "Homebrew"},
		{"/nix/store/", "Nix"},
		{"/usr/bin/", "the system package manager"},
		{"/usr/sbin/", "the system package manager"},
		{"/bin/", "the system package manager"},
		{"/sbin/", "the system package manager"},
		{"/usr/lib/", "the system package manager"},
	}
	for _, row := range prefixes {
		if strings.HasPrefix(path, row.prefix) {
			return row.manager
		}
	}
	return ""
}
