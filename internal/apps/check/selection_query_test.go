package check

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

// A master/detail screen lists the selected row's related records. Its query
// has no value until one row is selected: inside the selection's Panel that is
// correct, outside it the finding says how to guard the list.
func TestRelatedRowsOfTheSelectedRecord(t *testing.T) {
	related := func() *spec.Node {
		return &spec.Node{Part: "RecordList", ID: "their-deals", Label: "Deals", Bind: &spec.Binding{Records: "deal", Query: "contacts:{{selection.contacts.id}} sort:name"}, Props: map[string]any{"columns": []any{"name"}}}
	}
	for name, tc := range map[string]struct {
		place func(*spec.Screen)
		path  string
	}{
		"inside the selection panel": {place: func(s *spec.Screen) { s.Body.Children[1].Children = append(s.Body.Children[1].Children, related()) }},
		"always on screen":           {place: func(s *spec.Screen) { s.Body.Children = append(s.Body.Children, related()) }, path: "/body/children/2/bind/query"},
	} {
		t.Run(name, func(t *testing.T) {
			files, sample := fixture(t)
			bundle, err := spec.Decode(files)
			if err != nil {
				t.Fatal(err)
			}
			tc.place(bundle.Screens["contacts"])
			if files["screens/contacts.json"], err = json.Marshal(bundle.Screens["contacts"]); err != nil {
				t.Fatal(err)
			}
			report, err := New(svc.Config{}, services(sample)).Run(context.Background(), "crm", files, 1)
			if err != nil {
				t.Fatal(err)
			}
			if tc.path == "" {
				if !report.Pass {
					t.Fatalf("guarded related list rejected: %+v", report.Issues)
				}
				return
			}
			for _, issue := range report.Issues {
				if issue.Path == tc.path && strings.Contains(issue.String(), `$selection.contacts.count == 1`) {
					return
				}
			}
			t.Fatalf("no guard hint at %s: %+v", tc.path, report.Issues)
		})
	}
}

func TestAReservedFieldIdIsNamedNotTheExpression(t *testing.T) {
	hint := computedHint("shoot", &spec.RecordType{}, errors.New("overlapping identifier for name 'type'"))
	for _, want := range []string{`field id "type"`, "reserved word", "shoot_type"} {
		if !strings.Contains(hint, want) {
			t.Errorf("hint lacks %q: %s", want, hint)
		}
	}
}
