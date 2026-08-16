package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/catalog"
	"github.com/Agent-Field/aforge-v2/internal/command"
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
	SWEModel         sweModelReport
	Now              time.Time
}

// sweModelReport is doctor's answer to the one precondition of coding work that
// nothing else in the product can see: the swe leaf runs in a second process
// with a second catalog, and a model that catalog has never heard of kills it at
// startup, before a cent is spent and long after the person walked away.
//
// The failure it was written for was live and cost nothing to reproduce: the
// default model of every install, translated into a dated spelling models.dev
// does not publish. That was a config a person could not have known was broken
// until a job died on it.
type sweModelReport struct {
	// Model is the work model as configured, in aforge's spelling.
	Model string
	// Sent is what a swe leaf would actually hand the engine — the same
	// translation the executor makes, made here so doctor cannot be right about
	// a model the run does not use.
	Sent string
	// Resolves is whether the engine's catalog carries Sent.
	Resolves bool
	// Unknown is "nobody could be asked" — the catalog would not load — and is
	// a different report from a model that is genuinely absent.
	Unknown bool
	// Detail explains an Unknown, in the words of whatever failed.
	Detail string
}

func runDoctor(args []string) error {
	profileDir := strings.TrimSpace(os.Getenv("AFORGE_PROFILE_DIR"))
	dailyBudget, err := config.DailyBudgetUSDAt(profileDir)
	if err != nil {
		return err
	}
	return runDoctorWith(args, os.Stdout, dailyBudget, nil, sweModelCheck)
}

