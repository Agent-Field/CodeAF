package schema

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/extensions"
)

func (c *checker) checkGalleryTrigger(i int, at string) {
	trigger := c.app.Triggers[i]
	all, _ := object(c.def.Files[AppFile])["triggers"].([]any)
	value, _ := all[i].(map[string]any)
	if c.validateEntry(AppFile, at, CodeTriggerShape, "trigger/"+trigger.On, value) {
		ctx := actionCtx{file: AppFile}
		c.checkRules(ctx, at, c.kit.Registries.Triggers[trigger.On].Rules, value, false)
	}
	c.checkExpression(AppFile, at+"/when", trigger.When)
}

func (c *checker) checkGalleryAction(ctx actionCtx, at string, act Action) {
	c.checkEffect(ctx, at, "action", map[string]any(act), nil, c.kit.Registries.Actions, false)
}

func (c *checker) checkGalleryRun(file, recordType string) {
	def, _ := ParseRun(c.def.Files[file])
	c.checkGalleryAI(file, def, def.Steps, "/steps")
	raw, _ := RunJSON(c.def.Files[file])
	run := object(raw)
	params, _ := run["params"].(map[string]any)
	var walk func([]any, string, map[string]string, bool)
	walk = func(steps []any, at string, vars map[string]string, inRepo bool) {
		for i, value := range steps {
			step, _ := value.(map[string]any)
			sat := fmt.Sprintf("%s/%d", at, i)
			workspace := asString(step["workspace"])
			repo := asString(step["repo"])
			branch := asString(step["branch"])
			stepRepo := inRepo
			if workspace != "" || repo != "" || branch != "" {
				if workspace != "repo" || !repoName.MatchString(repo) || !validBranch(branch) {
					c.add(file, sat+"/workspace", CodeWorkspaceShape, "A repo workspace needs an owner/name repository and a branch name.")
				} else {
					stepRepo = true
				}
			}
			if target, ok := strings.CutPrefix(asString(step["on_error"]), "goto "); ok && !c.stepIDs[target] {
				c.add(file, sat+"/on_error", CodeStepRefUnknown, "There is no step named %q.", target)
			}
			ctx := actionCtx{file: file, rowType: recordType, locals: vars}
			c.wantStep(ctx, sat, step, stepRepo)
			c.checkExpression(file, sat+"/if", asString(step["if"]))
			if children, _ := step["steps"].([]any); len(children) != 0 {
				childVars := vars
				for kind, entry := range c.kit.Registries.Steps {
					if !contains(entry.Modifiers, "steps") {
						continue
					}
					config, _ := step[kind].(map[string]any)
					iter := asString(config["as"])
					if param, ok := strings.CutPrefix(asString(config["over"]), "$params."); ok && iter != "" {
						decl, _ := params[param].(map[string]any)
						childVars = make(map[string]string, len(vars)+1)
						for name, typ := range vars {
							childVars[name] = typ
						}
						childVars[iter] = asString(decl["to"])
					}
				}
				walk(children, sat+"/steps", childVars, stepRepo)
			}
		}
	}
	steps, _ := run["steps"].([]any)
	bindings := make(map[string]string, len(params))
	for name, value := range params {
		param, _ := value.(map[string]any)
		if typ := asString(param["to"]); typ != "" {
			bindings["params."+name] = typ
		}
	}
	walk(steps, "/steps", bindings, false)
}

func (c *checker) checkGalleryAI(file string, run Run, steps []Step, at string) {
	for i, step := range steps {
		path := fmt.Sprintf("%s/%d", at, i)
		if step.AI != nil {
			c.checkAIStep(file, path, run, step.AI)
		}
		c.checkGalleryAI(file, run, step.Steps, path+"/steps")
	}
}

func (c *checker) wantStep(ctx actionCtx, at string, step map[string]any, inRepo bool) {
	base := []string{"id", "title", "if", "on_error", "retry", "workspace", "repo", "branch"}
	c.checkEffect(ctx, at, "step", step, base, c.kit.Registries.Steps, inRepo)
}

