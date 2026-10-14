package prompt

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/apps/spec"
)

// Plan groups. Needs is not a group: codeaf computes it, so it cannot be edited.
const (
	GroupData       = "data"
	GroupAutomation = "automation"
	GroupScreens    = "screens"
	GroupAgent      = "agent"
)

// Edit operations on a plan-card line.
const (
	OpSet    = "set"
	OpAdd    = "add"
	OpRemove = "remove"
)

var lineID = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)

// Line is one editable plan line. The id is stable; only Line text changes.
type Line struct {
	ID   string `json:"id"`
	Line string `json:"line"`
}

// AgentLine is the plan's single agent line, which has no id.
type AgentLine struct {
	ID   string `json:"id,omitempty"`
	Line string `json:"line"`
}

// Plan is plan.json as the plan card draws it.
type Plan struct {
	// Brief is the design brief the PLAN reply opens with. plan.json has no
	// key for it, so it lives in build state and is never written there.
	Brief      Brief     `json:"brief,omitempty"`
	Ask        string    `json:"ask"`
	Name       string    `json:"name"`
	Data       []Line    `json:"data"`
	Automation []Line    `json:"automation"`
	Screens    []Line    `json:"screens"`
	Agent      AgentLine `json:"agent"`
	Needs      string    `json:"needs"`
}

// Edit is one change a person made to the plan card.
type Edit struct {
	Op    string `json:"op"`
	Group string `json:"group"`
	ID    string `json:"id,omitempty"`
	Line  string `json:"line,omitempty"`
}

// ParsePlan reads the model's PLAN reply and checks the plan's shape.
func ParsePlan(output string) (Plan, error) {
	last := errNoObject
	var long Plan
	for _, raw := range jsonObjects(output) {
		var plan Plan
		if err := json.Unmarshal(raw, &plan); err != nil {
			last = fmt.Errorf("prompt: plan: %w", err)
			continue
		}
		if err := plan.validate(); err != nil {
			if errors.Is(err, ErrLongLines) {
				long = plan
			}
			last = err
			continue
		}
		return plan, nil
	}
	if errors.Is(last, ErrLongLines) {
		return long, last
	}
	return Plan{}, last
}

// ErrLongLines marks a plan that is whole except for card lines over the
// limit. ParsePlan returns that plan beside the error, so a caller out of
// retries can keep it: the lines are cut to fit before Check.
var ErrLongLines = errors.New("plan lines are too long")

func (p Plan) validate() error {
	if len(p.Data) == 0 || len(p.Screens) == 0 {
		return fmt.Errorf("prompt: plan needs at least one data line and one screen line")
	}
	if strings.TrimSpace(p.Agent.Line) == "" {
		return fmt.Errorf("prompt: plan needs an agent line")
	}
	for _, group := range []string{GroupData, GroupAutomation, GroupScreens} {
		if err := checkLines(group, *p.lines(group)); err != nil {
			return err
		}
	}
	return p.checkLengths()
}

// checkLengths rejects card lines over the plan schema's limit, so the PLAN
// retry shortens them in the model's own words instead of a later cut.
func (p Plan) checkLengths() error {
	limit := PlanLineLimit()
	var long []string
	for _, group := range []string{GroupData, GroupAutomation, GroupScreens} {
		for _, l := range *p.lines(group) {
			if n := utf8.RuneCountInString(l.Line); limit > 0 && n > limit {
				long = append(long, fmt.Sprintf("%s %q is %d characters", group, l.ID, n))
			}
		}
	}
	if len(long) == 0 {
		return nil
	}
	return fmt.Errorf("prompt: plan lines must be at most %d characters: %s; shorten each to one clause, keeping its fields and choices%w", limit, strings.Join(long, "; "), silent{ErrLongLines})
}

