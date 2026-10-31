package appwire

import (
	"encoding/json"
	"strings"
	"time"
	"unicode"
)

// V79: TASK MODELS — how one task gets its worker, planner and checker.
//
// These are the wire's shapes, and the ONLY shapes an app reads: the engine's
// own policy (the profile's rows, the router's candidates, the providers' own
// listings) is turned into these by the engine, and the app draws them. Nothing
// here is a budget — the per-task and per-day caps are settings.get's
// task_cap_usd and day_cap_usd, set through settings.set, and this method
// family never carries a second copy of either.
//
// A role is EXACTLY one of worker, planner, checker; a mode is auto or one.
// A change applies to tasks admitted after it: a task that already started
// keeps the picks it was admitted with.

// The modes, the roles, and the words a role's warning may be.
const (
	TaskModelsAuto = "auto"
	TaskModelsOne  = "one"

	RoleWorker  = "worker"
	RolePlanner = "planner"
	RoleChecker = "checker"
	// RoleAuxiliary is the model the small jobs beside a conversation run on:
	// titles, route questions, captions, fold-ups. It is not a task role and
	// Auto never picks it; unpinned, it is the build's own small-work model.
	// Engines that serve it say [FeatureTaskModelsAuxiliary] in the hello.
	RoleAuxiliary = "auxiliary"

	// WarnPinNotAllowed is a role pinned to a model the allowed models no
	// longer admit: the pin is kept, and the role runs on Auto until the
	// model is allowed again or the pin is removed.
	WarnPinNotAllowed = "pin_not_allowed"
	// WarnProviderUnavailable is a pin (or the one model) whose provider is
	// not connected, turned off, signed out, refused its key or out of credit.
	WarnProviderUnavailable = "provider_unavailable"
	// WarnPinUnknown is a saved pin naming a model no connected provider
	// lists. It is kept as written, never silently deleted.
	WarnPinUnknown = "pin_unknown"
	// WarnNoEligible is an unpinned role with no allowed model that can do
	// it: a task that needs it waits and asks.
	WarnNoEligible = "no_eligible"
)

// The provider states, the one fix the engine can offer, and why a provider
// is unavailable.
const (
	ProviderConnected   = "connected"
	ProviderExpired     = "expired"
	ProviderNoKey       = "no_key"
	ProviderUnavailable = "unavailable"

	FixNone   = "none"
	FixSignIn = "sign_in"
	FixAddKey = "add_key"
)

// The reasons a model row is not one Auto may pick, and how a row bills.
const (
	ModelNotAllowed          = "not_allowed"
	ModelNoPrice             = "no_price"
	ModelProviderUnavailable = "provider_unavailable"
	ModelRouteUnavailable    = "route_unavailable"
	ModelCannotDoRole        = "cannot_do_role"
)

// TaskModelsLister is an engine that serves Task models. It is asserted, as
// [Competencer] is, so an engine without it answers unknown_method.
type TaskModelsLister interface {
	TaskModels() (TaskModels, error)
	TaskModelsList(p TaskModelsListParams) (TaskModelsPage, error)
	SetTaskModels(p TaskModelsPatch) (TaskModels, error)
}

// ModelRef names one model on one provider's route: the catalog id, and the
// provider id that route goes through. An empty provider is the model on
// whichever connected provider reaches it first.
type ModelRef struct {
	ID       string `json:"id"`
	Provider string `json:"provider,omitempty"`
}

// TaskModels is settings.models.get's answer, and settings.changed's
// task_models. It is small on purpose: provider headers and counts, never the
// model list, which is settings.models.list's, page by page.
type TaskModels struct {
	ReadAt time.Time `json:"read_at"`
	// Revision changes whenever any row behind this answer changes; a
	// settings.models.set that carries a revision is refused with conflict
	// when it no longer matches.
	Revision string `json:"revision"`
	Mode     string `json:"mode" enum:"auto,one"`
	// OneModel is the model one-model mode runs, kept while Auto is on so the
	// choice comes back; null when none was ever chosen.
	OneModel        *ModelRef            `json:"one_model"`
	OneModelWarning *string              `json:"one_model_warning" enum:"provider_unavailable,pin_unknown"`
	Roles           TaskModelRoles       `json:"roles"`
	Providers       []TaskModelsProvider `json:"providers"`
}

// TaskModelRoles is the three roles, each by its exact key.
type TaskModelRoles struct {
	Worker  TaskModelRole `json:"worker"`
	Planner TaskModelRole `json:"planner"`
	Checker TaskModelRole `json:"checker"`
	// Auxiliary is the small-jobs model ([RoleAuxiliary]): its pin, null for
	// the build's own default, and what is wrong with it. EligibleCount is how
	// many available models it could be pinned to. Absent from an engine that
	// does not serve it ([FeatureTaskModelsAuxiliary]).
	Auxiliary *TaskModelRole `json:"auxiliary,omitempty"`
}

