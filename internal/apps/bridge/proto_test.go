package bridge

import (
	"encoding/json"
	"testing"
)

func TestAppTintWire(t *testing.T) {
	for _, tint := range []string{"", "sage"} {
		for _, detail := range []bool{false, true} {
			name := "summary/" + tint
			if detail {
				name = "detail/" + tint
			}
			t.Run(name, func(t *testing.T) {
				summary := AppSummary{ID: "crm", Tint: tint}
				var value any = summary
				if detail {
					value = AppDetail{AppSummary: summary}
				}
				data, err := json.Marshal(value)
				if err != nil {
					t.Fatal(err)
				}
				var wire map[string]any
				if err := json.Unmarshal(data, &wire); err != nil {
					t.Fatal(err)
				}
				got, present := wire["tint"]
				if present != (tint != "") || present && got != tint {
					t.Fatalf("wire tint = %v, present %v, want %q", got, present, tint)
				}
			})
		}
	}
}