func (c *checker) checkEffect(ctx actionCtx, at, group string, value map[string]any, base []string, entries map[string]EffectManifest, inRepo bool) {
	code := CodeActionVerb
	if group == "step" {
		code = CodeStepKind
	}
	var kinds, auxiliary []string
	for _, key := range sortedKeys(value) {
		entry, ok := entries[key]
		if !ok {
			continue
		}
		if entry.Auxiliary {
			auxiliary = append(auxiliary, key)
		} else {
			kinds = append(kinds, key)
		}
	}
	if len(kinds) > 1 || len(kinds) == 0 && len(auxiliary) != 1 {
		c.add(ctx.file, at, code, "Choose one registered %s kind.", group)
		return
	}
	allowed := map[string]bool{}
	for _, key := range base {
		allowed[key] = true
	}
	for _, key := range append(kinds, auxiliary...) {
		entry := entries[key]
		projected := map[string]any{key: value[key]}
		allowed[key] = true
		for _, mod := range entry.Modifiers {
			allowed[mod] = true
			if v, ok := value[mod]; ok {
				projected[mod] = v
			}
		}
		if c.validateEntry(ctx.file, at, code, group+"/"+key, projected) {
			c.checkRules(ctx, at, entry.Rules, value, inRepo)
		}
	}
	for _, key := range sortedKeys(value) {
		if !allowed[key] {
			c.add(ctx.file, at+"/"+key, code, "%q is not a modifier of this %s.", key, group)
		}
	}
}

func (c *checker) checkRules(ctx actionCtx, at string, rules []ManifestRule, value map[string]any, inRepo bool) {
	for _, rule := range rules {
		for _, selected := range ruleValues(value, rule.Path) {
			where := at + selected.path
			val := asString(selected.value)
			body, _ := selected.value.(map[string]any)
			switch rule.Check {
			case "workspace":
				if !inRepo {
					c.add(ctx.file, where, CodeShellRefused, "This step needs a repository workspace.")
				}
			case "output-ref":
				c.checkRender(ctx, where, body)
			case "record-write":
				c.checkRecordWrite(ctx, where, body)
			case "tool-call":
				args, _ := value["args"].(map[string]any)
				gate, _ := value["gate"].(map[string]any)
				c.checkOperation(ctx.file, where, val, args, asString(gate["approval"]), false)
			case "tool-tuple":
				tup, _ := selected.value.([]any)
				if len(tup) >= 2 {
					args := map[string]any{}
					if len(tup) == 3 {
						args, _ = tup[2].(map[string]any)
					}
					c.checkOperation(ctx.file, where, asString(tup[0])+"."+asString(tup[1]), args, "", true)
				}
			case "connector-event":
				c.checkEvent(ctx.file, where, asString(value["connector"]), val)
			case "wait":
				if event := asString(body["for"]); event != "" && !durationText.MatchString(event) {
					con, operation, _ := strings.Cut(event, ".")
					c.checkEvent(ctx.file, where, con, operation)
				}
			case "loop":
				c.checkExpression(ctx.file, where+"/until", asString(body["until"]))
			case "ai":
				if !contains(c.app.Capabilities.AI, rule.Options) {
					c.add(ctx.file, where, CodeNotAllowed, "The app does not have the %s AI capability.", rule.Options)
				}
			case "link":
				c.checkLink(ctx.file, where, body, false)
			case "reveal":
				if typ := asString(body["type"]); c.checkRecordAccess(ctx.file, where, typ, "r") {
					c.checkTaggedField(ctx.file, where+"/field", typ, asString(body["field"]), "masked")
				}
			case "expression":
				if typ := asString(value["record"]); typ != "" {
					c.checkRecordExpression(ctx.file, where, typ, val)
				} else {
					c.checkExpression(ctx.file, where, val)
				}
			case "transition-effect":
				c.checkTransitionEffect(ctx.file, where, body)
			case "ask-work":
				c.checkProcessRef(ctx.file, where, "work_items", asString(body["item"]), ctx.rowType)
			case "process-ref":
				c.checkProcessRef(ctx.file, where, rule.Options, val, ctx.rowType)
			case "work-effect":
				c.checkProcessRef(ctx.file, where, "work_items", asString(body["template"]), ctx.rowType)
			case "timer-effect":
				c.checkProcessRef(ctx.file, where, "timers", asString(body["timer"]), ctx.rowType)
			case "template-effect":
				c.checkProcessRef(ctx.file, where, "templates", asString(body["template"]), ctx.rowType)
			case "signoff-effect":
				if _, ok := c.policy(asString(body["policy"])); !ok {
					c.add(ctx.file, where, CodePolicy, "Choose an existing sign-off policy.")
				}
			case "masked-fields":
				c.checkTaggedField(ctx.file, where, asString(value["record"]), val, "masked")
			case "signoff":
				c.checkSignoff(ctx.file, where, body)
			case "checklist":
				c.checkChecklist(ctx.file, where, body, rule.Options)
			default:
				c.checkReference(ctx, where, rule.Check, val)
			}
		}
	}
}

