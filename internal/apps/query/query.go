package query

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/Agent-Field/codeaf/internal/apps/expr"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
)

// Op is a typed comparison, never caller-supplied SQL.
type Op string

const (
	Eq       Op = "eq"
	Ne       Op = "ne"
	Lt       Op = "lt"
	Gt       Op = "gt"
	Le       Op = "le"
	Ge       Op = "ge"
	In       Op = "in"
	Contains Op = "contains"
	IsNull   Op = "isnull"
)

type Predicate struct {
	Field  string
	Op     Op
	Values []any
	Not    bool
}
type Sort struct {
	Field string
	Desc  bool
}
type AST struct {
	Filters []Predicate
	Sort    Sort
}

// PermissionFilter adds constraints before compilation. Personal space uses Identity.
type PermissionFilter func(AST) (AST, error)

func Identity(a AST) (AST, error) { return a, nil }

var slug = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,47}$`)

func ValidName(name string) bool { return slug.MatchString(name) }
func Table(recordType string) (string, error) {
	if !ValidName(recordType) {
		return "", fmt.Errorf("invalid record type %q", recordType)
	}
	return `"records_` + recordType + `"`, nil
}
func Column(field string) string { return `"field_` + field + `"` }
func Indexed(f *spec.Field) bool {
	return f != nil && (f.Index || f.Unique || f.Type == spec.FieldRef || f.Type == spec.FieldRefs || f.Type == spec.FieldEnum || f.Type == spec.FieldEnums)
}

// Expression resolves only fields declared by the schema, keeping identifiers out of input SQL.
func Expression(schema *spec.RecordType, field string) (string, error) {
	switch field {
	case "id":
		return `id`, nil
	case "created_at", "updated_at", "created_by", "updated_by", "version":
		return "json_extract(record, '$." + field + "')", nil
	}
	f := schema.Fields[field]
	if f == nil || !ValidName(field) {
		return "", fmt.Errorf("unknown field %q", field)
	}
	if Indexed(f) {
		return Column(field), nil
	}
	return "json_extract(record, '$.fields.\"" + field + "\"')", nil
}

// Parse accepts QueryField tokens. Quoted values can contain spaces and commas.
func Parse(schema *spec.RecordType, text, member string) (AST, error) {
	return ParseAt(schema, text, member, time.Now())
}

// ParseAt is Parse with the request clock: a date or datetime value written as
// an expr.Offset literal (`due<7d`, `last_touch<-7d`, `due<0d`) resolves
// against now, so a screen filter stays current without a computed field.
func ParseAt(schema *spec.RecordType, text, member string, now time.Time) (AST, error) {
	a := AST{Sort: Sort{Field: "id"}}
	tokens, err := split(normalize(schema, text), unicode.IsSpace)
	if err != nil {
		return a, err
	}
	for _, token := range dayRanges(schema, tokens, now) {
		if strings.HasPrefix(token, "sort:") {
			value := strings.TrimPrefix(token, "sort:")
			a.Sort = Sort{Field: strings.TrimPrefix(value, "-"), Desc: strings.HasPrefix(value, "-")}
			if _, err := Expression(schema, a.Sort.Field); err != nil {
				return a, err
			}
			continue
		}
		p, err := parseToken(schema, comparisonToken(token), member, now)
		if err != nil {
			return a, fmt.Errorf("query token %q: %w; query tokens are field<7d, field>=-30d, field:today, field:value; not CEL", token, err)
		}
		a.Filters = append(a.Filters, p)
	}
	return a, nil
}
func parseToken(schema *spec.RecordType, token, member string, now time.Time) (Predicate, error) {
	pos := strings.IndexAny(token, ":<>!=~")
	if pos < 1 {
		return Predicate{}, fmt.Errorf("expected field:value in %q", token)
	}
	field, value := token[:pos], token[pos+1:]
	p := Predicate{Field: field, Op: Eq}
	switch token[pos] {
	case '<':
		p.Op = Lt
		if strings.HasPrefix(value, "=") {
			p.Op, value = Le, value[1:]
		}
	case '>':
		p.Op = Gt
		if strings.HasPrefix(value, "=") {
			p.Op, value = Ge, value[1:]
		}
	case '!':
		if !strings.HasPrefix(value, "=") {
			return p, fmt.Errorf("expected !=")
		}
		p.Op, value = Ne, value[1:]
	case '=':
		value = strings.TrimPrefix(value, "=")
	case '~':
		p.Op = Contains
	}
	if field == "has" {
		p.Field, p.Op, p.Not = value, IsNull, true
		value = ""
	}
	if field == "is" {
		p.Field, value = value, "true"
	}
	if _, err := Expression(schema, p.Field); err != nil {
		return p, err
	}
	if p.Op == IsNull {
		return p, nil
	}
	if value == "" {
		return p, fmt.Errorf("missing value for %s", field)
	}
	if strings.HasPrefix(value, "-") && token[pos] == ':' {
		// A leading minus excludes text; negative numeric values retain their sign.
		f := schema.Fields[field]
		if f == nil || (f.Type != spec.FieldNumber && f.Type != spec.FieldMoney) {
			p.Not, value = true, value[1:]
		}
	}
	if value == "null" {
		p.Op = IsNull
		return p, nil
	}
	parts, err := split(value, func(r rune) bool { return r == ',' })
	if err != nil || len(parts) == 0 {
		return p, fmt.Errorf("invalid values for %s", field)
	}
	if len(parts) > 1 {
		if p.Op != Eq {
			return p, fmt.Errorf("multiple values require equality")
		}
		p.Op = In
	}
	for _, raw := range parts {
		v, err := unquote(raw)
		if err != nil {
			return p, err
		}
		if v == "me" && schema.Fields[field] != nil && (schema.Fields[field].Type == spec.FieldPerson || schema.Fields[field].Type == spec.FieldPeople) {
			v = member
		}
		if strings.Contains(v, "*") && p.Op == Eq {
			p.Op = Contains
		}
		f := schema.Fields[p.Field]
		if p.Field == "version" {
			f = &spec.Field{Type: spec.FieldNumber}
		}
		if p.Field == "created_at" || p.Field == "updated_at" {
			f = &spec.Field{Type: spec.FieldDatetime}
		}
		typed, err := valueFor(f, relativeValue(f, v, now))
		if err != nil {
			return p, err
		}
		p.Values = append(p.Values, typed)
	}
	return p, nil
}

// comparisonToken drops the colon a writer puts before a comparison
// (`due:<0d` is `due<0d`), so both spellings mean the same token.
func comparisonToken(token string) string {
	field, rest, found := strings.Cut(token, ":")
	if found && rest != "" && (rest[0] == '<' || rest[0] == '>') {
		return field + rest
	}
	return token
}

// dayRanges turns `field:today` (or tomorrow, yesterday) on a date or
// datetime field into the two comparisons that bound that day, so a day word
// means "within that day" whatever the stored precision.
func dayRanges(schema *spec.RecordType, tokens []string, now time.Time) []string {
	out := make([]string, 0, len(tokens))
	for _, token := range tokens {
		field, word, found := strings.Cut(token, ":")
		f := schema.Fields[field]
		if !found || !expr.IsDayWord(word) || f == nil || (f.Type != spec.FieldDate && f.Type != spec.FieldDatetime) {
			out = append(out, token)
			continue
		}
		start, _ := expr.RelativeTime(word, now)
		layout := time.RFC3339Nano
		if f.Type == spec.FieldDate {
			layout = "2006-01-02"
		}
		out = append(out, field+">="+start.Format(layout), field+"<"+start.AddDate(0, 0, 1).Format(layout))
	}
	return out
}

// relativeValue rewrites an Offset literal on a date or datetime field into
// the stored form valueFor expects; every other value passes through.
func relativeValue(f *spec.Field, value string, now time.Time) string {
	if f == nil || (f.Type != spec.FieldDate && f.Type != spec.FieldDatetime) {
		return value
	}
	at, ok := expr.RelativeTime(value, now)
	switch {
	case !ok:
		return value
	case f.Type == spec.FieldDate:
		return at.Format("2006-01-02")
	}
	return at.UTC().Format(time.RFC3339Nano)
}
func valueFor(f *spec.Field, value string) (any, error) {
	if f == nil {
		return value, nil
	}
	switch f.Type {
	case spec.FieldNumber, spec.FieldMoney:
		n, err := strconv.ParseFloat(value, 64)
		if err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
			return nil, fmt.Errorf("expected a finite number")
		}
		return n, nil
	case spec.FieldDate:
		if _, err := time.Parse("2006-01-02", value); err != nil {
			return nil, err
		}
		return value, nil
	case spec.FieldDatetime:
		t, err := time.Parse(time.RFC3339Nano, value)
		if err != nil {
			return nil, err
		}
		return t.UTC().Format("2006-01-02T15:04:05.000000000Z"), nil
	case spec.FieldBool:
		return strconv.ParseBool(value)
	}
	return value, nil
}
func split(text string, separator func(rune) bool) ([]string, error) {
	var result []string
	start, quote, escape := 0, rune(0), false
	for i, r := range text {
		if escape {
			escape = false
			continue
		}
		if quote != 0 {
			if r == '\\' {
				escape = true
			} else if r == quote {
				quote = 0
			}
			continue
		}
		if r == '\'' || r == '"' {
			quote = r
			continue
		}
		if separator(r) {
			if i > start {
				result = append(result, text[start:i])
			}
			start = i + len(string(r))
		}
	}
	if quote != 0 || escape {
		return nil, fmt.Errorf("unterminated quoted value")
	}
	if start < len(text) {
		result = append(result, text[start:])
	}
	return result, nil
}
func unquote(value string) (string, error) {
	if strings.HasPrefix(value, `"`) {
		return strconv.Unquote(value)
	}
	if strings.HasPrefix(value, "'") && strings.HasSuffix(value, "'") {
		return strings.ReplaceAll(value[1:len(value)-1], `\'`, `'`), nil
	}
	return value, nil
}

