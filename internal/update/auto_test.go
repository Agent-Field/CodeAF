package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestDismissalAnswersOneReleaseAndNotTheNext is the heart of "never nags".
func TestDismissalAnswersOneReleaseAndNotTheNext(t *testing.T) {
	dir := t.TempDir()
	if err := DismissRelease(dir, "v0.2.0"); err != nil {
		t.Fatalf("dismiss: %v", err)
	}
	state := LoadAutoState(dir)
	if !state.Dismissed("v0.2.0") {
		t.Fatal("the dismissed release is not remembered")
	}
	if state.Dismissed("v0.3.0") {
		t.Fatal("a dismissal answered a release it was never about")
	}
	now := time.Now()
	if ShouldOffer(state, "v0.2.0", now) {
		t.Fatal("the same release was offered again")
	}
	if !ShouldOffer(state, "v0.3.0", now) {
		t.Fatal("a newer release was not offered")
	}
}

// TestFailuresThrottleAndThenStop proves the bounded retries.
func TestFailuresThrottleAndThenStop(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	failure := errors.New("the release host is away")

	for attempt := 1; attempt <= maxAutoFailures; attempt++ {
		state := AutoFailure(dir, "v0.2.0", failure)
		if state.Failures != attempt {
			t.Fatalf("attempt %d recorded %d failures", attempt, state.Failures)
		}
	}
	state := LoadAutoState(dir)
	if !state.Blocked("v0.2.0") {
		t.Fatal("the exhausted release is still not blocked")
	}
	if ShouldOffer(state, "v0.2.0", now) {
		t.Fatal("a release that failed out is still offered")
	}
	// A different release starts its own count, and a success clears the whole
	// memory so the next release is tried cleanly.
	if state.Blocked("v0.3.0") {
		t.Fatal("one release's failures blocked another")
	}
	cleared := AutoSuccess(dir)
	if cleared.Failures != 0 || cleared.Blocked("v0.2.0") {
		t.Fatalf("success left %+v", cleared)
	}
}

// TestBackoffGrowsAndCaps proves the quiet period after a failure.
func TestBackoffGrowsAndCaps(t *testing.T) {
	if got := autoBackoff(1); got != autoRetryBase {
		t.Fatalf("first backoff = %s", got)
	}
	if got := autoBackoff(2); got != 2*autoRetryBase {
		t.Fatalf("second backoff = %s", got)
	}
	if got := autoBackoff(12); got != autoRetryCap {
		t.Fatalf("capped backoff = %s", got)
	}
	now := time.Now()
	state := AutoState{FailedRelease: "v0.2.0", Failures: 1, LastAttemptAt: now}
	if state.RetryDue("v0.2.0", now.Add(time.Minute)) {
		t.Fatal("a fresh failure was due again immediately")
	}
	if !state.RetryDue("v0.2.0", now.Add(autoRetryBase+time.Minute)) {
		t.Fatal("a failure was never due again")
	}
	if !state.RetryDue("v0.3.0", now) {
		t.Fatal("an unrelated release inherited a backoff")
	}
}

// TestCorruptStateReadsAsFresh proves a damaged file never breaks a launch.
func TestCorruptStateReadsAsFresh(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(AutoStatePath(dir), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if state := LoadAutoState(dir); state.Dismissed("v0.2.0") || state.Failures != 0 {
		t.Fatalf("corrupt state = %+v", state)
	}
}

// TestInstallLockIsOnePerExecutable proves the cross-terminal, cross-profile
// deduplication, the record of what is on disk, and that a released lock frees
// the target at once. There is no stale-takeover case to test: the OS releases
// the lock when its holder dies.
func TestInstallLockIsOnePerExecutable(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "codeaf")
	first, err := TryTargetLock(target)
	if err != nil {
		t.Fatalf("first lock: %v", err)
	}
	if _, err := TryTargetLock(target); !errors.Is(err, ErrInstallInFlight) {
		t.Fatalf("second lock error = %v, want in flight", err)
	}
	// The record lives inside the lock and is trusted only while the file
	// still hashes to it.
	digest := sha256.Sum256([]byte("installed bytes"))
	hexDigest := hex.EncodeToString(digest[:])
	if err := os.WriteFile(target, []byte("installed bytes"), 0o755); err != nil {
		t.Fatal(err)
	}
	first.RecordInstalled("v0.2.0", hexDigest, time.Now())
	if record, ok := first.Record(); !ok || record.Tag != "v0.2.0" {
		t.Fatalf("record = %+v, ok = %t", record, ok)
	}
	first.Release()
	second, err := TryTargetLock(target)
	if err != nil {
		t.Fatalf("relock: %v", err)
	}
	if record, ok := second.Record(); !ok || record.Tag != "v0.2.0" {
		t.Fatalf("the record did not survive release: %+v, ok = %t", record, ok)
	}
	// AN EXTERNALLY REPLACED FILE INVALIDATES THE RECORD.
	if err := os.WriteFile(target, []byte("someone else's build"), 0o755); err != nil {
		t.Fatal(err)
	}
	if record, ok := second.Record(); ok {
		t.Fatalf("a replaced file still trusted %+v", record)
	}
	second.Release()

	// A different target is a different lock and a different record.
	other := filepath.Join(dir, "devaf")
	otherLock, err := TryTargetLock(other)
	if err != nil {
		t.Fatalf("unrelated target: %v", err)
	}
	if _, ok := otherLock.Record(); ok {
		t.Fatal("a fresh target trusted a record")
	}
	otherLock.Release()
}

