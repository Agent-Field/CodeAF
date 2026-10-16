package appwire

import (
	"math"
	"strings"
	"testing"
)

type balanceEngine struct {
	fakeEngine
	asks  []ProviderBalanceAsk
	reply ProviderBalance
}

func (b *balanceEngine) ProviderBalance(ask ProviderBalanceAsk) (ProviderBalance, error) {
	b.asks = append(b.asks, ask)
	return b.reply, nil
}

func TestProviderBalanceIsUnknownMethodWithoutTheCapability(t *testing.T) {
	frame := serveOver(t, &fakeEngine{})(`{"id":1,"method":"providers.balance","params":{"provider":"openrouter"}}`)
	if frameCode(frame) != CodeUnknownMethod {
		t.Fatalf("%v", frame)
	}
}

func TestProviderBalanceOnTheWireOmitsWhatWasNotReported(t *testing.T) {
	remaining := 2.5
	engine := &balanceEngine{reply: ProviderBalance{Provider: "openrouter", Supported: true, Status: BalanceKnown, RemainingUSD: &remaining}}
	frame := serveOver(t, engine)(`{"id":1,"method":"providers.balance","params":{"provider":" OpenRouter "}}`)
	result, ok := frame["result"].(map[string]any)
	if !ok || len(engine.asks) != 1 || engine.asks[0].Provider != "openrouter" {
		t.Fatalf("%v %+v", frame, engine.asks)
	}
	if result["remaining_usd"] != 2.5 || result["supported"] != true || result["status"] != "known" {
		t.Fatalf("%v", result)
	}
	for _, absent := range []string{"account_remaining_usd", "key_limit_remaining_usd", "last_error_at", "error_kind", "stale", "busy", "retry_after_ms", "message"} {
		if _, there := result[absent]; there {
			t.Fatalf("%q present: %v", absent, result)
		}
	}
}

func TestProviderBalanceParamsAreStrict(t *testing.T) {
	engine := &balanceEngine{reply: ProviderBalance{Provider: "x", Status: BalanceUnavailable}}
	call := serveOver(t, engine)
	for name, params := range map[string]string{
		"missing":       `{}`,
		"blank":         `{"provider":"  "}`,
		"unknown field": `{"provider":"openrouter","force":true}`,
		"path":          `{"provider":"../x"}`,
		"space":         `{"provider":"open router"}`,
		"too long":      `{"provider":"` + strings.Repeat("a", ProviderBalanceIDMax+1) + `"}`,
		"not a string":  `{"provider":7}`,
	} {
		frame := call(`{"id":1,"method":"providers.balance","params":` + params + `}`)
		if frameCode(frame) != CodeBadParams {
			t.Errorf("%s: %v", name, frame)
		}
	}
	if len(engine.asks) != 0 {
		t.Fatalf("the engine heard a refused ask: %+v", engine.asks)
	}
}

func TestProviderBalanceRefusesAnAnswerThatBreaksTheWire(t *testing.T) {
	nan := math.NaN()
	for name, reply := range map[string]ProviderBalance{
		"nan":        {Provider: "p", Status: BalanceKnown, RemainingUSD: &nan},
		"long":       {Provider: "p", Status: BalanceInvalid, Message: strings.Repeat("x", ProviderBalanceStringMax+1)},
		"bad utf8":   {Provider: "p", Status: BalanceInvalid, Message: "\xff"},
		"long title": {Provider: "p", Status: BalanceKnown, Name: strings.Repeat("n", ProviderBalanceStringMax+1)},
	} {
		frame := serveOver(t, &balanceEngine{reply: reply})(`{"id":1,"method":"providers.balance","params":{"provider":"p"}}`)
		if frameCode(frame) != CodeEngine {
			t.Errorf("%s: %v", name, frame)
		}
	}
}
