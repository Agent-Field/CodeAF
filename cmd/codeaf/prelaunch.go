package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/buildinfo"
	"github.com/Agent-Field/codeaf/internal/config"
	internalenv "github.com/Agent-Field/codeaf/internal/env"
	codeupdate "github.com/Agent-Field/codeaf/internal/update"
)

// ── THE UPDATE BEFORE THE CHAT OPENS ────────────────────────────────────────
//
// A person who typed `codeaf` on a released build gets the newest stable
// release BEFORE anything is drawn: the launch looks once, installs, and hands
// the terminal to the new build with the same arguments. Everything here is
// bounded so that a launch is never held hostage by the network:
//
//   - The look is free on most launches — the answer is cached for a day — and
//     otherwise waits at most [codeupdate.PrelaunchLookup]. A look that got no
//     answer is not waited on again for an hour.
//   - The install waits at most [prelaunchInstallWait], and ctrl+c ends it at
//     once. Either way the build already on disk opens, and the chat's own
//     background road (internal/tui3's updateoffer.go) is still there.
//   - Anything that goes wrong is one line on stderr, and the chat opens.
//
// WHO IS INSTALLED UNASKED is [codeupdate.AutomaticFor]'s question, and it is
// asked here with the person's half beside it: the `update.auto` row, a pin, a
// release they skipped. A launch that will not install still SAYS what is out,
// on one line in the terminal, before the chat says it again inside.
//
// IT RUNS ONLY WHERE A PERSON IS WAITING ON IT: standard input and output are a
// terminal, `CI` is unset, and the launch is not `--once`. A source build, a
// `CODEAF_NO_UPDATE_CHECK=1` shell or a launch the last one started does not
// look at all.

// updatedFromEnv is how a launch tells the build it just installed what it
// replaced. The new build reads it, clears it so no child inherits it, and
// does not look again — which is also what makes a restart loop impossible.
const updatedFromEnv = "CODEAF_UPDATED_FROM"

// prelaunchInstallWait bounds the download a person watches before the chat
// opens. It is long enough for a release on an ordinary connection and short
// enough that a slow one opens the build on disk rather than holding the
// terminal; the chat's own road finishes the job in the background.
const prelaunchInstallWait = 20 * time.Second

var (
	prelaunchRevision             = buildinfo.Revision
	prelaunchExecutable           = os.Executable
	prelaunchClient               = codeupdate.NewClient
	prelaunchInstall              = codeupdate.Install
	prelaunchRelaunch             = codeupdate.Relaunch
	prelaunchArguments            = func() []string { return append([]string(nil), os.Args[1:]...) }
	prelaunchOut        io.Writer = os.Stderr
	prelaunchNow                  = time.Now
	prelaunchLookupWait           = codeupdate.PrelaunchLookup
	prelaunchInstallFor           = prelaunchInstallWait
	// prelaunchAttended is whether a person is at this terminal: both ends of
	// it are a terminal and no CI runner says otherwise. A pipe, a script or a
	// test harness is nobody to restart in front of.
	prelaunchAttended = func() bool {
		return stdinIsTerminal(os.Stdin) && stdinIsTerminal(os.Stdout) && strings.TrimSpace(internalenv.Value("CI")) == ""
	}
	// prelaunchInterrupt turns ctrl+c into "open what is on disk now" for as
	// long as the install runs, and gives the signal back afterwards.
	prelaunchInterrupt = func(parent context.Context) (context.Context, context.CancelFunc) {
		return signal.NotifyContext(parent, os.Interrupt)
	}
)

// launchUpdate is what this launch's own road leaves for the surface it opens.
// It is package state because four doors open the surface and all of them go
// through [runSurface], which is the one place that reads it.
var launchUpdate launchUpdateFacts

type launchUpdateFacts struct {
	// updatedFrom is the build the previous process replaced, or "".
	updatedFrom string
	// deferred is the release a person stopped with ctrl+c at this launch. The
	// chat names it on its quiet line instead of counting down to it again.
	deferred string
}

// updateBeforeLaunch is the launch's own road. It returns only to let the chat
// open; a successful install never returns, because the new build has taken
// the terminal.
func updateBeforeLaunch() {
	launchUpdate = launchUpdateFacts{}
	// THE BUILD A LAUNCH JUST STARTED DOES NOT LOOK AGAIN. The variable is
	// cleared before anything else, so an engine or a worker this process
	// starts never inherits a fact about a launch it was not part of.
	if from := strings.TrimSpace(internalenv.Get(updatedFromEnv)); from != "" {
		_ = os.Unsetenv(updatedFromEnv)
		launchUpdate.updatedFrom = from
		return
	}
	running := strings.TrimSpace(prelaunchRevision())
	if codeupdate.Kind(running) == "other" || internalenv.Get(codeupdate.NoUpdateCheckEnv) == "1" || !prelaunchAttended() {
		return
	}
	executable, err := prelaunchExecutable()
	if err != nil {
		return
	}
	profileDir := config.ProfileDir()
	client := prelaunchClient(running, codeupdate.CheckTimeout)
	look, cancel := context.WithTimeout(context.Background(), prelaunchLookupWait)
	available, newer := codeupdate.CheckLaunch(look, codeupdate.CheckOptions{
		Running: running, ProfileDir: profileDir, Client: client,
		Executable: executable, Prelaunch: true, Now: prelaunchNow,
	})
	cancel()
	if !newer {
		return
	}
	tag := strings.TrimSpace(available.Latest)
	command := codeupdate.InstallName(executable) + " update"
	curl := codeupdate.CurlLine(executable, codeupdate.FollowedChannel(running))
	headline := "codeaf " + tag + " is out · you have " + running
	// THE ORDER IS THE CHAT'S ORDER (internal/tui3's tookUpdateCheck), so the
	// terminal and the chat never give one launch two different answers.
	switch {
	case codeupdate.InstallRefusal(executable, curl) != "":
		fmt.Fprintln(prelaunchOut, headline+" · "+codeupdate.InstallRefusal(executable, curl))
		return
	case strings.TrimSpace(available.Pinned) != "":
		fmt.Fprintln(prelaunchOut, "codeaf "+tag+" is out · you have "+running+", pinned · "+command+" installs it and unpins")
		return
	case !config.UpdateAutoAt(profileDir) || !codeupdate.AutomaticFor(running, tag, executable):
		fmt.Fprintln(prelaunchOut, headline+" · "+command+" installs it")
		return
	case !codeupdate.ShouldOffer(codeupdate.LoadAutoState(profileDir), tag, prelaunchNow()):
		// A RELEASE SOMEBODY SKIPPED, OR ONE STILL BACKING OFF AFTER A FAILED
		// INSTALL, is not raised here — exactly as the chat does not raise it.
		return
	}
	installBeforeLaunch(client, available, executable, curl, profileDir)
}

