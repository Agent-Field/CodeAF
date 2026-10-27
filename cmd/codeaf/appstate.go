package main

// ── codeaf app: SETUP AND SETTINGS ──────────────────────────────────────────
//
// TWO KINDS OF SETTING, AND WHERE EACH LIVES. A setting the terminal also has
// — the default model, a conversation's budget, what asks before it runs, the
// task cap, the default effort — is the profile's own row, written through the
// profile's own writers, so the app and the terminal never disagree about it.
// A setting only the app has — the
// folder a new agent works in, the proposed-task clock, how long an idle agent
// stays open — and how far first-run setup has come are this file's: one small
// JSON file in codeaf's home, read the first time it is asked for and written
// whole on every change. The daily limit is NOT one of them: it is the
// profile's own row, shared with the terminal (appdaylimit.go).

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/effort"
	"github.com/Agent-Field/codeaf/internal/fsreplace"
	"github.com/Agent-Field/codeaf/internal/home"
)

// The app's own defaults. The day cap is what first-run setup offers at its
// second step; the terminal's daily limit keeps its own default.
const (
	appDayCapDefault      = 20.0
	appProposedClockS     = 12
	appIdleCloseDays      = 7
	appStateFolder        = "app"
	appStateName          = "state.json"
	appStateFileMode      = 0o600
	appStateFolderMode    = 0o700
	appMidnightRaiseScope = "day"
)

// appRaise is a cap raised until a moment, which is the next local midnight.
type appRaise struct {
	USD   float64   `json:"usd"`
	Until time.Time `json:"until"`
}

// appStateFile is the app's file. A nil figure is one nobody set, which reads
// as its default; a day cap of zero is one the person turned off.
type appStateFile struct {
	Setup *appwire.Setup `json:"setup,omitempty"`
	// SetupSteps is how many steps the kept setup's step is counted in. A
	// file kept before setup became three steps has none, and its step is
	// counted in four ([appSetupStepFromFour]).
	SetupSteps int    `json:"setup_steps,omitempty"`
	WorkFolder string `json:"work_folder,omitempty"`
	// DayCapUSD is LEGACY and read-only: the daily limit is the profile's
	// (appdaylimit.go), and this is read once, to hand it over.
	DayCapUSD          *float64            `json:"day_cap_usd,omitempty"`
	ProposedTaskClockS *int                `json:"proposed_task_clock_s,omitempty"`
	IdleCloseDays      *int                `json:"idle_close_days,omitempty"`
	Budgets            map[string]float64  `json:"budgets,omitempty"`
	Raises             map[string]appRaise `json:"raises,omitempty"`
	// Unlimited is the agents a person said have no budget of their own, and
	// when (appbudget.go). A zero in Budgets never meant that, so it is kept
	// apart rather than read into a figure an older file already holds.
	Unlimited map[string]time.Time `json:"budgets_unlimited,omitempty"`
	// ResumeAfterQuit is the agents the last quit stopped in the middle of a
	// reply, and QuitStopped the processes it stopped (appquit.go).
	// QuitReplies is those of them that were in the middle of a reply, and
	// QuitTasks the tasks they had running.
	ResumeAfterQuit []string          `json:"resume_after_quit,omitempty"`
	QuitReplies     []string          `json:"quit_replies,omitempty"`
	QuitTasks       []string          `json:"quit_tasks,omitempty"`
	QuitStopped     []appwire.Process `json:"quit_stopped,omitempty"`
	// CloseResumes and CloseStarts bound automatic work across process deaths.
	// CloseHandled keeps only the newest handled stamp per existing agent.
	// These guards rely on one app engine per home: the desktop shell enforces
	// single instance with tauri-plugin-single-instance, and its supervisor runs
	// exactly one engine, as the other app state fields already assume.
	CloseResumes   map[string]int       `json:"close_resumes,omitempty"`
	CloseResumedAt map[string]time.Time `json:"close_resumed_at,omitempty"`
	CloseStarts    []time.Time          `json:"close_starts,omitempty"`
	CloseHandled   map[string]time.Time `json:"close_handled,omitempty"`
	// ClosePending is owed until a started turn consumes its own keyed note.
	// Its original time bounds retries even if no busy claim remains.
	ClosePending   map[string]string    `json:"close_pending,omitempty"`
	ClosePendingAt map[string]time.Time `json:"close_pending_at,omitempty"`
	// Closed and Archived are when the person moved each agent to Closed or
	// put it away (apptidy.go).
	Closed   map[string]time.Time `json:"closed,omitempty"`
	Archived map[string]time.Time `json:"archived,omitempty"`
	// The Settings window's General rows and pinned models (appsettings.go);
	// a nil row is one nobody set, which reads as its default.
	Appearance   string   `json:"appearance,omitempty"`
	TextSize     string   `json:"text_size,omitempty"`
	OpenAtLogin  *bool    `json:"open_at_login,omitempty"`
	DockBadge    *bool    `json:"dock_badge,omitempty"`
	PinnedModels []string `json:"pinned_models,omitempty"`
}

