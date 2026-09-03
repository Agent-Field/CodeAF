package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/calllog"
	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/lease"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/watchdog"
)

type standingWatchStatus interface {
	Status() (watchdog.Status, error)
}

type doctorSnapshot struct {
	BrainPath        string
	BrainSize        int64
	BrainExists      bool
	Resident         string
	Watch            watchdog.Status
	Spend            float64
	Rail             float64
	RailUnlimited    bool
	ActiveCharters   int
	PendingQuestions int
	// CallLog is where the model-call log is and how big it has got
	// (internal/calllog), or nothing when nothing has ever been written to it.
	CallLog callLogReport
	Now     time.Time
}

// callLogReport is where the model-call log is and how big it has got, or
// nothing at all. Nothing is the honest answer on a machine that has not called
// a model yet: a path printed beside "0 B" for a file that does not exist reads
// as a broken log rather than an unused one (the emptiness law).
type callLogReport struct {
	Path string
	Size int64
	// Off is a log the operator switched off, which is a different report from
	// one that simply has not been written to.
	Off bool
}

func runDoctor(args []string) error {
	profileDir := strings.TrimSpace(os.Getenv("AFORGE_PROFILE_DIR"))
	dailyBudget, err := config.DailyBudgetUSDAt(profileDir)
	if err != nil {
		return err
	}
	return runDoctorWith(args, os.Stdout, dailyBudget, nil)
}

// runDoctorWith is doctor with its one outside reading injectable: the standing
// watch.
func runDoctorWith(args []string, output io.Writer, dailyBudget float64, override standingWatchStatus) error {
	flags := commandFlags("doctor")
	database := flags.String("db", defaultChatDB(), "path to the durable graph database")
	if err := parseCommandFlags(flags, reorder(flags, args)); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("usage: aforge doctor [--db path]")
	}
	path, err := expandHome(strings.TrimSpace(*database))
	if err != nil {
		return err
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("resolve brain file: %w", err)
	}

	var graph *store.Store
	if info, statErr := os.Stat(path); statErr == nil {
		if !info.Mode().IsRegular() {
			return fmt.Errorf("open brain file: %s is not a regular file", path)
		}
		graph, err = store.Open(path)
		if err != nil {
			return err
		}
		defer graph.Close()
	} else if !os.IsNotExist(statErr) {
		return fmt.Errorf("inspect brain file: %w", statErr)
	}

	watch := override
	if watch == nil {
		manager, managerErr := newStandingWatchManager(graph)
		if managerErr != nil {
			return managerErr
		}
		watch = manager
	}
	snapshot, err := collectDoctorSnapshot(path, graph, watch, dailyBudget, time.Now())
	if err != nil {
		return err
	}
	// The model-call log lives beside the quirks memo under the profile, which
	// `--db` does not move: it is read from the same environment runDoctor read
	// the budget from.
	snapshot.CallLog = readCallLogReport(calllog.PathFor(strings.TrimSpace(os.Getenv("AFORGE_PROFILE_DIR"))))
	_, err = io.WriteString(output, formatDoctor(snapshot))
	return err
}

// readCallLogReport measures the log without opening it for writing. A path
// that is not there yet is not an error and not a zero — it is a machine that
// has not made a call.
func readCallLogReport(path string) callLogReport {
	if path == "" {
		return callLogReport{Off: true}
	}
	info, err := os.Stat(path)
	if err != nil {
		return callLogReport{}
	}
	return callLogReport{Path: path, Size: info.Size()}
}

func newStandingWatchManager(graph *store.Store) (*watchdog.Manager, error) {
	var lastWake watchdog.LastWakeFunc
	if graph != nil {
		lastWake = graph.LastStandingWake
	}
	return watchdog.New(watchdog.Options{LastWake: lastWake})
}

