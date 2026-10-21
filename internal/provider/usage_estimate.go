package provider

import (
	"encoding/json"
	"strings"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/telemetry"
)

// usageObservation retains scalar observations for one attempt. Estimates never
// enter the billing ledger, model response, price limits, or provider totals.
type usageObservation struct {
	inputBytes     int
	inputTextOnly  bool
	outputBytes    int
	outputObserved bool
	usageParsed    bool
	inputReported  bool
	outputReported bool
	totalReported  bool
}

// UnmarshalJSON preserves absent versus explicit zero counts. Cost-only usage
// and a one-sided receipt must not turn an unknown token count into zero.
func (u *usageWire) UnmarshalJSON(data []byte) error {
	type plain usageWire
	var decoded plain
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*u = usageWire(decoded)
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	read := func(names ...string) (int, bool) {
		for _, name := range names {
			raw, exists := fields[name]
			if !exists || string(raw) == "null" {
				continue
			}
			var value int
			if json.Unmarshal(raw, &value) == nil && value >= 0 {
				return value, true
			}
		}
		return 0, false
	}
	if value, known := read("prompt_tokens", "input_tokens"); known {
		u.PromptTokens, u.inputReported = value, true
		// Native Anthropic input excludes its separately reported cache work.
		if _, canonical := fields["prompt_tokens"]; !canonical {
			u.PromptTokens += u.CacheReadInputTokens + u.CacheCreationInputTokens
		}
	}
	if value, known := read("completion_tokens", "output_tokens"); known {
		u.CompletionTokens, u.outputReported = value, true
	}
	if value, known := read("total_tokens"); known {
		u.TotalTokens, u.totalReported = value, true
	}
	return nil
}

func (o *usageObservation) observeUsage(u *usageWire) {
	o.usageParsed = true
	if u != nil {
		o.inputReported = o.inputReported || u.inputReported
		o.outputReported = o.outputReported || u.outputReported
		o.totalReported = o.totalReported || u.totalReported
	}
}

func usageInputBytes(messages, tools []json.RawMessage, source []ai.Message) (int, bool) {
	for _, message := range source {
		for _, part := range message.Content {
			if part.Type != "text" {
				return 0, false
			}
		}
	}
	size := 0
	for _, message := range messages {
		size += len(message)
	}
	for _, tool := range tools {
		size += len(tool)
	}
	return size, len(messages) > 0
}

func (o *usageObservation) observeDelta(delta streamDelta) {
	o.outputObserved = true
	o.outputBytes += len(delta.Content)
	// Some gateways duplicate the same thought in multiple aliases. Use one
	// textual channel, not the JSON wrapper or opaque encrypted signatures.
	o.outputBytes += max(len(delta.Reasoning), len(delta.ReasoningContent), len(delta.ReasoningText))
	for _, call := range delta.ToolCalls {
		o.outputBytes += len(call.Function.Name) + len(call.Function.Arguments)
	}
}

func (o *usageObservation) observeWhole(payload []byte) {
	var wire struct {
		Choices []struct {
			Message json.RawMessage `json:"message"`
		} `json:"choices"`
	}
	if json.Unmarshal(payload, &wire) != nil {
		return
	}
	for _, choice := range wire.Choices {
		var message ai.Message
		if json.Unmarshal(choice.Message, &message) != nil {
			continue
		}
		o.outputObserved = true
		for _, part := range message.Content {
			if part.Type == "text" {
				o.outputBytes += len(part.Text)
			}
		}
		for _, call := range message.ToolCalls {
			o.outputBytes += len(call.Function.Name) + len(call.Function.Arguments)
		}
		// Decode reasoning separately so a valid array of text parts in content
		// does not prevent its visible reasoning channel from being counted.
		var thought struct {
			Reasoning string `json:"reasoning"`
			Content   string `json:"reasoning_content"`
			Text      string `json:"reasoning_text"`
		}
		if json.Unmarshal(choice.Message, &thought) == nil {
			o.outputBytes += max(len(thought.Reasoning), len(thought.Content), len(thought.Text))
		}
	}
}

