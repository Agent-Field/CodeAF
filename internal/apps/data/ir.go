package data

import (
	"fmt"
	"strings"
)

// A QUERY HAS ONE INTERMEDIATE FORM. The tokens a person types are parsed
// into it, a declared view's query is written in it, and only it is compiled
// to SQL:
//
//	select     the fields a row carries back, a related record's included
//	           (company.name), one link away
//	where      conditions: field:value, comparisons, is:/has:, free words, and
//	           a related record's fields (company.industry:SaaS)
//	group      one or two fields to group by, a related record's included
//	aggregate  count, sum, avg, min and max per group, computed by SQLite, a
//	           money sum in whole cents so a ledger adds up exactly
//
// Policy is applied once, here: a type the scope may not read is refused
// wherever the IR reaches it, and a masked field is neither searched,
// grouped nor summed for an app and reads as the mask when selected.

// Cond is one condition: Field (a field, or ref.field) Op Value, or, with no
// Field, free words in Value. Op is one of : > >= < <= is has; a value of :
// may list several (stage:Lead,Won) and may begin with - (stage:-Lost).
type Cond struct {
	Field string
	Op    string
	Value string
	Neg   bool

	// raw is the token a condition was parsed from: a name that turns out
	// not to be a field is searched as the words typed.
	raw string
}

// Agg is one aggregate: Fn (count, sum, avg, min, max) of Field (none for
// count), answered under As.
type Agg struct {
	Fn    string
	Field string
	As    string
}

// QueryIR is a query in its one intermediate form.
type QueryIR struct {
	Type      string
	Where     []Cond
	Sort      string
	Group     []string
	Select    []string
	Aggregate []Agg
}

// ParseQuery parses a query's tokens into the IR (no type yet: the type is
// what the IR is compiled against).
func ParseQuery(text string) (*QueryIR, error) {
	if len(text) > queryMax {
		return nil, errorf(CodeInvalid, "the query is longer than %d characters", queryMax)
	}
	tokens, err := tokenize(text)
	if err != nil {
		return nil, err
	}
	if len(tokens) > tokensMax {
		return nil, errorf(CodeInvalid, "the query has more than %d parts", tokensMax)
	}
	ir := &QueryIR{}
	for _, tok := range tokens {
		parseToken(tok, ir)
	}
	return ir, nil
}

// The bounds of the IR beyond a query's.
const (
	selectMax    = 64
	groupByMax   = 2
	aggregateMax = 16
)

// QueryRows answers one page of the IR's rows. With Select set, a row carries
// only those fields (and its own id, type, _rev and _meta), a related one
// under its dotted name.
func (s *Store) QueryRows(sc Scope, ir *QueryIR, after string, limit int) (*Page, error) {
	sch := s.schema.Load()
	t, err := sch.allow(sc, ir.Type, false)
	if err != nil {
		return nil, err
	}
	if len(ir.Select) > selectMax {
		return nil, errorf(CodeInvalid, "a query selects at most %d fields", selectMax)
	}
	c, err := compileIR(sch, t, sc, ir, s.now())
	if err != nil {
		return nil, err
	}
	sel, err := s.selection(sch, t, sc, ir.Select)
	if err != nil {
		return nil, err
	}
	page, err := s.page(sch, t, sc, c, after, limit)
	if err != nil || sel == nil {
		return page, err
	}
	return page, sel.project(s, sch, sc, page.Rows)
}

// selection is a query's select list, checked: its own fields, and per link
// the fields of the related record it carries back.
type selection struct {
	own     []string
	related map[string][]string // link field → target fields
	links   []string
}

func (s *Store) selection(sch *schema, t *Type, sc Scope, list []string) (*selection, error) {
	if len(list) == 0 {
		return nil, nil
	}
	sel := &selection{related: map[string][]string{}}
	for _, name := range list {
		head, tail, dotted := strings.Cut(name, ".")
		if _, ok := pseudo[name]; ok {
			continue
		}
		f := t.field(head)
		if f == nil {
			return nil, fieldErr(name, "%s records have no field %q", t.Name, head)
		}
		if !dotted {
			sel.own = append(sel.own, f.Name)
			continue
		}
		if f.Type != TypeRef && f.Type != TypeRefs {
			return nil, fieldErr(name, "%s is not a link to another record", head)
		}
		target, err := sch.allow(sc, f.To, false)
		if err != nil {
			return nil, err
		}
		g := target.field(tail)
		if g == nil {
			return nil, fieldErr(name, "%s records have no field %q", target.Name, tail)
		}
		if _, seen := sel.related[f.Name]; !seen {
			sel.links = append(sel.links, f.Name)
		}
		sel.related[f.Name] = append(sel.related[f.Name], g.Name)
	}
	return sel, nil
}

