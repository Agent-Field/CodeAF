// Package watchdog keeps standing watches available when no codeaf terminal
// is open. The user-facing surfaces describe the consequence; this package
// owns the deliberately quiet operating-system details.
package watchdog

import (
	"context"
	"errors"
	"fmt"
	"html"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	// Interval is the cadence promised by the standing-watch ratification.
	Interval = 5 * time.Minute

	darwinLabel            = "ai.agentfield.codeaf.wake"
	darwinPlistName        = darwinLabel + ".plist"
	linuxTimerName         = "codeaf-wake.timer"
	linuxServiceName       = "codeaf-wake.service"
	standingUnitTitle      = "Keep codeaf standing watches current"
	legacyDarwinLabel      = "ai.agentfield.aforge.wake"            // legacy-name
	legacyLinuxTimerName   = "aforge-wake.timer"                    // legacy-name
	legacyLinuxServiceName = "aforge-wake.service"                  // legacy-name
	legacyStandingTitle    = "Keep aforge standing watches current" // legacy-name
)

// Runner is the complete process boundary used to load and unload the user
// timer. Tests provide a recorder; only commandRunner reaches os/exec.
type Runner interface {
	Run(ctx context.Context, name string, args ...string) error
}

// LastWakeFunc reads the newest durable wake observation from the graph
// journal. Keeping this as a callback prevents the OS package from owning or
// opening the user's brain file.
type LastWakeFunc func() (time.Time, bool, error)

// Options makes every host-specific input injectable for exact, hermetic
// tests. Empty values are filled from the current process by New.
type Options struct {
	Platform   string
	HomeDir    string
	Executable string
	UID        int
	Runner     Runner
	LastWake   LastWakeFunc
	Now        func() time.Time
}

// Status is the shared standing-watch view used by doctor and chat grounding.
type Status struct {
	Installed bool
	// Stale means the installed timer is from before the rename and startup
	// should replace it with the current generation.
	Stale    bool
	LastWake time.Time
	NextDue  time.Time
}

// Manager installs, removes, and inspects the current user's five-minute
// timer. It contains no process-local policy state.
type Manager struct {
	platform   string
	homeDir    string
	executable string
	uid        int
	runner     Runner
	lastWake   LastWakeFunc
	now        func() time.Time
}

// New resolves defaults and validates the immutable inputs once.
func New(options Options) (*Manager, error) {
	if strings.TrimSpace(options.Platform) == "" {
		options.Platform = runtime.GOOS
	}
	if strings.TrimSpace(options.HomeDir) == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("standing watch: resolve home: %w", err)
		}
		options.HomeDir = home
	}
	if strings.TrimSpace(options.Executable) == "" {
		executable, err := os.Executable()
		if err != nil {
			return nil, fmt.Errorf("standing watch: resolve executable: %w", err)
		}
		options.Executable = executable
	}
	absolute, err := filepath.Abs(options.Executable)
	if err != nil {
		return nil, fmt.Errorf("standing watch: resolve executable: %w", err)
	}
	if options.UID == 0 {
		options.UID = os.Getuid()
	}
	if options.Runner == nil {
		options.Runner = commandRunner{}
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	switch options.Platform {
	case "darwin", "linux":
	default:
		return nil, fmt.Errorf("standing watch is not available on %s", options.Platform)
	}
	return &Manager{
		platform: options.Platform, homeDir: filepath.Clean(options.HomeDir),
		executable: absolute, uid: options.UID, runner: options.Runner,
		lastWake: options.LastWake, now: options.Now,
	}, nil
}

// Install writes the exact timer definition and asks the current user's OS to
// use it immediately. Reinstalling repairs drift and is safe.
func (m *Manager) Install(ctx context.Context) error {
	if m == nil {
		return errors.New("standing watch: nil manager")
	}
	switch m.platform {
	case "darwin":
		return m.installDarwin(ctx)
	case "linux":
		return m.installLinux(ctx)
	default:
		return fmt.Errorf("standing watch is not available on %s", m.platform)
	}
}

// Uninstall stops the timer and removes its definition. Missing files are
// already uninstalled and therefore succeed without invoking host commands.
func (m *Manager) Uninstall(ctx context.Context) error {
	if m == nil {
		return errors.New("standing watch: nil manager")
	}
	switch m.platform {
	case "darwin":
		return m.uninstallDarwin(ctx)
	case "linux":
		return m.uninstallLinux(ctx)
	default:
		return fmt.Errorf("standing watch is not available on %s", m.platform)
	}
}

