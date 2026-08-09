package bus

import (
	"bufio"
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
)

type eventFixture struct {
	Name     string `json:"name"`
	Fn       string `json:"fn"`
	ArgsJSON string `json:"args_json"`
	OutJSON  string `json:"out_json"`
}

func loadEventFixtures(t *testing.T) []eventFixture {
	t.Helper()
	file, err := os.Open("testdata/fixtures.json")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	out := []eventFixture{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var fixture eventFixture
		if err := json.Unmarshal(scanner.Bytes(), &fixture); err != nil {
			t.Fatal(err)
		}
		out = append(out, fixture)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestEventFixtureParity(t *testing.T) {
	fixtures := loadEventFixtures(t)
	if len(fixtures) < 25 {
		t.Fatalf("fixture count = %d", len(fixtures))
	}
	definitions.Lock()
	definitions.order = nil
	definitions.byID = make(map[string]Definition)
	definitions.Unlock()

	seen := map[string]int{}
	for _, fixture := range fixtures {
		seen[fixture.Fn]++
		t.Run(fixture.Fn+"/"+fixture.Name, func(t *testing.T) {
			var args []json.RawMessage
			if err := json.Unmarshal([]byte(fixture.ArgsJSON), &args); err != nil {
				t.Fatal(err)
			}
			var out any
			switch fixture.Fn {
			case "define":
				var eventType, schema string
				mustEventDecode(t, args[0], &eventType)
				mustEventDecode(t, args[1], &schema)
				def := Define(eventType, schema)
				out = struct {
					Type string `json:"type"`
				}{Type: def.Type}
			case "payloads", "effectPayloads":
				var candidate map[string]any
				mustEventDecode(t, args[0], &candidate)
				descriptors := Payloads()
				if fixture.Fn == "effectPayloads" {
					descriptors = EffectPayloads()
				}
				results := make([]bool, len(descriptors))
				for i, descriptor := range descriptors {
					results[i] = validatesPayload(descriptor, candidate)
				}
				out = results
			case "payloadTypes":
				types := []string{}
				for _, descriptor := range Payloads() {
					types = append(types, descriptor.Type)
				}
				out = types
			default:
				t.Fatalf("unknown fn %q", fixture.Fn)
			}
			bytes, err := jscompat.Stringify(out)
			if err != nil {
				t.Fatal(err)
			}
			if string(bytes) != fixture.OutJSON {
				t.Fatalf("args=%s\n got %s\nwant %s", fixture.ArgsJSON, bytes, fixture.OutJSON)
			}
		})
	}
	if !reflect.DeepEqual(seen, map[string]int{
		"define":         3,
		"payloads":       8,
		"effectPayloads": 8,
		"payloadTypes":   6,
	}) {
		t.Fatalf("fixture coverage: %v", seen)
	}
}

func validatesPayload(descriptor PayloadDefinition, candidate map[string]any) bool {
	id, idOK := candidate["id"].(string)
	eventType, typeOK := candidate["type"].(string)
	properties, propertiesOK := candidate["properties"].(map[string]any)
	if !idOK || id == "" && candidate["id"] == nil || !typeOK || eventType != descriptor.Type || !propertiesOK {
		return false
	}
	switch descriptor.Properties {
	case "text":
		_, ok := properties["text"].(string)
		return ok
	case "count":
		_, ok := properties["count"].(float64)
		return ok
	case "flag":
		_, ok := properties["flag"].(bool)
		return ok
	default:
		return false
	}
}

func mustEventDecode(t *testing.T, raw json.RawMessage, dst any) {
	t.Helper()
	if err := json.Unmarshal(raw, dst); err != nil {
		t.Fatal(err)
	}
}
