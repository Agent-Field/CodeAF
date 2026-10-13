package parts

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
)

type geometry struct {
	Type        string          `json:"type"`
	Coordinates json.RawMessage `json:"coordinates"`
	Geometries  []geometry      `json:"geometries"`
}

type mapFeature struct {
	Type       string          `json:"type"`
	Geometry   json.RawMessage `json:"geometry"`
	Properties *struct {
		Label *string `json:"label"`
		Kind  string  `json:"kind"`
	} `json:"properties"`
}

func validateMap(text string, _ *bridge.PartExtra) error {
	var body struct {
		Type     string       `json:"type"`
		Features []mapFeature `json:"features"`
	}
	if err := decodeJSON(text, &body); err != nil {
		return err
	}
	if body.Type != "FeatureCollection" || body.Features == nil {
		return fmt.Errorf("needs a GeoJSON FeatureCollection with a features array")
	}
	for _, feature := range body.Features {
		if err := checkFeature(feature); err != nil {
			return err
		}
	}
	return nil
}
func checkFeature(feature mapFeature) error {
	if feature.Type != "Feature" {
		return fmt.Errorf("every feature must have type Feature")
	}
	if feature.Properties == nil || feature.Properties.Label == nil || !oneOf(feature.Properties.Kind, "place", "route", "live") {
		return fmt.Errorf("feature properties need label and kind (place, route or live)")
	}
	if len(feature.Geometry) == 0 {
		return fmt.Errorf("feature needs geometry (or null)")
	}
	if strings.TrimSpace(string(feature.Geometry)) == "null" {
		return nil
	}
	var g geometry
	if err := json.Unmarshal(feature.Geometry, &g); err != nil {
		return fmt.Errorf("invalid geometry: %w", err)
	}
	return checkGeometry(g)
}
func checkGeometry(g geometry) error {
	if g.Type == "GeometryCollection" {
		if g.Geometries == nil {
			return fmt.Errorf("GeometryCollection needs geometries array")
		}
		for _, child := range g.Geometries {
			if err := checkGeometry(child); err != nil {
				return err
			}
		}
		return nil
	}
	depths := map[string]int{"Point": 0, "MultiPoint": 1, "LineString": 1, "MultiLineString": 2, "Polygon": 2, "MultiPolygon": 3}
	depth, ok := depths[g.Type]
	if !ok {
		return fmt.Errorf("unsupported GeoJSON geometry type %q", g.Type)
	}
	var coordinates any
	if err := json.Unmarshal(g.Coordinates, &coordinates); err != nil {
		return fmt.Errorf("geometry needs coordinates")
	}
	if !coordinateShape(coordinates, depth) {
		return fmt.Errorf("invalid %s coordinates", g.Type)
	}
	if !coordinateTopology(g.Type, coordinates) {
		return fmt.Errorf("invalid %s line or polygon", g.Type)
	}
	return nil
}
func coordinateShape(value any, depth int) bool {
	list, ok := value.([]any)
	if !ok || len(list) == 0 {
		return false
	}
	if depth == 0 {
		if len(list) < 2 {
			return false
		}
		for _, n := range list {
			if _, ok := n.(float64); !ok {
				return false
			}
		}
		return true
	}
	for _, v := range list {
		if !coordinateShape(v, depth-1) {
			return false
		}
	}
	return true
}

func coordinateTopology(kind string, value any) bool {
	list := value.([]any)
	switch kind {
	case "LineString":
		return len(list) >= 2
	case "MultiLineString":
		for _, line := range list {
			if len(line.([]any)) < 2 {
				return false
			}
		}
	case "Polygon":
		return polygonRings(list)
	case "MultiPolygon":
		for _, polygon := range list {
			if !polygonRings(polygon.([]any)) {
				return false
			}
		}
	}
	return true
}
func polygonRings(rings []any) bool {
	for _, value := range rings {
		ring := value.([]any)
		if len(ring) < 4 || !samePosition(ring[0].([]any), ring[len(ring)-1].([]any)) {
			return false
		}
	}
	return true
}
func samePosition(a, b []any) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
