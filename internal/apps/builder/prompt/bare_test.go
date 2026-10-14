package prompt

import (
	"errors"
	"strings"
	"testing"
)

const bareScreen = `{"screen":"vendors","title":"Vendors","nav":{"part":"SectionTabs"},"body":{"part":"Stack"}}`

func TestParseFilesForWrapsBareObject(t *testing.T) {
	tests := []struct {
		name, out, want string
		expect          []string
	}{
		{"single expected screen", bareScreen, "screens/vendors.json", []string{"screens/vendors.json"}},
		{"single expected screen, other id", bareScreen, "screens/suppliers.json", []string{"screens/suppliers.json"}},
		{"several expected, mapped by id", bareScreen, "screens/vendors.json", []string{"app.json", "screens/vendors.json"}},
		{"fix call, any path", bareScreen, "screens/vendors.json", nil},
		{"bare run becomes yaml", `{"run":"nudge","steps":[{"agent":"hi"}]}`, "runs/nudge.yaml", []string{"runs/nudge.yaml"}},
		{"bare app", `{"id":"x","records":{}}`, "app.json", []string{"app.json"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			files, err := ParseFilesFor(tt.out, tt.expect)
			if err != nil {
				t.Fatal(err)
			}
			if len(files) != 1 || files[tt.want] == nil {
				t.Fatalf("files = %v, want only %s", files, tt.want)
			}
		})
	}
	files, _ := ParseFilesFor(`{"run":"nudge","steps":[{"agent":"hi"}]}`, nil)
	if !strings.Contains(string(files["runs/nudge.yaml"]), "run: nudge") {
		t.Fatalf("run not YAML: %q", files["runs/nudge.yaml"])
	}
}

func TestParseFilesForKeepsPathMaps(t *testing.T) {
	files, err := ParseFilesFor(`{"screens/vendors.json":`+bareScreen+`}`, []string{"screens/vendors.json"})
	if err != nil || files["screens/vendors.json"] == nil {
		t.Fatalf("files=%v err=%v", files, err)
	}
}

func TestParseFilesForNamesExpectedShape(t *testing.T) {
	tests := []struct {
		name, out string
		expect    []string
		want      []string
	}{
		{"wrong kind", bareScreen, []string{"runs/nudge.yaml"},
			[]string{`reply must be {"runs/nudge.yaml": {…}}`, "you sent the screen object itself — wrap it under its path"}},
		{"ambiguous screen", bareScreen, []string{"screens/a.json", "screens/b.json"},
			[]string{`reply must be {"screens/a.json": {…}}`, "one key per file: screens/a.json, screens/b.json", "screen object itself"}},
		{"unknown object", `{"foo":1}`, []string{"app.json"}, []string{`reply must be {"app.json": {…}}`}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseFilesFor(tt.out, tt.expect)
			var shape *FilesObjectError
			if !errors.As(err, &shape) {
				t.Fatalf("err = %v, want FilesObjectError", err)
			}
			for _, w := range tt.want {
				if !strings.Contains(err.Error(), w) {
					t.Fatalf("error %q lacks %q", err, w)
				}
			}
		})
	}
}