// project cuts rows down to the selection and adds each related record's
// fields, read in one query per link.
func (sel *selection) project(s *Store, sch *schema, sc Scope, rows []Row) error {
	t := sch.types[firstType(rows)]
	keep := map[string]bool{"id": true, "type": true, "created_at": true, "updated_at": true, "_rev": true, "_meta": true}
	for _, name := range sel.own {
		keep[name] = true
	}
	for _, link := range sel.links {
		if t == nil {
			break
		}
		f := t.Fields[link]
		target := sch.typeNamed(f.To)
		var ids []string
		for _, row := range rows {
			switch v := row[link].(type) {
			case string:
				ids = append(ids, v)
			case []any:
				for _, x := range v {
					if id, ok := x.(string); ok {
						ids = append(ids, id)
					}
				}
			}
		}
		related := map[string]Row{}
		if len(ids) > 0 {
			err := eachRow(s.rdb, `SELECT `+recordColumns+` FROM records WHERE type_id = ? AND deleted_at IS NULL AND id IN (SELECT value FROM json_each(?))`,
				[]any{target.ID, canonicalJSON(ids)}, func(scan func(...any) error) error {
					r, err := scanRecord(sch, scan)
					if err != nil {
						return err
					}
					related[r.ID] = s.row(target, r, sc, false)
					return nil
				})
			if err != nil {
				return err
			}
		}
		for _, row := range rows {
			for _, name := range sel.related[link] {
				key := link + "." + name
				switch v := row[link].(type) {
				case string:
					if rr := related[v]; rr != nil && rr[name] != nil {
						row[key] = rr[name]
					}
				case []any:
					var vals []any
					for _, x := range v {
						if id, ok := x.(string); ok && related[id] != nil && related[id][name] != nil {
							vals = append(vals, related[id][name])
						}
					}
					if vals != nil {
						row[key] = vals
					}
				}
			}
		}
	}
	for _, row := range rows {
		for k := range row {
			if !keep[k] && !strings.Contains(k, ".") {
				delete(row, k)
			}
		}
	}
	return nil
}

func firstType(rows []Row) string {
	if len(rows) == 0 {
		return ""
	}
	typ, _ := rows[0]["type"].(string)
	return typ
}

// AggGroup is one group of an aggregate: the values it is grouped by and
// each aggregate's value.
type AggGroup struct {
	Key    []any
	Values map[string]any
}

// AggResult is an aggregate's answer: its groups (one, keyless, with no group
// by), in order, and how many records matched in all.
type AggResult struct {
	Group  []string
	Groups []AggGroup
	Total  int
}

