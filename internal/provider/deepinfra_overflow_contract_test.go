package provider

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/taxonomy"
)

func TestDeepInfraInputLengthRefusalCarriesItsExactLimits(t *testing.T) {
	body := []byte(`{"error":{"message":"Provider returned error","code":400,"metadata":{"raw":"Requested input length 39818 exceeds maximum input length 32767 (via DeepInfra)","provider_name":"DeepInfra"}}}`)
	failure, ok := RefusalFrom(apiError(400, body))
	if !ok || !failure.Overflow || failure.ContextLimit != 32767 || failure.InputTokens != 39818 || failure.Provider != "DeepInfra" {
		t.Fatalf("DeepInfra overflow evidence = %+v, ok %v", failure, ok)
	}
	if verdict := taxonomy.Classify(Evidence(failure), taxonomy.Limits{}.Floored()); !verdict.Compacts() {
		t.Fatalf("DeepInfra overflow chose %s instead of recovery", verdict)
	}
	model := "review/deepinfra-overflow"
	quirksAt(t, model)
	client, err := NewClient(Config{BaseURL: "http://deepinfra.test", Model: model, Direct: true})
	if err != nil {
		t.Fatal(err)
	}
	client.rememberContextLimit(model, failure)
	if got := client.servingWindow(model, nil, 100_000, false); got != 32767 {
		t.Fatalf("learned endpoint limit = %d, want 32767", got)
	}
	other, _ := RefusalFrom(apiError(400, []byte(`{"error":{"message":"Provider returned error","code":400,"metadata":{"raw":"The input file could not be read","provider_name":"DeepInfra"}}}`)))
	if other.Overflow {
		t.Fatalf("unrelated refusal became overflow: %+v", other)
	}
}