// collectDoctorSnapshot reads everything doctor reports.
func collectDoctorSnapshot(path string, graph *store.Store, watch standingWatchStatus, dailyBudget float64,
	now time.Time) (doctorSnapshot, error) {
	snapshot := doctorSnapshot{
		BrainPath: path, Resident: readResident(residentLockFor(path)),
		Rail: dailyBudget, RailUnlimited: dailyBudget <= 0, Now: now,
	}
	if info, err := os.Stat(path); err == nil {
		snapshot.BrainExists, snapshot.BrainSize = true, info.Size()
	} else if !os.IsNotExist(err) {
		return doctorSnapshot{}, fmt.Errorf("inspect brain file: %w", err)
	}
	if watch != nil {
		status, err := watch.Status()
		if err != nil {
			return doctorSnapshot{}, fmt.Errorf("standing watch status is unavailable")
		}
		snapshot.Watch = status
	}
	if graph == nil {
		return snapshot, nil
	}
	rail, err := graph.DailyRailToday(dailyBudget)
	if err != nil {
		return doctorSnapshot{}, err
	}
	snapshot.Spend, snapshot.Rail, snapshot.RailUnlimited = rail.Spend, rail.Ceiling, rail.Unlimited
	charters, err := graph.ActiveCharters()
	if err != nil {
		return doctorSnapshot{}, err
	}
	snapshot.ActiveCharters = len(charters)
	questions, err := graph.UnresolvedQuestions(10000)
	if err != nil {
		return doctorSnapshot{}, err
	}
	snapshot.PendingQuestions = len(questions)
	return snapshot, nil
}

func formatDoctor(snapshot doctorSnapshot) string {
	brain := snapshot.BrainPath + " · not created"
	if snapshot.BrainExists {
		brain = snapshot.BrainPath + " · " + humanBytes(snapshot.BrainSize)
	}
	watch := "not installed"
	if snapshot.Watch.Installed {
		watch = "installed"
	}
	if snapshot.Watch.LastWake.IsZero() {
		watch += " · last wake not yet"
	} else {
		watch += " · last wake " + relativePast(snapshot.Watch.LastWake, snapshot.Now)
	}
	if snapshot.Watch.Installed && !snapshot.Watch.NextDue.IsZero() {
		watch += " · next check " + relativeFuture(snapshot.Watch.NextDue, snapshot.Now)
	}
	// An arranged watch whose checks stopped landing is the one failure the
	// user cannot see from the outside, so doctor says it rather than reading
	// healthy while nothing has woken for several cadences.
	if snapshot.Watch.Installed && !snapshot.Watch.LastWake.IsZero() &&
		snapshot.Now.Sub(snapshot.Watch.LastWake) > 3*watchdog.Interval {
		watch += " · checks look stalled"
	}
	// THE EMPTINESS LAW ON THE ONE PAGE PEOPLE OPEN WHEN NOTHING WORKS. A
	// machine that has not spent anything today has not measured zero — it has
	// not measured — and `$0.00 today` beside a rail reads as a machine that
	// counted. The rail itself is a figure somebody chose, so it stays.
	rail := fmt.Sprintf("rail $%.2f", snapshot.Rail)
	if snapshot.RailUnlimited {
		rail = "rail unlimited"
	}
	spend := rail
	if today := config.SpentFigure(snapshot.Spend); today != "" {
		spend = today + " today · " + rail
	}
	// The same law on the counts beside it: no charters and no questions is
	// nothing to say, not two zeros.
	var standingParts []string
	if snapshot.ActiveCharters > 0 {
		standingParts = append(standingParts, fmt.Sprintf("%d active %s",
			snapshot.ActiveCharters, pluralWord(snapshot.ActiveCharters, "charter")))
	}
	if snapshot.PendingQuestions > 0 {
		standingParts = append(standingParts, fmt.Sprintf("%d pending %s",
			snapshot.PendingQuestions, pluralWord(snapshot.PendingQuestions, "question")))
	}
	block := fmt.Sprintf("%-16s %s\n%-16s %s\n%-16s %s\n%-16s %s\n",
		"brain", brain,
		"resident", snapshot.Resident,
		"standing watch", watch,
		"spend", spend)
	if standing := strings.Join(standingParts, " · "); standing != "" {
		block += fmt.Sprintf("%-16s %s\n", "standing", standing)
	}
	if line := formatCallLog(snapshot.CallLog); line != "" {
		block += fmt.Sprintf("%-16s %s\n", "model calls", line)
	}
	return block
}

