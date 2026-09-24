//go:build !windows

package app

import (
	"bytes"
	"strings"
	"testing"
)

// A crew's models are pool entries under the model API's service, and one
// already filed there is left as it is.
func TestCrewModelFilesASeatUnderTheService(t *testing.T) {
	for in, want := range map[string]string{
		"z-ai/glm-5.3-flash":            "openrouter/z-ai/glm-5.3-flash",
		"openrouter/z-ai/glm-5.3-flash": "openrouter/z-ai/glm-5.3-flash",
		"  ":                            "",
	} {
		if got := CrewModel(in); got != want {
			t.Errorf("CrewModel(%q) = %q, want %q", in, got, want)
		}
	}
}

// Pools a crew filled keep only what the catalog can size, say what they
// dropped, and route on senior-dev's own list when the working seat is left
// with nothing.
func TestCrewPoolsDropWhatTheCatalogLacksAndFallBackToTheOwnList(t *testing.T) {
	known := func(ref string) bool { return !strings.Contains(ref, "unknown") }
	var notes bytes.Buffer
	args := crewPools(cliArgs{
		High:     "openrouter/vendor/hands",
		Frontier: "openrouter/vendor/unknown-brain",
		Low:      "openrouter/vendor/light",
	}, known, &notes)
	if args.High != "openrouter/vendor/hands" || args.Frontier != "" || args.Low != "openrouter/vendor/light" {
		t.Fatalf("pools = %+v, want the unknown frontier dropped and the rest kept", args)
	}
	if !strings.Contains(notes.String(), "openrouter/vendor/unknown-brain is not in the model catalog") {
		t.Fatalf("notes = %q, want the dropped model named", notes.String())
	}
	notes.Reset()
	if args := crewPools(cliArgs{High: "openrouter/vendor/unknown-hands"}, known, &notes); args.High != DefaultHighModels {
		t.Fatalf("an unusable working seat left --high = %q, want senior-dev's own list", args.High)
	}
	if !strings.Contains(notes.String(), "routing on senior-dev's own list") {
		t.Fatalf("notes = %q, want the fallback said", notes.String())
	}
}
