package check

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/draft"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
)

// draftBase builds, through the draft verbs, a small app both sides can judge.
func draftBase(t *testing.T) *draft.Store {
	t.Helper()
	s, err := draft.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	steps := [][]string{
		{"name", "Deals"}, {"plan data", "deal", "Deal · name"}, {"plan screen", "s", "S"}, {"plan agent", "Agent"}, {"plan ready"},
		{"record", "deal", "--title", "name"},
		{"field", "deal", "name", "text"}, {"field", "deal", "stage", "text"}, {"field", "deal", "amount", "number"},
	}
	for _, step := range steps {
		if _, err := s.Append(draft.AuthorBuilder, step[0], step[1:], ""); err != nil {
			t.Fatalf("%v: %v", step, err)
		}
	}
	return s
}

func screenWithWhen(t *testing.T, when string) string {
	t.Helper()
	list := map[string]any{"part": "RecordList", "id": "deals", "label": "Deals", "bind": map[string]any{"records": "deal"}, "props": map[string]any{"columns": []string{"name"}}}
	text := map[string]any{"part": "Text", "props": map[string]any{"text": "x"}, "when": when}
	doc := map[string]any{"$schema": spec.ScreenSchemaURL, "screen": "s", "title": "S", "body": map[string]any{"part": "Stack", "children": []any{list, text}}}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

var verdictExpressions = []struct {
	name, source string
	boolean      bool // a screen condition must yield a bool
}{
	{"now minus days token", `$now - 7d`, false},
	{"days function", `days(3)`, false},
	{"field compare", `amount > 3`, true},
	{"string compare", `stage == 'Lead'`, true},
	{"duration compare", `days(3) > days(1)`, true},
	{"now compare", `$now > $now - 7d`, true},
	{"selection count", `$selection.deals.count == 1`, true},
	{"typo in field", `amout > 3`, true},
	{"unknown function", `frobnicate(amount)`, true},
	{"unknown reference", `$selection.nope.count == 1`, true},
	{"bad syntax", `amount >`, true},
}

// The draft's verdict on an expression equals check's on the same rendered
// app, for computed fields and for screen conditions.
func TestDraftAndCheckAgreeOnExpressions(t *testing.T) {
	for _, tc := range verdictExpressions {
		t.Run("computed "+tc.name, func(t *testing.T) {
			s := draftBase(t)
			_, draftErr := s.Append(draft.AuthorBuilder, "field", []string{"deal", "x", "number", "--computed", tc.source}, "")
			st, _ := s.State()
			b, err := spec.Decode(draft.Render(st, draft.Defaults{AppID: "deals"}))
			if err != nil {
				t.Fatal(err)
			}
			b.App.Records["deal"].Fields["x"] = &spec.Field{Type: spec.FieldNumber, Computed: tc.source}
			if got := len(computedExpressions(b)) > 0; got != (draftErr != nil) {
				t.Fatalf("check refuses = %v, draft refuses = %v (%v)", got, draftErr != nil, draftErr)
			}
		})
		t.Run("when "+tc.name, func(t *testing.T) {
			s := draftBase(t)
			_, draftErr := s.Append(draft.AuthorBuilder, "screen", []string{"s"}, screenWithWhen(t, tc.source))
			st, _ := s.State()
			plain := draftBase(t)
			if _, err := plain.Append(draft.AuthorBuilder, "screen", []string{"s"}, screenWithWhen(t, "true")); err != nil {
				t.Fatal(err)
			}
			plainState, _ := plain.State()
			b, err := spec.Decode(draft.Render(plainState, draft.Defaults{AppID: "deals"}))
			if err != nil {
				t.Fatal(err)
			}
			_ = st
			var screen spec.Screen
			if err := json.Unmarshal([]byte(screenWithWhen(t, tc.source)), &screen); err != nil {
				t.Fatal(err)
			}
			kit, _ := spec.LoadKit()
			rows := map[string][]bridge.Record{"deal": {{ID: "d1", Version: 1, Fields: map[string]any{"name": "A", "stage": "Lead", "amount": float64(5)}}}}
			refused := false
			for _, issue := range render(context.Background(), b, kit, &screen, rows) {
				refused = refused || strings.HasPrefix(issue.Rule, "render/when")
			}
			if refused != (draftErr != nil) {
				t.Fatalf("check refuses = %v, draft refuses = %v (%v)", refused, draftErr != nil, draftErr)
			}
			if draftErr != nil && !strings.Contains(draftErr.Error(), "screens/s.json: /body/children/1/when: ") {
				t.Fatalf("error lacks the file and pointer: %v", draftErr)
			}
		})
	}
}
