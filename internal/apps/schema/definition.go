package schema

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// The fixed file names of an app folder. Screens and runs are one file per id under
// their own folders; the file name is the id.
const (
	AppFile    = "app.json"
	PlanFile   = "plan.json"
	SampleFile = "sample.json"
	ScreensDir = "screens"
	RunsDir    = "runs"
)

// Definition is an app's files, keyed by their path inside the app folder with
// forward slashes ("app.json", "screens/deals.json", "runs/follow-up.yaml",
// "agent.md", "sample.json"). It is deliberately the raw bytes: the Builder writes
// bytes, the store keeps bytes, and only the validator decides whether they parse.
type Definition struct {
	Files map[string][]byte
	// Links are paths Load found as symbolic links and did not follow. An app is
	// plain files; a link could point anywhere on the computer, so Validate
	// refuses the app rather than read through it.
	Links []string
}

// AgentPath cleans an agent file name from app.json into the slash path it has in
// Files. It is false for a name that is absolute, climbs out of the app folder, or
// is not Markdown. Load and Validate both go through it so they agree on the name.
func AgentPath(name string) (string, bool) {
	if name == "" || filepath.IsAbs(name) || strings.HasPrefix(name, "/") || strings.Contains(name, `\`) {
		return "", false
	}
	rel := filepath.ToSlash(filepath.Clean(filepath.FromSlash(name)))
	if rel == ".." || strings.HasPrefix(rel, "../") || !strings.HasSuffix(rel, ".md") {
		return "", false
	}
	return rel, true
}

// agentPathOf is the agent file app.json names, or agent.md when it names none or
// cannot be read.
func agentPathOf(appRaw []byte) (string, bool) {
	var head struct {
		Agent *struct {
			File string `json:"file"`
		} `json:"agent"`
	}
	if json.Unmarshal(appRaw, &head) == nil && head.Agent != nil && head.Agent.File != "" {
		return AgentPath(head.Agent.File)
	}
	return "agent.md", true
}

// Load reads an app folder into a Definition. It reads app.json, plan.json,
// sample.json, every screens/*.json, every runs/*.yaml and the agent file app.json
// names (agent.md when it names none). A missing optional file is simply absent;
// Validate is what says a needed one is missing.
func Load(dir string) (Definition, error) {
	def := Definition{Files: map[string][]byte{}}
	read := func(rel string) error {
		full := filepath.Join(dir, filepath.FromSlash(rel))
		// Every folder on the way is checked too: Lstat looks only at the last
		// name, and a linked folder would carry the read outside the app.
		if linked, err := linkOnPath(dir, rel); err != nil {
			return err
		} else if linked {
			def.Links = append(def.Links, rel)
			return nil
		}
		info, err := os.Lstat(full)
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			def.Links = append(def.Links, rel)
			return nil
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		raw, err := os.ReadFile(full)
		if err != nil {
			return err
		}
		def.Files[rel] = raw
		return nil
	}
	for _, rel := range []string{AppFile, PlanFile, SampleFile} {
		if err := read(rel); err != nil {
			return def, err
		}
	}
	// The agent file is read only from inside the app folder: a name that climbs
	// out of it is left unread, and Validate says why.
	if agentFile, ok := agentPathOf(def.Files[AppFile]); ok {
		if err := read(agentFile); err != nil {
			return def, err
		}
	}
	groups := DefinitionGroups(kitVersion(def.Files[AppFile]))
	for _, group := range sortedKeys(groups) {
		exts := groups[group]
		if info, err := os.Lstat(filepath.Join(dir, group)); err == nil && info.Mode()&os.ModeSymlink != 0 {
			def.Links = append(def.Links, group)
			continue
		}
		entries, err := os.ReadDir(filepath.Join(dir, group))
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return def, err
		}
		for _, e := range entries {
			if e.IsDir() || !hasExt(e.Name(), exts) {
				continue
			}
			if err := read(group + "/" + e.Name()); err != nil {
				return def, err
			}
		}
	}
	return def, nil
}

// linkOnPath reports whether any folder between dir and rel (rel itself excluded)
// is a symbolic link. A folder that does not exist is no link: the read that
// follows simply finds nothing.
func linkOnPath(dir, rel string) (bool, error) {
	parts := strings.Split(rel, "/")
	cur := dir
	for _, p := range parts[:len(parts)-1] {
		cur = filepath.Join(cur, p)
		info, err := os.Lstat(cur)
		if err != nil {
			if os.IsNotExist(err) {
				return false, nil
			}
			return false, err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return true, nil
		}
	}
	return false, nil
}

func hasExt(name string, exts []string) bool {
	for _, x := range exts {
		if strings.HasSuffix(name, x) {
			return true
		}
	}
	return false
}

// ScreenFiles lists the screen files, sorted, with the id each file name gives.
func (d Definition) ScreenFiles() []string { return d.under(ScreensDir+"/", ".json") }

// RunFiles lists the run files, sorted.
func (d Definition) RunFiles() []string { return d.under(RunsDir+"/", ".yaml", ".yml") }

func (d Definition) under(prefix string, exts ...string) []string {
	var out []string
	for name := range d.Files {
		rest, ok := strings.CutPrefix(name, prefix)
		if ok && !strings.Contains(rest, "/") && hasExt(rest, exts) {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// FileID is the id a screen or run file's name gives it: the base name without
// its extension.
func FileID(path string) string {
	base := path[strings.LastIndex(path, "/")+1:]
	if i := strings.LastIndex(base, "."); i > 0 {
		base = base[:i]
	}
	return base
}

// App is app.json, typed.
type App struct {
	Roles        map[string]Role        `json:"roles,omitempty"`
	Audiences    map[string]Audience    `json:"audiences,omitempty"`
	Channels     map[string]Channel     `json:"channels,omitempty"`
	Rules        map[string]RoutingRule `json:"rules,omitempty"`
	Resources    map[string]Resource    `json:"resources,omitempty"`
	Process      *Process               `json:"process,omitempty"`
	Policies     []Policy               `json:"policies,omitempty"`
	Relations    map[string]Relation    `json:"relations,omitempty"`
	Exports      map[string][]string    `json:"exports,omitempty"`
	ID           string                 `json:"id"`
	Name         string                 `json:"name"`
	Icon         string                 `json:"icon,omitempty"`
	Version      int                    `json:"version"`
	Kit          int                    `json:"kit"`
	Agent        *AgentRef              `json:"agent,omitempty"`
	Records      map[string]RecordType  `json:"records"`
	Capabilities Capabilities           `json:"capabilities"`
	Triggers     []Trigger              `json:"triggers,omitempty"`
	Commands     []Command              `json:"commands,omitempty"`
	Budget       *Budget                `json:"budget,omitempty"`

	Outputs map[string]OutputTemplate `json:"outputs,omitempty"`
}

// AgentRef names the app's own agent and the Markdown file that instructs it.
type AgentRef struct {
	Name  string `json:"name"`
	File  string `json:"file"`
	Model string `json:"model,omitempty"`
}

// RecordType is one record type the app owns, or, with Uses set, a type another app
// owns that this app uses and adds its own fields to.
type RecordType struct {
	Policy    *RecordPolicy    `json:"policy,omitempty"`
	Common    string           `json:"common,omitempty"`
	Lifecycle *RecordLifecycle `json:"lifecycle,omitempty"`
	ID        string           `json:"id,omitempty"`
	Ledger    *Ledger          `json:"ledger,omitempty"`
	Title     string           `json:"title,omitempty"`
	Label     string           `json:"label,omitempty"`
	Uses      bool             `json:"uses,omitempty"`
	Fields    map[string]Field `json:"fields"`
	Constants map[string]any   `json:"constants,omitempty"`
	Freeze    []Freeze         `json:"freeze,omitempty"`
}

// Field is one field of a record type and its rules.
type Field struct {
	Class      DataClass  `json:"class,omitempty"`
	ID         string     `json:"id,omitempty"`
	Sequence   *Sequence  `json:"sequence,omitempty"`
	Type       string     `json:"type"`
	Timer      *TimerRule `json:"timer,omitempty"`
	Blind      *BlindRule `json:"blind,omitempty"`
	Label      string     `json:"label,omitempty"`
	Required   bool       `json:"required,omitempty"`
	Unique     bool       `json:"unique,omitempty"`
	Default    any        `json:"default,omitempty"`
	Index      bool       `json:"index,omitempty"`
	Masked     bool       `json:"masked,omitempty"`
	Computed   string     `json:"computed,omitempty"`
	AI         string     `json:"ai,omitempty"`
	AIOnChange bool       `json:"ai_on_change,omitempty"`
	Values     []string   `json:"values,omitempty"`
	To         string     `json:"to,omitempty"`
	Currency   string     `json:"currency,omitempty"`
}

// Freeze is one freeze rule: a condition, or a period that locks after a delay.
type Freeze struct {
	When   string   `json:"when,omitempty"`
	Except []string `json:"except,omitempty"`
	Period string   `json:"period,omitempty"`
	Field  string   `json:"field,omitempty"`
	After  string   `json:"after,omitempty"`
}

// Capabilities is everything the app may touch; the engine checks every call
// against it.
type Capabilities struct {
	Records    map[string]string    `json:"records"`
	Connectors map[string]Connector `json:"connectors,omitempty"`
	AI         []string             `json:"ai,omitempty"`
	Tools      []string             `json:"tools,omitempty"`
}

// Connector is one connector the app may call, the verbs it may use, and the verbs
// that wait for the person's approval.
type Connector struct {
	As       string   `json:"as,omitempty"`
	Account  string   `json:"account,omitempty"`
	Can      []string `json:"can"`
	Approve  []string `json:"approve,omitempty"`
	Channels []string `json:"channels,omitempty"`
}

// Trigger starts a run on a schedule or when a record changes.
type Trigger struct {
	ID        string `json:"id"`
	On        string `json:"on"`
	Cron      string `json:"cron,omitempty"`
	Type      string `json:"type,omitempty"`
	When      string `json:"when,omitempty"`
	Connector string `json:"connector,omitempty"`
	Event     string `json:"event,omitempty"`
	Run       string `json:"run"`
}

// Command is a ⌘K verb on a record type: it starts a run or does a list of actions.
type Command struct {
	ID      string         `json:"id"`
	Label   string         `json:"label"`
	For     string         `json:"for,omitempty"`
	Key     string         `json:"key,omitempty"`
	Run     string         `json:"run,omitempty"`
	Params  map[string]any `json:"params,omitempty"`
	Confirm string         `json:"confirm,omitempty"`
	Do      []Action       `json:"do,omitempty"`
}

// Action is one action in an `on` list, a command's `do`, or a BulkBar button. It
// stays a map because its one verb is a key ("records.update": {...}).
type Action map[string]any

// Budget is the app's spending limit.
type Budget struct {
	Month    float64  `json:"month,omitempty"`
	Currency string   `json:"currency,omitempty"`
	PerRun   float64  `json:"per_run,omitempty"`
	AtCap    string   `json:"at_cap,omitempty"`
	Models   []string `json:"models,omitempty"`
}

// Plan is plan.json, what the plan card draws.
type Plan struct {
	Ask        string          `json:"ask"`
	Data       []PlanLine      `json:"data"`
	Automation []PlanLine      `json:"automation,omitempty"`
	Extensions []PlanExtension `json:"extensions,omitempty"`
	Actions    []PlanLine      `json:"actions,omitempty"`
	Screens    []PlanLine      `json:"screens"`
	Agent      *PlanAgent      `json:"agent,omitempty"`
	Needs      string          `json:"needs,omitempty"`
}

// PlanExtension names an extension the Builder needs to author and why.
type PlanExtension struct {
	ID     string `json:"id"`
	Reason string `json:"reason"`
}

// PlanLine is one editable line of the plan card, naming the id it describes.
type PlanLine struct {
	ID     string `json:"id"`
	Line   string `json:"line"`
	Uses   bool   `json:"uses,omitempty"`
	Create bool   `json:"create,omitempty"`
}

// PlanAgent is the plan card's agent line.
type PlanAgent struct {
	Line string `json:"line"`
}

// Screen is one screens/<id>.json.
type Screen struct {
	Views  map[string]QueryIR `json:"views,omitempty"`
	Screen string             `json:"screen"`
	Title  string             `json:"title"`
	Params []string           `json:"params,omitempty"`
	Nav    *Node              `json:"nav,omitempty"`
	Body   *Node              `json:"body"`
}

// Node is one kit part in a screen tree.
type Node struct {
	Slot     string              `json:"slot,omitempty"`
	Part     string              `json:"part"`
	ID       string              `json:"id,omitempty"`
	Label    string              `json:"label,omitempty"`
	Props    map[string]any      `json:"props,omitempty"`
	Bind     *Bind               `json:"bind,omitempty"`
	On       map[string][]Action `json:"on,omitempty"`
	When     string              `json:"when,omitempty"`
	Children []*Node             `json:"children,omitempty"`
}

// Bind is where a node's data comes from. Exactly one of Records, Record,
// Selection, Run or Runs is the binding; Query and Type qualify it.
type Bind struct {
	File        string   `json:"file,omitempty"`
	Field       string   `json:"field,omitempty"`
	ContentPart string   `json:"content-part,omitempty"`
	PartID      string   `json:"part-id,omitempty"`
	View        string   `json:"view,omitempty"`
	Records     string   `json:"records,omitempty"`
	Query       string   `json:"query,omitempty"`
	Record      string   `json:"record,omitempty"`
	Type        string   `json:"type,omitempty"`
	Selection   string   `json:"selection,omitempty"`
	Run         string   `json:"run,omitempty"`
	Runs        string   `json:"runs,omitempty"`
	For         string   `json:"for,omitempty"`
	Params      []string `json:"params,omitempty"`
}

// Run is one runs/<id>.yaml.
type Run struct {
	Run    string              `json:"run"`
	Title  string              `json:"title,omitempty"`
	Budget *RunBudget          `json:"budget,omitempty"`
	Params map[string]RunParam `json:"params,omitempty"`
	Steps  []Step              `json:"steps"`
}

// RunBudget is the most one run may spend.
type RunBudget struct {
	PerRun float64 `json:"per_run,omitempty"`
}

// RunParam is one parameter a run takes.
type RunParam struct {
	Type    string `json:"type"`
	To      string `json:"to,omitempty"`
	Default any    `json:"default,omitempty"`
}

// Step is one step of a run. Its kind is whichever of Agent, Tool, RecordsUpdate,
// RecordsCreate, Wait, Loop or Gate it sets; a Gate may also sit on a Tool step,
// where it holds the tool until the person approves.
type Step struct {
	Render        *RenderAction  `json:"render,omitempty"`
	ID            string         `json:"id"`
	Title         string         `json:"title,omitempty"`
	If            string         `json:"if,omitempty"`
	OnError       string         `json:"on_error,omitempty"`
	Retry         *Retry         `json:"retry,omitempty"`
	AI            *AIStep        `json:"ai,omitempty"`
	Agent         string         `json:"agent,omitempty"`
	Prompt        string         `json:"prompt,omitempty"`
	Tool          string         `json:"tool,omitempty"`
	As            string         `json:"as,omitempty"`
	Args          map[string]any `json:"args,omitempty"`
	Gate          *Gate          `json:"gate,omitempty"`
	Wait          *Wait          `json:"wait,omitempty"`
	OnTimeout     string         `json:"on_timeout,omitempty"`
	Loop          *Loop          `json:"loop,omitempty"`
	Steps         []Step         `json:"steps,omitempty"`
	RecordsUpdate *RecordsWrite  `json:"records.update,omitempty"`
	RecordsCreate *RecordsWrite  `json:"records.create,omitempty"`
	Shell         string         `json:"shell,omitempty"`
	Workspace     string         `json:"workspace,omitempty"`
	Repo          string         `json:"repo,omitempty"`
	Branch        string         `json:"branch,omitempty"`
}

// AIStep asks one model or harness for typed data.
type AIStep struct {
	Prompt  string         `json:"prompt"`
	Tier    string         `json:"tier,omitempty"`
	Harness string         `json:"harness,omitempty"`
	Output  map[string]any `json:"output"`
	Propose string         `json:"propose,omitempty"`
	Example any            `json:"example,omitempty"`
}

// Retry repeats a failed step.
type Retry struct {
	Max     int    `json:"max,omitempty"`
	Backoff string `json:"backoff,omitempty"`
}

// Gate holds a run until the named person approves.
type Gate struct {
	Approval string `json:"approval"`
	Who      string `json:"who,omitempty"`
}

// Wait pauses a run for a duration or until a time.
type Wait struct {
	For     string `json:"for,omitempty"`
	Until   string `json:"until,omitempty"`
	Thread  string `json:"thread,omitempty"`
	Timeout string `json:"timeout,omitempty"`
}

// Loop repeats its steps over a list or until a condition, never more than Max.
type Loop struct {
	Over  string `json:"over,omitempty"`
	As    string `json:"as,omitempty"`
	Until string `json:"until,omitempty"`
	Max   int    `json:"max,omitempty"`
}

// RecordsWrite is a records.update or records.create step. Type may be left out of
// an update when the id makes it plain (a loop over a run param of refs, or
// $record in a run a record.changed trigger starts).
type RecordsWrite struct {
	Type string         `json:"type,omitempty"`
	ID   string         `json:"id,omitempty"`
	IDs  string         `json:"ids,omitempty"`
	Set  map[string]any `json:"set,omitempty"`
}

// Sample is sample.json: records to preview an app with, keyed by type. Each row
// carries its own id so refs between sample rows resolve.
type Sample struct {
	Links   []RecordLink                `json:"links,omitempty"`
	Records map[string][]map[string]any `json:"records"`
}

// ParseApp decodes app.json.
func ParseApp(raw []byte) (App, error) {
	var a App
	err := json.Unmarshal(raw, &a)
	return a, err
}

// ParseScreen decodes one screen file.
func ParseScreen(raw []byte) (Screen, error) {
	var s Screen
	err := json.Unmarshal(raw, &s)
	return s, err
}

// ParsePlan decodes plan.json.
func ParsePlan(raw []byte) (Plan, error) {
	var p Plan
	err := json.Unmarshal(raw, &p)
	return p, err
}

// ParseRun decodes one run file from YAML.
func ParseRun(raw []byte) (Run, error) {
	var r Run
	j, err := RunJSON(raw)
	if err != nil {
		return r, err
	}
	err = json.Unmarshal(j, &r)
	return r, err
}

// RunJSON converts a run's YAML into JSON, the form its schema checks. Going through
// JSON rather than validating the YAML value directly means a number is a number
// and a key is a string exactly as they would be had the file been JSON.
func RunJSON(raw []byte) ([]byte, error) {
	var v any
	if err := yaml.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	v, err := jsonable(v)
	if err != nil {
		return nil, err
	}
	return json.Marshal(v)
}

// jsonable turns what yaml.v3 decodes into values encoding/json accepts. yaml.v3
// already uses string keys for mappings whose keys are all strings; any other key
// is a mistake in a run file, said as one.
func jsonable(v any) (any, error) {
	switch t := v.(type) {
	case map[string]any:
		for k, x := range t {
			c, err := jsonable(x)
			if err != nil {
				return nil, err
			}
			t[k] = c
		}
		return t, nil
	case map[any]any:
		out := make(map[string]any, len(t))
		for k, x := range t {
			ks, ok := k.(string)
			if !ok {
				return nil, fmt.Errorf("a key must be words, not %v", k)
			}
			c, err := jsonable(x)
			if err != nil {
				return nil, err
			}
			out[ks] = c
		}
		return out, nil
	case []any:
		for i, x := range t {
			c, err := jsonable(x)
			if err != nil {
				return nil, err
			}
			t[i] = c
		}
		return t, nil
	default:
		return v, nil
	}
}

// ExistingType is a record type the person already has, owned by some app. The
// validator resolves `uses` types and cross-app refs against these.
type ExistingType struct {
	Common string `json:",omitempty"`
	ID     string
	Owner  string
	Fields map[string]Field
}

// TypeSet is every record type the person already has, by type name.
type TypeSet map[string]ExistingType

// TypesOf returns the types a definition owns, with their fields, so a caller can
// build the TypeSet another app is validated against. Used types are left out:
// they belong to the app that owns them.
func TypesOf(def Definition) TypeSet {
	out := TypeSet{}
	app, err := ParseApp(def.Files[AppFile])
	if err != nil {
		return out
	}
	for name, t := range app.Records {
		if t.Uses {
			continue
		}
		out[name] = ExistingType{ID: t.ID, Owner: app.ID, Common: t.Common, Fields: t.Fields}
	}
	return out
}
