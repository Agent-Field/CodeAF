package spec

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestGoldenCRMDemoFields(t *testing.T) {
	b, err := Load(golden)
	if err != nil {
		t.Fatal(err)
	}
	for typ, record := range b.App.Records {
		t.Run(typ, func(t *testing.T) {
			for name, field := range record.Fields {
				if strings.TrimSpace(field.Label) == "" {
					t.Errorf("%s.%s needs a human label", typ, name)
				}
				if field.Type == FieldRef || field.Type == FieldRefs {
					if target := b.App.Records[field.To]; target == nil || target.Title != "name" {
						t.Errorf("%s.%s must display the referenced name", typ, name)
					}
				}
				for _, option := range field.Values {
					if option == "" || strings.Contains(option, "_") {
						t.Errorf("%s.%s has a raw option label %q", typ, name, option)
					}
				}
			}
		})
	}
	stage := b.App.Records["deal"].Fields["stage"]
	if string(stage.Default) != `"`+stage.Values[0]+`"` {
		t.Fatalf("stage default %s must be the first stage", stage.Default)
	}
}

func TestGoldenCRMDemoScreens(t *testing.T) {
	b, err := Load(golden)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name  string
		check func(*testing.T)
	}{
		{"new deal", func(t *testing.T) {
			button := crmNode(t, b.Screens["pipeline"], "New deal")
			var ask map[string]string
			var create struct {
				Type string            `json:"type"`
				Set  map[string]string `json:"set"`
			}
			action := button.On["click"][0]
			if err := json.Unmarshal(action["ask"], &ask); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(action[VerbRecordsCreate], &create); err != nil {
				t.Fatal(err)
			}
			want := map[string]string{"name": "text", "company": "ref", "amount": "money", "stage": "enum", "next_step": "text"}
			if create.Type != "deal" || !reflect.DeepEqual(ask, want) {
				t.Fatalf("create = %+v, ask = %v", create, ask)
			}
			for name := range want {
				if create.Set[name] != "$input."+name {
					t.Errorf("creation drops %s", name)
				}
			}
			for name, field := range b.App.Records["deal"].Fields {
				if field.Required && ask[name] == "" {
					t.Errorf("creation omits required %s", name)
				}
			}
		}},
		{"full width table", func(t *testing.T) {
			screen := b.Screens["pipeline"]
			if screen.Body.Part != "Stack" {
				t.Fatal("deals must fill the screen without a side panel")
			}
			list := crmNode(t, screen, "Deals")
			want := []any{"company", "name", "stage", "next_step", "amount", "last_touch"}
			if !reflect.DeepEqual(list.Props["columns"], want) {
				t.Fatalf("columns = %v", list.Props["columns"])
			}
			labels := []string{}
			for _, name := range want {
				labels = append(labels, b.App.Records["deal"].Fields[name.(string)].Label)
			}
			if strings.Join(labels, " · ") != "Company · Deal · Stage · Next step · Value · Touch" {
				t.Fatal(labels)
			}
		}},
		{"detail actions", func(t *testing.T) {
			screen := b.Screens["deal"]
			page := crmNode(t, screen, "Deals › {{record.name}}")
			if actions, ok := page.Props["actions"].([]any); ok && len(actions) > 0 {
				t.Fatal("detail commands duplicate explicit actions")
			}
			won := crmNode(t, screen, "Mark won…")
			if won.When != "$record.stage != 'Won'" {
				t.Fatal("Mark won must be hidden for won deals")
			}
			if crmNode(t, screen, "Draft follow-up").Props["kind"] != "primary" {
				t.Fatal("Draft follow-up must be the primary action")
			}
			if crmNode(t, screen, "Delete deal…").Props["kind"] != "destructive" {
				t.Fatal("Delete deal must be destructive")
			}
			crmNode(t, screen, "Back to deals")
		}},
		{"empty states", func(t *testing.T) {
			b.Screens["deal"].Walk(func(node *Node) {
				if node.Part == "ApprovalCard" || node.Part == "RunTimeline" {
					if empty := node.Slots["empty"]; empty == nil || empty.Props["text"] == "" {
						t.Errorf("%s needs empty-state text", node.Part)
					}
				}
			})
		}},
	} {
		t.Run(test.name, test.check)
	}
}

func crmNode(t *testing.T, screen *Screen, label string) *Node {
	t.Helper()
	var found *Node
	screen.Walk(func(node *Node) {
		if node.Label == label {
			if found != nil {
				t.Fatalf("duplicate %q", label)
			}
			found = node
		}
	})
	if found == nil {
		t.Fatalf("missing %q", label)
	}
	return found
}

func TestGoldenCRMDemoSample(t *testing.T) {
	b, err := Load(golden)
	if err != nil {
		t.Fatal(err)
	}
	_, data := GoldenCRM()
	type row struct {
		ID     string         `json:"id"`
		Fields map[string]any `json:"fields"`
	}
	var rows map[string][]row
	if err := json.Unmarshal(data, &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows["deal"]) < 8 || len(rows["deal"]) > 12 {
		t.Fatalf("demo has %d deals; want 8–12", len(rows["deal"]))
	}
	ids := map[string]map[string]bool{}
	for typ, records := range rows {
		ids[typ] = map[string]bool{}
		for _, record := range records {
			if record.ID == "" || ids[typ][record.ID] {
				t.Fatalf("empty or duplicate %s id %q", typ, record.ID)
			}
			ids[typ][record.ID] = true
		}
	}
	stages := map[string]bool{}
	for typ, records := range rows {
		t.Run(typ, func(t *testing.T) {
			for _, record := range records {
				for name, field := range b.App.Records[typ].Fields {
					value := record.Fields[name]
					if field.Required && (value == nil || value == "") {
						t.Errorf("%s is missing %s", record.ID, name)
					}
					if field.Type == FieldRef && value != nil && !ids[field.To][value.(string)] {
						t.Errorf("%s.%s has a broken reference", record.ID, name)
					}
					if field.Type == FieldRefs && value != nil {
						for _, ref := range value.([]any) {
							if !ids[field.To][ref.(string)] {
								t.Errorf("%s.%s has a broken reference", record.ID, name)
							}
						}
					}
				}
				if typ == "deal" {
					stages[record.Fields["stage"].(string)] = true
					for _, name := range []string{"next_step", "last_touch", "close_on"} {
						if record.Fields[name] == nil || record.Fields[name] == "" {
							t.Errorf("%s needs %s for the demo", record.ID, name)
						}
					}
				}
			}
		})
	}
	for _, stage := range b.App.Records["deal"].Fields["stage"].Values {
		if !stages[stage] {
			t.Errorf("demo has no %s deal", stage)
		}
	}
}