// PlanLineLimit is plan.schema.json's maxLength for a card line.
var PlanLineLimit = sync.OnceValue(func() int {
	raw, err := spec.SchemaJSON(spec.PlanSchemaURL)
	if err != nil {
		panic(err)
	}
	var schema struct {
		Defs map[string]struct {
			Properties map[string]struct {
				MaxLength int `json:"maxLength"`
			} `json:"properties"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		panic(err)
	}
	return schema.Defs["line"].Properties["line"].MaxLength
})

func checkLines(group string, lines []Line) error {
	seen := map[string]bool{}
	for _, l := range lines {
		if err := checkLine(group, l, seen); err != nil {
			return err
		}
		seen[l.ID] = true
	}
	return nil
}

func checkLine(group string, l Line, seen map[string]bool) error {
	switch {
	case !lineID.MatchString(l.ID):
		return fmt.Errorf("prompt: %s id %q must be lowercase letters, digits, - or _", group, l.ID)
	case seen[l.ID]:
		return fmt.Errorf("prompt: %s id %q appears twice", group, l.ID)
	case strings.TrimSpace(l.Line) == "":
		return fmt.Errorf("prompt: %s line %q is empty", group, l.ID)
	}
	return nil
}

// lines returns the slice for an id-bearing group, or nil for any other.
func (p *Plan) lines(group string) *[]Line {
	switch group {
	case GroupData:
		return &p.Data
	case GroupAutomation:
		return &p.Automation
	case GroupScreens:
		return &p.Screens
	}
	return nil
}

func (p Plan) clone() Plan {
	p.Data = append([]Line(nil), p.Data...)
	p.Automation = append([]Line(nil), p.Automation...)
	p.Screens = append([]Line(nil), p.Screens...)
	return p
}

// ApplyPlanEdits applies the edits in order to a copy of plan. Any bad edit
// fails the whole batch and leaves the original untouched.
func ApplyPlanEdits(plan Plan, edits []Edit) (Plan, error) {
	out := plan.clone()
	for i, e := range edits {
		if err := out.apply(e); err != nil {
			return plan, fmt.Errorf("prompt: edit %d: %w", i+1, err)
		}
	}
	return out, nil
}

func (p *Plan) apply(e Edit) error {
	if e.Group == GroupAgent {
		return p.editAgent(e)
	}
	lines := p.lines(e.Group)
	if lines == nil {
		return fmt.Errorf("unknown group %q", e.Group)
	}
	switch e.Op {
	case OpSet:
		return setLine(lines, e)
	case OpAdd:
		return addLine(lines, e)
	case OpRemove:
		return removeLine(lines, e)
	}
	return fmt.Errorf("unknown op %q", e.Op)
}

func (p *Plan) editAgent(e Edit) error {
	if e.Op != OpSet {
		return fmt.Errorf("the agent line can only be set")
	}
	if strings.TrimSpace(e.Line) == "" {
		return fmt.Errorf("the agent line cannot be empty")
	}
	p.Agent.Line = e.Line
	return nil
}

func indexOf(lines []Line, id string) int {
	for i, l := range lines {
		if l.ID == id {
			return i
		}
	}
	return -1
}

func setLine(lines *[]Line, e Edit) error {
	i := indexOf(*lines, e.ID)
	if i < 0 {
		return fmt.Errorf("no %s line with id %q", e.Group, e.ID)
	}
	if strings.TrimSpace(e.Line) == "" {
		return fmt.Errorf("line %q cannot be empty", e.ID)
	}
	(*lines)[i].Line = e.Line
	return nil
}

func addLine(lines *[]Line, e Edit) error {
	if err := checkLine(e.Group, Line{e.ID, e.Line}, map[string]bool{}); err != nil {
		return err
	}
	if indexOf(*lines, e.ID) >= 0 {
		return fmt.Errorf("%s id %q already exists", e.Group, e.ID)
	}
	*lines = append(*lines, Line{e.ID, e.Line})
	return nil
}

func removeLine(lines *[]Line, e Edit) error {
	i := indexOf(*lines, e.ID)
	if i < 0 {
		return fmt.Errorf("no %s line with id %q", e.Group, e.ID)
	}
	*lines = append((*lines)[:i], (*lines)[i+1:]...)
	return nil
}

// silent carries a sentinel through %w without adding to the message.
type silent struct{ err error }

func (silent) Error() string   { return "" }
func (s silent) Unwrap() error { return s.err }
