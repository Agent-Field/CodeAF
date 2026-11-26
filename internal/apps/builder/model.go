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
	"time"
	"unicode/utf8"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/apps/appcmd"
	"github.com/Agent-Field/codeaf/internal/apps/runs"
	"github.com/Agent-Field/codeaf/internal/apps/schema"
	"github.com/Agent-Field/codeaf/internal/apps/store"
	"github.com/Agent-Field/codeaf/internal/session"
)

// Completer is the same model door used by the engine's headless turns.
type Completer interface {
	CompleteWithMessages(context.Context, []ai.Message, ...ai.Option) (*ai.Response, error)
}

const (
	planFirstPass      = 150 * time.Second
	planCorrectionPass = 110 * time.Second
	// PlanWait allows both model passes and time to persist the result.
	PlanWait = planFirstPass + planCorrectionPass + 35*time.Second
)

const instructions = `You make personal apps. Use one codeaf app COMMAND {JSON}
at a time in the scoped executor. Finish when the requested edits are saved.

Use the supplied manifest, plan, file names, types, parts and Check findings.
Read additional files and types together with context {"select":[{"file":"screens/deals.json"},{"type":"contact"}]}.
Batch missing kit descriptors with parts {"select":["add-view/board","RecordList"]}.
Read reference {"schema":"app-2.json"} for app settings, process-2.json for lifecycle or run-2.json for automations.

plan {"name":"CRM","plan":{"ask":"Track deals","data":[{"id":"deal","line":"Track deal stage"}],"screens":[{"id":"deals","line":"Deals by stage"}]}}
writes the outline. Use the actual type, screen and automation ids in the plan.
Use "create":true for types with an Add form. Name other actions by command or run ID.
The executor selects CSV import, lifecycle and local approval. Keep its builtins.
Set the built-in record or run as needed. Declare lifecycle states and planned
transition IDs with add-type. For approval, name the run that saves the record.
Use the exact planned run IDs.
Declare custom parts in "extensions":[{"id":"dot-cells","reason":"Show raised and flat dots"}].
add-type {"name":"deal","definition":{"title":"name","fields":{"name":{"type":"text","required":true},"stage":{"type":"enum","values":["Lead","Won"]},"value":{"type":"money","currency":"USD"}}}}
declares a type with its fields in one command. Add fields to existing
types with add-field {"type":"deal","name":"stage","definition":{...}}.
For a new common contact, use add-type {"name":"contact","definition":{"common":"person","title":"name","fields":{"name":{"type":"text"}}}}.
For a new common company, put "common":"organization" in the same place.
Keep the common key on later edits so other apps can reuse the same records.
A linked field uses {"type":"ref","to":"company"}. New types start read-only.
For an app where the person tracks records, grant read and write to every
tracked type. For a CRM, use
configure {"capabilities":{"records":{"deal":"rw","company":"rw","contact":"rw"}}}
before writing screens. Each configure setting replaces its whole value.
Include the existing records when adding other capabilities to an app. Make title fields unique when records are identified by name.
recipe {"kind":"add-view","id":"deals","record":"deal","as":"board","by":"stage","fields":["name","stage","value"]}
compiles a collection. Use as "table" or "list" for ordinary collections.
Use add-chart with group and aggregate (e.g. "sum:amount") for grouped totals.
For a composed screen, write markup {"screen":"deals","title":"Deals","markup":"<...>"}
with kit elements and label, id, prop-name, bind-name and on-event attributes.
Use extensions only when no built-in fits. Read reference {"schema":"custom-extensions"} and state why.
set-run {"id":"follow-up","text":"..."} writes a run YAML file. Give each
requested automation its own runnable file, including manual actions.
Use action-verb ids. Record commands pass a ref parameter by id, with no default.
Use plan.agent.actions for the exact Action IDs the assistant promises.
Agent steps return only the requested text at $steps.<id>.output. For loops
and approvals, use the supplied run example as a syntax reference.
Use exact earlier step ids in args. Put send gates on outbound tools, or use
a standalone gate for review alone. Omit lifecycle-owned fields from writes.
Set an app-wide spending ceiling with configure {"budget":{"per_run":0.10}}.
Configure schedules, commands and agent. Write instructions with set-agent.
Use the supplied screen_controls guide for commands, Add buttons and approvals.
set-sample {"text":"{\"records\":{\"deal\":[{\"id\":\"00000000000000000000000001\",\"name\":\"Example deal\",\"stage\":\"Lead\",\"value\":120}]}}"}
writes the preview rows. Copy that 26-character id and change only its last
two digits for each row. Continue the sequence across ALL types. Each id
must be unique in the whole sample. Use the exact ids in reference fields.

Save three to six synthetic records per type unless the request calls for
more. A full example needs at least 30 total. Put one row in each run's
sample trigger or filter. Use existing recipe defaults for styling.
Use singular type ids such as deal, company, contact and candidate. Use
the person's existing type only when it appears under using. Before they
accept, declare a matching shape so they can see the reuse offer.
Find common keys with codeaf app common-types. Copy only needed fields without
role labels. Point refs at local type names and seed both ends of any linked examples.
For existing records, copy the common key from context.common_bindings.
If it lists no key for that type, omit common and keep its exact name.
Reuse uses {"uses":true,"fields":{}}. Apply still confirms headless proposals.
Keep candidate records distinct when a hiring app only links to contacts.
For a shared type this app's Actions create or edit, use this shape:
add-type {"name":"contact","access":"rw","definition":{"uses":true,"common":"person","fields":{}}}
Access goes beside definition. Use "access":"r" for lookup-only records.
In sample rows, use only the existing type's fields returned by context.
Fill visible sample fields. Claim marked nodes and preserve existing ids.
Your data already imports CSV into editable record types. For CSV import,
match field ids to the headers and use the built-in import, with no run.`

