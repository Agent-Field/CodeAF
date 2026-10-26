package keys

import (
	"errors"
	"io"
	"strings"
	"testing"
)

type rejectedReader struct{}

func (rejectedReader) Read([]byte) (int, error) { return 0, errors.New("synthetic read failure") }

func TestScannerReaderScreensWholeStreamAndNames(t *testing.T) {
	for _, test := range []struct{ name, path, text, rule string }{
		{"safe", "note.txt", "safe mock bytes", ""},
		{"dotenv", ".env", "MOCK=value", RuleDotenv},
		{"template", ".env.example", "MOCK=replace-me", ""},
		{"boundary", "note.txt", strings.Repeat("x", (64<<10)-10) + "-----BEGIN PRIVATE KEY-----\nSYNTHETIC", "private-key-file"},
		{"tail", "note.txt", strings.Repeat("safe bytes\n", 200000) + "-----BEGIN PRIVATE KEY-----\nSYNTHETIC", "private-key-file"},
	} {
		t.Run(test.name, func(t *testing.T) {
			scanner := Scanner{}
			found, err := scanner.Reader(test.path, strings.NewReader(test.text))
			if err != nil || found.Rule != test.rule {
				t.Fatalf("rule=%q error=%v, want %q", found.Rule, err, test.rule)
			}
		})
	}
	scanner := Scanner{}
	if _, err := scanner.Reader("safe.txt", rejectedReader{}); err == nil {
		t.Fatal("read failure judged clean")
	}
	// A caller owns framing: scanning a short object cannot consume a later one.
	input := strings.NewReader("safe-----BEGIN PRIVATE KEY-----")
	if found, err := scanner.Reader("safe.txt", io.LimitReader(input, 4)); err != nil || found.Rule != "" {
		t.Fatalf("limited safe object: %v %v", found, err)
	}
}
