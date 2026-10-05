package telemetry

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestUsageReceiptMissingDoesNotInventZeroTokens(t *testing.T) {
	testHome(t)
	event := UsageReceipt(ModeTask, 100, 20, "run", testNow, UsageDimensions{UsageStatus: "missing", AccountingSource: "provider"})
	for _, key := range []string{"input_tokens", "output_tokens", "total_tokens"} {
		if _, exists := event.Props[key]; exists {
			t.Fatalf("missing receipt contains %s", key)
		}
	}
	if event.Props["usage_status"] != "missing" {
		t.Fatal(event.Props)
	}
	zero := UsageReceipt(ModeTask, 0, 0, "run", testNow, UsageDimensions{UsageStatus: "reported"})
	if zero.Props["total_tokens"] != 0 {
		t.Fatal("known zero was discarded")
	}
}

func TestUsageReceiptReplayIdentityAndPrivacy(t *testing.T) {
	testHome(t)
	dimensions := UsageDimensions{RoutingProvider: "private.example", ModelFamily: "secret-model", AccountingSource: "private", ReceiptID: "call-id"}
	first := UsageReceipt(ModeTask, 5, 2, "run", testNow, dimensions)
	again := UsageReceipt(ModeTask, 5, 2, "run", testNow, dimensions)
	other := UsageReceipt(ModeTask, 5, 2, "other-run", testNow, dimensions)
	if first.ID != again.ID || first.ID == other.ID {
		t.Fatal("receipt identity is not stable and session scoped")
	}
	body, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"private.example", "secret-model", "call-id"} {
		if strings.Contains(string(body), secret) {
			t.Fatalf("private value escaped: %s", secret)
		}
	}
}

func TestRoutingProviderSeparatesRouterFromModelVendor(t *testing.T) {
	cases := map[string]string{
		"https://openrouter.ai/api/v1": "openrouter", "https://openrouter.ai.evil.test/v1": "other",
		"https://api.openai.com/v1": "openai", "https://api.anthropic.com": "anthropic",
		"https://generativelanguage.googleapis.com": "google", "https://east.openai.azure.com": "azure",
		"https://bedrock-runtime.us-east-1.amazonaws.com": "bedrock", "http://localhost:11434": "other", "": "unknown",
	}
	for endpoint, want := range cases {
		if got := RoutingProvider(endpoint); got != want {
			t.Errorf("%q: %s, want %s", endpoint, got, want)
		}
	}
	if ModelFamily("anthropic/claude-sonnet-4") != "claude" || ModelFamily("deepseek/deepseek-v4.1-flash") != "deepseek" || ModelFamily("private/my-custom-model") != "other" {
		t.Fatal("model families were not bounded")
	}
}

func TestCountUsagePersistsMissingReceiptBeforeReturn(t *testing.T) {
	testHome(t)
	finish := BeginUsageSession(ModeTask, "run")
	defer finish()
	CountUsage(0, 0, UsageDimensions{UsageStatus: "missing", AccountingSource: "provider"})
	rows := SpoolContents()
	if len(rows) != 1 {
		t.Fatalf("missing receipt was not synchronously persisted: %d rows", len(rows))
	}
}

func TestCountUsageKeepsKnownZeroReceipt(t *testing.T) {
	testHome(t)
	finish := BeginUsageSession(ModeTask, "run")
	defer finish()
	CountUsage(0, 0, UsageDimensions{UsageStatus: "reported", AccountingSource: "provider"})
	if rows := SpoolContents(); len(rows) != 1 {
		t.Fatalf("known-zero receipt was lost: %d rows", len(rows))
	}
}

func TestLateUsageReceiptKeepsOriginalSessionAndCountsOnce(t *testing.T) {
	testHome(t)
	finish := BeginUsageSession(ModeTask, "original")
	record := CaptureUsageRecorder(UsageDimensions{UsageStatus: "reported", AccountingSource: "provider", ReceiptID: "generation:one"})
	finish()
	finishNext := BeginUsageSession(ModeChat, "next")
	defer finishNext()
	record(10, 2)
	record(10, 2)
	rows := SpoolContents()
	if len(rows) != 1 {
		t.Fatalf("late receipt counted %d times", len(rows))
	}
	var row jsonEvent
	if err := json.Unmarshal(rows[0], &row); err != nil {
		t.Fatal(err)
	}
	if row.SessionIDHash != hashHex("original") || row.Props["mode"] != "task" || row.Props["total_tokens"] != float64(12) {
		t.Fatal(row)
	}
}
