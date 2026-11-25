package data

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	appschema "github.com/Agent-Field/codeaf/internal/apps/schema"
)

// QUERIES ARE THE TOKENS A PERSON TYPES, compiled into parameterised SQL.
//
//	stage:Proposal,Won     one of these values (enum, text, person, ref…)
//	stage:-Lost  -stage:Lost   not this value (records with no value included)
//	amount>10000 close_on<=2026-12-31 last_touch<-14d   comparisons; -14d is
//	                       fourteen days ago, +7d a week ahead, today and now
//	name:north*  email:*@globex.com   * is a wildcard on text
//	owner:me               the person
//	company:Northwind      a ref matches its record's id or its title
//	is:urgent  -is:urgent  a bool field is (not) true
//	has:phone  -has:phone  a field has (no) value
//	sort:-amount  group:stage   order and grouping; the last of each wins
//	"two words"  north      free words search the title and text fields
//
// A token whose name is not a field is a free word, so text pasted into the
// search box never fails as a query. A sort, group, is: or has: naming no
// field is refused: those are only ever typed on purpose.
//
//	company.industry:SaaS  a field of the record a link points to, one link away
//
// THE SQL HOLDS NO VALUE FROM A QUERY. Values are bound parameters; fields
// and types reach SQL only as the ids the store made, checked again by
// mustID where they are spliced.

// The bounds of a query.
const (
	// PageDefault is the rows one page answers when none is asked.
	PageDefault = 100
	// PageMax is the most rows one page answers.
	PageMax = 500
	// GroupsMax is the most groups one count answers.
	GroupsMax = 500
	// queryMax is the longest query text read.
	queryMax = 4096
	// tokensMax is the most tokens one query has.
	tokensMax = 64
)

// compiled is a query ready to run: its conditions over its row alias
// (r, or x inside a related record's test), their parameters, its sort and
// its group.
type compiled struct {
	t     *Type
	sch   *schema
	alias string
	// refTitle answers the title field of a type a ref points to.
	refTitle func(string) string
	where    []string
	args     []any
	sort     sortSpec
	group    string
}

// col is the SQL reading field f of the row this query tests.
func (c *compiled) col(f *Field) string { return jsonPath(c.alias+".data_json", f.ID) }

// eachOf is the SQL listing the items of list field f of the row.
func (c *compiled) eachOf(f *Field) string {
	return "json_each(" + c.alias + `.data_json, '$."` + mustID(f.ID) + `"')`
}

// sortSpec is how rows are ordered: an expression, its parameters, and its
// direction. Rows with no value come last in either direction, and the id
// breaks ties, so the order is total and a cursor can resume it.
type sortSpec struct {
	field string
	desc  bool
	expr  string
	args  []any
}

func (s sortSpec) signature() string {
	if s.desc {
		return "-" + s.field
	}
	return s.field
}

// pseudo are the row's own times, which every type can be queried and sorted
// by as datetime fields.
var pseudo = map[string]string{"created_at": "r.created_at", "updated_at": "r.updated_at"}

// compileQuery reads a query's tokens against t for scope sc: the tokens
// become the query IR, and the IR becomes SQL.
func compileQuery(sch *schema, t *Type, sc Scope, text string, now time.Time) (*compiled, error) {
	ir, err := ParseQuery(text)
	if err != nil {
		return nil, err
	}
	return compileIR(sch, t, sc, ir, now)
}

// compileIR compiles a query's IR against t for scope sc.
func compileIR(sch *schema, t *Type, sc Scope, ir *QueryIR, now time.Time) (*compiled, error) {
	c := &compiled{t: t, sch: sch, alias: "r", refTitle: refTitles(sch, sc), sort: sortSpec{field: "created_at", desc: true, expr: "r.created_at"}}
	if len(ir.Where) > tokensMax {
		return nil, errorf(CodeInvalid, "the query has more than %d parts", tokensMax)
	}
	for _, cond := range ir.Where {
		if err := c.cond(cond, sc, now); err != nil {
			return nil, err
		}
	}
	if ir.Sort != "" {
		if err := c.sortBy(ir.Sort, sc); err != nil {
			return nil, err
		}
	}
	if len(ir.Group) > 0 {
		f, err := c.usable(ir.Group[0], sc, "group by")
		if err != nil {
			return nil, err
		}
		c.group = f.Name
	}
	return c, nil
}

