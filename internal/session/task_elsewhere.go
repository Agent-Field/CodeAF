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
)

// TaskComputerControlWithin bounds best-effort stop and acknowledgement calls.
const TaskComputerControlWithin = 10 * time.Second

// TaskComputer is a roster entry whose name can change without changing a
// saved preference. An unsupported target carries the sentence explaining why.
type TaskComputer struct {
	Device       string
	Name         string
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
	if explicit && (strings.EqualFold(named, "here") || strings.EqualFold(named, config.TaskRunsHere) || strings.EqualFold(named, a.config.TaskComputers.ThisComputer())) {
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
	for _, computer := range computers {
		if explicit && strings.EqualFold(strings.TrimSpace(computer.Name), named) || !explicit && preference != "" && computer.Device == preference {
			if !computer.CanTakeTasks {
				if explicit {
					p.sentence = computer.Reason
				}
				return p, nil
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
				return p, nil
			}
			p.computer = &computer
			return p, nil
		}
	}
	if explicit {
		names := make([]string, 0, len(computers))
		for _, c := range computers {
			names = append(names, c.Name)
		}
		sort.Strings(names)
		if len(names) == 0 {
			names = append(names, config.TaskRunsHere)
		}
		return p, fmt.Errorf("no paired computer is named %q — your computers are: %s", named, strings.Join(names, ", "))
	}
	return p, nil
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
