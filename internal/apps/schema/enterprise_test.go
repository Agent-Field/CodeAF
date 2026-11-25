package schema

import "testing"

func TestGalleryEnterpriseDeclarationsResolveInPersonal(t *testing.T) {
	prepare := func(t *testing.T) Definition {
		d := load(t, "testdata/dispatch")
		jsonEdit(AppFile, func(app map[string]any) {
			fields := at(app, "records", "delivery", "fields")
			fields["tracking"] = map[string]any{"type": "text", "unique": true, "sequence": map[string]any{"prefix": "DLV-", "padding": 4}}
			fields["truck"] = map[string]any{"type": "ref", "to": "truck"}
			fields["starts"] = map[string]any{"type": "datetime"}
			fields["ends"] = map[string]any{"type": "datetime"}
			fields["driver"] = map[string]any{"type": "person"}
			fields["reversal_of"] = map[string]any{"type": "ref", "to": "delivery"}
			at(app, "records", "delivery")["ledger"] = map[string]any{"reversal": "reversal_of"}
			at(app, "records")["truck"] = map[string]any{"title": "name", "fields": map[string]any{"name": map[string]any{"type": "text"}}}
			at(app, "capabilities", "records")["truck"] = "r"
			app["process"] = map[string]any{
				"forms": map[string]any{"request": map[string]any{"record": "delivery", "title": "Request a delivery",
					"steps": []map[string]any{{"id": "details", "label": "Details", "fields": []map[string]any{{"field": "title"}}}}}},
				"work_items": map[string]any{"assignment": map[string]any{"title": "Assign driver", "subject": map[string]any{"type": "delivery", "id": "$record.id"},
					"assignee": "me", "form": "request"}},
			}
			app["roles"] = map[string]any{
				"driver":    map[string]any{"kind": "custom", "label": "Driver", "screens": []string{"map"}},
				"requester": map[string]any{"kind": "requester", "label": "Requester", "screens": []string{"schedule"}},
			}
			app["audiences"] = map[string]any{"dispatchers": map[string]any{"label": "Dispatchers", "roles": []string{"driver"}, "screens": []string{"map"}}}
			app["channels"] = map[string]any{"book": map[string]any{"kind": "form-link", "record": "delivery", "form": "request", "audience": "dispatchers"},
				"file": map[string]any{"kind": "file-drop", "record": "delivery", "form": "request", "accept": []string{"text/csv"}}}
			app["rules"] = map[string]any{
				"owner":    map[string]any{"kind": "assignment", "record": "delivery", "strategy": "field", "field": "driver", "workItem": "assignment"},
				"dispatch": map[string]any{"kind": "approval-routing", "record": "delivery", "order": "serial", "roles": []string{"driver"}},
			}
			app["resources"] = map[string]any{"trucks": map[string]any{"record": "truck", "label": "Trucks",
				"booking": map[string]any{"type": "delivery", "resource": "truck", "start": "starts", "end": "ends", "overlap": "reject"}}}
		})(t, d.Files)
		jsonEdit(PlanFile, func(v map[string]any) {
			v["data"] = append(v["data"].([]any), map[string]any{"id": "truck", "line": "Available trucks"})
		})(t, d.Files)
		return d
	}
	if r := Validate(prepare(t), nil); !r.OK() {
		t.Fatal(r)
	}
	cases := []struct {
		name, code string
		change     func(map[string]any)
	}{
		{"unknown role screen", CodeOpenUnknown, func(v map[string]any) { at(v, "roles", "driver")["screens"] = []string{"missing"} }},
		{"audience widens role", CodePolicy, func(v map[string]any) { at(v, "audiences", "dispatchers")["screens"] = []string{"schedule"} }},
		{"channel form missing", CodePolicy, func(v map[string]any) { at(v, "channels", "book")["form"] = "missing" }},
		{"agent audience role missing", CodePolicy, func(v map[string]any) { at(v, "rules", "dispatch")["roles"] = []string{"missing"} }},
		{"date range same endpoint", CodePolicy, func(v map[string]any) { at(v, "resources", "trucks", "booking")["end"] = "starts" }},
		{"booking must reference resource", CodePolicy, func(v map[string]any) { at(v, "resources", "trucks", "booking")["resource"] = "driver" }},
		{"sequence requires unique", CodePolicy, func(v map[string]any) { at(v, "records", "delivery", "fields", "tracking")["unique"] = false }},
		{"ledger needs reversing entry", CodePolicy, func(v map[string]any) { at(v, "records", "delivery", "ledger")["reversal"] = "truck" }},
		{"currency is required", CodePolicy, func(v map[string]any) {
			at(v, "records", "delivery", "fields")["price"] = map[string]any{"type": "money"}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := prepare(t)
			jsonEdit(AppFile, tc.change)(t, d.Files)
			if r := Validate(d, nil); !hasCode(r, tc.code) {
				t.Fatalf("want %s, got %s", tc.code, r)
			}
		})
	}
}