// Plan holds the reusable predicate (for counts) and stable order (for pages).
type Plan struct {
	Where string
	Args  []any
	Sort  Sort
	Expr  string
	AST   AST
}

func Compile(schema *spec.RecordType, a AST, permission PermissionFilter) (Plan, error) {
	if permission == nil {
		permission = Identity
	}
	a, err := permission(a)
	if err != nil {
		return Plan{}, err
	}
	if a.Sort.Field == "" {
		a.Sort.Field = "id"
	}
	expr, err := Expression(schema, a.Sort.Field)
	if err != nil {
		return Plan{}, err
	}
	if f := schema.Fields[a.Sort.Field]; f != nil && multiple(f.Type) {
		return Plan{}, fmt.Errorf("cannot sort a collection")
	}
	plan := Plan{Where: "1", Sort: a.Sort, Expr: expr, AST: a}
	var terms []string
	for _, p := range a.Filters {
		sql, args, err := predicate(schema, p)
		if err != nil {
			return Plan{}, err
		}
		terms = append(terms, sql)
		plan.Args = append(plan.Args, args...)
	}
	if len(terms) > 0 {
		plan.Where = strings.Join(terms, " AND ")
	}
	return plan, nil
}
func predicate(schema *spec.RecordType, p Predicate) (string, []any, error) {
	expr, err := Expression(schema, p.Field)
	if err != nil {
		return "", nil, err
	}
	if p.Op == IsNull {
		op := " IS NULL"
		if p.Not {
			op = " IS NOT NULL"
		}
		return expr + op, nil, nil
	}
	if len(p.Values) == 0 {
		return "", nil, fmt.Errorf("missing predicate values")
	}
	f := schema.Fields[p.Field]
	array := f != nil && multiple(f.Type)
	target := expr
	if array {
		target = "element.value"
	}
	var sql string
	args := append([]any(nil), p.Values...)
	switch p.Op {
	case Eq, Ne, Lt, Gt, Le, Ge:
		if len(args) != 1 {
			return "", nil, fmt.Errorf("comparison needs one value")
		}
		ops := map[Op]string{Eq: "=", Ne: "!=", Lt: "<", Gt: ">", Le: "<=", Ge: ">="}
		sql = target + " " + ops[p.Op] + " ?"
	case In:
		sql = target + " IN (" + strings.TrimSuffix(strings.Repeat("?,", len(args)), ",") + ")"
	case Contains:
		if len(args) != 1 {
			return "", nil, fmt.Errorf("contains needs one value")
		}
		value, ok := args[0].(string)
		if !ok {
			return "", nil, fmt.Errorf("contains needs text")
		}
		if array {
			sql = target + " = ?"
		} else {
			sql = target + ` LIKE ? ESCAPE '\'`
			args[0] = pattern(value)
		}
	default:
		return "", nil, fmt.Errorf("unknown comparison %q", p.Op)
	}
	if array {
		if p.Op == Ne {
			sql = "NOT EXISTS (SELECT 1 FROM json_each(" + expr + ") AS element WHERE element.value = ?)"
		} else {
			sql = "EXISTS (SELECT 1 FROM json_each(" + expr + ") AS element WHERE " + sql + ")"
		}
	}
	if p.Not {
		sql = "COALESCE(NOT (" + sql + "), 1)"
	}
	return "(" + sql + ")", args, nil
}
func multiple(t spec.FieldType) bool {
	return t == spec.FieldEnums || t == spec.FieldPeople || t == spec.FieldRefs || t == spec.FieldFiles
}
func pattern(value string) string {
	wildcard := strings.Contains(value, "*")
	value = strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`, "*", "%").Replace(value)
	if !wildcard {
		value = "%" + value + "%"
	}
	return value
}

// Fingerprint binds a cursor to its typed query, schema type and permission filters.
func (p Plan) Fingerprint(recordType string) string {
	data, _ := json.Marshal(struct {
		Type string
		AST  AST
	}{recordType, p.AST})
	return string(data)
}
