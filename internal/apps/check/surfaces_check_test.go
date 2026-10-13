package check

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/parts"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

var surfaceNames = []string{"Canvas", "Text", "Grid", "Timeline", "Graph", "Map"}

func testKit(t *testing.T) *spec.Kit {
	t.Helper()
	kit, err := spec.LoadKit()
	if err != nil {
		t.Fatal(err)
	}
	return kit
}

func surfaceNode(name string) *spec.Node {
	return &spec.Node{Part: name, Bind: &spec.Binding{Doc: "sample/document", Surface: strings.ToLower(name)}}
}

func TestSurfaceLawBindings(t *testing.T) {
	kit := testKit(t)
	for _, name := range surfaceNames {
		t.Run(name, func(t *testing.T) {
			kind := strings.ToLower(name)
			for _, tc := range []struct {
				name string
				bind *spec.Binding
				path string
			}{
				{"literal", &spec.Binding{Doc: "sample/doc", Surface: kind}, ""},
				{"reference", &spec.Binding{Doc: "$record.doc", Surface: kind}, ""},
				{"no binding", nil, "/body/bind"},
				{"no doc", &spec.Binding{Surface: kind}, "/body/bind/doc"},
				{"blank doc", &spec.Binding{Doc: " \t", Surface: kind}, "/body/bind/doc"},
				{"no surface", &spec.Binding{Doc: "sample/doc"}, "/body/bind/surface"},
				{"wrong surface", &spec.Binding{Doc: "sample/doc", Surface: "editor"}, "/body/bind/surface"},
				{"another surface", &spec.Binding{Doc: "sample/doc", Surface: alternateSurface(kind)}, "/body/bind/surface"},
				{"wrong case", &spec.Binding{Doc: "sample/doc", Surface: name}, "/body/bind/surface"},
				{"mixed binding", &spec.Binding{Doc: "sample/doc", Surface: kind, Records: "deck"}, "/body/bind"},
			} {
				t.Run(tc.name, func(t *testing.T) {
					screen := &spec.Screen{Body: &spec.Node{Part: name, Bind: tc.bind}}
					issues := surfaceLaw(screen, kit, "screens/deck.json")
					if tc.path == "" {
						if len(issues) != 0 {
							t.Fatal(issues)
						}
						return
					}
					if len(issues) != 1 || issues[0].Rule != "kit/surface" || issues[0].File != "screens/deck.json" || issues[0].Path != tc.path || issues[0].FixHint == "" {
						t.Fatalf("want %s; got %+v", tc.path, issues)
					}
				})
			}
		})
	}
}

func alternateSurface(kind string) string {
	if kind == "canvas" {
		return "text"
	}
	return "canvas"
}

func TestEditorRefusalThroughCheck(t *testing.T) {
	for _, name := range []string{"custom/SlideEditor", "SlideEditor"} {
		t.Run(name, func(t *testing.T) {
			files, sample := fixture(t)
			files["screens/deal.json"] = []byte(`{"screen":"deal","title":"Deck","body":{"part":"` + name + `"}}`)
			report, err := New(svc.Config{}, services(sample)).Run(context.Background(), "crm", files, 1)
			if err != nil || report.Pass {
				t.Fatalf("editor passed: %+v (%v)", report, err)
			}
			for _, issue := range report.Issues {
				if issue.File == "screens/deal.json" && issue.Path == "/body/part" && issue.Rule == "kit/surface" && strings.Contains(issue.Message, surfaceMessage) && strings.Contains(issue.Message, "Canvas") {
					return
				}
			}
			t.Fatalf("missing board refusal: %+v", report.Issues)
		})
	}
}

