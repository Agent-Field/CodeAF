package draft

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/spec"
)

func init() {
	register("connect", connectDef())
	register("trigger", triggerDef())
	register("command", commandDef())
}

type connectArgs struct {
	name string
	conn spec.Connector
}

func connectDef() def[connectArgs] {
	return def[connectArgs]{
		Usage:  "codeaf app connect <connector> --can a,b [--approve b] [--as member|workspace] [--account A] [--channels c1,c2]",
		Values: []string{"can", "approve", "as", "account", "channels"},
		Decode: func(a argv, _ string) (connectArgs, error) {
			if err := a.exactly(1); err != nil {
				return connectArgs{}, err
			}
			return connectArgs{a.pos[0], spec.Connector{
				As: orElse(a.str("as"), "member"), Account: a.str("account"),
				Can: a.list("can"), Approve: a.list("approve"), Channels: a.list("channels"),
			}}, nil
		},
		Piece: func(c connectArgs) Piece { return Piece{Kind: "connect", ID: c.name} },
		Check: func(_ State, _ Entry, c connectArgs) error {
			if err := checkSlug("connector", c.name); err != nil {
				return err
			}
			switch {
			case len(c.conn.Can) == 0:
				return refuse("connect %s: --can needs at least one verb, for example --can read,draft,send", c.name)
			case c.conn.As != "member" && c.conn.As != "workspace":
				return refuse("connect %s: --as must be member or workspace, got %q", c.name, c.conn.As)
			}
			for _, v := range c.conn.Approve {
				if !contains(c.conn.Can, v) {
					return refuse("connect %s: --approve %q is not in --can (can: %s) — approve only verbs the app may use", c.name, v, strings.Join(c.conn.Can, ", "))
				}
			}
			return nil
		},
		Apply: func(st *State, c connectArgs) { st.Connectors[c.name] = c.conn },
		Line: func(_ State, c connectArgs) string {
			line := titleOf(c.name) + " · " + strings.Join(c.conn.Can, ", ")
			if len(c.conn.Approve) > 0 {
				line += " (" + strings.Join(c.conn.Approve, ", ") + " asks you first)"
			}
			return line
		},
	}
}

type triggerArgs struct{ trigger spec.Trigger }

func triggerDef() def[triggerArgs] {
	return def[triggerArgs]{
		Usage:  `codeaf app trigger <id> --run <run> (--schedule "<cron>" | --on record.created|record.changed|record.deleted --type <record> [--when <expr>] | --on connector --connector <name> --event <event>) [--param k=v]`,
		Values: []string{"run", "schedule", "on", "type", "when", "connector", "event", "param"},
		Decode: func(a argv, _ string) (triggerArgs, error) {
			if err := a.exactly(1); err != nil {
				return triggerArgs{}, err
			}
			params, err := keyValues(a.vals["param"])
			if err != nil {
				return triggerArgs{}, err
			}
			t := spec.Trigger{
				ID: a.pos[0], Run: a.str("run"), On: spec.TriggerKind(a.str("on")), Type: a.str("type"),
				When: a.str("when"), Cron: a.str("schedule"), Connector: a.str("connector"), Event: a.str("event"), Params: params,
			}
			if t.Cron != "" && t.On == "" {
				t.On = spec.TriggerSchedule
			}
			return triggerArgs{t}, nil
		},
		Piece: func(t triggerArgs) Piece { return Piece{Kind: "trigger", ID: t.trigger.ID} },
		Check: func(st State, _ Entry, t triggerArgs) error { return checkTrigger(st, t.trigger) },
		Apply: func(st *State, t triggerArgs) { st.Triggers = upsertTrigger(st.Triggers, t.trigger) },
		Line:  func(_ State, t triggerArgs) string { return "Trigger · " + t.trigger.ID + " starts " + t.trigger.Run },
	}
}

func checkTrigger(st State, t spec.Trigger) error {
	label := "trigger " + t.ID
	if issues := spec.ValidateDef(label, spec.AppSchemaURL, "trigger", asAny(t)); len(issues) > 0 {
		return refuseAll(issueLines(label, issues, "codeaf app trigger (usage above)"))
	}
	if st.Runs[t.Run] == nil {
		return refuse("%s: /run %q is not a run yet (runs: %s) — make it first with: codeaf app run %s < run.yaml", label, t.Run, ids(sortedKeys(st.Runs)), t.Run)
	}
	if t.Type != "" && st.Records[t.Type] == nil {
		return refuse("%s: /type %q is not a record of this app (records: %s)", label, t.Type, ids(sortedKeys(st.Records)))
	}
	if t.On == spec.TriggerConnector && st.Connectors[t.Connector].Can == nil {
		return refuse("%s: /connector %q is not connected — connect it first with: codeaf app connect %s --can …", label, t.Connector, t.Connector)
	}
	if t.On == spec.TriggerSchedule {
		return checkCron(label, t.Cron)
	}
	return nil
}

func upsertTrigger(list []spec.Trigger, t spec.Trigger) []spec.Trigger {
	for i := range list {
		if list[i].ID == t.ID {
			list[i] = t
			return list
		}
	}
	return append(list, t)
}