// FeatureTaskModelsAuxiliary is the hello's word for an engine whose
// settings.models.get answers roles.auxiliary and whose settings.models.set
// takes pins.auxiliary.
const FeatureTaskModelsAuxiliary = "settings.models.auxiliary"

func withTaskModelsAuxiliaryFeature(h Hello, e Engine) Hello {
	if _, ok := e.(TaskModelsLister); !ok {
		return h
	}
	return withFeature(h, FeatureTaskModelsAuxiliary)
}

// TaskModelRole is one role: its saved pin (null is Auto), what is wrong with
// it, and how many allowed models Auto could pick for it right now.
type TaskModelRole struct {
	Pin           *ModelRef `json:"pin"`
	Warning       *string   `json:"warning" enum:"pin_not_allowed,provider_unavailable,pin_unknown,no_eligible"`
	EligibleCount int       `json:"eligible_count"`
}

// TaskModelsProvider is one provider's header. A provider with no key is
// listed with no models; a provider whose key or sign-in stopped working keeps
// its models listed (and ticked) and says why, with the one fix the engine can
// start. KeyHint is the key's last four characters and nothing more.
type TaskModelsProvider struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	State        string `json:"state" enum:"connected,expired,no_key,unavailable"`
	Fix          string `json:"fix" enum:"none,sign_in,add_key"`
	Reason       string `json:"reason,omitempty" enum:"key_refused,signed_out,out_of_credit,turned_off"`
	KeyHint      string `json:"key_hint,omitempty"`
	ModelCount   int    `json:"model_count"`
	AllowedCount int    `json:"allowed_count"`
}

// TaskModelsListParams is settings.models.list's params. Limit is 40 when
// absent and at most 100; q is at most 100 characters; a cursor is only good
// for the same filters and the same revision it came from.
type TaskModelsListParams struct {
	Provider    string `json:"provider,omitempty"`
	Q           string `json:"q,omitempty"`
	AllowedOnly bool   `json:"allowed_only,omitempty"`
	Role        string `json:"role,omitempty" enum:"worker,planner,checker"`
	Cursor      string `json:"cursor,omitempty"`
	Limit       int    `json:"limit,omitempty"`
}

// The list's bounds, spelled once.
const (
	TaskModelsPageDefault = 40
	TaskModelsPageMax     = 100
	TaskModelsQueryMax    = 100
	TaskModelsAllowedMax  = 200
	ModelRefIDMax         = 200
	ModelRefProviderMax   = 64
)

// TaskModelsPage is one page of model rows, providers in the header's order,
// then each provider's models in its own listing's order.
type TaskModelsPage struct {
	ReadAt     time.Time      `json:"read_at"`
	Revision   string         `json:"revision"`
	Models     []TaskModelRow `json:"models"`
	Total      int            `json:"total"`
	NextCursor string         `json:"next_cursor,omitempty"`
}

// TaskModelRow is one model on one provider. Price is that PROVIDER'S OWN
// published price per million tokens, and null when that provider published
// none — never another provider's figure, never an estimate. Allowed is the
// row's tick; Available is whether the route answers right now; AutoEligible
// is whether Auto may pick it today, and Reason says why not.
type TaskModelRow struct {
	ID           string          `json:"id"`
	Provider     string          `json:"provider"`
	Name         string          `json:"name"`
	Billing      string          `json:"billing" enum:"metered,plan,local,free"`
	Allowed      bool            `json:"allowed"`
	Available    bool            `json:"available"`
	AutoEligible bool            `json:"auto_eligible"`
	Price        *TaskModelPrice `json:"price"`
	Reason       string          `json:"reason,omitempty" enum:"not_allowed,no_price,provider_unavailable,route_unavailable,cannot_do_role"`
}

// TaskModelPrice is dollars per million tokens in and out.
type TaskModelPrice struct {
	InPerM  float64 `json:"in_per_m"`
	OutPerM float64 `json:"out_per_m"`
}

// TaskModelsPatch is settings.models.set's params as the engine reads them:
// every part optional, a pin present as null is Auto. Every part is checked
// before anything is written, and the whole patch is one write.
type TaskModelsPatch struct {
	Revision string
	Mode     *string
	OneModel *ModelRef
	// Pins holds a role only when the patch names it; a nil value is Auto.
	Pins    map[string]*ModelRef
	Allowed []TaskModelAllowed
}

// TaskModelAllowed is one row ticked or unticked.
type TaskModelAllowed struct {
	Model   ModelRef `json:"model"`
	Allowed bool     `json:"allowed"`
}

