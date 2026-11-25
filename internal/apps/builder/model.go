package builder

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"regexp"
	"sync"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/apps/appcmd"
	"github.com/Agent-Field/codeaf/internal/apps/schema"
	"github.com/Agent-Field/codeaf/internal/session"
)

// Completer is the same model door used by the engine's headless turns.
type Completer interface {
	CompleteWithMessages(context.Context, []ai.Message, ...ai.Option) (*ai.Response, error)
}

const instructions = `You make personal apps in codeaf. Use codeaf app COMMAND {JSON}
through the available command executor. It executes a single app command
directly. Run one command at a time so each sees the changes before it.
The engine supplies your scope from this session.

Start with codeaf app context. It returns the phase, your working app,
shape-only type catalog, marks and any Check report. Read the served kit and
schemas for the active vocabulary. The examples are references for how the
files fit together. Submit your own app with the commands below.

plan {"name":"CRM","plan":{...}} writes the outline and starts the manifest.
add-type {"name":"deal","definition":{...}} declares a record type.
add-field {"type":"deal","name":"stage","definition":{...}} declares a field.
configure {"capabilities":{...},"agent":{...},"triggers":[...]} updates app settings.
recipe {"kind":"add-view","id":"deals","record":"deal","as":"board","by":"stage"}
compiles a collection view. The served catalog describes its recipe kinds
and options. For a composed screen, use markup {"screen":"deals","title":"Deals",
"markup":"<...>"} with the catalog parts as elements. Use label, id, prop-name,
bind-name and on-event attributes. The compiler assigns missing node ids.
set-run {"id":"follow-up","text":"..."} writes one automation.
set-agent {"text":"..."} supplies the app agent's instructions.
set-sample {"text":"..."} supplies fictional rows for preview and tests.
claim {"screen":"deals","node":"..."} shows where you are working.

For planning, create the plan and its record types. For writing, compile the
screens, write the named automations and sample rows, and run the checks.
Use singular type ids such as deal, company, contact and candidate. Use
an existing type only when it appears in accepted reuse. Keep screen and node
ids stable. If the request names a node, keep the rest of the screen intact.
Give every main list usable search and a way to edit its records.

Use three to six synthetic records per relevant type. Link references to the
sample ids and include a row matching each run's filter or record trigger.
The Data place imports CSV. Create fields that can be mapped from the CSV and
never add a file or CSV automation. End with a short sentence about the change.
The person chooses when to Apply.`

var reference = schema.BuilderReference()

func systemPrompt() string {
	return skill + "\n\n" + instructions + "\n\nAPP KIT\n" + string(schema.KitJSON()) + "\n" + reference
}

var definitionPath = regexp.MustCompile(`^(app\.json|plan\.json|sample\.json|agent\.md|screens/[A-Za-z0-9][A-Za-z0-9_-]{0,127}\.json|runs/[A-Za-z0-9][A-Za-z0-9_-]{0,127}\.ya?ml)$`)

type DefinitionEdit struct {
	Path string  `json:"path"`
	Text *string `json:"text,omitempty"`
}

// applyEdits is used for both the app command and the scoped session tool.
// It changes only the provided snapshot, and only after every path is checked.
func applyEdits(files map[string]string, edits []DefinitionEdit) (map[string]string, error) {
	if len(edits) == 0 || len(edits) > 128 {
		return nil, errors.New("send one or more definition changes")
	}
	next := maps.Clone(files)
	for _, edit := range edits {
		if !definitionPath.MatchString(edit.Path) {
			return nil, errors.New("only app.json, plan.json, agent.md, sample.json, screens/<id>.json or runs/<id>.yaml can be edited")
		}
		if edit.Text == nil {
			delete(next, edit.Path)
			continue
		}
		if len(*edit.Text) > 512<<10 {
			return nil, errors.New("app file is too large")
		}
		next[edit.Path] = *edit.Text
	}
	size := 0
	for _, text := range next {
		size += len(text)
	}
	if len(next) > 128 || size > 2<<20 {
		return nil, errors.New("app definition is too large")
	}
	return next, nil
}

