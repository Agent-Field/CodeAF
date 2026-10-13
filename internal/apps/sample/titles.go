package sample

import (
	"strings"
	"unicode"

	"github.com/Agent-Field/codeaf/internal/apps/spec"
)

var titleVerbs = []string{"Review", "Update", "Check", "Improve", "Prepare", "Revise", "Simplify", "Confirm", "Adjust", "Organize", "Complete", "Refresh", "Restore", "Align", "Clarify", "Polish"}
var titleSubjects = []string{"overview", "search", "profile", "team", "account", "document", "project", "activity", "notification", "report", "workspace", "calendar"}
var titleDetails = []string{"details", "layout", "workflow", "settings", "access", "history", "summary", "notes", "attachments", "navigation", "schedule", "format"}
var titleContexts = []string{"", " before handoff", " for the next review", " after feedback", " for the team"}

func titleValue(typ string, schema *spec.RecordType, f *spec.Field, i int) string {
	// A literal default can name the first record, but cannot name the entire preview.
	if i == 0 && useDefault(f) {
		if value, err := defaultValue(f); err == nil {
			if text, ok := value.(string); ok && readableTitle(text) {
				return text
			}
		}
	}
	if personTitle(typ) || personTitle(schema.Label) || personField(schema.Title, f.Label) {
		return personName(i)
	}
	subjects := schemaSubjects(schema)
	subjectIndex := i % len(subjects)
	subject := subjects[subjectIndex]
	i /= len(subjects)
	verb := titleVerbs[(i+subjectIndex)%len(titleVerbs)]
	i /= len(titleVerbs)
	detail := titleDetails[(i+subjectIndex)%len(titleDetails)]
	i /= len(titleDetails)
	context := titleContexts[i%len(titleContexts)]
	// The vocabulary product covers the generator's count limit without counters.
	return label(schema.Label, typ) + ": " + verb + " " + subject + " " + detail + context
}

func schemaSubjects(schema *spec.RecordType) []string {
	var subjects []string
	seen := map[string]bool{}
	add := func(text string) {
		text = strings.Join(strings.Fields(text), " ")
		key := strings.ToLower(text)
		if readableSubject(key) && !seen[key] {
			subjects = append(subjects, text)
			seen[key] = true
		}
	}
	for _, name := range keys(schema.Fields) {
		f := schema.Fields[name]
		semantic := name + " " + f.Label
		if name == schema.Title || classificationField(semantic) {
			continue
		}
		for _, value := range f.Values {
			add(value)
		}
		add(strings.ToLower(label(f.Label, name)))
	}
	for _, subject := range titleSubjects {
		add(subject)
	}
	return subjects
}

func classificationField(text string) bool {
	return hasWord(text, "status", "state", "stage", "severity", "priority", "size", "id", "identifier", "code", "serial", "sku", "created", "updated", "owner", "assignee")
}

func readableSubject(text string) bool {
	if !readableTitle(text) || len(strings.Fields(text)) > 3 {
		return false
	}
	return !strings.ContainsFunc(text, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsSpace(r) && r != '-'
	})
}

func readableTitle(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" || text == "<nil>" || strings.Contains(strings.ToLower(text), "sample_") {
		return false
	}
	// Stored identifiers are not names, even when they came from a default or computation.
	if strings.ContainsAny(text, "_/\\") || len([]rune(text)) <= 2 {
		return false
	}
	return len(strings.Fields(text)) > 1 || !strings.ContainsFunc(text, unicode.IsDigit)
}