// TaskModelsPatchWire is settings.models.set's params as they travel.
type TaskModelsPatchWire struct {
	Revision string             `json:"revision,omitempty"`
	Mode     *string            `json:"mode,omitempty" enum:"auto,one"`
	OneModel *ModelRef          `json:"one_model,omitempty"`
	Pins     *TaskModelPinsWire `json:"pins,omitempty"`
	Allowed  []TaskModelAllowed `json:"allowed,omitempty"`
}

// TaskModelPinsWire is a patch's pins: a role left out is unchanged, a role
// sent as null is Auto.
type TaskModelPinsWire struct {
	Worker  *ModelRef `json:"worker,omitempty" nullable:"true"`
	Planner *ModelRef `json:"planner,omitempty" nullable:"true"`
	Checker *ModelRef `json:"checker,omitempty" nullable:"true"`
	// Auxiliary is the small-jobs model; null gives the build's default back.
	Auxiliary *ModelRef `json:"auxiliary,omitempty" nullable:"true"`
}

// TaskModelsResult is settings.models.get's and settings.models.set's answer.
type TaskModelsResult struct {
	TaskModels TaskModels `json:"task_models"`
}

func init() {
	methods["settings.models.get"] = typed(func(e Engine, _ struct{}) (*TaskModelsResult, error) {
		l, err := taskModelsLister(e, "settings.models.get")
		if err != nil {
			return nil, err
		}
		got, err := l.TaskModels()
		if err != nil {
			return nil, err
		}
		return &TaskModelsResult{got}, nil
	})
	methods["settings.models.list"] = typed(func(e Engine, p TaskModelsListParams) (*TaskModelsPage, error) {
		l, err := taskModelsLister(e, "settings.models.list")
		if err != nil {
			return nil, err
		}
		if p, err = checkTaskModelsList(p); err != nil {
			return nil, err
		}
		page, err := l.TaskModelsList(p)
		if err != nil {
			return nil, err
		}
		return &page, nil
	})
	methods["settings.models.set"] = shaped[TaskModelsPatchWire](func(e Engine, raw json.RawMessage) (*TaskModelsResult, error) {
		l, err := taskModelsLister(e, "settings.models.set")
		if err != nil {
			return nil, err
		}
		patch, err := decodeTaskModelsPatch(raw)
		if err != nil {
			return nil, err
		}
		got, err := l.SetTaskModels(patch)
		if err != nil {
			return nil, err
		}
		return &TaskModelsResult{got}, nil
	})
}

func taskModelsLister(e Engine, name string) (TaskModelsLister, error) {
	l, ok := e.(TaskModelsLister)
	if !ok {
		return nil, Errorf(CodeUnknownMethod, "this engine does not serve %s", name)
	}
	return l, nil
}

// TaskModelRoleKeys is the three role keys, in the order they are read out.
var TaskModelRoleKeys = []string{RoleWorker, RolePlanner, RoleChecker}

// checkTaskModelsList bounds a list request before the engine sees it.
func checkTaskModelsList(p TaskModelsListParams) (TaskModelsListParams, error) {
	switch {
	case p.Limit < 0:
		return p, Errorf(CodeBadParams, "limit is 1 to %d", TaskModelsPageMax)
	case p.Limit == 0:
		p.Limit = TaskModelsPageDefault
	case p.Limit > TaskModelsPageMax:
		return p, Errorf(CodeBadParams, "limit is at most %d", TaskModelsPageMax)
	}
	p.Q = strings.TrimSpace(p.Q)
	if len([]rune(p.Q)) > TaskModelsQueryMax {
		return p, Errorf(CodeBadParams, "q is at most %d characters", TaskModelsQueryMax)
	}
	if strings.IndexFunc(p.Q, unicode.IsControl) >= 0 {
		return p, Errorf(CodeBadParams, "q is one line of text")
	}
	if p.Role != "" && !taskModelRole(p.Role) {
		return p, Errorf(CodeBadParams, "role is worker, planner or checker, not %q", p.Role)
	}
	if p.Provider != "" {
		provider, err := checkProviderID(p.Provider)
		if err != nil {
			return p, err
		}
		p.Provider = provider
	}
	if len(p.Cursor) > 512 {
		return p, Errorf(CodeBadParams, "cursor is not one this engine gave out")
	}
	return p, nil
}

func taskModelRole(word string) bool {
	return word == RoleWorker || word == RolePlanner || word == RoleChecker
}

