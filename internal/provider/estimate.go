package provider

import (
	"context"
	"encoding/json"
	"math"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

type costAdmissionKey struct{}

// WithCostAdmission reserves each actual send, including retries and rescue arms.
// The callback must be concurrency-safe and refuse unknown prices or ceilings.
func WithCostAdmission(ctx context.Context, admit func(float64, bool) error) context.Context {
	return context.WithValue(ctx, costAdmissionKey{}, admit)
}

// admitCost reads the encoded ceiling, so a relaxed request cannot escape a cap.
func (c *Client) admitCost(ctx context.Context, model string, body []byte) error {
	admit, _ := ctx.Value(costAdmissionKey{}).(func(float64, bool) error)
	if admit == nil {
		return nil
	}
	var shape struct {
		MaxTokens           *int `json:"max_tokens"`
		MaxCompletionTokens *int `json:"max_completion_tokens"`
	}
	if json.Unmarshal(body, &shape) != nil || c.config.ModelPrice == nil {
		return admit(0, false)
	}
	ceiling := shape.MaxTokens
	if shape.MaxCompletionTokens != nil {
		ceiling = shape.MaxCompletionTokens
	}
	in, out, known := c.config.ModelPrice(model)
	if !known || ceiling == nil || *ceiling <= 0 || in < 0 || out < 0 || math.IsNaN(in+out) || math.IsInf(in+out, 0) {
		return admit(0, false)
	}
	return admit(float64(len(body))*in+float64(*ceiling)*out, true)
}

// EstimateCompletionCost is a conservative admission estimate, never a receipt.
// Counting every encoded byte as a token includes message framing and schemas;
// the output ceiling uses the same thinking allowance as the actual encoder.
// An unpublished price refuses admission rather than pretending the call is free.
func (c *Client) EstimateCompletionCost(ctx context.Context, messages []ai.Message, options ...ai.Option) (float64, bool, error) {
	request, err := c.newRequest(messages, options)
	if err != nil {
		return 0, false, err
	}
	if c.config.ModelPrice == nil {
		return 0, false, nil
	}
	in, out, known := c.config.ModelPrice(c.config.Model)
	if !known || in < 0 || out < 0 || math.IsNaN(in+out) || math.IsInf(in+out, 0) {
		return 0, false, nil
	}
	ceiling, bounded := c.ceilingFor(request, knobsFrom(ctx))
	if !bounded {
		return 0, false, nil
	}
	raw, err := json.Marshal(request)
	return float64(len(raw))*in + float64(ceiling)*out, true, err
}
