package agentfield

import (
	"encoding/json"
	"sort"
	"strings"
)

// ── THE SOURCE CATALOG IN A PERSON'S WORDS ──────────────────────────────────
//
// The control plane's catalog (GET /api/v1/sources) names each source as a
// word (generic_hmac) and its config as a JSON Schema. A page that lets a
// person add a trigger needs the source in words and the fields of its form;
// this file reads both out of what the control plane sent, so every door that
// shows the catalog says the same thing.

// sourceLabels are the sources AgentField ships, in words. A source the
// control plane adds later reads as its name made into words.
var sourceLabels = map[string]string{
	"cron":           "Schedule",
	"generic_bearer": "Webhook with a bearer token",
	"generic_hmac":   "Signed webhook",
	"github":         "GitHub",
	"linear":         "Linear",
	"sentry":         "Sentry",
	"slack":          "Slack",
	"stripe":         "Stripe",
	"databricks":     "Databricks",
	"snowflake":      "Snowflake",
}

// SourceLabel is an AgentField source in a person's words: Linear,
// Signed webhook, Schedule.
func SourceLabel(name string) string {
	key := strings.ToLower(strings.TrimSpace(name))
	if label, ok := sourceLabels[key]; ok {
		return label
	}
	return wordsOf(key)
}

// ConfigField is one top-level field of a source's config schema.
type ConfigField struct {
	Name  string
	Label string
	// Secret says the field holds a value a form never shows again: the
	// schema marks it writeOnly or as a password, or its name says it is a
	// secret, a token, a password or a key.
	Secret bool
}

// ConfigFields is the top-level fields schema names, sorted by name; nil for
// a schema that names none or cannot be read.
func ConfigFields(schema json.RawMessage) []ConfigField {
	var s struct {
		Properties map[string]struct {
			Title     string `json:"title"`
			Format    string `json:"format"`
			WriteOnly bool   `json:"writeOnly"`
		} `json:"properties"`
	}
	if len(schema) == 0 || json.Unmarshal(schema, &s) != nil || len(s.Properties) == 0 {
		return nil
	}
	names := make([]string, 0, len(s.Properties))
	for k := range s.Properties {
		names = append(names, k)
	}
	sort.Strings(names)
	out := make([]ConfigField, 0, len(names))
	for _, k := range names {
		p := s.Properties[k]
		label := strings.TrimSpace(p.Title)
		if label == "" {
			label = wordsOf(k)
		}
		out = append(out, ConfigField{Name: k, Label: label, Secret: p.WriteOnly || p.Format == "password" || secretName(k)})
	}
	return out
}

// secretName says a field's name is a secret's: one of its words is secret,
// token, password or key.
func secretName(name string) bool {
	for _, w := range strings.FieldsFunc(strings.ToLower(name), func(r rune) bool { return r == '_' || r == '-' || r == '.' }) {
		switch w {
		case "secret", "token", "password", "key", "apikey":
			return true
		}
	}
	return false
}

// wordsOf makes a word such as tolerance_seconds into Tolerance seconds.
func wordsOf(name string) string {
	words := strings.Join(strings.FieldsFunc(name, func(r rune) bool { return r == '_' || r == '-' }), " ")
	if words == "" {
		return name
	}
	return strings.ToUpper(words[:1]) + words[1:]
}
