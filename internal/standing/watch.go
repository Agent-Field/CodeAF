package standing

// watch.go keeps the standing items current when no aforge window is open. It
// is one launchd agent or one systemd user timer running `aforge tick` every
// [Interval], and it is the entire footprint this design has on the host: no
// server, no port, no account, no configuration file.
//
// IT IS THE SHAPE OF internal/watchdog AND NOT ITS CODE. v1's resident owns its
// own units and its own `aforge wake`, and the two must be able to sit on one
// machine without either one's install stepping on the other's. So the approach
// is copied deliberately — injectable platform, home, executable and runner;
// definitions written temp+rename; status derived from bytes on disk — and the
// names are v3's own.
//
// STATUS IS DERIVED, NEVER ASSERTED. Installed means the definition file still
// matches, byte for byte, what this build would write. Nothing shells out to
// ask, because the answer to "is my watch running" must not depend on a command
// that might not be on the PATH.

import (
	"context"
	"errors"
	"fmt"
	"html"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// DarwinTickLabel and LinuxTickTimer are what this machine's own scheduler
// calls the timer.
//
// THEY ARE EXPORTED BECAUSE THE SETTINGS ROW SAYS THEM OUT LOUD. "aforge
// installs a launchd agent" is a sentence nobody can check; the label is what
// `launchctl list` and `systemctl --user list-timers` answer to, and a person
// deciding whether to leave background checks on is entitled to the name they
// would have to type to go and look.
const (
	DarwinTickLabel = "ai.agentfield.aforge.tick"
	LinuxTickTimer  = "aforge-tick.timer"
)

const (
	darwinTickPlist  = DarwinTickLabel + ".plist"
	linuxTickService = "aforge-tick.service"
	tickUnitTitle    = "Keep aforge's standing items current"
)

// IntervalWords is [Interval] the way a person says it — `5 minutes`.
//
// ONE SOURCE OF TRUTH FOR THE CADENCE. Three sentences a person reads name it —
// the line the conversation says the first time something stands, the settings
// row's hint, and the manual — and a figure typed into any of them is a figure
// that will disagree with the timer the day this constant moves.
func IntervalWords() string {
	switch {
	case Interval >= time.Hour:
		return plainCount(int(Interval/time.Hour), "hour")
	case Interval >= time.Minute:
		return plainCount(int(Interval/time.Minute), "minute")
	default:
		return plainCount(int(Interval/time.Second), "second")
	}
}

func plainCount(count int, noun string) string {
	if count == 1 {
		return "1 " + noun
	}
	return strconv.Itoa(count) + " " + noun + "s"
}

// WatchRunner is the whole process boundary the timer needs. Tests hand it a
// recorder; only execRunner reaches os/exec.
type WatchRunner interface {
	Run(ctx context.Context, name string, args ...string) error
}

// WatchOptions makes every host-specific input injectable, so a test can
// install a timer for a machine it is not running on. Empty values are filled
// from the current process by [NewWatch].
type WatchOptions struct {
	Platform   string
	HomeDir    string
	Executable string
	UID        int
	Runner     WatchRunner
	// WakeLog is where passes leave their one line each — [Store.WakeLogPath].
	// It is a path rather than a store because the timer has no business
	// reading items, only proof that something woke.
	WakeLog string
	Now     func() time.Time
}

// Timer is the [Watch] this package installs. It holds no state of its own:
// everything it answers is read from the filesystem at the moment it is asked.
type Timer struct {
	platform   string
	homeDir    string
	executable string
	uid        int
	runner     WatchRunner
	wakeLog    string
	now        func() time.Time
}

// NewWatch resolves the defaults and validates the immutable inputs once.
func NewWatch(options WatchOptions) (*Timer, error) {
	if strings.TrimSpace(options.Platform) == "" {
		options.Platform = runtime.GOOS
	}
	if strings.TrimSpace(options.HomeDir) == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("standing: find your home directory: %w", err)
		}
		options.HomeDir = home
	}
	if strings.TrimSpace(options.Executable) == "" {
		executable, err := os.Executable()
		if err != nil {
			return nil, fmt.Errorf("standing: find this program: %w", err)
		}
		options.Executable = executable
	}
	absolute, err := filepath.Abs(options.Executable)
	if err != nil {
		return nil, fmt.Errorf("standing: find this program: %w", err)
	}
	if options.UID == 0 {
		options.UID = os.Getuid()
	}
	if options.Runner == nil {
		options.Runner = execRunner{}
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	switch options.Platform {
	case "darwin", "linux":
	default:
		return nil, fmt.Errorf("standing: keeping watch is not available on %s", options.Platform)
	}
	return &Timer{
		platform: options.Platform, homeDir: filepath.Clean(options.HomeDir),
		executable: absolute, uid: options.UID, runner: options.Runner,
		wakeLog: options.WakeLog, now: options.Now,
	}, nil
}

