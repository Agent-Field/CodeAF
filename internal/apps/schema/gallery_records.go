package schema

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync"

	"github.com/google/cel-go/cel"
)

type Policy struct {
	ID     string         `json:"id"`
	Kind   string         `json:"kind"`
	Config map[string]any `json:"config"`
}

type Relation struct {
	From  string `json:"from"`
	To    string `json:"to"`
	Label string `json:"label,omitempty"`
}

type RecordEndpoint struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

type RecordLink struct {
	Kind string         `json:"kind"`
	From RecordEndpoint `json:"from"`
	To   RecordEndpoint `json:"to"`
}

type TraceQuery struct {
	From      RecordEndpoint `json:"from"`
	Kinds     []string       `json:"kinds,omitempty"`
	Direction string         `json:"direction"`
	Depth     int            `json:"depth"`
}

type TimerRule struct {
	Field     string `json:"field"`
	Direction string `json:"direction"`
	Amber     string `json:"amber,omitempty"`
	Red       string `json:"red,omitempty"`
}

type BlindRule struct {
	Until string `json:"until"`
	Per   string `json:"per"`
}

var expressionEnv = sync.OnceValues(func() (*cel.Env, error) { return cel.NewEnv() })
var celDollar = regexp.MustCompile(`\$[a-zA-Z_][a-zA-Z0-9_]*`)

// Check parses CEL with the same parser as the store. Scope and function
// resolution belong to the runtime that supplies the record and run values.
func (c *checker) checkExpression(file, at, expr string) {
	if expr == "" {
		return
	}
	env, err := expressionEnv()
	if err != nil {
		c.add(file, at, CodeExpression, "%v", err)
		return
	}
	_, issues := env.Parse(celDollar.ReplaceAllStringFunc(expr, func(ref string) string { return ref[1:] }))
	if issues != nil && issues.Err() != nil {
		c.add(file, at, CodeExpression, "Invalid CEL expression: %v", issues.Err())
	}
}

// checkRecordExpression catches unknown names before a definition can reach
// the data store. The store supplies values, clock and cost limits at runtime.
func (c *checker) checkRecordExpression(file, at, typ, expr string) {
	if expr == "" {
		return
	}
	opts := []cel.EnvOption{
		cel.CrossTypeNumericComparisons(true),
		cel.Function("now", cel.Overload("now_void", nil, cel.TimestampType)),
		cel.Function("me", cel.Overload("me_void", nil, cel.StringType)),
		cel.Function("changed", cel.Overload("changed_string", []*cel.Type{cel.StringType}, cel.BoolType)),
		cel.Function("days_since", cel.Overload("days_since_dyn", []*cel.Type{cel.DynType}, cel.DoubleType)),
		cel.Function("count", cel.Overload("count_dyn", []*cel.Type{cel.DynType}, cel.DoubleType)),
		cel.Function("signed", cel.Overload("signed_id", []*cel.Type{cel.StringType}, cel.BoolType), cel.Overload("signed_void", nil, cel.BoolType)),
		cel.Function("open_work", cel.Overload("open_work_void", nil, cel.DoubleType)),
	}
	fields, _ := c.typeFields(typ)
	names := map[string]bool{}
	for name := range fields {
		names[name] = true
		names["record."+name] = true
		names["row."+name] = true
	}
	for name := range systemFields {
		names[name] = true
	}
	for name := range c.app.Records[typ].Constants {
		names[name] = true
	}
	for _, name := range []string{"params", "me", "now"} {
		names[name] = true
	}
	for _, name := range sortedKeys(names) {
		opts = append(opts, cel.Variable(name, cel.DynType))
	}
	env, err := cel.NewEnv(opts...)
	if err != nil {
		c.add(file, at, CodeExpression, "CEL environment: %v", err)
		return
	}
	_, issues := env.Compile(celDollar.ReplaceAllStringFunc(expr, func(ref string) string { return ref[1:] }))
	if issues != nil && issues.Err() != nil {
		c.add(file, at, CodeExpression, "Invalid record expression: %v", issues.Err())
	}
}