// checkModelRef bounds and canonicalises one model reference.
func checkModelRef(field string, ref ModelRef) (ModelRef, error) {
	id := strings.TrimSpace(ref.ID)
	switch {
	case id == "":
		return ref, Errorf(CodeBadParams, "%s needs a model id", field)
	case len(id) > ModelRefIDMax:
		return ref, Errorf(CodeBadParams, "%s: a model id is at most %d characters", field, ModelRefIDMax)
	case strings.IndexFunc(id, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0:
		return ref, Errorf(CodeBadParams, "%s: a model id has no spaces", field)
	case strings.Contains(id, "@"):
		return ref, Errorf(CodeBadParams, "%s: name the provider in provider, not after @", field)
	}
	out := ModelRef{ID: id}
	if strings.TrimSpace(ref.Provider) != "" {
		provider, err := checkProviderID(ref.Provider)
		if err != nil {
			return ref, Errorf(CodeBadParams, "%s: %s", field, err.(*Error).Message)
		}
		out.Provider = provider
	}
	return out, nil
}

// checkProviderID bounds a provider id: lowercase letters, digits, - _ .
func checkProviderID(raw string) (string, error) {
	id := strings.ToLower(strings.TrimSpace(raw))
	if id == "" || len(id) > ModelRefProviderMax || strings.IndexFunc(id, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.')
	}) >= 0 {
		return "", Errorf(CodeBadParams, "%q is not a provider id", raw)
	}
	return id, nil
}

// decodeTaskModelsPatch reads settings.models.set's params: the parts present,
// each checked, nothing it does not know, and no row named twice.
func decodeTaskModelsPatch(raw json.RawMessage) (TaskModelsPatch, error) {
	var fields map[string]json.RawMessage
	if err := decode(raw, &fields); err != nil {
		return TaskModelsPatch{}, err
	}
	var patch TaskModelsPatch
	for name, value := range fields {
		switch name {
		case "revision":
			if json.Unmarshal(value, &patch.Revision) != nil || len(patch.Revision) > 128 {
				return patch, Errorf(CodeBadParams, "revision is the string settings.models.get answered")
			}
		case "mode":
			var mode string
			if json.Unmarshal(value, &mode) != nil || (mode != TaskModelsAuto && mode != TaskModelsOne) {
				return patch, Errorf(CodeBadParams, "mode is auto or one")
			}
			patch.Mode = &mode
		case "one_model":
			var ref ModelRef
			if string(value) == "null" || json.Unmarshal(value, &ref) != nil {
				return patch, Errorf(CodeBadParams, "one_model is {id, provider?}")
			}
			ref, err := checkModelRef("one_model", ref)
			if err != nil {
				return patch, err
			}
			patch.OneModel = &ref
		case "pins":
			var pins map[string]json.RawMessage
			if json.Unmarshal(value, &pins) != nil || pins == nil {
				return patch, Errorf(CodeBadParams, "pins is {worker?, planner?, checker?, auxiliary?}")
			}
			patch.Pins = map[string]*ModelRef{}
			for role, pin := range pins {
				if !taskModelRole(role) && role != RoleAuxiliary {
					return patch, Errorf(CodeBadParams, "pins: %q is not a role · worker, planner, checker or auxiliary", role)
				}
				if string(pin) == "null" {
					patch.Pins[role] = nil
					continue
				}
				var ref ModelRef
				if json.Unmarshal(pin, &ref) != nil {
					return patch, Errorf(CodeBadParams, "pins.%s is {id, provider?} or null", role)
				}
				ref, err := checkModelRef("pins."+role, ref)
				if err != nil {
					return patch, err
				}
				patch.Pins[role] = &ref
			}
		case "allowed":
			var rows []TaskModelAllowed
			if json.Unmarshal(value, &rows) != nil {
				return patch, Errorf(CodeBadParams, "allowed is [{model: {id, provider}, allowed}]")
			}
			if len(rows) > TaskModelsAllowedMax {
				return patch, Errorf(CodeBadParams, "allowed changes at most %d rows at once", TaskModelsAllowedMax)
			}
			seen := map[ModelRef]bool{}
			for i, row := range rows {
				ref, err := checkModelRef("allowed", row.Model)
				if err != nil {
					return patch, err
				}
				if ref.Provider == "" {
					return patch, Errorf(CodeBadParams, "allowed: %s needs its provider · a row is one model on one provider", ref.ID)
				}
				if seen[ref] {
					return patch, Errorf(CodeBadParams, "allowed names %s on %s twice", ref.ID, ref.Provider)
				}
				seen[ref] = true
				rows[i].Model = ref
			}
			patch.Allowed = rows
		default:
			return patch, Errorf(CodeBadParams, "settings.models.set has no %s", name)
		}
	}
	if patch.Mode == nil && patch.OneModel == nil && patch.Pins == nil && patch.Allowed == nil {
		return patch, Errorf(CodeBadParams, "settings.models.set needs mode, one_model, pins or allowed")
	}
	return patch, nil
}
