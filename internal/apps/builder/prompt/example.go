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

// Example is the few-shot material: paragraph → brief and plan → the WRITE
// reply. PlanReply is what the PLAN call answers: the brief first, then the
// plan keys; Plan alone is plan.json as the card and Check see it.
type Example struct {
	Paragraph string
	Brief     string
	Plan      string
	PlanReply string
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
	brief, err := exampleFS.ReadFile("example/brief.json")
	if err != nil {
		return Example{}, err
	}
	files, err := exampleReply(func(string) bool { return true })
	if err != nil {
		return Example{}, err
	}
	var parsed Brief
	if err := parsed.UnmarshalJSON(brief); err != nil {
		return Example{}, err
	}
	planText := strings.TrimSpace(string(plan))
	return Example{
		Paragraph: strings.TrimSpace(string(paragraph)),
		Brief:     string(parsed),
		Plan:      planText,
		PlanReply: `{"brief":` + strings.TrimSpace(string(brief)) + "," + strings.TrimPrefix(planText, "{"),
		Files:     files,
	}, nil
}

// exampleGroup is the path prefix that selects a file's kind in Inputs.Only.
func exampleGroup(path string) string {
	if dir, _, found := strings.Cut(path, "/"); found {
		return dir + "/"
	}
	return path
}

// exampleReply writes the files that keep accepts as the model must: JSON
// files as objects, everything else as an escaped string, one path per line.
func exampleReply(keep func(path string) bool) (string, error) {
	entries := make([]string, 0, len(exampleFiles))
	for _, path := range exampleFiles {
		if !keep(path) {
			continue
		}
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
