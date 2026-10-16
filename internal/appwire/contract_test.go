package appwire

import (
	"encoding/json"
	"os"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// protocolDoc is the written protocol the app is built against.
const protocolDoc = "../../docs/design/app-v3/PROTOCOL.md"

// documented reads the protocol's method tables and its event lists: the
// names in the first column of every table headed "method", and the names in
// the event table and in every "Events" line.
func documented(t *testing.T) (methodsInDoc, eventsInDoc map[string]bool) {
	t.Helper()
	raw, err := os.ReadFile(protocolDoc)
	if err != nil {
		t.Fatal(err)
	}
	methodsInDoc, eventsInDoc = map[string]bool{}, map[string]bool{}
	tick := regexp.MustCompile("`([a-z_]+(?:\\.[a-z_]+)*)[` ]")
	table := ""
	for _, line := range strings.Split(string(raw), "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "| method |"):
			table = "method"
			continue
		case strings.HasPrefix(trimmed, "| event |"):
			table = "event"
			continue
		case !strings.HasPrefix(trimmed, "|"):
			table = ""
		}
		if strings.HasPrefix(trimmed, "Events") {
			for _, m := range tick.FindAllStringSubmatch(trimmed, -1) {
				eventsInDoc[m[1]] = true
			}
			continue
		}
		if table == "" || strings.HasPrefix(trimmed, "|---") {
			continue
		}
		cells := strings.Split(trimmed, "|")
		if len(cells) < 2 {
			continue
		}
		for _, m := range tick.FindAllStringSubmatch(cells[1]+" ", -1) {
			if table == "method" {
				methodsInDoc[m[1]] = true
			} else {
				eventsInDoc[m[1]] = true
			}
		}
	}
	return methodsInDoc, eventsInDoc
}

func missing(want []string, have map[string]bool) []string {
	var out []string
	for _, name := range want {
		if !have[name] {
			out = append(out, name)
		}
	}
	return out
}

// THE WRITTEN PROTOCOL NAMES EXACTLY THE METHODS AND EVENTS THE SERVER HAS.
// A method or event the server serves and the document does not name is one
// the app cannot know about; one the document names and the server does not
// serve is one the app would call into a refusal.
func TestTheProtocolDocumentNamesEveryMethodAndEvent(t *testing.T) {
	inDoc, eventsInDoc := documented(t)
	if gone := missing(Methods(), inDoc); len(gone) > 0 {
		t.Errorf("methods the server serves and PROTOCOL.md does not list: %v", gone)
	}
	served := map[string]bool{}
	for _, name := range Methods() {
		served[name] = true
	}
	var extra []string
	for name := range inDoc {
		if !served[name] {
			extra = append(extra, name)
		}
	}
	sort.Strings(extra)
	if len(extra) > 0 {
		t.Errorf("methods PROTOCOL.md lists and the server does not serve: %v", extra)
	}
	if gone := missing(Events(), eventsInDoc); len(gone) > 0 {
		t.Errorf("events the server emits and PROTOCOL.md does not list: %v", gone)
	}
	known := map[string]bool{}
	for _, name := range Events() {
		known[name] = true
	}
	var unknown []string
	for name := range eventsInDoc {
		if !known[name] && !served[name] {
			unknown = append(unknown, name)
		}
	}
	sort.Strings(unknown)
	if len(unknown) > 0 {
		t.Errorf("events PROTOCOL.md lists and the server does not emit: %v", unknown)
	}
}

// EVERY EVENT CONSTANT HAS A SHAPE, and every shape is a struct the schema
// can describe.
func TestEveryEventHasAShape(t *testing.T) {
	for name, shape := range eventShapes {
		if shape == nil || shape.Kind() != reflect.Struct {
			t.Errorf("%s has no struct shape", name)
		}
	}
	for _, m := range methods {
		if m.params == nil || m.result == nil || m.call == nil {
			t.Fatalf("a method was built without its types")
		}
	}
}

// THE SCHEMA IS ONE DOCUMENT THAT NAMES EVERY METHOD AND EVENT, and a field
// the app has broken on before says what it is: dock_badge is a boolean.
func TestTheSchemaIsGeneratedForTheWholeWire(t *testing.T) {
	out, err := SchemaJSON()
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Methods map[string]json.RawMessage `json:"methods"`
		Events  map[string]json.RawMessage `json:"events"`
		Defs    map[string]struct {
			Properties map[string]map[string]any `json:"properties"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Methods) != len(Methods()) || len(doc.Events) != len(Events()) {
		t.Fatalf("schema has %d methods and %d events, want %d and %d", len(doc.Methods), len(doc.Events), len(Methods()), len(Events()))
	}
	for _, field := range []string{"invoked", "rejected_before_run", "harness_made", "refused", "media_attempt"} {
		for _, name := range []string{"TurnTool", "Entry"} {
			if got := doc.Defs[name].Properties[field]["type"]; got != "boolean" {
				t.Fatalf("%s.%s schema type=%v", name, field, got)
			}
		}
	}
	if got := doc.Defs["Settings"].Properties["dock_badge"]["type"]; got != "boolean" {
		t.Fatalf("settings.dock_badge is %v in the schema", got)
	}
	if got := doc.Defs["Settings"].Properties["appearance"]["enum"]; got == nil {
		t.Fatalf("settings.appearance names no words")
	}
}