// Status derives installation from the definition files and timing from the
// journal plus the timer's fixed cadence. It never shells out.
func (m *Manager) Status() (Status, error) {
	if m == nil {
		return Status{}, errors.New("standing watch: nil manager")
	}
	status := Status{}
	if m.lastWake != nil {
		last, found, wakeErr := m.lastWake()
		if wakeErr != nil {
			return Status{}, fmt.Errorf("standing watch: read last wake: %w", wakeErr)
		}
		if found {
			status.LastWake = last
		}
	}
	definition := m.primaryPath()
	info, err := os.Stat(definition)
	former := false
	if err != nil {
		if os.IsNotExist(err) {
			former = true
			definition = m.legacyPrimaryPath()
			info, err = os.Stat(definition)
			if os.IsNotExist(err) {
				return status, nil
			}
			if err != nil {
				return Status{}, fmt.Errorf("standing watch: inspect definition: %w", err)
			}
		} else {
			return Status{}, fmt.Errorf("standing watch: inspect definition: %w", err)
		}
	}
	if !info.Mode().IsRegular() {
		return Status{}, fmt.Errorf("standing watch: %s is not a regular file", definition)
	}
	primary, err := os.ReadFile(definition)
	if err != nil {
		return Status{}, fmt.Errorf("standing watch: read definition: %w", err)
	}
	wantPrimary := darwinPlist(m.executable)
	if m.platform == "linux" {
		wantPrimary = linuxTimer()
	}
	if former {
		wantPrimary = legacyDarwinPlist(m.executable)
		if m.platform == "linux" {
			wantPrimary = legacyLinuxTimer()
		}
	}
	if string(primary) != wantPrimary {
		return status, nil
	}
	if m.platform == "linux" {
		servicePath := m.linuxServicePath()
		wantService := linuxService(m.executable)
		if former {
			servicePath = m.legacyLinuxServicePath()
			wantService = legacyLinuxService(m.executable)
		}
		service, serviceErr := os.Stat(servicePath)
		if serviceErr != nil {
			if os.IsNotExist(serviceErr) {
				return status, nil
			}
			return Status{}, fmt.Errorf("standing watch: inspect definition: %w", serviceErr)
		}
		if !service.Mode().IsRegular() {
			return Status{}, fmt.Errorf("standing watch: %s is not a regular file", servicePath)
		}
		serviceDefinition, readErr := os.ReadFile(servicePath)
		if readErr != nil {
			return Status{}, fmt.Errorf("standing watch: read definition: %w", readErr)
		}
		if string(serviceDefinition) != wantService {
			return status, nil
		}
	}

	status.Installed = true
	status.Stale = former
	base := info.ModTime()
	if status.LastWake.After(base) {
		base = status.LastWake
	}
	status.NextDue = nextDue(base, m.now())
	return status, nil
}

func (m *Manager) installDarwin(ctx context.Context) error {
	if err := m.removeLegacyDarwin(ctx); err != nil {
		return err
	}
	path := m.darwinPlistPath()
	_, priorErr := os.Stat(path)
	hadDefinition := priorErr == nil
	if priorErr != nil && !os.IsNotExist(priorErr) {
		return fmt.Errorf("standing watch: inspect definition: %w", priorErr)
	}
	if hadDefinition {
		if err := m.runner.Run(ctx, "launchctl", "bootout", m.darwinDomain(), path); err != nil {
			_ = m.runner.Run(ctx, "launchctl", "unload", path)
		}
	}
	if err := writeDefinition(path, []byte(darwinPlist(m.executable))); err != nil {
		return err
	}
	if err := m.runner.Run(ctx, "launchctl", "bootstrap", m.darwinDomain(), path); err != nil {
		if fallbackErr := m.runner.Run(ctx, "launchctl", "load", path); fallbackErr != nil {
			_ = os.Remove(path)
			return fmt.Errorf("standing watch: load timer: %w", errors.Join(err, fallbackErr))
		}
	}
	return nil
}

