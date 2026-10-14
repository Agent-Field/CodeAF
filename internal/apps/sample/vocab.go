package sample

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

// Model rows: one call per definition returns whole rows per record type.
const (
	vocabRows    = 6                // rows asked for per type
	vocabMinRows = 4                // fewer usable rows than this and the generator fills the type
	vocabTextMax = 120              // longest text value kept
	vocabNoteMax = 400              // longest longtext value kept
	vocabTimeout = 20 * time.Second // past this the preview uses the fixed generator
	workdayStart = 9                // hour of the first sample datetime
	workdayHours = 9                // sample datetimes fall in 09:00–17:30
)

// vocab is model-written rows per record type, keyed by field id and still
// raw: every value is validated and coerced per field before use, and a value
// that does not fit is dropped so the generator fills just that field. A nil
// vocab means the deterministic generator.
type vocab struct {
	Types map[string][]map[string]json.RawMessage `json:"types"`
	index map[string]map[string]int               // type → lower-case title → row
}

// limit is the row count of a type: the model's rows when it wrote enough.
func (v *vocab) limit(typ string, count int) int {
	if v == nil || len(v.Types[typ]) < vocabMinRows {
		return count
	}
	return min(count, len(v.Types[typ]))
}

func (v *vocab) written(typ string) bool {
	return v != nil && len(v.Types[typ]) >= vocabMinRows
}

func (v *vocab) raw(typ, name string, i int) (json.RawMessage, bool) {
	if !v.written(typ) || i >= len(v.Types[typ]) {
		return nil, false
	}
	raw, ok := v.Types[typ][i][name]
	return raw, ok && string(raw) != "null"
}

// title is the model's title of row i, when it is a usable text title.
func (v *vocab) title(typ string, i int, schema *spec.RecordType) (string, bool) {
	raw, ok := v.raw(typ, schema.Title, i)
	if !ok {
		return "", false
	}
	var text string
	if json.Unmarshal(raw, &text) != nil {
		return "", false
	}
	text = strings.Join(strings.Fields(text), " ")
	return text, readableTitle(text) && len(text) <= vocabTextMax
}

// value coerces the model's value for one cell; ok is false when the model
// wrote none or it does not fit the field.
func (v *vocab) value(c cell) (any, bool) {
	raw, ok := v.raw(c.typ, c.name, c.i)
	coerce := coercions[c.f.Type]
	if !ok || coerce == nil || c.f.Unique && !textual(c.f.Type) {
		return nil, false
	}
	return coerce(c, raw)
}

// coercions turn a raw model value into a stored value of the field's type.
// Types without an entry (person, people, place, duration, files) always come
// from the generator.
var coercions = map[spec.FieldType]func(c cell, raw json.RawMessage) (any, bool){
	spec.FieldText:     coerceText,
	spec.FieldLongtext: coerceText,
	spec.FieldNumber:   coerceNumber,
	spec.FieldMoney:    coerceNumber,
	spec.FieldBool:     coerceBool,
	spec.FieldEnum:     coerceEnum,
	spec.FieldEnums:    coerceEnums,
	spec.FieldDate:     coerceDate,
	spec.FieldDatetime: coerceDate,
	spec.FieldRef:      coerceRef,
	spec.FieldRefs:     coerceRefs,
	spec.FieldEmail:    coerceEmail,
	spec.FieldURL:      coerceURL,
	spec.FieldPhone:    coercePhone,
}

func rawText(raw json.RawMessage) (string, bool) {
	var text string
	if json.Unmarshal(raw, &text) != nil {
		return "", false
	}
	text = strings.TrimSpace(text)
	return text, text != ""
}

func coerceText(c cell, raw json.RawMessage) (any, bool) {
	text, ok := rawText(raw)
	limit := vocabTextMax
	if c.f.Type == spec.FieldLongtext {
		limit = vocabNoteMax
	}
	if !ok || len(text) > limit || strings.Contains(text, "sample_") {
		return nil, false
	}
	if c.name == c.schema.Title && !readableTitle(text) {
		return nil, false
	}
	return text, true
}

func coerceNumber(_ cell, raw json.RawMessage) (any, bool) {
	var n float64
	if json.Unmarshal(raw, &n) == nil {
		return n, n >= 0
	}
	text, _ := rawText(raw)
	n, err := strconv.ParseFloat(strings.ReplaceAll(strings.TrimLeft(text, "$€£ "), ",", ""), 64)
	return n, err == nil && n >= 0
}

func coerceBool(_ cell, raw json.RawMessage) (any, bool) {
	var b bool
	return b, json.Unmarshal(raw, &b) == nil && (string(raw) == "true" || string(raw) == "false")
}

func enumValue(f *spec.Field, text string) (string, bool) {
	i := slices.IndexFunc(f.Values, func(v string) bool { return strings.EqualFold(v, strings.TrimSpace(text)) })
	if i < 0 {
		return "", false
	}
	return f.Values[i], true
}

func coerceEnum(c cell, raw json.RawMessage) (any, bool) {
	text, _ := rawText(raw)
	return enumValue(c.f, text)
}

