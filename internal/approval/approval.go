// Package approval is the consent policy for the v3 session agent: one pure
// function from a tool call to allow, prompt or deny.
//
// Three packages in this tree guard three different moments and are easy to
// confuse. [consent] prices a job before it starts — money. [gate] holds a
// workforce command for a countdown before it commits — time. This one answers
// a narrower and more frequent question: the model has asked to run a tool
// right now, and something has to decide whether that runs, asks, or is
// refused. It is ported from omp's tools.approval plus its bash pattern list,
// because that design has one property worth keeping — the dangerous case is
// decided by MATCHING, not by a model's judgement about its own request.
//
// This package is policy only. It reads no config, touches no session, spawns
// nothing, and deliberately imports nothing from either — the wiring wave maps
// the config registry onto [Load] and hangs [Policy.Check] off the tool loop.
// Keeping it that way is what makes the bash matching law testable as a table
// rather than as an integration test with a shell on the other end.
//
// The interesting law lives in bash.go: deny and prompt catch a dangerous
// fragment anywhere in a compound line, while allow vouches only for a line it
// matches whole. That asymmetry is the entire point of the package.
package approval

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Action is what the policy says to do with a call.
type Action string

const (
	// ActionAllow runs the tool without asking.
	ActionAllow Action = "allow"
	// ActionPrompt runs it only if a person says so. This is the safe answer
	// and the one every unset or unreadable case falls back to.
	ActionPrompt Action = "prompt"
	// ActionDeny refuses the call outright; the model is told no and keeps
	// going, which is a result it can act on rather than a hang.
	ActionDeny Action = "deny"
)

// valid reports whether an Action is one of the three. A Policy built by hand
// can carry nonsense here; rules that do are skipped rather than obeyed.
func (a Action) valid() bool {
	switch a {
	case ActionAllow, ActionPrompt, ActionDeny:
		return true
	default:
		return false
	}
}

// ParseAction reads one of the three words. Anything else is an error rather
// than a silent fallback: a settings file that says "ask" or "always" has a
// mistake in it, and a consent engine that quietly reinterprets a typo is
// exactly the thing nobody can audit later.
func ParseAction(text string) (Action, error) {
	action := Action(strings.TrimSpace(text))
	if !action.valid() {
		return "", fmt.Errorf("unknown action %q (want allow, prompt or deny)", text)
	}
	return action, nil
}

// ToolBash is the one tool whose arguments this package looks inside. Every
// other tool is judged by name alone.
const ToolBash = "bash"

// Rule is one bash pattern and the answer it carries. Match is a glob in the
// restricted dialect documented in bash.go: '*' and literal text, nothing else.
type Rule struct {
	Match  string
	Action Action
}

// Policy is the whole decision surface: a blanket default, per-tool overrides
// keyed by tool name, and an ordered list of bash patterns.
//
// The zero Policy prompts for everything. That is the intended reading of "no
// policy configured" — a session with no settings should ask, not run.
type Policy struct {
	// Default applies to any tool with no rule of its own.
	Default Action
	// Tools is keyed by tool name (read, edit, write, bash, …). A tool rule
	// beats the default; for bash it is only the starting point, since the
	// patterns and the critical table still get their say.
	Tools map[string]Action
	// BashPatterns is ordered and FIRST MATCH WINS. Order is the author's
	// priority statement, so it is preserved exactly as loaded.
	BashPatterns []Rule
}

// Decision is an answer plus the reason to show for it. Rule is already
// phrased for display — `bash pattern "rm -rf *"`, `tool "edit"`, `default` —
// because every surface that renders a consent prompt needs the same sentence
// and none of them should be re-deriving it from the Policy.
type Decision struct {
	Action Action
	Rule   string
}

// String is the one-line form: `bash pattern "rm -rf *" → prompt`.
func (d Decision) String() string { return d.Rule + " → " + string(d.Action) }

// Check answers for one tool call. args is the raw JSON the model emitted; it
// is read only for bash, and only for its "command" field.
func (p Policy) Check(tool string, args json.RawMessage) Decision {
	tool = strings.TrimSpace(tool)
	base := p.base(tool)
	if tool != ToolBash {
		return base
	}
	command, ok := bashCommand(args)
	if !ok {
		// A bash call whose command cannot be read cannot be matched against
		// anything, so an allow here would be a blanket allow for the one tool
		// that most needs the patterns. Deny and prompt stand; allow becomes a
		// prompt and the person sees the raw arguments.
		if base.Action == ActionAllow {
			return Decision{Action: ActionPrompt, Rule: "bash call with no readable command"}
		}
		return base
	}
	return p.checkBash(command, base)
}

// CheckBash judges a command line directly, for callers that already have the
// string — a slash command, a queued shell action, a settings preview that
// wants to show what a pattern would do.
func (p Policy) CheckBash(command string) Decision {
	return p.checkBash(command, p.base(ToolBash))
}

// base is the answer before any bash-specific reasoning: the tool's own rule
// if it has one, otherwise the default, otherwise ask.
func (p Policy) base(tool string) Decision {
	if action, ok := p.Tools[tool]; ok && action.valid() {
		return Decision{Action: action, Rule: fmt.Sprintf("tool %q", tool)}
	}
	if !p.Default.valid() {
		return Decision{Action: ActionPrompt, Rule: "default (unset)"}
	}
	return Decision{Action: p.Default, Rule: "default"}
}

