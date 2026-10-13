package check

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
)

const surfaceMessage = "Hard editing uses a surface. No hand-rolled editors; an app is easy parts plus one surface."

// Wave-1 schemas reject slashes in part names before kit lint can run. Keep
// that refusal, but give an editor the board's actionable surface suggestion.
func surfaceSchemaIssue(issue spec.Issue, files spec.Files) Issue {
	result := Issue{Issue: issue, FixHint: "match the file schema in internal/apps/spec/schema"}
	if !strings.HasPrefix(issue.File, "screens/") || !strings.HasSuffix(issue.Path, "/part") {
		return result
	}
	var screen spec.Screen
	if json.Unmarshal(files[issue.File], &screen) != nil {
		return result
	}
	kit, err := spec.LoadKit()
	if err != nil {
		return result
	}
	for _, surfaceIssue := range surfaceLaw(&screen, kit, issue.File) {
		if surfaceIssue.Path == issue.Path && surfaceIssue.Message == surfaceMessage {
			return surfaceIssue
		}
	}
	return result
}

func surfaceKind(n *spec.Node, kit *spec.Kit) bridge.Surface {
	part := kit.Part(n.Part)
	if part == nil || part.Kind != spec.KindSurface {
		return ""
	}
	// Board 04 also uses Text for primitive copy; its text prop retains that
	// role while document-bound Text is the editing surface.
	if n.Part == "Text" && n.Bind == nil && n.Props["text"] != nil {
		return ""
	}
	return bridge.Surface(strings.ToLower(n.Part))
}

func surfaceLaw(screen *spec.Screen, kit *spec.Kit, file string) []Issue {
	var issues []Issue
	count := 0
	var walk func(*spec.Node, string)
	walk = func(n *spec.Node, path string) {
		if n == nil {
			return
		}
		if kit.Part(n.Part) == nil && strings.HasSuffix(strings.ToLower(n.Part), "editor") {
			issues = append(issues, finding(file, path+"/part", "kit/surface", surfaceMessage, editorSurface(n.Part)))
		}
		if kind := surfaceKind(n, kit); kind != "" {
			count++
			if count > 1 {
				issues = append(issues, finding(file, path+"/part", "kit/surface", "At most one surface per screen.", "move this surface to its own screen; Pages may accompany one surface"))
			}
			issues = append(issues, surfaceBinding(n.Bind, kind, file, path+"/bind")...)
		}
		nodeChildren(n, path, walk)
	}
	walk(screen.Nav, "/nav")
	walk(screen.Body, "/body")
	return issues
}

func editorSurface(name string) string {
	name = strings.ToLower(name)
	for _, suggestion := range []struct{ words, part string }{
		{"slide canvas artboard design whiteboard", `{"part":"Canvas","props":{"frame":"16:9"}}`},
		{"text document doc markdown email contract note", `{"part":"Text"}`},
		{"grid sheet spreadsheet csv cell", `{"part":"Grid"}`},
		{"timeline gantt calendar shift roadmap", `{"part":"Timeline"}`},
		{"graph flow tree node", `{"part":"Graph"}`},
		{"map place route geo", `{"part":"Map"}`},
	} {
		for _, word := range strings.Fields(suggestion.words) {
			if strings.Contains(name, word) {
				return "use " + suggestion.part + " with a matching {doc, surface} binding"
			}
		}
	}
	return "choose Text, Canvas, Grid, Timeline, Graph or Map with a matching {doc, surface} binding"
}

func surfaceBinding(bind *spec.Binding, kind bridge.Surface, file, path string) []Issue {
	hint := fmt.Sprintf(`bind {"doc":"<document or $reference>","surface":%q}`, kind)
	if bind == nil {
		return []Issue{finding(file, path, "kit/surface", "A surface needs a {doc, surface} binding.", hint)}
	}
	var issues []Issue
	if strings.TrimSpace(bind.Doc) == "" {
		issues = append(issues, finding(file, path+"/doc", "kit/surface", "A surface needs a document.", hint))
	}
	if bind.Surface != string(kind) {
		issues = append(issues, finding(file, path+"/surface", "kit/surface", fmt.Sprintf("Surface must match the part: expected %q, got %q.", kind, bind.Surface), hint))
	}
	if *bind != (spec.Binding{Doc: bind.Doc, Surface: bind.Surface}) {
		issues = append(issues, finding(file, path, "kit/surface", "A surface binding contains only doc and surface.", hint))
	}
	return issues
}

