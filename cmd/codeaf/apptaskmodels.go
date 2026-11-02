package main

// ── codeaf app: TASK MODELS (V79) ───────────────────────────────────────────
//
// settings.models.get, .list and .set, and settings.changed's task_models. A
// THIN DOOR: the policy — the mode, the pins, the per-provider ticks, every
// provider's own price — is internal/config's, read and written there; this
// file turns it into the wire's shapes and back, and adds only what the app
// process alone knows: a key's last four characters and which fixes this
// engine can start.

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/crewroute"
	"github.com/Agent-Field/codeaf/internal/roles"
)

// taskModelsReadWindow bounds one read of the task models, the competence
// map's bound: a person is waiting on the page.
const taskModelsReadWindow = 10 * time.Second

// taskModelsRead reads the whole configuration inside the read window.
func taskModelsRead(profile string) (config.TaskModelsState, []config.TaskModelRow, error) {
	ctx, cancel := context.WithTimeout(context.Background(), taskModelsReadWindow)
	defer cancel()
	state, rows, err := config.TaskModelsAt(ctx, profile)
	if err != nil {
		return state, nil, taskModelsError(err)
	}
	return state, rows, nil
}

// taskModelsError is a config error in the wire's words.
func taskModelsError(err error) error {
	var refusal config.TaskModelsRefusal
	switch {
	case errors.As(err, &refusal):
		return appwire.Errorf(appwire.CodeBadParams, "%s", refusal.Reason)
	case errors.Is(err, config.ErrTaskModelsStale):
		return appwire.Errorf(appwire.CodeConflict, "%s", err.Error())
	case errors.Is(err, config.ErrTaskModelsRead):
		return appwire.Errorf(appwire.CodeEngine, "task models read timed out")
	case errors.Is(err, config.ErrTaskModelsMoving):
		return appwire.Errorf(appwire.CodeBusy, "%s", err.Error())
	}
	return appEngineError(err)
}

// TaskModels is settings.models.get.
func (b *appBackend) TaskModels() (appwire.TaskModels, error) {
	profile := config.ProfileDir()
	state, _, err := taskModelsRead(profile)
	if err != nil {
		return appwire.TaskModels{}, err
	}
	return b.taskModelsWire(profile, state), nil
}

// taskModelsWire is the configuration as the wire carries it.
func (b *appBackend) taskModelsWire(profile string, state config.TaskModelsState) appwire.TaskModels {
	out := appwire.TaskModels{
		// READ_AT IS WHEN THE READ BEGAN ([config.TaskModelsState.ObservedAt]),
		// never when it finished: a slow read that started first must not
		// outrank a read or a write that started after it.
		ReadAt: state.ObservedAt.UTC(), Revision: state.Revision, Mode: appwire.TaskModelsAuto,
		Providers: make([]appwire.TaskModelsProvider, 0, len(state.Providers)),
	}
	if state.Mode == crewroute.ModeOne {
		out.Mode = appwire.TaskModelsOne
	}
	if state.One != nil {
		out.OneModel = &appwire.ModelRef{ID: state.One.Model, Provider: state.One.Provider}
		out.OneModelWarning = wireWord(state.OneWarning)
	}
	view := func(r config.TaskModelRole) appwire.TaskModelRole {
		view := appwire.TaskModelRole{Warning: wireWord(r.Warning), EligibleCount: r.Eligible}
		if r.Pin != nil {
			view.Pin = &appwire.ModelRef{ID: r.Pin.Model, Provider: r.Pin.Provider}
		}
		if r.Default != nil {
			view.Default = &appwire.ModelRef{ID: r.Default.Model, Provider: r.Default.Provider}
		}
		return view
	}
	auxiliary := view(state.Auxiliary)
	out.Roles = appwire.TaskModelRoles{Worker: view(state.Roles[crewroute.Worker]), Planner: view(state.Roles[crewroute.Planner]),
		Checker: view(state.Roles[crewroute.Checker]), Auxiliary: &auxiliary}
	for _, key := range appwire.InsightRoleKeys {
		row := view(state.Insight[roles.Role(key)])
		if family, ok := roles.FamilyOf(roles.Role(key)); ok {
			row.Follows = string(family)
		}
		*out.Roles.InsightRow(key) = &row
	}
	sources := config.ResolveSources(profile, config.APIKeyAt(profile), config.DefaultBaseURL)
	for _, p := range state.Providers {
		view := appwire.TaskModelsProvider{
			ID: p.ID, Name: p.Name, State: p.State, Reason: p.Reason, Fix: appwire.FixNone,
			ModelCount: p.Models, AllowedCount: p.Allowed,
		}
		if service, ok := sources.ByID(p.ID); ok && p.Connected {
			view.KeyHint = appKeyHint(service.Key)
		}
		view.Fix = b.taskModelsFix(p)
		out.Providers = append(out.Providers, view)
	}
	return out
}