func TestSurfaceLawEditors(t *testing.T) {
	kit := testKit(t)
	for _, tc := range []struct{ part, suggestion string }{
		{"custom/SlideEditor", "Canvas"},
		{"SlideEditor", "Canvas"},
		{"DocumentEditor", "Text"},
		{"SpreadsheetEditor", "Grid"},
		{"CalendarEditor", "Timeline"},
		{"GraphEditor", "Graph"},
		{"MapEditor", "Map"},
		{"Editor", "Text, Canvas, Grid, Timeline, Graph or Map"},
		{"custom/slideeditor", "Canvas"},
		{"Button", ""},
		{"custom/Card", ""},
	} {
		t.Run(tc.part, func(t *testing.T) {
			issues := surfaceLaw(&spec.Screen{Body: &spec.Node{Part: tc.part}}, kit, "screens/deck.json")
			if tc.suggestion == "" {
				if len(issues) != 0 {
					t.Fatal(issues)
				}
				return
			}
			if len(issues) != 1 || issues[0].Path != "/body/part" || issues[0].Message != surfaceMessage || !strings.Contains(issues[0].FixHint, tc.suggestion) {
				t.Fatalf("want board message and %s; got %+v", tc.suggestion, issues)
			}
		})
	}
}

func TestSurfaceLawCount(t *testing.T) {
	kit := testKit(t)
	for _, name := range surfaceNames {
		for _, tc := range []struct {
			name string
			nav  *spec.Node
			body *spec.Node
			path string
		}{
			{"one", nil, surfaceNode(name), ""},
			{"pages", nil, &spec.Node{Part: "Stack", Children: []*spec.Node{{Part: "Pages"}, surfaceNode(name)}}, ""},
			{"duplicate", nil, &spec.Node{Part: "Stack", Children: []*spec.Node{surfaceNode(name), surfaceNode(name)}}, "/body/children/1/part"},
			{"different", nil, &spec.Node{Part: "Stack", Children: []*spec.Node{surfaceNode(name), surfaceNode("Canvas")}}, "/body/children/1/part"},
			{"nav", surfaceNode(name), surfaceNode(name), "/body/part"},
			{"slot", nil, &spec.Node{Part: "Panel", Children: []*spec.Node{surfaceNode(name)}, Slots: map[string]*spec.Node{"empty": surfaceNode(name)}}, "/body/slots/empty/part"},
		} {
			t.Run(name+"/"+tc.name, func(t *testing.T) {
				issues := surfaceLaw(&spec.Screen{Nav: tc.nav, Body: tc.body}, kit, "screens/deck.json")
				if tc.path == "" {
					if len(issues) != 0 {
						t.Fatal(issues)
					}
					return
				}
				if len(issues) != 1 || issues[0].Path != tc.path || issues[0].Message != "At most one surface per screen." {
					t.Fatalf("want %s; got %+v", tc.path, issues)
				}
			})
		}
	}
}

func TestSurfaceTextCopyIsNotAnEditor(t *testing.T) {
	kit := testKit(t)
	screen := &spec.Screen{Body: &spec.Node{Part: "Stack", Children: []*spec.Node{
		{Part: "Text", Props: map[string]any{"text": "Heading"}},
		{Part: "Text", Props: map[string]any{"text": ""}},
		surfaceNode("Canvas"),
	}}}
	if issues := surfaceLaw(screen, kit, "screens/deck.json"); len(issues) != 0 {
		t.Fatal(issues)
	}
}

func TestSurfaceSampleDocuments(t *testing.T) {
	for _, tc := range []struct {
		kind  bridge.Surface
		props map[string]any
		part  string
	}{
		{bridge.SurfaceCanvas, nil, "slides/1.json"},
		{bridge.SurfaceCanvas, map[string]any{"frame": "board"}, "artboards/1.json"},
		{bridge.SurfaceText, nil, "section-1.md"},
		{bridge.SurfaceGrid, nil, "sheets/sample.csv"},
		{bridge.SurfaceTimeline, nil, "plan/sample.json"},
		{bridge.SurfaceTimeline, map[string]any{"view": "calendar"}, "plan/sample.json"},
		{bridge.SurfaceTimeline, map[string]any{"view": "shifts"}, "plan/sample.json"},
		{bridge.SurfaceGraph, nil, "graph/sample.json"},
		{bridge.SurfaceGraph, map[string]any{"view": "tree"}, "graph/sample.json"},
		{bridge.SurfaceGraph, map[string]any{"view": "flow"}, "graph/sample.json"},
		{bridge.SurfaceMap, nil, "places/sample.json"},
	} {
		t.Run(string(tc.kind)+"/"+tc.part+"/"+sampleProp(tc.props, "view", "default"), func(t *testing.T) {
			sample, err := sampleSurfaceDoc("sample/doc", tc.kind, tc.props)
			if err != nil {
				t.Fatal(err)
			}
			if sample.Doc.Doc != "sample/doc" || sample.Doc.Surface != tc.kind || len(sample.Doc.Parts) != 1 || sample.Doc.Parts[0].Part != tc.part || sample.Part.Part != tc.part || sample.Part.Version != 1 || sample.Part.Text != string(sample.Files[tc.part]) {
				t.Fatalf("bad document: %+v", sample)
			}
			var order struct{ Parts []string }
			if err := json.Unmarshal(sample.Files["doc.json"], &order); err != nil || !reflect.DeepEqual(order.Parts, []string{tc.part}) {
				t.Fatalf("bad order: %s (%v)", sample.Files["doc.json"], err)
			}
			if tc.kind == bridge.SurfaceGrid && (sample.Part.Extra == nil || sample.Part.Extra.Formulas == nil || string(sample.Files["sheets/sample.formulas.json"]) != `{}`) {
				t.Fatal("missing grid formulas")
			}
			if tc.kind == bridge.SurfaceCanvas && (sample.Part.Status != "Draft" || string(sample.Files["theme.json"]) != `{}`) {
				t.Fatal("missing Canvas theme or status")
			}
		})
	}
}

