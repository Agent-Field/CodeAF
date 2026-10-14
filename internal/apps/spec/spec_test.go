package spec

import (
	"errors"
	"strings"
	"testing"
)

const golden = "testdata/crm"

func TestGoldenCRMLoads(t *testing.T) {
	b, err := Load(golden)
	if err != nil {
		t.Fatal(err)
	}
	if b.App.ID != "crm" || len(b.App.Records) != 3 {
		t.Fatalf("app = %+v", b.App)
	}
	if got := strings.Join(b.ScreenIDs(), ","); got != "contacts,deal,pipeline" {
		t.Fatalf("screens = %s", got)
	}
	if got := strings.Join(b.RunIDs(), ","); got != "follow-up" {
		t.Fatalf("runs = %s", got)
	}
	if b.Agent.Name != "CRM agent" || b.Agent.Body == "" {
		t.Fatalf("agent = %+v", b.Agent)
	}
	if b.App.Records["deal"].Fields["company"].To != "company" {
		t.Fatal("deal.company should relate to company")
	}
	if !b.App.Records["contact"].Fields["email"].Index {
		t.Fatal("contact.email should be indexed")
	}
	if b.Runs["follow-up"].Steps[0].Loop.Max != 25 {
		t.Fatal("loop max not decoded")
	}
}

func TestGoldenScreensUseOnlyKitParts(t *testing.T) {
	b, err := Load(golden)
	if err != nil {
		t.Fatal(err)
	}
	kit, err := LoadKit()
	if err != nil {
		t.Fatal(err)
	}
	for id, screen := range b.Screens {
		screen.Walk(func(n *Node) {
			part := kit.Part(n.Part)
			if part == nil {
				t.Errorf("%s: unknown part %s", id, n.Part)
				return
			}
			for prop := range n.Props {
				if part.Props[prop] == nil {
					t.Errorf("%s: %s has no prop %s", id, n.Part, prop)
				}
			}
			for _, actions := range n.On {
				for _, action := range actions {
					if action.Verb() == "" {
						t.Errorf("%s: %s has an action without a verb", id, n.Part)
					}
				}
			}
		})
	}
}

func TestKitCatalogIsConsistent(t *testing.T) {
	kit, err := LoadKit()
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, part := range kit.Parts {
		if seen[part.Name] {
			t.Errorf("duplicate part %s", part.Name)
		}
		seen[part.Name] = true
		if rule, names := part.ChildRule(); rule != "none" && rule != "any" && len(names) == 0 {
			t.Errorf("%s: bad children rule %q", part.Name, part.Children)
		}
		for name, prop := range part.Props {
			if _, ok := kit.PropTypes[prop.Type]; !ok {
				t.Errorf("%s.%s: unknown prop type %s", part.Name, name, prop.Type)
			}
		}
	}
}

func TestValidateReportsSchemaIssues(t *testing.T) {
	files, err := ReadDir(golden)
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]struct {
		file, content, want string
	}{
		"unknown field type": {AppFile, strings.Replace(string(files[AppFile]), `"type": "url"`, `"type": "hyperlink"`, 1), "/records/company/fields/domain/type"},
		"enum without values": {AppFile, strings.Replace(string(files[AppFile]), `"type": "enum",
          "values": [
            "1-10",
            "11-50",
            "51-200",
            "201+"
          ]`, `"type": "enum"`, 1), "/records/company/fields/size"},
		"plan line without id": {PlanFile, strings.Replace(string(files[PlanFile]), `"id": "company",`, ``, 1), "/data/1"},
		"screen id differs":    {"screens/deal.json", strings.Replace(string(files["screens/deal.json"]), `"screen": "deal"`, `"screen": "deals"`, 1), "/screen"},
		"two verbs":            {"screens/deal.json", strings.Replace(string(files["screens/deal.json"]), `"open": "pipeline"`, `"open": "pipeline", "runs.start": "follow-up"`, 1), "/body/children/1/children/3/on/click/0"},
		"delete without confirm": {"screens/deal.json", strings.Replace(string(files["screens/deal.json"]), `,
                      "confirm": "Delete {{record.name}}?"`, ``, 1), "/body/children/1/children/4/children/0/on/click/0"},
		"loop without max":    {"runs/follow-up.yaml", strings.Replace(string(files["runs/follow-up.yaml"]), `, max: 25`, ``, 1), "/steps/0/loop"},
		"step without action": {"runs/follow-up.yaml", strings.Replace(string(files["runs/follow-up.yaml"]), "records.update: { type: deal, id: $deal, set: { last_touch: $now } }", "title: nothing", 1), "/steps/0/steps/3"},
	}
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			if test.content == string(files[test.file]) {
				t.Fatal("replacement did not apply")
			}
			changed := Files{}
			for k, v := range files {
				changed[k] = v
			}
			changed[test.file] = []byte(test.content)
			issues := Validate(changed)
			if len(issues) == 0 {
				t.Fatal("expected issues")
			}
			for _, issue := range issues {
				if issue.File == test.file && strings.HasPrefix(issue.Path, test.want) {
					return
				}
			}
			t.Fatalf("no issue at %s%s; got %v", test.file, test.want, issues)
		})
	}
}

func TestLoadReturnsInvalidError(t *testing.T) {
	_, err := Load(t.TempDir())
	if err == nil {
		t.Fatal("empty dir should fail")
	}
	files, _ := ReadDir(golden)
	delete(files, PlanFile)
	issues := Validate(files)
	var invalid *InvalidError
	if len(issues) != 1 || issues[0].Rule != "missing" || errors.As(err, &invalid) {
		t.Fatalf("issues = %v err = %v", issues, err)
	}
}

func TestParseAgentNeedsFrontMatter(t *testing.T) {
	if _, err := ParseAgent([]byte("no front matter")); err == nil {
		t.Fatal("expected error")
	}
	doc, err := ParseAgent([]byte("---\nname: A\n---\nbody\n"))
	if err != nil || doc.Name != "A" || doc.Body != "body" {
		t.Fatalf("doc = %+v err = %v", doc, err)
	}
}
