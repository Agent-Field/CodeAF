package appwire

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/route"
)

// ── THE FACTORY'S ROUTING RULES ─────────────────────────────────────────────
//
// factory.routes reads, and factory.routes_set replaces, the factory's
// routing table (internal/factory/route): an ordered list of rules, the first
// that matches an arriving item decides its repository, pipeline, priority
// and whether it starts by itself, asks first, or waits in Backlog.
//
// factory.routes_set PARAMS ARE {factory, rules, expect}. rules is the whole
// table and is required: an omitted one is bad_params, never read as empty,
// so a caller cannot wipe the table by forgetting it. The table is checked
// here by [route.Clean], the same check the factory makes, so a table it
// would refuse is refused before anything is sent.
//
// ONLY THE OPERATOR MAY CHANGE IT, because a rule decides where every
// source's work goes. A WRITE IS SENT ONCE; an outcome the engine cannot
// confirm is [CodeUncertain], and factory.routes says what was kept.

// FeatureFactoryRoutes is announced in hello when the engine serves
// factory.routes, factory.routes_set and factory.routes_test.
const FeatureFactoryRoutes = "factory.routes"

// FactoryRoutes is the read: the connection and the table as the factory
// keeps it (`[]` when empty), with changed_by and changed_at when it was ever
// written.
type FactoryRoutes struct {
	Factory string       `json:"factory"`
	Rules   []route.Rule `json:"rules"`
	factory.RulesChange
}

// FactoryRoutesSetParams is the whole request. Rules is a pointer so that an
// omitted table is refused instead of being taken as empty.
type FactoryRoutesSetParams struct {
	Factory string        `json:"factory"`
	Rules   *[]route.Rule `json:"rules"`
	// Expect is the table as the caller last read it (factory.routes). Sent,
	// a factory that keeps anything else changes nothing and the call is
	// [CodeStale]: read the table again, then make the change.
	Expect *[]route.Rule `json:"expect,omitempty"`
}

// FactoryRoutesKeeper is the engine's half of factory.routes and
// factory.routes_set.
type FactoryRoutesKeeper interface {
	FactoryRoutes(ctx context.Context, id string) (FactoryRoutes, error)
	FactoryRoutesSet(ctx context.Context, p FactoryRoutesSetParams) (FactoryRoutes, error)
}

// FactoryRoutesTestParams asks what a table would do with the newest jobs:
// rules is the table to try (required; it need not be the one kept), limit
// how many jobs, 0 for [route.DefaultPreview], at most [route.MaxPreview].
type FactoryRoutesTestParams struct {
	Factory string        `json:"factory"`
	Rules   *[]route.Rule `json:"rules"`
	Limit   int           `json:"limit,omitempty"`
}

// FactoryRoutesTried is factory.routes_test's answer, newest job first: per
// job, `matched` with the rule (and the kept answer's why when a question
// decided it), `pending` with the rule whose question has no answer yet, or
// `none`. NO MODEL IS ASKED; only answers already kept on jobs are read.
type FactoryRoutesTried struct {
	Factory string        `json:"factory"`
	Jobs    []route.Tried `json:"jobs"`
}

// FactoryRoutesTester is the engine's half of factory.routes_test.
type FactoryRoutesTester interface {
	FactoryRoutesTest(ctx context.Context, p FactoryRoutesTestParams) (FactoryRoutesTried, error)
}

// CheckRoutesTest bounds a preview the way the factory does.
func CheckRoutesTest(p FactoryRoutesTestParams) (FactoryRoutesTestParams, error) {
	var err error
	if p.Factory, err = factoryID(strings.TrimSpace(p.Factory)); err != nil {
		return p, err
	}
	if p.Rules == nil {
		return p, Errorf(CodeBadParams, "rules is required; send [] to try no routing rules")
	}
	if p.Limit < 0 || p.Limit > route.MaxPreview {
		return p, Errorf(CodeBadParams, "limit is 0 to %d jobs", route.MaxPreview)
	}
	rules, err := route.Clean(*p.Rules)
	if err != nil {
		return p, Errorf(CodeBadParams, "%s", err.Error())
	}
	p.Rules = &rules
	return p, nil
}

