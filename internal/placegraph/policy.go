package placegraph

// The recommendation policy: every number and switch that decides when codeaf
// offers to file a chat, group chats into a new place, or merge two places.
//
// EVERY DEFAULT HERE IS A PROVISIONAL ENGINEERING CHOICE, NOT A DESIGN DECISION.
// The figures the design does state are kept exactly (five chats to suggest a
// new place, "Not now" hides a suggestion for thirty days, one filing offer
// after the first reply, depth two to three); the rest were picked to keep the
// graph small and quiet until the designer decides. docs/AI-ROLES-AND-PLACES-POLICY.md
// lists which is which, and changing a default changes that page in the same
// commit.
//
// THE POLICY NEVER REORGANISES ANYTHING BY ITSELF. Creating, moving and merging
// are proposals a person approves (recommend.go). The one automatic action is
// filing a chat into a place that ALREADY EXISTS, and it is off by default.

import (
	"encoding/json"
	"fmt"
)

// RecommendPolicy is the whole set of choices. Confidence fields are percents.
type RecommendPolicy struct {
	// FilingOffers lets codeaf offer one existing place for a chat after its
	// first reply (design 6e: "at most one line after the first reply").
	FilingOffers bool `json:"filingOffers"`
	// ClusterOffers lets codeaf offer a new place, or offer to move a group of
	// unplaced chats into an existing one, once MinClusterChats belong together.
	ClusterOffers bool `json:"clusterOffers"`
	// MergeOffers lets codeaf offer to merge two places whose names say the
	// same thing under the same parent. It never calls a model.
	MergeOffers bool `json:"mergeOffers"`
	// AutoFile files a chat into an existing place without asking when the
	// offer is at least AutoFileConfidence sure. Off: offers wait for a person.
	AutoFile bool `json:"autoFile"`

	MinClusterChats    int `json:"minClusterChats"`
	MinConfidence      int `json:"minConfidence"`
	AutoFileConfidence int `json:"autoFileConfidence"`

	// Caps on places the AI created. A person's own places never count and
	// are never limited by these.
	MaxAITopLevel int `json:"maxAiTopLevel"`
	MaxAISiblings int `json:"maxAiSiblings"`
	MaxAIDepth    int `json:"maxAiDepth"`
	MaxAIPlaces   int `json:"maxAiPlaces"`
	MaxPending    int `json:"maxPending"`

	// Spending: model calls per rolling day for each role, and how often an
	// organizing pass may call at all.
	FilingCallsPerDay    int `json:"filingCallsPerDay"`
	ClusterCallsPerDay   int `json:"clusterCallsPerDay"`
	OrganizeEveryMinutes int `json:"organizeEveryMinutes"`
	DeclineSnoozeDays    int `json:"declineSnoozeDays"`
	MaxCandidates        int `json:"maxCandidates"`
}

// RecommendPolicyField is one row of the settings page, and the single source of the
// field's default and bounds: the page, the config writer and Normalized all
// read this table, so a bound cannot be stated twice.
type RecommendPolicyField struct {
	Key     string `json:"key"`
	Group   string `json:"group"`
	Name    string `json:"name"`
	Explain string `json:"explain"`
	// Kind is "switch" or "number".
	Kind    string `json:"kind"`
	Default any    `json:"default"`
	Min     int    `json:"min,omitempty"`
	Max     int    `json:"max,omitempty"`
	Unit    string `json:"unit,omitempty"`
	// Design says the default is the design's own figure; otherwise it is a
	// provisional engineering choice waiting on the designer.
	Design bool `json:"design"`

	get func(*RecommendPolicy) any
	set func(*RecommendPolicy, any)
}

func boolField(key, group, name, explain string, def, design bool, at func(*RecommendPolicy) *bool) RecommendPolicyField {
	return RecommendPolicyField{Key: key, Group: group, Name: name, Explain: explain, Kind: "switch", Default: def, Design: design,
		get: func(p *RecommendPolicy) any { return *at(p) },
		set: func(p *RecommendPolicy, v any) { *at(p) = v.(bool) }}
}

