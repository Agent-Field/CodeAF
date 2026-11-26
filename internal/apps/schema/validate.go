package schema

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/Agent-Field/codeaf/internal/apps/extensions"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Problem codes. They are stable: the Builder's Check matches on them and tests
// pin them, so a code is never renamed, only added.
const (
	CodeParse             = "parse"               // the file is not valid JSON or YAML
	CodeSchema            = "schema"              // the file breaks its JSON Schema
	CodeMissingFile       = "missing_file"        // a file the app needs is absent
	CodeTypeUnknown       = "type_unknown"        // a ref, binding, action or step names a type nobody has
	CodeTypeTaken         = "type_taken"          // the app makes a type the person already has from another app
	CodeUsesUnknown       = "uses_unknown"        // the app uses a type the person does not have
	CodeUsesFieldTaken    = "uses_field_taken"    // the app adds a field the used type already has
	CodeFieldUnknown      = "field_unknown"       // a title, column, set or sample names a field the type lacks
	CodeFieldNotEnum      = "field_not_enum"      // a board, meter or Move asks for an enum and gets something else
	CodeFieldNotAI        = "field_not_ai"        // ai.sort fills a field that is not an AI field
	CodeDefaultNotValue   = "default_not_value"   // an enum default is not one of its values
	CodeCapabilityUnknown = "capability_unknown"  // capabilities name a type the app neither owns nor uses
	CodeNotAllowed        = "not_allowed"         // something reads or writes beyond the app's capabilities
	CodeRunUnknown        = "run_unknown"         // a trigger, command or action names a run that has no file
	CodeCommandUnknown    = "command_unknown"     // a RecordPage lists a command app.json lacks
	CodeCommandEmpty      = "command_empty"       // a command neither starts a run nor does anything
	CodeDuplicateID       = "duplicate_id"        // two triggers, commands, nodes or steps share an id
	CodeTriggerV0         = "trigger_v0"          // a trigger kind v0 does not run
	CodeTriggerShape      = "trigger_shape"       // a trigger lacks the cron or type its kind needs
	CodePartUnknown       = "part_unknown"        // a screen names a part the kit does not have
	CodePartProps         = "part_props"          // a part's props break that part's schema
	CodePartChildren      = "part_children"       // a part that takes no children has some
	CodeLabelMissing      = "label_missing"       // an interactive node has no label
	CodeNavTwice          = "nav_twice"           // a screen has more than one SectionTabs
	CodeBindNotAccepted   = "bind_not_accepted"   // a part is given a binding it cannot use
	CodeBindTypeUnknown   = "bind_type_unknown"   // a record binding whose type cannot be worked out
	CodeEventUnknown      = "event_unknown"       // `on` names an event the part never fires
	CodeActionVerb        = "action_verb"         // an action has no verb or more than one
	CodeOpenUnknown       = "open_unknown"        // open, or a nav item, names no screen or type
	CodeListUnknown       = "list_unknown"        // a BulkBar, QueryField or ai.sort names no RecordList
	CodeSelectionUnknown  = "selection_unknown"   // $selection.<id> names no list on the screen
	CodeParamUndeclared   = "param_undeclared"    // $params.x on a screen that does not declare x
	CodeBoardByMissing    = "board_by_missing"    // a RecordList offers a board without saying by what
	CodeScreenIDMismatch  = "screen_id_mismatch"  // the screen's id differs from its file name
	CodeRunIDMismatch     = "run_id_mismatch"     // the run's id differs from its file name
	CodeStepKind          = "step_kind"           // a step does nothing, or two things, or a v0-unknown thing
	CodeShellRefused      = "shell_refused"       // v0 runs never use a shell
	CodeWorkspaceRefused  = "workspace_refused"   // v0 runs never check out a repository
	CodeLoopShape         = "loop_shape"          // a loop with no list or condition, or no steps
	CodeLoopMax           = "loop_max"            // a loop with no max
	CodeAgentUnknown      = "agent_unknown"       // an agent step names an agent that is not the app's
	CodeToolNotAllowed    = "tool_not_allowed"    // a tool the app's capabilities do not list
	CodeSendGateMissing   = "send_gate_missing"   // something leaves the computer without an approval
	CodeStepTypeUnknown   = "step_type_unknown"   // a records step whose type cannot be worked out
	CodeStepRefUnknown    = "step_ref_unknown"    // a run step reference cannot resolve
	CodeWaitV0            = "wait_v0"             // a wait on an outside event, which v0 does not have
	CodeRunBudgetOver     = "run_budget_over"     // a run may spend more than the app allows
	CodePlanUnknownID     = "plan_unknown_id"     // a plan line names something the files do not have
	CodePlanMissingLine   = "plan_missing_line"   // the files have something the plan card never shows
	CodeSampleUnknownType = "sample_unknown_type" // sample rows of a type the app does not have
	CodeSampleBadValue    = "sample_bad_value"    // a sample value that is not one of its field's values
	CodeSampleRefMissing  = "sample_ref_missing"  // a sample ref to a row that is not in the sample
	CodeSampleIDMissing   = "sample_id_missing"   // a sample row without an id
	CodeFileNotAllowed    = "file_not_allowed"    // a file an app may not have: outside the folder, a link, or unknown
)

// Problem is one thing wrong with an app's files, in words a person can act on.
// File is the path inside the app folder; Path is a JSON Pointer into it when the
// problem has a place.
type Problem struct {
	File    string `json:"file"`
	Path    string `json:"path,omitempty"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Report is the outcome of Validate. An empty report means the files may be
// loaded and run.
type Report struct {
	Problems []Problem `json:"problems"`
	Look     string    `json:"look,omitempty"`
}

// OK reports whether nothing is wrong.
func (r Report) OK() bool { return len(r.Problems) == 0 }

// Codes lists each problem's code in order, for tests and logs.
func (r Report) Codes() []string {
	out := make([]string, len(r.Problems))
	for i, p := range r.Problems {
		out[i] = p.Code
	}
	return out
}

// LookNote is the display line for a visual check outcome.
func (r Report) LookNote() string {
	if r.Look == "" {
		return ""
	}
	return "look: " + r.Look
}

// String renders each problem and the visual check outcome on separate lines.
func (r Report) String() string {
	var b strings.Builder
	for _, p := range r.Problems {
		fmt.Fprintf(&b, "%s%s: %s (%s)\n", p.File, p.Path, p.Message, p.Code)
	}
	if note := r.LookNote(); note != "" {
		b.WriteString(note + "\n")
	}
	return b.String()
}

// outbound is the set of tool verbs that leave the person's computer. Each one must
// wait for the person's approval: a gate on a run step, an `approve` entry on the
// connector. THIS IS A LAW: v0 never sends anything nobody said yes to.
var outbound = map[string]bool{"send": true, "post": true, "reply": true, "pay": true, "publish": true, "invite": true, "charge": true, "transfer": true, "refund": true, "payment": true}

// systemFields are fields every record has without the app declaring them.
var systemFields = map[string]bool{"id": true, "created_at": true, "updated_at": true}

var (
	selectionRef  = regexp.MustCompile(`\$selection\.([a-z][a-z0-9_-]*|[0-9A-HJKMNP-TV-Z]{26})`)
	paramsRef     = regexp.MustCompile(`\$params\.([a-z][a-z0-9_]*)`)
	stepsRef      = regexp.MustCompile(`\$steps\.([a-z][a-z0-9_-]*)`)
	durationText  = regexp.MustCompile(`^[0-9]+(s|m|h|d|w)$`)
	cronField     = regexp.MustCompile(`^[0-9*,/\-A-Za-z]+$`)
	fileIDPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,47}$`)
)