func (m *Manager) uninstallDarwin(ctx context.Context) error {
	path := m.darwinPlistPath()
	var current error
	if _, err := os.Stat(path); err != nil {
		if !os.IsNotExist(err) {
			current = fmt.Errorf("standing watch: inspect definition: %w", err)
		}
	} else {
		stopErr := m.runner.Run(ctx, "launchctl", "bootout", m.darwinDomain(), path)
		if stopErr != nil {
			if fallbackErr := m.runner.Run(ctx, "launchctl", "unload", path); fallbackErr != nil {
				stopErr = errors.Join(stopErr, fallbackErr)
			} else {
				stopErr = nil
			}
		}
		removeErr := os.Remove(path)
		if stopErr != nil || removeErr != nil {
			current = fmt.Errorf("standing watch: remove timer: %w", errors.Join(stopErr, removeErr))
		}
	}
	return errors.Join(current, m.removeLegacyDarwin(ctx))
}

func (m *Manager) installLinux(ctx context.Context) error {
	if err := m.removeLegacyLinux(ctx); err != nil {
		return err
	}
	if err := writeDefinition(m.linuxServicePath(), []byte(linuxService(m.executable))); err != nil {
		return err
	}
	if err := writeDefinition(m.linuxTimerPath(), []byte(linuxTimer())); err != nil {
		return err
	}
	if err := m.runner.Run(ctx, "systemctl", "--user", "daemon-reload"); err != nil {
		_ = removeIfPresent(m.linuxTimerPath())
		_ = removeIfPresent(m.linuxServicePath())
		return fmt.Errorf("standing watch: refresh timer: %w", err)
	}
	if err := m.runner.Run(ctx, "systemctl", "--user", "enable", "--now", linuxTimerName); err != nil {
		_ = removeIfPresent(m.linuxTimerPath())
		_ = removeIfPresent(m.linuxServicePath())
		_ = m.runner.Run(ctx, "systemctl", "--user", "daemon-reload")
		return fmt.Errorf("standing watch: start timer: %w", err)
	}
	return nil
}

func (m *Manager) uninstallLinux(ctx context.Context) error {
	timerPath, servicePath := m.linuxTimerPath(), m.linuxServicePath()
	_, timerErr := os.Stat(timerPath)
	_, serviceErr := os.Stat(servicePath)
	if timerErr != nil && !os.IsNotExist(timerErr) {
		return fmt.Errorf("standing watch: inspect definition: %w", timerErr)
	}
	if serviceErr != nil && !os.IsNotExist(serviceErr) {
		return fmt.Errorf("standing watch: inspect definition: %w", serviceErr)
	}
	currentPresent := timerErr == nil || serviceErr == nil
	var current error
	if currentPresent {
		stopErr := m.runner.Run(ctx, "systemctl", "--user", "disable", "--now", linuxTimerName)
		removeErr := errors.Join(removeIfPresent(timerPath), removeIfPresent(servicePath))
		if stopErr != nil || removeErr != nil {
			current = fmt.Errorf("standing watch: remove timer: %w", errors.Join(stopErr, removeErr))
		}
	}
	formerPresent := watchdogPathExists(m.legacyLinuxTimerPath()) || watchdogPathExists(m.legacyLinuxServicePath())
	former := m.removeLegacyLinux(ctx)
	var reload error
	if currentPresent || formerPresent {
		if err := m.runner.Run(ctx, "systemctl", "--user", "daemon-reload"); err != nil {
			reload = fmt.Errorf("standing watch: refresh timer: %w", err)
		}
	}
	return errors.Join(current, former, reload)
}

func (m *Manager) primaryPath() string {
	if m.platform == "darwin" {
		return m.darwinPlistPath()
	}
	return m.linuxTimerPath()
}

func (m *Manager) darwinPlistPath() string {
	return filepath.Join(m.homeDir, "Library", "LaunchAgents", darwinPlistName)
}

func (m *Manager) legacyDarwinPlistPath() string {
	return filepath.Join(m.homeDir, "Library", "LaunchAgents", legacyDarwinLabel+".plist")
}

func (m *Manager) legacyPrimaryPath() string {
	if m.platform == "darwin" {
		return m.legacyDarwinPlistPath()
	}
	return m.legacyLinuxTimerPath()
}

func (m *Manager) darwinDomain() string { return "gui/" + strconv.Itoa(m.uid) }

func (m *Manager) linuxUnitDir() string {
	return filepath.Join(m.homeDir, ".config", "systemd", "user")
}

func (m *Manager) linuxTimerPath() string { return filepath.Join(m.linuxUnitDir(), linuxTimerName) }

func (m *Manager) linuxServicePath() string {
	return filepath.Join(m.linuxUnitDir(), linuxServiceName)
}

func (m *Manager) legacyLinuxTimerPath() string {
	return filepath.Join(m.linuxUnitDir(), legacyLinuxTimerName)
}

