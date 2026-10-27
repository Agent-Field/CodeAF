package appwire

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// ── THE FACTORY'S OWN RULES ─────────────────────────────────────────────────
//
// factory.rules reads, and factory.rules_set replaces, the rules a factory
// keeps for every repository it watches ([factory.HouseRules]): the sentences
// every step must follow, what to ask about before a job starts, and the paths
// it never starts on by itself. A repository's own rules stay in its
// `.codeaf/factory.yaml`; these only ever ADD to them.
//
// factory.rules_set PARAMS ARE {factory, rules, ask_first, never}, the
// optional settings, and `expect`, the rules as last read. All three lists are
// required: an omitted list is bad_params, never read as
// empty, so a caller that meant to change one list cannot wipe the others. The
// lines are checked here as the factory checks them (one line each, bounded),
// so a request it would refuse is refused before anything is sent.
//
// ONLY THE OPERATOR MAY CHANGE THEM, because a rule binds every repository and
// every person's work. A caller who is not is refused in words before any
// request leaves. A WRITE IS SENT ONCE; an outcome the engine cannot confirm is
// [CodeUncertain], and factory.rules says what was actually kept.

// FeatureFactoryRules is announced in hello when the engine serves
// factory.rules and factory.rules_set.
const FeatureFactoryRules = "factory.rules"

// FactoryRules is the read: the connection and the factory's rules as it keeps
// them, every list present (`[]` when empty).
type FactoryRules struct {
	Factory string             `json:"factory"`
	Rules   factory.HouseRules `json:"rules"`
}

// FactoryRulesSetParams is the whole request. The lists are pointers so that
// an omitted one is refused instead of being taken as empty.
type FactoryRulesSetParams struct {
	Factory  string    `json:"factory"`
	Rules    *[]string `json:"rules"`
	AskFirst *[]string `json:"ask_first"`
	Never    *[]string `json:"never"`
	// Adapt is how much a job's plan may reshape its steps on every
	// repository: "", adapt, ask or fixed. Omitted keeps the factory's word.
	Adapt *string `json:"adapt,omitempty" enum:",adapt,ask,fixed"`
	// StartOnItsOwn turns the factory's own starts on or off, and
	// StartLabels are the labels that let an issue start by itself. Each
	// omitted keeps the factory's setting.
	StartOnItsOwn *bool     `json:"start_on_its_own,omitempty"`
	StartLabels   *[]string `json:"start_labels,omitempty"`
	// Expect is the rules as the caller last read them (factory.rules). Sent,
	// a factory that keeps anything else changes nothing and the call is
	// [CodeStale]: read the rules again, then make the change.
	Expect *factory.HouseRules `json:"expect,omitempty"`
}

// FactoryRulesKeeper is the engine's half of factory.rules and
// factory.rules_set.
type FactoryRulesKeeper interface {
	FactoryRules(ctx context.Context, id string) (FactoryRules, error)
	FactoryRulesSet(ctx context.Context, p FactoryRulesSetParams) (FactoryRules, error)
}

func withFactoryRulesFeature(hello Hello, e Engine) Hello {
	if _, ok := e.(Factory); !ok {
		return hello
	}
	if _, ok := e.(FactoryRulesKeeper); ok {
		hello = withFeature(hello, FeatureFactoryRules)
	}
	return hello
}

// CheckRulesSet bounds the request without asking anyone: a connection and
// three lists the factory would accept.
func CheckRulesSet(p FactoryRulesSetParams) (FactoryRulesSetParams, factory.HouseRules, error) {
	var err error
	if p.Factory, err = factoryID(strings.TrimSpace(p.Factory)); err != nil {
		return p, factory.HouseRules{}, err
	}
	if p.Rules == nil || p.AskFirst == nil || p.Never == nil {
		return p, factory.HouseRules{}, Errorf(CodeBadParams, "rules, ask_first and never are all required; send [] for an empty list")
	}
	rules := factory.HouseRules{Rules: *p.Rules, AskFirst: *p.AskFirst, Never: *p.Never}
	if p.Adapt != nil {
		rules.Adapt = factory.AdaptMode(*p.Adapt)
	}
	if p.StartOnItsOwn != nil {
		rules.StartOnItsOwn = *p.StartOnItsOwn
	}
	if p.StartLabels != nil {
		rules.StartLabels = *p.StartLabels
	}
	clean, err := rules.Clean()
	if err != nil {
		return p, factory.HouseRules{}, Errorf(CodeBadParams, "%s", err.Error())
	}
	if p.Expect != nil {
		expect, err := p.Expect.Clean()
		if err != nil {
			return p, factory.HouseRules{}, Errorf(CodeBadParams, "expect: %s", err.Error())
		}
		p.Expect = &expect
	}
	return p, clean, nil
}

func init() {
	methods["factory.rules"] = typed(func(e Engine, p FactoryRef) (*FactoryRules, error) {
		k, ok := e.(FactoryRulesKeeper)
		if _, isFactory := e.(Factory); !ok || !isFactory {
			return nil, Errorf(CodeUnknownMethod, "this engine does not serve factory.rules")
		}
		id, err := factoryID(p.Factory)
		if err != nil {
			return nil, err
		}
		ctx, cancel := factoryCall()
		defer cancel()
		out, err := k.FactoryRules(ctx, id)
		if err != nil {
			return nil, err
		}
		return &out, nil
	})
	const name = "factory.rules_set"
	methods[name] = shaped[FactoryRulesSetParams, FactoryRules](func(e Engine, raw json.RawMessage) (*FactoryRules, error) {
		k, ok := e.(FactoryRulesKeeper)
		if _, isFactory := e.(Factory); !ok || !isFactory {
			return nil, Errorf(CodeUnknownMethod, "this engine does not serve %s", name)
		}
		var p FactoryRulesSetParams
		if err := decodeStrict(name, raw, &p); err != nil {
			return nil, err
		}
		p, _, err := CheckRulesSet(p)
		if err != nil {
			return nil, err
		}
		ctx, cancel := factoryCall()
		defer cancel()
		out, err := k.FactoryRulesSet(ctx, p)
		if err != nil {
			return nil, err
		}
		return &out, nil
	})
}
