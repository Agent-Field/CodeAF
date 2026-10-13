package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	internalenv "github.com/Agent-Field/codeaf/internal/env"
	codeupdate "github.com/Agent-Field/codeaf/internal/update"
)

// launchRelease is a release host that answers the stable channel with one tag
// and serves its asset, counting every request it is asked.
type launchRelease struct {
	server   *httptest.Server
	requests atomic.Int64
	asset    []byte
	// hold, when set, is how long every request waits before it answers.
	hold time.Duration
	// stall, when set, is closed by the test to release a held asset.
	stall chan struct{}
	// checksum overrides the asset's real digest, to make a bad install.
	checksum string
}

func newLaunchRelease(t *testing.T, latest string) *launchRelease {
	t.Helper()
	release := &launchRelease{asset: []byte("codeaf " + latest + " release bytes")}
	release.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		release.requests.Add(1)
		if release.hold > 0 {
			select {
			case <-time.After(release.hold):
			case <-r.Context().Done():
				return
			}
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "/releases/latest"):
			fmt.Fprintf(w, `{"tag_name":%q}`, latest)
		case strings.HasSuffix(r.URL.Path, "/checksums.txt"):
			sum := sha256.Sum256(release.asset)
			checksum := hex.EncodeToString(sum[:])
			if release.checksum != "" {
				checksum = release.checksum
			}
			fmt.Fprintf(w, "%s  codeaf-%s-%s\n", checksum, runtime.GOOS, runtime.GOARCH)
		case strings.HasSuffix(r.URL.Path, "/codeaf-"+runtime.GOOS+"-"+runtime.GOARCH):
			if release.stall != nil {
				select {
				case <-release.stall:
				case <-r.Context().Done():
					return
				}
			}
			_, _ = w.Write(release.asset)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(release.server.Close)
	return release
}

// launchLab is one launch's whole world: the running build, the file it runs
// from, a profile of its own and what it printed and relaunched.
type launchLab struct {
	out        *bytes.Buffer
	target     string
	profile    string
	relaunched []codeupdate.Plan
	updatedEnv string
}

