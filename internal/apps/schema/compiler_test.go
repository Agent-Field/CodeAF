package schema

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
)

func TestGalleryScreenRecipes(t *testing.T) {
	def := load(t, "testdata/incident-room")
	testCases := []struct {
		r    Recipe
		part string
	}{
		{Recipe{Kind: "add-view", ID: "list", Record: "incident", As: "list"}, "RecordList"},
		{Recipe{Kind: "add-view", ID: "board", Record: "incident", As: "board", By: "status"}, "RecordList"},
		{Recipe{Kind: "add-view", ID: "table", Record: "incident", As: "table", Fields: []string{"title"}}, "RecordList"},
		{Recipe{Kind: "add-view", ID: "calendar", Record: "incident", As: "calendar", By: "opened_at"}, "RecordList"},
		{Recipe{Kind: "add-form", ID: "form", Record: "incident", Fields: []string{"title"}}, "Form"},
		{Recipe{Kind: "add-detail", ID: "detail", Record: "incident"}, "RecordPage"},
		{Recipe{Kind: "add-queue", ID: "triage", Record: "incident", Fields: []string{"status"}}, "TriageQueue"},
	}
	for _, tc := range testCases {
		t.Run(tc.r.ID, func(t *testing.T) {
			result, err := CompileRecipe(def, nil, tc.r)
			if err != nil {
				t.Fatal(err)
			}
			s, err := ParseScreen(result.Screen)
			if err != nil || findRecipePart(s.Body, tc.part) == nil || s.Body.ID == "" {
				t.Fatalf("wrong screen %s (%v)", result.Screen, err)
			}
			again, err := CompileRecipe(def, nil, tc.r)
			if err != nil || !bytes.Equal(again.Screen, result.Screen) {
				t.Fatalf("recipe changed the stable screen or node ID: %v", err)
			}
		})
	}
	if _, err := CompileRecipe(def, nil, Recipe{Kind: "add-view", ID: "invalid", Record: "incident", As: "board", By: "title"}); err == nil {
		t.Fatal("a board's grouping field must be described by a groupable field manifest")
	}
}

func TestGalleryChartRecipeUsesDeclaredAggregate(t *testing.T) {
	def := load(t, "testdata/dispatch")
	result, err := CompileRecipe(def, nil, Recipe{Kind: "add-chart", ID: "arrivals", Record: "delivery", Group: "eta", Aggregate: "count"})
	if err != nil {
		t.Fatal(err)
	}
	s, err := ParseScreen(result.Screen)
	if err != nil {
		t.Fatal(err)
	}
	q := s.Views[s.Body.Bind.View]
	if q.Type != "delivery" || len(q.Aggregate) != 1 || q.Aggregate[0].Op != "count" {
		t.Fatalf("wrong declared query: %+v", q)
	}
	finance := load(t, "testdata/month-end-close")
	result, err = CompileRecipe(finance, nil, Recipe{Kind: "add-chart", ID: "monthly", Record: "close", Group: "closed", Aggregate: "sum:amount"})
	if err != nil {
		t.Fatal(err)
	}
	s, _ = ParseScreen(result.Screen)
	if agg := s.Views[s.Body.Bind.View].Aggregate; len(agg) != 1 || agg[0].Field != "amount" || agg[0].Op != "sum" {
		t.Fatalf("wrong declared sum: %+v", agg)
	}
	if _, err := CompileRecipe(finance, nil, Recipe{Kind: "add-chart", ID: "bad-sum", Record: "close", Group: "closed", Aggregate: "sum:title"}); err == nil {
		t.Fatal("the field registry must reject summing text")
	}
}

