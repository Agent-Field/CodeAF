package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/crewroute"
	"github.com/Agent-Field/codeaf/internal/devname"
)

// TaskComputerControlWithin bounds best-effort stop and acknowledgement calls.
const TaskComputerControlWithin = 10 * time.Second

// TaskComputer is a roster entry whose name can change without changing a
// saved preference. An unsupported target carries the sentence explaining why.
type TaskComputer struct {
	Device       string
	Name         string
	OriginalName string `json:"originalName,omitempty"`
	CanTakeTasks bool
	Reason       string
}

// TaskComputers is absent where paired task placement cannot work. Session
// owns this boundary because the transport is built on the local run engine.
type TaskComputers interface {
	List(context.Context) ([]TaskComputer, error)
	ThisComputer() string
	Engine(context.Context, string) (TaskRunEngine, error)
}

// TaskRunEngine extends the ordinary engine only for a remote attempt's
// lifetime. Landing always belongs to the registered local engine.
type TaskRunEngine interface {
	RunEngine
	Resume(context.Context, RunSpec) RunSummary
	Stop(context.Context, TaskRemoteRecord) error
	Abandon(context.Context, TaskRemoteRecord) error
	Acknowledge(context.Context, TaskRemoteRecord) error
	CheckCopy(context.Context, string, string) error
}

// TaskComputerRecovery builds a watcher from an already admitted identity,
// even while the directory is unreachable. Network checks belong in the watch.
type TaskComputerRecovery interface {
	RecoverEngine(TaskExecutor) TaskRunEngine
}

func (a *Agent) taskExecutorEngine(ctx context.Context, executor TaskExecutor) (TaskRunEngine, error) {
	if computers, ok := a.config.TaskComputers.(TaskComputerRecovery); ok {
		return computers.RecoverEngine(executor), nil
	}
	return a.config.TaskComputers.Engine(ctx, executor.Device)
}

type TaskExecutor struct {
	Device string `json:"device"`
	Name   string `json:"name"`
}

// TaskRemoteRecord is additive to the run row. Missing records describe local
// work; abandoned records fence late results before a local replacement starts.
type TaskRemoteRecord struct {
	NamedModel  string          `json:"named_model,omitempty"`
	NamedEffort string          `json:"named_effort,omitempty"`
	MaxSteps    int             `json:"max_steps,omitempty"`
	NoProgress  int             `json:"no_progress,omitempty"`
	Executor    TaskExecutor    `json:"executor"`
	Attempt     string          `json:"attempt,omitempty"`
	Fingerprint string          `json:"fingerprint,omitempty"`
	Seq         uint64          `json:"seq,omitempty"`
	USD         float64         `json:"usd,omitempty"`
	StartFailed bool            `json:"start_failed,omitempty"`
	Unreachable bool            `json:"unreachable,omitempty"`
	Abandoned   bool            `json:"abandoned,omitempty"`
	Finished    bool            `json:"finished,omitempty"`
	Settled     bool            `json:"settled,omitempty"`
	AckPending  bool            `json:"ack_pending,omitempty"`
	Final       json.RawMessage `json:"final,omitempty"`
	Restored    bool            `json:"restored,omitempty"`
}

type taskPlacement struct {
	computer             *TaskComputer
	sentence             string
	model, effort        string
	maxSteps, noProgress int
}
type taskPlacementKey struct{}

func withTaskPlacement(ctx context.Context, p taskPlacement) context.Context {
	return context.WithValue(ctx, taskPlacementKey{}, p)
}
func taskPlacementOf(ctx context.Context) taskPlacement {
	p, _ := ctx.Value(taskPlacementKey{}).(taskPlacement)
	return p
}

const taskComputerSchema = `"computer":{"type":"string","description":"The paired computer to run the task on, by its shown name. Omitted means the Run tasks on setting decides; that is ` + config.TaskRunsHere + ` unless the person changed it. Only a task that works in its own copy can go."},`