// formatCallLog names the model-call log and what it weighs. It says nothing at
// all about a log that has never been written: a person who has not made a call
// has no log to be told about, and a path with no file behind it is the kind of
// line that sends somebody looking for a bug.
func formatCallLog(report callLogReport) string {
	if report.Off {
		return "off · " + calllog.EnvVar + "=" + calllog.OffValue
	}
	if report.Path == "" {
		return ""
	}
	return report.Path + " · " + humanBytes(report.Size)
}

func detailSuffix(detail string) string {
	detail = firstLine(strings.TrimSpace(detail))
	if detail == "" {
		return ""
	}
	return " (" + clip(detail, 120) + ")"
}

// residentLockFor is the lock guarding one store, asked of the package that
// owns the naming. Doctor used to spell the file itself, which is how a report
// keeps naming a lock nobody writes any more the day the key changes.
func residentLockFor(path string) string {
	lock, err := lease.LockPath(path)
	if err != nil {
		return path
	}
	return lock
}

func readResident(path string) string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "this terminal while open"
	}
	text := strings.TrimSpace(string(raw))
	if text == "" {
		return "this terminal while open"
	}
	var fields map[string]any
	if json.Unmarshal(raw, &fields) == nil {
		for _, key := range []string{"resident", "holder", "owner", "identity", "session_id", "surface"} {
			if value := strings.TrimSpace(fmt.Sprint(fields[key])); value != "" && value != "<nil>" {
				if pid := lockPID(fields["pid"]); pid != "" {
					return value + " · pid " + pid
				}
				return value
			}
		}
		if pid := lockPID(fields["pid"]); pid != "" {
			return "aforge · pid " + pid
		}
	}
	if line := firstLine(text); line != "" {
		return clip(line, 120)
	}
	return "this terminal while open"
}

func lockPID(value any) string {
	switch typed := value.(type) {
	case float64:
		if typed > 0 && typed == float64(int64(typed)) {
			return strconv.FormatInt(int64(typed), 10)
		}
	case string:
		if parsed, err := strconv.Atoi(strings.TrimSpace(typed)); err == nil && parsed > 0 {
			return strconv.Itoa(parsed)
		}
	}
	return ""
}

func humanBytes(size int64) string {
	if size < 1024 {
		return fmt.Sprintf("%d B", size)
	}
	units := []string{"KiB", "MiB", "GiB", "TiB"}
	value := float64(size)
	for _, unit := range units {
		value /= 1024
		if value < 1024 || unit == units[len(units)-1] {
			if value >= 10 {
				return fmt.Sprintf("%.0f %s", value, unit)
			}
			return fmt.Sprintf("%.1f %s", value, unit)
		}
	}
	return fmt.Sprintf("%d B", size)
}

func relativePast(then, now time.Time) string {
	if then.After(now) {
		return "just now"
	}
	delta := now.Sub(then)
	switch {
	case delta < time.Minute:
		return "just now"
	case delta < time.Hour:
		return fmt.Sprintf("%dm ago", int(delta/time.Minute))
	case delta < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(delta/time.Hour))
	default:
		return then.Local().Format("2006-01-02 15:04")
	}
}

func relativeFuture(then, now time.Time) string {
	if !then.After(now) {
		return "now"
	}
	delta := then.Sub(now)
	if delta < time.Minute {
		return "in less than a minute"
	}
	return fmt.Sprintf("in %dm", int((delta+time.Minute-1)/time.Minute))
}