// token is one word of a query: its text with quotes removed, whether it was
// a quoted phrase, and where the value began (after the operator).
type token struct {
	raw    string
	phrase bool
}

// tokenize splits on spaces, keeping a quoted stretch (anywhere in a token)
// inside its token.
func tokenize(text string) ([]token, error) {
	var out []token
	var cur strings.Builder
	quoted, inQuote, started := false, false, false
	flush := func() {
		if started {
			out = append(out, token{raw: cur.String(), phrase: quoted && !strings.ContainsAny(strings.SplitN(cur.String(), "\"", 2)[0], ":<>=")})
		}
		cur.Reset()
		quoted, started = false, false
	}
	for _, r := range text {
		switch {
		case r == '"':
			inQuote = !inQuote
			quoted, started = true, true
			cur.WriteRune(r)
		case !inQuote && (r == ' ' || r == '\t' || r == '\n' || r == '\r'):
			flush()
		default:
			started = true
			cur.WriteRune(r)
		}
	}
	if inQuote {
		return nil, errorf(CodeInvalid, "a quote in the query is not closed")
	}
	flush()
	return out, nil
}

// unquote removes the quotes a value was typed with.
func unquote(s string) string { return strings.ReplaceAll(s, "\"", "") }

// splitValues splits a value list on commas outside quotes.
func splitValues(s string) []string {
	var out []string
	var cur strings.Builder
	in := false
	for _, r := range s {
		switch {
		case r == '"':
			in = !in
		case r == ',' && !in:
			out = append(out, cur.String())
			cur.Reset()
		default:
			cur.WriteRune(r)
		}
	}
	return append(out, cur.String())
}

// operators are the operators a token may hold, longest first.
var operators = []string{">=", "<=", ":", ">", "<", "="}

// parseToken reads one token into the IR: a condition, or a sort or group.
func parseToken(tok token, ir *QueryIR) {
	text := tok.raw
	neg := false
	if strings.HasPrefix(text, "-") && len(text) > 1 {
		neg, text = true, text[1:]
	}
	if tok.phrase {
		ir.Where = append(ir.Where, Cond{Value: unquote(text), Neg: neg})
		return
	}
	name, op, value := "", "", ""
	end := 0
	dot := false
	for end < len(text) {
		ch := text[end]
		if ch == '.' && !dot && end > 0 && end+1 < len(text) && text[end+1] >= 'a' && text[end+1] <= 'z' {
			dot = true
			end++
			continue
		}
		if !(ch == '_' || ch >= 'a' && ch <= 'z' || ch >= '0' && ch <= '9') {
			break
		}
		end++
	}
	if end > 0 {
		for _, o := range operators {
			if strings.HasPrefix(text[end:], o) {
				name, op, value = text[:end], o, text[end+len(o):]
				break
			}
		}
	}
	if op == "" {
		ir.Where = append(ir.Where, Cond{Value: unquote(text), Neg: neg})
		return
	}
	switch name {
	case "sort":
		ir.Sort = unquote(value)
		return
	case "group":
		ir.Group = []string{unquote(value)}
		return
	case "is", "has":
		if op == ":" {
			ir.Where = append(ir.Where, Cond{Field: unquote(value), Op: name, Neg: neg, raw: tok.raw})
			return
		}
	}
	if op == "=" {
		op = ":"
	}
	ir.Where = append(ir.Where, Cond{Field: name, Op: op, Value: value, Neg: neg, raw: tok.raw})
}