// resolveTaskComputer runs once at admission, before a copy or a join. A stale
// preference is harmless, while an explicit misspelling is a correctable error.
func (a *Agent) resolveTaskComputer(ctx context.Context, named string, stand taskStand, kind TaskKind, program bool) (taskPlacement, error) {
	p := taskPlacementOf(ctx)
	named = strings.TrimSpace(named)
	if a.config.TaskComputers == nil {
		return p, nil
	}
	explicit := named != ""
	if explicit && (strings.EqualFold(named, "here") || strings.EqualFold(named, config.TaskRunsHere)) {
		return p, nil
	}
	preference := config.TaskRunsOnAt(a.config.ProfileDir)
	if !explicit && preference == "" {
		return p, nil
	}
	computers, err := a.config.TaskComputers.List(ctx)
	if err != nil {
		return p, err
	}
	localName := a.config.TaskComputers.ThisComputer()
	computer, err := chooseTaskComputer(TaskComputerNames(computers, localName), named, preference, localName)
	if err != nil || computer == nil {
		return p, err
	}
	return eligibleTaskComputer(p, *computer, stand, kind, program, explicit), nil
}

// TaskComputerNames keeps device labels consistent while retaining ambiguous bare names for refusal.
func TaskComputerNames(rows []TaskComputer, localName string) []TaskComputer {
	rows = append([]TaskComputer(nil), rows...)
	counts := map[string]int{strings.ToLower(strings.TrimSpace(localName)): 1}
	for _, row := range rows {
		counts[taskComputerBareName(row)]++
	}
	for i := range rows {
		row := &rows[i]
		if counts[taskComputerBareName(*row)] > 1 {
			if row.OriginalName == "" {
				row.OriginalName = row.Name
			}
			row.Name = devname.DistinctName(row.OriginalName, row.Device)
		}
	}
	return rows
}
func taskComputerBareName(row TaskComputer) string {
	name := row.OriginalName
	if name == "" {
		name = row.Name
	}
	return strings.ToLower(strings.TrimSpace(name))
}

// chooseTaskComputer never gives roster order authority over an ambiguous name.
func chooseTaskComputer(computers []TaskComputer, named, preference, localName string) (*TaskComputer, error) {
	explicit := named != ""
	local := explicit && strings.EqualFold(named, localName)
	var matches []TaskComputer
	for _, computer := range computers {
		if taskComputerMatches(computer, named, preference) {
			matches = append(matches, computer)
		}
	}
	if len(matches) > 1 || local && len(matches) > 0 {
		choices := taskComputerChoices(matches)
		if local {
			choices = append(choices, config.TaskRunsHere)
		}
		return nil, fmt.Errorf("%q matches more than one paired computer — choose one of: %s", named, strings.Join(choices, ", "))
	}
	if local {
		return nil, nil
	}
	if len(matches) == 1 {
		return &matches[0], nil
	}
	if explicit {
		return nil, fmt.Errorf("no paired computer is named %q — your computers are: %s", named, strings.Join(taskComputerChoices(computers), ", "))
	}
	return nil, nil
}
func taskComputerMatches(computer TaskComputer, named, preference string) bool {
	if named == "" {
		return preference != "" && computer.Device == preference
	}
	return strings.EqualFold(named, computer.Name) || strings.EqualFold(named, taskComputerBareName(computer))
}
func taskComputerChoices(computers []TaskComputer) []string {
	names := make([]string, 0, len(computers))
	for _, computer := range computers {
		names = append(names, computer.Name)
	}
	sort.Strings(names)
	if len(names) == 0 {
		names = append(names, config.TaskRunsHere)
	}
	return names
}

// Ineligible work retains the existing local road and its explicit-choice explanation.
func eligibleTaskComputer(p taskPlacement, computer TaskComputer, stand taskStand, kind TaskKind, program, explicit bool) taskPlacement {
	if !computer.CanTakeTasks {
		if explicit {
			p.sentence = computer.Reason
		}
		return p
	}
	reason := ""
	switch {
	case kind == TaskKindQuick:
		reason = "a quick task"
	case program:
		reason = "a program run"
	case !taskCopyCanTravel(stand):
		reason = "this task"
	}
	if reason != "" {
		if explicit {
			p.sentence = reason + " runs here — only a task that works in its own copy can run on " + computer.Name
		}
		return p
	}
	p.computer = &computer
	return p
}