func (r *renderer) surface(n *spec.Node, path string, values map[string]any) {
	kind := surfaceKind(n, r.kit)
	if kind == "" || n.Bind == nil {
		return
	}
	value, err := resolveValue(n.Bind.Doc, values)
	doc, ok := value.(string)
	if err == nil && (!ok || strings.TrimSpace(doc) == "") {
		err = fmt.Errorf("document must resolve to a non-empty string, got %v", value)
	}
	if err != nil {
		r.issue(path+"/bind/doc", "render/surface", err, "bind doc to a literal or a sample record field containing the document path")
		return
	}
	props := map[string]any{}
	for _, key := range []string{"frame", "view"} {
		if value, exists := n.Props[key]; exists {
			props[key], err = resolveValue(value, values)
			if err != nil {
				return // The ordinary prop check reports the same resolution error.
			}
		}
	}
	if _, err := sampleSurfaceDoc(doc, kind, props); err != nil {
		r.issue(path, "render/surface", err, "use a supported surface frame or view and valid sample content")
	}
}

type surfaceSample struct {
	Doc   bridge.DocInfo
	Part  bridge.Part
	Files spec.Files
}

func sampleSurfaceDoc(doc string, kind bridge.Surface, props map[string]any) (surfaceSample, error) {
	part, text, err := sampleSurfacePart(kind, props)
	if err != nil {
		return surfaceSample{}, err
	}
	if err := validateSurfaceSample(kind, text); err != nil {
		return surfaceSample{}, fmt.Errorf("%s sample: %w", kind, err)
	}
	sample := surfaceSample{
		Doc:   bridge.DocInfo{Doc: doc, Surface: kind, Title: "Sample document", Parts: []bridge.PartSummary{{Part: part, Version: 1, Title: "Sample part"}}},
		Part:  bridge.Part{Doc: doc, Part: part, Surface: kind, Version: 1, Text: text},
		Files: spec.Files{part: []byte(text)},
	}
	order, err := json.Marshal(map[string]any{"title": sample.Doc.Title, "parts": []string{part}})
	if err != nil {
		return surfaceSample{}, err
	}
	sample.Files["doc.json"] = order
	switch kind {
	case bridge.SurfaceCanvas:
		sample.Part.Status = "Draft"
		sample.Doc.Parts[0].Status = "Draft"
		sample.Files["theme.json"] = []byte(`{}`)
	case bridge.SurfaceGrid:
		sample.Part.Extra = &bridge.PartExtra{Formulas: map[string]string{}}
		sample.Files["sheets/sample.formulas.json"] = []byte(`{}`)
	}
	return sample, nil
}

func sampleSurfacePart(kind bridge.Surface, props map[string]any) (string, string, error) {
	switch kind {
	case bridge.SurfaceText:
		return "section-1.md", "# Sample section\n\nSample text.\n", nil
	case bridge.SurfaceGrid:
		return "sheets/sample.csv", "Name,Value\nSample,1\n", nil
	case bridge.SurfaceCanvas:
		frame := sampleProp(props, "frame", "16:9")
		folder := "slides/"
		if frame == "board" {
			folder = "artboards/"
		}
		return folder + "1.json", fmt.Sprintf(`{"title":"Sample slide","status":"Draft","frame":%q,"notes":"","boxes":[{"id":"title","type":"text","x":0,"y":0,"w":1,"h":1,"text":"Sample slide"}]}`, frame), nil
	case bridge.SurfaceTimeline:
		return "plan/sample.json", fmt.Sprintf(`{"items":[{"id":"sample","title":"Sample item","start":"2026-10-13","end":"2026-10-14"}],"view":%q}`, sampleProp(props, "view", "gantt")), nil
	case bridge.SurfaceGraph:
		return "graph/sample.json", fmt.Sprintf(`{"nodes":[{"id":"sample","label":"Sample node"}],"links":[],"layout":%q}`, sampleProp(props, "view", "free")), nil
	case bridge.SurfaceMap:
		return "places/sample.json", `{"type":"FeatureCollection","features":[{"type":"Feature","geometry":{"type":"Point","coordinates":[0,0]},"properties":{"label":"Sample place","kind":"place"}}]}`, nil
	default:
		return "", "", fmt.Errorf("unknown surface %q", kind)
	}
}

func sampleProp(props map[string]any, key, fallback string) string {
	if value, exists := props[key]; exists {
		return fmt.Sprint(value)
	}
	return fallback
}