func intField(key, group, name, explain, unit string, def, lo, hi int, design bool, at func(*RecommendPolicy) *int) RecommendPolicyField {
	return RecommendPolicyField{Key: key, Group: group, Name: name, Explain: explain, Kind: "number", Default: def, Min: lo, Max: hi, Unit: unit, Design: design,
		get: func(p *RecommendPolicy) any { return *at(p) },
		set: func(p *RecommendPolicy, v any) { *at(p) = v.(int) }}
}

// Groups of the policy rows on the settings page.
const (
	PolicyGroupOffers = "offers"
	PolicyGroupLimits = "limits"
	PolicyGroupSpend  = "spend"
)

var policyFields = []RecommendPolicyField{
	boolField("filingOffers", PolicyGroupOffers, "Offer a place for a chat",
		"After a chat's first reply, offer one place you already have. Never creates a place.",
		true, true, func(p *RecommendPolicy) *bool { return &p.FilingOffers }),
	boolField("clusterOffers", PolicyGroupOffers, "Offer new places",
		"When enough chats in no place belong together, offer to put them in a place. You approve every new place.",
		true, true, func(p *RecommendPolicy) *bool { return &p.ClusterOffers }),
	boolField("mergeOffers", PolicyGroupOffers, "Offer to merge look-alike places",
		"When two places under the same parent have the same name in different words, offer to merge them. You approve every merge.",
		true, false, func(p *RecommendPolicy) *bool { return &p.MergeOffers }),
	boolField("autoFile", PolicyGroupOffers, "File chats without asking",
		"Put a chat in a place you already have when codeaf is very sure, and say so. New places, moves and merges always ask.",
		false, false, func(p *RecommendPolicy) *bool { return &p.AutoFile }),
	intField("minClusterChats", PolicyGroupLimits, "Chats before a new place is offered",
		"How many chats in no place must belong together before codeaf offers a place for them.", "chats",
		5, 3, 50, true, func(p *RecommendPolicy) *int { return &p.MinClusterChats }),
	intField("minConfidence", PolicyGroupLimits, "How sure before offering",
		"Offers less sure than this are not shown.", "%",
		75, 50, 100, false, func(p *RecommendPolicy) *int { return &p.MinConfidence }),
	intField("autoFileConfidence", PolicyGroupLimits, "How sure before filing without asking",
		"Only used when filing without asking is on.", "%",
		90, 60, 100, false, func(p *RecommendPolicy) *int { return &p.AutoFileConfidence }),
	intField("maxAiTopLevel", PolicyGroupLimits, "Top-level places codeaf may create",
		"Counts only places created from codeaf's offers. Places you make yourself are never limited.", "places",
		6, 0, 50, false, func(p *RecommendPolicy) *int { return &p.MaxAITopLevel }),
	intField("maxAiSiblings", PolicyGroupLimits, "Places codeaf may create under one parent",
		"Counts only places created from codeaf's offers.", "places",
		8, 0, 100, false, func(p *RecommendPolicy) *int { return &p.MaxAISiblings }),
	intField("maxAiDepth", PolicyGroupLimits, "Deepest level for a new place",
		"A top-level place is level 1.", "levels",
		3, 1, 6, true, func(p *RecommendPolicy) *int { return &p.MaxAIDepth }),
	intField("maxAiPlaces", PolicyGroupLimits, "Places codeaf may create in all",
		"Counts active places created from codeaf's offers.", "places",
		30, 0, 500, false, func(p *RecommendPolicy) *int { return &p.MaxAIPlaces }),
	intField("maxPending", PolicyGroupLimits, "Offers waiting at once",
		"New offers wait until you answer one of these.", "offers",
		3, 1, 20, false, func(p *RecommendPolicy) *int { return &p.MaxPending }),
	intField("declineSnoozeDays", PolicyGroupLimits, "Days \"Not now\" hides an offer",
		"The same offer is not made again for this long.", "days",
		30, 1, 365, true, func(p *RecommendPolicy) *int { return &p.DeclineSnoozeDays }),
	intField("maxCandidates", PolicyGroupLimits, "Places shown to the model at once",
		"The model only chooses among these, picked by codeaf first.", "places",
		5, 2, 12, false, func(p *RecommendPolicy) *int { return &p.MaxCandidates }),
	intField("filingCallsPerDay", PolicyGroupSpend, "Filing calls per day",
		"Model calls spent on filing offers in any 24 hours.", "calls",
		40, 0, 1000, false, func(p *RecommendPolicy) *int { return &p.FilingCallsPerDay }),
	intField("clusterCallsPerDay", PolicyGroupSpend, "New-place calls per day",
		"Model calls spent on naming new places in any 24 hours.", "calls",
		6, 0, 200, false, func(p *RecommendPolicy) *int { return &p.ClusterCallsPerDay }),
	intField("organizeEveryMinutes", PolicyGroupSpend, "Minutes between organizing passes",
		"An organizing pass that would call a model waits at least this long after the last one.", "minutes",
		60, 5, 1440, false, func(p *RecommendPolicy) *int { return &p.OrganizeEveryMinutes }),
}

