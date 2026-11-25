package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/schema"
	"github.com/Agent-Field/codeaf/internal/apps/store"
	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/connect"
	"github.com/Agent-Field/codeaf/internal/exec/bare"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/session"
)

type appAssistantCall func(context.Context, json.RawMessage) (any, error)

type appAssistantBelt struct {
	scope *session.AppScope
	load  func() (schema.App, store.Definition, error)
}

func (b *appBackend) appAssistantTools(app schema.App, def store.Definition, manager *connect.Manager) *session.AppScope {
	belt := appAssistantBelt{
		scope: &session.AppScope{ID: def.Meta.ID, ApprovalTools: map[string]bool{}},
		load: func() (schema.App, store.Definition, error) {
			return b.appAssistantDefinition(def.Meta.ID)
		},
	}
	if data, ok := any(b).(appAssistantData); ok {
		belt.records(app, data)
	}
	belt.connectors(app, manager)
	if len(def.Runs) != 0 && appwire.HasAppAssistantMethod("runs.start") {
		belt.add("runs_start", "Start an automation in this app.", false, `{"run":%s,"params":{"type":"object"}}`, []string{"run"}, appEnum(def.Runs),
			func(ctx context.Context, raw json.RawMessage) (any, error) {
				var args struct {
					Run    string         `json:"run"`
					Params map[string]any `json:"params,omitempty"`
				}
				if err := appAssistantDecode(raw, &args); err != nil {
					return nil, err
				}
				_, live, err := belt.load()
				if err != nil {
					return nil, err
				}
				if !slices.Contains(live.Runs, args.Run) {
					return nil, errors.New("This app has no automation with that name.")
				}
				return b.appAssistantMethod("runs.start", map[string]any{"app": def.Meta.ID, "run": args.Run, "params": args.Params})
			})
	}
	if appwire.HasAppAssistantMethod("apps.change") && appwire.HasAppAssistantMethod("apps.build_state") {
		belt.add(session.AppChangeToolName, "Prepare a change when the person explicitly asks to change the app itself. Call before looking up data. The engine uses the person's exact words.", false, "{}", nil, "",
			func(ctx context.Context, raw json.RawMessage) (any, error) {
				if err := appAssistantDecode(raw, &struct{}{}); err != nil {
					return nil, err
				}
				request, ok := session.AppChangeRequest(ctx)
				if !ok {
					return nil, errors.New("A change needs a fresh request from the person, before looking up records or messages.")
				}
				if _, _, err := belt.load(); err != nil {
					return nil, err
				}
				return b.proposeAppChange(def, request)
			})
	}
	return belt.scope
}

// A tool's name only grants the operation in its closure. Every closure checks
// the current app and binds the app ID before crossing into another engine lane.
func (b *appAssistantBelt) add(name, description string, approval bool, properties string, required []string, enum string, call appAssistantCall) {
	if enum != "" {
		properties = fmt.Sprintf(properties, enum)
	}
	if required == nil {
		required = []string{}
	}
	fields, _ := json.Marshal(required)
	b.scope.Tools = append(b.scope.Tools, bare.Tool{
		Name: name, Description: description,
		Schema: json.RawMessage(fmt.Sprintf(`{"type":"object","properties":%s,"required":%s,"additionalProperties":false}`, properties, fields)),
		Execute: func(ctx context.Context, raw json.RawMessage) (string, bool, error) {
			if err := ctx.Err(); err != nil {
				return "", true, err
			}
			out, err := call(ctx, raw)
			if err != nil {
				return "", true, err
			}
			result, err := json.Marshal(out)
			return string(result), err != nil, err
		},
	})
	if approval {
		b.scope.ApprovalTools[name] = true
	}
}

func appAssistantDecode(raw json.RawMessage, dst any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if !json.Valid(raw) || !strings.HasPrefix(strings.TrimSpace(string(raw)), "{") {
		return errors.New("The tool needs a JSON object.")
	}
	return decoder.Decode(dst)
}

func appEnum(values []string) string {
	raw, _ := json.Marshal(values)
	return `{"type":"string","enum":` + string(raw) + `}`
}

func (b *appBackend) appAssistantMethod(name string, fields map[string]any) (any, error) {
	raw, err := json.Marshal(fields)
	if err != nil {
		return nil, err
	}
	return appwire.CallAppAssistantMethod(b, name, raw)
}

type appAssistantMessage struct {
	ID         string `json:"id"`
	Role       string `json:"role"`
	Text       string `json:"text"`
	Checkpoint string `json:"checkpoint"`
}

type appAssistantBuild struct {
	Draft       string                `json:"draft"`
	App         string                `json:"app"`
	Revision    int                   `json:"revision"`
	State       string                `json:"state"`
	Transcript  []appAssistantMessage `json:"transcript"`
	Checkpoints []struct {
		ID      string `json:"id"`
		Summary string `json:"summary"`
	} `json:"checkpoints"`
}

func (b *appBackend) appAssistantBuild(name string, args map[string]any) (appAssistantBuild, error) {
	var build appAssistantBuild
	err := b.appAssistantMethodInto(name, args, &build)
	return build, err
}

func (b *appBackend) appAssistantMethodInto(name string, args map[string]any, result any) error {
	value, err := b.appAssistantMethod(name, args)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, result)
}