// cond compiles one condition of the IR into c. A field that is not one is
// a free word, so text pasted into the search box never fails as a query; a
// related field (company.industry) tests the record the ref points to.
func (c *compiled) cond(cond Cond, sc Scope, now time.Time) error {
	if cond.Field == "" {
		return c.words(cond.Value, cond.Neg, sc)
	}
	if head, tail, ok := strings.Cut(cond.Field, "."); ok {
		return c.relate(head, tail, cond, sc, now)
	}
	if cond.Op == "is" || cond.Op == "has" {
		return c.flag(cond.Op, cond.Field, cond.Neg, sc)
	}
	if col, ok := pseudo[cond.Field]; ok {
		f := &Field{Name: cond.Field, FieldDef: FieldDef{Field: appschema.Field{Type: TypeDatetime}}}
		return c.compare(f, strings.Replace(col, "r.", c.alias+".", 1), cond.Op, cond.Value, cond.Neg, now)
	}
	f := c.t.field(cond.Field)
	if f == nil {
		if cond.raw == "" {
			return fieldErr(cond.Field, "%s records have no field %q", c.t.Name, cond.Field)
		}
		return c.words(unquote(cond.raw), false, sc)
	}
	if f.Masked && sc.App != "" {
		return fieldErr(f.Name, "%s is hidden until revealed, so it cannot be searched", f.Name)
	}
	return c.compare(f, c.col(f), cond.Op, cond.Value, cond.Neg, now)
}

// relate compiles a test of the record a ref points to: one hop, through a
// ref or refs field, into a type the scope may read.
func (c *compiled) relate(head, tail string, cond Cond, sc Scope, now time.Time) error {
	f := c.t.field(head)
	if f == nil || f.Type != TypeRef && f.Type != TypeRefs {
		return fieldErr(head, "%s is not a link to another record, so %s.%s means nothing", head, head, tail)
	}
	if strings.Contains(tail, ".") {
		return fieldErr(cond.Field, "a query reaches one link away, not two")
	}
	target, err := c.sch.allow(sc, f.To, false)
	if err != nil {
		return err
	}
	inner := &compiled{t: target, sch: c.sch, alias: "x", refTitle: c.refTitle}
	if err := inner.cond(Cond{Field: tail, Op: cond.Op, Value: cond.Value}, sc, now); err != nil {
		return err
	}
	sub := "SELECT x.id FROM records x WHERE " + inner.base() + " AND " + strings.Join(inner.where, " AND ")
	if f.Type == TypeRef {
		c.add(c.col(f)+" IN ("+sub+")", cond.Neg, inner.args...)
		return nil
	}
	c.add("EXISTS (SELECT 1 FROM "+c.eachOf(f)+" j WHERE j.value IN ("+sub+"))", cond.Neg, inner.args...)
	return nil
}

// usable is a field a query may sort or group by.
func (c *compiled) usable(name string, sc Scope, what string) (*Field, error) {
	f := c.t.field(name)
	if f == nil {
		return nil, fieldErr(name, "%s records have no field %q to %s", c.t.Name, name, what)
	}
	if f.Masked && sc.App != "" {
		return nil, fieldErr(name, "%s is hidden until revealed, so it cannot be used to %s", name, what)
	}
	return f, nil
}

// sortBy sets the order.
func (c *compiled) sortBy(value string, sc Scope) error {
	desc := strings.HasPrefix(value, "-")
	name := strings.TrimPrefix(value, "-")
	if col, ok := pseudo[name]; ok {
		c.sort = sortSpec{field: name, desc: desc, expr: col}
		return nil
	}
	f, err := c.usable(name, sc, "sort by")
	if err != nil {
		return err
	}
	spec := sortSpec{field: f.Name, desc: desc, expr: c.col(f)}
	if f.Type == TypeEnum {
		// An enum sorts in its declared order (Lead before Won), not by
		// spelling; a value it no longer has sorts with the empty ones.
		var b strings.Builder
		b.WriteString("CASE " + spec.expr)
		for i, v := range f.Values {
			b.WriteString(" WHEN ? THEN " + strconv.Itoa(i))
			spec.args = append(spec.args, v)
		}
		b.WriteString(" END")
		spec.expr = b.String()
	}
	c.sort = spec
	return nil
}

