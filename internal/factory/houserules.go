package factory

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ── THE FACTORY'S OWN RULES ─────────────────────────────────────────────────
//
// A repository's rules live in its `.codeaf/factory.yaml` and travel with its
// code. HouseRules are the rules a person gives THE FACTORY, for every
// repository it watches: "don't touch auth without checking with me" said
// once, not copied into every repository. They are kept by the factory that
// serves the floor (the store beside its runner settings), so on a team they
// are the team's and live on the shared server, never on one person's machine.
//
// THEY ONLY EVER ADD. A house rule is merged into each repository's file the
// way the personal file is ([pipeline.WithHouseRules]): it can add a sentence a
// step must follow, a thing to ask about before a job starts, or a path never
// to start on by itself. It can never take away anything the repository says.
//
// Rules are what every step is told (the brief's rules paragraph) and what code
// applies where it recognises the sentence (internal/factory/run's policy).
// AskFirst and Never are read before a job starts, exactly as the repository's
// own `start.ask_first` and `start.never` are.
type HouseRules struct {
	Rules    []string `json:"rules"`
	AskFirst []string `json:"ask_first"`
	Never    []string `json:"never"`
	// Adapt is how much a job's plan may reshape its steps on every
	// repository ([AdaptMode]): "" says nothing (each repository's own word
	// stands), ask makes a person ratify every reshaped plan, fixed lets
	// only a person change the steps. Like the rest it only tightens: a
	// repository that says fixed stays fixed.
	Adapt AdaptMode `json:"adapt,omitempty"`
	// StartOnItsOwn is a SETTING, not a rule: the factory starts an issue
	// by itself when it carries one of StartLabels (or its repository's own
	// `start.labels`) and clears every rule; anything a rule flags waits for
	// a person ("Approval needed: start · …"). Off unless a person turned it
	// on. A factory that runs on a person's own machine runs no start rules
	// at all while it is off; a team server runs its repositories' own start
	// labels either way, and adds StartLabels only while it is on.
	StartOnItsOwn bool `json:"start_on_its_own,omitempty"`
	// StartLabels are the labels that let an issue start on its own, for
	// every repository, beside each repository's own. They count only while
	// StartOnItsOwn is on, and they are the one thing here that lets more
	// work start rather than less: turning the switch on is that consent.
	StartLabels []string `json:"start_labels,omitempty"`
}

// HouseRulesMost is how many lines one list may hold, and HouseRuleLongest how
// long one line may be, in characters. They bound what every step's brief
// carries; a rule longer than a sentence is a document, and belongs in the
// repository.
const (
	HouseRulesMost   = 40
	HouseRuleLongest = 300
)

// Empty says the factory has no rules of its own.
func (h HouseRules) Empty() bool {
	return len(h.Rules) == 0 && len(h.AskFirst) == 0 && len(h.Never) == 0 && h.Adapt == "" && !h.StartOnItsOwn && len(h.StartLabels) == 0
}

// Clean is h with every line trimmed, blank and repeated lines (ignoring case)
// dropped, and nil lists made empty, so a saved file and the wire always say
// `[]` rather than nothing. It refuses a list that is too long and a line that
// is too long, holds a line break or a control character: a rule is one line,
// and the brief quotes it as one.
func (h HouseRules) Clean() (HouseRules, error) {
	var err error
	if h.Rules, err = cleanRuleList("rules", h.Rules); err != nil {
		return HouseRules{}, err
	}
	if h.AskFirst, err = cleanRuleList("ask first", h.AskFirst); err != nil {
		return HouseRules{}, err
	}
	if h.Never, err = cleanRuleList("never", h.Never); err != nil {
		return HouseRules{}, err
	}
	labels, err := cleanRuleList("start label", h.StartLabels)
	if err != nil {
		return HouseRules{}, err
	}
	h.StartLabels = nil
	if len(labels) > 0 {
		h.StartLabels = labels
	}
	switch h.Adapt {
	case "", AdaptFree, AdaptAsk, AdaptFixed:
	default:
		return HouseRules{}, fmt.Errorf("adapt is adapt, ask or fixed")
	}
	return h, nil
}

func cleanRuleList(name string, in []string) ([]string, error) {
	out := []string{}
	seen := map[string]bool{}
	for _, line := range in {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if !utf8.ValidString(line) || utf8.RuneCountInString(line) > HouseRuleLongest {
			return nil, fmt.Errorf("each %s line must be one sentence of at most %d characters", name, HouseRuleLongest)
		}
		for _, r := range line {
			if unicode.IsControl(r) || r == utf8.RuneError {
				return nil, fmt.Errorf("each %s line must be one line of plain text", name)
			}
		}
		key := strings.ToLower(line)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, line)
	}
	if len(out) > HouseRulesMost {
		return nil, fmt.Errorf("the %s list holds at most %d lines", name, HouseRulesMost)
	}
	return out, nil
}

// StartRules are what a job asks a person about (a repository's
// `start.ask_first` and `start.never`, with the factory's own added; a never
// line is kept for older files and is a protected path exactly like an
// ask-first `touch <path>`), stamped on the job when it is started so its plan can be checked
// against them wherever it runs ([Item.StartRules]).
type StartRules struct {
	AskFirst []string `json:"ask_first"`
	Never    []string `json:"never"`
	// Unread says the repository's own rules could not be read when the job
	// started, so the lines above may lack its own: the approval after its
	// plan is then always a person's.
	Unread bool `json:"unread,omitempty"`
}

// StartRulesUnreadHold is the hold a plan gets when its job started with the
// repository's rules unread ([StartRules.Unread]).
const StartRulesUnreadHold = "ask first · the repository's rules could not be read"