type commandArgs struct{ command spec.Command }

func commandDef() def[commandArgs] {
	return def[commandArgs]{
		Usage:  "codeaf app command <id> --label L [--for <record>] [--run <run>] [--confirm text] [--key k] [--param k=v] [< do-actions.json]",
		Values: []string{"label", "for", "run", "confirm", "key", "param"},
		Body:   true,
		Decode: func(a argv, body string) (commandArgs, error) {
			if err := a.exactly(1); err != nil {
				return commandArgs{}, err
			}
			params, err := keyValues(a.vals["param"])
			if err != nil {
				return commandArgs{}, err
			}
			c := spec.Command{
				ID: a.pos[0], Label: a.str("label"), For: a.str("for"), Run: a.str("run"),
				Confirm: a.str("confirm"), Key: a.str("key"), Params: params,
			}
			if strings.TrimSpace(body) != "" {
				if err := json.Unmarshal([]byte(body), &c.Do); err != nil {
					return commandArgs{}, refuse("command %s: the actions on stdin must be a JSON array of action objects: %v", c.ID, err)
				}
			}
			return commandArgs{c}, nil
		},
		Piece: func(c commandArgs) Piece { return Piece{Kind: "command", ID: c.command.ID} },
		Check: func(st State, _ Entry, c commandArgs) error { return checkCommand(st, c.command) },
		Apply: func(st *State, c commandArgs) { st.Commands = upsertCommand(st.Commands, c.command) },
		Line:  func(_ State, c commandArgs) string { return "Command · " + c.command.Label },
	}
}

func checkCommand(st State, c spec.Command) error {
	label := "command " + c.ID
	if issues := spec.ValidateDef(label, spec.AppSchemaURL, "command", asAny(c)); len(issues) > 0 {
		return refuseAll(issueLines(label, issues, "codeaf app command (usage above)"))
	}
	if c.Run != "" && st.Runs[c.Run] == nil {
		return refuse("%s: /run %q is not a run yet (runs: %s) — make it first with: codeaf app run %s < run.yaml", label, c.Run, ids(sortedKeys(st.Runs)), c.Run)
	}
	if c.For != "" && st.Records[c.For] == nil {
		return refuse("%s: /for %q is not a record of this app (records: %s)", label, c.For, ids(sortedKeys(st.Records)))
	}
	sink := &issueSink{file: label}
	for i, action := range c.Do {
		lintActionWrite(sink, st, action, c.For, fmt.Sprintf("/do/%d", i))
	}
	if len(sink.lines) > 0 {
		return refuseAll(sink.lines)
	}
	return nil
}

func upsertCommand(list []spec.Command, c spec.Command) []spec.Command {
	for i := range list {
		if list[i].ID == c.ID {
			list[i] = c
			return list
		}
	}
	return append(list, c)
}

// keyValues reads repeated k=v flags. A value that is valid JSON keeps its
// type (a list, a number); anything else, such as $selection.deals.ids, is text.
func keyValues(pairs []string) (map[string]any, error) {
	if len(pairs) == 0 {
		return nil, nil
	}
	out := map[string]any{}
	for _, pair := range pairs {
		k, v, ok := strings.Cut(pair, "=")
		if !ok || k == "" {
			return nil, usage("--param %q must look like name=value", pair)
		}
		var parsed any
		if json.Unmarshal([]byte(v), &parsed) != nil {
			parsed = v
		}
		out[k] = parsed
	}
	return out, nil
}

// cronFields are the five fields of a schedule and the range each may use.
var cronFields = []struct {
	name     string
	min, max int
}{{"minute", 0, 59}, {"hour", 0, 23}, {"day of month", 1, 31}, {"month", 1, 12}, {"day of week", 0, 7}}

func checkCron(label, expr string) error {
	parts := strings.Fields(expr)
	if len(parts) != len(cronFields) {
		return refuse("%s: /cron %q needs 5 fields (minute hour day-of-month month day-of-week), for example \"0 9 * * 1\"", label, expr)
	}
	for i, part := range parts {
		if err := cronField(part, cronFields[i].min, cronFields[i].max); err != nil {
			return refuse("%s: /cron %q: the %s field %q is invalid: %v", label, expr, cronFields[i].name, part, err)
		}
	}
	return nil
}

func cronField(field string, lo, hi int) error {
	for _, item := range strings.Split(field, ",") {
		rng, step, hasStep := strings.Cut(item, "/")
		if hasStep {
			if n, err := strconv.Atoi(step); err != nil || n < 1 {
				return fmt.Errorf("step %q must be a positive number", step)
			}
		}
		if rng == "*" {
			continue
		}
		from, to, isRange := strings.Cut(rng, "-")
		if err := cronBound(from, lo, hi); err != nil {
			return err
		}
		if isRange {
			if err := cronBound(to, lo, hi); err != nil {
				return err
			}
		}
	}
	return nil
}

func cronBound(text string, lo, hi int) error {
	n, err := strconv.Atoi(text)
	if err != nil || n < lo || n > hi {
		return fmt.Errorf("%q must be a number from %d to %d", text, lo, hi)
	}
	return nil
}
