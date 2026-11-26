package schema

import (
	"fmt"
	"regexp"
	"strings"
)

// OutputTemplate is both a declared render Action and its document layout.
type OutputTemplate struct {
	Label   string         `json:"label"`
	Format  string         `json:"format"`
	Record  string         `json:"record,omitempty"`
	Query   *QueryIR       `json:"query,omitempty"`
	Body    string         `json:"body,omitempty"`
	Columns []OutputColumn `json:"columns,omitempty"`
	Table   string         `json:"table,omitempty"`
}

type OutputColumn struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// RenderAction refers to an app's declared template. Record is a binding in
// a screen or run and a resolved record ID at dispatch.
type RenderAction struct {
	Output string `json:"output"`
	Record string `json:"record,omitempty"`
	Inbox  bool   `json:"inbox,omitempty"`
}

const OutputRowsMax = 500

var outputField = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_-]*$`)

// MergeOutput substitutes flat, declared field names. Record values are
// treated as text and never parsed as template syntax.
func MergeOutput(text string, value func(string) (string, error)) (string, error) {
	var merged strings.Builder
	for {
		before, tail, found := strings.Cut(text, "{{")
		if strings.Contains(before, "}}") {
			return "", fmt.Errorf("unexpected closing merge field")
		}
		merged.WriteString(before)
		if !found {
			return merged.String(), nil
		}
		name, rest, closed := strings.Cut(tail, "}}")
		name = strings.TrimSpace(name)
		if !closed || !outputField.MatchString(name) {
			return "", fmt.Errorf("invalid merge field %q", name)
		}
		replacement, err := value(name)
		if err != nil {
			return "", err
		}
		merged.WriteString(replacement)
		text = rest
	}
}

func (t OutputTemplate) SourceType() string {
	if t.Query != nil {
		return t.Query.Type
	}
	return t.Record
}

func (c *checker) checkOutput(at string, t OutputTemplate) {
	if !c.checkRecordAccess(AppFile, at, c.queryTypeName(t.SourceType()), "r") {
		return
	}
	fields, _ := c.typeFields(c.queryTypeName(t.SourceType()))
	names := map[string]bool{}
	if q := t.Query; q != nil {
		query := *q
		if q.Window != nil {
			window := *q.Window
			window.OrderBy = nil
			for _, field := range q.Window.OrderBy {
				window.OrderBy = append(window.OrderBy, strings.TrimPrefix(field, "-"))
			}
			query.Window = &window
		}
		c.checkQuery(AppFile, at+"/query", query)
		if len(q.Aggregate) == 0 {
			if len(q.Select) == 0 {
				for name, field := range fields {
					names[name] = !field.Masked
				}
			}
			for _, name := range q.Select {
				names[name] = fields[name].Type != "" && !fields[name].Masked
			}
			for name := range systemFields {
				names[name] = true
			}
		} else {
			for _, name := range q.Group {
				names[name] = fields[name].Type != "" && !fields[name].Masked
			}
		}
		for i, p := range q.Filter {
			if p.Value == nil && p.Op != "has" {
				c.add(AppFile, fmt.Sprintf("%s/query/filter/%d/value", at, i), CodeRegistryEntry, "This filter needs a value.")
			}
		}
		for _, a := range q.Aggregate {
			names[a.As] = true
		}
	} else {
		for name, field := range fields {
			names[name] = !field.Masked
		}
		for name := range systemFields {
			names[name] = true
		}
	}
	check := func(path, text string) {
		_, err := MergeOutput(text, func(name string) (string, error) {
			if !names[name] {
				return "", fmt.Errorf("merge field %q must name a visible field in the output source", name)
			}
			return "", nil
		})
		if err != nil {
			c.add(AppFile, path, CodeFieldUnknown, "%s.", err)
		}
	}
	if t.Table != "" && (!names[t.Table] || fields[t.Table].Type != "longtext") {
		c.add(AppFile, at+"/table", CodeFieldUnknown, "A worksheet output needs a visible longtext field.")
	}
	check(at+"/body", t.Body)
	for i, column := range t.Columns {
		check(fmt.Sprintf("%s/columns/%d/value", at, i), column.Value)
	}
}

func (c *checker) checkRender(ctx actionCtx, at string, body map[string]any) {
	id := asString(body["output"])
	t, ok := c.app.Outputs[id]
	if !ok {
		c.add(ctx.file, at+"/output", CodeActionVerb, "There is no output named %q.", id)
		return
	}
	record := asString(body["record"])
	hasRecord := record != ""
	if hasRecord != (t.Record != "") {
		c.add(ctx.file, at+"/record", CodeActionVerb, "Match the record input to the declared output source.")
	}
	if hasRecord && t.Record != "" {
		typ := ctx.resolveType("", record)
		if typ == "" && strings.HasPrefix(record, "$") {
			typ = ctx.locals[strings.TrimSuffix(strings.TrimPrefix(record, "$"), ".id")]
		}
		if typ != "" && c.queryTypeName(typ) != c.queryTypeName(t.Record) {
			c.add(ctx.file, at+"/record", CodeActionVerb, "This output takes a %s record, but the binding names a %s record.", t.Record, typ)
		}
	}
	if inbox, _ := body["inbox"].(bool); inbox && t.Query == nil {
		c.add(ctx.file, at+"/inbox", CodeActionVerb, "An inbox digest needs a declared query.")
	}
}