// RecommendPolicyFields lists the settings rows in page order. The returned rows are
// copies; their Default is the value DefaultRecommendPolicy holds.
func RecommendPolicyFields() []RecommendPolicyField {
	return append([]RecommendPolicyField(nil), policyFields...)
}

func policyField(key string) (RecommendPolicyField, bool) {
	for _, f := range policyFields {
		if f.Key == key {
			return f, true
		}
	}
	return RecommendPolicyField{}, false
}

// DefaultRecommendPolicy is the policy before anyone chose anything.
func DefaultRecommendPolicy() RecommendPolicy {
	var p RecommendPolicy
	for _, f := range policyFields {
		f.set(&p, f.Default)
	}
	return p
}

// Normalized clamps every number into its field's bounds, so a hand-edited or
// older config file can never ask for a negative budget or a thousand-level
// graph. A zero number that is below its minimum becomes the minimum, not the
// default: zero is a real answer for the AI caps and the call budgets.
func (p RecommendPolicy) Normalized() RecommendPolicy {
	for _, f := range policyFields {
		if f.Kind != "number" {
			continue
		}
		v := f.get(&p).(int)
		if v < f.Min {
			v = f.Min
		}
		if v > f.Max {
			v = f.Max
		}
		f.set(&p, v)
	}
	return p
}

// Value reads one field by its key.
func (p RecommendPolicy) Value(key string) (any, bool) {
	f, ok := policyField(key)
	if !ok {
		return nil, false
	}
	return f.get(&p), true
}

// Set writes one field from its JSON spelling. A number out of bounds is
// refused rather than clamped: a person who typed 900 should be told, not
// quietly given 50.
func (p *RecommendPolicy) Set(key string, raw json.RawMessage) error {
	f, ok := policyField(key)
	if !ok {
		return fmt.Errorf("%w: no setting %q", ErrInvalid, key)
	}
	switch f.Kind {
	case "switch":
		var b bool
		if err := json.Unmarshal(raw, &b); err != nil {
			return fmt.Errorf("%w: %s takes on or off", ErrInvalid, key)
		}
		f.set(p, b)
	default:
		var n float64
		if err := json.Unmarshal(raw, &n); err != nil || n != float64(int(n)) {
			return fmt.Errorf("%w: %s takes a whole number", ErrInvalid, key)
		}
		if int(n) < f.Min || int(n) > f.Max {
			return fmt.Errorf("%w: %s must be between %d and %d", ErrInvalid, key, f.Min, f.Max)
		}
		f.set(p, int(n))
	}
	return nil
}
