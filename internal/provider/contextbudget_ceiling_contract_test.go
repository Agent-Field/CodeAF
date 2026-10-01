package provider

import (
	"context"
	"testing"
)

func TestContextBudgetOmitsUnneededCeilingButBoundsSmallWindow(t *testing.T) {
	client, recorded := newTestClient(t, Config{Model: "review/output", Direct: true})
	for _, test := range []struct {
		name   string
		budget ContextBudget
		want   bool
	}{
		{"large", ContextBudget{Window: 131072, Reserve: 32768}, false},
		{"small", ContextBudget{Window: 16385, Reserve: 4096, PromptFloor: 13903}, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := WithContextBudget(context.Background(), test.budget)
			_, err := client.CompleteWithMessages(ctx, userMessages("hello"))
			if err != nil {
				t.Fatal(err)
			}
			wire := recorded.body(len(recorded.bodies) - 1)
			_, has := wire["max_tokens"]
			if has != test.want {
				t.Fatalf("max_tokens present=%v, want %v: %#v", has, test.want, wire)
			}
		})
	}
}
