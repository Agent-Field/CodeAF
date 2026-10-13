package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/builder/prompt"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
)

const (
	space  = "personal"
	budget = "up to $20 a month"
)

// connected stands in for the accounts an owner has in Settings → Accounts.
var connected = []string{"gmail"}

// result is one preset × model run, as printed and as saved to result.json.
type result struct {
	Preset    string         `json:"preset"`
	Model     string         `json:"model"`
	PlanSec   float64        `json:"plan_sec"`
	WriteSec  float64        `json:"write_sec"`
	FixSec    float64        `json:"fix_sec"`
	FixRounds int            `json:"fix_rounds"`
	Cost      float64        `json:"cost"`
	Pass      bool           `json:"pass"`
	Issues    []string       `json:"issues,omitempty"`
	Notes     []string       `json:"notes,omitempty"`
	Error     string         `json:"error,omitempty"`
	Files     spec.Files     `json:"-"`
	Sizes     map[string]int `json:"sizes,omitempty"`
}

// pipeline holds what every run shares: the model door and the prompt inputs.
type pipeline struct {
	model   completer
	kit     *spec.Kit
	catalog string
	schemas string
	rounds  int
}

func newPipeline(model completer, rounds int) (*pipeline, error) {
	kit, err := spec.LoadKit()
	if err != nil {
		return nil, err
	}
	schemas, err := schemasText()
	if err != nil {
		return nil, err
	}
	return &pipeline{model: model, kit: kit, catalog: catalogText(kit), schemas: schemas, rounds: rounds}, nil
}

func (p *pipeline) inputs(paragraph string) prompt.Inputs {
	return prompt.Inputs{Paragraph: paragraph, Space: space, Budget: budget, Connectors: connected, Catalog: p.catalog, Schemas: p.schemas}
}

// run is PLAN, WRITE, then FIX until the files verify or the rounds are spent.
func (p *pipeline) run(ctx context.Context, name, paragraph, model string) *result {
	res := &result{Preset: name, Model: model}
	if err := p.build(ctx, res, paragraph); err != nil {
		res.Error = err.Error()
	}
	return res
}

func (p *pipeline) build(ctx context.Context, res *result, paragraph string) error {
	in := p.inputs(paragraph)
	planText, err := p.plan(ctx, res, in)
	if err != nil {
		return err
	}
	in.Plan = planText
	files, err := p.write(ctx, res, in)
	if err != nil {
		return err
	}
	files["plan.json"] = []byte(planText)
	return p.fixLoop(ctx, res, in, files)
}

func (p *pipeline) call(ctx context.Context, res *result, kind prompt.Kind, in prompt.Inputs) (string, error) {
	text, err := prompt.Render(kind, in)
	if err != nil {
		return "", err
	}
	c, err := p.model.complete(ctx, res.Model, text)
	res.Cost += c.Cost
	switch kind {
	case prompt.KindPlan:
		res.PlanSec = c.Elapsed.Seconds()
	case prompt.KindWrite:
		res.WriteSec = c.Elapsed.Seconds()
	default:
		res.FixSec += c.Elapsed.Seconds()
	}
	return c.Text, err
}

// plan returns plan.json text. It keeps the model's own object because the
// prompt package's Plan type has no name field.
func (p *pipeline) plan(ctx context.Context, res *result, in prompt.Inputs) (string, error) {
	out, err := p.call(ctx, res, prompt.KindPlan, in)
	if err != nil {
		return "", err
	}
	if _, err := prompt.ParsePlan(out); err != nil {
		return "", fmt.Errorf("plan: %w", err)
	}
	return finishPlan(out, in.Paragraph)
}

// finishPlan stamps the fields the model must not decide: the schema, the
// owner's words verbatim, and needs.
func finishPlan(out, paragraph string) (string, error) {
	obj, err := firstObject(out, "data")
	if err != nil {
		return "", err
	}
	obj["$schema"] = spec.PlanSchemaURL
	obj["ask"] = paragraph
	obj["needs"] = "computed"
	if agent, ok := obj["agent"].(map[string]any); ok {
		agent["id"] = "agent"
	}
	text, err := json.MarshalIndent(obj, "", "  ")
	return string(text), err
}

// firstObject finds the first JSON object in out that has key, skipping any
// prose or fence around it.
func firstObject(out, key string) (map[string]any, error) {
	for rest := out; ; {
		start := strings.IndexByte(rest, '{')
		if start < 0 {
			return nil, fmt.Errorf("no JSON object with %q in the reply", key)
		}
		rest = rest[start:]
		dec := json.NewDecoder(strings.NewReader(rest))
		var obj map[string]any
		if err := dec.Decode(&obj); err != nil {
			rest = rest[1:]
			continue
		}
		if _, ok := obj[key]; ok {
			return obj, nil
		}
		rest = rest[dec.InputOffset():]
	}
}

func (p *pipeline) write(ctx context.Context, res *result, in prompt.Inputs) (spec.Files, error) {
	out, err := p.call(ctx, res, prompt.KindWrite, in)
	if err != nil {
		return nil, err
	}
	return parseFiles(out)
}

func parseFiles(out string) (spec.Files, error) {
	raw, err := prompt.ParseFiles(out)
	if err != nil {
		return nil, err
	}
	files := spec.Files{}
	for path, data := range raw {
		files[path] = data
	}
	return files, nil
}

func (p *pipeline) fixLoop(ctx context.Context, res *result, in prompt.Inputs, files spec.Files) error {
	for {
		v := verify(files, p.kit)
		res.Issues, res.Notes, res.Pass, res.Files = v.Issues, v.Notes, v.pass(), files
		if v.pass() || res.FixRounds >= p.rounds {
			return nil
		}
		res.FixRounds++
		changed, err := p.fix(ctx, res, in, files, v)
		if err != nil {
			return err
		}
		for path, data := range changed {
			files[path] = data
		}
	}
}

func (p *pipeline) fix(ctx context.Context, res *result, in prompt.Inputs, files spec.Files, v verdict) (spec.Files, error) {
	in.Files = map[string]string{}
	for path, data := range files {
		in.Files[path] = string(data)
	}
	in.Report = report(v.Issues)
	out, err := p.call(ctx, res, prompt.KindFix, in)
	if err != nil {
		return nil, err
	}
	return parseFiles(out)
}
