package schema

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"regexp"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v5"
)

const (
	CodeManifest       = "manifest"
	CodeRegistryEntry  = "registry_entry"
	CodeContent        = "content"
	CodeContentBinding = "content_binding"
	CodePolicy         = "policy"
	CodeExpression     = "expression"
	CodeRunBinding     = "run_binding"
	CodeWorkspaceShape = "workspace_shape"
)

func kitVersion(raw []byte) int {
	var head struct{ Kit int }
	_ = json.Unmarshal(raw, &head)
	return head.Kit
}

// DefinitionGroups describes the files the app store and Check read. Keep both
// on this door so a file previewed in a draft survives publication and Revert.
func DefinitionGroups(kit int) map[string][]string {
	groups := map[string][]string{ScreensDir: {".json"}, RunsDir: {".yaml", ".yml"}}
	if kit == 2 {
		for _, entry := range schemas().kits[2].Registries.Content {
			groups[entry.Directory] = append(groups[entry.Directory], entry.Extension)
		}
		groups["custom"] = []string{".json", ".tsx", ".step"}
	}
	return groups
}

var contentFileID = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,47}$`)

func fileInGroup(file string) (string, string, bool) {
	dir, base, ok := strings.Cut(file, "/")
	if !ok || strings.Contains(base, "/") {
		return "", "", false
	}
	ext := path.Ext(base)
	return dir, ext, contentFileID.MatchString(strings.TrimSuffix(base, ext))
}

func (c *checker) validGalleryPath(file string) bool {
	key, _ := c.contentEntry(file)
	dir, ext, ok := fileInGroup(file)
	return key != "" || ok && dir == "custom" && (ext == ".json" || ext == ".tsx" || ext == ".step")
}

func bindingKind(b *Bind) string {
	if b == nil {
		return ""
	}
	switch {
	case b.Records != "":
		return "records"
	case b.Record != "":
		return "record"
	case b.Selection != "":
		return "selection"
	case len(b.Params) != 0:
		return "params"
	case b.Run != "":
		return "run"
	case b.Runs != "":
		return "runs"
	case b.File != "":
		return "file"
	case b.ContentPart != "":
		return "content-part"
	}
	return ""
}

type customPart struct {
	part    Part
	props   *jsonschema.Schema
	program PartProgram
}

func (c *checker) findPart(n *Node) (Part, *jsonschema.Schema) {
	if custom, ok := c.custom[n.Part]; ok {
		return custom.part, custom.props
	}
	var fallback Part
	for _, p := range c.kit.Parts {
		if p.Name != n.Part {
			continue
		}
		if p.VariantBinding == "" {
			fallback = p
		} else if bindingKind(n.Bind) == p.VariantBinding {
			return p, schemas().props[c.kit.Kit][p.key()]
		}
	}
	return fallback, schemas().props[c.kit.Kit][fallback.key()]
}

func offlineSchema(_ string) (io.ReadCloser, error) {
	return nil, fmt.Errorf("app schemas cannot load resources outside the registry")
}

func (c *checker) loadCustomParts() {
	c.custom = map[string]customPart{}
	for _, file := range c.def.under("custom/", ".json") {
		raw := c.def.Files[file]
		if !c.schemaCheck(file, raw, schemas().resources["part-2.json"]) {
			continue
		}
		var p Part
		if err := json.Unmarshal(raw, &p); err != nil {
			c.add(file, "", CodeManifest, "%v", err)
			continue
		}
		base := strings.TrimSuffix(file, ".json")
		if p.Name != base || p.Source != base+".tsx" || len(bytes.TrimSpace(c.def.Files[p.Source])) == 0 {
			c.add(file, "/source", CodeManifest, "The custom manifest and its source must share their filename with the part: %s.", base)
			continue
		}
		compiler := jsonschema.NewCompiler()
		compiler.Draft = jsonschema.Draft2020
		compiler.LoadURL = offlineSchema
		url := "https://codeaf.dev/schema/" + file
		err := compiler.AddResource(url, bytes.NewReader(p.Props))
		var props *jsonschema.Schema
		if err == nil {
			props, err = compiler.Compile(url)
		}
		if err == nil {
			err = checkManifest(p.Name, Manifest{ID: p.Name, Version: p.Version, Doc: p.Summary, Example: p.Example}, props)
		}
		if err != nil {
			c.add(file, "/props", CodeManifest, "%s props: %v", p.Name, err)
			continue
		}
		p.Tier = "custom"
		p.Renderers = map[string]string{"desktop": "sandbox.iframe"}
		if p.Origin != nil {
			digest, err := PartDigest(raw, c.def.Files[p.Source])
			if err != nil || digest != p.Origin.Digest {
				c.add(file, "/origin", CodeManifest, "This part differs from the version imported from your Kit.")
				continue
			}
		}
		program, err := compilePart(p, c.def.Files[p.Source])
		if err != nil {
			c.add(file, "/source", CodeManifest, "%v", err)
			continue
		}
		c.custom[p.Name] = customPart{p, props, program}
	}
	for _, file := range c.def.under("custom/", ".tsx") {
		if _, ok := c.def.Files[strings.TrimSuffix(file, ".tsx")+".json"]; !ok {
			c.add(file, "", CodeManifest, "A custom part needs its own manifest beside its source.")
		}
	}
}

func (c *checker) validateEntry(file, at, code, key string, value any) bool {
	s := schemas().entries[key]
	if s == nil {
		c.add(file, at, CodeRegistryEntry, "%q is missing from the app registry.", key)
		return false
	}
	if err := s.Validate(jsonValue(value)); err != nil {
		for _, p := range schemaProblems(file, err) {
			p.Code = code
			p.Path = at + p.Path
			c.report.Problems = append(c.report.Problems, p)
		}
		return false
	}
	return true
}

// checkGalleryPart checks the record and content references the manifest opts
// into. Its props, events and children use the same path for first-party and
// local manifests.
func (c *checker) checkGalleryPart(ctx actionCtx, n *Node, p Part, at, typ string) string {
	b := n.Bind
	var projection QueryIR
	if p.BindingRequired && b == nil {
		c.add(ctx.file, at+"/bind", CodeBindNotAccepted, "%s needs a binding.", p.Name)
	}
	for i, child := range n.Children {
		if p.Children == "slots" && !contains(p.Slots, child.Slot) || p.Children != "slots" && child.Slot != "" {
			c.add(ctx.file, fmt.Sprintf("%s/children/%d/slot", at, i), CodePartChildren, "%q is not a slot on %s.", child.Slot, p.Name)
		}
	}
	if b != nil {
		switch bindingKind(b) {
		case "file":
			typ = ctx.resolveType(b.Type, b.File)
			if typ == "" {
				c.add(ctx.file, at+"/bind", CodeBindTypeUnknown, "Name the record type that owns the file field.")
			} else {
				c.checkRecordAccess(ctx.file, at+"/bind", typ, "r")
				c.checkTaggedField(ctx.file, at+"/bind/field", typ, b.Field, "blob")
			}
		case "content-part":
			c.checkContentBinding(ctx.file, at+"/bind/content-part", b.ContentPart, p.Content, b.PartID)
		case "run", "runs":
			ref := b.Run
			if b.Runs != "" {
				ref = b.Runs
			}
			if !strings.HasPrefix(ref, "$") && !c.runs[ref] {
				c.add(ctx.file, at+"/bind", CodeRunBinding, "There is no run named %q.", ref)
			}
		case "selection":
			if _, ok := ctx.lists[b.Selection]; !ok {
				c.add(ctx.file, at+"/bind/selection", CodeSelectionUnknown, "The screen has no selection named %q.", b.Selection)
			}
		case "records":
			projection = c.checkViewBinding(ctx, at+"/bind", b)
			if n.Part == "Chart" && n.Props["kind"] == "metric" &&
				(len(projection.Aggregate) != 1 || len(projection.Group) != 0 || projection.Window != nil || projection.Relate != nil) {
				c.add(ctx.file, at+"/bind/view", "metric_view", "Bind this metric to a view with one aggregate, no grouping and no window.")
			}
		}
	}
	if typ == "" && p.Capabilities != "" {
		typ, _ = n.Props["type"].(string)
		if typ == "" {
			c.add(ctx.file, at, CodeBindTypeUnknown, "%s needs a record binding or a record type.", p.Name)
		}
	}
	if typ != "" && p.Capabilities != "" {
		c.checkRecordAccess(ctx.file, at, typ, p.Capabilities)
	}
	if custom, ok := c.custom[n.Part]; ok {
		c.checkCustomPart(ctx, n, custom.program, at, typ, projection)
	}
	props := n.Props
	for _, rule := range p.Rules {
		for _, value := range ruleValues(props, rule.Path) {
			switch rule.Check {
			case "field":
				output := false
				for _, aggregate := range projection.Aggregate {
					output = output || aggregate.As == asString(value.value)
				}
				if output {
					continue
				}
				c.checkTaggedField(ctx.file, at+"/props"+value.path, typ, asString(value.value), rule.Options)
			case "writable-field":
				name := asString(value.value)
				c.checkTaggedField(ctx.file, at+"/props"+value.path, typ, name, "")
				fields, _ := c.typeFields(typ)
				life, lifecycle := c.lifecycle(typ)
				if systemFields[name] || fields[name].Computed != "" || lifecycle && life.Field == name {
					c.add(ctx.file, at+"/props"+value.path, CodeNotAllowed, "This form cannot edit %q.", name)
				}
			case "field-value":
				fieldName := value.path[strings.LastIndex(value.path, "/")+1:]
				fields, _ := c.typeFields(typ)
				if field, ok := fields[fieldName]; ok {
					c.checkGalleryValue(ctx.file, at+"/props"+value.path, field, value.value, nil)
				} else {
					c.checkTaggedField(ctx.file, at+"/props"+value.path, typ, fieldName, "")
				}
			case "form":
				if v := asString(value.value); v != "" && v != typ {
					c.add(ctx.file, at+"/props/type", CodeBindTypeUnknown, "The form type %q differs from the record binding type %q.", v, typ)
				}
			case "policy":
				policy, found := c.policy(asString(value.value))
				if !found || policy.Kind != rule.Options || asString(policy.Config["record"]) != typ {
					c.add(ctx.file, at+"/props"+value.path, CodePolicy, "Choose a %s policy for %s.", rule.Options, typ)
				}
			case "view":
				name := asString(value.value)
				entry, ok := c.kit.Registries.Views[name]
				if !ok {
					c.add(ctx.file, at+"/props"+value.path, CodeRegistryEntry, "%q is not a registered view.", name)
					continue
				}
				if confs, ok := props["viewOptions"].(map[string]any); ok {
					if conf, ok := confs[name].(map[string]any); ok &&
						c.validateEntry(ctx.file, at+"/props/viewOptions/"+name, CodePartProps, "view/"+name, conf) {
						for _, viewRule := range entry.Rules {
							for _, v := range ruleValues(conf, viewRule.Path) {
								c.checkTaggedField(ctx.file, at+"/props/viewOptions/"+name+v.path, typ, asString(v.value), viewRule.Options)
							}
						}
					}
				}
			case "lifecycle":
				if _, ok := c.lifecycle(typ); !ok {
					c.add(ctx.file, at, CodePolicy, "%s needs a record with a lifecycle.", p.Name)
				}
			case "lifecycle-group":
				c.checkLifecycleGroup(ctx, at, typ, asString(value.value), rule.Options, n)
			case "process-ref":
				c.checkProcessRef(ctx.file, at+"/props"+value.path, rule.Options, asString(value.value), typ)
			case "content-file":
				c.checkContentBinding(ctx.file, at+"/props"+value.path, asString(value.value), rule.Options, "")
			default:
				c.checkReference(ctx, at+"/props"+value.path, rule.Check, asString(value.value))
			}
		}
	}
	c.checkExpression(ctx.file, at+"/when", n.When)
	return typ
}

type selectedValue struct {
	path  string
	value any
}

func ruleValues(value any, rulePath string) []selectedValue {
	out := []selectedValue{{value: value}}
	if rulePath == "" {
		return out
	}
	for _, segment := range strings.Split(rulePath, "/") {
		next := []selectedValue{}
		for _, current := range out {
			switch v := current.value.(type) {
			case map[string]any:
				if item, ok := v[segment]; ok {
					next = append(next, selectedValue{current.path + "/" + segment, item})
				} else if segment == "*" {
					for _, key := range sortedKeys(v) {
						item := v[key]
						next = append(next, selectedValue{current.path + "/" + key, item})
					}
				}
			case []any:
				if segment == "*" {
					for i, item := range v {
						next = append(next, selectedValue{fmt.Sprintf("%s/%d", current.path, i), item})
					}
				}
			}
		}
		out = next
	}
	return out
}

func asString(v any) string { s, _ := v.(string); return s }

func object(raw []byte) map[string]any {
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return out
}

func (c *checker) checkReference(ctx actionCtx, at, check, value string) {
	if value == "" {
		return
	}
	switch check {
	case "run":
		if !c.runs[value] {
			c.add(ctx.file, at, CodeRunUnknown, "There is no run named %q.", value)
		}
	case "screen", "open":
		if check == "open" && value == "desk:canvases" {
			return
		}
		if !c.screens[value] && !(check == "open" && (ctx.sheets[value] || c.knownType(value) && c.canRead(value))) {
			c.add(ctx.file, at, CodeOpenUnknown, "There is no accessible screen, sheet or record type named %q.", value)
		}
	case "command":
		if _, ok := c.commands[value]; !ok {
			c.add(ctx.file, at, CodeCommandUnknown, "There is no command named %q.", value)
		}
	case "list":
		if _, ok := ctx.lists[value]; !ok {
			c.add(ctx.file, at, CodeListUnknown, "There is no collection with selection named %q.", value)
		}
	case "agent":
		if c.app.Agent == nil || c.app.Agent.Name != value {
			c.add(ctx.file, at, CodeAgentUnknown, "The app has no agent named %q.", value)
		}
	case "record-read":
		c.checkRecordAccess(ctx.file, at, value, "r")
	}
}
