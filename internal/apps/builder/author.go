package builder

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/check"
	"github.com/Agent-Field/codeaf/internal/apps/data"
	"github.com/Agent-Field/codeaf/internal/apps/schema"
)

// Compiler is supplied by the manifest lane. It accepts intent or markup and
// returns a native screen compiled against the requested app's vocabulary.
type Compiler interface {
	Compile(context.Context, string, schema.Definition, schema.TypeSet, json.RawMessage) (CompiledChange, error)
}

var errCompilerUnavailable = errors.New("compiler_unavailable: the engine has no recipe or markup compiler for this kit")

type CompiledChange struct {
	ScreenID string
	Screen   json.RawMessage
	App      json.RawMessage
}

type authoring struct {
	compiler      Compiler
	validateTypes func([]byte) error
}

type fileCommand struct {
	ID   string `json:"id,omitempty"`
	Text string `json:"text"`
}

func (a authoring) edit(ctx context.Context, verb string, files map[string]string, types schema.TypeSet, raw json.RawMessage) (map[string]string, error) {
	var edit DefinitionEdit
	switch verb {
	case "plan":
		var args struct {
			Name string          `json:"name"`
			Kit  *int            `json:"kit,omitempty"`
			Plan json.RawMessage `json:"plan"`
		}
		if json.Unmarshal(raw, &args) != nil || !json.Valid(args.Plan) {
			return nil, errors.New("plan needs the app name and a plan object")
		}
		plan, err := schema.ParsePlan(args.Plan)
		if err != nil || strings.TrimSpace(plan.Ask) == "" {
			return nil, errors.New("plan needs a plan object with a nonempty ask")
		}
		for _, extension := range plan.Extensions {
			if !validTypeWord(extension.ID) || strings.TrimSpace(extension.Reason) == "" {
				return nil, errors.New("each plan extension needs an id and a reason")
			}
		}
		if len(files[schema.AppFile]) == 0 {
			if strings.TrimSpace(args.Name) == "" {
				return nil, errors.New("give the app a name")
			}
			var catalog struct{ Kit int }
			_ = json.Unmarshal(schema.KitJSON(), &catalog)
			if args.Kit != nil {
				catalog.Kit = *args.Kit
			}
			manifest, _ := json.Marshal(map[string]any{
				"id": "app", "name": args.Name, "version": 1, "kit": catalog.Kit,
				"records": map[string]any{}, "capabilities": map[string]any{"records": map[string]string{}},
			})
			files = maps.Clone(files)
			files[schema.AppFile] = string(manifest)
		}
		text := string(args.Plan)
		edit = DefinitionEdit{Path: schema.PlanFile, Text: &text}
	case "add_type", "add_field", "configure":
		manifest, err := changeManifest(files[schema.AppFile], verb, raw)
		if err != nil {
			return nil, err
		}
		if a.validateTypes != nil {
			if err := a.validateTypes([]byte(manifest)); err != nil {
				return nil, err
			}
		}
		edit = DefinitionEdit{Path: schema.AppFile, Text: &manifest}
	case "recipe", "markup":
		if a.compiler == nil {
			return nil, errCompilerUnavailable
		}
		result, err := a.compiler.Compile(ctx, verb, definition(files), types, raw)
		if err != nil {
			return nil, err
		}
		edits := []DefinitionEdit{}
		if len(result.App) > 0 {
			if !json.Valid(result.App) {
				return nil, errors.New("the compiler returned an invalid app change")
			}
			text := string(result.App)
			edits = append(edits, DefinitionEdit{Path: schema.AppFile, Text: &text})
		}
		if len(result.Screen) > 0 {
			if !json.Valid(result.Screen) {
				return nil, errors.New("the compiler returned an invalid screen")
			}
			text := string(result.Screen)
			edits = append(edits, DefinitionEdit{Path: "screens/" + result.ScreenID + ".json", Text: &text})
		}
		return applyEdits(files, edits)
	case "set_run", "set_agent", "set_sample":
		var args fileCommand
		if json.Unmarshal(raw, &args) != nil {
			return nil, errors.New("supply the definition text")
		}
		edit = DefinitionEdit{Text: &args.Text}
		switch verb {
		case "set_run":
			edit.Path = "runs/" + args.ID + ".yaml"
			run, err := schema.ParseRun([]byte(args.Text))
			if err != nil {
				return nil, err
			}
			for _, name := range slices.Sorted(maps.Keys(run.Params)) {
				param := run.Params[name]
				if param.Type != "ref" || param.Default == nil {
					continue
				}
				id, _ := param.Default.(string)
				if !data.ValidRecordID(id) {
					return nil, fmt.Errorf("the %s input takes a single record. Remove its default and pass the record from the command, or use a refs input and a loop for a scheduled search", name)
				}
			}
		case "set_agent":
			edit.Path = "agent.md"
		case "set_sample":
			edit.Path = schema.SampleFile
			if _, err := check.Sample(definition(map[string]string{edit.Path: args.Text})); err != nil {
				return nil, err
			}
		}
	case "remove_run", "remove_screen":
		var args struct{ ID string }
		if json.Unmarshal(raw, &args) != nil {
			return nil, errors.New("supply an id")
		}
		edit.Path = "runs/" + args.ID + ".yaml"
		if verb == "remove_screen" {
			edit.Path = "screens/" + args.ID + ".json"
		}
	default:
		return nil, fmt.Errorf("unknown definition command %q", verb)
	}
	return applyEdits(files, []DefinitionEdit{edit})
}