// TestRecordSupersedesUsesPublishedOrdering proves the stale-caller guard and
// the same-day dev case M1 asked about.
func TestRecordSupersedesUsesPublishedOrdering(t *testing.T) {
	older := time.Date(2026, 10, 6, 9, 0, 0, 0, time.UTC)
	newer := older.Add(6 * time.Hour)
	for _, row := range []struct {
		name    string
		record  installRecord
		release Release
		want    bool
	}{
		{"stable newer", installRecord{Tag: "v0.3.0"}, Release{Tag: "v0.2.0"}, true},
		{"same tag", installRecord{Tag: "v0.2.0"}, Release{Tag: "v0.2.0"}, true},
		{"stable older", installRecord{Tag: "v0.2.0"}, Release{Tag: "v0.3.0"}, false},
		{"same-day dev, later publish", installRecord{Tag: "dev-20261006-bbbbbbbbbbbb", PublishedAt: newer}, Release{Tag: "dev-20261006-aaaaaaaaaaaa", PublishedAt: older}, true},
		{"same-day dev, earlier publish", installRecord{Tag: "dev-20261006-aaaaaaaaaaaa", PublishedAt: older}, Release{Tag: "dev-20261006-bbbbbbbbbbbb", PublishedAt: newer}, false},
		{"unorderable", installRecord{Tag: "dev-20261006-aaaaaaaaaaaa"}, Release{Tag: "staging-20261007-bbbbbbbbbbbb"}, false},
	} {
		t.Run(row.name, func(t *testing.T) {
			if got := recordSupersedes(row.record, row.release); got != row.want {
				t.Fatalf("recordSupersedes(%+v, %s) = %t", row.record, row.release.Tag, got)
			}
		})
	}
}

// TestPackageManagedNamesTheOwner proves the exclusion list.
func TestPackageManagedNamesTheOwner(t *testing.T) {
	for _, row := range []struct{ path, want string }{
		{"/opt/homebrew/bin/codeaf", "Homebrew"},
		{"/nix/store/abc-codeaf/bin/codeaf", "Nix"},
		{"/usr/bin/codeaf", "the system package manager"},
		{"/home/somebody/.local/bin/codeaf", ""},
		{"/tmp/codeaf", ""},
	} {
		if got := PackageManaged(row.path); got != row.want {
			t.Errorf("PackageManaged(%q) = %q, want %q", row.path, got, row.want)
		}
	}
}

// TestInstallRefusesADuplicateAndAStaleDowngrade proves H4/M1 at the shared
// installer: the record is tied to the bytes on disk, a duplicate is a quiet
// no-op, an automatic stale downgrade is refused, and a NAMED tag may roll back.
func TestInstallRefusesADuplicateAndAStaleDowngrade(t *testing.T) {
	asset := []byte("codeaf v0.9.3 bytes")
	digest := sha256.Sum256(asset)
	server, _ := servedRelease(t, asset, hex.EncodeToString(digest[:]))
	defer server.Close()
	target := filepath.Join(t.TempDir(), "codeaf")

	install := func(tag string, allow bool) (InstallResult, error) {
		return Install(context.Background(), InstallOptions{
			Client: releaseClient(server, tag), Release: Release{Tag: tag, Repository: primaryRepository},
			Target: target, Curl: CurlCommand, AllowDowngrade: allow,
		})
	}
	if _, err := install("v0.9.3", false); err != nil {
		t.Fatalf("first install: %v", err)
	}
	// Same release again: SUCCESS, and nothing to alarm about.
	again, err := install("v0.9.3", false)
	if err != nil || !again.Already {
		t.Fatalf("duplicate install = %+v, %v", again, err)
	}
	// An automatic older candidate is refused...
	if _, err := install("v0.9.2", false); err == nil {
		t.Fatal("an automatic downgrade was allowed")
	} else {
		var refusal *RefusalError
		if !errors.As(err, &refusal) {
			t.Fatalf("downgrade error = %v, want a refusal", err)
		}
	}
	// ...but a person naming the tag is doing a rollback and it proceeds.
	rolled, err := install("v0.9.2", true)
	if err != nil || rolled.Already {
		t.Fatalf("named rollback = %+v, %v", rolled, err)
	}
}