func TestGalleryMarkupUsesTheSameManifestChecks(t *testing.T) {
	def := load(t, "testdata/inventory-po")
	input := ScreenMarkup{Screen: "new-stock", Title: "Stock", Markup: `
		<Stack id="body">
		  <Text prop-text="On hand" />
		  <Table id="stock" label="Stock" bind-records="sku" prop-columns='["title","on_hand"]'/>
		</Stack>`}
	raw, err := CompileScreenMarkup(def, nil, input)
	if err != nil {
		t.Fatal(err)
	}
	s, err := ParseScreen(raw)
	if err != nil {
		t.Fatal(err)
	}
	if s.Body.ID != "body" || s.Body.Children[0].ID == "" || s.Views["stock"].Type != "sku" {
		t.Fatalf("wrong markup screen %s", raw)
	}
	again, err := CompileScreenMarkup(def, nil, input)
	if err != nil || !bytes.Equal(raw, again) {
		t.Fatalf("markup changed the stable ID: %v", err)
	}
	for name, markup := range map[string]string{
		"duplicate": `<Stack id="body"><Text id="body" prop-text="x"/></Stack>`,
		"binding":   `<Text prop-text="x" bind-records="sku"/>`,
		"props":     `<Text prop-style="red"/>`,
		"event":     `<Text prop-text="x" on-click='[{"toast":"yes"}]'/>`,
		"doctype":   `<!DOCTYPE test SYSTEM "https://example.org/schema"><Text prop-text="x"/>`,
	} {
		t.Run(name, func(t *testing.T) {
			input.Markup = markup
			if _, err := CompileScreenMarkup(def, nil, input); err == nil {
				t.Fatal("markup should be rejected")
			}
		})
	}
}

func TestGalleryNodeIDsAreRequiredAndUnique(t *testing.T) {
	def := load(t, "testdata/inventory-po")
	jsonEdit("screens/stock.json", func(v map[string]any) {
		delete(at(v, "body"), "id")
	})(t, def.Files)
	if r := Validate(def, nil); !hasCode(r, CodeSchema) {
		t.Fatalf("a missing node ID must be refused: %s", r)
	}
	def = load(t, "testdata/inventory-po")
	jsonEdit("screens/stock.json", func(v map[string]any) {
		at(v, "body")["id"] = at(v, "body", "children", 0)["id"]
	})(t, def.Files)
	if r := Validate(def, nil); !hasCode(r, CodeDuplicateID) {
		t.Fatalf("a duplicate node ID must be refused: %s", r)
	}
	def = load(t, "testdata/inventory-po")
	jsonEdit("screens/stock.json", func(v map[string]any) {
		at(v, "body")["id"] = "01KAT0000A0000000000000010"
	})(t, def.Files)
	if r := Validate(def, nil); !r.OK() {
		t.Fatalf("a ULID node ID must pass: %s", r)
	}
}

