package schema

import (
	"fmt"
	"slices"
	"strings"
)

// Collection recipes bind the original typed fields. The RecordList renderer
// measures formatted values for widths and keeps money and dates whole.
func (c *checker) designCollection(s *Screen, n *Node, q *QueryIR, r Recipe) {
	fields, _ := c.typeFields(r.Record)
	record := c.app.Records[r.Record]
	life, managed := c.lifecycle(r.Record)
	state, date, money := life.Field, "", ""
	for _, name := range sortedKeys(fields) {
		switch fields[name].Type {
		case "date", "datetime":
			if date == "" {
				date = name
			}
		case "money":
			if money == "" {
				money = name
			}
		case "enum":
			if state == "" && (name == "state" || name == "status" || name == "stage") {
				state = name
			}
		}
	}
	if r.As == "calendar" {
		date = r.By
	}
	columns := slices.Clone(r.Fields)
	if len(columns) == 0 {
		columns = sortedKeys(fields)
	}
	role := func(name string) int {
		switch {
		case name == record.Title || name == "name" || name == "title":
			return 0
		case name == state:
			return 1
		case fields[name].Type == "money":
			return 2
		case fields[name].Type == "date" || fields[name].Type == "datetime":
			return 3
		case fields[name].Type == "person" || fields[name].Type == "people":
			return 4
		default:
			return 5
		}
	}
	slices.SortStableFunc(columns, func(a, b string) int { return role(a) - role(b) })
	if len(r.Fields) == 0 && len(columns) > 5 {
		columns = columns[:5]
	}
	if len(columns) > 0 {
		n.Props["columns"] = columns
		q.Select = slices.Clone(columns)
	}
	views := []string{r.As}
	if r.As == "table" {
		views[0] = "list"
	}
	if managed && r.As != "board" {
		views = append(views, "board")
		n.Props["board_by"] = life.Field
		n.On = map[string][]Action{"move": {{"lifecycle.transition": map[string]any{"type": r.Record, "id": "$row.id", "to": "$input.to"}}}}
	}
	if date != "" && r.As != "calendar" {
		views = append(views, "calendar")
		n.Props["viewOptions"] = map[string]any{"calendar": map[string]any{"start": date}}
	}
	for _, name := range []string{asString(n.Props["board_by"]), date} {
		if name != "" && !slices.Contains(q.Select, name) {
			q.Select = append(q.Select, name)
		}
	}
	n.Props["views"] = views
	n.Props["search"] = true
	label := record.Label
	if label == "" {
		label = strings.ReplaceAll(r.Record, "_", " ")
	}
	n.Props["empty"] = fmt.Sprintf("New %s records will appear here.", strings.ToLower(label))

	var tiles []*Node
	metric := func(key, title, op, field string, filters []QueryPredicate) {
		id := nodeID("recipe/" + s.Screen + "/stat/" + key)
		props := map[string]any{"kind": "metric", "metric": op, "title": title}
		if field != "" {
			props["y"] = field
		}
		tiles = append(tiles, &Node{ID: id, Part: "Chart", Label: title, Props: props, Bind: &Bind{Records: r.Record, View: id}})
		s.Views[id] = QueryIR{Type: r.Record, Filter: filters, Aggregate: []QueryAggregate{{Op: op, Field: field, As: "result"}}}
	}
	if money != "" {
		label := fields[money].Label
		if label == "" {
			label = strings.ReplaceAll(money, "_", " ")
		}
		metric(money, "Total "+label, "sum", money, nil)
	}
	if values := fields[state].Values; len(values) > 0 {
		value := values[0]
		if slices.Contains(values, life.Initial) {
			value = life.Initial
		}
		metric(state, value, "count", "", []QueryPredicate{{Field: state, Op: "eq", Value: value}})
	}
	if len(tiles) > 0 {
		s.Body = &Node{ID: nodeID("recipe/" + s.Screen + "/layout"), Part: "Stack", Children: append(tiles, n)}
	}
}
