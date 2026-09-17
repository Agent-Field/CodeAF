package telemetry

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// The doc is held to the code. Its property table is parsed and compared with
// the allowlist the constructors are built from, so a prop added to the code
// without the doc — or written into the doc without the code — fails here.

func docBody(t *testing.T) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("..", "..", "docs", "TELEMETRY.md"))
	if err != nil {
		t.Fatalf("docs/TELEMETRY.md must exist beside the package: %v", err)
	}
	return string(body)
}

// docProps parses the property table under "## What is sent" into the
// every-event props and the extras per event. Anything else on the page is
// prose and not held to anything.
func docProps(t *testing.T, body string) (common []string, perEvent map[string][]string) {
	t.Helper()
	perEvent = map[string][]string{}
	inSection := false
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "## ") {
			inSection = trimmed == "## What is sent"
			continue
		}
		if !inSection || !strings.HasPrefix(trimmed, "|") {
			continue
		}
		cells := strings.Split(strings.Trim(trimmed, "|"), "|")
		if len(cells) != 3 {
			continue
		}
		for i := range cells {
			cells[i] = strings.TrimSpace(cells[i])
		}
		if cells[0] == "Event" || strings.HasPrefix(cells[0], "-") || strings.HasPrefix(cells[1], "-") {
			continue
		}
		if cells[0] == "every event" {
			common = append(common, cells[1])
			continue
		}
		if cells[0] != "" {
			perEvent[cells[0]] = append(perEvent[cells[0]], cells[1])
		}
	}
	return common, perEvent
}

func TestDocPropertyTableMatchesTheAllowlist(t *testing.T) {
	docCommon, docPerEvent := docProps(t, docBody(t))

	wantCommon := map[string]bool{}
	for _, name := range CommonPropNames() {
		wantCommon[name] = true
	}
	gotCommon := map[string]bool{}
	for _, name := range docCommon {
		gotCommon[name] = true
	}
	if !reflect.DeepEqual(gotCommon, wantCommon) {
		t.Errorf("the doc's every-event props drift from CommonPropNames: doc %v, allowlist %v", docCommon, CommonPropNames())
	}

	events := map[string]bool{}
	for _, name := range AllowlistedEvents() {
		events[name] = true
	}
	for name := range docPerEvent {
		if !events[name] {
			t.Errorf("the doc has property rows for %q, which is not one of the four events", name)
		}
	}
	for _, event := range AllowlistedEvents() {
		want := map[string]bool{}
		for _, name := range AllowlistedProps(event) {
			want[name] = true
		}
		got := map[string]bool{}
		for _, name := range docCommon {
			got[name] = true
		}
		for _, name := range docPerEvent[event] {
			if wantCommon[name] {
				t.Errorf("%s: %q is already an every-event prop; the doc lists it twice", event, name)
			}
			got[name] = true
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s: the doc's props drift from the allowlist: doc %v, allowlist %v", event, got, AllowlistedProps(event))
		}
	}
}

func TestDocCarriesTheNoticeAndTheSwitches(t *testing.T) {
	body := docBody(t)
	if !strings.Contains(body, Notice) {
		t.Error("docs/TELEMETRY.md must quote the notice byte for byte")
	}
	for _, wanted := range []string{"CODEAF_TELEMETRY=off", "DO_NOT_TRACK=1", "telemetry show"} {
		if !strings.Contains(body, wanted) {
			t.Errorf("docs/TELEMETRY.md must mention %q", wanted)
		}
	}
	lower := strings.ToLower(body)
	for _, never := range []string{"prompts", "paths", "hostnames", "usernames", "api keys", "panic messages"} {
		if !strings.Contains(lower, never) {
			t.Errorf("docs/TELEMETRY.md must name %q among what is never sent", never)
		}
	}
}