// runDoctorWith is doctor with its two outside readings injectable: the standing
// watch, and the coding worker's model probe. A test passing nil for the probe
// gets the block exactly as it was before the probe existed, which is also what
// it must render when nobody asks.
func runDoctorWith(args []string, output io.Writer, dailyBudget float64, override standingWatchStatus,
	sweModel func(profileDir string) sweModelReport) error {
	flags := flag.NewFlagSet("doctor", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	database := flags.String("db", defaultChatDB(), "path to the durable graph database")
	if err := flags.Parse(reorder(args, map[string]bool{"db": true})); err != nil {
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
	snapshot, err := collectDoctorSnapshot(path, graph, watch, dailyBudget, sweModel, time.Now())
	if err != nil {
		return err
	}
	_, err = io.WriteString(output, formatDoctor(snapshot))
	return err
}

// sweModelCheck asks the engine's catalog, from this side of the process
// boundary, whether the configured work model is a model it can find — the same
// question a swe leaf answers at startup, asked before a job is riding on it.
//
// The translation is the executor's own (engineModelID), because doctor
// reporting on a spelling the run does not use would be worse than not
// reporting at all. Everything here is a file read at steady state: models.dev
// is cached by the engine, and the OpenRouter catalog by this process's own
// daily cache.
func sweModelCheck(profileDir string) sweModelReport {
	prefs := loadChatPrefs(profileDir)
	model := firstNonEmptyString(prefs.TaskModel, os.Getenv("AFORGE_MODEL"), config.DefaultModel)
	report := sweModelReport{Model: model, Sent: model}
	resolver, err := sweModelResolver()
	if err != nil || resolver == nil {
		report.Unknown = true
		if err != nil {
			report.Detail = err.Error()
		}
		return report
	}
	ctx, cancel := context.WithTimeout(context.Background(), sweModelProbeTimeout)
	defer cancel()
	// No key: the listing is public, and doctor must work in a profile whose
	// key lives somewhere this function has no business reading.
	models := catalog.Load(ctx, catalog.Options{
		BaseURL: config.DefaultBaseURL, Dir: os.Getenv("AFORGE_PROFILE_DIR"),
	})
	report.Sent = models.Concrete(model, resolver)
	report.Resolves = resolver(report.Sent)
	return report
}

func newStandingWatchManager(graph *store.Store) (*watchdog.Manager, error) {
	var lastWake watchdog.LastWakeFunc
	if graph != nil {
		lastWake = graph.LastStandingWake
	}
	return watchdog.New(watchdog.Options{LastWake: lastWake})
}

// collectDoctorSnapshot reads everything doctor reports. sweModel is the coding
// worker's model probe and may be nil, which is how a caller that only wants the
// store's own state — the head's standing grounding — declines to pay for a
// catalog read on every question about the watch.
func collectDoctorSnapshot(path string, graph *store.Store, watch standingWatchStatus, dailyBudget float64,
	sweModel func(profileDir string) sweModelReport, now time.Time) (doctorSnapshot, error) {
	snapshot := doctorSnapshot{
		BrainPath: path, Resident: readResident(residentLockFor(path)),
		Rail: dailyBudget, RailUnlimited: dailyBudget <= 0, Now: now,
	}
	if sweModel != nil {
		snapshot.SWEModel = sweModel(filepath.Dir(path))
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
	spend := fmt.Sprintf("$%.2f today · rail $%.2f", snapshot.Spend, snapshot.Rail)
	if snapshot.RailUnlimited {
		spend = fmt.Sprintf("$%.2f today · rail unlimited", snapshot.Spend)
	}
	standing := fmt.Sprintf("%d active %s · %d pending %s",
		snapshot.ActiveCharters, pluralWord(snapshot.ActiveCharters, "charter"),
		snapshot.PendingQuestions, pluralWord(snapshot.PendingQuestions, "question"))
	block := fmt.Sprintf("%-16s %s\n%-16s %s\n%-16s %s\n%-16s %s\n%-16s %s\n",
		"brain", brain,
		"resident", snapshot.Resident,
		"standing watch", watch,
		"spend", spend,
		"standing", standing)
	if line := formatSWEModel(snapshot.SWEModel); line != "" {
		block += fmt.Sprintf("%-16s %s\n", "coding model", line)
	}
	return block
}

// formatSWEModel says whether coding work can start, in one line and in the
// words of the failure it prevents. Nothing is printed when the probe did not
// run, because a line that only ever means "we did not look" is a line a person
// learns to read past.
func formatSWEModel(report sweModelReport) string {
	if strings.TrimSpace(report.Model) == "" {
		return ""
	}
	line := report.Model
	if report.Sent != "" && report.Sent != report.Model {
		line += " → " + report.Sent
	}
	switch {
	case report.Unknown:
		// Not a verdict on the model. The engine's catalog could not be read,
		// and doctor says which so the next step is obvious.
		return line + " · unchecked, the engine's model catalog is unavailable" + detailSuffix(report.Detail)
	case report.Resolves:
		return line + " · the coding worker can price it"
	default:
		return line + " · the coding worker's catalog (models.dev) has no such model — swe leaves will fail at startup"
	}
}

func detailSuffix(detail string) string {
	detail = firstLine(strings.TrimSpace(detail))
	if detail == "" {
		return ""
	}
	return " (" + clip(detail, 120) + ")"
}

// standingWatchCharterCap bounds what one grounding read names. A person with
// more standing charters than this has a policy rather than a watch list, and
// the count above the lines still says how many there are.
const standingWatchCharterCap = 8

// watchGrounding is the head's standing read: doctor's own calm status block,
// plus what each active charter actually watches for.
//
// The block alone said "3 active charters" and stopped. Everything the question
// is about — the invariant, the cadence — was one store read away and rendered
// in full three surfaces over, so the head could report a number and nothing a
// person would recognise as an answer. The lines are the /standing lines
// verbatim, because two renderings of one thing eventually disagree.
// The coding-model probe is deliberately not asked for here: the head's
// question is about the watch, and a standing read must not spend a catalog
// lookup — or a line of the answer — on a worker nobody asked about.
func watchGrounding(path string, graph *store.Store, watch standingWatchStatus, dailyBudget float64) string {
	snapshot, err := collectDoctorSnapshot(path, graph, watch, dailyBudget, nil, time.Now())
	if err != nil {
		return "standing watch status unavailable: " + err.Error()
	}
	grounding := strings.TrimSpace(formatDoctor(snapshot))
	lines, err := command.CharterLines(graph, standingWatchCharterCap)
	if err != nil || len(lines) == 0 {
		return grounding
	}
	return grounding + "\n\nactive charters (id · cadence · what it watches for):\n" +
		strings.Join(lines, "\n")
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
