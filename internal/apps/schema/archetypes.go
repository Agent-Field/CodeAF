package schema

import (
	"fmt"
	"strings"
)

// ScreenSlots supplies the parts of the job that the compiler cannot infer.
// The archetype owns the resulting layout and the references between its parts.
type ScreenSlots struct {
	Agenda      *ScreenAgenda     `json:"agenda,omitempty"`
	Reason      string            `json:"reason,omitempty"`
	Summary     string            `json:"summary,omitempty"`
	State       string            `json:"state,omitempty"`
	Start       string            `json:"start,omitempty"`
	End         string            `json:"end,omitempty"`
	Message     string            `json:"message,omitempty"`
	Reply       string            `json:"reply,omitempty"`
	ReplyParams map[string]any    `json:"reply_params,omitempty"`
	Macros      []ReplyMacro      `json:"macros,omitempty"`
	Actions     []string          `json:"actions,omitempty"`
	Sort        string            `json:"sort,omitempty"`
	Filter      []QueryPredicate  `json:"filter,omitempty"`
	Metrics     []ScreenMetric    `json:"metrics,omitempty"`
	Visual      *ScreenVisual     `json:"visual,omitempty"`
	Changed     *ScreenCollection `json:"changed,omitempty"`
	Related     *ScreenCollection `json:"related,omitempty"`
}

type ReplyMacro struct {
	Label string `json:"label"`
	Text  string `json:"text"`
}

type ScreenAgenda struct {
	Record    string `json:"record"`
	Start     string `json:"start"`
	Attendees string `json:"attendees,omitempty"`
}

type ScreenCollection struct {
	Record string           `json:"record"`
	Title  string           `json:"title"`
	Fields []string         `json:"fields,omitempty"`
	Sort   string           `json:"sort,omitempty"`
	Filter []QueryPredicate `json:"filter,omitempty"`
}

type ScreenMetric struct {
	Format string           `json:"format,omitempty"`
	Target *float64         `json:"target,omitempty"`
	Limit  *float64         `json:"limit,omitempty"`
	Label  string           `json:"label"`
	Op     string           `json:"op"`
	Field  string           `json:"field,omitempty"`
	Where  []QueryPredicate `json:"where,omitempty"`
}

type ScreenVisual struct {
	Kind    string `json:"kind"`
	Title   string `json:"title"`
	Group   string `json:"group,omitempty"`
	Value   string `json:"value,omitempty"`
	Content string `json:"content,omitempty"`
	Field   string `json:"field,omitempty"`
}

