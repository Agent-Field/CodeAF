package id

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
)

type fixture struct {
	Name     string `json:"name"`
	Fn       string `json:"fn"`
	ArgsJSON string `json:"args_json"`
	OutJSON  string `json:"out_json"`
}

type queuedEntropy struct {
	bytes []byte
}

func (q *queuedEntropy) Read(p []byte) (int, error) {
	if len(q.bytes) < len(p) {
		return 0, io.ErrUnexpectedEOF
	}
	copy(p, q.bytes[:len(p)])
	q.bytes = q.bytes[len(p):]
	return len(p), nil
}

func (q *queuedEntropy) enqueueSuffix(value string) {
	suffix := value[len(value)-randomLength:]
	for _, char := range suffix {
		q.bytes = append(q.bytes, byte(strings.IndexRune(base62, char)))
	}
}

func loadFixtures(t *testing.T) []fixture {
	t.Helper()
	file, err := os.Open("testdata/fixtures.json")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	out := []fixture{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var item fixture
		if err := json.Unmarshal(scanner.Bytes(), &item); err != nil {
			t.Fatal(err)
		}
		out = append(out, item)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestFixtureParity(t *testing.T) {
	fixtures := loadFixtures(t)
	if len(fixtures) < 25 {
		t.Fatalf("fixture count = %d", len(fixtures))
	}
	entropy := &queuedEntropy{}
	generator := NewGenerator(func() int64 { return 1_800_000_000_000 }, entropy)
	seen := map[string]int{}
	for _, fx := range fixtures {
		seen[fx.Fn]++
		t.Run(fx.Fn+"/"+fx.Name, func(t *testing.T) {
			var args []json.RawMessage
			if err := json.Unmarshal([]byte(fx.ArgsJSON), &args); err != nil {
				t.Fatal(err)
			}
			var out any
			switch fx.Fn {
			case "schema":
				var kind, value string
				mustDecode(t, args[0], &kind)
				mustDecode(t, args[1], &value)
				out = SchemaAccepts(kind, value)
			case "create":
				var prefix string
				var direction Direction
				var timestamp int64
				mustDecode(t, args[0], &prefix)
				mustDecode(t, args[1], &direction)
				mustDecode(t, args[2], &timestamp)
				var expected string
				mustDecode(t, json.RawMessage(fx.OutJSON), &expected)
				entropy.enqueueSuffix(expected)
				value, err := generator.Create(prefix, direction, timestamp)
				if err != nil {
					t.Fatal(err)
				}
				out = value
			case "timestamp":
				var value string
				mustDecode(t, args[0], &value)
				timestamp, err := Timestamp(value)
				if err != nil {
					t.Fatal(err)
				}
				out = timestamp
			case "ascending", "descending":
				var kind, given string
				mustDecode(t, args[0], &kind)
				mustDecode(t, args[1], &given)
				var expectedString string
				if json.Unmarshal([]byte(fx.OutJSON), &expectedString) == nil && given == "" {
					entropy.enqueueSuffix(expectedString)
				}
				var value string
				var err error
				if fx.Fn == "ascending" {
					value, err = generator.Ascending(kind, given)
				} else {
					value, err = generator.Descending(kind, given)
				}
				if err != nil {
					out = map[string]any{"error": "Error: " + err.Error()}
				} else {
					out = value
				}
			default:
				t.Fatalf("unknown fn %q", fx.Fn)
			}
			bytes, err := jscompat.Stringify(out)
			if err != nil {
				t.Fatal(err)
			}
			if string(bytes) != fx.OutJSON {
				t.Fatalf("args=%s\n got %s\nwant %s", fx.ArgsJSON, bytes, fx.OutJSON)
			}
		})
	}
	for _, fn := range []string{"schema", "create", "timestamp", "ascending", "descending"} {
		if seen[fn] == 0 {
			t.Errorf("no fixtures for %s", fn)
		}
	}
}

func mustDecode(t *testing.T, raw json.RawMessage, dst any) {
	t.Helper()
	if err := json.Unmarshal(raw, dst); err != nil {
		t.Fatal(err)
	}
}