func TestInvalidSurfaceSamples(t *testing.T) {
	for _, tc := range []struct {
		kind bridge.Surface
		text string
	}{
		{bridge.SurfaceCanvas, `{}`},
		{bridge.SurfaceCanvas, `{"title":"Slide","status":"Draft","frame":"16:9","notes":"","boxes":[{"id":"title","type":"text","text":"Title","x":0,"y":0,"w":-1,"h":1}]}`},
		{bridge.SurfaceCanvas, `{"title":"Slide","status":"Draft","frame":"16:9","notes":"","boxes":[{"id":"title","type":"text","text":"Title","x":1280,"y":0,"w":1,"h":1}]}`},
		{bridge.SurfaceText, string([]byte{0xff})},
		{bridge.SurfaceGrid, `header,"unterminated`},
		{bridge.SurfaceGrid, "A,B\n1\n"},
		{bridge.SurfaceTimeline, `{"items":[],"view":"wrong"}`},
		{bridge.SurfaceGraph, `{"nodes":[{"id":"x","label":"X"}],"layout":"free"}`},
		{bridge.SurfaceMap, `{"type":"FeatureCollection","features":[{"type":"Feature","geometry":{"type":"Point","coordinates":["bad",0]},"properties":{"label":"Place","kind":"place"}}]}`},
		{bridge.SurfaceMap, `{} {}`},
		{bridge.Surface("unknown"), `{}`},
	} {
		t.Run(string(tc.kind), func(t *testing.T) {
			if err := parts.Validate(tc.kind, tc.text, nil); err == nil {
				t.Fatalf("accepted invalid sample: %s", tc.text)
			}
		})
	}
	for _, kind := range []bridge.Surface{bridge.SurfaceCanvas, bridge.SurfaceTimeline, bridge.SurfaceGraph, "unknown"} {
		if _, err := sampleSurfaceDoc("doc", kind, map[string]any{"frame": "wrong", "view": "wrong"}); err == nil {
			t.Fatalf("accepted unsupported options: %s", kind)
		}
	}
}

func TestSurfaceRenderBindings(t *testing.T) {
	kit := testKit(t)
	for _, name := range surfaceNames {
		for _, tc := range []struct {
			doc  string
			fail bool
		}{
			{"decks/q4", false},
			{"$record.name", false},
			{"decks/{{record.name}}", false},
			{"$record.missing", true},
			{"$record.amount", true},
			{"", true},
		} {
			t.Run(name+"/"+tc.doc, func(t *testing.T) {
				files, sample := fixture(t)
				bundle, err := spec.Decode(files)
				if err != nil {
					t.Fatal(err)
				}
				node := surfaceNode(name)
				node.Bind.Doc = tc.doc
				screen := &spec.Screen{Screen: "deck", Params: []string{"id"}, Body: &spec.Node{Part: "Stack", Children: []*spec.Node{
					{Part: "RecordPage", Bind: &spec.Binding{Record: "$params.id", Type: "deal"}}, node,
				}}}
				issues := render(context.Background(), bundle, kit, screen, sample.rows)
				if !tc.fail {
					if len(issues) != 0 {
						t.Fatal(issues)
					}
					return
				}
				if len(issues) != 1 || issues[0].Rule != "render/surface" || issues[0].Path != "/body/children/1/bind/doc" {
					t.Fatalf("want one surface render error; got %+v", issues)
				}
			})
		}
	}
}