func TestGalleryLifecycleRecipesRouteBoardMovesAndCheckGuards(t *testing.T) {
	def := load(t, "testdata/incident-room")
	apply := func(r Recipe) {
		t.Helper()
		result, err := CompileRecipe(def, nil, r)
		if err != nil || len(result.App) == 0 {
			t.Fatalf("%s: no app declaration (%v)", r.Kind, err)
		}
		def.Files[AppFile] = result.App
	}
	apply(Recipe{Kind: "add-lifecycle", ID: "incident-states", Record: "incident", By: "status", States: []string{"investigating", "resolved"}})

	transition := Transition{Label: "Resolve", From: []string{"investigating"}, To: "resolved", Actors: []string{"person"}, Guard: `$record.missing != ""`}
	bad := Recipe{Kind: "add-transition", ID: "resolve", Record: "incident", Transition: &transition}
	if _, err := CompileRecipe(def, nil, bad); err == nil || !strings.Contains(err.Error(), "expression") {
		t.Fatalf("a guard cannot read an undeclared field: %v", err)
	}
	transition.Guard = `$record.title != ""`
	apply(bad)

	r := Recipe{Kind: "add-view", ID: "state-board", Record: "incident", As: "board", By: "status"}
	result, err := CompileRecipe(def, nil, r)
	if err != nil {
		t.Fatal(err)
	}
	screen, _ := ParseScreen(result.Screen)
	board := findRecipePart(screen.Body, "RecordList")
	if board == nil || len(board.On["move"]) != 1 || board.On["move"][0]["lifecycle.transition"] == nil {
		t.Fatalf("board did not compile a transition: %s", result.Screen)
	}
	for _, bad := range []string{
		`<RecordList label="States" bind-records="incident" prop-views='["board"]' prop-board_by="status"/>`,
		`<RecordList label="States" bind-records="incident" prop-views='["board"]' prop-board_by="status"
		    on-move='[{"records.update":{"type":"incident","id":"$row.id","set":{"status":"resolved"}}}]'/>`,
	} {
		if _, err := CompileScreenMarkup(def, nil, ScreenMarkup{Screen: "unsafe-board", Title: "State board", Markup: bad}); err == nil {
			t.Fatal("a board must route its drag through the lifecycle")
		}
	}

	form := ProcessForm{Record: "incident", Title: "Prepare evidence", SaveDraft: true, Steps: []FormStep{
		{ID: "facts", Label: "Facts", Fields: []FormField{{Field: "title", Required: true}}},
		{ID: "timeline", Label: "Timeline", When: `title != ""`, Fields: []FormField{{Field: "opened_at", When: `title != ""`}}},
	}}
	work := WorkItem{Title: "Prepare evidence", Subject: RecordEndpoint{Type: "incident", ID: "$record.id"}, Form: "evidence", Assignee: "me", SLA: "PT4H"}
	apply(Recipe{Kind: "add-form", ID: "evidence", Record: "incident", Form: &form, WorkItem: &work})
	apply(Recipe{Kind: "add-checklist", ID: "response", Template: &Template{Record: "incident", Title: "Response",
		Items: []TemplateItem{{ID: "prepare", WorkItem: "evidence"}, {ID: "review", WorkItem: "evidence", After: []string{"prepare"}}}}})
	apply(Recipe{Kind: "add-signoff", ID: "review", SignOff: &Policy{Kind: "signoff", Config: map[string]any{
		"record": "incident", "statement": "I reviewed the response.",
		"roles": []map[string]any{{"id": "reviewer", "label": "Reviewer", "person": "me"}},
	}}})
	apply(Recipe{Kind: "add-timer", ID: "follow-up", Timer: &ProcessTimer{Record: "incident", After: "P3D",
		Effects: []Action{{"work.create": map[string]any{"template": "evidence", "id": "$record.id"}}}}})
	if r := Validate(def, nil); !r.OK() {
		t.Fatal(r)
	}
}

func TestGalleryDeclaredQueryResolvesStableTypeAndFieldIDs(t *testing.T) {
	const typeID = "01KAT0000A0000000000000011"
	const fieldID = "01KAT0000A0000000000000012"
	d := load(t, "testdata/inventory-po")
	jsonEdit(AppFile, func(v map[string]any) {
		at(v, "records", "sku")["id"] = typeID
		at(v, "records", "sku", "fields", "on_hand")["id"] = fieldID
	})(t, d.Files)
	jsonEdit("screens/stock.json", func(v map[string]any) {
		q := at(v, "views", "sku-list")
		q["type"] = typeID
		q["select"] = []string{fieldID}
	})(t, d.Files)
	if r := Validate(d, nil); !r.OK() {
		t.Fatal(r)
	}
}

func TestGalleryCompilerReadsLinkedSamples(t *testing.T) {
	def := load(t, "testdata/ncr")
	result, err := CompileRecipe(def, nil, Recipe{Kind: "add-lifecycle", ID: "ncr-states", Record: "ncr", States: []string{"open", "closed"}})
	if err != nil || len(result.App) == 0 {
		t.Fatalf("a linked sample must compile: %v", err)
	}
}

