package provider

import (
	"bytes"
	"context"
	"testing"

	"github.com/Agent-Field/codeaf/internal/effort"
	"github.com/Agent-Field/codeaf/internal/modelsource"
)

// ── the endpoint that reads its own word ────────────────────────────────────
//
// DeepSeek's first-party API ignores OpenRouter's `reasoning` object and reads a
// top-level `reasoning_effort` of `low`, `high` or `max`. Until its service row
// declared that, every level a person chose ran at DeepSeek's default and max
// was unreachable, while every body looked right to a test that read the
// object. So these assertions read the serialized body, both fields, every rung.

// EVERY RUNG ON THE DEEPSEEK DIALECT, AND WHAT EACH ONE ASKS FOR. Medium rounds
// up to high because the endpoint has no word between low and high, and the
// two rungs above high are max because max is the endpoint's own top word.
func TestEveryRungReachesTheDeepSeekDialectAsItsOwnWord(t *testing.T) {
	for _, want := range []struct {
		rung effort.Rung
		word string
	}{
		{effort.None, ""},
		{effort.Low, "low"},
		{effort.Medium, "high"},
		{effort.High, "high"},
		{effort.XHigh, "max"},
		{effort.Max, "max"},
	} {
		t.Run(want.rung.String(), func(t *testing.T) {
			client, recorded := newTestClient(t, Config{
				Direct:            true,
				ReasoningDialect:  modelsource.ReasoningEffortLowHighMax,
				SupportsParameter: func(string, string) (bool, bool) { return true, true },
			})
			ctx := WithConfiguredEffortRung(context.Background(), want.rung)
			if _, err := client.CompleteWithMessages(ctx, userMessages("think")); err != nil {
				t.Fatal(err)
			}
			body := recorded.body(0)
			if _, present := body["reasoning"]; present {
				t.Fatalf("%s sent the unified object to an endpoint that ignores it: %#v", want.rung, body)
			}
			word, present := body["reasoning_effort"].(string)
			switch {
			case want.word == "" && present:
				t.Fatalf("an unset rung sent reasoning_effort %q, want no field at all", word)
			case want.word != "" && word != want.word:
				t.Fatalf("%s sent reasoning_effort %q, want %q (body %#v)", want.rung, word, want.word, body)
			}
		})
	}
}

// THE BODY AT MAX, BYTE FOR BYTE in the part that matters: the word is there,
// spelled exactly as DeepSeek's listing spells it, and neither half of the
// unified object rides beside it.
func TestTheDeepSeekDialectAtMaxCarriesOnlyItsOwnWord(t *testing.T) {
	client, recorded := newTestClient(t, Config{
		Direct:           true,
		ReasoningDialect: modelsource.ReasoningEffortLowHighMax,
	})
	ctx := WithConfiguredEffortRung(context.Background(), effort.Max)
	if _, err := client.CompleteWithMessages(ctx, userMessages("think")); err != nil {
		t.Fatal(err)
	}
	raw := recorded.raw[0]
	t.Logf("body: %s", raw)
	if !bytes.Contains(raw, []byte(`"reasoning_effort":"max"`)) {
		t.Fatalf("body = %s, want \"reasoning_effort\":\"max\"", raw)
	}
	if bytes.Contains(raw, []byte(`"reasoning":`)) {
		t.Fatalf("body = %s, want no unified reasoning object beside the word", raw)
	}
}

// OFF ON A MODEL THAT CANNOT STOP THINKING keeps the existing answer — the
// lowest level, not nothing — and it is spelled in the endpoint's dialect.
func TestOffOnTheDeepSeekDialectSendsItsLowestWord(t *testing.T) {
	client, recorded := newTestClient(t, Config{
		Direct:            true,
		ReasoningDialect:  modelsource.ReasoningEffortLowHighMax,
		SupportsParameter: func(string, string) (bool, bool) { return true, true },
		ReasoningProfile: func(string) (ReasoningProfile, bool) {
			return ReasoningProfile{Mandatory: true, Default: EffortHigh}, true
		},
	})
	ctx := WithConfiguredReasoningEffort(context.Background(), EffortOff)
	if _, err := client.CompleteWithMessages(ctx, userMessages("think")); err != nil {
		t.Fatal(err)
	}
	body := recorded.body(0)
	if _, present := body["reasoning"]; present {
		t.Fatalf("off sent the unified object: %#v", body)
	}
	if word, _ := body["reasoning_effort"].(string); word != "low" {
		t.Fatalf("off sent reasoning_effort %q, want low (body %#v)", word, body)
	}
}

// THE ROUTER'S BODY DID NOT MOVE. A client that declares no dialect — every
// OpenRouter client, including one asking for a `deepseek/...` model — sends
// the unified object exactly as it did before the dialect existed, and never
// the top-level word.
func TestTheUnifiedDialectBodyIsUnchangedForEveryRung(t *testing.T) {
	for _, want := range []struct {
		rung effort.Rung
		json string
	}{
		{effort.Low, `"reasoning":{"effort":"low"}`},
		{effort.Medium, `"reasoning":{"effort":"medium"}`},
		{effort.High, `"reasoning":{"effort":"high"}`},
		{effort.XHigh, `"reasoning":{"max_tokens":32000}`},
		{effort.Max, `"reasoning":{"max_tokens":64000}`},
	} {
		t.Run(want.rung.String(), func(t *testing.T) {
			client, recorded := newTestClient(t, Config{
				Model:             "deepseek/deepseek-v4-pro",
				SupportsParameter: func(string, string) (bool, bool) { return true, true },
			})
			ctx := WithConfiguredEffortRung(context.Background(), want.rung)
			if _, err := client.CompleteWithMessages(ctx, userMessages("think")); err != nil {
				t.Fatal(err)
			}
			raw := recorded.raw[0]
			if !bytes.Contains(raw, []byte(want.json)) {
				t.Fatalf("%s body = %s, want %s", want.rung, raw, want.json)
			}
			if bytes.Contains(raw, []byte(`reasoning_effort`)) {
				t.Fatalf("%s body = %s, want no reasoning_effort on the router", want.rung, raw)
			}
		})
	}
}

// The word mapping as a table, including the levels only the catalog or the
// running-effort floor can name, so a future level reaches a decided word.
func TestReasoningEffortForCoversEveryLevel(t *testing.T) {
	for _, want := range []struct {
		level  Effort
		stated bool
		word   string
	}{
		{EffortNone, false, ""},
		{EffortOff, false, "low"},
		{EffortMinimal, false, "low"},
		{EffortLow, false, "low"},
		{EffortMedium, false, "high"},
		{EffortHigh, false, "high"},
		{EffortHigh, true, "max"},
		{"xhigh", false, "max"},
		{"max", false, "max"},
	} {
		if got := reasoningEffortFor(want.level, want.stated); got != want.word {
			t.Errorf("reasoningEffortFor(%q, %v) = %q, want %q", want.level, want.stated, got, want.word)
		}
	}
}
