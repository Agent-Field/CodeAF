package sample

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

// Vocabulary limits: one cheap call returns at most this much per type.
const (
	vocabTitles   = 8
	vocabValues   = 4
	vocabTextMax  = 80
	vocabNoteMax  = 240
	vocabTimeout  = 30 * time.Second
	vocabFieldMax = 6
)

// vocab is model-written example values per record type, already validated
// against the field types. A nil vocab means the deterministic generator.
type vocab struct {
	Types map[string]typeVocab `json:"types"`
}

type typeVocab struct {
	Titles []string            `json:"titles"`
	Fields map[string][]string `json:"fields"`
}

func (v *vocab) title(typ string, i int) (string, bool) {
	if v == nil || i >= len(v.Types[typ].Titles) {
		return "", false
	}
	return v.Types[typ].Titles[i], true
}

func (v *vocab) value(typ, name string, f *spec.Field, i int) (string, bool) {
	if v == nil || f.Unique {
		return "", false
	}
	values := v.Types[typ].Fields[name]
	if len(values) == 0 {
		return "", false
	}
	return values[i%len(values)], true
}

// vocabCache remembers one answer (or failure) per definition fingerprint, so
// Check rounds and the preview share a single call.
type vocabCache struct {
	mu   sync.Mutex
	byID map[string]*vocab
}

func (c *vocabCache) get(ctx context.Context, model svc.Model, def *spec.Bundle) *vocab {
	if model == nil || def == nil || def.App == nil {
		return nil
	}
	key := fingerprint(def)
	c.mu.Lock()
	defer c.mu.Unlock()
	if v, ok := c.byID[key]; ok {
		return v
	}
	v, err := askVocab(ctx, model, def)
	if err != nil {
		if ctx.Err() != nil {
			return nil // a cancelled caller should not poison the cache
		}
		v = nil
	}
	if c.byID == nil {
		c.byID = map[string]*vocab{}
	}
	c.byID[key] = v
	return v
}

func fingerprint(def *spec.Bundle) string {
	h := sha256.New()
	data, _ := json.Marshal(struct {
		App  *spec.App
		Plan *spec.Plan
	}{def.App, def.Plan})
	h.Write(data)
	return hex.EncodeToString(h.Sum(nil))
}

const vocabSystem = `You write realistic sample records for a preview of a small app, as a product designer would for a demo.
Return one JSON object only: {"types":{"<type id>":{"titles":[...],"fields":{"<field id>":[...]}}}}.
Rules: titles are what a real user would type for this record (people: full names; companies: company names; items: short specific noun phrases, never starting with a verb, never "<Type>: ..."), all distinct, no numbering.
Field values fit the field's meaning and the app's domain; longtext values are one or two sentences. Use only the listed type and field ids. Never use real company domains; any email or link uses the .example domain.`

func askVocab(ctx context.Context, model svc.Model, def *spec.Bundle) (*vocab, error) {
	ctx, cancel := context.WithTimeout(ctx, vocabTimeout)
	defer cancel()
	reply, err := model.Complete(ctx, svc.ModelCall{
		System:   vocabSystem,
		Messages: []svc.ModelMessage{{Role: "user", Text: vocabPrompt(def)}},
		App:      def.App.ID,
		Role:     "sample data",
	})
	if err != nil {
		return nil, err
	}
	return parseVocab(reply.Text, def.App)
}

// vocabPrompt lists the app, the owner's ask and every type with the fields the
// model may fill: the title and non-unique text or longtext fields.
func vocabPrompt(def *spec.Bundle) string {
	var b strings.Builder
	fmt.Fprintf(&b, "App: %s\n", def.App.Name)
	if def.App.Description != "" {
		fmt.Fprintf(&b, "Description: %s\n", def.App.Description)
	}
	if def.Plan != nil && def.Plan.Ask != "" {
		fmt.Fprintf(&b, "Owner's ask: %s\n", def.Plan.Ask)
	}
	fmt.Fprintf(&b, "Give %d titles per type and %d values per listed field.\n", vocabTitles, vocabValues)
	for _, typ := range keys(def.App.Records) {
		schema := def.App.Records[typ]
		fmt.Fprintf(&b, "\nType %s (%s), title field %s\n", typ, label(schema.Label, typ), schema.Title)
		for _, name := range vocabFields(schema) {
			f := schema.Fields[name]
			fmt.Fprintf(&b, "- field %s: %s, %s\n", name, label(f.Label, name), f.Type)
		}
	}
	return b.String()
}

func vocabFields(schema *spec.RecordType) []string {
	var result []string
	for _, name := range keys(schema.Fields) {
		f := schema.Fields[name]
		if name != schema.Title && !f.Unique && f.Computed == "" && f.AI == "" && len(f.Default) == 0 && textual(f.Type) {
			result = append(result, name)
		}
		if len(result) == vocabFieldMax {
			break
		}
	}
	return result
}

func textual(kind spec.FieldType) bool {
	return kind == spec.FieldText || kind == spec.FieldLongtext
}

// parseVocab keeps only values that fit: known types and fields, readable
// distinct titles, bounded lengths. Anything else is dropped, not repaired.
func parseVocab(text string, app *spec.App) (*vocab, error) {
	text = strings.TrimSpace(text)
	if start, end := strings.Index(text, "{"), strings.LastIndex(text, "}"); start >= 0 && end > start {
		text = text[start : end+1]
	}
	var raw vocab
	if err := json.Unmarshal([]byte(text), &raw); err != nil {
		return nil, err
	}
	out := &vocab{Types: map[string]typeVocab{}}
	for typ, got := range raw.Types {
		schema := app.Records[typ]
		if schema == nil {
			continue
		}
		out.Types[typ] = typeVocab{Titles: cleanTitles(got.Titles), Fields: cleanFields(schema, got.Fields)}
	}
	if len(out.Types) == 0 {
		return nil, fmt.Errorf("sample vocabulary names no known record type")
	}
	return out, nil
}

func cleanTitles(values []string) []string {
	var result []string
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.Join(strings.Fields(value), " ")
		key := strings.ToLower(value)
		if readableTitle(value) && len(value) <= vocabTextMax && !seen[key] && !strings.Contains(value, ": ") {
			result = append(result, value)
			seen[key] = true
		}
	}
	return result
}

func cleanFields(schema *spec.RecordType, fields map[string][]string) map[string][]string {
	allowed := map[string]bool{}
	for _, name := range vocabFields(schema) {
		allowed[name] = true
	}
	result := map[string][]string{}
	for name, values := range fields {
		if !allowed[name] {
			continue
		}
		limit := vocabTextMax
		if schema.Fields[name].Type == spec.FieldLongtext {
			limit = vocabNoteMax
		}
		var kept []string
		for _, value := range values {
			value = strings.TrimSpace(value)
			if value != "" && len(value) <= limit && !strings.Contains(value, "_") {
				kept = append(kept, value)
			}
		}
		if len(kept) > 0 {
			result[name] = kept
		}
	}
	return result
}