// StartTaskOn is the shared session entry for a person's explicit placement.
// Model proposals reach the same resolver through their normal admission road.
// The optional effort belongs to this task and leaves the standing crew alone.
func (a *Agent) StartTaskOn(ctx context.Context, brief string, solo bool, computer string, namedEffort ...string) (uint64, string, string, error) {
	wish := crewWishOf(ctx)
	if len(namedEffort) > 0 {
		wish.effort, _ = crewroute.ParseEffort(namedEffort[0])
		ctx = withCrewWish(ctx, wish)
	}
	stand := a.taskGroundOrStandingIn(taskSpec{title: taskPersonTitle(brief), request: brief, brief: brief, acceptance: taskPersonAcceptance})
	p, err := a.resolveTaskComputer(ctx, computer, stand, "", false)
	if err != nil {
		return 0, "", "", err
	}
	p.effort = string(wish.effort)
	var id uint64
	var title, note string
	if p.computer != nil {
		id, title, note, err = a.startTaskRun(withTaskPlacement(context.WithoutCancel(ctx), p), brief, solo, "")
	} else {
		id, title, note, err = a.StartTask(withTaskPlacement(ctx, p), brief, solo)
	}
	return id, title, withReport(note, p.sentence), err
}

// TaskComputersSupported lets a connected window omit controls that this
// conversation cannot honor, even though the agent has the method shapes.
func (a *Agent) TaskComputersSupported() bool { return a.config.TaskComputers != nil }

var errRemotePlacementBusy = errors.New("the work already underway uses another computer; let it finish before starting this task")

// cloneTaskRemote keeps published rows immutable while the watcher advances.
func cloneTaskRemote(r *TaskRemoteRecord) *TaskRemoteRecord {
	if r == nil {
		return nil
	}
	copy := *r
	copy.Final = append(json.RawMessage(nil), r.Final...)
	return &copy
}

func (a *Agent) taskComputerPreference() bool {
	return a.config.TaskComputers != nil && config.TaskRunsOnAt(a.config.ProfileDir) != ""
}

// A private singleton uses the same carrier as an ordinary repository copy.
// Its folder mirror also has the independent history prepared by that road.
func taskCopyCanTravel(stand taskStand) bool {
	if stand.mode == TaskModeInPlace || stand.mode == TaskModeReference || stand.mode == TaskModeFolder {
		return false
	}
	if stand.workspaceSet != nil {
		return len(stand.workspaceSet.Roots) == 1 && (stand.mode == TaskModeWorktree || stand.mode == TaskModeMirror)
	}
	return stand.mode == TaskModeWorktree
}

// Pairing supplies the person's placement door. Only a saved remote preference
// adds placement to the model's vocabulary. Both reads are local.
func (c Config) mayNameTaskComputer() bool {
	return c.TaskComputers != nil && config.TaskRunsOnAt(c.ProfileDir) != ""
}

// The selected engine is resolved before start admission without changing local placement.
func (a *Agent) placeTaskRunEngine(ctx context.Context, engine RunEngine, stand taskStand, program bool) (context.Context, RunEngine, error) {
	placement := taskPlacementOf(ctx)
	if _, resolved := ctx.Value(taskPlacementKey{}).(taskPlacement); !resolved {
		var err error
		placement, err = a.resolveTaskComputer(ctx, "", stand, "", program)
		if err != nil {
			return ctx, nil, err
		}
		ctx = withTaskPlacement(ctx, placement)
	}
	if placement.computer == nil {
		return ctx, engine, nil
	}
	remote, err := a.config.TaskComputers.Engine(ctx, placement.computer.Device)
	if err != nil {
		return ctx, nil, err
	}
	if remote == nil {
		return ctx, nil, errors.New("this computer cannot run that task elsewhere right now")
	}
	return ctx, remote, nil
}