const automationGuide = `Use the listed connector operations and trigger kinds. If a source is missing,
say so and use a supported input or labeled local draft if allowed. Run prompts cannot fetch
URLs. Pass the document text from a source, previous step or intake attachment.
A refs parameter can default to a query such as "status:Open" or take the
trigger's matching link via default: $input.record.links. Read IDs with $params.post.id.`

const extensionReference = `Use a built-in part whenever it can meet the request. If none can, say why,
check kit-parts for a reusable part, and only then use set-custom-part with
{"id":"name","manifest":{...},"source":"<part>...</part>"}. Get its manifest
format with reference {"schema":"part-2.json"} and include the reason. The
source draws one group per queried record with up to 12 <title/>, <text/>,
<display/>, <dot-cells/> or <button/> elements. Dot-cells draws Unicode Braille as round raised and flat dot positions. Each takes text="literal" or field="a
manifest input". A button also takes event="a declared event". No scripts or
custom styles. Bind it with <Part part="custom/name" bind-records="record"
bind-view="query" label="Name" .../> or bind-record with bind-type for one record. Map each on-event to one registered
Action and limit the view to 24 records. Import a Kit entry by exact version
with use-kit-part {"id":"name","version":1} and bind it the same way.
When built-in automation steps do not express a calculation, say why and use
set-custom-step {"id":"name","definition":{"version":1,"summary":"...",
"reason":"...","inputs":[{"name":"from","type":"date"}],"output":"integer",
"effects":[],"timeout_ms":100,"memory_pages":1,"source":"(module ...)"}}
with WebAssembly text. Export run with i64 arguments in input order, and one
i64 result. Integer and bool are direct scalars. A date or timestamp is a UTC
day number since 1970-01-01. A dates list takes two i64 arguments (offset and
length), and the guest exports memory. The host writes consecutive
little-endian i64 day numbers at the offset. The module has no imports,
tables or start function. Grant "custom.name" in capabilities.tools and call
it with a tool: custom.name run step plus args matching the input names. Its
result is $steps.<id>.output. Check exercises it on example parameters.
Consult kit-steps before authoring a calculation. Import by ID and version
with use-kit-step, then grant and call it like any other custom tool.`

func systemPrompt() string {
	return skill + "\n\n" + instructions + "\nUse the supplied screen archetypes for work screens."
}

