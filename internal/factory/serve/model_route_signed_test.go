package serve

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/provider"
)

func TestEveryErrorTheDoorWritesIsSignedAndOnlyThoseItWrites(t *testing.T) {
	rg := newMeteredRig(t, 0.5, func(ctx context.Context, _, model string) (*ai.Response, error) {
		return meteredAnswer(ctx, model, "paid", 0.5), nil
	})
	chat := rg.create(t, "santosh")
	if w := callHostedModel(rg, chat, testHostedBody); w.Code != http.StatusOK ||
		strings.Contains(w.Body.String(), provider.DoorKey) {
		t.Fatalf("an answer carries the door's mark or failed: %d %s", w.Code, w.Body)
	}
	refused := callHostedModel(rg, chat, testHostedBody)
	if refused.Code != http.StatusPaymentRequired ||
		!strings.Contains(refused.Body.String(), `"`+provider.DoorKey+`":"`+provider.OrganizationDoor+`"`) {
		t.Fatalf("the door's refusal is not signed: %d %s", refused.Code, refused.Body)
	}
	hostedModelErrorIs(t, refused, http.StatusPaymentRequired, "insufficient_quota",
		"This chat has reached its spending limit of $0.50, so it is paused. Start a new chat to carry on.")
}

func TestALimitIsWrittenAsTheNumberItIs(t *testing.T) {
	// Contract 3, and the emptiness law where it applied before: zero stays "$0.00".
	for usd, want := range map[float64]string{
		0:        "$0.00",
		0.001:    "$0.001",
		0.0015:   "$0.0015",
		0.005:    "$0.005",
		0.015:    "$0.015",
		0.01:     "$0.01",
		0.25:     "$0.25",
		0.5:      "$0.50",
		2:        "$2.00",
		5:        "$5.00",
		20:       "$20.00",
		1234.5:   "$1234.50",
		0.07:     "$0.07",
		0.000001: "$0.000001",
		10000.0:  "$10000.00",
	} {
		if got := limitMoney(usd); got != want {
			t.Errorf("limitMoney(%v) = %q, want %q", usd, got, want)
		}
	}
}