// checkBash walks the patterns in order and then applies the critical table.
func (p Policy) checkBash(command string, base Decision) Decision {
	segments, compound := splitSegments(command)
	whole := strings.TrimSpace(command)

	decision := base
	for _, rule := range p.BashPatterns {
		if !ruleMatches(rule, whole, segments, compound) {
			continue
		}
		decision = Decision{Action: rule.Action, Rule: fmt.Sprintf("bash pattern %q", rule.Match)}
		break
	}

	// The critical table is a floor under allow and nothing more. An explicit
	// deny already refuses, a prompt already asks, and only a decision that
	// would have run silently is worth interrupting.
	if decision.Action == ActionAllow {
		if hit, ok := criticalHit(command, segments); ok {
			return Decision{Action: ActionPrompt, Rule: fmt.Sprintf("critical command %q", hit)}
		}
	}
	return decision
}

// bashCommand pulls the command out of a bash tool call's arguments. The bool
// distinguishes "no command to judge" from "the empty command", which the
// caller treats very differently.
func bashCommand(args json.RawMessage) (string, bool) {
	text := strings.TrimSpace(string(args))
	if text == "" || text == "null" {
		return "", false
	}
	var fields struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal([]byte(text), &fields); err != nil {
		return "", false
	}
	if strings.TrimSpace(fields.Command) == "" {
		return "", false
	}
	return fields.Command, true
}

// ── loading ─────────────────────────────────────────────────────────────────

// Load builds a Policy from a settings-shaped generic map:
//
//	default | mode : "allow" | "prompt" | "deny"
//	tools          : {"read": "allow", "edit": "prompt", …}
//	bash.patterns  : [{"match": "git status*", "approval": "allow"}, …]
//
// The patterns list is accepted both nested (bash → patterns) and under the
// flattened dotted key, because aforge's config registry keys are dotted and
// a JSON settings file is not. Keys this package does not know are ignored:
// the map it is handed is a whole settings tree, not a struct built for it.
//
// Every malformed value is an error rather than a skip. A pattern that was
// meant to deny something and was silently dropped for a typo is the worst
// possible failure mode here.
func Load(raw map[string]any) (Policy, error) {
	policy := Policy{}

	if value, ok := firstPresent(raw, "default", "mode"); ok {
		text, ok := value.(string)
		if !ok {
			return Policy{}, fmt.Errorf("approval: default must be a string, got %T", value)
		}
		action, err := ParseAction(text)
		if err != nil {
			return Policy{}, fmt.Errorf("approval: default: %w", err)
		}
		policy.Default = action
	}

	if value, ok := raw["tools"]; ok && value != nil {
		tools, ok := value.(map[string]any)
		if !ok {
			return Policy{}, fmt.Errorf("approval: tools must be a map, got %T", value)
		}
		policy.Tools = make(map[string]Action, len(tools))
		// Sorted so that a file with two bad entries always reports the same
		// one; Go map order would make the error message a coin flip.
		names := make([]string, 0, len(tools))
		for name := range tools {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			text, ok := tools[name].(string)
			if !ok {
				return Policy{}, fmt.Errorf("approval: tools[%q] must be a string, got %T", name, tools[name])
			}
			action, err := ParseAction(text)
			if err != nil {
				return Policy{}, fmt.Errorf("approval: tools[%q]: %w", name, err)
			}
			policy.Tools[name] = action
		}
	}

	patterns, err := loadPatterns(raw)
	if err != nil {
		return Policy{}, err
	}
	policy.BashPatterns = patterns
	return policy, nil
}

// loadPatterns reads bash.patterns in either shape.
func loadPatterns(raw map[string]any) ([]Rule, error) {
	value, ok := raw["bash.patterns"]
	if !ok {
		bash, present := raw["bash"]
		if !present || bash == nil {
			return nil, nil
		}
		section, isMap := bash.(map[string]any)
		if !isMap {
			return nil, fmt.Errorf("approval: bash must be a map, got %T", bash)
		}
		value, ok = section["patterns"]
		if !ok {
			return nil, nil
		}
	}
	if value == nil {
		return nil, nil
	}
	list, ok := toList(value)
	if !ok {
		return nil, fmt.Errorf("approval: bash.patterns must be a list, got %T", value)
	}

	rules := make([]Rule, 0, len(list))
	for index, item := range list {
		entry, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("approval: bash.patterns[%d] must be a map, got %T", index, item)
		}
		match, _ := entry["match"].(string)
		if strings.TrimSpace(match) == "" {
			return nil, fmt.Errorf("approval: bash.patterns[%d]: missing match", index)
		}
		approval, ok := entry["approval"]
		if !ok {
			return nil, fmt.Errorf("approval: bash.patterns[%d] (%q): missing approval", index, match)
		}
		text, ok := approval.(string)
		if !ok {
			return nil, fmt.Errorf("approval: bash.patterns[%d] (%q): approval must be a string, got %T", index, match, approval)
		}
		action, err := ParseAction(text)
		if err != nil {
			return nil, fmt.Errorf("approval: bash.patterns[%d] (%q): %w", index, match, err)
		}
		rules = append(rules, Rule{Match: match, Action: action})
	}
	return rules, nil
}

func firstPresent(raw map[string]any, keys ...string) (any, bool) {
	for _, key := range keys {
		if value, ok := raw[key]; ok && value != nil {
			return value, true
		}
	}
	return nil, false
}

// toList accepts the two shapes a decoded settings list arrives in: []any from
// encoding/json, []map[string]any from a hand-built map.
func toList(value any) ([]any, bool) {
	switch typed := value.(type) {
	case []any:
		return typed, true
	case []map[string]any:
		list := make([]any, 0, len(typed))
		for _, item := range typed {
			list = append(list, item)
		}
		return list, true
	default:
		return nil, false
	}
}
