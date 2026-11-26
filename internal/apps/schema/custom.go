package schema

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/extensions"
)

// PartOrigin pins the app and version that first published a Kit part.
type PartOrigin = extensions.Origin

// PartDigest excludes lineage so the same immutable revision is identified
// in the original app, the Kit, and the app that imports it.
func PartDigest(manifest, source []byte) (string, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(manifest, &fields); err != nil {
		return "", err
	}
	delete(fields, "origin")
	canonical, err := json.Marshal(fields)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", sha256.Sum256(append(canonical, source...))), nil
}

// PartElement is the compiled form of an app-authored presentation leaf. The desk
// receives data-only instructions and owns every element and event handler.
type PartElement struct {
	Kind  string `json:"kind"`
	Text  string `json:"text,omitempty"`
	Field string `json:"field,omitempty"`
	Event string `json:"event,omitempty"`
}

// PartProgram describes the authored presentation and its bridge contract.
type PartProgram struct {
	Manifest Part          `json:"manifest"`
	Elements []PartElement `json:"elements"`
}

func compilePart(p Part, source []byte) (PartProgram, error) {
	fail := func(message string) (PartProgram, error) { return PartProgram{}, errors.New(message) }
	if len(source) == 0 || len(source) > 8<<10 {
		return fail("Custom part source must be between 1 and 8192 bytes.")
	}
	if strings.TrimSpace(p.Reason) == "" {
		return fail("Explain which requirement the built-in parts cannot meet.")
	}
	if p.Interactive != (len(p.Events) > 0) {
		return fail("A custom part is interactive when it declares events.")
	}

	decoder := xml.NewDecoder(bytes.NewReader(source))
	depth := 0
	foundRoot := false
	elements := make([]PartElement, 0, 8)
	emits := map[string]bool{}
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fail("Use a single <part> with title, text, display, dot-cells or button elements.")
		}
		switch x := token.(type) {
		case xml.StartElement:
			if depth == 0 && !foundRoot && x.Name == (xml.Name{Local: "part"}) && len(x.Attr) == 0 {
				foundRoot = true
				depth++
				continue
			}
			if depth != 1 || x.Name.Space != "" || len(elements) == 12 {
				return fail("A custom part takes up to 12 direct children.")
			}
			element, err := compilePartElement(x, p)
			if err != nil {
				return PartProgram{}, err
			}
			emits[element.Event] = true
			elements = append(elements, element)
			depth++
		case xml.EndElement:
			depth--
		case xml.CharData:
			if len(bytes.TrimSpace(x)) != 0 {
				return fail("Use a text or field attribute for visible text.")
			}
		default:
			return fail("Custom source only accepts presentation elements.")
		}
	}
	if len(elements) == 0 || depth != 0 {
		return fail("Give the custom part a nonempty presentation.")
	}
	for _, event := range p.Events {
		if !emits[event] {
			return fail("Every declared event needs a matching button.")
		}
	}
	return PartProgram{Manifest: p, Elements: elements}, nil
}

func compilePartElement(x xml.StartElement, p Part) (PartElement, error) {
	el := PartElement{Kind: x.Name.Local}
	switch el.Kind {
	case "title", "text", "display", "dot-cells", "button":
	default:
		return el, fmt.Errorf("%q is not a custom presentation element", el.Kind)
	}
	seen := map[string]bool{}
	for _, attr := range x.Attr {
		if attr.Name.Space != "" || seen[attr.Name.Local] {
			return el, errors.New("Use each presentation attribute once, without a namespace.")
		}
		seen[attr.Name.Local] = true
		switch attr.Name.Local {
		case "text":
			el.Text = strings.TrimSpace(attr.Value)
			if len(el.Text) > 240 {
				return el, errors.New("A presentation label takes up to 240 bytes.")
			}
		case "field":
			el.Field = attr.Value
			if !slices.Contains(p.Inputs, el.Field) {
				return el, fmt.Errorf("field %q is missing from manifest inputs", el.Field)
			}
		case "event":
			el.Event = attr.Value
			if el.Kind != "button" || !slices.Contains(p.Events, el.Event) {
				return el, fmt.Errorf("event %q is unavailable on %s", el.Event, el.Kind)
			}
		default:
			return el, fmt.Errorf("%q is not a presentation attribute", attr.Name.Local)
		}
	}
	if (el.Text == "") == (el.Field == "") || (el.Kind == "button") != (el.Event != "") {
		return el, errors.New("Give each element text or a field. Give each button a declared event.")
	}
	return el, nil
}

func (c *checker) checkCustomPart(ctx actionCtx, n *Node, program PartProgram, at, typ string, query QueryIR) {
	if typ != "" {
		fields, _ := c.typeFields(typ)
		for _, name := range program.Manifest.Inputs {
			if !systemFields[name] {
				if _, ok := fields[name]; !ok {
					c.add(ctx.file, at+"/bind", CodeBindNotAccepted, "The custom part needs the %q record field.", name)
					continue
				}
			}
			if len(query.Select) != 0 && !slices.Contains(query.Select, name) {
				c.add(ctx.file, at+"/bind/view", CodeBindNotAccepted, "The custom part's query must select %q.", name)
			}
		}
		if len(query.Group) != 0 || len(query.Aggregate) != 0 || query.Relate != nil || query.Window != nil && len(query.Window.OrderBy) > 1 {
			c.add(ctx.file, at+"/bind/view", CodeBindNotAccepted, "A custom part needs a query that returns individual records.")
		}
	}
	for _, event := range program.Manifest.Events {
		if len(n.On[event]) != 1 {
			c.add(ctx.file, at+"/on/"+event, CodeEventUnknown, "Map the %q custom event to one registered Action.", event)
		}
	}
}

// ScreenParts compiles the custom programs used by one screen. Keep and
// Check use the same source compiler, and previews may still be in progress.
func ScreenParts(def Definition, screen []byte) (map[string]PartProgram, error) {
	view, err := ParseScreen(screen)
	if err != nil {
		return nil, err
	}
	programs := map[string]PartProgram{}
	c := checker{def: def}
	c.loadCustomParts()
	var walk func(*Node)
	walk = func(node *Node) {
		if node == nil {
			return
		}
		if program, ok := c.custom[node.Part]; ok {
			programs[node.Part] = program.program
		}
		for _, child := range node.Children {
			walk(child)
		}
	}
	walk(view.Nav)
	walk(view.Body)
	return programs, nil
}

// CustomPartBundle validates one complete app-authored part for authoring and
// Kit promotion, even before the builder has added a screen that binds it.
func CustomPartBundle(id string, manifest, source []byte) (PartProgram, error) {
	base := "custom/" + id
	if dir, ext, ok := fileInGroup(base + ".json"); dir != "custom" || ext != ".json" || !ok {
		return PartProgram{}, errors.New("Give the custom part a valid ID.")
	}
	c := checker{def: Definition{Files: map[string][]byte{base + ".json": manifest, base + ".tsx": source}}}
	c.loadCustomParts()
	if !c.report.OK() {
		return PartProgram{}, errors.New(c.report.String())
	}
	return c.custom[base].program, nil
}