// Install writes the exact definition and asks this user's operating system to
// use it now. Installing over an existing one repairs drift and is safe.
func (w *Timer) Install(ctx context.Context) error {
	if w == nil {
		return errors.New("standing: no timer")
	}
	switch w.platform {
	case "darwin":
		return w.installDarwin(ctx)
	case "linux":
		return w.installLinux(ctx)
	}
	return fmt.Errorf("standing: keeping watch is not available on %s", w.platform)
}

// Uninstall stops the timer and removes its definition. A definition that is
// not there is already uninstalled, and says so without touching the host.
func (w *Timer) Uninstall(ctx context.Context) error {
	if w == nil {
		return errors.New("standing: no timer")
	}
	switch w.platform {
	case "darwin":
		return w.uninstallDarwin(ctx)
	case "linux":
		return w.uninstallLinux(ctx)
	}
	return fmt.Errorf("standing: keeping watch is not available on %s", w.platform)
}

// Status derives everything: installation from the definition's own bytes, the
// last wake from the wake log's last line, the next check from the cadence.
func (w *Timer) Status() (WatchStatus, error) {
	if w == nil {
		return WatchStatus{}, errors.New("standing: no timer")
	}
	status := WatchStatus{}
	last, err := lastWake(w.wakeLog)
	if err != nil {
		return WatchStatus{}, err
	}
	status.LastWake = last

	definition := w.primaryPath()
	info, err := os.Stat(definition)
	if err != nil {
		if os.IsNotExist(err) {
			return status, nil
		}
		return WatchStatus{}, fmt.Errorf("standing: look at the timer: %w", err)
	}
	if !info.Mode().IsRegular() {
		return WatchStatus{}, fmt.Errorf("standing: %s is not a regular file", definition)
	}
	primary, err := os.ReadFile(definition)
	if err != nil {
		return WatchStatus{}, fmt.Errorf("standing: read the timer: %w", err)
	}
	want := darwinPlist(w.executable)
	if w.platform == "linux" {
		want = linuxTimerUnit()
	}
	if string(primary) != want {
		return status, nil
	}
	if w.platform == "linux" {
		service, err := os.ReadFile(w.linuxServicePath())
		if err != nil {
			if os.IsNotExist(err) {
				return status, nil
			}
			return WatchStatus{}, fmt.Errorf("standing: read the timer: %w", err)
		}
		if string(service) != linuxServiceUnit(w.executable) {
			return status, nil
		}
	}
	status.Installed = true
	base := status.LastWake
	if base.IsZero() {
		// Nothing has woken yet, so the only honest guess at the next check is
		// one interval after the timer was installed.
		base = info.ModTime()
	}
	status.NextDue = base.Add(Interval)
	return status, nil
}

// WatchDrift is what [Timer.Drift] answers: a definition already on this
// machine, and whether the program it names is still this one.
//
// IT IS A SEPARATE READING FROM [WatchStatus] ON PURPOSE. Status answers the
// only question a person asks — is anything checking — and a definition
// pointing at a binary that has moved is not checking, so Status says no. This
// says WHY it said no, which is a different question with exactly one caller:
// the launch that repairs the drift ([Timer.Install] rewrites it).
type WatchDrift struct {
	// Present is a definition file on disk, whatever it says.
	Present bool
	// Executable is the program that definition names, or empty when the file
	// could not be parsed for one. It is read for the log line the repair
	// writes; nothing decides on it.
	Executable string
	// Stale is Present and the definition is not the one this build would write
	// — the program moved, was deleted, or an older build wrote the file. Every
	// one of those is repaired by installing over it, which is why they are one
	// bool rather than three.
	Stale bool
}