// The parts package is a stub on this branch and exports no format validator.
// These checks validate only generated sample shapes from contract surfaces §2;
// they do not replace server-side validation of content writes.
func validateSurfaceSample(kind bridge.Surface, text string) error {
	if kind == bridge.SurfaceText {
		if strings.TrimSpace(text) == "" {
			return fmt.Errorf("sample Markdown is empty")
		}
		return nil
	}
	if kind == bridge.SurfaceGrid {
		rows, err := csv.NewReader(strings.NewReader(text)).ReadAll()
		if err != nil {
			return err
		}
		if len(rows) < 2 {
			return fmt.Errorf("sample CSV needs a header and a row")
		}
		return nil
	}
	var object map[string]any
	decoder := json.NewDecoder(strings.NewReader(text))
	if err := decoder.Decode(&object); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("sample must be one JSON object")
	}
	switch kind {
	case bridge.SurfaceCanvas:
		return validateCanvasSample(object)
	case bridge.SurfaceTimeline:
		return validateTimelineSample(object)
	case bridge.SurfaceGraph:
		return validateGraphSample(object)
	case bridge.SurfaceMap:
		return validateMapSample(object)
	default:
		return fmt.Errorf("unknown surface %q", kind)
	}
}

func validateCanvasSample(object map[string]any) error {
	if !sampleStrings(object, "title", "status", "frame", "notes") || !contains([]string{"Draft", "In review", "Final"}, fmt.Sprint(object["status"])) || !contains([]string{"16:9", "board"}, fmt.Sprint(object["frame"])) {
		return fmt.Errorf("sample Canvas needs title, status, frame and notes")
	}
	box, err := sampleItem(object, "boxes")
	if err != nil {
		return err
	}
	if !sampleStrings(box, "id", "type", "text") || box["type"] != "text" {
		return fmt.Errorf("sample Canvas needs a text box")
	}
	for _, key := range []string{"x", "y", "w", "h"} {
		value, ok := box[key].(float64)
		if !ok || value < 0 || ((key == "w" || key == "h") && value == 0) {
			return fmt.Errorf("sample box has invalid %s", key)
		}
	}
	// Generated boxes fit in a unit square at the origin, safely inside the
	// slide frame as well as the unbounded board, without defining editor units.
	if box["x"].(float64)+box["w"].(float64) > 1 || box["y"].(float64)+box["h"].(float64) > 1 {
		return fmt.Errorf("sample box extends outside its frame area")
	}
	return nil
}

func validateTimelineSample(object map[string]any) error {
	item, err := sampleItem(object, "items")
	if err != nil {
		return err
	}
	if !sampleStrings(item, "id", "title", "start", "end") || !contains([]string{"gantt", "calendar", "shifts"}, fmt.Sprint(object["view"])) {
		return fmt.Errorf("sample Timeline needs dated items and a view")
	}
	return nil
}

func validateGraphSample(object map[string]any) error {
	node, err := sampleItem(object, "nodes")
	if err != nil {
		return err
	}
	if _, ok := object["links"].([]any); !ok || !sampleStrings(node, "id", "label") || !contains([]string{"tree", "flow", "free"}, fmt.Sprint(object["layout"])) {
		return fmt.Errorf("sample Graph needs nodes, links and a layout")
	}
	return nil
}

func sampleStrings(object map[string]any, fields ...string) bool {
	for _, field := range fields {
		if _, ok := object[field].(string); !ok {
			return false
		}
	}
	return true
}

func sampleItem(object map[string]any, field string) (map[string]any, error) {
	items, ok := object[field].([]any)
	if !ok || len(items) != 1 {
		return nil, fmt.Errorf("sample %s needs one item", field)
	}
	item, ok := items[0].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("sample %s item must be an object", field)
	}
	return item, nil
}

func validateMapSample(object map[string]any) error {
	feature, err := sampleItem(object, "features")
	if err != nil {
		return err
	}
	geometry, _ := feature["geometry"].(map[string]any)
	properties, _ := feature["properties"].(map[string]any)
	coordinates, _ := geometry["coordinates"].([]any)
	if object["type"] != "FeatureCollection" || feature["type"] != "Feature" || geometry["type"] != "Point" || !sampleStrings(properties, "label") || properties["kind"] != "place" || len(coordinates) != 2 {
		return fmt.Errorf("sample Map needs a labelled GeoJSON place")
	}
	for _, coordinate := range coordinates {
		if _, ok := coordinate.(float64); !ok {
			return fmt.Errorf("sample Map coordinates must be numbers")
		}
	}
	return nil
}