// approximateUsageTokens is a versioned heuristic, not a tokenizer. UTF-8 byte
// counts include message and tool serialization overhead on input and observed
// content, tool arguments and visible thinking on output. Non-text input is
// unknown; hidden reasoning, unseen output, and media are never invented.
func approximateUsageTokens(size int) int {
	if size <= 0 {
		return 0
	}
	return 1 + (size-1)/4
}

func (c *Client) usageDimensions(facts recordFacts, attemptID, model string) telemetry.UsageDimensions {
	base := facts.baseFor(c.config.BaseURL)
	id := attemptID
	if facts.response != nil && facts.response.ID != "" {
		id = "generation:" + base + ":" + facts.response.ID
	}
	d := telemetry.UsageDimensions{RoutingProvider: c.usageRoutingProvider(base), ModelFamily: telemetry.ModelFamily(model), AccountingSource: "provider", ReceiptID: id}
	o := usageObservation{}
	if facts.knobs.trace != nil {
		o = facts.knobs.trace.usageObservation
	}
	reported := facts.response != nil && facts.response.Usage != nil
	if o.usageParsed {
		d.MissingInput, d.MissingOutput = !o.inputReported, !o.outputReported
	} else {
		d.MissingInput, d.MissingOutput = !reported, !reported
	}
	// A provider may report only a total. Keep that measurement intact and do
	// not invent an input/output split or add estimates to a known total.
	if reported && o.totalReported && d.MissingInput && d.MissingOutput {
		total := facts.response.Usage.TotalTokens
		d.ReportedTotal = &total
		d.UsageStatus = "total_only"
		return d
	}
	// A request that was rejected or never accepted proves no consumed work.
	// An interrupted 2xx stream may still contribute observed output, explicitly
	// marked partial; it is never extrapolated to a hypothetical completed reply.
	accepted := facts.status >= 200 && facts.status < 300 && facts.response != nil
	if accepted {
		if d.MissingInput && o.inputTextOnly {
			estimate := approximateUsageTokens(o.inputBytes)
			d.EstimatedInput = &estimate
		}
		if d.MissingOutput && o.outputObserved {
			estimate := approximateUsageTokens(o.outputBytes)
			d.EstimatedOutput = &estimate
		}
	}
	if d.EstimatedInput != nil || d.EstimatedOutput != nil {
		d.EstimateScope = "text_only"
		if !o.inputTextOnly {
			d.EstimateScope = "multimodal_input"
		}
		if facts.err != nil {
			d.EstimateScope = "observed_partial"
		}
	}
	switch {
	case !d.MissingInput && !d.MissingOutput:
		d.UsageStatus = "reported"
	case d.EstimatedInput != nil || d.EstimatedOutput != nil:
		d.UsageStatus = "estimated"
		if !d.MissingInput || !d.MissingOutput {
			d.UsageStatus = "mixed"
		}
	case !d.MissingInput || !d.MissingOutput:
		d.UsageStatus = "partial"
	default:
		d.UsageStatus = "missing"
	}
	return d
}

// usageEndpoint follows the actual attempt when a billing road changed.
func (c *Client) usageEndpoint(knobs callKnobs) string {
	if knobs.trace != nil && knobs.trace.attemptBase != "" {
		return knobs.trace.attemptBase
	}
	return c.config.BaseURL
}

func (c *Client) streamUsageSupported(knobs callKnobs) bool {
	_, refused := c.streamUsageUnsupported.Load(c.usageEndpoint(knobs))
	return !refused
}

// Only a refusal naming the option itself permits this repair. An unrelated
// validation error must not silently turn off accounting or duplicate work.
func refusesStreamUsage(body []byte) bool {
	text := strings.ToLower(string(body))
	if !strings.Contains(text, "stream_options") && !strings.Contains(text, "include_usage") {
		return false
	}
	for _, phrase := range []string{"unsupported", "not supported", "unrecognized", "unknown", "not permitted", "extra inputs", "extra field", "unexpected"} {
		if strings.Contains(text, phrase) {
			return true
		}
	}
	return false
}