// Drift reads that. A machine with no definition answers a zero value and no
// error: nothing to repair is not a fault.
func (w *Timer) Drift() (WatchDrift, error) {
	if w == nil {
		return WatchDrift{}, errors.New("standing: no timer")
	}
	raw, err := os.ReadFile(w.primaryPath())
	if err != nil {
		if os.IsNotExist(err) {
			return WatchDrift{}, nil
		}
		return WatchDrift{}, fmt.Errorf("standing: read the timer: %w", err)
	}
	drift := WatchDrift{Present: true}
	want := darwinPlist(w.executable)
	if w.platform == "linux" {
		want = linuxTimerUnit()
		// The timer unit names no program at all — the SERVICE beside it does —
		// so on Linux the executable is read from there, and a service that is
		// missing is itself drift.
		service, serviceErr := os.ReadFile(w.linuxServicePath())
		switch {
		case serviceErr == nil:
			drift.Executable = definitionExecutable("linux", string(service))
			if string(service) != linuxServiceUnit(w.executable) {
				drift.Stale = true
			}
		case os.IsNotExist(serviceErr):
			drift.Stale = true
		default:
			return WatchDrift{}, fmt.Errorf("standing: read the timer: %w", serviceErr)
		}
	} else {
		drift.Executable = definitionExecutable(w.platform, string(raw))
	}
	if string(raw) != want {
		drift.Stale = true
	}
	// AND A DEFINITION THAT STILL READS RIGHT CAN STILL POINT AT NOTHING. The
	// bytes match when the program was replaced in place under the same name;
	// they also match when that name was deleted, and a timer firing at a path
	// with no program on it is a check that fails every five minutes in
	// silence. So the program is looked for as well.
	if drift.Executable != "" && !drift.Stale {
		if _, err := os.Stat(drift.Executable); err != nil {
			drift.Stale = true
		}
	}
	return drift, nil
}

// definitionExecutable digs the program out of a definition this package wrote.
// It is a best effort for ONE LOG LINE and never a decision: a file it cannot
// read gives an empty string, and the emptiness law leaves that clause off the
// line rather than printing a placeholder.
func definitionExecutable(platform, content string) string {
	if platform == "linux" {
		for _, line := range strings.Split(content, "\n") {
			rest, found := strings.CutPrefix(strings.TrimSpace(line), "ExecStart=")
			if !found {
				continue
			}
			rest = strings.TrimSuffix(strings.TrimSpace(rest), " tick")
			return unquoteSystemd(rest)
		}
		return ""
	}
	_, after, found := strings.Cut(content, "<key>ProgramArguments</key>")
	if !found {
		return ""
	}
	_, after, found = strings.Cut(after, "<string>")
	if !found {
		return ""
	}
	value, _, found := strings.Cut(after, "</string>")
	if !found {
		return ""
	}
	return html.UnescapeString(value)
}

