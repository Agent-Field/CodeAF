package builder

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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

const instructions = `You make personal apps. Use one codeaf app COMMAND {JSON}
at a time in the scoped executor. Report progress during long tool sequences.

Start with codeaf app context for the manifest, plan, file names and Check
findings. Get an external shape with context {"type":"contact"} and a file
with context {"file":"screens/deals.json"}. Reuse replies for unchanged files.
Fetch needed kit descriptors together with parts {"select":["add-view/board","RecordList"]}.
Use the examples below first. If needed, use reference {"schema":"app-2.json"}
for types and settings, process-2.json for lifecycle, and run-2.json or
{"example":"crm","file":"runs/follow-up.yaml"} for automations.

plan {"name":"CRM","plan":{"ask":"Track deals","data":[{"id":"deal","line":"Track deal stage"}],"screens":[{"id":"deals","line":"Deals by stage"}]}}
writes the outline and starts the manifest. Each plan line has id and line.
Use data for type ids, screens for screen ids, and automation for each
planned run id. Reuse those ids in the compiled definitions.
add-type {"name":"deal","definition":{"title":"name","fields":{"name":{"type":"text","required":true},"stage":{"type":"enum","values":["Lead","Won"]},"value":{"type":"money","currency":"USD"}}}}
declares a type with its fields in one command. Add fields to existing
types with add-field {"type":"deal","name":"stage","definition":{...}}.
A linked field uses {"type":"ref","to":"company"}. New types start read-only.
For an app where the person tracks records, grant read and write to every
tracked type. For a CRM, use
configure {"capabilities":{"records":{"deal":"rw","company":"rw","contact":"rw"}}}
before writing screens. Each configure setting replaces its whole value.
Include the existing records when adding other capabilities to an app. Make title fields unique when records are identified by name.
A new record should be creatable with its title and the essential input
the person supplied. Keep the person's field names as ids: a book's title
is title, a person's name is name, and an imported post's text is text.
Use the corresponding field as the record title. Give a required status
an initial value, for example
{"type":"enum","values":["To read","Reading","Finished"],"required":true,"default":"To read"}.
Leave other fields optional unless the request requires a value at creation.
Use a plain enum for status unless the request needs guarded transitions.
Guarded transitions use a matching enum field and this definition: "lifecycle":{"field":"status","initial":"Planned",
"transitions":[{"id":"ship","from":["Planned"],"to":"Shipped"}]}.
recipe {"kind":"add-view","id":"deals","record":"deal","as":"board","by":"stage","fields":["name","stage","value"]}
compiles a collection. For ordinary collections use as "table" or "list"
and fields, for example companies and contacts in a CRM. The recipe id is
the screen id. If asked, use add-form or add-detail for a separate screen.
Use add-chart with group and aggregate
(e.g. "sum:amount") for grouped totals. For composed screens, use markup
{"screen":"deals","title":"Deals","markup":"<...>"} with the kit parts as
elements and label, id, prop-name, bind-name and on-event attributes.
set-run {"id":"follow-up","text":"..."} writes a run YAML file. Give each
requested automation its own runnable file, including manual actions.
Use action-verb ids such as launch-research or draft-thread. Use params for
inputs, agent and prompt for model steps, and records.create for saved results.
Use the configured agent name and omit lifecycle-owned fields from record
writes. Put a send gate on the outbound tool step with the exact text in its args.
Set an app-wide spending ceiling with configure {"budget":{"per_run":0.10}}.
Configure schedules, commands and agent. Write instructions with set-agent.
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
For a shared type this app's Actions create or edit, use this shape:
add-type {"name":"contact","access":"rw","definition":{"uses":true,"common":"person","fields":{}}}
Access goes beside definition. Use "access":"r" for lookup-only records.
In sample rows, use only the existing type's fields returned by context.
If showing spending totals, use an add-chart recipe with aggregate "sum:amount".
Fill the visible fields in your examples so Check can measure bound columns.
Keep screen and node ids. Claim marked nodes before editing. Map CSV headers
to fields. Say what to try. The person chooses when to Apply.`

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
	Headless   bool
	Editing    bool
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

type contextRequest struct {
	File   string
	Offset int
	Type   string
}