var appSettings = []string{"name", "icon", "agent", "capabilities", "triggers", "commands", "budget", "outputs"}

func changeManifest(text, verb string, raw json.RawMessage) (string, error) {
	var manifest map[string]json.RawMessage
	if json.Unmarshal([]byte(text), &manifest) != nil || manifest == nil {
		return "", errors.New("plan the app before adding its definitions")
	}
	if verb == "configure" {
		var patch map[string]json.RawMessage
		if json.Unmarshal(raw, &patch) != nil {
			return "", errors.New("configure needs a JSON object")
		}
		for key, value := range patch {
			if !slices.Contains(appSettings, key) {
				return "", fmt.Errorf("%s is not an app setting", key)
			}
			manifest[key] = value
		}
	} else if err := editType(manifest, verb, raw); err != nil {
		return "", err
	}
	out, err := json.MarshalIndent(manifest, "", "  ")
	return string(out), err
}

func editType(manifest map[string]json.RawMessage, verb string, raw json.RawMessage) error {
	var args struct {
		Type       string          `json:"type,omitempty"`
		Name       string          `json:"name"`
		Access     string          `json:"access,omitempty"`
		Definition json.RawMessage `json:"definition"`
	}
	if json.Unmarshal(raw, &args) != nil || !validTypeWord(args.Name) || !json.Valid(args.Definition) ||
		!strings.HasPrefix(string(args.Definition), "{") {
		return errors.New("a type or field needs a stable name and a definition object")
	}
	var records map[string]json.RawMessage
	if json.Unmarshal(manifest["records"], &records) != nil || records == nil {
		return errors.New("the app has no record definitions")
	}
	if verb == "add_type" {
		records[args.Name] = args.Definition
		if err := declareRecordAccess(manifest, args.Name, args.Access); err != nil {
			return err
		}
	} else {
		var record map[string]json.RawMessage
		if json.Unmarshal(records[args.Type], &record) != nil || record == nil {
			return errors.New("add the field's record type first")
		}
		var fields map[string]json.RawMessage
		if json.Unmarshal(record["fields"], &fields) != nil || fields == nil {
			fields = map[string]json.RawMessage{}
		}
		fields[args.Name] = args.Definition
		record["fields"], _ = json.Marshal(fields)
		records[args.Type], _ = json.Marshal(record)
	}
	manifest["records"], _ = json.Marshal(records)
	return nil
}

func validTypeWord(name string) bool {
	return len(name) > 0 && len(name) < 129 && strings.Trim(name, "abcdefghijklmnopqrstuvwxyz0123456789_-") == ""
}

// A type starts read-only unless the author declares write access for its
// edit Actions. The existing grant survives a shape-only edit.
func declareRecordAccess(manifest map[string]json.RawMessage, name, access string) error {
	if access != "" && access != "r" && access != "rw" {
		return errors.New("record access is r or rw")
	}
	caps := map[string]json.RawMessage{}
	if raw := manifest["capabilities"]; len(raw) > 0 {
		if err := json.Unmarshal(raw, &caps); err != nil {
			return err
		}
	}
	records := map[string]string{}
	if raw := caps["records"]; len(raw) > 0 {
		if err := json.Unmarshal(raw, &records); err != nil {
			return err
		}
	}
	if access == "" {
		if records[name] != "" {
			return nil
		}
		access = "r"
	}
	if records == nil {
		records = map[string]string{}
	}
	if caps == nil {
		caps = map[string]json.RawMessage{}
	}
	records[name] = access
	caps["records"], _ = json.Marshal(records)
	manifest["capabilities"], _ = json.Marshal(caps)
	return nil
}