// appState is the file, loaded once and written whole.
type appState struct {
	mu     sync.Mutex
	path   string
	loaded bool
	file   appStateFile
}

// where is where this state is kept.
func (s *appState) where() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.path == "" {
		return appStatePath()
	}
	return s.path
}

// appStatePath is where the file lives under codeaf's home.
func appStatePath() string {
	return filepath.Join(home.Dir(), appStateFolder, appStateName)
}

// loadLocked reads the file the first time. A file that is missing is the
// defaults; one that cannot be read is refused rather than written over.
func (s *appState) loadLocked() error {
	if s.loaded {
		return nil
	}
	if s.path == "" {
		s.path = appStatePath()
	}
	raw, err := os.ReadFile(s.path)
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		return err
	default:
		if err := json.Unmarshal(raw, &s.file); err != nil {
			return err
		}
	}
	s.loaded = true
	return nil
}

// read hands fn the file as it stands.
func (s *appState) read(fn func(*appStateFile)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadLocked(); err != nil {
		return err
	}
	fn(&s.file)
	return nil
}

// write changes the file and saves it whole: a new file beside it, renamed
// over it, so a quit halfway never leaves half a file.
func (s *appState) write(fn func(*appStateFile) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.loadLocked(); err != nil {
		return err
	}
	next := s.file.clone()
	if err := fn(&next); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), appStateFolderMode); err != nil {
		return err
	}
	temp := s.path + ".new"
	if err := os.WriteFile(temp, raw, appStateFileMode); err != nil {
		return err
	}
	if err := fsreplace.Rename(temp, s.path); err != nil {
		return err
	}
	s.file = next
	return nil
}

// clone is a copy that shares nothing with the file it came from.
func (f appStateFile) clone() appStateFile {
	out := f
	if f.Setup != nil {
		setup := *f.Setup
		out.Setup = &setup
	}
	out.Budgets = make(map[string]float64, len(f.Budgets))
	for k, v := range f.Budgets {
		out.Budgets[k] = v
	}
	out.Raises = make(map[string]appRaise, len(f.Raises))
	for k, v := range f.Raises {
		out.Raises[k] = v
	}
	out.Unlimited = make(map[string]time.Time, len(f.Unlimited))
	for k, v := range f.Unlimited {
		out.Unlimited[k] = v
	}
	out.Closed = make(map[string]time.Time, len(f.Closed))
	for k, v := range f.Closed {
		out.Closed[k] = v
	}
	out.Archived = make(map[string]time.Time, len(f.Archived))
	for k, v := range f.Archived {
		out.Archived[k] = v
	}
	out.ResumeAfterQuit = append([]string(nil), f.ResumeAfterQuit...)
	out.QuitReplies = append([]string(nil), f.QuitReplies...)
	out.QuitTasks = append([]string(nil), f.QuitTasks...)
	out.QuitStopped = append([]appwire.Process(nil), f.QuitStopped...)
	out.CloseStarts = append([]time.Time(nil), f.CloseStarts...)
	out.CloseResumes = make(map[string]int, len(f.CloseResumes))
	for k, v := range f.CloseResumes {
		out.CloseResumes[k] = v
	}
	out.CloseHandled = make(map[string]time.Time, len(f.CloseHandled))
	for k, v := range f.CloseHandled {
		out.CloseHandled[k] = v
	}
	out.CloseResumedAt = make(map[string]time.Time, len(f.CloseResumedAt))
	for k, v := range f.CloseResumedAt {
		out.CloseResumedAt[k] = v
	}
	out.ClosePending = make(map[string]string, len(f.ClosePending))
	for k, v := range f.ClosePending {
		out.ClosePending[k] = v
	}
	out.ClosePendingAt = make(map[string]time.Time, len(f.ClosePendingAt))
	for k, v := range f.ClosePendingAt {
		out.ClosePendingAt[k] = v
	}
	out.PinnedModels = append([]string(nil), f.PinnedModels...)
	return out
}

