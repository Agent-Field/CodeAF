package pipeline

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// Files implements the local seam's YAML writes. Edits read the working copy,
// while execution continues to read the default branch through Load.
type Files struct{}

// LocalFiles supplies both the YAML interpreter and the writer to a local seam.
func LocalFiles() factory.LocalOption {
	return factory.WithOptions(factory.WithRecipeFiles(Files{}), factory.WithRecipeLoader(RecipeLoader("")))
}

// Working reads the editable file, preferring YAML and converting Markdown
// only in memory. Bad YAML is refused so an edit never erases an unread value.
func Working(dir string) (File, error) {
	if text, ok, err := readWorking(dir, RepoFile); err != nil {
		return File{}, err
	} else if ok {
		f, err := Parse(text)
		if err != nil {
			return f, err
		}
		if len(f.Problems) > 0 {
			return f, errors.New(f.Problems[0].Error())
		}
		return f, nil
	}
	if text, ok, err := readWorking(dir, factory.RecipeFile); err != nil {
		return File{}, err
	} else if ok {
		return FromMarkdown(text), nil
	}
	return Defaults(), nil
}

// Write atomically replaces only the YAML file, using the shared encoder.
func Write(dir string, f File) error {
	data, err := Encode(f)
	if err != nil {
		return err
	}
	path := filepath.Join(dir, RepoFile)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".factory-*.yaml")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// WriteDefault creates the shared file from the current YAML defaults or the
// in-memory Markdown conversion. It never replaces an existing YAML recipe.
func (Files) WriteDefault(dir string) error {
	if _, err := os.Stat(filepath.Join(dir, RepoFile)); !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("has a recipe already · %s", RepoFile)
	}
	f, err := Working(dir)
	if err != nil {
		return err
	}
	return Write(dir, f)
}

// Save changes the recipe fields and keeps start, size and unchanged step
// definitions from YAML. A free until sentence must not become a second prompt.
func (Files) Save(dir string, r factory.Recipe) error {
	f, err := Working(dir)
	if err != nil {
		return err
	}
	before := f.Recipe()
	for _, kind := range []factory.Kind{factory.KindIssue, factory.KindPR, factory.KindCI} {
		if reflect.DeepEqual(before.For(kind), r.For(kind)) {
			continue
		}
		if err := replaceStages(&f, kind, r.For(kind)); err != nil {
			return err
		}
	}
	f.Rules = dedupeFold(append(append([]string{}, r.Policy...), r.Habits...))
	return Write(dir, f)
}

// BankStages changes one pipeline, keeping every unrelated YAML setting.
func (Files) BankStages(dir string, kind factory.Kind, stages []factory.Stage) error {
	if len(stages) == 0 {
		return errors.New("there are no stages to bank")
	}
	if kind == "" {
		kind = factory.KindIssue
	}
	f, err := Working(dir)
	if err != nil {
		return err
	}
	if err := replaceStages(&f, kind, stages); err != nil {
		return err
	}
	return Write(dir, f)
}

func replaceStages(f *File, kind factory.Kind, stages []factory.Stage) error {
	previous := f.Recipe().For(kind)
	oldNames := append([]string(nil), f.Pipeline[kind]...)
	var names []string
	for _, stage := range stages {
		name, step, keep, _ := convertStage(stage)
		if !keep {
			continue
		}
		// Keep the YAML name and any prompt or stop sentence the edit did not change.
		for i, old := range previous {
			if old.Name == stage.Name && i < len(oldNames) {
				name = oldNames[i]
				original := f.Steps[name]
				if old.Ask == stage.Ask {
					step.Prompt = original.Prompt
				}
				if old.Until == stage.Until {
					step.Until = original.Until
					_, free := mechanicalUntil(original.Until)
					if free {
						step.Prompt = strings.TrimSuffix(step.Prompt, " Stop when: "+original.Until)
					}
				}
				break
			}
		}
		if !factory.ApproveWord(name) && strings.TrimSpace(step.Prompt) == "" {
			if builtin, ok := f.Steps[name]; ok {
				step = builtin
			} else {
				return fmt.Errorf("%s needs a prompt", name)
			}
		}
		// One step definition is shared by every pipeline that names it.
		f.Steps[name] = step
		if stage.Fixed {
			f.Fixed = unionExact(f.Fixed, []string{name})
		} else {
			var fixed []string
			for _, n := range f.Fixed {
				if n != name {
					fixed = append(fixed, n)
				}
			}
			f.Fixed = fixed
		}
		names = append(names, name)
	}
	f.Pipeline[kind] = names
	return nil
}

// BankRule saves policy and habits as rules, once, because both are sentences
// given to the job and YAML has one place for those sentences.
func (Files) BankRule(dir, sentence string) error {
	sentence = strings.Join(strings.Fields(sentence), " ")
	if sentence == "" {
		return errors.New("say the rule")
	}
	f, err := Working(dir)
	if err != nil {
		return err
	}
	f.Rules = unionFold(f.Rules, []string{sentence})
	return Write(dir, f)
}