// installBeforeLaunch downloads and installs the release a launch found, then
// hands the terminal to it. Every way it can stop short says one line and
// returns, so the build already on disk opens.
func installBeforeLaunch(client *codeupdate.Client, available codeupdate.Available, executable, curl, profileDir string) {
	running, tag := strings.TrimSpace(available.Running), strings.TrimSpace(available.Latest)
	target, err := codeupdate.ExecutableTarget(func() (string, error) { return executable, nil })
	if err != nil {
		fmt.Fprintln(prelaunchOut, "codeaf "+tag+" is out · this codeaf could not be found on disk · opening "+running)
		return
	}
	fmt.Fprintln(prelaunchOut, "updating codeaf "+running+" to "+tag+" · ctrl+c opens "+running+" now")
	interrupted, stopInterrupt := prelaunchInterrupt(context.Background())
	install, cancel := context.WithTimeout(interrupted, prelaunchInstallFor)
	meter := newDownloadMeter(prelaunchOut)
	result, err := prelaunchInstall(install, codeupdate.InstallOptions{
		Client: client, Target: target, Curl: curl, Automatic: true,
		// THE RELEASE THE LOOK NAMED, with the moment it carried, and never a
		// fresh channel resolve: what was announced is what is installed.
		Release:  codeupdate.Release{Tag: tag, PublishedAt: available.LatestPublished},
		Progress: meter.observe,
	})
	timedOut := errors.Is(install.Err(), context.DeadlineExceeded)
	stopped := interrupted.Err() != nil
	cancel()
	stopInterrupt()
	meter.clear()
	if err != nil {
		var refusal *codeupdate.RefusalError
		switch {
		case stopped:
			launchUpdate.deferred = tag
			fmt.Fprintln(prelaunchOut, "update stopped · opening "+running)
		case timedOut:
			fmt.Fprintln(prelaunchOut, "codeaf "+tag+" is still downloading · opening "+running+" now · the chat installs it in the background")
		case errors.As(err, &refusal):
			fmt.Fprintln(prelaunchOut, refusal.Reason)
		default:
			// A REAL FAILURE COUNTS, on the same record the chat keeps, so a
			// release that cannot be installed backs off instead of costing
			// every launch the same wait.
			codeupdate.AutoFailure(profileDir, tag, err)
			fmt.Fprintln(prelaunchOut, "codeaf "+tag+" could not be installed · "+err.Error()+" · opening "+running)
		}
		return
	}
	codeupdate.AutoSuccess(profileDir)
	fmt.Fprintln(prelaunchOut, "codeaf "+tag+" installed · opening it")
	_ = os.Setenv(updatedFromEnv, running)
	if err := prelaunchRelaunch(codeupdate.Plan{Path: result.Path, Args: prelaunchArguments()}); err != nil {
		// The file on disk is the new build; this process is still the old one,
		// and it is better to open it than to leave the person at a prompt.
		_ = os.Unsetenv(updatedFromEnv)
		fmt.Fprintln(prelaunchOut, "could not start codeaf "+tag+": "+err.Error()+" · opening "+running+" · the next launch opens "+tag)
	}
}

// downloadMeter draws the one line that moves while a release arrives. It
// redraws at most ten times a second and only when the shown number changes,
// because a 32 KB read is far finer than anybody can see.
type downloadMeter struct {
	out   io.Writer
	shown string
	last  time.Time
	drew  bool
}

func newDownloadMeter(out io.Writer) *downloadMeter { return &downloadMeter{out: out} }

func (m *downloadMeter) observe(received, total int64) {
	line := "  " + megabytes(received)
	if total > 0 {
		line += " of " + megabytes(total)
	}
	line += " MB"
	now := time.Now()
	if line == m.shown || (m.drew && now.Sub(m.last) < 100*time.Millisecond && received != total) {
		return
	}
	m.shown, m.last, m.drew = line, now, true
	fmt.Fprint(m.out, "\r\x1b[K"+line)
}

// clear takes the moving line away so whatever is said next starts clean.
func (m *downloadMeter) clear() {
	if m.drew {
		fmt.Fprint(m.out, "\r\x1b[K")
	}
}

func megabytes(bytes int64) string {
	return fmt.Sprintf("%d", (bytes+500_000)/1_000_000)
}