func rawList(raw json.RawMessage) []string {
	var list []string
	if json.Unmarshal(raw, &list) != nil {
		if text, ok := rawText(raw); ok {
			list = []string{text}
		}
	}
	return list
}

func coerceEnums(c cell, raw json.RawMessage) (any, bool) {
	var kept []string
	for _, text := range rawList(raw) {
		if value, ok := enumValue(c.f, text); ok && !slices.Contains(kept, value) {
			kept = append(kept, value)
		}
	}
	return kept, len(kept) > 0
}

// Offsets are days from today with an optional time: "+3d", "-12d 14:30".
var offsetPattern = regexp.MustCompile(`^([+-]?\d{1,3})d(?:[ T]+(\d{1,2}):(\d{2}))?$`)

func coerceDate(c cell, raw json.RawMessage) (any, bool) {
	text, _ := rawText(raw)
	m := offsetPattern.FindStringSubmatch(strings.ToLower(text))
	if m == nil {
		return nil, false
	}
	days, _ := strconv.Atoi(m[1])
	day := c.now.Truncate(24*time.Hour).AddDate(0, 0, days)
	if c.f.Type == spec.FieldDate {
		return day.Format("2006-01-02"), true
	}
	hour, minute := workHour(c.i), 0
	if m[2] != "" {
		hour, _ = strconv.Atoi(m[2])
		minute, _ = strconv.Atoi(m[3])
	}
	if hour > 23 || minute > 59 {
		return nil, false
	}
	return day.Add(time.Duration(hour)*time.Hour + time.Duration(minute)*time.Minute).Format(time.RFC3339Nano), true
}

// resolve maps a target row's title, as the model wrote it, to that row's id.
func resolve(c cell, title string) (string, bool) {
	j, ok := c.words.index[c.f.To][strings.ToLower(strings.TrimSpace(title))]
	if !ok || j >= len(c.rows[c.f.To]) {
		return "", false
	}
	return c.rows[c.f.To][j].ID, true
}

func coerceRef(c cell, raw json.RawMessage) (any, bool) {
	text, _ := rawText(raw)
	return resolve(c, text)
}