type modelInput struct {
	Marks      []Mark            `json:"marks"`
	Stage      string            `json:"stage"`
	Request    string            `json:"request"`
	Target     *Target           `json:"target,omitempty"`
	Types      any               `json:"existing_types,omitempty"`
	Using      []string          `json:"using,omitempty"`
	Definition map[string]string `json:"definition"`
	Report     *schema.Report    `json:"report,omitempty"`
}

// The model edits only this map. Paths, file contents, and the agent file
// referenced by app.json never become filesystem read or write arguments.
func modelTurn(ctx context.Context, model Completer, workspace string, in modelInput, types schema.TypeSet, author authoring, progress func(map[string]string) error, look func(context.Context, string) ([]string, error), claim func(context.Context, Target) error, presence func() (*Presence, error)) (map[string]string, string, error) {
	raw, err := json.Marshal(struct {
		Request string  `json:"request"`
		Target  *Target `json:"target,omitempty"`
	}{in.Request, in.Target})
	if err != nil {
		return nil, "", err
	}
	files := maps.Clone(in.Definition)
	var mu sync.Mutex
	var compilerErr error
	execute := func(ctx context.Context, command string) (string, error) {
		verb, args, err := appcmd.Parse(command)
		if err != nil {
			return "", err
		}
		mu.Lock()
		defer mu.Unlock()
		if in.Stage == Planning && (verb == "recipe" || verb == "markup" || verb == "look") {
			return "", errors.New("This turn is for the plan. Save the outline and record types, then finish the turn so the person can review it.")
		}
		switch verb {
		case "context":
			view := in
			view.Definition = files
			out, err := json.Marshal(view)
			return string(out), err
		case "kit":
			return string(schema.KitJSON()), nil
		case "reference":
			return reference, nil
		case "check":
			report := schema.Validate(definition(files), types)
			return report.String(), nil
		case "look":
			if look == nil {
				return "", errors.New("desk_unavailable: open the desktop renderer")
			}
			var target Target
			if json.Unmarshal(args, &target) != nil || !validTarget(&target) {
				return "", errors.New("supply a valid screen id")
			}
			problems, err := look(ctx, target.Screen)
			if err != nil {
				return "", errors.New("the desktop could not draw that screen")
			}
			out, err := json.Marshal(map[string][]string{"problems": problems})
			return string(out), err
		case "claim":
			var target Target
			if json.Unmarshal(args, &target) != nil || !validTarget(&target) {
				return "", errors.New("supply a valid screen and node address")
			}
			return "Claimed this element.", claim(ctx, target)
		case "mark":
			raw, err := json.Marshal(in.Marks)
			return string(raw), err
		case "presence":
			claim, err := presence()
			if err != nil {
				return "", err
			}
			raw, err := json.Marshal(claim)
			return string(raw), err
		default:
			next, err := author.edit(ctx, verb, files, types, args)
			if err != nil {
				if errors.Is(err, errCompilerUnavailable) {
					compilerErr = err
				}
				return "", err
			}
			if err := progress(next); err != nil {
				return "", err
			}
			files = next
			return "Saved the definition change.", nil
		}
	}
	prompt := systemPrompt() + "\n\nCURRENT PHASE: " + in.Stage + "\n"
	if in.Stage == Planning {
		prompt += "This turn ends with a reviewable plan and type shapes. Do not compile screens in this turn."
	}
	reply, err := session.ScopedCommandTurn(ctx, workspace, builderModelID(model), prompt, string(raw), &boundedModel{model: model}, execute)
	return files, reply, errors.Join(err, compilerErr)
}

func builderModelID(model Completer) string {
	if named, ok := model.(interface{ ModelID() string }); ok {
		return named.ModelID()
	}
	return "app-worker"
}

type boundedModel struct {
	model Completer
	mu    sync.Mutex
	calls int
}

func (b *boundedModel) CompleteWithMessages(ctx context.Context, messages []ai.Message, opts ...ai.Option) (*ai.Response, error) {
	b.mu.Lock()
	b.calls++
	steps := b.calls
	b.mu.Unlock()
	if steps > 20 {
		return nil, errors.New("the app model ran out of steps")
	}
	return b.model.CompleteWithMessages(ctx, messages, append(opts, ai.WithMaxTokens(12000))...)
}

func definition(files map[string]string) schema.Definition {
	def := schema.Definition{Files: make(map[string][]byte, len(files))}
	for path, text := range files {
		def.Files[path] = []byte(text)
	}
	return def
}
