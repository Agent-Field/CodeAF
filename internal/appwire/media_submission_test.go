package appwire

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
)

// composedEngine is an Engine that is a ComposedSender.
type composedEngine struct {
	fakeEngine
	agent string
	heard AgentMessage
	back  TurnReceipt
}

func (c *composedEngine) SendComposed(agent string, message AgentMessage) (TurnReceipt, error) {
	c.agent, c.heard = agent, message
	return c.back, nil
}

const goodSubmission = `{"id":"a1","text":"redo","attachments":["at1","at2"],"submission":{"media":[{"id":"at1","source":{"source_id":"src_1","at_ms":3000}},{"id":"at2"}],
"annotation":{"original":"at1","marked":"at2","marks_path":"/w/m.json","change_only_marked":true,
"marks":[{"kind":"pin","note":"warmer","at":{"x":24,"y":18}},{"kind":"box","from":{"x":70,"y":56},"to":{"x":82,"y":76}},{"kind":"shade","shade":{"points":[{"x":1,"y":2}],"brush_width":4,"area_percent":6}}]}}}`

func TestAgentsSendCarriesASubmissionAndAnswersTheReceipt(t *testing.T) {
	eng := &composedEngine{back: TurnReceipt{Turn: 4, MessageID: "msg_x", Delivery: DeliveryDescription, SeenBy: "v/seer", Queued: true}}
	out, err := methods["agents.send"].call(eng, json.RawMessage(goodSubmission))
	if err != nil {
		t.Fatalf("agents.send: %v", err)
	}
	raw, _ := json.Marshal(out)
	for _, want := range []string{`"turn":4`, `"message_id":"msg_x"`, `"delivery":"description"`, `"seen_by":"v/seer"`, `"queued":true`} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("result %s lacks %s", raw, want)
		}
	}
	got := eng.heard.Submission
	if got == nil || len(got.Media) != 2 || got.Annotation == nil || len(got.Annotation.Marks) != 3 ||
		got.Media[0].Source == nil || *got.Media[0].Source.AtMS != 3000 || !got.Annotation.ChangeOnlyMarked {
		t.Fatalf("engine heard %+v", eng.heard)
	}
}

func TestASubmissionToAnEngineWithoutADoorIsRefusedUnknownMethod(t *testing.T) {
	_, err := methods["agents.send"].call(&sendEngine{}, json.RawMessage(goodSubmission))
	if codeOf(err) != CodeUnknownMethod {
		t.Fatalf("answered %v, want unknown_method", err)
	}
}

func TestAnInvalidSubmissionIsBadParamsBeforeTheEngineHearsOfIt(t *testing.T) {
	cases := map[string]func(*MessageSubmission){
		"no pictures":          func(s *MessageSubmission) { s.Media = nil },
		"unstaged picture":     func(s *MessageSubmission) { s.Media[0].ID = "at9" },
		"picture twice":        func(s *MessageSubmission) { s.Media[1].ID = "at1" },
		"original not a media": func(s *MessageSubmission) { s.Annotation.Original = "at9" },
		"marked is original":   func(s *MessageSubmission) { s.Annotation.Marked = "at1" },
		"no marks":             func(s *MessageSubmission) { s.Annotation.Marks = nil },
		"nan":                  func(s *MessageSubmission) { s.Annotation.Marks[0].At.X = math.NaN() },
		"over 100":             func(s *MessageSubmission) { s.Annotation.Marks[0].At.Y = 101 },
		"inverted box":         func(s *MessageSubmission) { s.Annotation.Marks[1].To.X = 1 },
		"unknown kind":         func(s *MessageSubmission) { s.Annotation.Marks[0].Kind = "arrow" },
		"shade without brush":  func(s *MessageSubmission) { s.Annotation.Marks[2].Shade.BrushWidth = 0 },
		"note too long":        func(s *MessageSubmission) { s.Annotation.Marks[0].Note = strings.Repeat("x", MaxSubmissionNote+1) },
		"too many marks": func(s *MessageSubmission) {
			s.Annotation.Marks = make([]AnnotationMark, MaxSubmissionMarks+1)
			for i := range s.Annotation.Marks {
				s.Annotation.Marks[i] = AnnotationMark{Kind: "pin", At: &Point{}}
			}
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			var p struct {
				Submission MessageSubmission `json:"submission"`
			}
			if err := json.Unmarshal([]byte(goodSubmission), &p); err != nil {
				t.Fatal(err)
			}
			mutate(&p.Submission)
			if err := p.Submission.Check([]string{"at1", "at2"}); codeOf(err) != CodeBadParams {
				t.Fatalf("Check answered %v, want bad_params", err)
			}
		})
	}
	// Through the method, the engine is never reached.
	eng := &composedEngine{}
	bad := strings.Replace(goodSubmission, `"x":24`, `"x":240`, 1)
	if _, err := methods["agents.send"].call(eng, json.RawMessage(bad)); codeOf(err) != CodeBadParams || eng.agent != "" {
		t.Fatalf("answered %v, engine heard %q", err, eng.agent)
	}
}

func TestThePathsOtherDoorsRefuseASubmission(t *testing.T) {
	sub := `"submission":{"media":[{"id":"at1"}]}`
	for name, call := range map[string]struct{ method, params string }{
		"steer":      {"agents.steer", `{"id":"a1","text":"x",` + sub + `}`},
		"queue":      {"agents.queue", `{"id":"a1","text":"x",` + sub + `}`},
		"queue.edit": {"queue.edit", `{"agent":"a1","id":"q1","text":"x",` + sub + `}`},
		"crew":       {"crews.message", `{"crew":"c1","text":"x",` + sub + `}`},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := methods[call.method].call(&sendEngine{}, json.RawMessage(call.params)); codeOf(err) != CodeBadParams {
				t.Fatalf("%s answered %v, want bad_params", call.method, err)
			}
		})
	}
}

func TestHelloAdvertisesPicturesOnlyForAnEngineThatTakesThem(t *testing.T) {
	has := func(h Hello) bool {
		for _, f := range h.Features {
			if f == FeatureMessageMedia {
				return true
			}
		}
		return false
	}
	if !has(withMessageReadsFeature(Hello{}, &composedEngine{})) || has(withMessageReadsFeature(Hello{}, &sendEngine{})) {
		t.Fatal("the messages.media feature does not follow the engine's door")
	}
}
