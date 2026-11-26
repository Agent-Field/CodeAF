package builder

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
	"sync"
	"unicode/utf8"

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
The engine supplies your scope from this session. During long tool sequences,
report brief progress before six consecutive tool replies without text.

Start with codeaf app context. It gives your phase, manifest, plan, other
definition names and the last Check findings. Inspect an existing type with
context {"type":"contact"}. Read an individual definition
with context {"file":"screens/deals.json"}. Use codeaf app parts --for
add-view/board for that view's descriptor or --for PartName for markup.
The examples below cover normal planning, types, and views. Consult
reference for a schema or synthetic worked-example file only when needed.
Use {"schema":"run-2.json"} or {"example":"crm","file":"runs/follow-up.yaml"}.
The other synthetic example is hiring.

plan {"name":"CRM","plan":{"ask":"Track deals","data":[{"id":"deal","line":"Track deal stage"}],"screens":[{"id":"deals","line":"Deals by stage"}]}}
writes the outline and starts the manifest. Each plan line has id and line.
add-type {"name":"deal","definition":{"title":"name","fields":{"name":{"type":"text","required":true},"stage":{"type":"enum","values":["Lead","Won"]},"value":{"type":"money","currency":"USD"}}}}
declares a type with its fields in one command. Add fields to existing
types with add-field {"type":"deal","name":"stage","definition":{...}}.
A linked field uses {"type":"ref","to":"company"}. New types start read-only.
For an app where the person tracks records, grant read and write to every
tracked type. For a CRM, use
configure {"capabilities":{"records":{"deal":"rw","company":"rw","contact":"rw"}}}
before writing screens. Keep other capabilities when editing an existing
app. Make title fields unique when records are identified by name.
recipe {"kind":"add-view","id":"deals","record":"deal","as":"board","by":"stage","fields":["name","stage","value"]}
compiles a collection. For ordinary collections use as "table" or "list"
and fields, for example companies and contacts in a CRM. The recipe id is
the screen id. If asked, use add-form or add-detail for a separate screen.
Use add-chart with group and aggregate
(e.g. "sum:amount") for grouped totals. For composed screens, use markup
{"screen":"deals","title":"Deals","markup":"<...>"} with the kit parts as
elements and label, id, prop-name, bind-name and on-event attributes.
set-run {"id":"follow-up","text":"..."} writes a run if requested.
set-agent {"text":"..."} writes instructions if requested.
set-sample {"text":"{\"records\":{\"deal\":[{\"id\":\"00000000000000000000000001\",\"name\":\"Example deal\",\"stage\":\"Lead\",\"value\":120}]}}"}
writes the preview rows. Copy that 26-character id and change only its last
two digits for each row. Continue the sequence across ALL types. Each id
must be unique in the whole sample. Use the exact ids in reference fields.

