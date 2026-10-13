package parts

import (
	"encoding/json"
	"fmt"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
)

type validator func(string, *bridge.PartExtra) error

var validators = map[bridge.Surface]validator{
	bridge.SurfaceCanvas: validateCanvas, bridge.SurfaceText: validateText,
	bridge.SurfaceGrid: validateGrid, bridge.SurfaceTimeline: validateTimeline,
	bridge.SurfaceGraph: validateGraph, bridge.SurfaceMap: validateMap,
}

func knownSurface(surface bridge.Surface) bool { _, ok := validators[surface]; return ok }
func validate(surface bridge.Surface, text string, extra *bridge.PartExtra) error {
	f, ok := validators[surface]
	if !ok {
		return invalid("unknown surface")
	}
	if surface != bridge.SurfaceGrid && extra != nil {
		return invalid("extra formulas are only supported for grid parts")
	}
	if err := f(text, extra); err != nil {
		return invalid(fmt.Sprintf("%s: %s", surface, err))
	}
	return nil
}
func decodeJSON(text string, v any) error {
	if !utf8.ValidString(text) {
		return fmt.Errorf("JSON must be valid UTF-8")
	}
	if err := json.Unmarshal([]byte(text), v); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	return nil
}
func oneOf(value string, values ...string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}