// flag compiles is:field and has:field.
func (c *compiled) flag(kind, name string, neg bool, sc Scope) error {
	f := c.t.field(name)
	if f == nil {
		return fieldErr(name, "%s records have no field %q", c.t.Name, name)
	}
	if f.Masked && sc.App != "" && kind == "is" {
		return fieldErr(name, "%s is hidden until revealed, so it cannot be searched", name)
	}
	col := c.col(f)
	var cond string
	switch kind {
	case "is":
		if f.Type != TypeBool {
			return fieldErr(name, "is: takes a true-or-false field; %s is %s", name, f.Type)
		}
		cond = col + " = 1"
	default:
		cond = col + " IS NOT NULL"
	}
	c.add(cond, neg)
	return nil
}

// add adds a condition, negated when asked. A negated condition is true for a
// record with no value: "-stage:Lost" keeps the deals with no stage.
func (c *compiled) add(cond string, neg bool, args ...any) {
	if neg {
		cond = "NOT COALESCE((" + cond + "), 0)"
	}
	c.where = append(c.where, "("+cond+")")
	c.args = append(c.args, args...)
}

// words compiles free words: each must appear in the title or a text field.
func (c *compiled) words(text string, neg bool, sc Scope) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	var cols []string
	title := c.t.titleField()
	for _, name := range c.t.Order {
		f := c.t.field(name)
		if f == nil || f.Masked {
			continue
		}
		if name == title || f.Type == TypeText || f.Type == TypeLongtext || f.Type == TypeEmail {
			cols = append(cols, c.col(f))
		}
	}
	if len(cols) == 0 {
		// A type with nothing to search finds nothing for a word.
		c.add("0", neg)
		return nil
	}
	pattern := "%" + likeEscape(text) + "%"
	parts := make([]string, len(cols))
	args := make([]any, len(cols))
	for i, col := range cols {
		parts[i] = "COALESCE(" + col + ", '') LIKE ? ESCAPE '\\'"
		args[i] = pattern
	}
	c.add(strings.Join(parts, " OR "), neg, args...)
	return nil
}

