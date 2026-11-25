package schema

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"strings"
)

// ScreenMarkup is the small XML-shaped screen syntax used by set-screen.
// Props and actions are JSON values inside attributes. Unquoted JSON values
// such as false become their JSON types, and bare words become strings.
type ScreenMarkup struct {
	Screen string
	Title  string
	Params []string
	Views  map[string]QueryIR
	Markup string
}

// CompileScreenMarkup compiles markup to the same checked tree as a recipe.
func CompileScreenMarkup(def Definition, existing TypeSet, input ScreenMarkup) ([]byte, error) {
	c, err := compilerFor(def, existing)
	if err != nil {
		return nil, err
	}
	if !fileIDPattern.MatchString(input.Screen) {
		return nil, fmt.Errorf("screen %q must be a slug", input.Screen)
	}
	s := Screen{Screen: input.Screen, Title: input.Title, Params: input.Params, Views: map[string]QueryIR{}}
	for id, query := range input.Views {
		s.Views[id] = query
	}
	decoder := xml.NewDecoder(strings.NewReader(input.Markup))
	var parents []*Node
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("screen markup: %w", err)
		}
		switch value := token.(type) {
		case xml.StartElement:
			if value.Name.Space != "" || len(parents) >= 64 {
				return nil, fmt.Errorf("screen markup needs unqualified parts and at most 64 nested nodes")
			}
			node, err := markupNode(value)
			if err != nil {
				return nil, err
			}
			parentID, ordinal := s.Screen, 0
			if len(parents) == 0 {
				if s.Body != nil {
					return nil, fmt.Errorf("screen markup needs one root part")
				}
				s.Body = node
			} else {
				parent := parents[len(parents)-1]
				parentID = parent.ID
				for _, child := range parent.Children {
					if child.Part == node.Part {
						ordinal++
					}
				}
				parent.Children = append(parent.Children, node)
			}
			if node.ID == "" {
				node.ID = nodeID(fmt.Sprintf("markup/%s/%s/%s/%d", s.Screen, parentID, node.Part, ordinal))
			}
			if b := node.Bind; b != nil && b.Records != "" && b.View == "" {
				b.View = node.ID
				s.Views[b.View] = QueryIR{Type: b.Records}
			}
			parents = append(parents, node)
		case xml.EndElement:
			parents = parents[:len(parents)-1]
		case xml.CharData:
			if strings.TrimSpace(string(value)) != "" {
				return nil, fmt.Errorf("screen markup puts visible text in a prop or label")
			}
		case xml.Comment:
		default:
			return nil, fmt.Errorf("screen markup accepts only parts and comments")
		}
	}
	if s.Body == nil {
		return nil, fmt.Errorf("screen markup needs a root part")
	}
	return c.compileScreen(s)
}

func markupNode(element xml.StartElement) (*Node, error) {
	name := element.Name.Local
	node := &Node{Part: name, Props: map[string]any{}, On: map[string][]Action{}}
	bind := map[string]any{}
	seen := map[string]bool{}
	for _, a := range element.Attr {
		key := a.Name.Local
		if a.Name.Space != "" || seen[key] {
			return nil, fmt.Errorf("invalid or duplicate %s attribute on %s", key, name)
		}
		seen[key] = true
		switch key {
		case "id":
			node.ID = a.Value
		case "label":
			node.Label = a.Value
		case "when":
			node.When = a.Value
		case "slot":
			node.Slot = a.Value
		case "part":
			// App-local entries have a slash in their names, which XML tag
			// names cannot express. A Part wrapper names the same entry.
			if name != "Part" {
				return nil, fmt.Errorf("the part attribute belongs on a Part wrapper")
			}
			node.Part = a.Value
		default:
			prefix, property, ok := strings.Cut(key, "-")
			if !ok || property == "" {
				return nil, fmt.Errorf("unknown %s attribute on %s", key, name)
			}
			switch prefix {
			case "prop":
				node.Props[property] = markupValue(a.Value)
			case "bind":
				bind[property] = markupValue(a.Value)
			case "on":
				var actions []Action
				if err := json.Unmarshal([]byte(a.Value), &actions); err != nil {
					return nil, fmt.Errorf("%s: %w", key, err)
				}
				node.On[property] = actions
			default:
				return nil, fmt.Errorf("unknown %s attribute on %s", key, name)
			}
		}
	}
	if len(bind) > 0 {
		raw, _ := json.Marshal(bind)
		node.Bind = &Bind{}
		dec := json.NewDecoder(strings.NewReader(string(raw)))
		dec.DisallowUnknownFields()
		if err := dec.Decode(node.Bind); err != nil {
			return nil, fmt.Errorf("%s binding: %w", name, err)
		}
	}
	return node, nil
}

func markupValue(raw string) any {
	var v any
	if json.Unmarshal([]byte(raw), &v) != nil {
		return raw
	}
	return v
}
