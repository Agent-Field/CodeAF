package appwire

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/skillpick"
)

// SkillsParams identifies the conversation whose shelf is read.
type SkillsParams struct {
	Agent string `json:"agent"`
}

// SkillSetParams asks for a state rather than toggling an unknown replica.
type SkillSetParams struct {
	Agent string `json:"agent"`
	Name  string `json:"name"`
	On    bool   `json:"on"`
}

// skillSetWire distinguishes an omitted on from an explicit false.
type skillSetWire struct {
	Agent string `json:"agent"`
	Name  string `json:"name"`
	On    *bool  `json:"on"`
}

// SkillState is the state read back from the conversation after a set.
type SkillState struct {
	Name string `json:"name"`
	On   bool   `json:"on"`
}

// SkillShelf is the optional engine capability behind the two methods.
type SkillShelf interface {
	ListSkills(agent string) ([]skillpick.Row, error)
	SetSkill(SkillSetParams) (SkillState, error)
}

// SkillsChanged invalidates every view of one conversation's attachment.
type SkillsChanged struct {
	Agent string   `json:"agent"`
	Names []string `json:"names"`
}

// SkillNameMax bounds a skill name carried by a set request.
const SkillNameMax = 256

const EventSkillsChanged = "skills.changed"

func init() {
	methods["skills.list"] = typed(func(e Engine, p SkillsParams) (*[]skillpick.Row, error) {
		shelf, ok := e.(SkillShelf)
		if !ok {
			return nil, Errorf(CodeUnknownMethod, "this engine does not serve skills.list")
		}
		if err := checkActionAgent(p.Agent); err != nil {
			return nil, err
		}
		rows, err := shelf.ListSkills(p.Agent)
		rows = nonNil(rows)
		return &rows, err
	})
	methods["skills.set"] = shaped[SkillSetParams](func(e Engine, raw json.RawMessage) (*SkillState, error) {
		shelf, ok := e.(SkillShelf)
		if !ok {
			return nil, Errorf(CodeUnknownMethod, "this engine does not serve skills.set")
		}
		var p skillSetWire
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&p); err != nil {
			return nil, Errorf(CodeBadParams, "the params could not be read: %v", err)
		}
		if err := checkActionAgent(p.Agent); err != nil {
			return nil, err
		}
		name := strings.TrimSpace(p.Name)
		if !utf8.ValidString(name) || !plainText(name, SkillNameMax) || p.On == nil {
			return nil, Errorf(CodeBadParams, "name must be 1–%d bytes on one line, and on must be a boolean", SkillNameMax)
		}
		state, err := shelf.SetSkill(SkillSetParams{Agent: p.Agent, Name: name, On: *p.On})
		return &state, err
	})
	eventShapes[EventSkillsChanged] = reflect.TypeFor[SkillsChanged]()
}

func withSkillsFeatures(hello Hello, e Engine) Hello {
	if _, ok := e.(SkillShelf); !ok {
		return hello
	}
	return withFeature(withFeature(hello, "skills.list"), "skills.set")
}