// taskModelsFix is the one fix THIS ENGINE CAN START for a provider: a browser
// sign-in only where providers.sign_in serves one, a key only where a pasted
// key connects it — never for the ChatGPT sign-in, which takes no key, nor an
// address — and nothing otherwise: a plan whose sign-in ran out is signed in
// again where it was made, which is not here.
func (b *appBackend) taskModelsFix(p config.TaskModelProvider) string {
	connectable, signIn := false, false
	if provider, ok := b.appProviderNamed(p.ID); ok {
		connectable, signIn = provider.kind == appwire.ProviderKey, provider.signIn
	}
	switch {
	case p.State == config.ProviderStateNoKey && connectable:
		return appwire.FixAddKey
	case p.State == config.ProviderStateExpired && p.Reason == config.ProviderReasonKeyRefused && signIn:
		return appwire.FixSignIn
	case p.State == config.ProviderStateExpired && p.Reason == config.ProviderReasonKeyRefused && connectable:
		return appwire.FixAddKey
	}
	return appwire.FixNone
}

// wireWord is a warning as the wire carries it: nil for none.
func wireWord(word string) *string {
	if word == "" {
		return nil
	}
	return &word
}

// TaskModelsList is settings.models.list: one page of rows, filtered.
func (b *appBackend) TaskModelsList(p appwire.TaskModelsListParams) (appwire.TaskModelsPage, error) {
	if p.Limit <= 0 || p.Limit > appwire.TaskModelsPageMax {
		// The wire bounds a request before it gets here; a caller in this
		// process gets the same bounds.
		p.Limit = appwire.TaskModelsPageDefault
	}
	profile := config.ProfileDir()
	state, rows, err := taskModelsRead(profile)
	if err != nil {
		return appwire.TaskModelsPage{}, err
	}
	q := strings.ToLower(p.Q)
	var matched []config.TaskModelRow
	for _, row := range rows {
		switch {
		case p.Provider != "" && row.Provider != p.Provider:
			continue
		case p.AllowedOnly && !row.Allowed:
			continue
		case p.Role != "" && !row.Seats[crewroute.Seat(p.Role)]:
			continue
		case q != "" && !strings.Contains(strings.ToLower(row.Model.ID), q) && !strings.Contains(strings.ToLower(row.Name), q):
			continue
		}
		matched = append(matched, row)
	}
	fingerprint := taskModelsFingerprint(p, state.Revision, len(rows))
	start := 0
	if p.Cursor != "" {
		if start, err = taskModelsCursorAt(p.Cursor, fingerprint); err != nil || start > len(matched) {
			return appwire.TaskModelsPage{}, appwire.Errorf(appwire.CodeBadParams, "cursor is stale or not from this list · list again from the start")
		}
	}
	end := min(start+p.Limit, len(matched))
	page := appwire.TaskModelsPage{
		ReadAt: state.ObservedAt.UTC(), Revision: state.Revision, Total: len(matched),
		Models: make([]appwire.TaskModelRow, 0, end-start),
	}
	for _, row := range matched[start:end] {
		page.Models = append(page.Models, taskModelRowWire(row))
	}
	if end < len(matched) {
		page.NextCursor = taskModelsCursor(end, fingerprint)
	}
	return page, nil
}

// taskModelRowWire is one row as the wire carries it.
func taskModelRowWire(row config.TaskModelRow) appwire.TaskModelRow {
	view := appwire.TaskModelRow{
		ID: row.Model.ID, Provider: row.Provider, Name: row.Name, Billing: string(row.Kind),
		Allowed: row.Allowed, Available: row.Available, AutoEligible: row.AutoEligible, Reason: string(row.Reason),
	}
	if view.Name == "" {
		view.Name = crewroute.ShortModel(row.Model.ID)
	}
	if row.Price != nil {
		view.Price = &appwire.TaskModelPrice{InPerM: row.Price.InPerM, OutPerM: row.Price.OutPerM}
	}
	return view
}

// taskModelsFingerprint names the list a cursor belongs to: its filters, the
// revision and how many rows there were.
func taskModelsFingerprint(p appwire.TaskModelsListParams, revision string, rows int) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{p.Provider, p.Q, p.Role, strconv.FormatBool(p.AllowedOnly), revision, strconv.Itoa(rows)}, "\x00")))
	return hex.EncodeToString(sum[:8])
}

type taskModelsCursorWire struct {
	At int    `json:"a"`
	F  string `json:"f"`
}