// Aggregate answers the IR's aggregates over the records matching it, per
// group, computed by SQLite. Groups come in order (an enum in its declared
// order, values with none last), at most [GroupsMax].
func (s *Store) Aggregate(sc Scope, ir *QueryIR) (*AggResult, error) {
	sch := s.schema.Load()
	t, err := sch.allow(sc, ir.Type, false)
	if err != nil {
		return nil, err
	}
	if len(ir.Group) > groupByMax {
		return nil, errorf(CodeInvalid, "an aggregate groups by at most %d fields", groupByMax)
	}
	if len(ir.Aggregate) == 0 || len(ir.Aggregate) > aggregateMax {
		return nil, errorf(CodeInvalid, "an aggregate asks for between 1 and %d values", aggregateMax)
	}
	noGroup := *ir
	noGroup.Group = nil
	c, err := compileIR(sch, t, sc, &noGroup, s.now())
	if err != nil {
		return nil, err
	}
	where, args := c.filter()

	var selects, groupCols, orders []string
	var groupArgs []any
	var groupFields []*Field
	for _, name := range ir.Group {
		col, f, err := c.groupColumn(sc, name)
		if err != nil {
			return nil, err
		}
		groupFields = append(groupFields, f)
		selects = append(selects, col)
		groupCols = append(groupCols, col)
		order := col
		if f.Type == TypeEnum {
			var b strings.Builder
			b.WriteString("CASE " + col)
			for i, v := range f.Values {
				b.WriteString(fmt.Sprintf(" WHEN ? THEN %d", i))
				groupArgs = append(groupArgs, v)
			}
			b.WriteString(" END")
			order = b.String()
		}
		orders = append(orders, "("+col+") IS NULL", order, col)
	}
	names := map[string]bool{}
	for i, a := range ir.Aggregate {
		expr, err := c.aggregate(sc, a)
		if err != nil {
			return nil, err
		}
		as := a.As
		if as == "" {
			as = a.Fn
			if a.Field != "" {
				as += "_" + strings.ReplaceAll(a.Field, ".", "_")
			}
		}
		if !identShape.MatchString(as) || names[as] {
			return nil, errorf(CodeInvalid, "aggregate %d needs a name of its own (as)", i+1)
		}
		names[as] = true
		ir.Aggregate[i].As = as
		selects = append(selects, expr)
	}
	stmt := `SELECT ` + strings.Join(selects, ", ") + ` FROM records r WHERE ` + where
	if len(groupCols) > 0 {
		stmt += ` GROUP BY ` + strings.Join(groupCols, ", ") + ` ORDER BY ` + strings.Join(orders, ", ")
	}
	stmt += fmt.Sprintf(` LIMIT %d`, GroupsMax)
	// The group expressions appear in SELECT, GROUP BY and ORDER BY, and the
	// enum order's values are bound where its CASE is.
	allArgs := append([]any(nil), args...)
	allArgs = append(allArgs, groupArgs...)
	out := &AggResult{Group: append([]string{}, ir.Group...), Groups: []AggGroup{}}
	err = eachRow(s.rdb, stmt, allArgs, func(scan func(...any) error) error {
		dest := make([]any, len(selects))
		ptrs := make([]any, len(selects))
		for i := range dest {
			ptrs[i] = &dest[i]
		}
		if err := scan(ptrs...); err != nil {
			return err
		}
		g := AggGroup{Key: []any{}, Values: map[string]any{}}
		for i, f := range groupFields {
			k := normalizeKey(dest[i])
			if f.Type == TypeBool && k != nil {
				k = k == float64(1) || k == int64(1)
			}
			g.Key = append(g.Key, k)
		}
		for i, a := range ir.Aggregate {
			v := normalizeKey(dest[len(groupFields)+i])
			if n, ok := v.(int64); ok {
				v = float64(n)
			}
			g.Values[a.As] = v
		}
		out.Groups = append(out.Groups, g)
		return nil
	})
	if err != nil {
		return nil, err
	}
	if err := s.rdb.QueryRow(`SELECT count(*) FROM records r WHERE `+where, args...).Scan(&out.Total); err != nil {
		return nil, err
	}
	return out, nil
}

// groupColumn is the SQL a group-by field reads: one of the row's fields, or
// a field of the record its link points to.
func (c *compiled) groupColumn(sc Scope, name string) (string, *Field, error) {
	head, tail, dotted := strings.Cut(name, ".")
	f, err := c.usable(head, sc, "group by")
	if err != nil {
		return "", nil, err
	}
	if !dotted {
		if f.Type == TypeEnums || f.Type == TypeRefs {
			return "", nil, fieldErr(name, "%s holds a list; count its values with data.count", name)
		}
		return c.col(f), f, nil
	}
	if f.Type != TypeRef {
		return "", nil, fieldErr(name, "%s is not a link to one record", head)
	}
	target, err := c.sch.allow(sc, f.To, false)
	if err != nil {
		return "", nil, err
	}
	g := target.field(tail)
	if g == nil {
		return "", nil, fieldErr(name, "%s records have no field %q", target.Name, tail)
	}
	if g.Masked && sc.App != "" {
		return "", nil, fieldErr(name, "%s is hidden until revealed, so it cannot be used to group by", name)
	}
	return "(SELECT " + jsonPath("x.data_json", g.ID) + " FROM records x WHERE x.id = " + c.col(f) + " AND x.deleted_at IS NULL)", g, nil
}