// checker carries what every check needs: the parsed app, the types it can see,
// and the report being written.
type checker struct {
	def       Definition
	kit       Kit
	custom    map[string]customPart
	sampleIDs map[string]map[string]bool
	app       App
	appOK     bool
	existing  TypeSet
	screens   map[string]bool
	runs      map[string]bool
	commands  map[string]Command
	stepIDs   map[string]bool // the run being checked, for goto
	report    Report
}

func (c *checker) add(file, path, code, format string, args ...any) {
	c.report.Problems = append(c.report.Problems, Problem{File: file, Path: path, Code: code, Message: fmt.Sprintf(format, args...)})
}

// Validate checks an app's files: each against its JSON Schema first, then the
// semantic rules that a schema cannot say (refs resolve, capabilities cover every
// read and write, the kit law, v0's refusals). existing is every record type the
// person already has from other apps; `uses` types and cross-app refs resolve
// against it. Problems come back sorted by file so a report reads top to bottom.
func Validate(def Definition, existing TypeSet) Report {
	c := &checker{def: def, existing: existing, screens: map[string]bool{}, runs: map[string]bool{}, commands: map[string]Command{}}
	compiled := schemas()
	if compiled.err != nil {
		c.add("", "", CodeSchema, "codeaf's own app schemas do not load: %v", compiled.err)
		return c.report
	}
	version := kitVersion(def.Files[AppFile])
	c.kit = compiled.kits[0]
	appSchema, screenSchema, runSchema := compiled.app, compiled.screen, compiled.run
	if version == 2 {
		c.kit = compiled.kits[2]
		appSchema, screenSchema, runSchema = compiled.resources["app-2.json"], compiled.resources["screen-2.json"], compiled.resources["run-2.json"]
	}
	if c.def.Files == nil {
		c.def.Files = map[string][]byte{}
	}

	for _, f := range c.def.ScreenFiles() {
		c.screens[FileID(f)] = true
	}
	for _, f := range c.def.RunFiles() {
		if c.runs[FileID(f)] {
			c.add(f, "", CodeDuplicateID, "Two run files are called %q; keep one of runs/%s.yaml and runs/%s.yml.", FileID(f), FileID(f), FileID(f))
		}
		c.runs[FileID(f)] = true
	}

	if raw, ok := c.def.Files[AppFile]; !ok {
		c.add(AppFile, "", CodeMissingFile, "The app has no app.json, so it has no record types or capabilities.")
	} else if c.schemaCheck(AppFile, raw, appSchema) {
		app, err := ParseApp(raw)
		if err != nil {
			c.add(AppFile, "", CodeParse, "app.json could not be read: %v", err)
		} else {
			c.app, c.appOK = app, true
			for _, cmd := range app.Commands {
				c.commands[cmd.ID] = cmd
			}
		}
	}
	c.checkPaths()
	if c.appOK && version == 2 {
		c.loadCustomParts()
		for _, file := range c.def.under("custom/", ".step") {
			if _, err := extensions.Parse(c.def.Files[file]); err != nil {
				c.add(file, "", CodeManifest, "%v", err)
			}
		}
		c.indexSample()
	}
	if c.appOK {
		c.checkApp()
		c.checkContent()
	}

	var plan *Plan
	if raw, ok := c.def.Files[PlanFile]; !ok {
		c.add(PlanFile, "", CodeMissingFile, "The app has no plan.json, so the plan card has nothing to draw.")
	} else if c.schemaCheck(PlanFile, raw, compiled.plan) {
		if p, err := ParsePlan(raw); err == nil {
			plan = &p
		} else {
			c.add(PlanFile, "", CodeParse, "plan.json could not be read: %v", err)
		}
	}

	if len(c.screens) == 0 {
		c.add(ScreensDir, "", CodeMissingFile, "The app has no screens, so there is nothing to open.")
	}
	for _, f := range c.def.ScreenFiles() {
		raw := c.def.Files[f]
		if c.kit.Kit == 0 {
			raw = WithLegacyNodeIDs(raw)
		}
		if !c.schemaCheck(f, raw, screenSchema) {
			continue
		}
		s, err := ParseScreen(raw)
		if err != nil {
			c.add(f, "", CodeParse, "The screen could not be read: %v", err)
			continue
		}
		c.checkScreen(f, s)
	}

	for _, f := range c.def.RunFiles() {
		j, err := RunJSON(c.def.Files[f])
		if err != nil {
			c.add(f, "", CodeParse, "The run is not valid YAML: %v", err)
			continue
		}
		if !c.schemaCheck(f, j, runSchema) {
			continue
		}
		var r Run
		if err := json.Unmarshal(j, &r); err != nil {
			c.add(f, "", CodeParse, "The run could not be read: %v", err)
			continue
		}
		c.checkRun(f, r)
	}

	if c.appOK && plan != nil {
		c.checkPlan(*plan)
	}
	if c.appOK {
		if raw, ok := c.def.Files[SampleFile]; ok {
			c.checkSample(raw)
		}
	}

	sort.SliceStable(c.report.Problems, func(i, j int) bool {
		return fileRank(c.report.Problems[i].File) < fileRank(c.report.Problems[j].File)
	})
	return c.report
}

// fileRank orders a report the way a person reads an app: the definition, the
// plan, the screens, the runs, then everything else.
func fileRank(f string) string {
	switch {
	case f == AppFile:
		return "0"
	case f == PlanFile:
		return "1"
	case strings.HasPrefix(f, ScreensDir):
		return "2" + f
	case strings.HasPrefix(f, RunsDir):
		return "3" + f
	default:
		return "4" + f
	}
}

// schemaCheck parses raw as JSON and validates it, reporting what is wrong. It
// returns true only when the file is well formed and matches its schema; the
// semantic checks never run on a file that failed here, because their messages
// would be about a shape the file does not have.
func (c *checker) schemaCheck(file string, raw []byte, s interface{ Validate(any) error }) bool {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		c.add(file, "", CodeParse, "%s is not valid JSON: %v", file, err)
		return false
	}
	if err := s.Validate(v); err != nil {
		c.report.Problems = append(c.report.Problems, schemaProblems(file, err)...)
		return false
	}
	return true
}