// raised is a cap with its raise for today laid over it, if one is current.
func (f *appStateFile) raised(scope string, base float64, now time.Time) float64 {
	if raise, ok := f.Raises[scope]; ok && now.Before(raise.Until) && raise.USD > base {
		return raise.USD
	}
	return base
}

func (f *appStateFile) clock() int {
	if f.ProposedTaskClockS == nil {
		return appProposedClockS
	}
	return *f.ProposedTaskClockS
}

func (f *appStateFile) idleDays() int {
	if f.IdleCloseDays == nil {
		return appIdleCloseDays
	}
	return *f.IdleCloseDays
}

// appMidnight is the next local midnight after now, which is when a raise for
// today ends.
func appMidnight(now time.Time) time.Time {
	return time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, now.Location())
}

// workFolder is where an agent created with no workspace works: the folder
// the person chose, or their home folder.
func (f *appStateFile) workFolder() string {
	if folder := strings.TrimSpace(f.WorkFolder); folder != "" {
		return folder
	}
	if dir, err := os.UserHomeDir(); err == nil {
		return dir
	}
	return ""
}

// appSetupStepsBefore is how many steps setup had before the welcome and the
// connecting step became one.
const appSetupStepsBefore = 4

// appSetupStepFromFour is where a step kept by a four-step build lands now:
// the welcome (1) and connecting a provider (2) are both the new first step,
// because the welcome is now the head of the connecting step; the defaults (3)
// are the second, and how to start (4) is the third. "done" stays done.
//
// A PERSON HALFWAY THROUGH SETUP WHEN THEIR APP UPDATES IS NEVER STRANDED:
// without this a kept 3 would reopen on the last step, skipping the defaults,
// and a kept 4 would be no step at all.
func appSetupStepFromFour(step appwire.SetupStep) appwire.SetupStep {
	if step.Done {
		return step
	}
	switch {
	case step.N <= 2:
		return appwire.SetupStep{N: 1}
	case step.N >= appSetupStepsBefore:
		return appwire.SetupStep{N: appwire.SetupSteps}
	default:
		return appwire.SetupStep{N: step.N - 1}
	}
}

// Setup answers how far first-run setup has come; a setup never started is at
// step one, offering the app's day cap. A setup a four-step build kept is
// answered in three steps.
func (b *appBackend) Setup() (appwire.Setup, error) {
	// The day cap offered is the profile's limit, whatever the saved step says.
	base, err := b.dayBase()
	if err != nil {
		return appwire.Setup{}, appEngineError(err)
	}
	var setup appwire.Setup
	err = b.state.read(func(f *appStateFile) {
		if f.Setup != nil {
			setup = *f.Setup
			if f.SetupSteps == 0 {
				setup.Step = appSetupStepFromFour(setup.Step)
			}
			return
		}
		setup = appwire.Setup{Step: appwire.SetupStep{N: 1}}
	})
	if err != nil {
		return appwire.Setup{}, appEngineError(err)
	}
	setup.DayCapUSD = appFigure(base)
	return setup, nil
}

