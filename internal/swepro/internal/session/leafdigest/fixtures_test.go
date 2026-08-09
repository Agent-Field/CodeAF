package leafdigest

import (
	"bufio"
	"encoding/json"
	"os"
	"testing"

	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
)

type fixture struct {
	Name     string `json:"name"`
	Fn       string `json:"fn"`
	ArgsJSON string `json:"args_json"`
	OutJSON  string `json:"out_json"`
}

func loadFixtures(t *testing.T) []fixture {
	t.Helper()
	f, err := os.Open("testdata/fixtures.json")
	if err != nil {
		t.Fatalf("open fixtures: %v", err)
	}
	defer f.Close()

	out := []fixture{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), 1<<24)
	for sc.Scan() {
		if len(sc.Bytes()) == 0 {
			continue
		}
		var fx fixture
		if err := json.Unmarshal(sc.Bytes(), &fx); err != nil {
			t.Fatalf("decode fixture: %v", err)
		}
		out = append(out, fx)
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan fixtures: %v", err)
	}
	return out
}

// fixtureString makes jscompat.Stringify use the package's JS-compatible
// string quoter. That matters when a fixture truncates between an emoji's
// surrogate halves.
type fixtureString string

func (s fixtureString) MarshalJSON() ([]byte, error) {
	return appendJSQuoted(nil, string(s)), nil
}

func appendJSQuoted(dst []byte, s string) []byte {
	const hex = "0123456789abcdef"
	dst = append(dst, '"')
	for i := 0; i < len(s); {
		cp, size := decodeWTF8(s, i)
		i += size
		switch {
		case cp == '"':
			dst = append(dst, '\\', '"')
		case cp == '\\':
			dst = append(dst, '\\', '\\')
		case cp == '\b':
			dst = append(dst, '\\', 'b')
		case cp == '\f':
			dst = append(dst, '\\', 'f')
		case cp == '\n':
			dst = append(dst, '\\', 'n')
		case cp == '\r':
			dst = append(dst, '\\', 'r')
		case cp == '\t':
			dst = append(dst, '\\', 't')
		case cp < 0x20:
			dst = append(dst, '\\', 'u', '0', '0', hex[cp>>4], hex[cp&0xf])
		case cp >= 0xd800 && cp <= 0xdfff:
			dst = append(dst, '\\', 'u',
				hex[(cp>>12)&0xf],
				hex[(cp>>8)&0xf],
				hex[(cp>>4)&0xf],
				hex[cp&0xf],
			)
		default:
			dst = append(dst, s[i-size:i]...)
		}
	}
	return append(dst, '"')
}

func TestFixtureParity(t *testing.T) {
	fixtures := loadFixtures(t)
	if len(fixtures) < 15 {
		t.Fatalf("expected at least 15 fixtures, got %d", len(fixtures))
	}
	for _, fx := range fixtures {
		t.Run(fx.Name, func(t *testing.T) {
			if fx.Fn != "buildLeafDigest" {
				t.Fatalf("unknown fixture function %q", fx.Fn)
			}
			var args []json.RawMessage
			if err := json.Unmarshal([]byte(fx.ArgsJSON), &args); err != nil {
				t.Fatalf("decode args: %v", err)
			}
			if len(args) != 1 {
				t.Fatalf("buildLeafDigest wants 1 argument, got %d", len(args))
			}
			var input LeafDigestInput
			if err := json.Unmarshal(args[0], &input); err != nil {
				t.Fatalf("decode input: %v", err)
			}
			got, err := jscompat.Stringify(fixtureString(BuildLeafDigest(input)))
			if err != nil {
				t.Fatalf("stringify: %v", err)
			}
			if string(got) != fx.OutJSON {
				t.Errorf("args=%s\n got: %s\nwant: %s", fx.ArgsJSON, got, fx.OutJSON)
			}
		})
	}
}

func TestDigestUsesUTF16Cap(t *testing.T) {
	s := BuildLeafDigest(LeafDigestInput{
		TaskID:       "emoji",
		Verdict:      "pass",
		ChangedFiles: []string{"a.ts"},
		KeyDecisions: []string{repeat("😀", DigestMaxChars)},
	})
	if got := utf16Length(s); got > DigestMaxChars {
		t.Fatalf("UTF-16 length %d exceeds cap", got)
	}
}

func repeat(s string, n int) string {
	var out []byte
	for range n {
		out = append(out, s...)
	}
	return string(out)
}