// checkPaths accepts the fixed app files and the groups its kit declares.
// Every path must stay inside the app because the store copies checked files
// to an immutable version.
func (c *checker) checkPaths() {
	agent, agentOK := agentPathOf(c.def.Files[AppFile])
	screens, runs := c.def.ScreenFiles(), c.def.RunFiles()
	for _, name := range sortedKeys(c.def.Files) {
		clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(name)))
		switch {
		case name == "" || strings.HasPrefix(name, "/") || filepath.IsAbs(name) || strings.Contains(name, `\`):
			c.add(name, "", CodeFileNotAllowed, "%q is not a path inside the app folder.", name)
		case clean != name || clean == ".." || strings.HasPrefix(clean, "../"):
			c.add(name, "", CodeFileNotAllowed, "%q is not a plain path inside the app folder; write it as %q.", name, clean)
		case name == AppFile || name == PlanFile || name == SampleFile || (agentOK && name == agent):
		case c.kit.Kit == 2 && c.validGalleryPath(name):
		case contains(screens, name) || contains(runs, name):
			if !fileIDPattern.MatchString(FileID(name)) {
				c.add(name, "", CodeFileNotAllowed, "%s is not a usable name; a screen or run file is named <id>.json or <id>.yaml, with an id of lowercase letters, digits, - and _.", name)
			}
		case !agentOK && !strings.Contains(name, "/") && strings.HasSuffix(name, ".md"):
			// app.json already says its agent file name is wrong; one mistake is
			// one problem.
		default:
			c.add(name, "", CodeFileNotAllowed, "An app has no file called %s. It may have app.json, plan.json, sample.json, its agent file, screens/<id>.json and runs/<id>.yaml.", name)
		}
	}
	for _, link := range c.def.Links {
		c.add(link, "", CodeFileNotAllowed, "%s is a link to somewhere else. An app is plain files; put the file itself here.", link)
	}
}

// ---------- app.json ----------

// typeFields returns the fields a type has as this app sees it: the app's own
// declaration plus, for a used type, the owner's fields. The bool is false when no
// such type exists anywhere.
func (c *checker) typeFields(name string) (map[string]Field, bool) {
	t, own := c.app.Records[name]
	if own && !t.Uses {
		return t.Fields, true
	}
	ex, has := c.existing.ForRecord(name, t)
	if !has && !own {
		return nil, false
	}
	out := map[string]Field{}
	for f, d := range ex.Fields {
		out[f] = d
	}
	for f, d := range t.Fields {
		out[f] = d
	}
	return out, has || own
}

// knownType reports whether a type exists for this app to name.
func (c *checker) knownType(name string) bool {
	if _, ok := c.app.Records[name]; ok {
		return true
	}
	_, ok := c.existing[name]
	return ok
}

func (c *checker) checkApp() {
	a := c.app
	claimed := map[string]string{}
	for _, name := range sortedKeys(a.Records) {
		t := a.Records[name]
		at := "/records/" + name
		ex, exists := c.existing.ForRecord(name, t)
		if t.Common != "" {
			switch {
			case !HasCommonType(t.Common):
				c.add(AppFile, at+"/common", CodeSchema, "The kit has no common type %q.", t.Common)
			case ex.Common != "" && ex.Common != t.Common:
				c.add(AppFile, at+"/common", CodeTypeTaken, "%s records already use the common key %q.", name, ex.Common)
			case claimed[t.Common] != "":
				c.add(AppFile, at+"/common", CodeTypeTaken, "%s and %s both name common type %q.", claimed[t.Common], name, t.Common)
			}
			claimed[t.Common] = name
		}
		if t.Uses {
			if !exists {
				c.add(AppFile, at, CodeUsesUnknown, "%q uses the record type %q, but you have no %s records from another app. Make the type here instead, without \"uses\".", a.ID, name, name)
			} else {
				for _, f := range sortedKeys(t.Fields) {
					if _, have := ex.Fields[f]; have {
						c.add(AppFile, at+"/fields/"+f, CodeUsesFieldTaken, "%s already has a %q field from %s; use it as it is instead of adding it again.", name, f, ex.Owner)
					}
				}
			}
		} else if exists && ex.Owner != a.ID {
			c.add(AppFile, at, CodeTypeTaken, "You already have %s records from %s. Use them with \"uses\": true instead of making a second %s type.", name, ex.Owner, name)
		}
		fields, _ := c.typeFields(name)
		if t.Title != "" {
			if _, ok := fields[t.Title]; !ok {
				c.add(AppFile, at+"/title", CodeFieldUnknown, "The title of %s is %q, but %s has no such field.", name, t.Title, name)
			}
		}
		if p := t.Present; p != nil {
			check := func(slot, field string) {
				if field == "" {
					return
				}
				if _, ok := fields[field]; !ok {
					c.add(AppFile, at+"/present/"+slot, CodeFieldUnknown, "%s has no field %q to show here.", name, field)
				}
			}
			for _, slot := range [][2]string{{"title", p.Title}, {"trailing", p.Trailing}, {"state", p.State},
				{"avatar", p.Avatar}, {"money", p.Money}, {"link", p.Link}} {
				check(slot[0], slot[1])
			}
			for i, field := range p.Meta {
				check(fmt.Sprintf("meta/%d", i), field)
			}
		}
		for _, fname := range sortedKeys(t.Fields) {
			f := t.Fields[fname]
			fat := at + "/fields/" + fname
			if a.Kit == 2 {
				c.checkGalleryField(name, fname, f)
				continue
			}
			if (f.Type == "ref" || f.Type == "refs") && !c.knownType(f.To) {
				c.add(AppFile, fat+"/to", CodeTypeUnknown, "%s.%s links to %q, which is not a record type here or in your other apps.", name, fname, f.To)
			}
			if f.Type == "enum" && f.Default != nil {
				if s, ok := f.Default.(string); !ok || !contains(f.Values, s) {
					c.add(AppFile, fat+"/default", CodeDefaultNotValue, "%s.%s starts as %v, which is not one of its values (%s).", name, fname, f.Default, strings.Join(f.Values, ", "))
				}
			}
		}
		for i, fr := range t.Freeze {
			if a.Kit == 2 {
				c.checkRecordExpression(AppFile, fmt.Sprintf("%s/freeze/%d/when", at, i), name, fr.When)
			}
			for _, e := range append(append([]string{}, fr.Except...), fr.Field) {
				if e == "" {
					continue
				}
				if _, ok := fields[e]; !ok && !systemFields[e] {
					c.add(AppFile, fmt.Sprintf("%s/freeze/%d", at, i), CodeFieldUnknown, "A freeze rule on %s names %q, but %s has no such field.", name, e, name)
				}
			}
		}
	}

	for _, name := range sortedKeys(a.Capabilities.Records) {
		if _, ok := a.Records[name]; !ok {
			c.add(AppFile, "/capabilities/records/"+name, CodeCapabilityUnknown, "The app asks to reach %s records but neither makes nor uses that type. Add it under records, with \"uses\": true if it is another app's.", name)
		}
	}
	for _, name := range sortedKeys(a.Capabilities.Connectors) {
		con := a.Capabilities.Connectors[name]
		for _, verb := range con.Can {
			if a.Kit == 2 {
				c.checkConnectorDeclaration(name, verb, con)
				continue
			}
			if isOutbound(verb) && !contains(con.Approve, verb) {
				c.add(AppFile, "/capabilities/connectors/"+name, CodeSendGateMissing, "%s may %s, which leaves your computer, so %q must also be listed under approve.", name, verb, verb)
			}
		}
	}

	seen := map[string]bool{}
	for i, t := range a.Triggers {
		at := fmt.Sprintf("/triggers/%d", i)
		if seen[t.ID] {
			c.add(AppFile, at+"/id", CodeDuplicateID, "Two triggers are called %q.", t.ID)
		}
		seen[t.ID] = true
		if a.Kit == 2 {
			c.checkGalleryTrigger(i, at)
		} else {
			switch t.On {
			case "schedule":
				if fields := strings.Fields(t.Cron); len(fields) != 5 || !allMatch(fields, cronField) {
					c.add(AppFile, at+"/cron", CodeTriggerShape, "The trigger %q runs on a schedule but its cron %q is not five fields like \"0 9 * * 1-5\".", t.ID, t.Cron)
				}
			case "record.changed":
				if t.Type == "" {
					c.add(AppFile, at, CodeTriggerShape, "The trigger %q runs when a record changes but does not say which type.", t.ID)
				} else if !c.knownType(t.Type) {
					c.add(AppFile, at+"/type", CodeTypeUnknown, "The trigger %q watches %q, which is not a record type here.", t.ID, t.Type)
				} else if !c.canRead(t.Type) {
					c.add(AppFile, at+"/type", CodeNotAllowed, "The trigger %q watches %s records, which the app may not read. Add %q to capabilities.records.", t.ID, t.Type, t.Type)
				}
			default:
				c.add(AppFile, at+"/on", CodeTriggerV0, "The trigger %q starts on %q. This version of codeaf starts runs only on a schedule or when a record changes.", t.ID, t.On)
			}
		}
		if !c.runs[t.Run] {
			c.add(AppFile, at+"/run", CodeRunUnknown, "The trigger %q starts the run %q, but there is no runs/%s.yaml.", t.ID, t.Run, t.Run)
		}
	}

	seen = map[string]bool{}
	for i, cmd := range a.Commands {
		at := fmt.Sprintf("/commands/%d", i)
		if seen[cmd.ID] {
			c.add(AppFile, at+"/id", CodeDuplicateID, "Two commands are called %q.", cmd.ID)
		}
		seen[cmd.ID] = true
		if cmd.For != "" && !c.knownType(cmd.For) {
			c.add(AppFile, at+"/for", CodeTypeUnknown, "The command %q is for %q, which is not a record type here.", cmd.ID, cmd.For)
		}
		if cmd.Run == "" && len(cmd.Do) == 0 {
			c.add(AppFile, at, CodeCommandEmpty, "The command %q neither starts a run nor does anything.", cmd.ID)
		}
		if cmd.Run != "" && !c.runs[cmd.Run] {
			c.add(AppFile, at+"/run", CodeRunUnknown, "The command %q starts the run %q, but there is no runs/%s.yaml.", cmd.ID, cmd.Run, cmd.Run)
		}
		ctx := actionCtx{file: AppFile, rowType: cmd.For, selectionType: cmd.For}
		for j, act := range cmd.Do {
			c.checkAction(ctx, fmt.Sprintf("%s/do/%d", at, j), act)
		}
	}

	if a.Kit == 2 {
		c.checkGalleryApp()
	}
	if a.Agent != nil {
		if rel, ok := AgentPath(a.Agent.File); !ok {
			c.add(AppFile, "/agent/file", CodeFileNotAllowed, "The agent file %q must be a Markdown file inside the app folder.", a.Agent.File)
		} else if _, ok := c.def.Files[rel]; !ok {
			c.add(a.Agent.File, "", CodeMissingFile, "app.json names the agent file %q, but the app has no such file.", a.Agent.File)
		}
	}
}

func (c *checker) access(typ string) string {
	return c.app.Capabilities.Records[typ]
}

func (c *checker) canRead(typ string) bool  { a := c.access(typ); return a == "r" || a == "rw" }
func (c *checker) canWrite(typ string) bool { return c.access(typ) == "rw" }

// ---------- actions ----------

// actionCtx is what an action can lean on to work out which type it touches: the
// type of the row or record around it, the type of the lists on its screen, and,
// in a command, the type the command is for.
type actionCtx struct {
	views         map[string]QueryIR
	locals        map[string]string
	file          string
	rowType       string
	selectionType string            // what a bare $selection.ids means (commands)
	lists         map[string]string // RecordList id → type, on a screen
	sheets        map[string]bool
}

var verbs = []string{"open", "records.update", "records.create", "records.delete", "runs.start", "approve", "tools.call", "ai.sort", "toast"}

// resolveType works out which type an id expression names.
func (ctx actionCtx) resolveType(explicit, ids string) string {
	if explicit != "" {
		return explicit
	}
	if m := selectionRef.FindStringSubmatch(ids); m != nil {
		switch m[1] {
		case "ids", "id", "count":
			return ctx.selectionType
		default:
			return ctx.lists[m[1]]
		}
	}
	if strings.HasPrefix(ids, "$row") || strings.HasPrefix(ids, "$record") {
		return ctx.rowType
	}
	return ""
}

func (c *checker) checkAction(ctx actionCtx, at string, act Action) {
	if !c.appOK {
		return
	}
	if c.kit.Kit == 2 {
		c.checkGalleryAction(ctx, at, act)
		return
	}
	var found []string
	for _, v := range verbs {
		if _, ok := act[v]; ok {
			found = append(found, v)
		}
	}
	if len(found) != 1 {
		if len(found) == 0 {
			c.add(ctx.file, at, CodeActionVerb, "This action does nothing: it needs one of %s.", strings.Join(verbs, ", "))
		} else {
			c.add(ctx.file, at, CodeActionVerb, "This action does %s at once; each action does one thing, so split it into one action each.", strings.Join(found, " and "))
		}
		return
	}
	switch verb := found[0]; verb {
	case "open":
		target, _ := act["open"].(string)
		if !c.screens[target] && !c.knownType(target) {
			c.add(ctx.file, at+"/open", CodeOpenUnknown, "This opens %q, which is neither a screen nor a record type of the app.", target)
		}
	case "records.update", "records.delete", "records.create":
		body, _ := act[verb].(map[string]any)
		explicit, _ := body["type"].(string)
		ids, _ := body["ids"].(string)
		if ids == "" {
			ids, _ = body["id"].(string)
		}
		typ := explicit
		if verb != "records.create" {
			typ = ctx.resolveType(explicit, ids)
		}
		if typ == "" {
			c.add(ctx.file, at+"/"+verb, CodeTypeUnknown, "This %s cannot tell which record type it changes; add \"type\".", verb)
			return
		}
		fields, ok := c.typeFields(typ)
		if !ok {
			c.add(ctx.file, at+"/"+verb, CodeTypeUnknown, "This %s changes %q, which is not a record type here.", verb, typ)
			return
		}
		if !c.canWrite(typ) {
			c.add(ctx.file, at+"/"+verb, CodeNotAllowed, "This changes %s records, which the app may only read. It needs \"%s\": \"rw\" in capabilities.records.", typ, typ)
		}
		set, _ := body["set"].(map[string]any)
		for _, f := range sortedKeys(set) {
			if _, ok := fields[f]; !ok {
				c.add(ctx.file, at+"/"+verb+"/set/"+f, CodeFieldUnknown, "This sets %s.%s, but %s has no such field.", typ, f, typ)
			}
		}
		ask, _ := act["ask"].(map[string]any)
		for _, f := range sortedKeys(ask) {
			fd, ok := fields[f]
			if !ok {
				c.add(ctx.file, at+"/ask/"+f, CodeFieldUnknown, "This asks for %s.%s, but %s has no such field.", typ, f, typ)
			} else if ask[f] == "enum" && fd.Type != "enum" {
				c.add(ctx.file, at+"/ask/"+f, CodeFieldNotEnum, "This asks to pick %s.%s from a list, but it is a %s field, not a list of values.", typ, f, fd.Type)
			}
		}
	case "runs.start":
		run, _ := act["runs.start"].(string)
		if !c.runs[run] {
			c.add(ctx.file, at+"/runs.start", CodeRunUnknown, "This starts the run %q, but there is no runs/%s.yaml.", run, run)
		}
	case "tools.call":
		call, _ := act["tools.call"].([]any)
		con, _ := call[0].(string)
		verb, _ := call[1].(string)
		c.checkTool(ctx.file, at+"/tools.call", con+"."+verb, true)
	case "ai.sort":
		if !contains(c.app.Capabilities.AI, "sort") {
			c.add(ctx.file, at+"/ai.sort", CodeNotAllowed, "This asks AI to sort, which needs \"sort\" in capabilities.ai.")
		}
		list, _ := act["ai.sort"].(string)
		typ, ok := ctx.lists[list]
		if !ok {
			c.add(ctx.file, at+"/ai.sort", CodeListUnknown, "This sorts the list %q, but the screen has no RecordList with that id.", list)
			return
		}
		field, _ := act["field"].(string)
		fields, _ := c.typeFields(typ)
		if fd, ok := fields[field]; !ok || fd.AI == "" {
			c.add(ctx.file, at+"/field", CodeFieldNotAI, "AI can only fill a field that says what to ask with \"ai\"; %s.%s is not one.", typ, field)
		}
	}
}

// ParseSourceTool recognizes a typed source mutation. A regular connector
// named connections can still offer its own single-segment verbs.
func ParseSourceTool(tool string) (connection, write string, ok bool) {
	path, ok := strings.CutPrefix(tool, "connections.")
	if !ok {
		return "", "", false
	}
	connection, write, ok = strings.Cut(path, ".")
	return connection, write, ok && connection != "" && write != "" && !strings.Contains(write, ".")
}

// checkTool checks one connector verb ("gmail.send") or MCP tool name against the
// capabilities. In a screen, an outbound verb is allowed only when the connector
// lists it under approve, since a screen has no gate of its own.
func (c *checker) checkTool(file, at, tool string, fromScreen bool) {
	if _, _, ok := ParseSourceTool(tool); ok {
		if fromScreen {
			c.add(file, at, CodeSendGateMissing, "A source write starts a run through runs.start.")
		} else if !contains(c.app.Capabilities.Tools, tool) {
			c.add(file, at, CodeToolNotAllowed, "This source write needs the exact tool in capabilities.tools.")
		}
		return
	}
	con, verb, _ := strings.Cut(tool, ".")
	// Whether a call leaves the computer is decided before anything can wave it
	// through: an MCP tool listed whole under capabilities.tools has no approve
	// list of its own, so from a screen, which has no gate, it can never send.
	if fromScreen && isOutbound(tool) {
		cc, isConnector := c.app.Capabilities.Connectors[con]
		if !isConnector || !contains(cc.Approve, verb) {
			c.add(file, at, CodeSendGateMissing, "This would %s through %s without asking you first; a screen can only do that through a connector that lists %q under approve.", lastSegment(tool), con, verb)
		}
	}
	if contains(c.app.Capabilities.Tools, tool) || contains(c.app.Capabilities.Tools, con) {
		return
	}
	cc, ok := c.app.Capabilities.Connectors[con]
	if !ok {
		c.add(file, at, CodeToolNotAllowed, "This calls %s, but the app lists no %s connector or tool in its capabilities.", tool, con)
		return
	}
	if !contains(cc.Can, verb) {
		c.add(file, at, CodeToolNotAllowed, "This asks %s to %s, but the app may only %s with it.", con, verb, strings.Join(cc.Can, ", "))
		return
	}
}

// lastSegment is the verb of a dotted tool name: "slack.chat.post" posts.
func lastSegment(tool string) string {
	return tool[strings.LastIndex(tool, ".")+1:]
}

// isOutbound reports whether a tool name's verb leaves the computer, judged by its
// last segment so a longer name cannot hide a send in its middle.
//
// The verb is compared lowercased and trimmed, and each underscore-separated word
// of it counts, so "Send", "send " and "send_message" are all sends. The schemas
// already insist on lowercase verbs; this is the second lock on the same door.
func isOutbound(tool string) bool {
	if _, _, ok := ParseSourceTool(tool); ok {
		return true
	}
	verb := strings.ToLower(strings.TrimSpace(lastSegment(strings.TrimSpace(tool))))
	for _, word := range strings.Split(verb, "_") {
		if outbound[word] {
			return true
		}
	}
	return false
}

// ---------- screens ----------

var displayRecordID = regexp.MustCompile(`(?i)\b[0-7][0-9A-HJKMNP-TV-Z]{25}\b`)

func (c *checker) checkScreen(file string, s Screen) {
	if c.kit.Kit == 2 && displayRecordID.MatchString(s.Title) {
		c.add(file, "/title", "display_record_id", "Use the record title in the screen instead of a record id.")
	}
	id := FileID(file)
	if s.Screen != id {
		c.add(file, "/screen", CodeScreenIDMismatch, "The screen calls itself %q but its file is %s; the file name is the screen's id.", s.Screen, file)
	}
	declared := map[string]bool{}
	for _, p := range s.Params {
		declared[p] = true
	}
	ctx := actionCtx{file: file, lists: map[string]string{}, sheets: map[string]bool{}, views: s.Views}
	if c.kit.Kit == 2 {
		for _, id := range sortedKeys(s.Views) {
			c.checkQuery(file, "/views/"+id, s.Views[id])
		}
	}
	ids := map[string]bool{}
	navs := 0

	// The first pass learns every list on the screen and its type, so an action
	// above a list in the tree can still name it.
	var learn func(n *Node, at string)
	learn = func(n *Node, at string) {
		if n == nil {
			return
		}
		part, _ := c.findPart(n)
		if n.ID != "" {
			if ids[n.ID] {
				c.add(file, at+"/id", CodeDuplicateID, "Two parts on this screen are called %q.", n.ID)
			}
			ids[n.ID] = true
			if n.Part == "Sheet" {
				ctx.sheets[n.ID] = true
			}
			if part.Selection && n.Bind != nil && n.Bind.Records != "" {
				ctx.lists[n.ID] = n.Bind.Records
			}
		}
		if part.Navigation {
			navs++
		}
		// A params binding on a layout part declares params the same way the
		// screen's own list does, as A2 writes it on the root.
		if n.Bind != nil {
			for _, p := range n.Bind.Params {
				declared[p] = true
			}
		}
		for i, ch := range n.Children {
			learn(ch, fmt.Sprintf("%s/children/%d", at, i))
		}
	}
	learn(s.Nav, "/nav")
	learn(s.Body, "/body")
	if navs > 1 {
		c.add(file, "", CodeNavTwice, "The screen has %d SectionTabs; an app has one navigation row.", navs)
	}

	c.walkNode(ctx, s.Nav, "/nav", "")
	c.walkNode(ctx, s.Body, "/body", "")

	// References are checked over the whole file's text values, so a $selection
	// in a when, a prop, a query or an action all count.
	var raw any
	_ = json.Unmarshal(c.def.Files[file], &raw)
	for _, str := range stringsIn(raw) {
		for _, m := range selectionRef.FindAllStringSubmatch(str, -1) {
			if _, ok := ctx.lists[m[1]]; !ok {
				c.add(file, "", CodeSelectionUnknown, "%q reads the selection of %q, but the screen has no RecordList with that id.", str, m[1])
			}
		}
		for _, m := range paramsRef.FindAllStringSubmatch(str, -1) {
			if !declared[m[1]] {
				c.add(file, "", CodeParamUndeclared, "%q reads $params.%s, but the screen does not list %q in its params.", str, m[1], m[1])
			}
		}
	}
}

// walkNode checks one node and its children. rowType is the type a $row or
// $record inside it means.
func (c *checker) walkNode(ctx actionCtx, n *Node, at, rowType string) {
	if n == nil {
		return
	}
	part, propsSchema := c.findPart(n)
	if propsSchema == nil {
		c.add(ctx.file, at+"/part", CodePartUnknown, "%q is not registered for this screen. First-party parts: %s.", n.Part, strings.Join(c.kit.Names(), ", "))
		return
	}
	if c.kit.Kit == 2 {
		if displayRecordID.MatchString(n.Label) {
			c.add(ctx.file, at+"/label", "display_record_id", "Use a readable label or a link to the record title.")
		}
		for _, key := range []string{"title", "text", "empty"} {
			if value, ok := n.Props[key].(string); ok && displayRecordID.MatchString(value) {
				c.add(ctx.file, at+"/props/"+key, "display_record_id", "Use a readable value or a link to the record title.")
			}
		}
	}
	props := n.Props
	if props == nil {
		props = map[string]any{}
	}
	if err := propsSchema.Validate(jsonValue(props)); err != nil {
		for _, p := range schemaProblems(ctx.file, err) {
			p.Code = CodePartProps
			p.Path = at + "/props" + p.Path
			msg := strings.TrimPrefix(p.Message, ctx.file+": ")
			msg = strings.Replace(msg, "it cannot say", "cannot take", 1)
			msg = strings.Replace(msg, "that key is not part of this file's format", "it is not one of its props, and a screen never styles a part itself", 1)
			p.Message = n.Part + " " + msg
			c.report.Problems = append(c.report.Problems, p)
		}
	}
	if part.Children == "none" && len(n.Children) > 0 {
		c.add(ctx.file, at+"/children", CodePartChildren, "%s takes no parts inside it.", n.Part)
	}
	if part.Interactive && strings.TrimSpace(n.Label) == "" {
		c.add(ctx.file, at, CodeLabelMissing, "This %s can be acted on, so it needs a label: screen readers, tooltips and ⌘K say it.", n.Part)
	}
	for _, ev := range sortedKeys(n.On) {
		if !contains(part.Events, ev) {
			if len(part.Events) == 0 {
				c.add(ctx.file, at+"/on/"+ev, CodeEventUnknown, "%s has no events, so on.%s never happens.", n.Part, ev)
			} else {
				c.add(ctx.file, at+"/on/"+ev, CodeEventUnknown, "%s has no %q event; it has %s.", n.Part, ev, strings.Join(part.Events, ", "))
			}
		}
	}

	// The binding decides which type this node and its children talk about.
	// Everything from here on needs app.json; when it failed, its own problems
	// are the ones worth reading, so the screen does not echo them a dozen times.
	if !c.appOK {
		for i, ch := range n.Children {
			c.walkNode(ctx, ch, fmt.Sprintf("%s/children/%d", at, i), "")
		}
		return
	}
	typ := rowType
	if b := n.Bind; b != nil {
		kind := bindingKind(b)
		if kind == "" || !contains(part.Bindings, kind) {
			accepts := "no binding"
			if len(part.Bindings) > 0 {
				accepts = strings.Join(part.Bindings, " or ")
			}
			c.add(ctx.file, at+"/bind", CodeBindNotAccepted, "%s takes %s, not this binding.", n.Part, accepts)
		}
		switch kind {
		case "records":
			typ = b.Records
		case "record":
			inner := ctx
			inner.rowType = rowType
			typ = inner.resolveType(b.Type, b.Record)
			if typ == "" {
				c.add(ctx.file, at+"/bind", CodeBindTypeUnknown, "This shows the record %q but cannot tell its type; add \"type\" to the binding.", b.Record)
			}
		case "selection":
			typ = ctx.lists[b.Selection]
		}
		if typ != "" && (kind == "records" || kind == "record") {
			if !c.knownType(typ) {
				c.add(ctx.file, at+"/bind", CodeTypeUnknown, "This shows %q records, which is not a record type here.", typ)
				typ = ""
			} else if !c.canRead(typ) {
				c.add(ctx.file, at+"/bind", CodeNotAllowed, "This shows %s records, which the app may not read. Add %q to capabilities.records.", typ, typ)
			}
		}
	}
	if c.kit.Kit == 2 {
		typ = c.checkGalleryPart(ctx, n, part, at, typ)
	} else {
		typ = c.checkLegacyPart(ctx, n, part, at, typ)
	}

	actx := ctx
	actx.rowType = typ
	for _, ev := range sortedKeys(n.On) {
		for i, act := range n.On[ev] {
			c.checkAction(actx, fmt.Sprintf("%s/on/%s/%d", at, ev, i), act)
		}
	}
	for i, ch := range n.Children {
		c.walkNode(ctx, ch, fmt.Sprintf("%s/children/%d", at, i), typ)
	}
}

// checkLegacyPart interprets the kit 0 descriptors while preserving the original
// error codes and the kit 0 form's create-versus-edit semantics.
func (c *checker) checkLegacyPart(ctx actionCtx, n *Node, part Part, at, typ string) string {
	for _, rule := range part.Rules {
		if rule.Check == "create-type" && (n.Bind == nil || n.Bind.Record == "") {
			t := asString(n.Props[rule.Path])
			if t == "" {
				c.add(ctx.file, at, CodeBindTypeUnknown, "%s needs a bound record or a type to create.", n.Part)
			} else if c.checkRecordAccess(ctx.file, at+"/props/"+rule.Path, t, "rw") {
				typ = t
			}
		}
		for _, value := range ruleValues(n.Props, rule.Path) {
			name, where := asString(value.value), at+"/props"+value.path
			switch rule.Check {
			case "field", "enum-field":
				if fields, haveType := c.typeFields(typ); haveType && name != "" {
					if f, ok := fields[name]; !ok && !systemFields[name] {
						c.add(ctx.file, where, CodeFieldUnknown, "%s has no field named %q.", typ, name)
					} else if ok && rule.Check == "enum-field" && f.Type != "enum" {
						c.add(ctx.file, where, CodeFieldNotEnum, "%s.%s is not an enum field.", typ, name)
					}
				}
			case "requires-for-value":
				match, requiredProp, _ := strings.Cut(rule.Options, "/")
				if name == match && asString(n.Props[requiredProp]) == "" {
					c.add(ctx.file, where, CodeBoardByMissing, "%s needs %s to show %s.", n.Part, requiredProp, match)
				}
			default:
				c.checkReference(ctx, where, rule.Check, name)
			}
		}
	}
	return typ
}

// ---------- runs ----------

func (c *checker) checkRun(file string, r Run) {
	id := FileID(file)
	if r.Run != id {
		c.add(file, "/run", CodeRunIDMismatch, "The run calls itself %q but its file is %s; the file name is the run's id.", r.Run, file)
	}
	if r.Budget != nil && c.app.Budget != nil {
		if c.app.Budget.PerRun > 0 && r.Budget.PerRun > c.app.Budget.PerRun {
			c.add(file, "/budget/per_run", CodeRunBudgetOver, "This run may spend $%.2f, more than the app's $%.2f a run.", r.Budget.PerRun, c.app.Budget.PerRun)
		} else if c.app.Budget.Month > 0 && r.Budget.PerRun > c.app.Budget.Month {
			c.add(file, "/budget/per_run", CodeRunBudgetOver, "This run may spend $%.2f, more than the app's $%.2f a month.", r.Budget.PerRun, c.app.Budget.Month)
		}
	}

	// $record in a run means the record whose change started it, when every
	// trigger that starts this run watches the same type.
	recordType := ""
	for _, t := range c.app.Triggers {
		recordTrigger := c.kit.Kit == 2 && t.Type != "" || c.kit.Kit == 0 && t.On == "record.changed"
		if t.Run == id && recordTrigger {
			if recordType == "" || recordType == t.Type {
				recordType = t.Type
			} else {
				recordType = ""
				break
			}
		}
	}

	stepIDs := map[string]bool{}
	textOutputs := map[string]bool{}
	var collect func(steps []Step, at string)
	collect = func(steps []Step, at string) {
		for i, s := range steps {
			sat := fmt.Sprintf("%s/%d", at, i)
			if stepIDs[s.ID] {
				c.add(file, sat+"/id", CodeDuplicateID, "Two steps are called %q.", s.ID)
			}
			stepIDs[s.ID] = true
			textOutputs[s.ID] = s.Agent != ""
			if len(s.AgentTools) > 0 && (s.Agent == "" || c.kit.Kit != 2 || len(c.app.Outputs) == 0) {
				c.add(file, sat+"/tools", CodeNotAllowed, "The render tool needs an agent step and declared outputs in a kit 2 app.")
			}
			collect(s.Steps, sat+"/steps")
		}
	}
	collect(r.Steps, "/steps")

	c.stepIDs = stepIDs
	loopVars := map[string]string{}
	if c.kit.Kit == 2 {
		c.checkGalleryRun(file, recordType)
	} else {
		c.checkSteps(file, r, r.Steps, "/steps", recordType, loopVars)
	}

	var raw any
	if j, err := RunJSON(c.def.Files[file]); err == nil {
		_ = json.Unmarshal(j, &raw)
	}
	c.checkRunReferences(file, raw, stepIDs, textOutputs)
}

func (c *checker) checkSteps(file string, r Run, steps []Step, at, recordType string, loopVars map[string]string) {
	for i, s := range steps {
		sat := fmt.Sprintf("%s/%d", at, i)
		if s.Shell != "" {
			c.add(file, sat+"/shell", CodeShellRefused, "The step %q runs a shell command. Apps in this version of codeaf never run shell commands; use a tool or an agent step.", s.ID)
		}
		if s.Workspace != "" {
			c.add(file, sat+"/workspace", CodeWorkspaceRefused, "The step %q checks out a repository (workspace: %s). Apps in this version of codeaf do not work in repositories.", s.ID, s.Workspace)
		}
		var kinds []string
		if s.AI != nil {
			kinds = append(kinds, "ai")
			c.checkAIStep(file, sat, r, s.AI)
		}
		if s.Agent != "" {
			kinds = append(kinds, "agent")
		}
		if s.Tool != "" {
			kinds = append(kinds, "tool")
		}
		if s.RecordsUpdate != nil {
			kinds = append(kinds, "records.update")
		}
		if s.RecordsCreate != nil {
			kinds = append(kinds, "records.create")
		}
		if s.Wait != nil {
			kinds = append(kinds, "wait")
		}
		if s.Loop != nil {
			kinds = append(kinds, "loop")
		}
		if len(kinds) == 0 && s.Gate == nil && s.Shell == "" {
			c.add(file, sat, CodeStepKind, "The step %q does nothing; give it one of agent, tool, records.update, records.create, gate, wait or loop.", s.ID)
		}
		if len(kinds) > 1 {
			c.add(file, sat, CodeStepKind, "The step %q is %s at once; split it into one step each.", s.ID, strings.Join(kinds, " and "))
		}
		if target, ok := strings.CutPrefix(s.OnError, "goto "); ok && !c.stepIDs[strings.TrimSpace(target)] {
			c.add(file, sat+"/on_error", CodeStepRefUnknown, "The step %q goes to %q when it fails, but the run has no step by that id.", s.ID, strings.TrimSpace(target))
		}
		if len(s.Steps) > 0 && s.Loop == nil {
			c.add(file, sat+"/steps", CodeStepKind, "The step %q has steps inside it but is not a loop.", s.ID)
		}

		if c.appOK && s.Agent != "" && (c.app.Agent == nil || c.app.Agent.Name != s.Agent) {
			name := "none"
			if c.app.Agent != nil {
				name = fmt.Sprintf("%q", c.app.Agent.Name)
			}
			c.add(file, sat+"/agent", CodeAgentUnknown, "The step %q asks %q, but the app's agent is %s.", s.ID, s.Agent, name)
		}
		if s.Tool != "" {
			if c.appOK {
				c.checkTool(file, sat+"/tool", s.Tool, false)
			}
			if isOutbound(s.Tool) && (s.Gate == nil || s.Gate.Approval != "send") {
				c.add(file, sat, CodeSendGateMissing, "The step %q uses %s, which leaves your computer, so it needs gate: {approval: send} to wait for your yes.", s.ID, s.Tool)
			}
		}
		if s.Wait != nil {
			if s.Wait.Thread != "" || (s.Wait.For != "" && !durationText.MatchString(s.Wait.For)) {
				c.add(file, sat+"/wait", CodeWaitV0, "The step %q waits for %q. This version of codeaf waits only for a length of time like 2h or 3d, or until a time.", s.ID, s.Wait.For)
			} else if s.Wait.For == "" && s.Wait.Until == "" {
				c.add(file, sat+"/wait", CodeWaitV0, "The step %q waits but does not say how long; give it for: 2h or until.", s.ID)
			}
		}
		for _, w := range []struct {
			verb string
			body *RecordsWrite
		}{{"records.update", s.RecordsUpdate}, {"records.create", s.RecordsCreate}} {
			if w.body == nil || !c.appOK {
				continue
			}
			typ := w.body.Type
			if typ == "" {
				ref := w.body.ID
				if ref == "" {
					ref = w.body.IDs
				}
				if strings.HasPrefix(ref, "$record") {
					typ = recordType
				} else if v, _, ok := strings.Cut(strings.TrimPrefix(ref, "$"), "."); ok {
					typ = loopVars[v]
				}
			}
			if typ == "" {
				c.add(file, sat+"/"+w.verb, CodeStepTypeUnknown, "The step %q cannot tell which record type it changes; add type.", s.ID)
				continue
			}
			fields, ok := c.typeFields(typ)
			if !ok {
				c.add(file, sat+"/"+w.verb, CodeTypeUnknown, "The step %q changes %q, which is not a record type here.", s.ID, typ)
				continue
			}
			if !c.canWrite(typ) {
				c.add(file, sat+"/"+w.verb, CodeNotAllowed, "The step %q changes %s records, which needs \"%s\": \"rw\" in capabilities.records.", s.ID, typ, typ)
			}
			for _, f := range sortedKeys(w.body.Set) {
				if _, ok := fields[f]; !ok {
					c.add(file, sat+"/"+w.verb+"/set/"+f, CodeFieldUnknown, "The step %q sets %s.%s, but %s has no such field.", s.ID, typ, f, typ)
				}
			}
		}

		inner := loopVars
		if s.Loop != nil {
			if s.Loop.Max <= 0 {
				c.add(file, sat+"/loop", CodeLoopMax, "The loop %q has no max. Every loop says how many rounds it may take, so a run can never go round forever.", s.ID)
			}
			if s.Loop.Over == "" && s.Loop.Until == "" {
				c.add(file, sat+"/loop", CodeLoopShape, "The loop %q needs either over (a list) or until (a condition).", s.ID)
			}
			if len(s.Steps) == 0 {
				c.add(file, sat+"/loop", CodeLoopShape, "The loop %q has no steps to repeat.", s.ID)
			}
			if s.Loop.As != "" {
				inner = map[string]string{}
				for k, v := range loopVars {
					inner[k] = v
				}
				if p, ok := strings.CutPrefix(s.Loop.Over, "$params."); ok {
					if rp, ok := r.Params[p]; ok && (rp.Type == "refs" || rp.Type == "ref") {
						inner[s.Loop.As] = rp.To
					}
				}
			}
		}
		c.checkSteps(file, r, s.Steps, sat+"/steps", recordType, inner)
	}
}

// ---------- plan.json ----------

func (c *checker) checkPlan(p Plan) {
	check := func(section string, lines []PlanLine, have map[string]bool, what string) {
		named := map[string]bool{}
		for i, l := range lines {
			named[l.ID] = true
			if !have[l.ID] {
				c.add(PlanFile, fmt.Sprintf("/%s/%d/id", section, i), CodePlanUnknownID, "The plan shows %s %q, but the app has no %s by that id.", what, l.ID, what)
			}
		}
		for _, id := range sortedKeys(have) {
			if !named[id] {
				c.add(PlanFile, "/"+section, CodePlanMissingLine, "The app has the %s %q, but the plan card never mentions it.", what, id)
			}
		}
	}
	types := map[string]bool{}
	for name := range c.app.Records {
		types[name] = true
	}
	check("data", p.Data, types, "record type")
	check("automation", p.Automation, c.runs, "run")
	check("screens", p.Screens, c.screens, "screen")
	seen := map[string]bool{}
	for i, capability := range p.Builtins {
		at := fmt.Sprintf("/builtins/%d", i)
		if seen[capability.ID] {
			c.add(PlanFile, at+"/id", CodeDuplicateID, "The plan uses the same built-in name twice.")
		}
		seen[capability.ID] = true
		if capability.Kind == BuiltinApproval {
			c.checkLocalApproval(at+"/run", capability.Run, p.Automation)
			continue
		}
		if capability.Kind != BuiltinCSV && capability.Kind != BuiltinLifecycle {
			c.add(PlanFile, at+"/kind", CodeSchema, "Choose a supported built-in.")
			continue
		}
		if c.runs[capability.ID] {
			c.add(PlanFile, at+"/id", CodeDuplicateID, "Keep the planned run and the built-in under separate names.")
		}
		at += "/record"
		if !types[capability.Record] {
			c.add(PlanFile, at, CodePlanUnknownID, "The plan's %s needs a declared record type.", capability.Kind)
			continue
		}
		if c.app.Capabilities.Records[capability.Record] != "rw" {
			c.add(PlanFile, at, CodeNotAllowed, "Allow editing %s to use the planned %s.", capability.Record, capability.Kind)
		}
		if capability.Kind == BuiltinLifecycle {
			lifecycle, ok := c.lifecycle(capability.Record)
			if !ok || len(lifecycle.Transitions) == 0 {
				c.add(PlanFile, at, CodePlanUnknownID, "Give %s a lifecycle with a state field and its planned transitions.", capability.Record)
			} else if _, ok := lifecycle.Transitions[capability.Transition]; capability.Transition != "" && !ok {
				c.add(PlanFile, at, CodePlanUnknownID, "Add the planned transition %q to the lifecycle for %s.", capability.Transition, capability.Record)
			}
		}
	}
}

func (c *checker) checkLocalApproval(at, id string, automation []PlanLine) {
	planned := false
	for _, line := range automation {
		planned = planned || line.ID == id
	}
	if !planned || !c.runs[id] {
		c.add(PlanFile, at, CodePlanUnknownID, "Choose the planned run whose record changes need your approval.")
		return
	}
	for _, path := range c.def.RunFiles() {
		if FileID(path) != id {
			continue
		}
		run, err := ParseRun(c.def.Files[path])
		if err != nil {
			return
		}
		writes, unguarded := localWriteApprovals(run.Steps)
		if !writes || unguarded {
			c.add(PlanFile, at, CodeNotAllowed, "Make %s ask for approval before saving its record changes.", id)
		}
		return
	}
}

func localWriteApprovals(steps []Step) (writes, unguarded bool) {
	for _, step := range steps {
		if step.IsLocalWrite() {
			writes = true
			unguarded = unguarded || step.Gate == nil
		}
		nestedWrites, nestedUnguarded := localWriteApprovals(step.Steps)
		writes = writes || nestedWrites
		unguarded = unguarded || nestedUnguarded
	}
	return writes, unguarded
}

// ---------- sample.json ----------

func (c *checker) checkSample(raw []byte) {
	var s Sample
	if err := json.Unmarshal(raw, &s); err != nil {
		c.add(SampleFile, "", CodeParse, "sample.json is not valid JSON: %v", err)
		return
	}
	ids := map[string]map[string]bool{}
	for _, typ := range sortedKeys(s.Records) {
		ids[typ] = map[string]bool{}
		for i, row := range s.Records[typ] {
			id, _ := row["id"].(string)
			if id == "" {
				c.add(SampleFile, fmt.Sprintf("/records/%s/%d", typ, i), CodeSampleIDMissing, "A sample %s has no id, so nothing can link to it.", typ)
				continue
			}
			if ids[typ][id] {
				c.add(SampleFile, fmt.Sprintf("/records/%s/%d/id", typ, i), CodeDuplicateID, "Two sample %s rows share the id %q.", typ, id)
			}
			ids[typ][id] = true
		}
	}
	for _, typ := range sortedKeys(s.Records) {
		fields, ok := c.typeFields(typ)
		if _, mine := c.app.Records[typ]; !ok || !mine {
			c.add(SampleFile, "/records/"+typ, CodeSampleUnknownType, "The sample has %s rows, but the app neither makes nor uses %s.", typ, typ)
			continue
		}
		for i, row := range s.Records[typ] {
			at := fmt.Sprintf("/records/%s/%d", typ, i)
			for _, f := range sortedKeys(row) {
				if systemFields[f] {
					continue
				}
				fd, ok := fields[f]
				if !ok {
					c.add(SampleFile, at+"/"+f, CodeFieldUnknown, "A sample %s sets %q, but %s has no such field.", typ, f, typ)
					continue
				}
				v := row[f]
				if c.kit.Kit == 2 {
					c.checkGalleryValue(SampleFile, at+"/"+f, fd, v, ids)
					continue
				}
				switch fd.Type {
				case "enum":
					if sv, ok := v.(string); !ok || !contains(fd.Values, sv) {
						c.add(SampleFile, at+"/"+f, CodeSampleBadValue, "A sample %s has %s %v, which is not one of %s.", typ, f, v, strings.Join(fd.Values, ", "))
					}
				case "ref", "refs":
					targets, known := ids[fd.To]
					if !known {
						continue
					}
					refs := []string{}
					if sv, ok := v.(string); ok {
						refs = append(refs, sv)
					} else {
						refs = stringList(v)
					}
					for _, ref := range refs {
						if !targets[ref] {
							c.add(SampleFile, at+"/"+f, CodeSampleRefMissing, "A sample %s links %s to %q, which is not a sample %s.", typ, f, ref, fd.To)
						}
					}
				}
			}
		}
	}
}

// ---------- helpers ----------

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func allMatch(list []string, re *regexp.Regexp) bool {
	for _, x := range list {
		if !re.MatchString(x) {
			return false
		}
	}
	return true
}

func stringList(v any) []string {
	items, _ := v.([]any)
	out := make([]string, 0, len(items))
	for _, it := range items {
		if s, ok := it.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// stringsIn returns every string value in a decoded JSON value, keys excluded.
func stringsIn(v any) []string {
	var out []string
	walkStrings(v, "", func(_, value string) { out = append(out, value) })
	return out
}

var pointerToken = strings.NewReplacer("~", "~0", "/", "~1")

func walkStrings(v any, path string, visit func(path, value string)) {
	switch value := v.(type) {
	case string:
		visit(path, value)
	case map[string]any:
		for _, key := range sortedKeys(value) {
			token := pointerToken.Replace(key)
			walkStrings(value[key], path+"/"+token, visit)
		}
	case []any:
		for i, item := range value {
			walkStrings(item, fmt.Sprintf("%s/%d", path, i), visit)
		}
	}
}

// jsonValue round-trips a decoded value through JSON with numbers kept as
// json.Number, the form the schema validator expects.
func jsonValue(v any) any {
	raw, err := json.Marshal(v)
	if err != nil {
		return v
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var out any
	if dec.Decode(&out) != nil {
		return v
	}
	return out
}