// TestTargetLockAnswersOneTargetThroughEverySpelling proves the canonicalization
// the whole duplicate/downgrade guard rests on: a relative path, an absolute
// path and a path through a symlinked folder are ONE target, so the second
// spelling answers "in flight" rather than opening a second lock file beside a
// path nothing else names.
func TestTargetLockAnswersOneTargetThroughEverySpelling(t *testing.T) {
	root := t.TempDir()
	realDir := filepath.Join(root, "real")
	if err := os.MkdirAll(realDir, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(realDir, "codeaf")
	if err := os.WriteFile(target, []byte("build"), 0o755); err != nil {
		t.Fatal(err)
	}
	held, err := TryTargetLock(target)
	if err != nil {
		t.Fatalf("lock the real path: %v", err)
	}
	defer held.Release()

	spellings := map[string]string{"absolute": target, "dirty": realDir + "/./codeaf"}
	if rel, err := filepath.Rel(mustGetwd(t), target); err == nil && !filepath.IsAbs(rel) {
		spellings["relative"] = rel
	}
	if runtime.GOOS != "windows" {
		alias := filepath.Join(root, "alias")
		if err := os.Symlink(realDir, alias); err != nil {
			t.Fatalf("symlink the folder: %v", err)
		}
		spellings["symlinked folder"] = filepath.Join(alias, "codeaf")
	}
	for name, spelling := range spellings {
		t.Run(name, func(t *testing.T) {
			second, err := TryTargetLock(spelling)
			if !errors.Is(err, ErrInstallInFlight) {
				t.Fatalf("TryTargetLock(%q) = %v, want in flight", spelling, err)
			}
			if second != nil {
				t.Fatal("an alias of a held target handed back a lock")
			}
		})
	}
}

func mustGetwd(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return wd
}

// TestTargetLockSerialisesTwoRealProcesses is the cross-process half: a second
// PROCESS, with its own file descriptors and its own address space, must find
// the lock already held. The child is this test binary run again, so nothing is
// simulated about it (see [TestTargetLockSecondProcessHelper]).
func TestTargetLockSerialisesTwoRealProcesses(t *testing.T) {
	if os.Getenv(targetLockHelperEnv) != "" {
		t.Skip("helper process")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "codeaf")
	if err := os.WriteFile(target, []byte("build"), 0o755); err != nil {
		t.Fatal(err)
	}
	held, err := TryTargetLock(target)
	if err != nil {
		t.Fatalf("hold the target: %v", err)
	}

	child := exec.Command(os.Args[0], "-test.run=^TestTargetLockSecondProcessHelper$", "-test.v")
	child.Env = append(os.Environ(), targetLockHelperEnv+"="+target)
	output, runErr := child.CombinedOutput()
	if runErr != nil {
		t.Fatalf("the child exited %v, want 0 (in flight):\n%s", runErr, output)
	}
	held.Release()

	// AND WITH THE LOCK DROPPED, the same child takes it: the first answer was
	// the other process's lock and not a child that cannot lock at all.
	child = exec.Command(os.Args[0], "-test.run=^TestTargetLockSecondProcessHelper$", "-test.v")
	child.Env = append(os.Environ(), targetLockHelperEnv+"="+target)
	output, runErr = child.CombinedOutput()
	if runErr == nil {
		t.Fatalf("the child took a released lock with no error, so the guard proved nothing:\n%s", output)
	}
}

// targetLockHelperEnv carries one target to the child process above.
const targetLockHelperEnv = "CODEAF_UPDATE_LOCK_HELPER_TARGET"

// TestTargetLockSecondProcessHelper is the child half of the test above. It
// exits 0 when the target is already held by another process, and EXITS
// NON-ZERO when it could take the lock, which the parent reads as "the lock did
// not serialize".
func TestTargetLockSecondProcessHelper(t *testing.T) {
	targetPtr := strings.TrimSpace(os.Getenv(targetLockHelperEnv))
	if targetPtr == "" {
		t.Skip("not the helper")
	}
	lock, err := TryTargetLock(targetPtr)
	switch {
	case errors.Is(err, ErrInstallInFlight):
		os.Exit(0)
	case err != nil:
		fmt.Fprintf(os.Stderr, "helper lock error: %v\n", err)
		os.Exit(1)
	}
	lock.Release()
	fmt.Fprintln(os.Stderr, "helper took the lock")
	os.Exit(2)
}

// TestInstallRefusalFollowsASymlinkedTarget proves the ownership screen answers
// about the file the replacement lands on, not about the alias that named it: a
// launcher holding the running executable as a symlink into a folder this
// account cannot write is refused, where the alias's own (writable) folder would
// have answered "ordinary" and left the installer to refuse it a moment later.
func TestInstallRefusalFollowsASymlinkedTarget(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinked folders need a privilege this test cannot assume")
	}
	if os.Geteuid() == 0 {
		t.Skip("root may write into any folder, so there is no refusal to see")
	}
	root := t.TempDir()
	realDir := filepath.Join(root, "real")
	if err := os.MkdirAll(realDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(realDir, "codeaf"), []byte("build"), 0o755); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(root, "alias")
	if err := os.Symlink(realDir, alias); err != nil {
		t.Fatal(err)
	}
	if !dirWritable(alias) {
		t.Fatal("the alias's own folder is not writable; this test would prove nothing")
	}
	if err := os.Chmod(realDir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(realDir, 0o755) })

	refusal := InstallRefusal(filepath.Join(alias, "codeaf"), "curl -fsSL https://example.invalid/codeaf | bash")
	if refusal == "" || !strings.Contains(refusal, "cannot write") {
		t.Fatalf("a symlinked target in an unwritable folder = %q, want the write refusal", refusal)
	}
}