func TestSurfaceRenderRejectsInvalidPartsSample(t *testing.T) {
	kit := testKit(t)
	for _, tc := range []struct{ name, prop string }{
		{"Canvas", "frame"},
		{"Timeline", "view"},
		{"Graph", "view"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			files, sample := fixture(t)
			bundle, err := spec.Decode(files)
			if err != nil {
				t.Fatal(err)
			}
			node := surfaceNode(tc.name)
			node.Props = map[string]any{tc.prop: "unsupported"}
			kind := bridge.Surface(strings.ToLower(tc.name))
			_, text, err := sampleSurfacePart(kind, node.Props)
			if err != nil {
				t.Fatal(err)
			}
			formatErr := parts.Validate(kind, text, nil)
			if formatErr == nil {
				t.Fatal("parts accepted the invalid generated sample")
			}
			screen := &spec.Screen{Screen: "deck", Body: node}
			issues := render(context.Background(), bundle, kit, screen, sample.rows)
			want := string(kind) + " sample: " + formatErr.Error()
			for _, issue := range issues {
				if issue.Rule == "render/surface" && issue.File == "screens/deck.json" && issue.Path == "/body" && issue.Message == want {
					return
				}
			}
			t.Fatalf("want surface render failure %q; got %+v", want, issues)
		})
	}
}

func TestSlidesDeckScreen(t *testing.T) {
	files, err := spec.ReadDir("testdata/slides")
	if err != nil {
		t.Fatal(err)
	}
	sample := &samples{rows: map[string][]bridge.Record{"deck": {{ID: "sample-deck", Type: "deck", Version: 1, Fields: map[string]any{"title": "Q4 board deck", "doc": "decks/q4-board-deck"}}}}}
	report, err := New(svc.Config{}, services(sample)).Run(context.Background(), "slides", files, 1)
	if err != nil || !report.Pass || !reflect.DeepEqual(report.Rendered, []string{"deck"}) {
		t.Fatalf("Slides fixture failed: %+v (%v)", report, err)
	}
}

func TestSurfacesThroughCheck(t *testing.T) {
	for _, name := range surfaceNames {
		for _, tc := range []struct {
			name string
			body *spec.Node
			rule string
		}{
			{"valid", surfaceNode(name), ""},
			{"missing binding", &spec.Node{Part: name}, "kit/surface"},
			{"mismatch", &spec.Node{Part: name, Bind: &spec.Binding{Doc: "sample/doc", Surface: alternateSurface(strings.ToLower(name))}}, "kit/surface"},
			{"two surfaces", &spec.Node{Part: "Stack", Children: []*spec.Node{surfaceNode(name), surfaceNode("Canvas")}}, "kit/surface"},
			{"unresolved doc", &spec.Node{Part: name, Bind: &spec.Binding{Doc: "$params.missing", Surface: strings.ToLower(name)}}, "render/surface"},
		} {
			t.Run(name+"/"+tc.name, func(t *testing.T) {
				files, sample := fixture(t)
				data, err := json.Marshal(spec.Screen{Screen: "deal", Title: "Document", Body: tc.body})
				if err != nil {
					t.Fatal(err)
				}
				files["screens/deal.json"] = data
				report, err := New(svc.Config{}, services(sample)).Run(context.Background(), "crm", files, 1)
				if err != nil {
					t.Fatal(err)
				}
				if tc.rule == "" {
					if !report.Pass || !contains(report.Rendered, "deal") {
						t.Fatalf("surface not rendered: %+v", report)
					}
					return
				}
				for _, issue := range report.Issues {
					if !report.Pass && issue.Rule == tc.rule && issue.File == "screens/deal.json" {
						return
					}
				}
				t.Fatalf("missing %s: %+v", tc.rule, report)
			})
		}
	}
}
