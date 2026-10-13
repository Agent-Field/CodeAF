package runs

import (
	"context"
	"os"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

func (e *engine) listen() {
	if e.all == nil || e.all.Hub == nil {
		close(e.ready)
		return
	}
	events, cancel := e.all.Hub.Subscribe("app/*")
	close(e.ready)
	defer cancel()
	for {
		select {
		case <-e.ctx.Done():
			return
		case ev, ok := <-events:
			if !ok {
				return
			}
			// Drive can publish more record diffs. Reading stays on this
			// goroutine's caller so a full hub buffer cannot deadlock it.
			e.launch(func() { e.onEvent(e.ctx, ev) })
		}
	}
}

func (e *engine) onEvent(ctx context.Context, ev svc.Event) {
	parts := strings.Split(ev.Topic, "/")
	if len(parts) != 4 || parts[0] != "app" || parts[2] != "records" {
		return
	}
	diff, ok := recordDiff(ev.Data)
	if !ok {
		return
	}
	e.handleRecord(ctx, parts[1], diff)
}

func recordDiff(data any) (bridge.RecordDiff, bool) {
	switch typed := data.(type) {
	case bridge.RecordDiff:
		return typed, true
	case *bridge.RecordDiff:
		if typed == nil {
			return bridge.RecordDiff{}, false
		}
		return *typed, true
	default:
		return bridge.RecordDiff{}, false
	}
}

func (e *engine) snapshot(ctx context.Context) {
	if e.all == nil || e.all.Store == nil || e.all.Records == nil {
		return
	}
	apps, err := e.all.Store.List(ctx, svc.Caller{Member: svc.Owner, Role: svc.RoleEditor})
	if err != nil {
		return
	}
	for _, app := range apps {
		bundle, err := e.bundle(ctx, app.ID)
		if err != nil || bundle.App == nil {
			continue
		}
		scope := e.scopeFor(ctx, app.ID, app.State)
		for recordType := range bundle.App.Records {
			page, err := e.all.Records.Query(ctx, scope, bridge.QueryParams{Type: recordType})
			if err != nil {
				continue
			}
			e.mu.Lock()
			for _, row := range page.Rows {
				e.seen[seenKey(app.ID, recordType, row.ID)] = copyFields(row.Fields)
			}
			e.mu.Unlock()
		}
	}
}

func (e *engine) handleRecord(ctx context.Context, app string, diff bridge.RecordDiff) {
	bundle, err := e.bundle(ctx, app)
	if err != nil || bundle.App == nil {
		return
	}
	created, deleted, changed := e.observe(app, diff)
	for _, trigger := range bundle.App.Triggers {
		if !triggerMatches(trigger, diff, created, deleted) {
			continue
		}
		if trigger.When != "" && !e.when(trigger.When, diff.Record, changed) {
			continue
		}
		e.fire(ctx, app, trigger, diff)
	}
}

func (e *engine) observe(app string, diff bridge.RecordDiff) (created, deleted bool, changed map[string]bool) {
	key := seenKey(app, diff.Type, diff.ID)
	e.mu.Lock()
	defer e.mu.Unlock()
	prev := e.seen[key]
	if diff.Op == bridge.DiffDelete {
		delete(e.seen, key)
		return false, true, map[string]bool{}
	}
	fields := map[string]any{}
	if diff.Record != nil {
		fields = copyFields(diff.Record.Fields)
	}
	changed = map[string]bool{}
	if prev == nil {
		for name := range fields {
			changed[name] = true
		}
		created = diff.Record == nil || diff.Record.Version <= 1
	} else {
		for name, value := range fields {
			if asText(prev[name]) != asText(value) {
				changed[name] = true
			}
		}
		for name := range prev {
			if _, ok := fields[name]; !ok {
				changed[name] = true
			}
		}
	}
	e.seen[key] = fields
	return created, false, changed
}

func triggerMatches(trigger spec.Trigger, diff bridge.RecordDiff, created, deleted bool) bool {
	if trigger.Type != "" && trigger.Type != diff.Type {
		return false
	}
	switch trigger.On {
	case spec.TriggerRecordCreated:
		return created && !deleted
	case spec.TriggerRecordChanged, "record.updated":
		return !created && !deleted
	case spec.TriggerRecordDeleted:
		return deleted
	default:
		return false
	}
}

func (e *engine) when(expr string, row *bridge.Record, changed map[string]bool) bool {
	env := map[string]any{"_changed": changed}
	if row != nil {
		env["id"] = row.ID
		env["record"] = map[string]any{"id": row.ID, "type": row.Type}
		for key, value := range row.Fields {
			env[key] = value
		}
	}
	value, err := eval(expr, env)
	return err == nil && truthy(value)
}

func (e *engine) fire(ctx context.Context, app string, trigger spec.Trigger, diff bridge.RecordDiff) {
	scope := e.scopeFor(ctx, app, "")
	env := map[string]any{
		"me":     scope.Caller.Member,
		"record": map[string]any{"id": diff.ID, "type": diff.Type},
	}
	if diff.Record != nil {
		env["record"] = map[string]any{"id": diff.Record.ID, "type": diff.Record.Type}
		for key, value := range diff.Record.Fields {
			env[key] = value
		}
	}
	params, _ := resolveValue(trigger.Params, env).(map[string]any)
	_, _ = e.start(ctx, scope, bridge.StartRunParams{Run: trigger.Run, Params: params}, trigger.ID)
}

func (e *engine) scopeFor(ctx context.Context, app string, state bridge.AppState) svc.Scope {
	scope := svc.Scope{App: app, Caller: svc.Caller{Member: svc.Owner, Role: svc.RoleEditor}, Data: svc.DataSample}
	if state == "" && e.all != nil && e.all.Store != nil {
		detail, err := e.all.Store.Get(ctx, app)
		if err == nil {
			state = detail.State
		}
	}
	if state == bridge.StateKept {
		scope.Data = svc.DataReal
	}
	return scope
}

func (e *engine) fireSchedules(ctx context.Context) {
	if e.all == nil || e.all.Store == nil {
		return
	}
	apps, err := e.all.Store.List(ctx, svc.Caller{Member: svc.Owner, Role: svc.RoleEditor})
	if err != nil {
		return
	}
	now := e.clock()
	for _, app := range apps {
		bundle, err := e.bundle(ctx, app.ID)
		if err != nil {
			continue
		}
		for _, trigger := range bundle.App.Triggers {
			if trigger.On != spec.TriggerSchedule || trigger.Cron == "" {
				continue
			}
			key := app.ID + "/" + trigger.ID
			e.mu.Lock()
			last := e.lastFire[key]
			e.mu.Unlock()
			if !due(trigger.Cron, now, last) {
				continue
			}
			e.rememberFire(key, now)
			scope := e.scopeFor(ctx, app.ID, app.State)
			_, _ = e.start(ctx, scope, bridge.StartRunParams{Run: trigger.Run, Params: trigger.Params}, trigger.ID)
		}
	}
}

func (e *engine) resumeRunning() {
	for _, app := range e.appIDs() {
		jobs, err := e.loadJobs(app)
		if err != nil {
			continue
		}
		for _, job := range jobs {
			if job.State.Status == bridge.RunRunning {
				e.launch(func() { e.drive(e.ctx, app, job.State.ID) })
			}
		}
	}
}

func (e *engine) resumeDue(ctx context.Context) {
	now := e.clock()
	for _, app := range e.appIDs() {
		jobs, err := e.loadJobs(app)
		if err != nil {
			continue
		}
		for _, job := range jobs {
			if job.State.Status != bridge.RunWaiting || job.WaitKind == "gate" || job.WaitKind == "" {
				continue
			}
			if now.Before(job.WaitUntil) {
				continue
			}
			e.launch(func() { e.drive(ctx, app, job.State.ID) })
		}
	}
}

func (e *engine) appIDs() []string {
	entries, err := os.ReadDir(e.root)
	if err != nil {
		return nil
	}
	var ids []string
	for _, entry := range entries {
		if entry.IsDir() {
			ids = append(ids, entry.Name())
		}
	}
	return ids
}

func seenKey(app, recordType, id string) string {
	return app + "/" + recordType + "/" + id
}

func copyFields(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