func (c *checker) indexSample() {
	c.sampleIDs = map[string]map[string]bool{}
	var sample Sample
	if json.Unmarshal(c.def.Files[SampleFile], &sample) != nil {
		return
	}
	for typ, rows := range sample.Records {
		c.sampleIDs[typ] = map[string]bool{}
		for _, row := range rows {
			c.sampleIDs[typ][asString(row["id"])] = true
		}
	}
}

func (c *checker) checkRecordAccess(file, at, typ, needs string) bool {
	if !c.knownType(typ) {
		c.add(file, at, CodeTypeUnknown, "The app has no record type %q.", typ)
		return false
	}
	if needs == "r" && c.canRead(typ) || needs == "rw" && c.canWrite(typ) {
		return true
	}
	c.add(file, at, CodeNotAllowed, "This needs %s access to %s records.", needs, typ)
	return false
}

func (c *checker) checkTaggedField(file, at, typ, name, tag string) {
	if name == "" || typ == "" {
		return
	}
	fields, ok := c.typeFields(typ)
	if !ok {
		return
	}
	f, ok := fields[name]
	if !ok && !systemFields[name] {
		c.add(file, at, CodeFieldUnknown, "%s has no field named %q.", typ, name)
		return
	}
	if tag == "" || tag == "masked" && f.Masked || tag == "clock" && !ok && name != "id" ||
		contains(c.kit.Registries.Fields[f.Type].Tags, tag) {
		return
	}
	c.add(file, at, CodePartProps, "%s.%s cannot be used where a %s field is needed.", typ, name, tag)
}

func (c *checker) checkGalleryField(typ, name string, field Field) {
	at := "/records/" + typ + "/fields/" + name
	entry, ok := c.kit.Registries.Fields[field.Type]
	if !ok {
		c.add(AppFile, at+"/type", CodeRegistryEntry, "Unknown field type %q.", field.Type)
		return
	}
	if contains(entry.Tags, "choices") && len(field.Values) == 0 {
		c.add(AppFile, at+"/values", CodeSchema, "List the choices for %s.%s.", typ, name)
	}
	if contains(entry.Tags, "relation") && !c.knownType(field.To) {
		c.add(AppFile, at+"/to", CodeTypeUnknown, "Choose an existing record type for the reference.")
	}
	if field.Default != nil {
		c.checkGalleryValue(AppFile, at+"/default", field, field.Default, nil)
	}
	if field.Sequence != nil && (!contains(entry.Tags, "sequence") || len(field.Sequence.Prefix) > 20 || field.Default != nil || field.Computed != "" || field.AI != "") {
		c.add(AppFile, at+"/sequence", CodePolicy, "A sequence needs a plain text field managed by the engine.")
	}
	if contains(entry.Tags, "currency") != (field.Currency != "") {
		c.add(AppFile, at+"/currency", CodePolicy, "Only a money field declares a currency, and it must have one.")
	}
	if field.AI != "" && !entry.AIFill {
		c.add(AppFile, at+"/ai", CodeNotAllowed, "This field type does not support AI fill.")
	}
	if field.AIOnChange && (field.AI == "" || AIFieldOutput(field) == nil || c.app.Budget == nil || c.app.Budget.PerRun <= 0 || !contains(c.app.Capabilities.AI, "sort")) {
		c.add(AppFile, at+"/ai_on_change", CodeNotAllowed, "Automatic AI fill needs an optional scalar AI field, the sort capability, and a positive per-run budget.")
	}
	c.checkRecordExpression(AppFile, at+"/computed", typ, field.Computed)
	if field.Blind != nil {
		c.checkTaggedField(AppFile, at+"/blind/per", typ, field.Blind.Per, "")
	}
	if field.Timer != nil {
		c.checkTaggedField(AppFile, at+"/timer/field", typ, field.Timer.Field, "clock")
		if !contains(entry.Tags, "clock") || field.Computed == "" {
			c.add(AppFile, at+"/timer", CodePolicy, "A timer rule needs a computed date or timestamp field.")
		}
	}
}