Save three to six synthetic records per type and put one row in each
run's sample trigger or filter. Use existing recipe defaults for styling.
Use singular type ids such as deal, company, contact and candidate. Use
the person's existing type only when it appears under using. Before they
accept, declare a matching shape so they can see the reuse offer.
Use codeaf app common-types to find stable keys. The library's role labels
describe fields. Copy type and relevant fields only, omit role in app.json,
and point refs at the draft's local type names. Leave out optional fields and
relations the app does not need, especially circular references in examples.
Declare "common":"person" on contacts and "common":"organization" on
companies. Inspect context's common_bindings before proposing shared records.
After acceptance, use the same common key and set "uses":true with "fields":{}.
For an older unbound type, set "uses":true under its exact existing name.
Keep candidate records distinct when a hiring app only links to contacts.
Set "access":"rw" on add-type for every type this app's Actions create or
edit, including shared records. Use "access":"r" for lookup-only records.
If showing spending totals, use an add-chart recipe with aggregate "sum:amount".
Fill the visible fields in your examples so Check can measure bound columns.
Keep existing screen and node ids. For marked edits, use claim with screen
and node and preserve the rest of that screen. The Data place imports CSV.
Provide fields that map to the CSV headers. Finish with what the person can
try. The person chooses when to Apply.`

func systemPrompt() string {
	return skill + "\n\n" + instructions
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
	Marks      []Mark
	Stage      string
	Request    string
	Target     *Target
	Types      []modelType
	Using      []string
	Definition map[string]string
	Report     *schema.Report
}

const contextFilePage = 6 << 10

var schemaName = regexp.MustCompile(`^[a-z0-9-]+\.json$`)

func (in modelInput) context(files map[string]string, args json.RawMessage) (string, error) {
	var opts struct {
		File   string
		Offset int
		Type   string
	}
	if json.Unmarshal(args, &opts) != nil {
		return "", errors.New("choose a definition file or existing type by name")
	}
	if opts.Type != "" {
		for _, typ := range in.Types {
			if typ.Name == opts.Type {
				raw, err := json.Marshal(typ)
				return string(raw), err
			}
		}
		return "", errors.New("that existing type is unavailable")
	}
	if opts.File != "" {
		body, found := files[opts.File]
		if !found || !definitionPath.MatchString(opts.File) {
			return "", errors.New("that app definition file is unavailable")
		}
		if opts.Offset < 0 || opts.Offset > len(body) || (opts.Offset < len(body) && !utf8.RuneStart(body[opts.Offset])) {
			return "", errors.New("use the next offset from the previous page")
		}
		end := min(len(body), opts.Offset+contextFilePage)
		for end < len(body) && !utf8.RuneStart(body[end]) {
			end--
		}
		var next int
		if end < len(body) {
			next = end
		}
		raw, err := json.Marshal(struct {
			Text string `json:"text"`
			Next int    `json:"next_offset,omitempty"`
		}{body[opts.Offset:end], next})
		return string(raw), err
	}
	preview := func(name string) json.RawMessage {
		raw := json.RawMessage(files[name])
		if len(raw) > contextFilePage || !json.Valid(raw) {
			return nil
		}
		return raw
	}
	typeNames := make([]string, 0, len(in.Types))
	commonBindings := map[string]string{}
	for _, typ := range in.Types {
		typeNames = append(typeNames, typ.Name)
		if typ.Common != "" {
			commonBindings[typ.Name] = typ.Common
		}
	}
	raw, err := json.Marshal(struct {
		Stage          string            `json:"stage"`
		Manifest       json.RawMessage   `json:"manifest,omitempty"`
		Plan           json.RawMessage   `json:"plan,omitempty"`
		Files          []string          `json:"files,omitempty"`
		Types          []string          `json:"existing_types,omitempty"`
		CommonBindings map[string]string `json:"common_bindings,omitempty"`
		Using          []string          `json:"using,omitempty"`
		Report         *modelReport      `json:"report,omitempty"`
	}{
		in.Stage, preview(schema.AppFile), preview(schema.PlanFile), slices.Sorted(maps.Keys(files)),
		typeNames, commonBindings, in.Using, briefReport(in.Report),
	})
	return string(raw), err
}

type modelReport struct {
	Problems []schema.Problem `json:"problems"`
	Total    int              `json:"total"`
}

func briefReport(report *schema.Report) *modelReport {
	if report == nil {
		return nil
	}
	// The desk gets the full report. The model can fix the first group and
	// run Check again to surface the next group.
	count := min(len(report.Problems), 8)
	problems := make([]schema.Problem, count)
	for i, p := range report.Problems[:count] {
		p.Message = briefText(p.Message, 400)
		problems[i] = p
	}
	return &modelReport{Problems: problems, Total: len(report.Problems)}
}

func briefText(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	for !utf8.RuneStart(text[limit]) {
		limit--
	}
	return strings.TrimSpace(text[:limit]) + "…"
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
	seen := map[string]bool{}
	execute := func(ctx context.Context, command string) (result string, err error) {
		verb, args, err := appcmd.Parse(command)
		if err != nil {
			return "", err
		}
		mu.Lock()
		defer mu.Unlock()
		defer func() {
			if err != nil || len(result) < 512 {
				return
			}
			// The scoped turn still contains the earlier reply.
			if seen[result] {
				result = "Same result as the earlier reply in this step."
				return
			}
			seen[result] = true
		}()
		if in.Stage == Planning && (verb == "recipe" || verb == "markup" || verb == "look") {
			return "", errors.New("This turn is for the plan. Save the outline and record types, then finish the turn so the person can review it.")
		}
		switch verb {
		case "context":
			return in.context(files, args)
		case "common_types":
			raw, err := json.Marshal(schema.CommonTypes())
			return string(raw), err
		case "parts", "kit":
			var options struct{ For string }
			if err := json.Unmarshal(args, &options); err != nil {
				return "", errors.New("select one intent with parts --for")
			}
			out, err := Parts(options.For)
			return string(out), err
		case "reference":
			var options struct{ Schema, Example, File string }
			if err := json.Unmarshal(args, &options); err != nil {
				return "", errors.New("choose one file format or worked example file")
			}
			if (options.Example == "crm" || options.Example == "hiring") && definitionPath.MatchString(options.File) {
				raw, err := schema.BuilderExampleFile(options.Example, options.File)
				if err != nil {
					return "", errors.New("that worked example file is unavailable")
				}
				return string(raw), nil
			}
			if !schemaName.MatchString(options.Schema) {
				return "", errors.New("choose one file format with reference {\"schema\":\"plan-1.json\"}")
			}
			raw, err := schema.SchemaJSON(options.Schema)
			if err != nil {
				return "", errors.New("that file schema is unavailable")
			}
			var compact bytes.Buffer
			if err := json.Compact(&compact, raw); err != nil {
				return "", err
			}
			return compact.String(), nil
		case "check":
			report := schema.Validate(definition(files), types)
			out, err := json.Marshal(briefReport(&report))
			return string(out), err
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
				return "", errors.New(briefText(err.Error(), 1600))
			}
			if err := progress(next); err != nil {
				return "", err
			}
			files = next
			return "Saved the definition change.", nil
		}
	}
	prompt := systemPrompt() + "\n\nCURRENT PHASE: " + in.Stage + "\n"
	switch in.Stage {
	case Planning:
		prompt += "Write the plan and type shapes. End after those commands succeed. Check runs after the screens and samples exist."
	case "repair":
		prompt += `Fix the issues in context.report first. Add missing planned screens
