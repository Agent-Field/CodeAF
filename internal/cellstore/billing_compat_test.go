package cellstore

import (
	"encoding/json"
	"testing"

	"github.com/Agent-Field/codeaf/internal/executor"
)

// This mirror freezes the app/v3 reader before the optional billing keys.
type appV3ModelCall struct {
	ID           string `json:"id,omitempty"`
	Model        string `json:"model"`
	Role         string `json:"role,omitempty"`
	ParamsHash   string `json:"params_hash"`
	RequestHash  string `json:"request_hash"`
	ResponseHash string `json:"response_hash"`
	TokensIn     uint32 `json:"tokens_in"`
	TokensOut    uint32 `json:"tokens_out"`
	TokensCached uint32 `json:"tokens_cached,omitempty"`
	CostMicroUSD int64  `json:"cost_micro_usd"`
}

func TestBillingReceiptKeepsDollarsForAppV3Readers(t *testing.T) {
	c := newCell(t)
	fake := &fakeEngine{}
	seat := seatWith(t, c, fake.engine(t))
	executor.NoteModelCall(seat, executor.ModelCall{ID: "priced", Model: "m", TokensIn: 100, TokensOut: 10, CostMicroUSD: 1234, Billing: "metered", BillingBasis: "door:test", FactoryJob: "o/r#1"})
	executor.NoteModelCall(seat, executor.ModelCall{ID: "missing", Kind: executor.UnbilledMarkerKind, Model: "m", Unbilled: true, FactoryJob: "o/r#1"})
	toolCall(t, seat)
	head, err := Head(c)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(head.Receipt)
	if err != nil {
		t.Fatal(err)
	}
	var old struct {
		ModelCalls []appV3ModelCall `json:"model_calls"`
	}
	if err := json.Unmarshal(raw, &old); err != nil {
		t.Fatal(err)
	}
	if len(old.ModelCalls) != 2 || old.ModelCalls[0].CostMicroUSD != 1234 || old.ModelCalls[0].TokensIn != 100 || old.ModelCalls[1].CostMicroUSD != 0 {
		t.Fatalf("old reader=%+v", old)
	}
	calls, err := ModelCalls(c)
	if err != nil || len(calls) != 2 {
		t.Fatalf("calls=%+v err=%v", calls, err)
	}
	if calls[0].Billing != "metered" || calls[0].BillingBasis != "door:test" || calls[0].FactoryJob != "o/r#1" || calls[1].Kind != executor.UnbilledMarkerKind || !calls[1].Unbilled {
		t.Fatalf("saved facts=%+v", calls)
	}
}