// SetSetup keeps the setup the app sent, so quitting halfway resumes at the
// same step. What it chose is also put in force at once: a default model is the
// profile's default, and a day cap is the app's day cap.
func (b *appBackend) SetSetup(setup appwire.Setup) (appwire.Setup, error) {
	setup.Provider = strings.ToLower(strings.TrimSpace(setup.Provider))
	setup.DefaultModel = strings.TrimSpace(setup.DefaultModel)
	if setup.DefaultModel != "" && setup.DefaultModel != appDefaultModel() {
		if err := config.WriteChatModel(config.ProfileDir(), setup.DefaultModel); err != nil {
			return appwire.Setup{}, appEngineError(err)
		}
	}
	// A day cap the environment would keep overriding is refused before the
	// setup is kept, so nothing is reported as chosen that is not in force.
	if setup.DayCapUSD != nil {
		if err := dayLimitFree(); err != nil {
			return appwire.Setup{}, err
		}
	}
	err := b.state.write(func(f *appStateFile) error {
		kept := setup
		f.Setup = &kept
		f.SetupSteps = appwire.SetupSteps
		return nil
	})
	if err != nil {
		return appwire.Setup{}, appEngineError(err)
	}
	if setup.DayCapUSD != nil {
		if err := b.setDayBase(*setup.DayCapUSD); err != nil {
			return appwire.Setup{}, err
		}
	}
	if setup.DefaultModel != "" || setup.DayCapUSD != nil {
		b.settingsMoved()
	}
	return setup, nil
}

// Settings reads the profile's rows and the app's own file; a figure nobody
// set is null.
func (b *appBackend) Settings() (appwire.Settings, error) {
	profile := config.ProfileDir()
	var file appStateFile
	if err := b.state.read(func(f *appStateFile) { file = f.clone() }); err != nil {
		return appwire.Settings{}, appEngineError(err)
	}
	// SETTINGS SAYS THE BASE LIMIT; Spend says the one in force with any
	// raise for today laid over it.
	base, err := b.dayBase()
	if err != nil {
		return appwire.Settings{}, appEngineError(err)
	}
	settings := appwire.Settings{
		DefaultModel:       appDefaultModel(),
		AsksBefore:         config.ToolApprovalModeAt(profile),
		BudgetUSD:          appFigure(config.SpendRailUSDAt(profile)),
		DayCapUSD:          appFigure(base),
		WorkFolder:         file.workFolder(),
		TaskCapUSD:         appFigure(config.CrewTaskCapAt(profile)),
		ProposedTaskClockS: file.clock(),
		IdleCloseDays:      file.idleDays(),
	}
	if rung := config.DefaultEffortAt(profile); rung.Valid() {
		settings.Effort = rung.String()
	}
	settingsExtra(&settings, file, profile)
	return settings, nil
}