// likeEscape escapes LIKE's own wildcards so a value is matched as typed.
func likeEscape(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// wildcard is a typed value with * as LIKE's %.
func wildcard(s string) string {
	return strings.ReplaceAll(likeEscape(s), "*", "%")
}

// compare compiles field-op-value.
func (c *compiled) compare(f *Field, col, op, value string, neg bool, now time.Time) error {
	if op == "=" {
		op = ":"
	}
	if op == ":" {
		return c.match(f, col, value, neg, now)
	}
	sqlOp := op
	var v any
	switch f.Type {
	case TypeNumber, TypeMoney:
		n, err := number(unquote(value))
		if err != nil {
			return fieldErr(f.Name, "%s%s needs a number", f.Name, op)
		}
		v = n
	case TypeDate, TypeDatetime:
		at, err := queryTime(unquote(value), now)
		if err != nil {
			return fieldErr(f.Name, "%s%s needs a date like 2026-12-31 or -14d", f.Name, op)
		}
		if f.Type == TypeDate {
			v = at.Local().Format(dateLayout)
		} else {
			v = at.UTC().Format(stamp)
		}
	case TypeText, TypeEmail, TypePhone, TypeURL, TypePerson, TypeEnum:
		v = unquote(value)
	default:
		return fieldErr(f.Name, "%s cannot be compared with %s", f.Name, op)
	}
	c.add(col+" "+sqlOp+" ?", neg, v)
	return nil
}

// match compiles field:value,value: any of the values.
func (c *compiled) match(f *Field, col, value string, neg bool, now time.Time) error {
	values := splitValues(value)
	textual := f.Type != TypeNumber && f.Type != TypeMoney && f.Type != TypeDate && f.Type != TypeDatetime
	// stage:-Lost is the same as -stage:Lost on a field whose values cannot
	// begin with a minus sign.
	if textual && len(values) > 0 && strings.HasPrefix(values[0], "-") {
		for i, v := range values {
			if !strings.HasPrefix(v, "-") {
				return fieldErr(f.Name, "%s mixes values with and without -", f.Name)
			}
			values[i] = v[1:]
		}
		neg = !neg
	}
	var parts []string
	var args []any
	for _, raw := range values {
		v := strings.TrimSpace(raw)
		if v == "" {
			continue
		}
		part, partArgs, err := c.matchOne(f, col, v, now)
		if err != nil {
			return err
		}
		parts = append(parts, part)
		args = append(args, partArgs...)
	}
	if len(parts) == 0 {
		return nil
	}
	c.add(strings.Join(parts, " OR "), neg, args...)
	return nil
}

// matchOne is the condition for one value of field:value.
func (c *compiled) matchOne(f *Field, col, v string, now time.Time) (string, []any, error) {
	switch f.Type {
	case TypeNumber, TypeMoney:
		n, err := number(v)
		if err != nil {
			return "", nil, fieldErr(f.Name, "%s: needs a number", f.Name)
		}
		return col + " = ?", []any{n}, nil
	case TypeBool:
		b, err := f.coerce(v, coercion{lenient: true})
		if err != nil {
			return "", nil, fieldErr(f.Name, "%s: is true or false", f.Name)
		}
		if b == true {
			return col + " = 1", nil, nil
		}
		return "COALESCE(" + col + ", 0) = 0", nil, nil
	case TypeDate:
		at, err := queryTime(v, now)
		if err != nil {
			return "", nil, fieldErr(f.Name, "%s: needs a date like 2026-12-31", f.Name)
		}
		return col + " = ?", []any{at.Local().Format(dateLayout)}, nil
	case TypeDatetime:
		at, err := queryTime(v, now)
		if err != nil {
			return "", nil, fieldErr(f.Name, "%s: needs a date like 2026-12-31", f.Name)
		}
		if len(v) == len(dateLayout) || v == "today" {
			// A day matches every time within it, where the person is.
			y, m, d := at.Local().Date()
			start := time.Date(y, m, d, 0, 0, 0, 0, time.Local)
			return col + " >= ? AND " + col + " < ?", []any{start.UTC().Format(stamp), start.AddDate(0, 0, 1).UTC().Format(stamp)}, nil
		}
		return col + " = ?", []any{at.UTC().Format(stamp)}, nil
	case TypeEnum:
		canon, err := f.enumValue(v)
		if err != nil {
			return "", nil, fieldErr(f.Name, "%s %v", f.Name, err)
		}
		return col + " = ?", []any{canon}, nil
	case TypeEnums:
		canon, err := f.enumValue(v)
		if err != nil {
			return "", nil, fieldErr(f.Name, "%s %v", f.Name, err)
		}
		return "EXISTS (SELECT 1 FROM " + c.eachOf(f) + " j WHERE j.value = ?)", []any{canon}, nil
	case TypePerson:
		if v == "$me" || v == Me {
			v = c.sch.person
		}
		if strings.Contains(v, "*") {
			return col + " LIKE ? ESCAPE '\\'", []any{wildcard(v)}, nil
		}
		return col + " = ?", []any{v}, nil
	case TypeRef, TypeRefs:
		return c.refMatch(f, col, v)
	}
	// text, longtext, email, phone, url: case-insensitive, * a wildcard.
	return col + " LIKE ? ESCAPE '\\'", []any{wildcard(v)}, nil
}

// refMatch matches a ref (or one of a refs) by the id it holds or by the
// title of the record it points to.
func (c *compiled) refMatch(f *Field, col, v string) (string, []any, error) {
	titles := "SELECT t.id FROM records t WHERE t.type_id = ? AND t.deleted_at IS NULL AND "
	var args []any
	title := c.refTitle(f.To)
	var byTitle string
	target := c.sch.typeNamed(f.To)
	if title != "" && target != nil {
		byTitle = titles + "COALESCE(" + jsonPath("t.data_json", target.Fields[title].ID) + ", '') LIKE ? ESCAPE '\\'"
	}
	pattern := wildcard(v)
	if f.Type == TypeRef {
		cond := col + " = ?"
		args = append(args, v)
		if byTitle != "" {
			cond += " OR " + col + " IN (" + byTitle + ")"
			args = append(args, target.ID, pattern)
		}
		return cond, args, nil
	}
	each := "SELECT 1 FROM " + c.eachOf(f) + " j WHERE j.value = ?"
	args = append(args, v)
	if byTitle != "" {
		each += " OR j.value IN (" + byTitle + ")"
		args = append(args, target.ID, pattern)
	}
	return "EXISTS (" + each + ")", args, nil
}

// queryTime reads a value a query compares a date with: a date, a time,
// today and now, or a span from now (-14d, +7d, -2w, -6h, -3mo, -1y).
func queryTime(s string, now time.Time) (time.Time, error) {
	s = strings.TrimSpace(s)
	if len(s) >= 3 && (s[0] == '-' || s[0] == '+') {
		body := s[1:]
		months := strings.HasSuffix(body, "mo")
		if months {
			body = strings.TrimSuffix(body, "mo")
			n, err := strconv.Atoi(body)
			if err != nil {
				return time.Time{}, err
			}
			if s[0] == '-' {
				n = -n
			}
			return now.AddDate(0, n, 0), nil
		}
		span, err := parseSpan(body)
		if err != nil {
			return time.Time{}, err
		}
		if s[0] == '-' {
			span = -span
		}
		return now.Add(span), nil
	}
	return readTime(s, now)
}

// cursor is where a page ended: the sort it belongs to, the last row's sort
// value (or that it had none) and its id.
type cursor struct {
	Sort string `json:"s"`
	Null bool   `json:"n,omitempty"`
	Key  any    `json:"k,omitempty"`
	ID   string `json:"id"`
}

func (cur cursor) encode() string {
	raw, _ := json.Marshal(cur)
	return base64.RawURLEncoding.EncodeToString(raw)
}

func decodeCursor(s string) (*cursor, error) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	var cur cursor
	if err != nil || json.Unmarshal(raw, &cur) != nil || !validID(cur.ID) {
		return nil, errorf(CodeInvalid, "the cursor is not one this store gave")
	}
	switch cur.Key.(type) {
	case nil, float64, string:
	default:
		return nil, errorf(CodeInvalid, "the cursor is not one this store gave")
	}
	return &cur, nil
}

