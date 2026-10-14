package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
)

// para is one owner paragraph the bench builds on every road.
type para struct {
	ID        string `json:"id"`
	Paragraph string `json:"paragraph"`
	Family    string `json:"family,omitempty"`
	// Jobs is the zoo's acceptance-jobs line; the judge reads it, the Builder never does.
	Jobs string `json:"jobs,omitempty"`
}

// readParagraphs reads a paragraphs file. A file whose first non-blank byte is
// "{" is JSON lines; anything else is plain text, one paragraph per block of
// lines separated by a blank line, with ids p01, p02 and so on.
func readParagraphs(path string) ([]para, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out []para
	if bytes.HasPrefix(bytes.TrimSpace(raw), []byte("{")) {
		out, err = parseJSONLines(raw)
	} else {
		out = parseBlocks(string(raw))
	}
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%s: no paragraphs", path)
	}
	return out, nil
}

func parseJSONLines(raw []byte) ([]para, error) {
	var out []para
	sc := bufio.NewScanner(bytes.NewReader(raw))
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var p para
		if err := json.Unmarshal([]byte(line), &p); err != nil {
			return nil, fmt.Errorf("line %d: %w", n, err)
		}
		if strings.TrimSpace(p.Paragraph) == "" {
			return nil, fmt.Errorf("line %d: empty paragraph", n)
		}
		if p.ID == "" {
			p.ID = fmt.Sprintf("p%02d", len(out)+1)
		}
		out = append(out, p)
	}
	return out, sc.Err()
}

// tagged matches the zoo's pick shape: "[app-id] paragraph".
var tagged = regexp.MustCompile(`^\[([^\]]+)\]\s+(.+)$`)

func parseBlocks(text string) []para {
	var out []para
	for _, block := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n\n") {
		block = strings.Join(strings.Fields(block), " ")
		switch m := tagged.FindStringSubmatch(block); {
		case block == "":
		case m != nil:
			out = append(out, para{ID: m[1], Paragraph: m[2]})
		case len(out) > 0 && strings.HasPrefix(block, "jobs:") && out[len(out)-1].Jobs == "":
			out[len(out)-1].Jobs = strings.TrimSpace(strings.TrimPrefix(block, "jobs:"))
		default:
			out = append(out, para{ID: fmt.Sprintf("p%02d", len(out)+1), Paragraph: block})
		}
	}
	return out
}