func coerceRefs(c cell, raw json.RawMessage) (any, bool) {
	var ids []string
	for _, title := range rawList(raw) {
		if id, ok := resolve(c, title); ok && !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	return ids, len(ids) > 0
}

var emailPattern = regexp.MustCompile(`^[a-z0-9._+-]+@[a-z0-9-]+(\.[a-z0-9-]+)*\.example$`)

// coerceEmail keeps only addresses on the reserved .example domain.
func coerceEmail(_ cell, raw json.RawMessage) (any, bool) {
	text, _ := rawText(raw)
	text = strings.ToLower(text)
	return text, emailPattern.MatchString(text)
}

func coerceURL(_ cell, raw json.RawMessage) (any, bool) {
	text, _ := rawText(raw)
	host, _, _ := strings.Cut(strings.TrimPrefix(text, "https://"), "/")
	return text, strings.HasPrefix(text, "https://") && strings.HasSuffix(host, ".example") && !strings.ContainsAny(text, " ")
}

var phonePattern = regexp.MustCompile(`^\+?[0-9 ()-]{7,20}$`)

func coercePhone(_ cell, raw json.RawMessage) (any, bool) {
	text, _ := rawText(raw)
	return text, phonePattern.MatchString(text)
}

// vocabCache remembers one answer (or failure, including a timeout) per
// definition fingerprint, so Check rounds and the preview share one call.
// Callers for the same key wait on the call in flight; other keys never wait.
type vocabCache struct {
	mu   sync.Mutex
	byID map[string]*vocabEntry
}

type vocabEntry struct {
	done  chan struct{}
	words *vocab
}

func (c *vocabCache) get(ctx context.Context, model svc.Model, def *spec.Bundle, now time.Time) *vocab {
	if model == nil || def == nil || def.App == nil {
		return nil
	}
	key := fingerprint(def)
	entry, owner := c.claim(key)
	if !owner {
		select {
		case <-entry.done:
			return entry.words
		case <-ctx.Done():
			return nil
		}
	}
	words, err := askVocab(ctx, model, def, now)
	if err != nil && ctx.Err() != nil {
		// A cancelled caller must not poison the cache: forget the entry.
		c.forget(key, entry)
		return nil
	}
	entry.words = words
	close(entry.done)
	return words
}

// claim returns the entry for key and whether this caller must fill it.
func (c *vocabCache) claim(key string) (*vocabEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if entry, ok := c.byID[key]; ok {
		return entry, false
	}
	if c.byID == nil {
		c.byID = map[string]*vocabEntry{}
	}
	entry := &vocabEntry{done: make(chan struct{})}
	c.byID[key] = entry
	return entry, true
}

func (c *vocabCache) forget(key string, entry *vocabEntry) {
	c.mu.Lock()
	delete(c.byID, key)
	c.mu.Unlock()
	close(entry.done) // waiters see nil words and use the fixed generator
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

const vocabSystem = `You write the sample records a product designer would seed a demo of a small app with.
Return one compact JSON object only, no prose: {"types":{"<type id>":[{"<field id>":value,...},...]}}.
Write every listed type, with the listed number of complete rows each, using only the listed field ids.
Rules:
- Every row is internally consistent: its status agrees with its dates, amounts, flags and notes; a note never contradicts another field of the same row; a row named after a person or thing refers to that same person or thing.
- People are people (distinct full names), companies are companies, things are specific things a real user would type. No placeholders, no numbering, no month or type name standing in for a name, no repeated notes.
- Magnitudes are realistic for this domain (weights, hours, prices, quantities).
- ref and refs values are the exact title of a row you wrote for the target type; every target row you write is used by some row.
- date and datetime values are offsets from today: "-12d", "+3d"; datetimes add a working-hours time: "+3d 14:30". Past events are negative, upcoming ones positive, and an event named for a season or holiday falls near it.
- enum values are exactly one of the listed values; spread rows across statuses with most mid-pipeline.
- email and url values use made-up names on the .example domain (maya.chen@northwind.example, https://www.northwind.example).`

func askVocab(ctx context.Context, model svc.Model, def *spec.Bundle, now time.Time) (*vocab, error) {
	ctx, cancel := context.WithTimeout(ctx, vocabTimeout)
	defer cancel()
	reply, err := model.Complete(ctx, svc.ModelCall{
		System:   vocabSystem,
		Messages: []svc.ModelMessage{{Role: "user", Text: vocabPrompt(def, now)}},
		App:      def.App.ID,
		Role:     "sample data",
	})
	if err != nil {
		return nil, err
	}
	return parseVocab(reply.Text, def.App)
}

// vocabPrompt gives the app, the owner's paragraph, the plan's data lines,
// today's date and every type with the fields the model writes.
func vocabPrompt(def *spec.Bundle, now time.Time) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Today: %s\nApp: %s\n", now.Format("Monday 2006-01-02"), def.App.Name)
	if def.App.Description != "" {
		fmt.Fprintf(&b, "Description: %s\n", def.App.Description)
	}
	if def.Plan != nil {
		if def.Plan.Ask != "" {
			fmt.Fprintf(&b, "Owner's ask: %s\n", def.Plan.Ask)
		}
		for _, line := range def.Plan.Data {
			fmt.Fprintf(&b, "Plan: %s\n", line.Line)
		}
	}
	counts := rowCounts(def.App, vocabRows, nil)
	for _, typ := range keys(def.App.Records) {
		schema := def.App.Records[typ]
		fmt.Fprintf(&b, "\nType %s (%s): %d rows, title field %s\n", typ, label(schema.Label, typ), counts[typ], schema.Title)
		for _, name := range vocabFields(schema) {
			fmt.Fprintf(&b, "- %s\n", fieldLine(name, schema.Fields[name]))
		}
	}
	return b.String()
}

func fieldLine(name string, f *spec.Field) string {
	line := fmt.Sprintf("%s (%s): %s", name, label(f.Label, name), f.Type)
	switch {
	case len(f.Values) > 0:
		line += " of " + strings.Join(f.Values, " | ")
	case f.To != "":
		line += " to " + f.To
	}
	if f.Unique {
		line += ", unique"
	}
	return line
}

// vocabFields are the fields the model writes: those with a coercion that are
// not computed.
func vocabFields(schema *spec.RecordType) []string {
	var result []string
	for _, name := range keys(schema.Fields) {
		f := schema.Fields[name]
		if f.Computed == "" && coercions[f.Type] != nil && (!f.Unique || textual(f.Type)) {
			result = append(result, name)
		}
	}
	return result
}

func textual(kind spec.FieldType) bool {
	return kind == spec.FieldText || kind == spec.FieldLongtext
}

// parseVocab keeps the rows of known types and indexes their titles for refs.
// Values stay raw; each is coerced, or dropped, when its cell is filled.
func parseVocab(text string, app *spec.App) (*vocab, error) {
	text = strings.TrimSpace(text)
	if start, end := strings.Index(text, "{"), strings.LastIndex(text, "}"); start >= 0 && end > start {
		text = text[start : end+1]
	}
	var raw vocab
	if err := json.Unmarshal([]byte(text), &raw); err != nil {
		return nil, err
	}
	out := &vocab{Types: map[string][]map[string]json.RawMessage{}, index: map[string]map[string]int{}}
	for typ, rows := range raw.Types {
		schema := app.Records[typ]
		if schema == nil || len(rows) < vocabMinRows {
			continue
		}
		out.Types[typ] = rows
		out.index[typ] = map[string]int{}
		for i, row := range rows {
			if title, ok := rawText(row[schema.Title]); ok {
				if _, taken := out.index[typ][strings.ToLower(title)]; !taken {
					out.index[typ][strings.ToLower(title)] = i
				}
			}
		}
	}
	if len(out.Types) == 0 {
		return nil, fmt.Errorf("sample rows name no known record type")
	}
	return out, nil
}
