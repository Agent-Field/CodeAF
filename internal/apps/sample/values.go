package sample

import (
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

// cell is everything one generated value may depend on.
type cell struct {
	app    *spec.App
	rows   map[string][]bridge.Record
	typ    string
	name   string
	schema *spec.RecordType
	f      *spec.Field
	i      int
	rng    *rand.Rand
	now    time.Time
	title  string // the row's title, filled before every other field
	words  *vocab
}

func fieldValue(c cell) (any, error) {
	if c.name == c.schema.Title && (c.f.Type == spec.FieldText || c.f.Type == spec.FieldLongtext) {
		if title, ok := c.words.title(c.typ, c.i, c.schema); ok {
			return title, nil
		}
		return rowTitle(c), nil
	}
	if value, ok := c.words.value(c); ok {
		return value, nil
	}
	if useDefault(c.f) {
		return defaultValue(c.f)
	}
	switch c.f.Type {
	case spec.FieldText:
		return textValue(c.typ, c.name, c.schema, c.f, c.i), nil
	case spec.FieldLongtext:
		return noteValue(c.title, c.i+len(c.name)), nil
	case spec.FieldDate, spec.FieldDatetime:
		return dateValue(c.name, c.f, c.i, c.rng, c.now), nil
	case spec.FieldEnum, spec.FieldEnums:
		return choiceValue(c.name, c.f, c.i)
	case spec.FieldRef, spec.FieldRefs:
		return referenceValue(c.rows[c.f.To], c.f, c.i)
	case spec.FieldNumber:
		return numberValue(c.name+" "+c.f.Label, numberRanges, defaultNumber, c.rng, c.f.Unique, c.i), nil
	case spec.FieldMoney:
		return numberValue(c.name+" "+c.f.Label, moneyRanges, defaultMoney, c.rng, c.f.Unique, c.i), nil
	default:
		return scalarValue(c, c.f.Type)
	}
}

func useDefault(f *spec.Field) bool {
	if len(f.Default) == 0 {
		return false
	}
	// Defaults are stored values, not sample hints: relations need generated
	// targets, dates need today's clock, enums need all states on display, and
	// numbers and checkboxes start at their default but a lived-in list moved on.
	switch f.Type {
	case spec.FieldRef, spec.FieldRefs, spec.FieldDate, spec.FieldDatetime, spec.FieldEnum, spec.FieldNumber, spec.FieldMoney, spec.FieldBool:
		return false
	default:
		return !f.Unique
	}
}

func defaultValue(f *spec.Field) (any, error) {
	var value any
	if err := json.Unmarshal(f.Default, &value); err != nil {
		return nil, err
	}
	if text, ok := value.(string); ok && text == "$me" {
		value = svc.Owner
	}
	return value, nil
}

// choiceValue shows every value once, then weights pipeline fields (status,
// stage, phase) toward their middle states the way a live list looks.
func choiceValue(name string, f *spec.Field, i int) (any, error) {
	if len(f.Values) == 0 || (f.Unique && i >= len(f.Values)) {
		return nil, fmt.Errorf("not enough enum values")
	}
	value := f.Values[choiceIndex(name+" "+f.Label, len(f.Values), i, f.Unique)]
	if f.Type == spec.FieldEnums {
		return []string{value}, nil
	}
	return value, nil
}

func choiceIndex(semantic string, n, i int, unique bool) int {
	if unique || i < n || n < 3 || !hasWord(semantic, "status", "state", "stage", "phase", "pipeline", "progress") {
		return i % n
	}
	return 1 + (i-n)%(n-2)
}

func referenceValue(targets []bridge.Record, f *spec.Field, i int) (any, error) {
	if len(targets) == 0 {
		return nil, fmt.Errorf("no target records for %s", f.To)
	}
	id := targets[i%len(targets)].ID
	if f.Type == spec.FieldRefs {
		ids := []string{id}
		if len(targets) > 2 && i%2 == 0 {
			ids = append(ids, targets[(i+len(targets)/2)%len(targets)].ID)
		}
		return ids, nil
	}
	return id, nil
}

// numberRange is a plausible span for a field named with one of its words.
type numberRange struct {
	words    []string
	min, max float64
	step     float64
}

var moneyRanges = []numberRange{
	{[]string{"salary", "wage", "compensation"}, 45000, 160000, 1000},
	{[]string{"budget", "revenue", "arr", "contract"}, 2000, 60000, 500},
	{[]string{"amount", "value", "total", "deal", "invoice"}, 500, 25000, 50},
	{[]string{"price", "cost", "msrp"}, 12, 480, 1},
	{[]string{"fee", "deposit", "rent"}, 50, 1500, 5},
	{[]string{"rate", "hourly"}, 40, 250, 5},
}
var defaultMoney = numberRange{min: 100, max: 5000, step: 25}

var numberRanges = []numberRange{
	{[]string{"rating", "stars", "score"}, 1, 5, 1},
	{[]string{"percent", "percentage", "progress", "probability", "completion", "pct"}, 5, 95, 5},
	{[]string{"quantity", "qty", "count", "stock", "units", "inventory", "seats", "capacity", "attendees"}, 2, 180, 1},
	{[]string{"hours", "hrs"}, 0.5, 8, 0.5},
	{[]string{"minutes", "duration", "days", "length"}, 15, 90, 15},
	{[]string{"age"}, 18, 70, 1},
	{[]string{"year"}, 2019, 2026, 1},
	{[]string{"weight", "kg", "lbs"}, 1, 60, 0.5},
}
var defaultNumber = numberRange{min: 1, max: 100, step: 1}

func numberValue(semantic string, ranges []numberRange, fallback numberRange, rng *rand.Rand, unique bool, i int) float64 {
	r := fallback
	for _, candidate := range ranges {
		if hasWord(semantic, candidate.words...) {
			r = candidate
			break
		}
	}
	steps := int(math.Max(1, (r.max-r.min)/r.step))
	n := rng.Intn(steps + 1)
	if unique {
		// Unique numbers step through the range and continue past it if needed.
		n = i
	}
	return r.min + float64(n)*r.step
}

func scalarValue(c cell, kind spec.FieldType) (any, error) {
	switch kind {
	case spec.FieldDuration:
		return fmt.Sprintf("PT%dH", c.i%8+1), nil
	case spec.FieldBool:
		return boolValue(c.name+" "+c.f.Label, c.i), nil
	case spec.FieldPerson:
		return svc.Owner, nil
	case spec.FieldPeople:
		return []string{svc.Owner}, nil
	default:
		return addressValue(c, kind)
	}
}

// Flags mark exceptions, so few rows carry them; other checkboxes are mostly on.
var flagWords = []string{"flagged", "flag", "overdue", "late", "risk", "blocked", "urgent", "missed", "cancelled", "vip", "archived", "disputed"}

func boolValue(semantic string, i int) bool {
	if hasWord(semantic, flagWords...) {
		return i%4 == 1
	}
	return i%3 != 2
}

// contactName is the person a row's contact fields belong to: the row itself
// when it is a person, otherwise a name chosen by row.
func contactName(c cell) string {
	if personRecord(c.typ, c.schema) && c.title != "" {
		return c.title
	}
	return personName(c.i)
}

func personRecord(typ string, schema *spec.RecordType) bool {
	return kindOf(typ, schema).name == kindPerson
}

func addressValue(c cell, kind spec.FieldType) (any, error) {
	i := c.i
	switch kind {
	case spec.FieldEmail:
		return emailFor(contactName(c), i), nil
	case spec.FieldPhone:
		return fmt.Sprintf("+1 416 555 %04d", i+100), nil
	case spec.FieldURL:
		return linkValue(c), nil
	case spec.FieldFile:
		return fileName(c), nil
	case spec.FieldFiles:
		return []string{fileName(c)}, nil
	case spec.FieldPlace:
		return map[string]any{"lat": 43.65 + float64(i)*0.001, "lng": -79.38 + float64(i)*0.001, "label": cities[i%len(cities)]}, nil
	default:
		return nil, fmt.Errorf("unknown field type %q", kind)
	}
}

// linkValue is a company's home page for a company, or a page about the row
// on a company site for anything else.
func linkValue(c cell) string {
	site := "https://www." + orgDomain(c.i)
	if kindOf(c.typ, c.schema).name == kindOrg || c.title == "" {
		return site
	}
	return site + "/" + slug(c.title, "-")
}

// fileKinds give a file field the extension its name suggests.
var fileKinds = []struct {
	words []string
	ext   string
}{
	{[]string{"audio", "recording", "podcast", "voice", "track", "mix"}, "mp3"},
	{[]string{"video", "footage", "clip", "reel"}, "mp4"},
	{[]string{"photo", "image", "picture", "artwork", "logo", "cover", "headshot", "thumbnail", "avatar", "scan"}, "jpg"},
	{[]string{"spreadsheet", "sheet", "budget", "ledger"}, "xlsx"},
}

// fileName reads like an uploaded file: "Contract – Harbor Group.pdf".
func fileName(c cell) string {
	semantic := c.name + " " + c.f.Label
	ext := "pdf"
	for _, kind := range fileKinds {
		if hasWord(semantic, kind.words...) {
			ext = kind.ext
			break
		}
	}
	name := upperFirst(label(c.f.Label, c.name))
	if c.title == "" {
		return name + "." + ext
	}
	return name + " – " + c.title + "." + ext
}