func newLaunchLab(t *testing.T, running string, release *launchRelease, name string) *launchLab {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	lab := &launchLab{out: &bytes.Buffer{}, target: filepath.Join(dir, name), profile: t.TempDir()}
	if err := os.WriteFile(lab.target, []byte("codeaf "+running+" bytes"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv(config.ProfileDirEnv, lab.profile)
	t.Setenv(updatedFromEnv, "")
	t.Setenv(codeupdate.NoUpdateCheckEnv, "")
	saved := struct {
		revision    func() string
		executable  func() (string, error)
		client      func(string, time.Duration) *codeupdate.Client
		relaunch    func(codeupdate.Plan) error
		arguments   func() []string
		attended    func() bool
		lookup      time.Duration
		installFor  time.Duration
		interrupt   func(context.Context) (context.Context, context.CancelFunc)
		launchFacts launchUpdateFacts
	}{prelaunchRevision, prelaunchExecutable, prelaunchClient, prelaunchRelaunch, prelaunchArguments, prelaunchAttended, prelaunchLookupWait, prelaunchInstallFor, prelaunchInterrupt, launchUpdate}
	savedOut := prelaunchOut
	prelaunchRevision = func() string { return running }
	prelaunchExecutable = func() (string, error) { return lab.target, nil }
	if release != nil {
		prelaunchClient = func(revision string, _ time.Duration) *codeupdate.Client {
			return &codeupdate.Client{HTTP: release.server.Client(), APIBase: release.server.URL, DownloadBase: release.server.URL, Revision: revision}
		}
	}
	prelaunchRelaunch = func(plan codeupdate.Plan) error {
		lab.relaunched = append(lab.relaunched, plan)
		lab.updatedEnv = internalenv.Get(updatedFromEnv)
		return nil
	}
	prelaunchArguments = func() []string { return []string{"chat", "--model", "test/model"} }
	prelaunchAttended = func() bool { return true }
	prelaunchOut = lab.out
	t.Cleanup(func() {
		prelaunchRevision, prelaunchExecutable, prelaunchClient = saved.revision, saved.executable, saved.client
		prelaunchRelaunch, prelaunchArguments, prelaunchAttended = saved.relaunch, saved.arguments, saved.attended
		prelaunchLookupWait, prelaunchInstallFor, prelaunchInterrupt = saved.lookup, saved.installFor, saved.interrupt
		prelaunchOut = savedOut
		launchUpdate = saved.launchFacts
	})
	return lab
}

func (lab *launchLab) onDisk(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(lab.target)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// TestAReleasedCodeafUpdatesAndRestartsBeforeTheChatOpens is the whole road: a
// stable codeaf behind the newest stable installs it, says so, and hands the
// terminal to the new file with the same arguments and the fact of what it
// replaced.
func TestAReleasedCodeafUpdatesAndRestartsBeforeTheChatOpens(t *testing.T) {
	release := newLaunchRelease(t, "v0.7.1")
	lab := newLaunchLab(t, "v0.7.0", release, "codeaf")
	updateBeforeLaunch()
	if got := lab.onDisk(t); got != string(release.asset) {
		t.Fatalf("the file on disk is %q, want the release", got)
	}
	if len(lab.relaunched) != 1 {
		t.Fatalf("relaunched %d times, want once:\n%s", len(lab.relaunched), lab.out)
	}
	plan := lab.relaunched[0]
	if plan.Path != lab.target || strings.Join(plan.Args, " ") != "chat --model test/model" {
		t.Fatalf("relaunch plan = %+v, want %s with the original arguments", plan, lab.target)
	}
	if lab.updatedEnv != "v0.7.0" {
		t.Fatalf("the new build was told it replaced %q, want v0.7.0", lab.updatedEnv)
	}
	for _, want := range []string{
		"updating codeaf v0.7.0 to v0.7.1 · ctrl+c opens v0.7.0 now",
		"codeaf v0.7.1 installed · opening it",
	} {
		if !strings.Contains(lab.out.String(), want) {
			t.Fatalf("the terminal did not say %q:\n%s", want, lab.out)
		}
	}
	if state := codeupdate.LoadAutoState(lab.profile); state.Failures != 0 {
		t.Fatalf("a successful launch install left failures behind: %+v", state)
	}
}

// TestTheBuildALaunchStartedDoesNotLookAgain proves the restart cannot loop:
// the new build reads what it replaced, clears it so no child inherits it, and
// makes no request at all.
func TestTheBuildALaunchStartedDoesNotLookAgain(t *testing.T) {
	release := newLaunchRelease(t, "v0.7.2")
	newLaunchLab(t, "v0.7.1", release, "codeaf")
	t.Setenv(updatedFromEnv, "v0.7.0")
	updateBeforeLaunch()
	if release.requests.Load() != 0 {
		t.Fatalf("the restarted build looked again: %d requests", release.requests.Load())
	}
	if launchUpdate.updatedFrom != "v0.7.0" {
		t.Fatalf("updatedFrom = %q, want v0.7.0", launchUpdate.updatedFrom)
	}
	if value, present := os.LookupEnv(updatedFromEnv); present && value != "" {
		t.Fatalf("%s is still %q for every child to inherit", updatedFromEnv, value)
	}
}

// TestEveryLaunchThatWillNotInstallSaysWhatIsOutInTheTerminal proves the other
// half of the road: auto update off, a file not named codeaf and a pinned file
// each print one line naming the release and the road, and replace nothing.
func TestEveryLaunchThatWillNotInstallSaysWhatIsOutInTheTerminal(t *testing.T) {
	for _, row := range []struct {
		name  string
		file  string
		setup func(t *testing.T, lab *launchLab, release *launchRelease)
		want  string
	}{
		{
			name: "auto update off", file: "codeaf",
			setup: func(t *testing.T, lab *launchLab, _ *launchRelease) {
				if err := config.SaveUpdateAuto(lab.profile, false); err != nil {
					t.Fatal(err)
				}
			},
			want: "codeaf v0.7.1 is out · you have v0.7.0 · codeaf update installs it",
		},
		{
			name: "a file under another name", file: "oldaf",
			want: "codeaf v0.7.1 is out · you have v0.7.0 · oldaf update installs it",
		},
		{
			name: "a pinned file", file: "codeaf",
			setup: func(t *testing.T, lab *launchLab, release *launchRelease) {
				_, err := codeupdate.Install(context.Background(), codeupdate.InstallOptions{
					Client: prelaunchClient("v0.7.0", time.Second), Target: lab.target,
					Release: codeupdate.Release{Tag: "v0.7.0"}, AllowDowngrade: true,
				})
				if err != nil {
					t.Fatal(err)
				}
			},
			want: "codeaf v0.7.1 is out · you have v0.7.0, pinned · codeaf update installs it and unpins",
		},
	} {
		t.Run(row.name, func(t *testing.T) {
			release := newLaunchRelease(t, "v0.7.1")
			lab := newLaunchLab(t, "v0.7.0", release, row.file)
			if row.setup != nil {
				row.setup(t, lab, release)
			}
			before := lab.onDisk(t)
			updateBeforeLaunch()
			if !strings.Contains(lab.out.String(), row.want) {
				t.Fatalf("the terminal said:\n%s\nwant %q", lab.out, row.want)
			}
			if lab.onDisk(t) != before || len(lab.relaunched) != 0 {
				t.Fatalf("a launch that only announces replaced or restarted: relaunched %d", len(lab.relaunched))
			}
		})
	}
}

// TestNoPersonNoLookAndNoReleaseNoLook proves the launch road never runs where
// nobody is waiting on it — a pipe, a script, CI — or for a build from source,
// or in a shell that asked for no request.
func TestNoPersonNoLookAndNoReleaseNoLook(t *testing.T) {
	for _, row := range []struct {
		name    string
		running string
		setup   func(t *testing.T)
	}{
		{name: "nobody at the terminal", running: "v0.7.0", setup: func(*testing.T) { prelaunchAttended = func() bool { return false } }},
		{name: "a source build", running: "a65a9eed"},
		{name: "CODEAF_NO_UPDATE_CHECK", running: "v0.7.0", setup: func(t *testing.T) { t.Setenv(codeupdate.NoUpdateCheckEnv, "1") }},
	} {
		t.Run(row.name, func(t *testing.T) {
			release := newLaunchRelease(t, "v0.7.1")
			lab := newLaunchLab(t, row.running, release, "codeaf")
			if row.setup != nil {
				row.setup(t)
			}
			updateBeforeLaunch()
			if release.requests.Load() != 0 || lab.out.Len() != 0 || len(lab.relaunched) != 0 {
				t.Fatalf("requests %d, said %q, relaunched %d", release.requests.Load(), lab.out, len(lab.relaunched))
			}
		})
	}
}

// TestASilentNetworkCostsOneShortWaitAndThenNone proves the look gives up on
// its own clock, says nothing, and does not make the next launch wait again.
func TestASilentNetworkCostsOneShortWaitAndThenNone(t *testing.T) {
	release := newLaunchRelease(t, "v0.7.1")
	release.hold = 10 * time.Second
	lab := newLaunchLab(t, "v0.7.0", release, "codeaf")
	prelaunchLookupWait = 50 * time.Millisecond
	started := time.Now()
	updateBeforeLaunch()
	if waited := time.Since(started); waited > 2*time.Second {
		t.Fatalf("the launch waited %s on a silent network", waited)
	}
	if lab.out.Len() != 0 || len(lab.relaunched) != 0 {
		t.Fatalf("a silent network said %q and relaunched %d", lab.out, len(lab.relaunched))
	}
	asked := release.requests.Load()
	updateBeforeLaunch()
	if release.requests.Load() != asked {
		t.Fatal("the next launch waited on the same silent network again")
	}
}

// TestAnInstallThatCannotFinishOpensTheBuildOnDisk proves every way the install
// can stop short says one line, replaces nothing, restarts nothing — and that
// only a real failure counts against the release, while ctrl+c is remembered
// as the person's own "not now" for the chat that opens next.
func TestAnInstallThatCannotFinishOpensTheBuildOnDisk(t *testing.T) {
	t.Run("a bad checksum", func(t *testing.T) {
		release := newLaunchRelease(t, "v0.7.1")
		release.checksum = strings.Repeat("0", 64)
		lab := newLaunchLab(t, "v0.7.0", release, "codeaf")
		before := lab.onDisk(t)
		updateBeforeLaunch()
		if lab.onDisk(t) != before || len(lab.relaunched) != 0 {
			t.Fatal("a failed install replaced or restarted")
		}
		if !strings.Contains(lab.out.String(), "codeaf v0.7.1 could not be installed") || !strings.Contains(lab.out.String(), "opening v0.7.0") {
			t.Fatalf("the failure was not said:\n%s", lab.out)
		}
		if state := codeupdate.LoadAutoState(lab.profile); state.FailedRelease != "v0.7.1" || state.Failures != 1 {
			t.Fatalf("the failure was not counted: %+v", state)
		}
		// AND IT BACKS OFF: the next launch inside the backoff does not try.
		lab.out.Reset()
		updateBeforeLaunch()
		if strings.Contains(lab.out.String(), "updating codeaf") {
			t.Fatalf("a launch inside the backoff tried again:\n%s", lab.out)
		}
	})
	t.Run("the install clock", func(t *testing.T) {
		release := newLaunchRelease(t, "v0.7.1")
		release.stall = make(chan struct{})
		defer close(release.stall)
		lab := newLaunchLab(t, "v0.7.0", release, "codeaf")
		prelaunchInstallFor = 100 * time.Millisecond
		updateBeforeLaunch()
		if !strings.Contains(lab.out.String(), "codeaf v0.7.1 is still downloading · opening v0.7.0 now · the chat installs it in the background") {
			t.Fatalf("the clock was not said:\n%s", lab.out)
		}
		if state := codeupdate.LoadAutoState(lab.profile); state.Failures != 0 || len(lab.relaunched) != 0 || launchUpdate.deferred != "" {
			t.Fatalf("a slow download counted as a failure or a stop: %+v, relaunched %d, deferred %q", state, len(lab.relaunched), launchUpdate.deferred)
		}
	})
	t.Run("ctrl+c", func(t *testing.T) {
		release := newLaunchRelease(t, "v0.7.1")
		release.stall = make(chan struct{})
		defer close(release.stall)
		lab := newLaunchLab(t, "v0.7.0", release, "codeaf")
		prelaunchInterrupt = func(parent context.Context) (context.Context, context.CancelFunc) {
			stopped, cancel := context.WithCancel(parent)
			time.AfterFunc(50*time.Millisecond, cancel)
			return stopped, cancel
		}
		updateBeforeLaunch()
		if !strings.Contains(lab.out.String(), "update stopped · opening v0.7.0") {
			t.Fatalf("the stop was not said:\n%s", lab.out)
		}
		if launchUpdate.deferred != "v0.7.1" {
			t.Fatalf("deferred = %q, want the release the person stopped", launchUpdate.deferred)
		}
		if state := codeupdate.LoadAutoState(lab.profile); state.Failures != 0 || len(lab.relaunched) != 0 {
			t.Fatalf("ctrl+c counted as a failure: %+v, relaunched %d", state, len(lab.relaunched))
		}
	})
}