// TestRecordReadsThroughTheHeldHandle proves the record is read on the handle
// the installer already holds — the property Windows' LockFileEx makes a
// requirement rather than a preference (see [InstallLock.Record]). Reading must not disturb that handle's write
// cursor (a second RecordInstalled after a read must land at offset zero, not
// append), no second handle is opened to a name another process may replace, and
// what the read cannot make sense of reads as NO record rather than as bytes.
func TestRecordReadsThroughTheHeldHandle(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "codeaf")
	if err := os.WriteFile(target, []byte("first bytes"), 0o755); err != nil {
		t.Fatal(err)
	}
	lock, err := TryTargetLock(target)
	if err != nil {
		t.Fatalf("lock: %v", err)
	}
	defer lock.Release()

	digest := func(body string) string {
		sum := sha256.Sum256([]byte(body))
		return hex.EncodeToString(sum[:])
	}
	lock.RecordInstalled("v0.1.0", digest("first bytes"), time.Now())
	if record, ok := lock.Record(); !ok || record.Tag != "v0.1.0" {
		t.Fatalf("the record just written = %+v, ok = %t", record, ok)
	}

	// A SECOND WRITE AFTER A READ IS STILL THE WHOLE FILE.
	if err := os.WriteFile(target, []byte("second bytes"), 0o755); err != nil {
		t.Fatal(err)
	}
	lock.RecordInstalled("v0.2.0", digest("second bytes"), time.Now())
	record, ok := lock.Record()
	if !ok || record.Tag != "v0.2.0" {
		t.Fatalf("the record after a read = %+v, ok = %t", record, ok)
	}

	// A FILE THAT NO LONGER MATCHES IS NOT A FACT ABOUT THE FILE.
	if err := os.WriteFile(target, []byte("somebody else's build"), 0o755); err != nil {
		t.Fatal(err)
	}
	if record, ok := lock.Record(); ok {
		t.Fatalf("a replaced file still trusted %+v", record)
	}

	// NOTHING THE READ CANNOT PARSE IS A RECORD.
	if err := lock.file.Truncate(0); err != nil {
		t.Fatal(err)
	}
	if _, err := lock.file.WriteAt([]byte("{not json"), 0); err != nil {
		t.Fatal(err)
	}
	if record, ok := lock.Record(); ok {
		t.Fatalf("a corrupt record file trusted %+v", record)
	}
	// AND IT IS READ BOUNDED: the object the installer writes sits at offset
	// zero, so bytes beyond the ceiling are not read whole and cannot turn into
	// a trusted record.
	valid := fmt.Sprintf(`{"tag":"v0.9.9","sha256":"%s"}`, digest("somebody else's build"))
	padded := strings.Repeat(" ", maxInstallRecordBytes*2) + valid
	if err := lock.file.Truncate(0); err != nil {
		t.Fatal(err)
	}
	if _, err := lock.file.WriteAt([]byte(padded), 0); err != nil {
		t.Fatal(err)
	}
	if record, ok := lock.Record(); ok {
		t.Fatalf("bytes past the read bound were trusted as %+v", record)
	}
}
