package pipeline_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
)

const editableYAML = `start:
  labels: [ready]
  max_size: S
  max_cost: 0.5
  never: [auth/**]
  ask_first: [ask about secrets]
size:
  pr_lines: [12, 90]
  issue: label
pipeline:
  issue: [plan, approve, write, test, review, proof]
  pr: [read, review, proof]
  ci: [read, write, test, proof]
steps:
  plan:
    prompt: choose the files carefully
    until: the files are named
    max: 3
  doc-check:
    prompt: inspect the docs
    until: every link resolves
    max: 2
rules: [keep my rule]
fixed: [approve, proof]
`

func TestEveryWriterPreservesYAMLSettingsAndLeavesMarkdownAlone(t *testing.T) {
	for _, action := range []string{"save", "stages", "rule"} {
		t.Run(action, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, dir, pipeline.RepoFile, editableYAML)
			const markdown = "## issue\n1. plan · chat · retired input\n"
			writeFile(t, dir, factory.RecipeFile, markdown)
			before, err := pipeline.Working(dir)
			if err != nil {
				t.Fatal(err)
			}
			stages := factory.CopyStages(before.Recipe().Stages)
			stages[3].Ask = "run the regression suite"
			files := pipeline.Files{}
			switch action {
			case "save":
				recipe := before.Recipe()
				recipe.Stages = stages
				err = files.Save(dir, recipe)
			case "stages":
				err = files.BankStages(dir, factory.KindIssue, stages)
			case "rule":
				err = files.BankRule(dir, "add my rule")
				if err == nil {
					err = files.BankRule(dir, "add my rule")
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			after, err := pipeline.Load(dir)
			if err != nil || len(after.Problems) != 0 {
				t.Fatalf("saved YAML: %+v %v", after.Problems, err)
			}
			if !reflect.DeepEqual(before.Start, after.Start) || before.Size != after.Size {
				t.Fatal("start or size changed")
			}
			for _, name := range []string{"plan", "doc-check"} {
				if !reflect.DeepEqual(before.Steps[name], after.Steps[name]) {
					t.Fatalf("%s definition changed: %+v", name, after.Steps[name])
				}
			}
			if !reflect.DeepEqual(before.Pipeline[factory.KindPR], after.Pipeline[factory.KindPR]) || !reflect.DeepEqual(before.Fixed, after.Fixed) {
				t.Fatal("unrelated pipeline or fixed steps changed")
			}
			if action != "rule" && after.Steps["test"].Prompt != "run the regression suite" {
				t.Fatal("step edit was lost")
			}
			if action == "rule" && len(after.Rules) != 2 {
				t.Fatalf("rules duplicated: %v", after.Rules)
			}
			data, _ := os.ReadFile(filepath.Join(dir, factory.RecipeFile))
			if string(data) != markdown {
				t.Fatal("Markdown was written")
			}
			entries, _ := os.ReadDir(filepath.Join(dir, ".codeaf"))
			if len(entries) != 2 {
				t.Fatal("temporary file left behind")
			}
		})
	}
}

func TestFirstWriterUsesYAMLBuiltinsOrConvertsMarkdown(t *testing.T) {
	for _, markdown := range []string{"", "## issue\n1. plan · chat · keep this prompt · until clean · max 2\n\n## habits\n- keep this habit\n"} {
		t.Run(markdown, func(t *testing.T) {
			dir := t.TempDir()
			if markdown != "" {
				writeFile(t, dir, factory.RecipeFile, markdown)
			}
			if err := (pipeline.Files{}).WriteDefault(dir); err != nil {
				t.Fatal(err)
			}
			f, err := pipeline.Load(dir)
			if err != nil || len(f.Problems) != 0 {
				t.Fatalf("first YAML: %+v %v", f.Problems, err)
			}
			if len(f.Start.Labels) != 0 {
				t.Fatal("writing enabled automatic starts")
			}
			if markdown == "" {
				if !reflect.DeepEqual(f.Pipeline, pipeline.Defaults().Pipeline) {
					t.Fatal("first recipe is not the YAML defaults")
				}
			} else {
				if f.Steps["plan"].Prompt != "keep this prompt" || f.Steps["plan"].Until != "clean" || f.Steps["plan"].Max != 2 || len(f.Rules) != 1 {
					t.Fatalf("converted recipe: %+v", f)
				}
				data, _ := os.ReadFile(filepath.Join(dir, factory.RecipeFile))
				if string(data) != markdown {
					t.Fatal("converted Markdown was written")
				}
			}
			if err := (pipeline.Files{}).WriteDefault(dir); err == nil {
				t.Fatal("existing YAML overwritten")
			}
		})
	}
}

func TestWritersRefuseUnreadableYAMLWithoutReplacingIt(t *testing.T) {
	for _, invalid := range []string{"steps: [", "unknown: keep me", "steps:\n  extra:\n    prompt: keep me\n    effort: strong\n"} {
		t.Run(invalid, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, dir, pipeline.RepoFile, invalid)
			if err := (pipeline.Files{}).BankRule(dir, "new rule"); err == nil {
				t.Fatal("unreadable YAML accepted")
			}
			data, _ := os.ReadFile(filepath.Join(dir, pipeline.RepoFile))
			if string(data) != invalid {
				t.Fatal("unreadable YAML overwritten")
			}
		})
	}
}

func TestStepBankWritesOnlyPromptUntilAndMax(t *testing.T) {
	dir := t.TempDir()
	stages := []factory.Stage{{Name: "security", Kind: factory.StageChat, On: true, Ask: "look for secrets", When: "touches auth", Until: factory.UntilClean, Max: 2, Fixed: true}}
	if err := (pipeline.Files{}).BankStages(dir, factory.KindIssue, stages); err != nil {
		t.Fatal(err)
	}
	f, err := pipeline.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	s := f.Steps["security"]
	if !strings.Contains(s.Prompt, "touches auth") || s.Until != "clean" || s.Max != 2 || !f.Recipe().Stages[0].Fixed {
		t.Fatalf("step: %+v", s)
	}
	data, _ := os.ReadFile(filepath.Join(dir, pipeline.RepoFile))
	for _, retired := range []string{"kind:", "when:", "fanout:", "effort:", "## issue"} {
		if strings.Contains(string(data), retired) {
			t.Fatalf("retired grammar %s", retired)
		}
	}
}

func TestSavingRoundsKeepsFreeUntilAndHyphenatedName(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, pipeline.RepoFile, "pipeline:\n  issue: [doc-check]\nsteps:\n  doc-check:\n    prompt: inspect the docs\n    until: every link resolves\n    max: 2\n")
	before, err := pipeline.Working(dir)
	if err != nil {
		t.Fatal(err)
	}
	r := before.Recipe()
	r.Stages[0].Max = 3
	if err := (pipeline.Files{}).Save(dir, r); err != nil {
		t.Fatal(err)
	}
	after, err := pipeline.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(after.Pipeline[factory.KindIssue], []string{"doc-check"}) {
		t.Fatal("YAML name changed")
	}
	step := after.Steps["doc-check"]
	if step.Prompt != "inspect the docs" || step.Until != "every link resolves" || step.Max != 3 {
		t.Fatalf("round edit: %+v", step)
	}
}
