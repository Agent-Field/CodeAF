package appwire

import (
	"encoding/json"
	"testing"
)

// Contract (a), (b): the registered event schema exposes an optional numeric
// spend, and the JSON event carries a supplied figure while omitting absence.
func TestTurnToolSpendWireContract(t *testing.T) {
	out, err := SchemaJSON()
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Events map[string]struct {
			Ref string `json:"$ref"`
		} `json:"events"`
		Defs map[string]struct {
			Properties map[string]struct {
				AnyOf []struct {
					Type string `json:"type"`
				} `json:"anyOf"`
			} `json:"properties"`
			Required []string `json:"required"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Events[EventTurnTool].Ref != "#/$defs/TurnTool" {
		t.Fatalf("turn.tool schema = %+v", doc.Events[EventTurnTool])
	}
	shape := doc.Defs["TurnTool"]
	alts := shape.Properties["spent_usd"].AnyOf
	if len(alts) != 2 || alts[0].Type != "number" || alts[1].Type != "null" {
		t.Fatalf("TurnTool.spent_usd schema = %+v", alts)
	}
	for _, field := range shape.Required {
		if field == "spent_usd" {
			t.Fatal("TurnTool.spent_usd became required")
		}
	}
	if _, present := doc.Defs["Entry"].Properties["spent_usd"]; present {
		t.Fatal("history entries acquired live spend")
	}
	usd := .375
	for _, figure := range []*float64{nil, &usd} {
		raw, err := json.Marshal(TurnTool{Agent: "a", Turn: 1, Call: "c", Name: "read", Phase: PhaseBegin, SpentUSD: figure})
		if err != nil {
			t.Fatal(err)
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &fields); err != nil {
			t.Fatal(err)
		}
		if _, present := fields["spent_usd"]; present != (figure != nil) {
			t.Fatalf("wrong spend presence: %s", raw)
		}
		if figure != nil && string(fields["spent_usd"]) != "0.375" {
			t.Fatalf("wrong spend figure: %s", raw)
		}
	}
}