func (c *checker) checkGalleryValue(file, at string, field Field, value any, ids map[string]map[string]bool) {
	if schemas().entries["field/"+field.Type] == nil ||
		!c.validateEntry(file, at, CodeSampleBadValue, "field/"+field.Type, value) {
		return
	}
	for _, rule := range c.kit.Registries.Fields[field.Type].Rules {
		for _, selected := range ruleValues(value, rule.Path) {
			switch rule.Check {
			case "choices":
				for _, choice := range valueStrings(selected.value) {
					if !contains(field.Values, choice) {
						c.add(file, at+selected.path, CodeSampleBadValue, "%q is not one of the declared choices.", choice)
					}
				}
			case "sample-reference":
				for _, id := range valueStrings(selected.value) {
					if targets, known := ids[field.To]; known && !targets[id] {
						c.add(file, at+selected.path, CodeSampleRefMissing, "No sample %s record has id %q.", field.To, id)
					}
				}
			case "blob-regions":
				blob, _ := selected.value.(map[string]any)
				c.checkUniqueIDs(file, at+selected.path+"/pins", blob["pins"], CodeSampleBadValue)
				for _, region := range ruleValues(blob, "masks/*") {
					rect, _ := region.value.(map[string]any)
					if numeric(rect["x"])+numeric(rect["w"]) > 1 || numeric(rect["y"])+numeric(rect["h"]) > 1 {
						c.add(file, at+selected.path+region.path, CodeSampleBadValue, "A mask must fit inside the page.")
					}
				}
			}
		}
	}
}

func valueStrings(v any) []string {
	if s, ok := v.(string); ok {
		return []string{s}
	}
	return stringList(v)
}

func numeric(v any) float64 {
	n, _ := v.(float64)
	return n
}

func (c *checker) checkGalleryApp() {
	c.checkTypeIdentities()
	c.checkProcess()
	c.checkEnterprise()
	for _, typ := range sortedKeys(c.app.Records) {
		c.checkLedger(typ, c.app.Records[typ].Ledger)
	}
	for _, name := range sortedKeys(c.app.Relations) {
		rel := c.app.Relations[name]
		at := "/relations/" + name
		c.checkRecordAccess(AppFile, at+"/from", rel.From, "r")
		c.checkRecordAccess(AppFile, at+"/to", rel.To, "r")
	}
	ctx := actionCtx{file: AppFile}
	seen := map[string]bool{}
	for i, policy := range c.app.Policies {
		at := fmt.Sprintf("/policies/%d", i)
		if seen[policy.ID] {
			c.add(AppFile, at, CodeDuplicateID, "Two policies are named %q.", policy.ID)
		}
		seen[policy.ID] = true
		if c.validateEntry(AppFile, at+"/config", CodePolicy, "policy/"+policy.Kind, policy.Config) {
			c.checkRules(ctx, at+"/config", c.kit.Registries.Policies[policy.Kind].Rules, policy.Config, false)
		}
	}
	for _, item := range []struct {
		kind string
		has  func(string) bool
	}{
		{"types", func(name string) bool { r, ok := c.app.Records[name]; return ok && !r.Uses && c.canRead(name) }},
		{"commands", func(name string) bool { _, ok := c.commands[name]; return ok }},
		{"views", func(name string) bool { return c.screens[name] }},
	} {
		for _, name := range c.app.Exports[item.kind] {
			if !item.has(name) {
				c.add(AppFile, "/exports/"+item.kind, CodeRegistryEntry, "The app cannot export %q.", name)
			}
		}
	}
	if raw := c.def.Files[SampleFile]; len(raw) > 0 {
		if c.schemaCheck(SampleFile, raw, schemas().resources["sample-2.json"]) {
			for _, selected := range ruleValues(object(raw), "links/*") {
				row, _ := selected.value.(map[string]any)
				c.checkLink(SampleFile, selected.path, row, true)
			}
		}
	}
}