with their exact ids, and remove extras with remove-screen {"id":"extra"}.
For a layout finding, replace only the affected screen with markup
{"screen":"...","replace":true,"markup":"..."}, keeping existing node ids.
Keep everything unaffected. Run check and finish.`
	default:
		prompt += `Complete the planned screens and samples before making refinements.
Set record permissions and compile one recipe for each missing plan screen,
using its exact screen id. Keep existing screens unless asked to edit them.
Use markup only for a composition the request needs and recipes cannot do.
Then write the sample rows, run check once, and finish with a short summary.`
	}
	bounded := &boundedModel{model: model}
	reply, err := session.ScopedCommandTurn(ctx, workspace, builderModelID(model), prompt, string(raw), bounded, execute)
	if bounded.exhausted() {
		err = errModelSteps
	}
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

const modelStepLimit = 20

var errModelSteps = fmt.Errorf("the Builder reached its %d-call limit before completing this step", modelStepLimit)

func (b *boundedModel) exhausted() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.calls > modelStepLimit
}

func (b *boundedModel) CompleteWithMessages(ctx context.Context, messages []ai.Message, opts ...ai.Option) (*ai.Response, error) {
	b.mu.Lock()
	b.calls++
	steps := b.calls
	b.mu.Unlock()
	if steps > modelStepLimit {
		return nil, errModelSteps
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
