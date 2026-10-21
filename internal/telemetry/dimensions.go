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
	source := dimensions.AccountingSource
	if source != "provider" {
		source = "engine"
	}
	event.Props["accounting_source"] = source
	// Status can describe absence for older callers, but the final status is
	// derived from the safe components that actually survive serialization.
	missingInput := dimensions.MissingInput || dimensions.UsageStatus == "missing" || dimensions.UsageStatus == "estimated"
	missingOutput := dimensions.MissingOutput || dimensions.UsageStatus == "missing" || dimensions.UsageStatus == "estimated"
	for _, key := range []string{"input_tokens", "output_tokens", "total_tokens"} {
		delete(event.Props, key)
	}
	measured := [2]*int{}
	if !missingInput && safeUsageCount(input) {
		measured[0] = &input
	}
	if !missingOutput && safeUsageCount(output) {
		measured[1] = &output
	}
	estimated := [2]*int{}
	if dimensions.ReceiptID != "" {
		for i, value := range [2]*int{dimensions.EstimatedInput, dimensions.EstimatedOutput} {
			if measured[i] == nil && value != nil && safeUsageCount(*value) {
				estimated[i] = value
			}
		}
	}
	// A pair whose sum is unsafe cannot be sent with a relay-valid total.
	// Leave that group unknown rather than clamp either component to fiction.
	measuredCount := emitUsageComponents(event.Props, "", measured)
	estimatedCount := emitUsageComponents(event.Props, "estimated_", estimated)
	status := "missing"
	switch {
	case measuredCount > 0 && estimatedCount > 0:
		status = "mixed"
	case measuredCount == 2:
		status = "reported"
	case measuredCount == 1:
		status = "partial"
	case estimatedCount > 0:
		status = "estimated"
	case dimensions.ReportedTotal != nil && safeUsageCount(*dimensions.ReportedTotal):
		event.Props["total_tokens"] = *dimensions.ReportedTotal
		status = "total_only"
	}
	event.Props["usage_status"] = status
	if estimatedCount > 0 {
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

// safeUsageCount matches the relay's nonnegative JavaScript safe integers.
func safeUsageCount(value int) bool {
	return value >= 0 && uint64(value) <= 1<<53-1
}

func emitUsageComponents(props map[string]any, prefix string, values [2]*int) int {
	total, count := 0, 0
	for _, value := range values {
		if value != nil {
			if uint64(total)+uint64(*value) > 1<<53-1 {
				return 0
			}
			total += *value
			count++
		}
	}
	if count > 0 {
		for i, key := range []string{"input_tokens", "output_tokens"} {
			if values[i] != nil {
				props[prefix+key] = *values[i]
			}
		}
		props[prefix+"total_tokens"] = total
	}
	return count
}