func (c *checker) policy(id string) (Policy, bool) {
	for _, p := range c.app.Policies {
		if p.ID == id {
			return p, true
		}
	}
	return Policy{}, false
}

func (c *checker) checkSignoff(file, at string, conf map[string]any) {
	typ := asString(conf["record"])
	c.checkRecordAccess(file, at+"/record", typ, "rw")
	c.checkUniqueIDs(file, at+"/roles", conf["roles"], CodePolicy)
	roles := ruleValues(conf, "roles/*")
	if len(roles) < 2 {
		return
	}
	maker := ""
	for i, role := range roles {
		values, _ := role.value.(map[string]any)
		person := asString(values["person"])
		if i == 0 {
			maker = person
		} else if conf["makerCannotSign"] == true && maker == person {
			c.add(file, at+role.path+"/person", CodePolicy, "The maker cannot be a later signer.")
		}
		if field, ok := strings.CutPrefix(person, "$record."); ok {
			c.checkTaggedField(file, at+role.path+"/person", typ, field, "person")
		}
	}
}

func (c *checker) checkChecklist(file, at string, conf map[string]any, signoffKind string) {
	typ := asString(conf["record"])
	c.checkRecordAccess(file, at+"/record", typ, "rw")
	c.checkUniqueIDs(file, at+"/steps", conf["steps"], CodePolicy)
	signoff := asString(conf["signoff"])
	p, found := c.policy(signoff)
	if signoff != "" && (!found || p.Kind != signoffKind || asString(p.Config["record"]) != typ) {
		c.add(file, at+"/signoff", CodePolicy, "Choose a sign-off chain for %s.", typ)
	}
	roles := map[string]bool{}
	for _, role := range ruleValues(p.Config, "roles/*/id") {
		roles[asString(role.value)] = true
	}
	for _, step := range ruleValues(conf, "steps/*/role") {
		if !roles[asString(step.value)] {
			c.add(file, at+step.path, CodePolicy, "This role is absent from the checklist's sign-off chain.")
		}
	}
}

func (c *checker) checkTrace(file, at string, trace TraceQuery) {
	c.checkRecordAccess(file, at+"/from/type", trace.From.Type, "r")
	for _, kind := range trace.Kinds {
		if _, ok := c.app.Relations[kind]; !ok {
			c.add(file, at+"/kinds", CodePolicy, "Unknown relation %q.", kind)
		}
	}
}

func (c *checker) checkLink(file, at string, link map[string]any, sample bool) {
	kind := asString(link["kind"])
	rel, ok := c.app.Relations[kind]
	if !ok {
		c.add(file, at+"/kind", CodePolicy, "Unknown relation %q.", kind)
		return
	}
	access := "rw"
	if sample {
		access = "r"
	}
	for _, end := range []struct{ key, want, needs string }{{"from", rel.From, access}, {"to", rel.To, "r"}} {
		value, _ := link[end.key].(map[string]any)
		typ, id := asString(value["type"]), asString(value["id"])
		if typ != end.want || id == "" {
			c.add(file, at+"/"+end.key, CodePolicy, "A %s endpoint must name a %s record ID.", end.key, end.want)
			continue
		}
		if c.checkRecordAccess(file, at+"/"+end.key, typ, end.needs) && sample && !c.sampleIDs[typ][id] {
			c.add(file, at+"/"+end.key, CodeSampleRefMissing, "No sample %s record has id %q.", typ, id)
		}
	}
}

func (c *checker) checkTypeIdentities() {
	seen := map[string]bool{}
	add := func(at, id string) {
		if id == "" {
			return
		}
		if seen[id] {
			c.add(AppFile, at, CodeDuplicateID, "Multiple types or fields use id %q.", id)
		}
		seen[id] = true
	}
	for _, name := range sortedKeys(c.app.Records) {
		t := c.app.Records[name]
		at := "/records/" + name
		add(at+"/id", t.ID)
		for _, field := range sortedKeys(t.Fields) {
			add(at+"/fields/"+field+"/id", t.Fields[field].ID)
		}
	}
}