func (c *checker) compileArchetype(r Recipe) (*Screen, error) {
	id := r.Screen
	if id == "" {
		id = r.ID
	}
	title := strings.TrimSpace(r.Title)
	if title == "" {
		title = strings.ReplaceAll(id, "-", " ")
	}
	fields, known := c.typeFields(r.Record)
	if !known {
		return nil, fmt.Errorf("choose a record type for the %s screen", r.As)
	}
	s := &Screen{Screen: id, Title: title, Views: map[string]QueryIR{}}
	var slots ScreenSlots
	if r.Slots != nil {
		slots = *r.Slots
	}
	node := func(id, part, label string, props map[string]any, children ...*Node) *Node {
		return &Node{ID: id, Part: part, Label: label, Props: props, Children: children}
	}
	stack := func(id string, children ...*Node) *Node {
		return node(id, "Stack", "", map[string]any{"gap": 2}, children...)
	}
	collection := func(key string, view ScreenCollection, mode string, selectOne bool) *Node {
		props := map[string]any{"select": "none"}
		if len(view.Fields) > 0 {
			props["columns"] = view.Fields
		}
		if selectOne {
			props["select"] = "one"
		}
		if mode != "" {
			props["views"] = []string{mode}
		}
		n := node(key, "RecordList", view.Title, props)
		n.Bind = &Bind{Records: view.Record, View: key}
		query := QueryIR{Type: view.Record, Filter: view.Filter}
		if view.Sort != "" {
			query.Window = &QueryWindow{OrderBy: []string{view.Sort}}
		}
		s.Views[key] = query
		return n
	}
	view := ScreenCollection{Record: r.Record, Title: title, Fields: r.Fields, Sort: slots.Sort, Filter: slots.Filter}
	state := slots.State
	if state == "" {
		if life, ok := c.lifecycle(r.Record); ok {
			state = life.Field
		}
	}
	if state == "" {
		for _, field := range []string{"state", "status", "stage"} {
			if len(fields[field].Values) != 0 {
				state = field
				break
			}
		}
	}
	if state != "" && len(fields[state].Values) == 0 {
		return nil, fmt.Errorf("choose an enum field for the state slot")
	}
	record := func(ref string) *Node {
		props := map[string]any{}
		if len(r.Fields) != 0 {
			props["fields"] = r.Fields
		}
		if len(slots.Actions) != 0 {
			props["actions"] = slots.Actions
		}
		detail := node("record", "RecordPage", "Record details", props)
		detail.Bind = &Bind{Record: ref, Type: r.Record}
		return detail
	}
	schedule := func(n *Node) error {
		start := slots.Start
		if start == "" {
			for _, key := range sortedKeys(fields) {
				if fields[key].Type == "date" || fields[key].Type == "datetime" {
					start = key
					break
				}
			}
		}
		date := func(key string) bool { return fields[key].Type == "date" || fields[key].Type == "datetime" }
		if !date(start) || slots.End != "" && !date(slots.End) {
			return fmt.Errorf("choose date or date-time fields for the schedule")
		}
		dates := map[string]any{"start": start}
		if slots.End != "" {
			dates["end"] = slots.End
		}
		n.Props["viewOptions"] = map[string]any{"timeline": dates}
		return nil
	}
	selection := "$selection.work.id"
	var visual *Node
	if slots.Visual != nil {
		var err error
		visual, err = c.screenVisual(s, r.Record, selection, *slots.Visual)
		if err != nil {
			return nil, err
		}
	}
	var pair *Node
	switch r.As {
	case "queue-detail", "board-detail", "inbox-thread", "brief", "agenda":
		mode := "list"
		switch r.As {
		case "board-detail":
			if state == "" {
				return nil, fmt.Errorf("choose a state field for the board")
			}
			mode = "board"
		case "agenda":
			mode = "timeline"
		}
		queue := collection("work", view, mode, true)
		if r.As == "brief" {
			queue.Label = "Needs you"
			reason := slots.Reason
			if reason == "" && fields["reason"].Type != "" {
				reason = "reason"
			}
			if reason != "" {
				if field := fields[reason]; field.Masked || (field.Type != "text" && field.Type != "longtext") {
					return nil, fmt.Errorf("choose a readable text field for the reason slot")
				}
				queue.Props["reason"] = reason
			}
		}
		if mode == "board" {
			queue.Props["board_by"] = state
		} else if mode == "timeline" {
			if err := schedule(queue); err != nil {
				return nil, err
			}
		} else if state != "" {
			queue.Props["meter"] = state
		}
		details := []*Node{record(selection)}
		if r.As == "inbox-thread" {
			field := slots.Message
			if field == "" {
				for _, key := range sortedKeys(fields) {
					if fields[key].Type == "longtext" {
						field = key
						break
					}
				}
			}
			if fields[field].Type != "longtext" && fields[field].Type != "text" {
				return nil, fmt.Errorf("choose a text field for the message slot")
			}
			thread := node("thread", "Thread", "Conversation", map[string]any{"messages": field, "compose": slots.Reply != ""})
			thread.Bind = &Bind{Record: selection, Type: r.Record}
			if len(slots.Macros) > 0 {
				if slots.Reply == "" {
					return nil, fmt.Errorf("choose a reply action to use reply macros")
				}
				thread.Props["macros"] = slots.Macros
			}
			if slots.Reply != "" {
				thread.On = map[string][]Action{"reply": {{"runs.start": slots.Reply, "params": slots.ReplyParams}}}
			}
			details = append(details, thread)
		}
		if visual != nil {
			details = append(details, visual)
		}
		panel := node("detail", "Panel", "Details", map[string]any{"title": "Details"}, details...)
		panel.Bind = &Bind{Selection: "work"}
		workspace := "queue"
		if mode == "board" {
			workspace = "board"
		}
		pair = node("workspace", "Row", "", map[string]any{"align": "stretch", "workspace": workspace}, queue, panel)
	}

	switch r.As {
	case "queue-detail", "board-detail", "inbox-thread", "agenda":
		s.Body = pair
	case "brief":
		summary := slots.Summary
		if summary == "" {
			summary = title
		}
		hero := node("summary", "Text", "", map[string]any{"text": summary, "size": "large", "tone": "primary"})
		var supporting []*Node
		agenda := slots.Agenda
		if agenda == nil && slots.Start != "" {
			agenda = &ScreenAgenda{Record: r.Record, Start: slots.Start}
		}
		if agenda != nil {
			agendaFields, _ := c.typeFields(agenda.Record)
			start := agendaFields[agenda.Start]
			if start.Masked || (start.Type != "date" && start.Type != "datetime") {
				return nil, fmt.Errorf("choose a readable date for the day agenda")
			}
			dates := map[string]any{"start": agenda.Start}
			if agenda.Attendees != "" {
				people := agendaFields[agenda.Attendees]
				if people.Type == "" || people.Masked {
					return nil, fmt.Errorf("choose a readable attendee field for the day agenda")
				}
				dates["attendees"] = agenda.Attendees
			}
			today := collection("agenda", ScreenCollection{Record: agenda.Record, Title: "Today’s agenda", Sort: agenda.Start}, "agenda", false)
			today.Props["search"] = false
			today.Props["viewOptions"] = map[string]any{"agenda": dates}
			supporting = append(supporting, today)
		}
		if changed := slots.Changed; changed != nil {
			recent := collection("changed", *changed, "list", false)
			recent.Props["search"] = false
			label := changed.Title
			if label == "" {
				label = "What changed"
			}
			supporting = append(supporting, node("changed-title", "Text", "", map[string]any{"text": label, "role": "section", "tone": "primary"}), recent)
		}
		needs := stack("needs-attention", node("needs-title", "Text", "", map[string]any{"text": "Needs you", "role": "section", "tone": "primary"}), pair)
		work := needs
		if len(supporting) != 0 {
			work = node("brief-work", "Row", "", map[string]any{"wrap": true, "gap": 2},
				needs, stack("around-the-queue", supporting...))
		}
		s.Body = stack("brief", hero, work)
	case "dashboard":
		if len(slots.Metrics) == 0 || visual == nil {
			return nil, fmt.Errorf("choose at least one metric and a visual for the dashboard")
		}
		var parts []*Node
		for i, metric := range slots.Metrics {
			if strings.TrimSpace(metric.Label) == "" {
				return nil, fmt.Errorf("give every dashboard metric a label")
			}
			key := fmt.Sprintf("metric-%d", i+1)
			props := map[string]any{"kind": "metric", "title": metric.Label}
			if metric.Format != "" {
				props["format"] = metric.Format
			}
			if metric.Target != nil {
				props["target"] = *metric.Target
			}
			if metric.Limit != nil {
				props["limit"] = *metric.Limit
			}
			n := node(key, "Chart", metric.Label, props)
			n.Bind = &Bind{Records: r.Record, View: key}
			s.Views[key] = QueryIR{Type: r.Record, Filter: metric.Where, Aggregate: []QueryAggregate{{Op: metric.Op, Field: metric.Field, As: "result"}}}
			parts = append(parts, n)
		}
		view.Title = "Exceptions"
		s.Body = stack("dashboard", node("measures", "Row", "", map[string]any{"wrap": true}, parts...), visual, collection("exceptions", view, "list", false))
	case "record":
		s.Params = []string{"id"}
		timeline := node("history", "Timeline", "History", nil)
		timeline.Bind = &Bind{Record: "$params.id", Type: r.Record}
		parts := []*Node{record("$params.id"), timeline}
		if visual != nil {
			if visual.Bind != nil && visual.Bind.File == selection {
				visual.Bind.File = "$params.id"
			}
			parts = append(parts, visual)
		}
		if related := slots.Related; related != nil {
			parts = append(parts, collection("related", *related, "list", false))
		}
		s.Body = stack("details", parts...)
	case "canvas-visual":
		if visual == nil || visual.Bind != nil && visual.Bind.File != "" {
			return nil, fmt.Errorf("choose a visual that can open without a selected record")
		}
		s.Body = node("canvas-workspace", "Row", "", map[string]any{"align": "stretch"}, visual, collection("work", view, "list", false))
	default:
		return nil, fmt.Errorf("unknown screen archetype %q", r.As)
	}
	return s, nil
}

func (c *checker) screenVisual(s *Screen, record, selected string, v ScreenVisual) (*Node, error) {
	n := &Node{ID: "visual", Label: v.Title, Props: map[string]any{}}
	if n.Label == "" {
		return nil, fmt.Errorf("give the signature visual a title")
	}
	switch v.Kind {
	case "line", "bar", "area":
		n.Part = "Chart"
		n.Bind = &Bind{Records: record, View: n.ID}
		n.Props = map[string]any{"kind": v.Kind, "x": v.Group, "title": v.Title}
		if v.Value != "" {
			n.Props["y"] = v.Value
		}
		s.Views[n.ID] = QueryIR{Type: record}
	case "timeline", "map", "graph", "canvas":
		parts := map[string]string{"timeline": "Timeline", "map": "Map", "graph": "Graph", "canvas": "Canvas"}
		n.Part = parts[v.Kind]
		n.Bind = &Bind{ContentPart: v.Content}
	case "file":
		n.Part = "FileViewer"
		n.Bind = &Bind{File: selected, Type: record, Field: v.Field}
	default:
		return nil, fmt.Errorf("unknown signature visual %q", v.Kind)
	}
	return n, nil
}
