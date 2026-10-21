package telemetry

import (
	"net/url"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/modelsource"
)

// UsageDimensions contains only bounded diagnostic categories. ReceiptID is
// used locally to derive a replay-stable event identity and is never sent raw.
type UsageDimensions struct {
	RoutingProvider  string
	ModelFamily      string
	UsageStatus      string
	AccountingSource string
	ReceiptID        string
	MissingInput     bool
	MissingOutput    bool
	EstimatedInput   *int
	EstimatedOutput  *int
	EstimateScope    string
	ReportedTotal    *int
}

// category refuses arbitrary caller strings even when a caller bypasses the
// normal classifier. Private endpoints and custom model names stay local.
func category(value string, choices string) string {
	for _, choice := range strings.Fields(choices) {
		if value == choice {
			return value
		}
	}
	return "unknown"
}

// RoutingProvider names the service receiving the request, not the vendor
// of the model it serves. No URL or custom hostname is returned.
func RoutingProvider(endpoint string) string {
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Hostname() == "" {
		return "unknown"
	}
	host := strings.ToLower(parsed.Hostname())
	switch host {
	case "openrouter.ai":
		return modelsource.DefaultID
	case "api.openai.com":
		return "openai"
	case "api.anthropic.com":
		return "anthropic"
	case "generativelanguage.googleapis.com":
		return "google"
	case "localhost", "127.0.0.1", "::1":
		return "other"
	}
	if strings.HasSuffix(host, ".openai.azure.com") {
		return "azure"
	}
	if strings.HasSuffix(host, ".amazonaws.com") && strings.HasPrefix(host, "bedrock-runtime.") {
		return "bedrock"
	}
	return "other"
}

// ModelFamily reduces a model slug to a public family. The exact name,
// variant and any operator-defined suffix are never part of the event.
func ModelFamily(model string) string {
	model = strings.ToLower(strings.TrimSpace(model))
	if model == "" {
		return "unknown"
	}
	_, name, hasVendor := strings.Cut(model, "/")
	if hasVendor {
		model = name
	}
	for _, family := range strings.Fields("deepseek claude gpt gemini llama qwen kimi glm") {
		suffix := strings.TrimPrefix(model, family)
		if model == family || (suffix != model && suffix != "" && (suffix[0] == '-' || suffix[0] == '.' || (suffix[0] >= '0' && suffix[0] <= '9'))) {
			return family
		}
	}
	return "other"
}

// UsageReceipt extends a token delta with categories that make missing usage
// visible without collecting raw model names or endpoint addresses.
func UsageReceipt(mode Mode, input, output int, sessionID string, now time.Time, dimensions UsageDimensions) Event {
	event := UsageDelta(mode, input, output, sessionID, now)
	event.Props["routing_provider"] = category(dimensions.RoutingProvider, "openrouter openai anthropic google bedrock azure ollama other unknown")
	event.Props["model_family"] = category(dimensions.ModelFamily, "deepseek claude gpt gemini llama qwen kimi glm other unknown")
	status := category(dimensions.UsageStatus, "reported mixed estimated partial missing total_only")
	if status == "unknown" {
		status = "reported"
	}
	event.Props["usage_status"] = status
	source := dimensions.AccountingSource
	if source != "provider" {
		source = "engine"
	}
	event.Props["accounting_source"] = source
	missingInput := dimensions.MissingInput || status == "missing" || status == "estimated"
	missingOutput := dimensions.MissingOutput || status == "missing" || status == "estimated"
	if missingInput {
		delete(event.Props, "input_tokens")
	}
	if missingOutput {
		delete(event.Props, "output_tokens")
	}
	if missingInput && missingOutput {
		delete(event.Props, "total_tokens")
		if dimensions.ReportedTotal != nil && status == "total_only" {
			event.Props["total_tokens"] = max(0, *dimensions.ReportedTotal)
		}
	} else {
		measured := 0
		if !missingInput {
			measured += max(0, input)
		}
		if !missingOutput {
			measured += max(0, output)
		}
		event.Props["total_tokens"] = measured
	}
	estimated := 0
	hasEstimate := false
	if dimensions.EstimatedInput != nil && missingInput {
		value := max(0, *dimensions.EstimatedInput)
		event.Props["estimated_input_tokens"] = value
		estimated += value
		hasEstimate = true
	}
	if dimensions.EstimatedOutput != nil && missingOutput {
		value := max(0, *dimensions.EstimatedOutput)
		event.Props["estimated_output_tokens"] = value
		estimated += value
		hasEstimate = true
	}
	if hasEstimate {
		event.Props["estimated_total_tokens"] = estimated
		event.Props["estimate_method"] = "utf8_bytes_4_v1"
		event.Props["estimate_scope"] = category(dimensions.EstimateScope, "text_only observed_partial multimodal_input")
	}
	if dimensions.ReceiptID != "" {
		receipt := hashHex("usage_receipt|" + sessionID + "|" + dimensions.ReceiptID)
		event.Props["usage_receipt"] = receipt
		// An estimate and its later provider receipt are different events about
		// one receipt. PostHog groups this hash and prefers measured components.
		event.ID = hashHex("usage_delta|" + receipt + "|" + status)[:32]
	}
	return event
}