var definitionPath = regexp.MustCompile(`^(app\.json|plan\.json|sample\.json|agent\.md|screens/[A-Za-z0-9][A-Za-z0-9_-]{0,127}\.json|runs/[A-Za-z0-9][A-Za-z0-9_-]{0,127}\.ya?ml|custom/[a-z][a-z0-9_-]{0,47}\.(json|tsx|step))$`)

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
			return nil, errors.New("only app definition files can be edited")
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
	SourceOperations   map[string][]string
	Headless           bool
	Editing            bool
	Marks              []Mark
	Stage              string
	Request            string
	Target             *Target
	Types              []modelType
	Using              []string
	Definition         map[string]string
	Report             *schema.Report
	Continued          bool
	OwnerNameAvailable bool
	Reuse              map[string]reuseBinding
}

const recordPolicyGuide = `To hide a field, include policy.fields in its owning type, for example
{"fields":{"salary":"hide","tax_id":"mask"}}. To show a person's own rows,
include policy.row {"field":"owner","attribute":"id"}. Preserve existing
classifications and apply any handling the person asked for. A restricted or
pii field is masked even for the owner. For ordinary titles the owner needs
to read and edit in Personal, use internal or confidential. Reserve restricted
and pii for fields whose purpose calls for that protection.`

const screenControlsGuide = `A new record should be creatable with its title and the essential input
the person supplied. Keep the person's field names as ids: a book's title
is title, a person's name is name, and an imported post's text is text.
Use the corresponding field as the record title. Give a required status
an initial value, for example
{"type":"enum","values":["To read","Reading","Finished"],"required":true,"default":"To read"}.
Leave other fields optional unless the request requires a value at creation.
Use a plain enum for status unless the request needs guarded transitions.
Guarded transitions use a matching enum field and this definition: "lifecycle":{"field":"status","initial":"Planned",
"transitions":[{"id":"ship","from":["Planned"],"to":"Shipped"}]}.

Declare record commands with "for":"deal" and params {"deal":"$record.id"}
so record views can show them with the current record. For an app-wide
action without a record parameter, use a Button with on-click='[{"runs.start":"review"}]'. A local save requiring approval puts gate: {approval: proof}
on the records.create or records.update step itself so the Inbox shows the
actual proposed write. Set a run title and step titles in plain language.
For an assistant that changes records, give it a declared screen view to
find the target. Use one typed ref parameter in the run, such as deal, and
write only the selected row ($params.deal.id). A record-writing run is
grantable by run ID, or by the command ID when a command launches it.
Use a run for multi-step record commands. For a CRM Follow up button,
draft a ready-to-send message addressed to the linked contact. Save it as
a separate note linked to the deal, with approval on the save and no sending.
For a follow-up draft written on the person's behalf, address the bound
contact and sign with the available owner name. Write the greeting and
signature into the draft using {{params.deal.contact.name}} and {{owner.name}},
using the actual declared relation and title field ids. Don't leave
bracketed placeholders in draft prompts, template values or example replies.
A collection recipe adds the creation control for a planned type with
"create":true and write access. For a screen composed by hand, use a Button
labelled "Add <record>" with on-click to open a Sheet
containing a Form, or use a Button with an asked records.create
action. Supply every required field with no default through the form, or
through records.create.set and ask, for example name: $input.name with
ask: {name: text}. A Check finding for a missing control means add it in
this round. Record creation needs no run file unless the request calls for
an automation.`

const contextFilePage = 6 << 10

var schemaName = regexp.MustCompile(`^[a-z0-9-]+\.json$`)

type contextRequest struct {
	File   string `json:"file,omitempty"`
	Offset int    `json:"offset,omitempty"`
	Type   string `json:"type,omitempty"`
}

const maxContextSelectors = 6

