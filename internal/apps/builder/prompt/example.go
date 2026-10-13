package prompt

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

// The worked example is the CRM of board 08, kept as real files so the tests
// can prove it parses and the prompt never shows the model a broken sample.
//
//go:embed example
var exampleFS embed.FS

// exampleFiles is the order the example reply lists its files in.
var exampleFiles = []string{
	"app.json", "screens/deals.json", "screens/companies.json",
	"runs/follow-up.yaml", "agent.md",
}

// Example is the few-shot material: paragraph → plan → the WRITE reply.
type Example struct {
	Paragraph string
	Plan      string
	Files     string
}

var loadExample = sync.OnceValues(buildExample)

func buildExample() (Example, error) {
	paragraph, err := exampleFS.ReadFile("example/paragraph.txt")
	if err != nil {
		return Example{}, err
	}
	plan, err := exampleFS.ReadFile("example/plan.json")
	if err != nil {
		return Example{}, err
	}
	files, err := exampleReply()
	if err != nil {
		return Example{}, err
	}
	return Example{
		Paragraph: strings.TrimSpace(string(paragraph)),
		Plan:      strings.TrimSpace(string(plan)),
		Files:     files,
	}, nil
}

// exampleReply writes the files as the model must: JSON files as objects,
// everything else as an escaped string, one path per line.
func exampleReply() (string, error) {
	entries := make([]string, 0, len(exampleFiles))
	for _, path := range exampleFiles {
		raw, err := exampleFS.ReadFile("example/" + path)
		if err != nil {
			return "", err
		}
		value, err := exampleValue(path, raw)
		if err != nil {
			return "", fmt.Errorf("prompt: example %s: %w", path, err)
		}
		key, _ := json.Marshal(path)
		entries = append(entries, "  "+string(key)+": "+value)
	}
	return "{\n" + strings.Join(entries, ",\n") + "\n}", nil
}

func exampleValue(path string, raw []byte) (string, error) {
	if strings.HasSuffix(path, ".json") {
		var compact bytes.Buffer
		err := json.Compact(&compact, raw)
		return compact.String(), err
	}
	quoted, err := json.Marshal(string(raw))
	return string(quoted), err
}