func withFactoryRoutesFeature(hello Hello, e Engine) Hello {
	if _, ok := e.(Factory); !ok {
		return hello
	}
	_, keeps := e.(FactoryRoutesKeeper)
	if _, tests := e.(FactoryRoutesTester); keeps && tests {
		hello = withFeature(hello, FeatureFactoryRoutes)
	}
	return hello
}

// CheckRoutesSet bounds the request without asking anyone: a connection and
// a table the factory would keep. It answers the request with both tables
// cleaned.
func CheckRoutesSet(p FactoryRoutesSetParams) (FactoryRoutesSetParams, error) {
	var err error
	if p.Factory, err = factoryID(strings.TrimSpace(p.Factory)); err != nil {
		return p, err
	}
	if p.Rules == nil {
		return p, Errorf(CodeBadParams, "rules is required; send [] for no routing rules")
	}
	rules, err := route.Clean(*p.Rules)
	if err != nil {
		return p, Errorf(CodeBadParams, "%s", err.Error())
	}
	p.Rules = &rules
	if p.Expect != nil {
		expect, err := route.Clean(*p.Expect)
		if err != nil {
			return p, Errorf(CodeBadParams, "expect: %s", err.Error())
		}
		p.Expect = &expect
	}
	return p, nil
}

func init() {
	methods["factory.routes"] = typed(func(e Engine, p FactoryRef) (*FactoryRoutes, error) {
		k, ok := e.(FactoryRoutesKeeper)
		if _, isFactory := e.(Factory); !ok || !isFactory {
			return nil, Errorf(CodeUnknownMethod, "this engine does not serve factory.routes")
		}
		id, err := factoryID(p.Factory)
		if err != nil {
			return nil, err
		}
		ctx, cancel := factoryCall()
		defer cancel()
		out, err := k.FactoryRoutes(ctx, id)
		if err != nil {
			return nil, err
		}
		return &out, nil
	})
	const name = "factory.routes_set"
	methods[name] = shaped[FactoryRoutesSetParams, FactoryRoutes](func(e Engine, raw json.RawMessage) (*FactoryRoutes, error) {
		k, ok := e.(FactoryRoutesKeeper)
		if _, isFactory := e.(Factory); !ok || !isFactory {
			return nil, Errorf(CodeUnknownMethod, "this engine does not serve %s", name)
		}
		var p FactoryRoutesSetParams
		if err := decodeStrict(name, raw, &p); err != nil {
			return nil, err
		}
		p, err := CheckRoutesSet(p)
		if err != nil {
			return nil, err
		}
		ctx, cancel := factoryCall()
		defer cancel()
		out, err := k.FactoryRoutesSet(ctx, p)
		if err != nil {
			return nil, err
		}
		return &out, nil
	})
	const test = "factory.routes_test"
	methods[test] = shaped[FactoryRoutesTestParams, FactoryRoutesTried](func(e Engine, raw json.RawMessage) (*FactoryRoutesTried, error) {
		k, ok := e.(FactoryRoutesTester)
		if _, isFactory := e.(Factory); !ok || !isFactory {
			return nil, Errorf(CodeUnknownMethod, "this engine does not serve %s", test)
		}
		var p FactoryRoutesTestParams
		if err := decodeStrict(test, raw, &p); err != nil {
			return nil, err
		}
		p, err := CheckRoutesTest(p)
		if err != nil {
			return nil, err
		}
		ctx, cancel := factoryCall()
		defer cancel()
		out, err := k.FactoryRoutesTest(ctx, p)
		if err != nil {
			return nil, err
		}
		return &out, nil
	})
}
