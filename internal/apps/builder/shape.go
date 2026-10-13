package builder

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/builder/prompt"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

type Shaper struct{ builder *Service }

var _ svc.Shaper = (*Shaper)(nil)

func NewShaper(cfg svc.Config, all *svc.Services) *Shaper { return &Shaper{builder: New(cfg, all)} }

func (s *Shaper) Shape(ctx context.Context, in svc.ShapeInput) (svc.ShapeOutput, error) {
	for name := range in.Files {
		if !prompt.ValidPath(name) {
			return svc.ShapeOutput{}, fmt.Errorf("shape cannot read %q", name)
		}
	}
	if s.builder.all == nil || s.builder.all.Model == nil {
		return svc.ShapeOutput{}, bridge.NotBuilt("model")
	}
	report, err := json.Marshal(in.Report)
	if err != nil {
		return svc.ShapeOutput{}, err
	}
	sample, err := json.Marshal(in.Sample)
	if err != nil {
		return svc.ShapeOutput{}, err
	}
	text, err := prompt.RenderShape(prompt.Inputs{Request: in.Request, Files: asText(in.Files), Catalog: catalogText(), Report: string(report)}, string(sample))
	if err != nil {
		return svc.ShapeOutput{}, err
	}
	b := &build{app: in.App, model: s.builder.model, budget: shapeBudget(in.Files["app.json"])}
	out, err := converse(ctx, s.builder, b, text, parseShape)
	out.Cost = b.cost().Spent
	return out, err
}

func shapeBudget(raw []byte) float64 {
	var app struct {
		Budget struct {
			Month float64 `json:"month"`
		} `json:"budget"`
	}
	if json.Unmarshal(raw, &app) == nil && app.Budget.Month > 0 {
		return app.Budget.Month
	}
	return defaultBudget
}

func parseShape(reply string) (svc.ShapeOutput, error) {
	var envelope struct {
		Files    json.RawMessage       `json:"files"`
		Summary  string                `json:"summary"`
		Question *bridge.ShapeQuestion `json:"question"`
	}
	// The shared file parser also accepts older direct file-map replies.
	raw := strings.TrimSpace(reply)
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(strings.TrimSpace(raw), "```")
	if json.Unmarshal([]byte(raw), &envelope) == nil {
		if envelope.Question != nil {
			if len(envelope.Files) != 0 {
				return svc.ShapeOutput{}, fmt.Errorf("a question cannot include files")
			}
			if err := validateQuestion(envelope.Question); err != nil {
				return svc.ShapeOutput{}, err
			}
			return svc.ShapeOutput{Question: envelope.Question}, nil
		}
		if len(envelope.Files) != 0 {
			reply = string(envelope.Files)
		}
	}
	files, err := prompt.ParseFiles(reply)
	return svc.ShapeOutput{Files: files, Summary: envelope.Summary}, err
}

func validateQuestion(q *bridge.ShapeQuestion) error {
	if strings.TrimSpace(q.Text) == "" || len(q.Options) < 2 {
		return fmt.Errorf("a question needs text and concrete alternatives")
	}
	ids := map[string]bool{}
	for _, option := range q.Options {
		if option.ID == "" || option.Label == "" || ids[option.ID] {
			return fmt.Errorf("question options need unique ids and labels")
		}
		ids[option.ID] = true
	}
	last := q.Options[len(q.Options)-1]
	if last.ID != "drop" || last.Label != "Drop this request" {
		return fmt.Errorf("Drop this request must be the last option")
	}
	return nil
}
