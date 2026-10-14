package draft

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/spec"
)

// The read verbs print the worker what it needs and change nothing. The worker
// reads the draft here, never from files.
func init() {
	add("show", Verb{Usage: "codeaf app show [plan|app|screen <id>|run <id>|agent]", Read: showVerb})
	add("schema", Verb{Usage: "codeaf app schema <screen|run|field|agent>", Read: schemaVerb})
	add("catalog", Verb{Usage: "codeaf app catalog", Read: catalogVerb})
}

func showVerb(st State, d Defaults, args []string) (string, error) {
	if len(args) == 0 {
		return overview(st), nil
	}
	files := Render(st, d)
	name, err := showFile(st, args)
	if err != nil {
		return "", err
	}
	data, ok := files[name]
	if !ok {
		return "", refuse("%s is not made yet — see what exists with: codeaf app show", name)
	}
	return strings.TrimRight(string(data), "\n"), nil
}

// showFile maps a show target to the file Render makes for it.
func showFile(st State, args []string) (string, error) {
	switch args[0] {
	case "plan":
		return spec.PlanFile, nil
	case "app":
		return spec.AppFile, nil
	case "agent":
		return spec.AgentFile, nil
	case "screen", "run":
		if len(args) != 2 {
			return "", usage("show %s needs an id — usage: codeaf app show %s <id>", args[0], args[0])
		}
		if args[0] == "screen" {
			return spec.ScreensDir + "/" + args[1] + ".json", nil
		}
		return spec.RunsDir + "/" + args[1] + ".yaml", nil
	}
	return "", usage("show what? usage: %s", registry["show"].Usage)
}

// overview is the one-screen summary of the draft.
func overview(st State) string {
	var b strings.Builder
	fmt.Fprintf(&b, "phase: %s\nname: %s\n", st.Phase, orElse(st.Name, "(not set)"))
	fmt.Fprintf(&b, "plan: %s data, %s, %s, agent %s\n", ids(planIDs(st.Plan.Data)), plural(len(st.Plan.Automation), "run", "runs"),
		plural(len(st.Plan.Screens), "screen", "screens"), yesNo(st.Plan.Agent.Line != ""))
	for _, id := range sortedKeys(st.Records) {
		fmt.Fprintf(&b, "record %s (title %s): %s\n", id, orElse(st.Records[id].Title, "unset"), ids(fieldOrder(st, id)))
	}
	fmt.Fprintf(&b, "screens: %s\nruns: %s\nagent: %s\n", ids(sortedKeys(st.Screens)), ids(sortedKeys(st.Runs)), yesNo(st.Agent != nil))
	fmt.Fprintf(&b, "connectors: %s\ntriggers: %s\ncommands: %s", ids(sortedKeys(st.Connectors)), ids(triggerIDs(st)), ids(commandIDs(st)))
	return b.String()
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func schemaVerb(_ State, _ Defaults, args []string) (string, error) {
	if len(args) != 1 {
		return "", usage("usage: %s", registry["schema"].Usage)
	}
	switch args[0] {
	case "screen":
		return schemaText(spec.ScreenSchemaURL)
	case "run":
		return schemaText(spec.RunSchemaURL)
	case "field":
		return fieldSchema()
	case "agent":
		return agentSchema, nil
	}
	return "", usage("schema of what? usage: %s", registry["schema"].Usage)
}

const agentSchema = `agent.md is a YAML front matter block and then the agent's instructions:

---
name: <the agent's name>
model: <model id, optional; codeaf fills the default>
tools: [records.query, records.get, records.create, records.update, <connector>.<verb>]
---
<instructions in plain sentences>

The tools list must include records.query and records.get for every record and
records.create, records.update and records.delete for every record the agent
may write, and <connector>.<verb> only for verbs granted with codeaf app connect.`

func schemaText(url string) (string, error) {
	raw, err := spec.SchemaJSON(url)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(raw)), nil
}

// fieldSchema prints the field definition out of app.json's schema, with the
// flags that fill it.
func fieldSchema() (string, error) {
	raw, err := spec.SchemaJSON(spec.AppSchemaURL)
	if err != nil {
		return "", err
	}
	var doc struct {
		Defs map[string]json.RawMessage `json:"$defs"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return "", err
	}
	out, _ := json.MarshalIndent(doc.Defs["field"], "", "  ")
	return registry["field"].Usage + "\n\n" + string(out), nil
}

func catalogVerb(_ State, _ Defaults, _ []string) (string, error) {
	kit, err := spec.LoadKit()
	if err != nil {
		return "", err
	}
	lines := []string{fmt.Sprintf("kit %d — one line per part: Name [bindings] props(type) events", kit.Kit)}
	for _, p := range kit.Parts {
		lines = append(lines, partLine(p))
	}
	lines = append(lines, "", "prop types:")
	for _, k := range sortedKeys(kit.PropTypes) {
		lines = append(lines, "  "+k+": "+kit.PropTypes[k])
	}
	return strings.Join(lines, "\n"), nil
}

func partLine(p *spec.Part) string {
	props := make([]string, 0, len(p.Props))
	for _, name := range sortedKeys(p.Props) {
		pr := p.Props[name]
		desc := name + "(" + pr.Type
		if len(pr.Values) > 0 {
			desc += ": " + strings.Join(pr.Values, "|")
		}
		if pr.Required {
			desc += ", required"
		}
		props = append(props, desc+")")
	}
	rule, names := p.ChildRule()
	if rule == "list" {
		rule = "children " + strings.Join(names, "|")
	} else {
		rule = "children " + rule
	}
	line := p.Name + " [" + strings.Join(p.Bind, ",") + "] " + strings.Join(props, " ") + " events:" + strings.Join(p.Events, ",") + " " + rule
	if len(p.Slots) > 0 {
		line += " slots:" + strings.Join(p.Slots, ",")
	}
	return line
}
