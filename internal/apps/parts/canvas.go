package parts

import (
	"fmt"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
)

const frameWidth = 1280
const frameHeight = 720

type canvasBody struct {
	Title  *string     `json:"title"`
	Status string      `json:"status"`
	Frame  string      `json:"frame"`
	Notes  string      `json:"notes"`
	Boxes  []canvasBox `json:"boxes"`
}
type canvasBox struct {
	ID    string   `json:"id"`
	Type  string   `json:"type"`
	X     *float64 `json:"x"`
	Y     *float64 `json:"y"`
	W     *float64 `json:"w"`
	H     *float64 `json:"h"`
	Text  string   `json:"text"`
	Src   string   `json:"src"`
	Style *struct {
		Tone string `json:"tone"`
		Font string `json:"font"`
	} `json:"style"`
}

func validateCanvas(text string, _ *bridge.PartExtra) error {
	var body canvasBody
	if err := decodeJSON(text, &body); err != nil {
		return err
	}
	if body.Title == nil || body.Boxes == nil || !oneOf(body.Frame, "16:9", "board") {
		return fmt.Errorf("needs title, frame (16:9 or board), and boxes array")
	}
	if body.Status != "" && !oneOf(body.Status, "Draft", "In review", "Final") {
		return fmt.Errorf("status must be Draft, In review or Final")
	}
	seen := map[string]bool{}
	for _, box := range body.Boxes {
		if seen[box.ID] {
			return fmt.Errorf("duplicate box id %q", box.ID)
		}
		seen[box.ID] = true
		if err := checkBox(box, body.Frame); err != nil {
			return err
		}
	}
	return nil
}
func checkBox(b canvasBox, frame string) error {
	if b.ID == "" || !oneOf(b.Type, "text", "image", "shape", "chart", "record", "media") {
		return fmt.Errorf("box needs id and a supported type")
	}
	if b.X == nil || b.Y == nil || b.W == nil || b.H == nil {
		return fmt.Errorf("box %q needs numeric x, y, w, h", b.ID)
	}
	if *b.W <= 0 || *b.H <= 0 {
		return fmt.Errorf("box %q width and height must be positive", b.ID)
	}
	if frame == "16:9" && (*b.X < 0 || *b.Y < 0 || *b.X+*b.W > frameWidth || *b.Y+*b.H > frameHeight) {
		return fmt.Errorf("box %q must fit inside the 1280×720 frame", b.ID)
	}
	if b.Style != nil && b.Style.Font != "" && !oneOf(b.Style.Font, "heading", "body") {
		return fmt.Errorf("box %q font must be heading or body", b.ID)
	}
	return nil
}
