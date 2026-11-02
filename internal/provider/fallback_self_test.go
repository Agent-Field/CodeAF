package provider

import "testing"

func TestAFallbackListNamingOnlyTheFailingModelIsAPin(t *testing.T) {
	guessed := func(string) []string { return []string{"guessed/one", "guessed/two"} }
	for name, written := range map[string][]string{
		"the pinned model alone":    {"sim/model"},
		"the pinned model repeated": {"sim/model", " SIM/model "},
		"blank rows around the pin": {"", "sim/model"},
	} {
		client, _ := refusingClient(t, func(map[string]any) bool { return false }, Config{
			Fallbacks: written, NearestModels: guessed,
		})
		if got := client.FallbackModels("sim/model"); len(got) != 0 {
			t.Fatalf("%s: FallbackModels = %v, want none: a pin sends no call to another model", name, got)
		}
	}
	// A row of blanks is no answer at all, and the catalog still helps.
	client, _ := refusingClient(t, func(map[string]any) bool { return false }, Config{
		Fallbacks: []string{" ", ""}, NearestModels: guessed,
	})
	if got := client.FallbackModels("sim/model"); len(got) != 2 {
		t.Fatalf("FallbackModels = %v, want the catalog's two for a row that said nothing", got)
	}
}
