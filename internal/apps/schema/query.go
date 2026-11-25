package schema

import "fmt"

// QueryIR is the declaration a screen gives the engine. The desk addresses it
// by view ID and sends arguments, so it cannot substitute a different query.
type QueryIR struct {
	Type      string            `json:"type"`
	Select    []string          `json:"select,omitempty"`
	Filter    []QueryPredicate  `json:"filter,omitempty"`
	Relate    *TraceQuery       `json:"relate,omitempty"`
	Group     []string          `json:"group,omitempty"`
	Aggregate []QueryAggregate  `json:"aggregate,omitempty"`
	Window    *QueryWindow      `json:"window,omitempty"`
	Args      map[string]string `json:"args,omitempty"`
}

type QueryPredicate struct {
	Field string `json:"field"`
	Op    string `json:"op"`
	Value any    `json:"value"`
}

type QueryAggregate struct {
	Op    string `json:"op"`
	Field string `json:"field,omitempty"`
	As    string `json:"as"`
}

type QueryWindow struct {
	OrderBy []string `json:"order_by,omitempty"`
	Limit   int      `json:"limit,omitempty"`
}

func (c *checker) checkQuery(file, at string, q QueryIR) {
	q.Type = c.queryTypeName(q.Type)
	if !c.checkRecordAccess(file, at+"/type", q.Type, "r") {
		return
	}
	fields, _ := c.typeFields(q.Type)
	resolve := func(name string) string {
		if name != "" {
			for key, field := range fields {
				if field.ID == name {
					return key
				}
			}
		}
		return name
	}
	field := func(at, name, tag string) { c.checkTaggedField(file, at, q.Type, resolve(name), tag) }
	for i, name := range q.Select {
		field(fmt.Sprintf("%s/select/%d", at, i), name, "")
	}
	for i, name := range q.Group {
		field(fmt.Sprintf("%s/group/%d", at, i), name, "")
	}
	for i, p := range q.Filter {
		path := fmt.Sprintf("%s/filter/%d", at, i)
		field(path+"/field", p.Field, "")
		f, exists := fields[resolve(p.Field)]
		if exists && !contains(c.kit.Registries.Fields[f.Type].Operators, p.Op) {
			c.add(file, path+"/op", CodeRegistryEntry, "The field type %q does not support %q.", f.Type, p.Op)
		}
	}
	seen := map[string]bool{}
	for i, aggregate := range q.Aggregate {
		path := fmt.Sprintf("%s/aggregate/%d", at, i)
		if seen[aggregate.As] || fields[aggregate.As].Type != "" || systemFields[aggregate.As] {
			c.add(file, path+"/as", CodeDuplicateID, "Aggregate %q conflicts with an existing output or field.", aggregate.As)
		}
		seen[aggregate.As] = true
		tag := ""
		switch aggregate.Op {
		case "sum", "avg":
			tag = "numeric"
		case "count":
		case "min", "max":
		}
		field(path+"/field", aggregate.Field, tag)
	}
	if q.Window != nil {
		for i, name := range q.Window.OrderBy {
			field(fmt.Sprintf("%s/window/order_by/%d", at, i), name, "")
		}
	}
	if q.Relate != nil {
		c.checkTrace(file, at+"/relate", *q.Relate)
	}
	for _, arg := range sortedKeys(q.Args) {
		if _, ok := c.kit.Registries.Fields[q.Args[arg]]; !ok {
			c.add(file, at+"/args/"+arg, CodeRegistryEntry, "%q is not a registered argument type.", q.Args[arg])
		}
	}
}

func (c *checker) checkViewBinding(ctx actionCtx, at string, b *Bind) QueryIR {
	query, ok := ctx.views[b.View]
	if !ok || c.queryTypeName(query.Type) != c.queryTypeName(b.Records) {
		c.add(ctx.file, at+"/view", CodeBindNotAccepted, "Choose a declared view of %s records.", b.Records)
		return QueryIR{}
	}
	return query
}

func (c *checker) queryTypeName(name string) string {
	if name == "" || c.knownType(name) {
		return name
	}
	for key, entry := range c.app.Records {
		if entry.ID == name {
			return key
		}
	}
	for key, entry := range c.existing {
		if entry.ID == name {
			return key
		}
	}
	return name
}
