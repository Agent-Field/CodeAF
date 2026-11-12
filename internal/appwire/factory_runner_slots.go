package appwire

import (
	"context"
	"encoding/json"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// ── CHANGING HOW MANY JOBS ONE OF YOUR COMPUTERS RUNS AT ONCE ───────────────
//
// factory.runner_slots sets the number of runner slots of ONE computer, for the
// person who holds the connection now and owns that computer. factory.runners
// (the read of the directory) is unchanged and is how a caller learns the
// actual state after any outcome that was not a plain yes.
//
// THE PARAMS ARE EXACTLY {factory, runner, slots}. There is no method that
// names a route, a verb, a body, a token or a folder; an unknown field is
// bad_params and never believed to have been honoured. "slots" has no default
// and is an integer from 1 to [api.MaxRunnerSlots], the server's own number
// and not a copy of it: a request outside it is refused before anything is
// sent. "runner" is the computer's id exactly as factory.runners lists it,
// treated as opaque text of a bounded size.
//
// THIS CHANGES CAPACITY AND NOTHING ELSE. It does not pause, remove or add a
// computer, does not touch its checkouts, and writes nothing to GitHub.
//
// A WRITE IS SENT ONCE. It is never retried or tried by a second route. An
// outcome the engine cannot confirm is [CodeUncertain]: the capacity may or may
// not have changed, and the answer claims neither. The receipt is answered only
// after the factory said it completed, and it repeats what was asked, never
// more: read factory.runners for what is actually granted.
//
// MIXED VERSIONS. The feature is announced by the engine, but whether THIS
// factory can take the write is read from its runner directory
// (api.Runners.SlotsPolicy, with the range and requires_live) and from the
// computer's own live_capacity flag. A factory that sends no policy, or one
// that is not the expected one, and a computer whose worker cannot follow a
// change, are refused as [CodeConflict] before any write leaves.
//
// ALL OF THIS IS OPTIONAL AND DISTINCT FROM factory.repos_manage. An engine
// that is not a [FactoryRunnerAdmin] answers the method as unknown_method and
// does not announce [FeatureFactoryRunnerManage].

// FeatureFactoryRunnerManage is announced in hello when the engine serves
// factory.runner_slots.
const FeatureFactoryRunnerManage = "factory.runner_manage"

// RunnerIDMax bounds a computer's id, in bytes, so a request that cannot name
// one is never sent.
const RunnerIDMax = 200

// FactoryRunnerSlotsParams is the whole request. Slots is a pointer so that an
// omitted value is refused instead of being taken as zero.
type FactoryRunnerSlotsParams struct {
	Factory string `json:"factory"`
	Runner  string `json:"runner"`
	Slots   *int   `json:"slots"`
}

// FactoryRunnerSlotsDone is what the factory confirmed: the same connection,
// computer and number that were asked for. Read factory.runners for the roster.
type FactoryRunnerSlotsDone struct {
	Factory string `json:"factory"`
	Runner  string `json:"runner"`
	Slots   int    `json:"slots"`
}

// FactoryRunnerAdmin is the engine's half of factory.runner_slots. It makes at
// most one write to the factory and never sends it twice; an outcome it cannot
// confirm is [CodeUncertain].
type FactoryRunnerAdmin interface {
	FactoryRunnerSlots(ctx context.Context, p FactoryRunnerSlotsParams) (FactoryRunnerSlotsDone, error)
}

func withFactoryRunnerManageFeature(hello Hello, e Engine) Hello {
	if _, ok := e.(Factory); !ok {
		return hello
	}
	if _, ok := e.(FactoryRunnerAdmin); ok {
		hello = withFeature(hello, FeatureFactoryRunnerManage)
	}
	return hello
}

// CheckRunnerID is true for a computer id that is safe to put in one path
// segment after escaping: bounded, valid UTF-8, without control or format
// characters, separators, or surrounding space, and not a dot segment.
func CheckRunnerID(id string) bool {
	if id == "" || len(id) > RunnerIDMax || !utf8.ValidString(id) || id != strings.TrimSpace(id) || id == "." || id == ".." {
		return false
	}
	for _, ch := range id {
		if unicode.IsControl(ch) || unicode.Is(unicode.Cf, ch) || ch == '/' || ch == '\\' || ch == utf8.RuneError {
			return false
		}
	}
	return true
}

// CheckRunnerSlots bounds the request without asking anyone: a connection, a
// valid computer id, and an explicit number inside the server's range.
func CheckRunnerSlots(p FactoryRunnerSlotsParams) (FactoryRunnerSlotsParams, error) {
	var err error
	if p.Factory, err = factoryID(strings.TrimSpace(p.Factory)); err != nil {
		return p, err
	}
	if !CheckRunnerID(p.Runner) {
		return p, Errorf(CodeBadParams, "runner must be a computer id as listed")
	}
	if p.Slots == nil || *p.Slots < 1 || *p.Slots > api.MaxRunnerSlots {
		return p, Errorf(CodeBadParams, "slots must be a whole number from 1 to %d", api.MaxRunnerSlots)
	}
	return p, nil
}

func init() {
	const name = "factory.runner_slots"
	methods[name] = runnerSlotsMethod(name)
	factoryRunnerMethods[name] = methods[name]
}

// runnerSlotsMethod reads params STRICTLY, checks them, then asks the engine once.
func runnerSlotsMethod(name string) method {
	return shaped[FactoryRunnerSlotsParams, FactoryRunnerSlotsDone](func(e Engine, raw json.RawMessage) (*FactoryRunnerSlotsDone, error) {
		a, ok := e.(FactoryRunnerAdmin)
		if _, isFactory := e.(Factory); !ok || !isFactory {
			return nil, Errorf(CodeUnknownMethod, "this engine does not serve %s", name)
		}
		var p FactoryRunnerSlotsParams
		if err := decodeStrict(name, raw, &p); err != nil {
			return nil, err
		}
		p, err := CheckRunnerSlots(p)
		if err != nil {
			return nil, err
		}
		ctx, cancel := factoryCall()
		defer cancel()
		out, err := a.FactoryRunnerSlots(ctx, p)
		if err != nil {
			return nil, err
		}
		return &out, nil
	})
}

// factoryRunnerMethods is the table of factory.runner_slots, for the tests that walk it.
var factoryRunnerMethods = map[string]method{}