func taskModelsCursor(at int, fingerprint string) string {
	raw, _ := json.Marshal(taskModelsCursorWire{At: at, F: fingerprint})
	return base64.RawURLEncoding.EncodeToString(raw)
}

func taskModelsCursorAt(cursor, fingerprint string) (int, error) {
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return 0, err
	}
	var c taskModelsCursorWire
	if err := json.Unmarshal(raw, &c); err != nil || c.F != fingerprint || c.At <= 0 {
		return 0, errors.New("stale cursor")
	}
	return c.At, nil
}

// SetTaskModels is settings.models.set: the patch as one checked change, one
// write, and settings.changed with what everything reads now.
func (b *appBackend) SetTaskModels(p appwire.TaskModelsPatch) (appwire.TaskModels, error) {
	profile := config.ProfileDir()
	change := config.TaskModelsChange{Revision: p.Revision}
	if p.Mode != nil {
		mode := crewroute.ModeAuto
		if *p.Mode == appwire.TaskModelsOne {
			mode = crewroute.ModeOne
		}
		change.Mode = &mode
	}
	if p.OneModel != nil {
		change.One = &config.CrewPin{Model: p.OneModel.ID, Provider: p.OneModel.Provider}
	}
	if p.Pins != nil {
		change.Pins = map[crewroute.Seat]*config.CrewPin{}
		for role, ref := range p.Pins {
			if slices.Contains(appwire.InsightRoleKeys, role) {
				if change.RolePins == nil {
					change.RolePins = map[roles.Role]*config.CrewPin{}
				}
				change.RolePins[roles.Role(role)] = nil
				if ref != nil {
					change.RolePins[roles.Role(role)] = &config.CrewPin{Model: ref.ID, Provider: ref.Provider}
				}
				continue
			}
			if role == appwire.RoleAuxiliary {
				change.AuxiliarySet = true
				if ref != nil {
					change.Auxiliary = &config.CrewPin{Model: ref.ID, Provider: ref.Provider}
				}
				continue
			}
			if ref == nil {
				change.Pins[crewroute.Seat(role)] = nil
				continue
			}
			change.Pins[crewroute.Seat(role)] = &config.CrewPin{Model: ref.ID, Provider: ref.Provider}
		}
	}
	for _, row := range p.Allowed {
		change.Ticks = append(change.Ticks, config.TaskModelTick{Model: row.Model.ID, Provider: row.Model.Provider, Allowed: row.Allowed})
	}
	change.Served = b.servedModel()
	ctx, cancel := context.WithTimeout(context.Background(), taskModelsReadWindow)
	defer cancel()
	if err := config.ApplyTaskModels(ctx, profile, change); err != nil {
		return appwire.TaskModels{}, taskModelsError(err)
	}
	got, err := b.TaskModels()
	if err != nil {
		return appwire.TaskModels{}, err
	}
	if settings, err := b.Settings(); err == nil {
		// The checker and planner pins are settings' Checks and Planner rows,
		// so the settings ride the same event.
		b.emit.Emit(appwire.EventSettingsChanged, appwire.SettingsChanged{Settings: settings, TaskModels: &got})
	}
	return got, nil
}

// servedModel answers whether models.list offers a model on a provider, so
// the list a person picks from and the check on what they picked are one
// source. It is read once, and only when a pin is not among the crew's rows.
// With no process open there is no list yet, and nil leaves the rows to
// decide alone rather than opening one for a settings write.
func (b *appBackend) servedModel() func(model, provider string) (string, bool) {
	if openedEngineProcess() == nil {
		return nil
	}
	listed := sync.OnceValue(func() []appwire.Model {
		models, _ := b.ListModels()
		return models
	})
	return func(model, provider string) (string, bool) {
		provider = strings.ToLower(strings.TrimSpace(provider))
		for _, m := range listed() {
			if provider != "" && !strings.EqualFold(m.Provider, provider) {
				continue
			}
			if strings.EqualFold(m.ID, model) || (provider != "" && strings.EqualFold(m.ID, provider+"/"+model)) {
				return m.ID, true
			}
		}
		return "", false
	}
}

// settingsChanged says settings.changed with the settings and, when the
// change touched a task-models row, the task models as they read now. A
// task-models read that fails leaves the field out — the app keeps what it
// last read and asks again on its own — rather than sending a configuration
// that is not the profile's; the settings themselves are said either way.
func (b *appBackend) settingsChanged(settings appwire.Settings, taskModels bool) {
	changed := appwire.SettingsChanged{Settings: settings}
	if taskModels {
		if got, err := b.TaskModels(); err == nil {
			changed.TaskModels = &got
		}
	}
	b.emit.Emit(appwire.EventSettingsChanged, changed)
}