func (b *appBackend) proposeAppChange(def store.Definition, request string) (appwire.AppChangeProposed, error) {
	app := def.Meta.ID
	job, err := b.appAssistantBuild("apps.change", map[string]any{"app": app, "request": ""})
	if err != nil {
		return appwire.AppChangeProposed{}, err
	}
	if job.Draft == "" {
		return appwire.AppChangeProposed{}, errors.New("The change job returned no ID.")
	}
	before, err := b.appAssistantBuild("apps.build_state", map[string]any{"draft": job.Draft})
	if err != nil {
		return appwire.AppChangeProposed{}, err
	}
	if before.App != app || before.Draft != job.Draft {
		return appwire.AppChangeProposed{}, errors.New("The editing job could not be opened for this app.")
	}
	started, err := b.appAssistantBuild("apps.change", map[string]any{"app": app, "request": request})
	if err != nil {
		return appwire.AppChangeProposed{}, err
	}
	if started.Draft != job.Draft {
		return appwire.AppChangeProposed{}, errors.New("The change opened a different editing job.")
	}
	pending := appwire.AppChangeProposed{App: app, Draft: job.Draft, SummaryLines: []string{}}
	guard.Go("apps/assistant-proposal", func() { b.followAppChange(pending, def, request, before) })
	return pending, nil
}

func (b *appBackend) followAppChange(card appwire.AppChangeProposed, prior store.Definition, request string, before appAssistantBuild) {
	// The builder owns the durable job and its progress. This follower only
	// reports the checkpoint answering the person's request.
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()
	timeout := time.NewTimer(3 * time.Minute)
	defer timeout.Stop()
	for {
		state, err := b.appAssistantBuild("apps.build_state", map[string]any{"draft": card.Draft})
		if err == nil {
			checkpoint, show := appAssistantChangeCard(&card, state, request, before)
			if show {
				if checkpoint != "" {
					if screen := b.appAssistantChangedScreen(card, checkpoint, prior); screen != "" {
						card.Kind, card.Screen = "small", screen
					}
					current, err := b.appAssistantBuild("apps.build_state", map[string]any{"draft": card.Draft})
					if err != nil || current.Revision != state.Revision {
						continue
					}
				}
				b.emit.Emit(appwire.EventAppChangeProposed, card)
				return
			}
		}
		select {
		case <-tick.C:
		case <-timeout.C:
			return
		case <-b.left:
			return
		}
	}
}

func appAssistantChangeCard(card *appwire.AppChangeProposed, state appAssistantBuild, request string, before appAssistantBuild) (string, bool) {
	if state.Draft != card.Draft || state.App != card.App || state.Revision <= before.Revision {
		return "", false
	}
	var matched bool
	var checkpointID string
	for _, msg := range slices.Backward(state.Transcript) {
		if msg.Role == "assistant" && checkpointID == "" {
			checkpointID = msg.Checkpoint
		}
		if msg.Role == "user" {
			matched = msg.Text == request && !slices.ContainsFunc(before.Transcript, func(old appAssistantMessage) bool { return old.ID == msg.ID })
			break
		}
	}
	if !matched {
		return "", false
	}
	if state.State == "needs_you" || state.State == "failed" {
		card.Kind = "big"
		card.SummaryLines = []string{"Open this change to continue."}
		return "", true
	}
	if state.State != "ready" || len(state.Checkpoints) == 0 || checkpointID == "" {
		return "", false
	}
	checkpoint := state.Checkpoints[len(state.Checkpoints)-1]
	if checkpoint.ID != checkpointID {
		return "", false
	}
	if checkpoint.Summary != "" {
		card.SummaryLines = []string{checkpoint.Summary}
	}
	return checkpoint.ID, true
}

// A small change keeps the screen list and alters one existing screen. The
// checkpoint's screen list is its full preview, so compare the screen files.
func (b *appBackend) appAssistantChangedScreen(card appwire.AppChangeProposed, checkpoint string, prior store.Definition) string {
	var preview struct {
		App     json.RawMessage `json:"app"`
		Scope   string          `json:"scope"`
		Screens []string        `json:"screens"`
	}
	err := b.appAssistantMethodInto("apps.preview", map[string]any{
		"draft": card.Draft, "checkpoint": checkpoint,
	}, &preview)
	if err != nil || preview.Scope == "" || !slices.Equal(preview.Screens, prior.Screens) || !appAssistantSmallDefinition(prior.App, preview.App) {
		return ""
	}
	changed := ""
	for _, screen := range preview.Screens {
		before, found := prior.Screen(screen)
		if !found {
			return ""
		}
		var after struct {
			Screen json.RawMessage `json:"screen"`
		}
		if err := b.appAssistantMethodInto("apps.screen", map[string]any{
			"app": card.App, "screen": screen, "draft": preview.Scope,
		}, &after); err != nil || len(after.Screen) == 0 {
			return ""
		}
		var oldNode, newNode any
		if json.Unmarshal(before, &oldNode) != nil || json.Unmarshal(after.Screen, &newNode) != nil {
			return ""
		}
		if !reflect.DeepEqual(oldNode, newNode) {
			if changed != "" {
				return ""
			}
			changed = screen
		}
	}
	return changed
}

func appAssistantSmallDefinition(before, after json.RawMessage) bool {
	old, err := schema.ParseApp(before)
	if err != nil {
		return false
	}
	next, err := schema.ParseApp(after)
	if err != nil || len(old.Records) != len(next.Records) {
		return false
	}
	oldRecords, nextRecords := old.Records, next.Records
	old.Records, next.Records = nil, nil
	old.ID, next.ID, old.Version, next.Version = "", "", 0, 0
	if !reflect.DeepEqual(old, next) {
		return false
	}
	changedTypes := 0
	for id, prior := range oldRecords {
		current, found := nextRecords[id]
		if !found {
			return false
		}
		if !reflect.DeepEqual(prior, current) {
			changedTypes++
		}
	}
	return changedTypes <= 1
}
