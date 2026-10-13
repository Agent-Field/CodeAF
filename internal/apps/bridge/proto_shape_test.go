package bridge

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestShapeJSONContract(t *testing.T) {
	tests := []struct {
		name  string
		value any
		wire  string
	}{
		{"draft", &Draft{}, `{"app":"crm","member":"me","base":12,"state":"ready","changes":[],"replayed":{"from":12,"to":13,"by":"raj","requests":2,"failed":0,"dismissed":false},"updated_at":"now"}`},
		{"change", &Change{}, `{"id":"c1","n":1,"request":"rename","from":"user","state":"passed","round":3,"steps":[{"step":"render","status":"done","log":"ok"}],"files":["app.json"],"summary":"renamed","check":{"pass":true,"round":3,"issues":[],"rendered":[],"runs":[]},"question":{"text":"Group?","options":[{"id":"drop","label":"Drop","sub":"keep the other change"}]},"error":"failed","at":"now"}`},
		{"step", &ShapeStep{}, `{"step":"plan","status":"running","log":"planning"}`},
		{"question", &ShapeQuestion{}, `{"text":"Group?","options":[{"id":"drop","label":"Drop","sub":"keep"}]}`},
		{"question option", &ShapeQuestionOption{}, `{"id":"stage","label":"By stage","sub":"Lead"}`},
		{"replay", &Replay{}, `{"from":12,"to":13,"by":"raj","requests":2,"failed":1,"dismissed":true}`},
		{"file diff", &FileDiff{}, `{"path":"app.json","op":"modify","before":"old","after":"new"}`},
		{"preview", &ApplyPreview{}, `{"from":12,"to":13,"changes":[{"n":1,"request":"rename"}],"audience":"only you","needs_changed":true,"needs":[{"kind":"records","line":"Read deals"}],"migration":[{"op":"rename","type":"deal","field":"value","to":"amount","affected":312,"line":"Rename Value","note":"alias kept"}],"records_checked":312}`},
		{"apply change", &ApplyChange{}, `{"n":1,"request":"rename"}`},
		{"migration", &MigrationLine{}, `{"op":"rename","type":"deal","field":"value","to":"amount","affected":312,"line":"Rename Value","note":"alias kept"}`},
		{"version", &Version{}, `{"n":12,"requests":["rename"],"by":"mona","at":"now","changes":1,"note":"kept","current":true,"restorable":false}`},
		{"health", &AppHealth{}, `{"version":13,"broken":{"version":13,"request":"add","error":"no date","file":"screens/pipeline.json","line":42,"affected":3,"last_good":12}}`},
		{"broken", &Broken{}, `{"version":13,"request":"add","error":"no date","file":"screen.json","line":42,"affected":3,"last_good":12}`},
		{"shape event", &ShapeEvent{}, `{"kind":"applied","draft":{"app":"crm","member":"me","base":12,"state":"applied","changes":[],"updated_at":"now"},"change":{"id":"c1","n":1,"request":"add","from":"user","state":"passed","round":1,"steps":[],"files":[],"at":"now"},"version":{"n":13,"requests":[],"by":"me","at":"now","changes":1,"current":true,"restorable":true},"at":"now"}`},
		{"version event", &VersionEvent{}, `{"version":13,"by":"me","request":"add","at":"now"}`},
		{"open params", &ShapeOpenParams{}, `{}`},
		{"get params", &ShapeGetParams{}, `{}`},
		{"get result", &ShapeGetResult{}, `{"draft":null}`},
		{"request params", &ShapeRequestParams{}, `{"text":"rename","from":"fix"}`},
		{"plan edit params", &ShapePlanEditParams{}, `{"section":"data","id":"deal","text":"rename"}`},
		{"fix params", &ShapeChangeFixParams{}, `{"change":"c1","hint":"try again"}`},
		{"cancel params", &ShapeChangeCancelParams{}, `{"change":"c1"}`},
		{"answer params", &ShapeAnswerParams{}, `{"change":"c1","option":"stage","text":"By stage"}`},
		{"diff params", &ShapeDiffParams{}, `{"change":"c1"}`},
		{"diff result", &ShapeDiffResult{}, `{"files":[{"path":"app.json","op":"delete","before":"old","after":""}]}`},
		{"preview params", &ShapePreviewParams{}, `{}`},
		{"apply params", &ShapeApplyParams{}, `{"base":12}`},
		{"discard params", &ShapeDiscardParams{}, `{}`},
		{"discard result", &ShapeDiscardResult{}, `{}`},
		{"dismiss params", &ShapeReplayDismissParams{}, `{}`},
		{"ask params", &ShapeAskParams{}, `{"text":"add column"}`},
		{"ask result", &ShapeAskResult{}, `{"to":"mona"}`},
		{"versions list params", &VersionsListParams{}, `{}`},
		{"versions list result", &VersionsListResult{}, `{"versions":[]}`},
		{"versions diff params", &VersionsDiffParams{}, `{"version":12}`},
		{"versions diff result", &VersionsDiffResult{}, `{"files":[]}`},
		{"restore params", &VersionsRestoreParams{}, `{"version":12,"confirm":true}`},
		{"health params", &AppsHealthParams{}, `{}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := json.Unmarshal([]byte(tt.wire), tt.value); err != nil {
				t.Fatal(err)
			}
			got, err := json.Marshal(tt.value)
			if err != nil {
				t.Fatal(err)
			}
			var wantJSON, gotJSON any
			if err := json.Unmarshal([]byte(tt.wire), &wantJSON); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(got, &gotJSON); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(wantJSON, gotJSON) {
				t.Fatalf("wire changed: got %s, want %s", got, tt.wire)
			}
		})
	}
}

func TestShapeOptionalJSONFields(t *testing.T) {
	tests := []struct {
		value  any
		absent []string
	}{
		{Draft{}, []string{"replayed"}},
		{Change{}, []string{"summary", "check", "question", "error"}},
		{ShapeQuestionOption{}, []string{"sub"}},
		{MigrationLine{}, []string{"to"}},
		{Version{}, []string{"note"}},
		{AppHealth{}, []string{"broken"}},
		{Broken{}, []string{"line"}},
		{ShapeEvent{}, []string{"draft", "change", "version"}},
		{ShapeRequestParams{}, []string{"from"}},
		{ShapeAnswerParams{}, []string{"option", "text"}},
		{ShapeDiffParams{}, []string{"change"}},
		{VersionsRestoreParams{}, []string{"confirm"}},
	}
	for _, tt := range tests {
		t.Run(reflect.TypeOf(tt.value).Name(), func(t *testing.T) {
			raw, err := json.Marshal(tt.value)
			if err != nil {
				t.Fatal(err)
			}
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(raw, &fields); err != nil {
				t.Fatal(err)
			}
			for _, key := range tt.absent {
				if _, ok := fields[key]; ok {
					t.Fatalf("optional %s present in %s", key, raw)
				}
			}
		})
	}
}

func TestShapeTopics(t *testing.T) {
	for _, tt := range []struct{ got, want string }{
		{TopicShape("crm", "mona"), "app/crm/shape/mona"},
		{TopicVersion("crm"), "app/crm/version"},
		{TopicHealth("crm"), "app/crm/health"},
	} {
		if tt.got != tt.want {
			t.Fatalf("topic = %s, want %s", tt.got, tt.want)
		}
	}
}