// base is the conditions every query of t starts with. The type is spliced
// as a literal, not bound, because the partial indexes are made with it as
// a literal and SQLite uses a partial index only for a matching literal.
func (c *compiled) base() string {
	return c.alias + ".type_id = '" + mustID(c.t.ID) + "' AND " + c.alias + ".deleted_at IS NULL"
}

// filter is the WHERE clause and its parameters.
func (c *compiled) filter() (string, []any) {
	parts := append([]string{c.base()}, c.where...)
	return strings.Join(parts, " AND "), append([]any(nil), c.args...)
}

// Page is one page of a query: its rows, the cursor of the next page (empty
// at the end) and how many rows match in all.
type Page struct {
	Rows  []Row
	Next  string
	Total int
}

// Query answers one page of the records of typ matching query.
func (s *Store) Query(sc Scope, typ, query, after string, limit int) (*Page, error) {
	sch := s.schema.Load()
	t, err := sch.allow(sc, typ, false)
	if err != nil {
		return nil, err
	}
	c, err := compileQuery(sch, t, sc, query, s.now())
	if err != nil {
		return nil, err
	}
	return s.page(sch, t, sc, c, after, limit)
}

// recordColumnsR is [recordColumns] read through the alias r.
const recordColumnsR = `r.id, r.type_id, r.rev, r.data_json, r.meta_json, r.created_at, r.updated_at, r.deleted_at`