func (in modelInput) context(files map[string]string, reads map[contextRequest]string, args json.RawMessage) (string, error) {
	var opts contextRequest
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
		if previous, read := reads[opts]; read && previous == body {
			return "This page is unchanged. Use the earlier reply for this file and offset.", nil
		}
		reads[opts] = body
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
	var unchanged []string
	preview := func(name string) json.RawMessage {
		raw := json.RawMessage(files[name])
		if len(raw) > contextFilePage || !json.Valid(raw) {
			return nil
		}
		page := contextRequest{File: name}
		if previous, read := reads[page]; read && previous == files[name] {
			unchanged = append(unchanged, name)
			return nil
		}
		reads[page] = files[name]
		return raw
	}
	manifest, plan := preview(schema.AppFile), preview(schema.PlanFile)
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
		Unchanged      []string          `json:"unchanged,omitempty"`
		Types          []string          `json:"existing_types,omitempty"`
		CommonBindings map[string]string `json:"common_bindings,omitempty"`
		Using          []string          `json:"using,omitempty"`
		Report         *modelReport      `json:"report,omitempty"`
	}{
		in.Stage, manifest, plan, slices.Sorted(maps.Keys(files)),
		unchanged, typeNames, commonBindings, in.Using, briefReport(in.Report),
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
	// Include every location so one repair can address all the findings.
	problems := make([]schema.Problem, len(report.Problems))
	for i, p := range report.Problems {
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
	reads := map[contextRequest]string{}
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
			return in.context(files, reads, args)
		case "common_types":
			raw, err := json.Marshal(schema.CommonTypes())
			return string(raw), err
		case "parts", "kit":
			var options struct {
				For    string
				Select []string
			}
			if err := json.Unmarshal(args, &options); err != nil || options.For != "" && options.Select != nil {
				return "", errors.New("use parts with one for selector or a select list")
			}
			if options.Select != nil {
				return batchParts(options.Select)
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
	switch in.Stage {
	case Planning:
		prompt += "Write the plan and type shapes. End after those commands succeed. Check runs after the screens and samples exist."
	case "repair":
		prompt += `Fix the issues in context.report first. Add missing planned screens
with their exact ids, and remove extras with remove-screen {"id":"extra"}.
For a layout finding, replace only the affected screen with markup
{"screen":"...","replace":true,"markup":"..."}, keeping existing node ids.
Keep everything unaffected. Fix the reported locations using the commands
shown above, then run check once to confirm and finish.`
	default:
		if in.Editing {
			prompt += `Apply only the requested change. If it adds an optional field,
use add-field with just {"type":"number"} for an optional number. Keep the
plan, screens and samples when the request leaves them alone. Then run check once and finish.`
			break
		}
		prompt += `Complete the planned screens and samples before making refinements.
Set record permissions and compile one recipe for each missing plan screen,
using its exact screen id. Implement each planned run and its requested
entry point before Check. Keep existing screens unless asked to edit them.
Use markup only for a composition the request needs and recipes cannot do.
Write the sample rows and run check once. When check reports no problems,
finish immediately with a short summary. The engine tries sample data and
Desk Look after this turn.`
	}
	if look == nil {
		prompt += "\nThe desktop renderer is unavailable. Skip look."
	}
	if in.Headless {
		prompt += "\nHeadless build: propose matching types from using. Continue to Check. Apply confirms reuse."
	}
	bounded := &boundedModel{model: model, limit: in.callBudget()}
	reply, err := session.ScopedCommandTurn(ctx, workspace, builderModelID(model), prompt, string(raw), bounded, execute)
	if bounded.exhausted() {
		err = errModelCallLimit
	}
	return files, reply, errors.Join(err, compilerErr)
}

func builderModelID(model Completer) string {
	if named, ok := model.(interface{ ModelID() string }); ok {
		return named.ModelID()
	}
	return "app-worker"
}

const (
	planCallBudget = 20
	buildCallBase  = 12
	callsPerType   = 2
	callsPerScreen = 3
	callsPerRun    = 5
	maxBuildCalls  = 64
)

// Planning starts before the outline exists. Writing and repair budget for
// the plan at the start of the turn, so editing it cannot extend a live turn.
func (in modelInput) callBudget() int {
	if in.Stage == Planning {
		return planCallBudget
	}
	plan, _ := schema.ParsePlan([]byte(in.Definition[schema.PlanFile]))
	return min(maxBuildCalls, buildCallBase+
		len(plan.Data)*callsPerType+len(plan.Screens)*callsPerScreen+len(plan.Automation)*callsPerRun)
}

type boundedModel struct {
	model Completer
	mu    sync.Mutex
	limit int
	calls int
	hit   bool
}

var errModelCallLimit = errors.New("the Builder reached its model call limit")

func (b *boundedModel) exhausted() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.hit
}

func (b *boundedModel) CompleteWithMessages(ctx context.Context, messages []ai.Message, opts ...ai.Option) (*ai.Response, error) {
	b.mu.Lock()
	if b.calls >= b.limit {
		b.hit = true
		b.mu.Unlock()
		return nil, errModelCallLimit
	}
	b.calls++
	b.mu.Unlock()
	return b.model.CompleteWithMessages(ctx, messages, append(opts, ai.WithMaxTokens(12000))...)
}

func definition(files map[string]string) schema.Definition {
	def := schema.Definition{Files: make(map[string][]byte, len(files))}
	for path, text := range files {
		def.Files[path] = []byte(text)
	}
	return def
}
