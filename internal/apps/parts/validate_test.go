package parts

import (
	"errors"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
)

func TestSurfaceValidators(t *testing.T) {
	tests := []struct {
		name    string
		surface bridge.Surface
		text    string
		extra   *bridge.PartExtra
		want    string
	}{
		{"canvas empty", bridge.SurfaceCanvas, `{"title":"Deck","frame":"16:9","boxes":[]}`, nil, ""},
		{"canvas edge", bridge.SurfaceCanvas, `{"title":"Deck","frame":"16:9","status":"Final","boxes":[{"id":"a","type":"text","x":0,"y":0,"w":1280,"h":720}]}`, nil, ""},
		{"canvas board negative", bridge.SurfaceCanvas, `{"title":"Board","frame":"board","boxes":[{"id":"a","type":"shape","x":-10,"y":-20,"w":9000,"h":8000}]}`, nil, ""},
		{"canvas outside width", bridge.SurfaceCanvas, `{"title":"Deck","frame":"16:9","boxes":[{"id":"a","type":"text","x":1,"y":0,"w":1280,"h":720}]}`, nil, "1280×720"},
		{"canvas outside height", bridge.SurfaceCanvas, `{"title":"Deck","frame":"16:9","boxes":[{"id":"a","type":"text","x":0,"y":1,"w":1280,"h":720}]}`, nil, "1280×720"},
		{"canvas negative x", bridge.SurfaceCanvas, `{"title":"Deck","frame":"16:9","boxes":[{"id":"a","type":"text","x":-1,"y":0,"w":1,"h":1}]}`, nil, "1280×720"},
		{"canvas missing geometry", bridge.SurfaceCanvas, `{"title":"Deck","frame":"16:9","boxes":[{"id":"a","type":"text","x":0,"y":0}]}`, nil, "numeric"},
		{"canvas negative size", bridge.SurfaceCanvas, `{"title":"Board","frame":"board","boxes":[{"id":"a","type":"shape","x":0,"y":0,"w":-1,"h":1}]}`, nil, "positive"},
		{"canvas unknown box", bridge.SurfaceCanvas, `{"title":"Deck","frame":"board","boxes":[{"id":"a","type":"unknown"}]}`, nil, "supported type"},
		{"canvas missing array", bridge.SurfaceCanvas, `{"title":"Deck","frame":"board"}`, nil, "boxes array"},
		{"canvas bad status", bridge.SurfaceCanvas, `{"title":"Deck","frame":"board","status":"Ready","boxes":[]}`, nil, "status"},
		{"canvas duplicate", bridge.SurfaceCanvas, `{"title":"Deck","frame":"board","boxes":[{"id":"a","type":"shape","x":0,"y":0,"w":1,"h":1},{"id":"a","type":"shape","x":0,"y":0,"w":1,"h":1}]}`, nil, "duplicate"},
		{"canvas font", bridge.SurfaceCanvas, `{"title":"Deck","frame":"board","boxes":[{"id":"a","type":"shape","x":0,"y":0,"w":1,"h":1,"style":{"font":"comic"}}]}`, nil, "font"},
		{"text markdown", bridge.SurfaceText, "# Goals\nHi {{deal.name}} · ✓", nil, ""},
		{"text empty", bridge.SurfaceText, "", nil, ""},
		{"text invalid utf8", bridge.SurfaceText, string([]byte{0xff}), nil, "UTF-8"},
		{"text extra", bridge.SurfaceText, "hello", &bridge.PartExtra{}, "only supported for grid"},
		{"grid csv", bridge.SurfaceGrid, "Name,Value\nQ4,12\n", &bridge.PartExtra{Formulas: map[string]string{"C5": "=SUM(C2:C4)-B5"}}, ""},
		{"grid quoted", bridge.SurfaceGrid, `name,notes
a,"line 1
line 2"
`, nil, ""},
		{"grid empty", bridge.SurfaceGrid, "", nil, ""},
		{"grid malformed", bridge.SurfaceGrid, `a,"b`, nil, "invalid CSV"},
		{"grid ragged", bridge.SurfaceGrid, "a,b\nc\n", nil, "invalid CSV"},
		{"grid invalid utf8", bridge.SurfaceGrid, string([]byte{0xff}), nil, "UTF-8"},
		{"grid address", bridge.SurfaceGrid, "a", &bridge.PartExtra{Formulas: map[string]string{"../file": "=1"}}, "cell addresses"},
		{"grid formula", bridge.SurfaceGrid, "a", &bridge.PartExtra{Formulas: map[string]string{"A1": "12"}}, "starting with ="},
		{"timeline empty", bridge.SurfaceTimeline, `{"items":[],"view":"calendar"}`, nil, ""},
		{"timeline items", bridge.SurfaceTimeline, `{"items":[{"id":"a","title":"Plan","start":"2026-10-13","end":"2026-10-14"}],"view":"gantt"}`, nil, ""},
		{"timeline missing date", bridge.SurfaceTimeline, `{"items":[{"id":"a","title":"Plan"}],"view":"gantt"}`, nil, "start and end"},
		{"timeline unknown dependency", bridge.SurfaceTimeline, `{"items":[{"id":"a","title":"Plan","start":"2026-10-13","end":"2026-10-14","dependsOn":["b"]}],"view":"gantt"}`, nil, "dependency"},
		{"timeline bad view", bridge.SurfaceTimeline, `{"items":[],"view":"list"}`, nil, "view"},
		{"graph empty", bridge.SurfaceGraph, `{"nodes":[],"links":[],"layout":"free"}`, nil, ""},
		{"graph link", bridge.SurfaceGraph, `{"nodes":[{"id":"a","label":"A"},{"id":"b","label":"B","x":12}],"links":[{"from":"a","to":"b"}],"layout":"tree"}`, nil, ""},
		{"graph missing arrays", bridge.SurfaceGraph, `{"layout":"tree"}`, nil, "nodes and links"},
		{"graph dangling", bridge.SurfaceGraph, `{"nodes":[{"id":"a","label":"A"}],"links":[{"from":"a","to":"b"}],"layout":"flow"}`, nil, "endpoints"},
		{"graph duplicate", bridge.SurfaceGraph, `{"nodes":[{"id":"a","label":"A"},{"id":"a","label":"B"}],"links":[],"layout":"tree"}`, nil, "unique"},
		{"map empty", bridge.SurfaceMap, `{"type":"FeatureCollection","features":[]}`, nil, ""},
		{"map place", bridge.SurfaceMap, `{"type":"FeatureCollection","features":[{"type":"Feature","properties":{"label":"HQ","kind":"place"},"geometry":{"type":"Point","coordinates":[-79.4,43.7]}}]}`, nil, ""},
		{"map null geometry", bridge.SurfaceMap, `{"type":"FeatureCollection","features":[{"type":"Feature","properties":{"label":"HQ","kind":"live"},"geometry":null}]}`, nil, ""},
		{"map not collection", bridge.SurfaceMap, `{"type":"Point","coordinates":[1,2]}`, nil, "FeatureCollection"},
		{"map bad feature", bridge.SurfaceMap, `{"type":"FeatureCollection","features":[{"type":"Point"}]}`, nil, "type Feature"},
		{"map bad kind", bridge.SurfaceMap, `{"type":"FeatureCollection","features":[{"type":"Feature","properties":{"label":"HQ","kind":"other"}}]}`, nil, "kind"},
		{"map bad coordinates", bridge.SurfaceMap, `{"type":"FeatureCollection","features":[{"type":"Feature","properties":{"label":"HQ","kind":"place"},"geometry":{"type":"Point","coordinates":[1]}}]}`, nil, "coordinates"},
		{"map missing geometry", bridge.SurfaceMap, `{"type":"FeatureCollection","features":[{"type":"Feature","properties":{"label":"HQ","kind":"place"}}]}`, nil, "geometry"},
		{"map short line", bridge.SurfaceMap, `{"type":"FeatureCollection","features":[{"type":"Feature","properties":{"label":"HQ","kind":"route"},"geometry":{"type":"LineString","coordinates":[[1,2]]}}]}`, nil, "line or polygon"},
		{"map open polygon", bridge.SurfaceMap, `{"type":"FeatureCollection","features":[{"type":"Feature","properties":{"label":"HQ","kind":"place"},"geometry":{"type":"Polygon","coordinates":[[[0,0],[0,1],[1,1],[1,0]]]}}]}`, nil, "line or polygon"},
		{"map polygon", bridge.SurfaceMap, `{"type":"FeatureCollection","features":[{"type":"Feature","properties":{"label":"HQ","kind":"place"},"geometry":{"type":"Polygon","coordinates":[[[0,0],[0,1],[1,1],[0,0]]]}}]}`, nil, ""},
		{"map wrong coordinates type", bridge.SurfaceMap, `{"type":"FeatureCollection","features":[{"type":"Feature","properties":{"label":"HQ","kind":"place"},"geometry":{"type":"Point","coordinates":["x","y"]}}]}`, nil, "coordinates"},
		{"map collection geometry", bridge.SurfaceMap, `{"type":"FeatureCollection","features":[{"type":"Feature","properties":{"label":"HQ","kind":"route"},"geometry":{"type":"GeometryCollection","geometries":[{"type":"LineString","coordinates":[[1,2],[3,4]]}]}}]}`, nil, ""},
		{"unknown", bridge.Surface("other"), "", nil, "unknown surface"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validate(tt.surface, tt.text, tt.extra)
			if tt.want == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			var wire *bridge.Error
			if !errors.As(err, &wire) || wire.Code != bridge.CodeInvalid || !strings.Contains(wire.Message, tt.want) {
				t.Fatalf("error = %v, want invalid containing %q", err, tt.want)
			}
		})
	}
	for _, surface := range []bridge.Surface{bridge.SurfaceCanvas, bridge.SurfaceTimeline, bridge.SurfaceGraph, bridge.SurfaceMap} {
		for _, text := range []string{`null`, `[]`, `{`, "{}", string([]byte{'{', '"', 'x', '"', ':', '"', 0xff, '"', '}'})} {
			if err := validate(surface, text, nil); err == nil {
				t.Errorf("%s accepted %q", surface, text)
			}
		}
	}
}
