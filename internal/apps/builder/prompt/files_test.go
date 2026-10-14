package prompt

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestParseFilesAcceptsModelHabits(t *testing.T) {
	const obj = `{"app.json":{"id":"x"},"agent.md":"hi\n","runs/a.yaml":"run: a\nsteps: []\n"}`
	tests := []struct {
		name, out string
	}{
		{"bare", obj},
		{"fenced", "```json\n" + obj + "\n```"},
		{"leading prose", "Here are the files {as asked}:\n" + obj + "\nDone."},
		{"prose with braces then fence", "I will use {kit} parts.\n```\n" + obj + "\n```\nHope it helps."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			files, err := ParseFiles(tt.out)
			if err != nil {
				t.Fatal(err)
			}
			if len(files) != 3 || !strings.Contains(string(files["app.json"]), `"id": "x"`) || string(files["agent.md"]) != "hi\n" {
				t.Errorf("files = %v", files)
			}
		})
	}
}

func TestParseFilesAcceptsJSONAsEscapedString(t *testing.T) {
	files, err := ParseFiles(`{"app.json":"{\"id\":\"x\"}"}`)
	if err != nil || string(files["app.json"]) != `{"id":"x"}` {
		t.Fatalf("files=%v err=%v", files, err)
	}
}

func TestParseFilesRejects(t *testing.T) {
	tests := []struct{ name, out, want string }{
		{"no object", "sorry, I cannot", "got keys []"},
		{"empty object", `{}`, "got keys []"},
		{"outside path", `{"../app.json":"{}"}`, "../app.json"},
		{"absolute", `{"/etc/passwd":"x"}`, "/etc/passwd"},
		{"custom tsx", `{"custom/x.tsx":"x"}`, "custom/x.tsx"},
		{"nested screen", `{"screens/a/b.json":{}}`, "screens/a/b.json"},
		{"wrong ext", `{"screens/a.yaml":"x"}`, "screens/a.yaml"},
		{"migration", `{"migrations/0001.json":{}}`, "migrations/0001.json"},
		{"one bad among good", `{"app.json":{},"notes.txt":"x"}`, "notes.txt"},
		{"bad json string", `{"app.json":"{nope"}`, "not valid JSON"},
		{"bad yaml", `{"runs/a.yaml":"a: [1"}`, "not valid YAML"},
		{"md object", `{"agent.md":{"a":1}}`, "must be a string"},
		{"array content", `{"app.json":[1]}`, "must be a string"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseFiles(tt.out)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestParseFilesChoosesFilesObject(t *testing.T) {
	const files = `{"agent.md":"hello"}`
	tests := []struct{ name, output string }{
		{"unrelated", `note {"x":1} then ` + files},
		{"inline app body", `{"budget":{"month":20},"records":{}} then ` + files},
		{"plan fragment", `{"data":[{"id":"lot","line":"Lots"}]} then ` + files},
		{"mixed paths", `{"app.json":{},"notes.txt":"x"} then ` + files},
		{"wrapper", `{"files":` + files + `,"explanation":"done"}`},
		{"prefer wrapper", `{"agent.md":"earlier"} then {"files":` + files + `}`},
		{"invalid wrapper", `{"files":{"budget":{}}} then ` + files},
		{"first map", files + ` then {"agent.md":"later"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseFiles(tt.output)
			if err != nil || len(got) != 1 || string(got["agent.md"]) != "hello" {
				t.Fatalf("files=%v err=%v", got, err)
			}
		})
	}
}

func TestParseFilesMissingObjectReturnsCorrection(t *testing.T) {
	tests := []struct {
		name, output string
		keys         []string
	}{
		{"garbage", "not JSON {broken", []string{}},
		{"inline definition", `{"records":{},"budget":{}}`, []string{"budget", "records"}},
		{"several objects", `{"z":1} then {"a":2,"z":3}`, []string{"a", "z"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseFiles(tt.output)
			var correction *FilesObjectError
			if !errors.As(err, &correction) || !reflect.DeepEqual(correction.Keys, tt.keys) {
				t.Fatalf("err = %v, want correction with keys %v", err, tt.keys)
			}
			if !strings.Contains(err.Error(), "return a JSON object mapping app file paths to contents; got keys") {
				t.Fatalf("missing repair instruction: %v", err)
			}
		})
	}
}

func TestParseFilesDoesNotHideInvalidSelectedContent(t *testing.T) {
	for _, output := range []string{
		`{"app.json":"{broken"} then {"app.json":{}}`,
		`{"app.json":{}} then {"files":{"app.json":"{broken"}}`,
	} {
		if _, err := ParseFiles(output); err == nil || !strings.Contains(err.Error(), "not valid JSON") {
			t.Fatalf("err = %v", err)
		}
	}
}

func TestValidPath(t *testing.T) {
	good := []string{"app.json", "plan.json", "agent.md", "screens/deals.json", "runs/follow-up.yaml", "screens/a_1.json"}
	bad := []string{"", "App.json", "screens/.json", "screens/../app.json", "runs/x.yml", "agent.md/", "screens/deals.json\n", "content/a.md"}
	for _, p := range good {
		if !ValidPath(p) {
			t.Errorf("%q rejected", p)
		}
	}
	for _, p := range bad {
		if ValidPath(p) {
			t.Errorf("%q accepted", p)
		}
	}
}

// The few-shot reply is what the model imitates, so it must itself pass the
// parser, and its plan must pass the plan parser.
func TestExampleIsValid(t *testing.T) {
	ex, err := loadExample()
	if err != nil {
		t.Fatal(err)
	}
	files, err := ParseFiles(ex.Files)
	if err != nil {
		t.Fatalf("example reply: %v", err)
	}
	if len(files) != len(exampleFiles) {
		t.Errorf("parsed %d files, want %d", len(files), len(exampleFiles))
	}
	plan, err := ParsePlan(ex.Plan)
	if err != nil {
		t.Fatalf("example plan: %v", err)
	}
	for _, s := range plan.Screens {
		if _, ok := files["screens/"+s.ID+".json"]; !ok {
			t.Errorf("plan screen %q has no file", s.ID)
		}
	}
	for _, r := range plan.Automation {
		if _, ok := files["runs/"+r.ID+".yaml"]; !ok {
			t.Errorf("plan run %q has no file", r.ID)
		}
	}
}