// normalizeKey is a sort value as a cursor carries it.
func normalizeKey(k any) any {
	switch x := k.(type) {
	case int64:
		return float64(x)
	case []byte:
		return string(x)
	}
	return k
}

// refTitles answers the title field of a type, for matching refs by name;
// none when the scope may not read that type, so a ref query never tells an
// app whether a record it cannot read has some name.
func refTitles(sch *schema, sc Scope) func(string) string {
	return func(typ string) string {
		if _, err := sch.allow(sc, typ, false); err != nil {
			return ""
		}
		if t := sch.typeNamed(typ); t != nil {
			return t.titleField()
		}
		return ""
	}
}

// Group is one group of a count: the value (nil for records with none) and
// how many records have it.
type Group struct {
	Key   any
	Count int
}

// Count counts the records of typ matching query, grouped by a field: the
// counts a StateMeter draws. An enum's groups come in its declared order,
// zeros included, so the meter does not reshuffle as counts move.
func (s *Store) Count(sc Scope, typ, query, group string) ([]Group, int, error) {
	sch := s.schema.Load()
	t, err := sch.allow(sc, typ, false)
	if err != nil {
		return nil, 0, err
	}
	c, err := compileQuery(sch, t, sc, query, s.now())
	if err != nil {
		return nil, 0, err
	}
	if group == "" {
		group = c.group
	}
	where, args := c.filter()
	var total int
	if err := s.rdb.QueryRow(`SELECT count(*) FROM records r WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	if group == "" {
		return []Group{}, total, nil
	}
	f, err := c.usable(group, sc, "group by")
	if err != nil {
		return nil, 0, err
	}
	var stmt string
	if f.Type == TypeEnums || f.Type == TypeRefs {
		stmt = `SELECT j.value, count(*) FROM records r, ` + c.eachOf(f) + ` j WHERE ` + where + ` GROUP BY j.value`
	} else {
		col := c.col(f)
		stmt = `SELECT ` + col + `, count(*) FROM records r WHERE ` + where + ` GROUP BY ` + col
	}
	rows, err := s.rdb.Query(stmt, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	counts := map[string]int{}
	keys := map[string]any{}
	for rows.Next() {
		var k any
		var n int
		if err := rows.Scan(&k, &n); err != nil {
			return nil, 0, err
		}
		k = normalizeKey(k)
		if f.Type == TypeBool && k != nil {
			k = k == float64(1) || k == int64(1)
		}
		id := canonicalJSON(k)
		counts[id] += n
		keys[id] = k
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	var out []Group
	if f.Type == TypeEnum || f.Type == TypeEnums {
		for _, v := range f.Values {
			id := canonicalJSON(v)
			out = append(out, Group{Key: v, Count: counts[id]})
			delete(counts, id)
		}
	}
	rest := make([]Group, 0, len(counts))
	for id, n := range counts {
		rest = append(rest, Group{Key: keys[id], Count: n})
	}
	sort.Slice(rest, func(i, j int) bool {
		if (rest[i].Key == nil) != (rest[j].Key == nil) {
			return rest[j].Key == nil
		}
		if rest[i].Count != rest[j].Count {
			return rest[i].Count > rest[j].Count
		}
		return canonicalJSON(rest[i].Key) < canonicalJSON(rest[j].Key)
	})
	out = append(out, rest...)
	if len(out) > GroupsMax {
		out = out[:GroupsMax]
	}
	return out, total, nil
}

// String is a short account of a compiled query, for a test's failure.
func (c *compiled) String() string {
	where, args := c.filter()
	return fmt.Sprintf("%s %v sort %s", where, args, c.sort.signature())
}