// aggregate is the SQL of one aggregate. A sum or average of money is taken
// in whole cents and answered rounded to the cent: 0.1 + 0.2 is 0.30.
func (c *compiled) aggregate(sc Scope, a Agg) (string, error) {
	switch a.Fn {
	case "count":
		if a.Field == "" {
			return "count(*)", nil
		}
		f, err := c.usable(a.Field, sc, "count")
		if err != nil {
			return "", err
		}
		return "count(" + c.col(f) + ")", nil
	case "sum", "avg", "min", "max":
	default:
		return "", errorf(CodeInvalid, "%q is not an aggregate; use count, sum, avg, min or max", a.Fn)
	}
	f, err := c.usable(a.Field, sc, a.Fn)
	if err != nil {
		return "", err
	}
	col := c.col(f)
	numeric := f.Type == TypeNumber || f.Type == TypeMoney
	dated := f.Type == TypeDate || f.Type == TypeDatetime
	if !numeric && !(dated && (a.Fn == "min" || a.Fn == "max")) {
		return "", fieldErr(f.Name, "%s of %s needs a number field", a.Fn, f.Name)
	}
	if f.Type == TypeMoney {
		cents := "CAST(round(" + col + " * 100) AS INTEGER)"
		switch a.Fn {
		case "sum":
			return "round(sum(" + cents + ") / 100.0, 2)", nil
		case "avg":
			return "round(avg(" + cents + ") / 100.0, 2)", nil
		}
	}
	return a.Fn + "(" + col + ")", nil
}

// page is one page of the records a compiled query matches, in its order,
// resuming after a cursor.
func (s *Store) page(sch *schema, t *Type, sc Scope, c *compiled, after string, limit int) (*Page, error) {
	if limit <= 0 {
		limit = PageDefault
	}
	if limit > PageMax {
		limit = PageMax
	}
	where, args := c.filter()
	var total int
	if err := s.rdb.QueryRow(`SELECT count(*) FROM records r WHERE `+where, args...).Scan(&total); err != nil {
		return nil, err
	}
	dir, cmp := "ASC", ">"
	if c.sort.desc {
		dir, cmp = "DESC", "<"
	}
	key := c.sort.expr
	pageWhere := where
	pageArgs := append([]any(nil), c.sort.args...)
	pageArgs = append(pageArgs, args...)
	if after != "" {
		cur, err := decodeCursor(after)
		if err != nil {
			return nil, err
		}
		if cur.Sort != c.sort.signature() {
			return nil, errorf(CodeInvalid, "the cursor belongs to another order; start again from the first page")
		}
		if cur.Null {
			pageWhere += " AND (" + key + " IS NULL AND r.id " + cmp + " ?)"
			pageArgs = append(pageArgs, c.sort.args...)
			pageArgs = append(pageArgs, cur.ID)
		} else {
			pageWhere += " AND ((" + key + ") IS NULL OR (" + key + ") " + cmp + " ? OR ((" + key + ") = ? AND r.id " + cmp + " ?))"
			pageArgs = append(pageArgs, c.sort.args...)
			pageArgs = append(pageArgs, c.sort.args...)
			pageArgs = append(pageArgs, cur.Key)
			pageArgs = append(pageArgs, c.sort.args...)
			pageArgs = append(pageArgs, cur.Key, cur.ID)
		}
	}
	// The order's parameters come after the WHERE's in the text, so they are
	// bound again at the end.
	stmt := `SELECT ` + recordColumnsR + `, ` + key + ` FROM records r WHERE ` + pageWhere +
		` ORDER BY (` + key + `) IS NULL, ` + key + ` ` + dir + `, r.id ` + dir + ` LIMIT ?`
	pageArgs = append(pageArgs, c.sort.args...)
	pageArgs = append(pageArgs, c.sort.args...)
	pageArgs = append(pageArgs, limit+1)
	rows, err := s.rdb.Query(stmt, pageArgs...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	page := &Page{Rows: []Row{}, Total: total}
	var last *record
	var lastKey any
	for rows.Next() {
		var k any
		r, err := scanRecord(sch, func(dest ...any) error { return rows.Scan(append(dest, &k)...) })
		if err != nil {
			return nil, err
		}
		if len(page.Rows) == limit {
			cur := cursor{Sort: c.sort.signature(), ID: last.ID, Key: normalizeKey(lastKey), Null: lastKey == nil}
			page.Next = cur.encode()
			break
		}
		page.Rows = append(page.Rows, s.row(t, r, sc, false))
		last, lastKey = r, k
	}
	return page, rows.Err()
}