func unquoteSystemd(value string) string {
	value = strings.TrimPrefix(value, `"`)
	value = strings.TrimSuffix(value, `"`)
	value = strings.ReplaceAll(value, `%%`, `%`)
	value = strings.ReplaceAll(value, `\"`, `"`)
	return strings.ReplaceAll(value, `\\`, `\`)
}

func (w *Timer) installDarwin(ctx context.Context) error {
	path := w.darwinPlistPath()
	_, err := os.Stat(path)
	switch {
	case err == nil:
		if err := w.runner.Run(ctx, "launchctl", "bootout", w.darwinDomain(), path); err != nil {
			_ = w.runner.Run(ctx, "launchctl", "unload", path)
		}
	case !os.IsNotExist(err):
		return fmt.Errorf("standing: look at the timer: %w", err)
	}
	if err := writeDefinition(path, []byte(darwinPlist(w.executable))); err != nil {
		return err
	}
	if err := w.runner.Run(ctx, "launchctl", "bootstrap", w.darwinDomain(), path); err != nil {
		if fallback := w.runner.Run(ctx, "launchctl", "load", path); fallback != nil {
			_ = os.Remove(path)
			return fmt.Errorf("standing: start the timer: %w", errors.Join(err, fallback))
		}
	}
	return nil
}

func (w *Timer) uninstallDarwin(ctx context.Context) error {
	path := w.darwinPlistPath()
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("standing: look at the timer: %w", err)
	}
	stop := w.runner.Run(ctx, "launchctl", "bootout", w.darwinDomain(), path)
	if stop != nil {
		if fallback := w.runner.Run(ctx, "launchctl", "unload", path); fallback != nil {
			stop = errors.Join(stop, fallback)
		} else {
			stop = nil
		}
	}
	remove := os.Remove(path)
	if stop != nil || remove != nil {
		return fmt.Errorf("standing: stop the timer: %w", errors.Join(stop, remove))
	}
	return nil
}

func (w *Timer) installLinux(ctx context.Context) error {
	if err := writeDefinition(w.linuxServicePath(), []byte(linuxServiceUnit(w.executable))); err != nil {
		return err
	}
	if err := writeDefinition(w.linuxTimerPath(), []byte(linuxTimerUnit())); err != nil {
		return err
	}
	if err := w.runner.Run(ctx, "systemctl", "--user", "daemon-reload"); err != nil {
		_ = removeIfPresent(w.linuxTimerPath())
		_ = removeIfPresent(w.linuxServicePath())
		return fmt.Errorf("standing: tell systemd about the timer: %w", err)
	}
	if err := w.runner.Run(ctx, "systemctl", "--user", "enable", "--now", LinuxTickTimer); err != nil {
		_ = removeIfPresent(w.linuxTimerPath())
		_ = removeIfPresent(w.linuxServicePath())
		_ = w.runner.Run(ctx, "systemctl", "--user", "daemon-reload")
		return fmt.Errorf("standing: start the timer: %w", err)
	}
	return nil
}

func (w *Timer) uninstallLinux(ctx context.Context) error {
	timerPath, servicePath := w.linuxTimerPath(), w.linuxServicePath()
	_, timerErr := os.Stat(timerPath)
	_, serviceErr := os.Stat(servicePath)
	if os.IsNotExist(timerErr) && os.IsNotExist(serviceErr) {
		return nil
	}
	if timerErr != nil && !os.IsNotExist(timerErr) {
		return fmt.Errorf("standing: look at the timer: %w", timerErr)
	}
	if serviceErr != nil && !os.IsNotExist(serviceErr) {
		return fmt.Errorf("standing: look at the timer: %w", serviceErr)
	}
	stop := w.runner.Run(ctx, "systemctl", "--user", "disable", "--now", LinuxTickTimer)
	remove := errors.Join(removeIfPresent(timerPath), removeIfPresent(servicePath))
	reload := w.runner.Run(ctx, "systemctl", "--user", "daemon-reload")
	if stop != nil || remove != nil || reload != nil {
		return fmt.Errorf("standing: stop the timer: %w", errors.Join(stop, remove, reload))
	}
	return nil
}

func (w *Timer) primaryPath() string {
	if w.platform == "darwin" {
		return w.darwinPlistPath()
	}
	return w.linuxTimerPath()
}

func (w *Timer) darwinPlistPath() string {
	return filepath.Join(w.homeDir, "Library", "LaunchAgents", darwinTickPlist)
}

func (w *Timer) darwinDomain() string { return "gui/" + strconv.Itoa(w.uid) }

func (w *Timer) linuxUnitDir() string {
	return filepath.Join(w.homeDir, ".config", "systemd", "user")
}

func (w *Timer) linuxTimerPath() string { return filepath.Join(w.linuxUnitDir(), LinuxTickTimer) }

func (w *Timer) linuxServicePath() string { return filepath.Join(w.linuxUnitDir(), linuxTickService) }

// lastWake reads the last line of the wake log. It reads the tail rather than
// the file because the log is one line every five minutes forever, and the only
// thing anybody ever wants from it is the end.
func lastWake(path string) (time.Time, error) {
	if strings.TrimSpace(path) == "" {
		return time.Time{}, nil
	}
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return time.Time{}, nil
		}
		return time.Time{}, fmt.Errorf("standing: read the wake log: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return time.Time{}, fmt.Errorf("standing: read the wake log: %w", err)
	}
	const tail = 4 << 10
	size := info.Size()
	start := size - tail
	if start < 0 {
		start = 0
	}
	buffer := make([]byte, size-start)
	if _, err := file.ReadAt(buffer, start); err != nil && err != io.EOF {
		return time.Time{}, fmt.Errorf("standing: read the wake log: %w", err)
	}
	lines := strings.Split(strings.TrimRight(string(buffer), "\n"), "\n")
	for index := len(lines) - 1; index >= 0; index-- {
		fields := strings.Fields(lines[index])
		if len(fields) == 0 {
			continue
		}
		when, err := time.Parse(time.RFC3339, fields[0])
		if err != nil {
			continue
		}
		return when, nil
	}
	return time.Time{}, nil
}

func writeDefinition(path string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("standing: make room for the timer: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".aforge-tick-*")
	if err != nil {
		return fmt.Errorf("standing: write the timer: %w", err)
	}
	name := temporary.Name()
	defer os.Remove(name)
	if err := temporary.Chmod(0o644); err != nil {
		temporary.Close()
		return fmt.Errorf("standing: write the timer: %w", err)
	}
	if _, err := temporary.Write(content); err != nil {
		temporary.Close()
		return fmt.Errorf("standing: write the timer: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("standing: write the timer: %w", err)
	}
	if err := os.Rename(name, path); err != nil {
		return fmt.Errorf("standing: write the timer: %w", err)
	}
	return nil
}

func removeIfPresent(path string) error {
	err := os.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// darwinPlist and the two systemd units below interpolate [Interval] rather
// than spelling five minutes again: a cadence that appears in two places is a
// cadence that will disagree with itself.
func darwinPlist(executable string) string {
	return `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>` + DarwinTickLabel + `</string>
  <key>ProgramArguments</key>
  <array>
    <string>` + html.EscapeString(executable) + `</string>
    <string>tick</string>
  </array>
  <key>StartInterval</key>
  <integer>` + strconv.Itoa(int(Interval/time.Second)) + `</integer>
  <key>RunAtLoad</key>
  <false/>
</dict>
</plist>
`
}

func linuxServiceUnit(executable string) string {
	return `[Unit]
Description=` + tickUnitTitle + `

[Service]
Type=oneshot
ExecStart=` + quoteSystemd(executable) + ` tick
`
}

// linuxTimerUnit is a calendar timer with Persistent=true on purpose: a
// monotonic timer forgets the checks a closed laptop missed, and the design
// promises that a machine waking up catches up rather than pretending the
// night did not happen.
func linuxTimerUnit() string {
	return `[Unit]
Description=` + tickUnitTitle + `

[Timer]
OnCalendar=*:0/` + strconv.Itoa(int(Interval/time.Minute)) + `
Persistent=true
Unit=` + linuxTickService + `

[Install]
WantedBy=timers.target
`
}

func quoteSystemd(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `"`, `\"`)
	value = strings.ReplaceAll(value, `%`, `%%`)
	return `"` + value + `"`
}

type execRunner struct{}

func (execRunner) Run(ctx context.Context, name string, args ...string) error {
	command := exec.CommandContext(ctx, name, args...)
	output, err := command.CombinedOutput()
	if err == nil {
		return nil
	}
	if detail := strings.TrimSpace(string(output)); detail != "" {
		return fmt.Errorf("%w: %s", err, detail)
	}
	return err
}

// A Timer is a Watch. The contract's interface is what every caller holds, and
// this line is where a signature that drifted would be caught.
var _ Watch = (*Timer)(nil)
