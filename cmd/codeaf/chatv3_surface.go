package main

import (
	"context"
	"log"
	"os"

	"github.com/Agent-Field/codeaf/internal/buildinfo"
	internalenv "github.com/Agent-Field/codeaf/internal/env"
	"github.com/Agent-Field/codeaf/internal/tui3"
	codeupdate "github.com/Agent-Field/codeaf/internal/update"
)

var (
	runSurfaceProgram = func(ctx context.Context, options tui3.Options) error {
		return tui3.Run(ctx, options)
	}
	surfaceUpdateClient      = codeupdate.NewClient
	surfaceExecutable        = codeupdate.ExecutableTarget
	surfaceRunningExecutable = os.Executable
	surfaceRevision          = buildinfo.Revision
	surfaceArguments         = func() []string { return append([]string(nil), os.Args[1:]...) }
)

// ── THE ONE WAY THE v3 SURFACE IS RUN ───────────────────────────────────────
//
// Four doors open the same surface: the in-process door (chatv3.go), the ssh
// door (chatv3_host.go), the relay door (chatv3_at.go), and the unix-socket
// door a plain `codeaf chat` takes when a session host already holds this
// workspace (chatv3_local.go). Everything that is true BECAUSE THE SURFACE OWNS
// THE TERMINAL belongs here rather than at any of them, because a rule stated
// four times is a rule three doors will eventually be missing: the logger
// redirect was written once, at the in-process door, and for as long as it lived
// there the other three left the standard logger on stderr and any line written
// to it tore straight through the alt screen (#404).

// runSurface is the ONLY path in this command to [tui3.Run], and every door
// takes it. It arms the byte meter the surface paints through, parks the
// standard logger in the profile's chat.log for the surface's whole lifetime,
// and hands the terminal back with both undone.
//
// A door that called tui3.Run itself would compile and would look right; what
// it would lack is everything below, which is why [TestOnlyTheSurfaceHelperRunsTheV3Surface]
// reads this package's sources rather than trusting the next door to remember.
func runSurface(ctx context.Context, options tui3.Options) error {
	revision := surfaceRevision()
	executable, executableErr := surfaceRunningExecutable()
	curl := codeupdate.CurlCommand
	if executableErr == nil {
		curl = codeupdate.CurlLine(executable, codeupdate.FollowedChannel(revision))
	}
	client := surfaceUpdateClient(revision, codeupdate.CheckTimeout)
	restart := options.Restart
	if restart == nil {
		restart = &codeupdate.Plan{}
		options.Restart = restart
	}
	options.UpdateRunning = revision
	options.UpdateCurl = curl
	options.UpdateArgs = surfaceArguments()
	options.UpdateCheck = func(check context.Context) (codeupdate.Available, bool) {
		return codeupdate.CheckLaunch(check, codeupdate.CheckOptions{
			Running: revision, ProfileDir: options.ProfileDir, Client: client,
			Disabled:   internalenv.Get(codeupdate.NoUpdateCheckEnv) == "1",
			Executable: executable,
		})
	}
	options.ResolveUpdate = client.Select
	options.InstallUpdate = func(install context.Context, release codeupdate.Release) (codeupdate.InstallResult, error) {
		// Resolution belongs to the off-frame installation command. A launch may
		// live on a slow mount, but its first frame never has to wait for that
		// mount merely because /update exists.
		target, err := surfaceExecutable(func() (string, error) { return executable, executableErr })
		if err != nil {
			return codeupdate.InstallResult{}, err
		}
		return codeupdate.Install(install, codeupdate.InstallOptions{Client: client, Release: release, Target: target, Curl: curl})
	}
	// The byte meter, off unless a developer named a log file (wire.go). It
	// measures what this surface DRAWS and is therefore as local as the terminal
	// is, so it is armed here and never at a door: with CODEAF_WIRE_LOG unset
	// this is a nil writer and a close that does nothing, and tui3.Run leaves
	// the output option alone so Bubble Tea paints straight into os.Stdout.
	wire, closeWire := v3Wire()
	defer closeWire()
	options.Output = wire
	// THE USAGE NOTICE IS THE SURFACE'S TO SHOW when this chat still owes it
	// (telemetry_lifecycle.go), because every door that draws a surface comes
	// through here and a notice printed before the alt screen is a notice
	// nobody reads until they quit.
	telemetryNoticeForSurface(&options)
	return withSurfaceLogger(options.ProfileDir, func() error {
		return runSurfaceProgram(ctx, options)
	})
}

// withSurfaceLogger runs fn with the standard logger parked in the profile's
// chat.log, and puts it back on os.Stderr afterwards.
//
// Anything written to the standard logger while the surface owns the terminal
// tears straight through the frame as a raw row — a recovered fault with its
// stack (internal/guard), a checkpoint warning, a media fallback — spliced into
// whatever the person is typing, and gone with the next repaint. So the logger
// goes to a file beside the profile for the surface's whole lifetime and comes
// back on the way out. It is the SAME file a fault appends its stack to
// (fault.go's [chatLogPath]), so "what happened" has one answer; an empty
// profile is the state root there and never the working directory.
//
// A PROFILE THAT CANNOT TAKE THE FILE KEEPS STDERR — a lost frame is better
// than a lost warning. The surface still runs, and the line lands somewhere a
// person can at least scroll back to.
func withSurfaceLogger(profileDir string, run func() error) error {
	logFile, err := os.OpenFile(chatLogPath(profileDir),
		os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return run()
	}
	// The writer that was there and not os.Stderr as an assumption: a test
	// stands its own writer up around this call, and a door that restored a
	// guess would be quietly undoing somebody else's redirect.
	previous := log.Writer()
	log.SetOutput(logFile)
	defer func() {
		log.SetOutput(previous)
		_ = logFile.Close()
	}()
	return run()
}