func (c *checker) checkRecordWrite(ctx actionCtx, at string, body map[string]any) {
	id := asString(body["id"])
	if id == "" {
		id = asString(body["ids"])
	}
	typ := ctx.resolveType(asString(body["type"]), id)
	if name, _, ok := strings.Cut(strings.TrimPrefix(id, "$"), "."); typ == "" && ok {
		typ = ctx.locals[name]
	}
	if typ == "" {
		c.add(ctx.file, at, CodeStepTypeUnknown, "Name the record type this changes.")
		return
	}
	if !c.checkRecordAccess(ctx.file, at, typ, "rw") {
		return
	}
	fields, _ := c.typeFields(typ)
	set, _ := body["set"].(map[string]any)
	for _, key := range sortedKeys(set) {
		field, ok := fields[key]
		if !ok {
			c.add(ctx.file, at+"/set/"+key, CodeFieldUnknown, "%s has no field named %q.", typ, key)
			continue
		}
		if life, managed := c.lifecycle(typ); managed && life.Field == key {
			c.add(ctx.file, at+"/set/"+key, CodeNotAllowed, "Change %s.%s through a lifecycle transition.", typ, key)
		}
		if field.Computed != "" || contains(c.kit.Registries.Fields[field.Type].Tags, "policy-write") {
			c.add(ctx.file, at+"/set/"+key, CodeNotAllowed, "%s.%s is written by a rule.", typ, key)
		}
	}
}

func (c *checker) checkConnectorDeclaration(name, verb string, connector Connector) {
	if GraphSourceTrigger(name, verb) {
		return
	}
	at := "/capabilities/connectors/" + name
	op, ok := c.kit.Registries.Tools[name].Operations[verb]
	if !ok {
		c.add(AppFile, at, CodeToolNotAllowed, "%s.%s is not a registered operation.", name, verb)
	} else if approvalKind(op) != "" && !contains(connector.Approve, verb) {
		c.add(AppFile, at, CodeSendGateMissing, "%s.%s needs an approval.", name, verb)
	}
}

func approvalKind(op OperationManifest) string {
	if op.Outbound == "read" || op.Outbound == "event" {
		return ""
	}
	return op.Outbound
}

func (c *checker) checkOperation(file, at, tool string, args map[string]any, gate string, screen bool) {
	if id, ok := extensions.ToolID(tool); ok {
		raw := c.def.Files["custom/"+id+".step"]
		if len(raw) == 0 || screen || !contains(c.app.Capabilities.Tools, tool) {
			c.add(file, at, CodeToolNotAllowed, "Declare and grant %q as an automation tool.", tool)
			return
		}
		var step extensions.Step
		if json.Unmarshal(raw, &step) == nil {
			if len(step.Inputs) != len(args) {
				c.add(file, at, CodeStepKind, "Supply exactly the custom step's declared inputs.")
				return
			}
			for _, input := range step.Inputs {
				if _, exists := args[input.Name]; !exists {
					c.add(file, at, CodeStepKind, "Give the custom step %q.", input.Name)
				}
			}
		}
		return
	}
	con, verb, _ := strings.Cut(tool, ".")
	op, exists := c.kit.Registries.Tools[con].Operations[verb]
	need := c.app.Capabilities.Connectors[con]
	if !exists || op.Outbound == "event" || !contains(need.Can, verb) && !contains(c.app.Capabilities.Tools, tool) {
		c.add(file, at, CodeToolNotAllowed, "This app cannot call %q.", tool)
		return
	}
	if kind := approvalKind(op); kind != "" {
		if screen {
			c.add(file, at, CodeSendGateMissing, "Move %s to a run step with a %s approval gate.", tool, kind)
		} else if !contains(need.Approve, verb) || gate != kind {
			c.add(file, at, CodeSendGateMissing, "%s needs a %s approval.", tool, kind)
		}
	}
	if args == nil {
		args = map[string]any{}
	}
	c.validateEntry(file, at+"/args", CodeStepKind, "tool/"+con+"/"+verb, args)
}

func (c *checker) checkEvent(file, at, con, verb string) {
	op, exists := c.kit.Registries.Tools[con].Operations[verb]
	if !exists || op.Outbound != "event" || !contains(c.app.Capabilities.Connectors[con].Can, verb) {
		c.add(file, at, CodeToolNotAllowed, "%s.%s must be a registered inbound event allowed by the app.", con, verb)
	}
}

var repoName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*/[A-Za-z0-9][A-Za-z0-9_.-]*$`)
var branchTemplate = regexp.MustCompile(`\{\{[^{}]+\}\}`)

func validBranch(branch string) bool {
	name := branchTemplate.ReplaceAllString(branch, "value")
	if name == "" || strings.ContainsAny(name, " \t\r\n~^:?*[\\{}\x00") || strings.Contains(name, "..") || strings.Contains(name, "@{") || strings.HasSuffix(name, ".") {
		return false
	}
	for _, segment := range strings.Split(name, "/") {
		if segment == "" || strings.HasPrefix(segment, ".") || strings.HasSuffix(segment, ".lock") {
			return false
		}
	}
	return !strings.HasPrefix(name, "-") && name != "@"
}