func TestGalleryCompilerRequiresExplicitScreenReplacement(t *testing.T) {
	def := load(t, "testdata/inventory-po")
	if _, err := CompileRecipe(def, nil, Recipe{Kind: "add-view", ID: "stock", Record: "sku", As: "table"}); err == nil {
		t.Fatal("add-view should refuse an existing screen")
	}
	markup := ScreenMarkup{Screen: "stock", Title: "Stock", Markup: `<Text id="stock" prop-text="In stock"/>`}
	if _, err := CompileScreenMarkup(def, nil, markup); err == nil {
		t.Fatal("markup should require explicit replacement for an existing screen")
	}
	markup.Replace = true
	if _, err := CompileScreenMarkup(def, nil, markup); err != nil {
		t.Fatalf("set-screen should accept an explicit replacement: %v", err)
	}
}

func findRecipePart(root *Node, part string) *Node {
	if root == nil || root.Part == part {
		return root
	}
	for _, child := range root.Children {
		if found := findRecipePart(child, part); found != nil {
			return found
		}
	}
	return nil
}

func TestCollectionRecipeDesignFromTheRecordShape(t *testing.T) {
	def := load(t, "testdata/incident-room")
	jsonEdit(AppFile, func(app map[string]any) {
		fields := at(app, "records", "incident", "fields")
		fields["owner"] = map[string]any{"type": "person"}
		fields["cost"] = map[string]any{"type": "money", "currency": "USD"}
	})(t, def.Files)
	life, err := CompileRecipe(def, nil, Recipe{Kind: "add-lifecycle", ID: "incident-states", Record: "incident", By: "status", States: []string{"investigating", "resolved"}})
	if err != nil {
		t.Fatal(err)
	}
	def.Files[AppFile] = life.App
	output, err := CompileRecipe(def, nil, Recipe{Kind: "add-view", ID: "incidents", Record: "incident", As: "table", Fields: []string{"owner", "opened_at", "cost", "status", "title"}})
	if err != nil {
		t.Fatal(err)
	}
	screen, err := ParseScreen(output.Screen)
	if err != nil {
		t.Fatal(err)
	}
	list := findRecipePart(screen.Body, "RecordList")
	if list == nil {
		t.Fatal("the collection must use the Desk’s content-sized record columns and whole-value money and date cells")
	}
	t.Run("column order", func(t *testing.T) {
		want := []any{"title", "status", "cost", "opened_at", "owner"}
		if !reflect.DeepEqual(list.Props["columns"], want) {
			t.Fatalf("columns = %v, want %v", list.Props["columns"], want)
		}
	})
	t.Run("content widths and whole values", func(t *testing.T) {
		view := screen.Views[list.Bind.View]
		if view.Type != "incident" || !reflect.DeepEqual(view.Select, []string{"title", "status", "cost", "opened_at", "owner"}) {
			t.Fatalf("typed renderer must receive the original fields, got %+v", view)
		}
	})
	t.Run("board and calendar", func(t *testing.T) {
		if !reflect.DeepEqual(list.Props["views"], []any{"list", "board", "calendar"}) || list.Props["board_by"] != "status" ||
			at(list.Props, "viewOptions", "calendar")["start"] != "opened_at" {
			t.Fatalf("view defaults = %+v", list.Props)
		}
	})
	t.Run("stat tiles", func(t *testing.T) {
		var cost, status bool
		for _, n := range screen.Body.Children {
			if n.Part != "Chart" || n.Props["kind"] != "metric" {
				continue
			}
			q := screen.Views[n.Bind.View]
			if len(q.Aggregate) == 1 {
				cost = cost || q.Aggregate[0].Op == "sum" && q.Aggregate[0].Field == "cost"
				status = status || q.Aggregate[0].Op == "count" && len(q.Filter) == 1 && q.Filter[0].Field == "status"
			}
		}
		if !cost || !status {
			t.Fatalf("expected a cost and status metric: %s", output.Screen)
		}
	})
	t.Run("empty state", func(t *testing.T) {
		if list.Props["empty"] != "New incident records will appear here." {
			t.Fatalf("empty = %q", list.Props["empty"])
		}
	})
}
