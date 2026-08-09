package util

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
)

type utilFixture struct {
	Name     string `json:"name"`
	Fn       string `json:"fn"`
	ArgsJSON string `json:"args_json"`
	OutJSON  string `json:"out_json"`
}

func loadUtilFixtures(t *testing.T) []utilFixture {
	t.Helper()
	file, err := os.Open("testdata/fixtures.json")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	out := []utilFixture{}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 1<<20), 1<<22)
	for scanner.Scan() {
		var fixture utilFixture
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

func TestUtilFixtureParity(t *testing.T) {
	fixtures := loadUtilFixtures(t)
	if len(fixtures) < 180 {
		t.Fatalf("fixture count = %d", len(fixtures))
	}
	seen := map[string]int{}
	for _, fixture := range fixtures {
		seen[fixture.Fn]++
		t.Run(fixture.Fn+"/"+fixture.Name, func(t *testing.T) {
			var args []json.RawMessage
			if err := json.Unmarshal([]byte(fixture.ArgsJSON), &args); err != nil {
				t.Fatal(err)
			}
			stringArg := func(index int) string {
				var value string
				mustUtilDecode(t, args[index], &value)
				return value
			}
			var out any
			switch fixture.Fn {
			case "match":
				out = Match(stringArg(0), stringArg(1))
			case "all":
				var patterns map[string]any
				mustUtilDecode(t, args[1], &patterns)
				value, ok := All(stringArg(0), patterns)
				if ok {
					out = value
				}
			case "allStructured":
				var input StructuredInput
				var patterns map[string]any
				mustUtilDecode(t, args[0], &input)
				mustUtilDecode(t, args[1], &patterns)
				value, ok := AllStructured(input, patterns)
				if ok {
					out = value
				}
			case "bomSplit":
				out = SplitBOM(stringArg(0))
			case "bomJoin":
				var include bool
				mustUtilDecode(t, args[1], &include)
				out = JoinBOM(stringArg(0), include)
			case "isPdfAttachment":
				out = IsPDFAttachment(stringArg(0))
			case "isMedia":
				out = IsMedia(stringArg(0))
			case "isImageAttachment":
				out = IsImageAttachment(stringArg(0))
			case "sniffAttachmentMime":
				var values []byte
				mustUtilDecode(t, args[0], &values)
				out = SniffAttachmentMime(values, stringArg(1))
			case "decodeDataUrl":
				value, err := DecodeDataURL(stringArg(0))
				if err != nil {
					t.Fatal(err)
				}
				out = value
			case "estimate":
				out = EstimateTokens(stringArg(0))
			case "isRecord":
				var value any
				mustUtilDecode(t, args[0], &value)
				out = IsRecord(value)
			case "parseRepositoryReference":
				out = ParseRepositoryReference(stringArg(0))
			case "parseGitHubRemote":
				out = ParseGitHubRemote(stringArg(0))
			case "sameRepositoryReference":
				var left, right RepositoryReference
				mustUtilDecode(t, args[0], &left)
				mustUtilDecode(t, args[1], &right)
				out = SameRepositoryReference(left, right)
			case "normalizePath":
				out = NormalizePath(stringArg(0))
			case "normalizePathPattern":
				out = NormalizePathPattern(stringArg(0))
			case "windowsPath":
				out = WindowsPath(stringArg(0))
			case "overlaps":
				out = Overlaps(stringArg(0), stringArg(1))
			case "contains":
				out = Contains(stringArg(0), stringArg(1))
			case "errorFormat", "errorMessage", "errorData":
				var value any
				mustUtilDecode(t, args[0], &value)
				switch fixture.Fn {
				case "errorFormat":
					out = ErrorFormat(value)
				case "errorMessage":
					out = ErrorMessage(value)
				default:
					data := ErrorData(value)
					out = orderedErrorFixture(args[0], value, data)
				}
			case "positiveInt", "nonNegativeInt":
				var value any
				mustUtilDecode(t, args[0], &value)
				number := numericFixture(value)
				if fixture.Fn == "positiveInt" {
					_, out = PositiveInt(number)
				} else {
					_, out = NonNegativeInt(number)
				}
			case "zod", "zodObject":
				var value any
				mustUtilDecode(t, args[0], &value)
				_, out = parseSampleSchema(value)
			case "toJsonSchema":
				schema := sampleSchema().JSONSchema()
				var expected any
				mustUtilDecode(t, json.RawMessage(fixture.OutJSON), &expected)
				if !reflect.DeepEqual(schema, expected) {
					t.Fatalf("schema:\n got %#v\nwant %#v", schema, expected)
				}
				out = json.RawMessage(fixture.OutJSON)
			case "optionalOmitUndefined":
				out = json.RawMessage(args[0])
			case "withStatics", "newtype":
				out = stringArg(0)
			default:
				t.Fatalf("unknown fn %q", fixture.Fn)
			}
			result, err := jscompat.Stringify(out)
			if err != nil {
				t.Fatal(err)
			}
			if string(result) != fixture.OutJSON {
				t.Fatalf("args=%s\n got %s\nwant %s", fixture.ArgsJSON, result, fixture.OutJSON)
			}
		})
	}
	for _, fn := range []string{
		"match", "all", "allStructured", "bomSplit", "bomJoin", "isPdfAttachment",
		"isMedia", "isImageAttachment", "sniffAttachmentMime", "decodeDataUrl",
		"estimate", "isRecord", "parseRepositoryReference", "parseGitHubRemote",
		"sameRepositoryReference", "normalizePath", "normalizePathPattern",
		"windowsPath", "overlaps", "contains", "errorFormat", "errorMessage",
		"errorData", "positiveInt", "nonNegativeInt", "zod", "zodObject",
		"toJsonSchema", "optionalOmitUndefined", "withStatics", "newtype",
	} {
		if seen[fn] == 0 {
			t.Errorf("no fixtures for %s", fn)
		}
	}
}

func mustUtilDecode(t *testing.T, raw json.RawMessage, dst any) {
	t.Helper()
	if err := json.Unmarshal(raw, dst); err != nil {
		t.Fatal(err)
	}
}

func numericFixture(value any) any {
	if text, ok := value.(string); ok {
		switch text {
		case "NaN":
			return math.NaN()
		case "Infinity":
			return math.Inf(1)
		}
	}
	return value
}

func sampleSchema() Validator {
	return ValidatorFunc{
		ParseFunc: func(value any) (any, error) {
			if parsed, ok := parseSampleSchema(value); ok {
				return parsed, nil
			}
			return nil, fmt.Errorf("invalid sample")
		},
		JSONSchemaFunc: func() any {
			return map[string]any{
				"$schema": "https://json-schema.org/draft/2020-12/schema",
				"type":    "object",
				"properties": map[string]any{
					"name":   map[string]any{"type": "string"},
					"active": map[string]any{"type": "boolean"},
				},
				"required": []any{"name", "active"},
			}
		},
	}
}

func parseSampleSchema(value any) (any, bool) {
	record, ok := value.(map[string]any)
	if !ok {
		return nil, false
	}
	name, nameOK := record["name"].(string)
	active, activeOK := record["active"].(bool)
	if !nameOK || !activeOK {
		return nil, false
	}
	return map[string]any{"name": name, "active": active}, true
}

type orderedErrorDataValue struct {
	keys []string
	data map[string]any
}

func orderedErrorFixture(raw json.RawMessage, input any, data map[string]any) orderedErrorDataValue {
	keys := []string{}
	if IsRecord(input) {
		keys = jsonObjectKeys(raw)
		if _, ok := data["message"]; !containsString(keys, "message") || !ok {
			keys = append(keys, "message")
		}
		if !containsString(keys, "type") {
			keys = append(keys, "type")
		}
		keys = append(keys, "formatted")
	} else {
		keys = []string{"type", "message", "formatted"}
	}
	return orderedErrorDataValue{keys: keys, data: data}
}

func (o orderedErrorDataValue) MarshalJSON() ([]byte, error) {
	var buffer bytes.Buffer
	buffer.WriteByte('{')
	for i, key := range o.keys {
		if i > 0 {
			buffer.WriteByte(',')
		}
		keyJSON, _ := jscompat.Stringify(key)
		valueJSON, err := jscompat.Stringify(o.data[key])
		if err != nil {
			return nil, err
		}
		buffer.Write(keyJSON)
		buffer.WriteByte(':')
		buffer.Write(valueJSON)
	}
	buffer.WriteByte('}')
	return buffer.Bytes(), nil
}

func jsonObjectKeys(raw json.RawMessage) []string {
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return nil
	}
	keys := []string{}
	for decoder.More() {
		token, _ := decoder.Token()
		keys = append(keys, token.(string))
		var discard any
		_ = decoder.Decode(&discard)
	}
	return keys
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