// SetSettings changes the fields the patch names, each through its own door,
// and says settings.changed once with what they all read now.
func (b *appBackend) SetSettings(patch appwire.SettingsPatch) (appwire.Settings, error) {
	profile := config.ProfileDir()
	if patch.DayCapUSD != nil {
		// Before any other field lands: a limit the environment pins is
		// refused whole, not half-applied.
		if err := dayLimitFree(); err != nil {
			return appwire.Settings{}, err
		}
	}
	apply := func(key, raw string) error {
		row, ok := config.NewSettings(config.SettingsOptions{ProfileDir: profile}).Row(key)
		if !ok {
			return appwire.Errorf(appwire.CodeEngine, "this build has no %s setting", key)
		}
		if err := row.Apply(raw); err != nil {
			return appwire.Errorf(appwire.CodeBadParams, "%s", err.Error())
		}
		return nil
	}
	if patch.DefaultModel != nil {
		if *patch.DefaultModel == "" {
			return appwire.Settings{}, appwire.Errorf(appwire.CodeBadParams, "name a model")
		}
		if err := config.WriteChatModel(profile, *patch.DefaultModel); err != nil {
			return appwire.Settings{}, appwire.Errorf(appwire.CodeBadParams, "%s", err.Error())
		}
	}
	if patch.AsksBefore != nil {
		if err := apply(config.KeyToolApprovalMode, *patch.AsksBefore); err != nil {
			return appwire.Settings{}, err
		}
	}
	if patch.BudgetUSD != nil {
		raw := "none"
		if patch.BudgetUSD.USD != nil {
			raw = appDollars(*patch.BudgetUSD.USD)
		}
		if err := apply(config.KeySpendRail, raw); err != nil {
			return appwire.Settings{}, err
		}
	}
	if patch.TaskCapUSD != nil {
		if patch.TaskCapUSD.USD == nil {
			return appwire.Settings{}, appwire.Errorf(appwire.CodeBadParams, "a task always has a limit — a dollar amount above zero")
		}
		if err := config.SetCrewTaskCap(profile, appDollars(*patch.TaskCapUSD.USD)); err != nil {
			return appwire.Settings{}, appwire.Errorf(appwire.CodeBadParams, "%s", err.Error())
		}
	}
	if patch.Effort != nil {
		rung, ok := effort.Parse(*patch.Effort)
		if !ok || (rung != effort.None && rung != effort.Low && rung != effort.Medium && rung != effort.High) {
			return appwire.Settings{}, appwire.Errorf(appwire.CodeBadParams, "effort is low, medium, high, or empty for the model's own")
		}
		if err := config.WriteDefaultEffort(profile, rung); err != nil {
			return appwire.Settings{}, appEngineError(err)
		}
	}
	if err := b.applyExtra(patch, profile); err != nil {
		return appwire.Settings{}, err
	}
	if patch.WorkFolder != nil && *patch.WorkFolder != "" {
		if _, err := engineWorkspace(*patch.WorkFolder); err != nil {
			return appwire.Settings{}, appwire.Errorf(appwire.CodeBadParams, "%s", err.Error())
		}
	}
	if patch.WorkFolder != nil || patch.ProposedTaskClockS != nil || patch.IdleCloseDays != nil {
		err := b.state.write(func(f *appStateFile) error {
			if patch.WorkFolder != nil {
				f.WorkFolder = *patch.WorkFolder
			}
			if patch.ProposedTaskClockS != nil {
				n := *patch.ProposedTaskClockS
				f.ProposedTaskClockS = &n
			}
			if patch.IdleCloseDays != nil {
				n := *patch.IdleCloseDays
				f.IdleCloseDays = &n
			}
			return nil
		})
		if err != nil {
			return appwire.Settings{}, appEngineError(err)
		}
	}
	if patch.DayCapUSD != nil {
		// ONE WRITE, ONE AUTHORITY: the profile's limit, which also ends any
		// raise for today, so the figure just chosen is the one in force.
		usd := 0.0
		if patch.DayCapUSD.USD != nil {
			usd = *patch.DayCapUSD.USD
		}
		if err := b.setDayBase(usd); err != nil {
			return appwire.Settings{}, err
		}
	}
	settings, err := b.Settings()
	if err != nil {
		return appwire.Settings{}, err
	}
	// THE TASK MODELS RIDE THIS EVENT ONLY WHEN THIS PATCH CHANGED ONE OF
	// THEIR ROWS — the checker and planner pins are the Checks and Planner
	// rows — so a budget, an appearance or a folder is never held up by them.
	b.settingsChanged(settings, patch.ChecksModel != nil || patch.PlannerModel != nil)
	if patch.DayCapUSD != nil {
		b.spendSay()
	}
	return settings, nil
}

// settingsMoved says settings.changed with what the settings read now.
func (b *appBackend) settingsMoved() {
	if settings, err := b.Settings(); err == nil {
		b.emit.Emit(appwire.EventSettingsChanged, appwire.SettingsChanged{Settings: settings})
	}
}

// appDollars writes a figure the way the profile's money writers read one.
func appDollars(usd float64) string {
	return strconv.FormatFloat(usd, 'f', -1, 64)
}