func (m *Manager) legacyLinuxServicePath() string {
	return filepath.Join(m.linuxUnitDir(), legacyLinuxServiceName)
}

func (m *Manager) removeLegacyDarwin(ctx context.Context) error {
	path := m.legacyDarwinPlistPath()
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("standing watch: inspect former definition: %w", err)
	}
	stop := m.runner.Run(ctx, "launchctl", "bootout", m.darwinDomain(), path)
	if stop != nil {
		stop = m.runner.Run(ctx, "launchctl", "unload", path)
	}
	remove := removeIfPresent(path)
	if stop != nil || remove != nil {
		return fmt.Errorf("standing watch: remove former timer: %w", errors.Join(stop, remove))
	}
	return nil
}

func (m *Manager) removeLegacyLinux(ctx context.Context) error {
	timerPath, servicePath := m.legacyLinuxTimerPath(), m.legacyLinuxServicePath()
	timerPresent, servicePresent := watchdogPathExists(timerPath), watchdogPathExists(servicePath)
	if !timerPresent && !servicePresent {
		return nil
	}
	var stopTimer, stopService error
	if timerPresent {
		stopTimer = m.runner.Run(ctx, "systemctl", "--user", "disable", "--now", legacyLinuxTimerName)
	}
	if servicePresent {
		stopService = m.runner.Run(ctx, "systemctl", "--user", "stop", legacyLinuxServiceName)
	}
	remove := errors.Join(removeIfPresent(timerPath), removeIfPresent(servicePath))
	if stopTimer != nil || stopService != nil || remove != nil {
		return fmt.Errorf("standing watch: remove former timer: %w", errors.Join(stopTimer, stopService, remove))
	}
	return nil
}

func writeDefinition(path string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("standing watch: create definition directory: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".codeaf-wake-*")
	if err != nil {
		return fmt.Errorf("standing watch: create definition: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o644); err != nil {
		temporary.Close()
		return fmt.Errorf("standing watch: set definition permissions: %w", err)
	}
	if _, err := temporary.Write(content); err != nil {
		temporary.Close()
		return fmt.Errorf("standing watch: write definition: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("standing watch: close definition: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("standing watch: install definition: %w", err)
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

func watchdogPathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func nextDue(base, now time.Time) time.Time {
	due := base.Add(Interval)
	if due.After(now) {
		return due
	}
	missed := now.Sub(due)/Interval + 1
	return due.Add(missed * Interval)
}

func darwinPlist(executable string) string {
	return `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>` + darwinLabel + `</string>
  <key>ProgramArguments</key>
  <array>
    <string>` + html.EscapeString(executable) + `</string>
    <string>wake</string>
  </array>
  <key>StartInterval</key>
  <integer>300</integer>
  <key>RunAtLoad</key>
  <false/>
</dict>
</plist>
`
}

func legacyDarwinPlist(executable string) string {
	return strings.ReplaceAll(darwinPlist(executable), darwinLabel, legacyDarwinLabel)
}

func linuxService(executable string) string {
	return `[Unit]
Description=` + standingUnitTitle + `

[Service]
Type=oneshot
ExecStart=` + quoteSystemd(executable) + ` wake
`
}

func legacyLinuxService(executable string) string {
	return strings.ReplaceAll(linuxService(executable), standingUnitTitle, legacyStandingTitle)
}

func linuxTimer() string {
	return `[Unit]
Description=` + standingUnitTitle + `

[Timer]
OnActiveSec=5min
OnUnitActiveSec=5min
Unit=` + linuxServiceName + `

[Install]
WantedBy=timers.target
`
}

func legacyLinuxTimer() string {
	text := strings.ReplaceAll(linuxTimer(), standingUnitTitle, legacyStandingTitle)
	return strings.ReplaceAll(text, linuxServiceName, legacyLinuxServiceName)
}

func quoteSystemd(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `"`, `\"`)
	value = strings.ReplaceAll(value, `%`, `%%`)
	return `"` + value + `"`
}

type commandRunner struct{}

func (commandRunner) Run(ctx context.Context, name string, args ...string) error {
	command := exec.CommandContext(ctx, name, args...)
	output, err := command.CombinedOutput()
	if err == nil {
		return nil
	}
	detail := strings.TrimSpace(string(output))
	if detail == "" {
		return err
	}
	return fmt.Errorf("%w: %s", err, detail)
}
