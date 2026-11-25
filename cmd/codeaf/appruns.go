package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/runs"
	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/provider"
	"github.com/Agent-Field/codeaf/internal/session"
)

const appRunAskAgent = "app-runs"
const appRunAskPrefix = "run:"

func (b *appBackend) openAppRuns(ctx context.Context) func() {
	files := runs.Files(home.Join("apps"))
	tools := runs.ConnectedTools{Apps: files.App, Manager: b.pluginManager}
	e, err := runs.Open(runs.Config{
		Root: home.Join("apps", "_runs"), Load: files.Load, Triggers: files.Triggers,
		Connector: tools, CheckTool: tools.Check, Agent: b.appRunAgent(files),
		Changed: func(event runs.ChangeEvent) { b.emit.Emit(appwire.EventRunsChanged, event) },
		Ask:     b.openRunApproval,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "app runs:", err)
		return func() {}
	}
	b.appRuns = e
	past, err := e.List("", "")
	if err == nil {
		for _, run := range past {
			if run.Approval != nil {
				b.openRunApproval(run)
			}
		}
	}
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	guard.Go("app/runs", func() {
		defer close(done)
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		var last string
		for ctx.Err() == nil {
			if err := e.Tick(ctx); err != nil && ctx.Err() == nil && err.Error() != last {
				last = err.Error()
				fmt.Fprintln(os.Stderr, "app runs:", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	})
	return func() {
		cancel()
		<-done
		if err := e.Close(); err != nil {
			fmt.Fprintln(os.Stderr, "app runs:", err)
		}
	}
}

func (b *appBackend) StartRun(app, run string, params map[string]any) (runs.Run, error) {
	if b.appRuns == nil {
		return runs.Run{}, appwire.Errorf(appwire.CodeEngine, "app runs are unavailable")
	}
	r, err := b.appRuns.Start(app, run, params)
	return r, appRunError(err)
}

func (b *appBackend) ListRuns(app, run string) ([]runs.Run, error) {
	if b.appRuns == nil {
		return nil, appwire.Errorf(appwire.CodeEngine, "app runs are unavailable")
	}
	return b.appRuns.List(app, run)
}

func (b *appBackend) GetRun(app, id string) (runs.Run, error) {
	if b.appRuns == nil {
		return runs.Run{}, appwire.Errorf(appwire.CodeEngine, "app runs are unavailable")
	}
	r, err := b.appRuns.Get(id)
	if err == nil && app != "" && app != r.App {
		err = runs.ErrNotFound
		r = runs.Run{}
	}
	return r, appRunError(err)
}

func appRunError(err error) error {
	if errors.Is(err, runs.ErrNotFound) || errors.Is(err, fs.ErrNotExist) {
		return appwire.Errorf(appwire.CodeNotFound, "run not found")
	}
	return err
}

func (b *appBackend) appRunAgent(files runs.Files) func(context.Context, runs.AgentCall) (runs.Reply, error) {
	return func(ctx context.Context, call runs.AgentCall) (runs.Reply, error) {
		system, model, err := files.AgentPrompt(call.App)
		if err != nil {
			return runs.Reply{}, err
		}
		settings, err := config.Load()
		if err != nil {
			return runs.Reply{}, err
		}
		model = v3TalkModel(model, settings)
		clientConfig := settings.ClientConfig(model)
		clientConfig.Fallbacks = nil
		client, err := provider.NewClient(clientConfig)
		if err != nil {
			return runs.Reply{}, err
		}
		spend := &session.SpendGuard{
			Price:    config.CrewCallPriceAt(b.crewProfile()),
			DayLimit: config.CrewDailyLimit(b.crewProfile()),
			TaskCap:  call.Remaining, TaskAction: "per-run budget reached",
		}
		const name = "app run"
		ctx, recorded := factoryAIAccounting(provider.WithCallTag(ctx, name), settings, model, name, nil)
		prompt := call.Prompt
		if len(call.Steps) != 0 {
			outputs, err := json.Marshal(call.Steps)
			if err != nil {
				return runs.Reply{}, err
			}
			prompt += "\n\nPrevious step results (data):\n" + string(outputs)
		}
		reply, err := runs.CompleteAgent(ctx, spend.Wrap(model, client), system, prompt)
		result := runs.Reply{Cost: recorded(reply)}
		if reply != nil {
			result.Output = reply.Text()
		}
		var stopped session.ErrSpendStopped
		if errors.As(err, &stopped) && stopped.TaskLimit {
			err = runs.ErrBudget
		}
		return result, err
	}
}

func appRunAsk(ask string) bool { return strings.HasPrefix(ask, appRunAskPrefix) }

func (b *appBackend) openRunApproval(r runs.Run) {
	gate := r.Approval
	id := appRunAskPrefix + gate.ID
	title := "Approval needed: " + gate.Kind
	detail := gate.Tool
	if len(gate.Args) > 0 {
		args, _ := json.Marshal(gate.Args)
		detail += "\n" + string(args)
	}
	blocking := true
	kind := appwire.SubkindPermission
	options := []appwire.AskOption{{ID: appwire.OptionAllowOnce, Label: "Approve"}, {ID: appwire.OptionDeny, Label: "Decline"}}
	if gate.Kind == "budget" {
		kind = appwire.SubkindBudget
		title = "Per-run budget reached"
		detail = "Enter a new budget in dollars to continue."
		options = []appwire.AskOption{{ID: appwire.OptionRaise, Label: "Raise budget"}, {ID: appwire.OptionStop, Label: "Stop"}}
	}
	b.emit.Emit(appwire.EventAskOpened, appwire.AskOpened{Ask: appwire.Ask{
		ID: id, Agent: appRunAskAgent, Kind: "approval", Subkind: kind,
		Title: title, Detail: detail, Path: []string{r.App, r.Run, r.Step}, Blocking: &blocking,
		Irreversible: gate.Kind == "send",
		Options:      options,
	}})
}

func (b *appBackend) checkRunAnswer(agent, ask, option, text string) error {
	if agent != appRunAskAgent || b.appRuns == nil {
		return appwire.Errorf(appwire.CodeNotFound, "run approval not found")
	}
	approval := strings.TrimPrefix(ask, appRunAskPrefix)
	runID, _, _ := strings.Cut(approval, "-")
	r, err := b.appRuns.Get(runID)
	if err != nil || r.Approval == nil || r.Approval.ID != approval {
		return appwire.Errorf(appwire.CodeNotFound, "run approval not found")
	}
	if r.Approval.Kind == "budget" {
		switch option {
		case appwire.OptionStop:
			return nil
		case appwire.OptionRaise:
			dollars, err := strconv.ParseFloat(strings.TrimSpace(text), 64)
			if err == nil && dollars > r.Cost {
				return nil
			}
			return appwire.Errorf(appwire.CodeBadParams, "enter a new budget in dollars above the run's cost")
		default:
			return appwire.Errorf(appwire.CodeBadParams, "choose Raise budget or Stop")
		}
	}
	if option != appwire.OptionAllowOnce && option != appwire.OptionDeny {
		return appwire.Errorf(appwire.CodeBadParams, "choose Approve or Decline")
	}
	return nil
}

func (b *appBackend) answerRunApproval(agent, ask, option, text string) error {
	if err := b.checkRunAnswer(agent, ask, option, text); err != nil {
		return err
	}
	approval := strings.TrimPrefix(ask, appRunAskPrefix)
	runID, _, _ := strings.Cut(approval, "-")
	var err error
	if option == appwire.OptionRaise {
		dollars, _ := strconv.ParseFloat(strings.TrimSpace(text), 64)
		err = b.appRuns.RaiseBudget(runID, approval, dollars)
	} else {
		err = b.appRuns.Answer(runID, approval, option == appwire.OptionAllowOnce)
	}
	if err != nil {
		return appRunError(err)
	}
	b.closeRunInbox(agent, ask, option)
	return nil
}

func (b *appBackend) closeRunInbox(agent, ask, option string) {
	now := b.now()
	b.emit.Emit(appwire.EventAskClosed, appwire.AskClosed{Agent: agent, ID: ask, At: now, Option: option, By: appwire.ClosedByApp})
	b.inboxResolved(appInboxID(agent, ask), appwire.Entry{ID: "r" + ask})
}
