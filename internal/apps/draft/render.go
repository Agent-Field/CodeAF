package draft

import (
	"encoding/json"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/spec"
)

// perRunDivisor derives a run's cap from the month's: a run may spend up to a
// fortieth of what the app may spend in a month, as in the golden CRM.
const perRunDivisor = 40

// Render turns a State into the app's definition files. It is pure: the same
// State and Defaults give the same bytes. A part no verb has made yet is left
// out, so a half draft renders to a half app; plan.json is always there.
func Render(st State, d Defaults) spec.Files {
	files := spec.Files{spec.PlanFile: marshalFile(planDoc(st, d))}
	if st.Name != "" && len(st.Records) > 0 {
		files[spec.AppFile] = marshalFile(appDoc(st, d))
	}
	for id, raw := range st.Screens {
		files[spec.ScreensDir+"/"+id+".json"] = withNewline(raw)
	}
	for id, body := range st.Runs {
		files[spec.RunsDir+"/"+id+".yaml"] = withNewline(body)
	}
	if st.Agent != nil {
		files[spec.AgentFile] = withNewline(st.Agent)
	}
	return files
}

// planFile and appFile add the $schema key the files carry on disk.
type planFile struct {
	Schema string `json:"$schema"`
	spec.Plan
}

// appFile also shadows capabilities so tools is always written, as an empty
// list when no tool is granted: the app definition spells it out.
type appFile struct {
	Schema       string   `json:"$schema"`
	Capabilities capsFile `json:"capabilities"`
	spec.App
}

type capsFile struct {
	Records    map[string]spec.Access    `json:"records"`
	Connectors map[string]spec.Connector `json:"connectors,omitempty"`
	AI         []string                  `json:"ai,omitempty"`
	Tools      []string                  `json:"tools"`
}

func planDoc(st State, d Defaults) planFile {
	plan := st.Plan
	plan.Ask = orElse(d.Ask, st.About)
	plan.Name = st.Name
	plan.Needs = "computed"
	plan.Data, plan.Automation, plan.Screens = nonNil(plan.Data), nonNil(plan.Automation), nonNil(plan.Screens)
	plan.Agent.ID = "agent"
	return planFile{Schema: spec.PlanSchemaURL, Plan: plan}
}

func nonNil(lines []spec.PlanLine) []spec.PlanLine {
	if lines == nil {
		return []spec.PlanLine{}
	}
	return lines
}

func appDoc(st State, d Defaults) appFile {
	app := spec.App{
		ID: d.AppID, Name: st.Name, Description: st.About, Space: d.Space, Version: 1, Kit: d.Kit,
		Agent:    spec.AgentRef{Name: agentName(st), File: spec.AgentFile, Model: d.AgentModel},
		Records:  st.Records,
		Triggers: st.Triggers,
		Commands: st.Commands,
		Budget:   budget(d),
	}
	return appFile{Schema: spec.AppSchemaURL, Capabilities: capabilities(st), App: app}
}

func agentName(st State) string {
	if doc, err := spec.ParseAgent(st.Agent); err == nil {
		return doc.Name
	}
	return st.Name + " agent"
}

func budget(d Defaults) spec.Budget {
	b := spec.Budget{Month: d.BudgetMonth, Currency: "USD", PerRun: d.BudgetMonth / perRunDivisor, AtCap: "ask"}
	if d.AgentModel != "" {
		b.Models = []string{d.AgentModel}
	}
	return b
}

// capabilities is derived, never typed: every record is read-write, the AI
// verbs follow from what the app does, and connectors come from `connect`.
func capabilities(st State) capsFile {
	caps := capsFile{Records: map[string]spec.Access{}, AI: aiVerbs(st), Tools: []string{}}
	for id := range st.Records {
		caps.Records[id] = spec.AccessReadWrite
	}
	if len(st.Connectors) > 0 {
		caps.Connectors = st.Connectors
	}
	return caps
}

// aiVerbs lists, in the schema's order, the AI abilities the draft uses: sort
// when a field or a screen asks the model to rank, draft and summarise when a
// run has an agent step.
func aiVerbs(st State) []string {
	var out []string
	if usesSort(st) {
		out = append(out, "sort")
	}
	if usesAgentStep(st) {
		out = append(out, "summarise", "draft")
	}
	return out
}

func usesSort(st State) bool {
	for _, rec := range st.Records {
		for _, f := range rec.Fields {
			if f.AI != "" {
				return true
			}
		}
	}
	for _, raw := range st.Screens {
		if strings.Contains(string(raw), `"`+spec.VerbAISort+`"`) {
			return true
		}
	}
	return false
}

func usesAgentStep(st State) bool {
	for _, body := range st.Runs {
		asJSON, err := spec.YAMLToJSON(body)
		if err != nil {
			continue
		}
		var run spec.Run
		if json.Unmarshal(asJSON, &run) == nil && hasAgentStep(run.Steps) {
			return true
		}
	}
	return false
}

func hasAgentStep(steps []*spec.Step) bool {
	for _, s := range steps {
		if s.Agent != "" || hasAgentStep(s.Steps) {
			return true
		}
	}
	return false
}

func marshalFile(v any) []byte {
	out, _ := json.MarshalIndent(v, "", "  ")
	return append(out, '\n')
}

func withNewline(b []byte) []byte {
	if len(b) > 0 && b[len(b)-1] == '\n' {
		return b
	}
	return append(append([]byte{}, b...), '\n')
}