func (in modelInput) context(files map[string]string, reads map[contextRequest]string, args json.RawMessage) (string, error) {
	var request struct {
		contextRequest
		Select []contextRequest
	}
	if json.Unmarshal(args, &request) != nil {
		return "", errors.New("choose a definition file or existing type by name")
	}
	opts := request.contextRequest
	if request.Select != nil {
		if opts != (contextRequest{}) || len(request.Select) == 0 || len(request.Select) > maxContextSelectors {
			return "", fmt.Errorf("choose one file or type, or between 1 and %d context selectors", maxContextSelectors)
		}
		type item struct {
			contextRequest
			Result  json.RawMessage `json:"result,omitempty"`
			Problem string          `json:"problem,omitempty"`
		}
		items := make([]item, 0, len(request.Select))
		for _, selector := range request.Select {
			result := item{contextRequest: selector}
			if (selector.File == "") == (selector.Type == "") {
				result.Problem = "choose one definition file or existing type"
			} else {
				raw, _ := json.Marshal(selector)
				text, err := in.context(files, reads, raw)
				switch {
				case err != nil:
					result.Problem = err.Error()
				case json.Valid([]byte(text)):
					result.Result = json.RawMessage(text)
				default:
					result.Result, _ = json.Marshal(text)
				}
			}
			items = append(items, result)
		}
		raw, err := json.Marshal(map[string][]item{"items": items})
		return string(raw), err
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
		if opts.File == schema.SampleFile {
			return "Existing example rows are withheld. Leave them in place unless the change needs new examples. Use reference for synthetic examples, or set-sample with invented values.", nil
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
	triggers, tools := schema.AutomationCatalog()
	triggers = slices.DeleteFunc(triggers, func(kind string) bool { return kind == "timer" })
	for connector, names := range tools {
		executable := slices.DeleteFunc(names, func(name string) bool {
			return !runs.NativeConnectorTool(connector + "." + name)
		})
		if len(executable) == 0 {
			delete(tools, connector)
		} else {
			tools[connector] = executable
		}
	}
	maps.Copy(tools, in.SourceOperations)
	raw, err := json.Marshal(struct {
		Automation     string              `json:"automation_guide"`
		Triggers       []string            `json:"trigger_kinds"`
		Connectors     map[string][]string `json:"connector_operations"`
		RecordPolicy   string              `json:"record_policy"`
		ScreenControls string              `json:"screen_controls"`
		Stage          string              `json:"stage"`
		Manifest       json.RawMessage     `json:"manifest,omitempty"`
		Plan           json.RawMessage     `json:"plan,omitempty"`
		Files          []string            `json:"files,omitempty"`
		Unchanged      []string            `json:"unchanged,omitempty"`
		Types          []string            `json:"existing_types,omitempty"`
		CommonBindings map[string]string   `json:"common_bindings,omitempty"`
		Using          []string            `json:"using,omitempty"`
		Report         *modelReport        `json:"report,omitempty"`
	}{
		automationGuide, triggers, tools, recordPolicyGuide, screenControlsGuide, in.Stage, manifest, plan, slices.Sorted(maps.Keys(files)),
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
		switch {
		case p.File == schema.SampleFile && p.Code != "sample_unavailable":
			p.Message = "Check the example file at this location. Use synthetic records and valid sample links."
		case p.Code == "run_failed" && strings.HasSuffix(p.Message, "a refs parameter needs record ids or a record query"):
			p.Message = "Give the refs parameter a query string or a list of record IDs. For a scheduled run, put the query string in the parameter's default."
		case p.Code == "run_failed":
			p.Message = "The automation could not finish on the example data. Check the reported run and step."
		default:
			p.Message = briefText(p.Message, 400)
		}
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
func modelTurn(ctx context.Context, model Completer, workspace string, in modelInput, types schema.TypeSet, author authoring, preflight func(map[string]string) schema.Report, progress func(map[string]string) error, look func(context.Context, string) ([]string, error), claim func(context.Context, Target) error, presence func() (*Presence, error)) (map[string]string, string, BuildCalls, error) {
	in.Definition = maps.Clone(in.Definition)
	plan, _ := schema.ParsePlan([]byte(in.Definition[schema.PlanFile]))
	work := plannedWork(in.Request, plan)
	if err := restoreReuse(in.Definition, in.Reuse, types); err != nil {
		return nil, "", BuildCalls{}, err
	}
	if err := work.apply(in.Definition); err != nil {
		return nil, "", BuildCalls{}, err
	}
	initial, err := in.context(in.Definition, map[contextRequest]string{}, json.RawMessage(`{}`))
	if err != nil {
		return nil, "", BuildCalls{}, err
	}
	// Share the vocabulary without copying every external type into a large
	// workspace's prompt. The same shapes are still available by name.
	typeShapes, err := json.Marshal(in.Types)
	if err != nil {
		return nil, "", BuildCalls{}, err
	}
	if len(typeShapes) > contextFilePage {
		typeShapes = nil
	}
	var catalog string
	var runExample string
	if in.Stage != Planning {
		if planned, _ := schema.ParsePlan([]byte(in.Definition[schema.PlanFile])); len(planned.Automation) > 0 {
			example, err := schema.BuilderExampleFile("draft-review", "runs/draft-thread.yaml")
			if err != nil {
				return nil, "", BuildCalls{}, err
			}
			runExample = string(example)
		}
		catalog, err = batchParts([]string{"add-view/board", "add-view/table", "add-view/list", "add-chart", "Form", "RecordPage", "Button", "Sheet"})
		if err != nil {
			return nil, "", BuildCalls{}, err
		}
	}
	raw, err := json.Marshal(struct {
		Request        string          `json:"request"`
		Target         *Target         `json:"target,omitempty"`
		Context        json.RawMessage `json:"context"`
		Parts          json.RawMessage `json:"parts,omitempty"`
		ExistingShapes json.RawMessage `json:"existing_shapes,omitempty"`
		RunExample     string          `json:"run_example,omitempty"`
		Archetypes     string          `json:"screen_archetypes"`
	}{in.Request, in.Target, json.RawMessage(initial), json.RawMessage(catalog), typeShapes, runExample, archetypeInstructions})
	if err != nil {
		return nil, "", BuildCalls{}, err
	}
	files := maps.Clone(in.Definition)
	var mu sync.Mutex
	var compilerErr error
	seen := map[string]bool{}
	reads := map[contextRequest]string{}
	var planSaved bool
	var planErr error
	execute := func(ctx context.Context, command string) (result string, err error) {
		verb, args, err := appcmd.Parse(command)
		if err != nil {
			mu.Lock()
			planErr = err
			mu.Unlock()
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
		if in.Stage == Planning && (verb == "recipe" || verb == "markup" || verb == "look" || verb == "set_custom_part" || verb == "set_custom_step" || verb == "use_kit_part" || verb == "use_kit_step") {
			return "", errors.New("This turn is for the plan. Save the outline and record types, then finish the turn so the person can review it.")
		}
		switch verb {
		case "context":
			return in.context(files, reads, args)
		case "common_types":
			raw, err := json.Marshal(schema.CommonTypes())
			return string(raw), err
		case "kit_steps":
			if author.store == nil {
				return "[]", nil
			}
			steps, err := author.store.KitSteps()
			if err != nil {
				return "", err
			}
			if steps == nil {
				steps = []store.KitStep{}
			}
			raw, err := json.Marshal(steps)
			return string(raw), err
		case "kit_parts":
			if author.store == nil {
				return "[]", nil
			}
			parts, err := author.store.KitParts()
			if err != nil {
				return "", err
			}
			raw, err := json.Marshal(parts)
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
			if (options.Example == "crm" || options.Example == "hiring" || options.Example == "draft-review") && definitionPath.MatchString(options.File) {
				raw, err := schema.BuilderExampleFile(options.Example, options.File)
				if err != nil {
					return "", errors.New("that worked example file is unavailable")
				}
				return string(raw), nil
			}
			if options.Schema == "custom-extensions" {
				return extensionReference, nil
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
			report := preflight(files)
			status := "definition_only"
			if slices.Contains(report.Codes(), "choose_reuse") {
				status = NeedsYou
			}
			out, err := json.Marshal(struct {
				Status string `json:"status"`
				*modelReport
			}{status, briefReport(&report)})
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
			if err := work.checkEdit(verb, args, files); err != nil {
				return "", err
			}
			next, err := author.edit(ctx, verb, files, types, args)
			if err != nil {
				if verb == "plan" {
					planErr = err
				}
				if errors.Is(err, errCompilerUnavailable) {
					compilerErr = err
				}
				return "", err
			}
			if err := restoreReuse(next, in.Reuse, types); err != nil {
				return "", err
			}
			nextWork := work
			nextWork.builtins = slices.Clone(work.builtins)
			if err := nextWork.apply(next); err != nil {
				return "", err
			}
			if err := progress(next); err != nil {
				return "", err
			}
			files, work = next, nextWork
			if verb == "plan" {
				planSaved = true
				compiled, _ := schema.ParsePlan([]byte(next[schema.PlanFile]))
				if len(compiled.Builtins) > 0 {
					settings, err := json.Marshal(compiled.Builtins)
					if err != nil {
						return "", err
					}
					return "Saved the plan. Selected built-ins and settings: " + string(settings), nil
				}
			}
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
shown above, then finish. The engine checks the draft automatically.`
	default:
		if in.Editing {
			prompt += `Apply only the requested change. If it adds an optional field,
use add-field with just {"type":"number"} for an optional number. Keep the
plan, screens and samples when the request leaves them alone. Then finish so the engine can check the draft.`
			break
		}
		prompt += `Keep planned types and common keys. Edit types only as requested or to bind types from using.
Complete the planned screens and samples before making refinements.
Set record permissions and compile one recipe for each missing plan screen,
using its exact screen id. Implement each planned run and its requested
entry point before Check. Keep existing screens unless asked to edit them.
Use markup only for a composition the request needs and recipes cannot do.
Write the sample rows and finish with a short summary. The engine runs Check,
tries the sample data and asks Desk to draw the screens after this turn.`
	}
	prompt += "\nThe check command only preflights the definition and reuse choice. The engine checks after this turn. Let the person choose reuse."
	if in.Continued {
		prompt += "\nThe previous turn saved progress. Read the supplied context and continue the unfinished work. Keep completed files."
	}
	if in.Stage != Planning {
		if in.OwnerNameAvailable {
			prompt += "\nThe owner's name is available to run templates as {{owner.name}}."
		} else {
			prompt += "\nNo owner name is available. Omit the owner sign-off from drafts."
		}
	}
	if look == nil {
		prompt += "\nThe desktop renderer is unavailable. Skip look."
	}
	if in.Headless {
		prompt += "\nHeadless build: propose matching types from using. Continue to Check. Apply confirms reuse."
	}
	limit := in.callBudget()
	bounded := &boundedModel{model: model, stage: in.Stage, stopAt: limit, stats: BuildCalls{Limit: limit}}
	run := func(prompt string, allowance time.Duration) (string, error) {
		pass := ctx
		if allowance > 0 {
			var cancel context.CancelFunc
			pass, cancel = context.WithTimeout(ctx, allowance)
			defer cancel()
			bounded.stopPass = cancel
		}
		reply, err := session.ScopedCommandTurn(pass, workspace, builderModelID(model), prompt, string(raw), bounded, execute)
		if allowance > 0 && (bounded.timedOut || errors.Is(pass.Err(), context.DeadlineExceeded)) {
			err = context.DeadlineExceeded
		}
		return reply, err
	}
	var allowance time.Duration
	if in.Stage == Planning {
		// Leave time for one correction before the synchronous plan request expires.
		allowance = planFirstPass
	}
	reply, err := run(prompt, allowance)
	if in.Stage == Planning {
		// The saved plan is the reply. Some providers end after the last tool
		// call or omit the closing text, even though the plan was written.
		timedOut := errors.Is(err, context.DeadlineExceeded)
		if planSaved && (err == nil || bounded.hit || bounded.incomplete || timedOut) {
			err = nil
		} else if !planSaved && ctx.Err() == nil && (err == nil || bounded.incomplete || timedOut) && !bounded.hit {
			reason := "the reply did not save a plan object"
			if planErr != nil {
				reason = planErr.Error()
			} else if timedOut {
				reason = "the model did not finish in time"
			} else if bounded.incomplete {
				reason = "the model returned an empty or truncated reply"
			}
			bounded.incomplete = false
			bounded.timedOut = false
			bounded.stopAt = min(bounded.stats.Limit, bounded.stats.Total+4)
			reply, err = run(prompt+"\nThe previous plan failed: "+reason+
				". Save the plan with codeaf app plan. If tool calls are unavailable, return the same {\"name\",\"plan\"} arguments as a single JSON object.", planCorrectionPass)
			if !planSaved && err == nil {
				fallback := strings.TrimSpace(reply)
				if body, ok := strings.CutPrefix(fallback, "```json"); ok {
					fallback = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(body), "```"))
				}
				if _, parseErr := execute(ctx, "codeaf app plan "+fallback); parseErr == nil {
					reply = "I outlined the app for review."
				} else {
					err = fmt.Errorf("the corrected reply did not contain a usable plan: %w", parseErr)
				}
			}
			if planSaved && (bounded.incomplete || bounded.hit || errors.Is(err, context.DeadlineExceeded)) {
				err = nil
			}
		}
		if errors.Is(err, context.DeadlineExceeded) {
			err = errors.New("the model did not finish in time")
		}
	}
	if bounded.hit && !(in.Stage == Planning && planSaved && err == nil) {
		err = errModelCallLimit
	}
	return files, reply, bounded.stats, errors.Join(err, compilerErr)
}

func builderModelID(model Completer) string {
	if named, ok := model.(interface{ ModelID() string }); ok {
		return named.ModelID()
	}
	return "app-worker"
}

const (
	planCallBudget    = 20
	buildCallBase     = 14
	callsPerType      = 2
	callsPerScreen    = 3
	callsPerRun       = 5
	callsPerExtension = 8
	revisionCalls     = 8
	maxBuildCalls     = 80
)

// Planning starts before the outline exists. Writing and repair budget for
// the plan at the start of the turn, so editing it cannot extend a live turn.
func (in modelInput) callBudget() int {
	if in.Stage == Planning {
		return planCallBudget
	}
	plan, _ := schema.ParsePlan([]byte(in.Definition[schema.PlanFile]))
	calls := buildCallBase + len(plan.Data)*callsPerType + len(plan.Screens)*callsPerScreen +
		len(plan.Automation)*callsPerRun + len(plan.Extensions)*callsPerExtension
	if in.Editing {
		// Revisions can add work that was absent from the kept plan.
		calls += revisionCalls
	}
	return min(maxBuildCalls, calls)
}

// BuildCalls counts each submitted completion once, using the work it returned.
type BuildCalls struct {
	Limit     int `json:"limit"`
	Total     int `json:"total"`
	Planning  int `json:"planning"`
	Lookups   int `json:"lookups"`
	Authoring int `json:"authoring"`
	Repairs   int `json:"repairs"`
	Other     int `json:"other"`
}

type boundedModel struct {
	model      Completer
	stats      BuildCalls
	mu         sync.Mutex
	stage      string
	stopAt     int
	hit        bool
	incomplete bool
	timedOut   bool
	stopPass   context.CancelFunc
}

var errModelCallLimit = errors.New("the Builder reached its model call limit")

func (b *boundedModel) CompleteWithMessages(ctx context.Context, messages []ai.Message, opts ...ai.Option) (*ai.Response, error) {
	b.mu.Lock()
	if b.stats.Total >= b.stopAt {
		b.hit = true
		b.mu.Unlock()
		return nil, errModelCallLimit
	}
	b.stats.Total++
	b.mu.Unlock()
	resp, err := b.model.CompleteWithMessages(ctx, messages, append(opts, ai.WithMaxTokens(12000))...)

	b.mu.Lock()
	defer b.mu.Unlock()
	if b.stopPass != nil && errors.Is(err, context.DeadlineExceeded) {
		// A planning timeout gets one clean correction pass. End this turn so
		// the generic session retry ladder cannot spend the correction time.
		b.timedOut = true
		b.stopPass()
	}
	category := &b.stats.Other
	if resp != nil && len(resp.Choices) > 0 {
		choice := resp.Choices[0]
		b.incomplete = b.incomplete || choice.FinishReason == "length" || len(choice.Message.Content) == 0 && len(choice.Message.ToolCalls) == 0
		for _, call := range choice.Message.ToolCalls {
			var args struct{ Command string }
			if json.Unmarshal([]byte(call.Function.Arguments), &args) != nil {
				b.incomplete = true
				continue
			}
			verb, _, err := appcmd.Parse(args.Command)
			if err != nil {
				continue
			}
			switch verb {
			case "context", "common_types", "parts", "kit", "reference", "mark", "presence":
				if category == &b.stats.Other {
					category = &b.stats.Lookups
				}
			default:
				// A batch containing both reads and edits counts as editing work.
				switch b.stage {
				case Planning:
					category = &b.stats.Planning
				case "repair":
					category = &b.stats.Repairs
				default:
					category = &b.stats.Authoring
				}
			}
		}
	} else if err == nil {
		b.incomplete = true
	}
	*category++
	return resp, err
}

func definition(files map[string]string) schema.Definition {
	def := schema.Definition{Files: make(map[string][]byte, len(files))}
	for path, text := range files {
		def.Files[path] = []byte(text)
	}
	return def
}
