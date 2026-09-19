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
	common, perEvent, _ = docPropsWithDocs(t, body)
	return common, perEvent
}

// docPropsWithDocs is docProps with the third column kept, keyed
// "<event>\t<prop>", for the test that holds the doc's words to PropDoc.
func docPropsWithDocs(t *testing.T, body string) (common []string, perEvent map[string][]string, docs map[string]string) {
	t.Helper()
	perEvent = map[string][]string{}
	docs = map[string]string{}
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
			docs[cells[0]+"\t"+cells[1]] = cells[2]
			continue
		}
		if cells[0] != "" {
			perEvent[cells[0]] = append(perEvent[cells[0]], cells[1])
			docs[cells[0]+"\t"+cells[1]] = cells[2]
		}
	}
	return common, perEvent, docs
}

// TestDocPropertyWordsMatchPropDoc holds the doc's third column to the table
// `codeaf telemetry show` prints from, word for word, and holds that table to
// the allowlist: every allowlisted prop has a description, and every
// description is of an allowlisted prop.
func TestDocPropertyWordsMatchPropDoc(t *testing.T) {
	_, _, docs := docPropsWithDocs(t, docBody(t))
	for key, words := range docs {
		event, prop, _ := strings.Cut(key, "\t")
		if got := PropDoc(event, prop); got != words {
			t.Errorf("%s %s: the doc says %q, PropDoc says %q", event, prop, words, got)
		}
	}
	for _, name := range CommonPropNames() {
		if PropDoc(EveryEvent, name) == "" {
			t.Errorf("every-event prop %q has no PropDoc", name)
		}
		if _, ok := docs[EveryEvent+"\t"+name]; !ok {
			t.Errorf("every-event prop %q has no row in the doc", name)
		}
	}
	for _, event := range AllowlistedEvents() {
		names := EventPropNames(event)
		want := map[string]bool{}
		for _, name := range AllowlistedProps(event) {
			if PropDoc(EveryEvent, name) == "" {
				want[name] = true
			}
		}
		got := map[string]bool{}
		for _, name := range names {
			got[name] = true
			if PropDoc(event, name) == "" {
				t.Errorf("%s: %q has no PropDoc", event, name)
			}
			if _, ok := docs[event+"\t"+name]; !ok {
				t.Errorf("%s: %q has no row in the doc", event, name)
			}
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s: EventPropNames %v drift from the allowlist's own props %v", event, names, AllowlistedProps(event))
		}
	}
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
