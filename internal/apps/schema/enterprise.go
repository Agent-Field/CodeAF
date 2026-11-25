package schema

import "fmt"

type Role struct {
	Kind        string          `json:"kind"`
	Label       string          `json:"label"`
	Screens     []string        `json:"screens,omitempty"`
	Transitions []TransitionRef `json:"transitions,omitempty"`
}

type Audience struct {
	Label       string          `json:"label"`
	Roles       []string        `json:"roles"`
	Screens     []string        `json:"screens,omitempty"`
	Transitions []TransitionRef `json:"transitions,omitempty"`
}

type TransitionRef struct {
	Type       string `json:"type"`
	Transition string `json:"transition"`
}

type Channel struct {
	Kind      string   `json:"kind"`
	Record    string   `json:"record"`
	Audience  string   `json:"audience,omitempty"`
	Run       string   `json:"run,omitempty"`
	Form      string   `json:"form,omitempty"`
	Connector string   `json:"connector,omitempty"`
	Event     string   `json:"event,omitempty"`
	Accept    []string `json:"accept,omitempty"`
}

type RoutingRule struct {
	Kind     string   `json:"kind"`
	Record   string   `json:"record"`
	When     string   `json:"when,omitempty"`
	WorkItem string   `json:"workItem,omitempty"`
	Strategy string   `json:"strategy,omitempty"`
	Field    string   `json:"field,omitempty"`
	Skill    string   `json:"skill,omitempty"`
	Order    string   `json:"order,omitempty"`
	Roles    []string `json:"roles,omitempty"`
	Delegate string   `json:"delegate,omitempty"`
}

type Resource struct {
	Record  string  `json:"record"`
	Label   string  `json:"label"`
	Booking Booking `json:"booking"`
}

type Booking struct {
	Type     string `json:"type"`
	Resource string `json:"resource"`
	Start    string `json:"start"`
	End      string `json:"end"`
	Overlap  string `json:"overlap"`
}

type Sequence struct {
	Prefix  string `json:"prefix"`
	Padding int    `json:"padding"`
}

type Ledger struct {
	Reversal string `json:"reversal"`
}

func (c *checker) checkEnterprise() {
	app := object(c.def.Files[AppFile])
	for _, group := range []struct{ name, entry string }{
		{"roles", "role"}, {"audiences", "audience"}, {"resources", "resource"},
		{"channels", ""}, {"rules", ""},
	} {
		entries, _ := app[group.name].(map[string]any)
		for _, id := range sortedKeys(entries) {
			value, _ := entries[id].(map[string]any)
			at := "/" + group.name + "/" + id
			key := "declaration/" + group.entry
			manifest := c.kit.Registries.AppDeclarations[group.entry]
			if group.name == "channels" {
				kind := asString(value["kind"])
				key = "channel/" + kind
				manifest = c.kit.Registries.ChannelKinds[kind]
			} else if group.name == "rules" {
				kind := asString(value["kind"])
				key = "rule/" + kind
				manifest = c.kit.Registries.RuleKinds[kind]
			}
			if !c.validateEntry(AppFile, at, CodePolicy, key, value) {
				continue
			}
			for _, rule := range manifest.Rules {
				switch rule.Check {
				case "role":
					role := c.app.Roles[id]
					c.checkAudienceGrants(at, role.Screens, role.Transitions)
				case "audience":
					c.checkAudience(at, c.app.Audiences[id])
				case "channel":
					c.checkChannel(at, c.app.Channels[id])
				case "assignment", "approval-routing":
					c.checkRoutingRule(at, c.app.Rules[id])
				case "resource":
					c.checkResource(at, c.app.Resources[id])
				}
			}
		}
	}
}

func (c *checker) checkAudienceGrants(at string, screens []string, transitions []TransitionRef) {
	for _, id := range screens {
		if !c.screens[id] {
			c.add(AppFile, at+"/screens", CodeOpenUnknown, "There is no screen named %q.", id)
		}
	}
	for i, ref := range transitions {
		l, _ := c.lifecycle(ref.Type)
		if _, ok := l.Transitions[ref.Transition]; !ok {
			c.add(AppFile, fmt.Sprintf("%s/transitions/%d", at, i), CodePolicy, "There is no %s transition named %q.", ref.Type, ref.Transition)
		}
	}
}

func (c *checker) checkAudience(at string, a Audience) {
	c.checkAudienceGrants(at, a.Screens, a.Transitions)
	screens, transitions := map[string]bool{}, map[TransitionRef]bool{}
	for _, id := range a.Roles {
		role, ok := c.app.Roles[id]
		if !ok {
			c.add(AppFile, at+"/roles", CodePolicy, "There is no role named %q.", id)
		}
		for _, screen := range role.Screens {
			screens[screen] = true
		}
		for _, transition := range role.Transitions {
			transitions[transition] = true
		}
	}
	for _, screen := range a.Screens {
		if !screens[screen] {
			c.add(AppFile, at+"/screens", CodePolicy, "No audience role has access to screen %q.", screen)
		}
	}
	for _, ref := range a.Transitions {
		if !transitions[ref] {
			c.add(AppFile, at+"/transitions", CodePolicy, "No audience role may use transition %s.%s.", ref.Type, ref.Transition)
		}
	}
}

func (c *checker) checkChannel(at string, channel Channel) {
	c.checkRecordAccess(AppFile, at+"/record", channel.Record, "rw")
	if channel.Audience != "" {
		if _, ok := c.app.Audiences[channel.Audience]; !ok {
			c.add(AppFile, at+"/audience", CodePolicy, "There is no audience named %q.", channel.Audience)
		}
	}
	c.checkProcessRef(AppFile, at+"/form", "forms", channel.Form, channel.Record)
	c.checkReference(actionCtx{file: AppFile}, at+"/run", "run", channel.Run)
	if channel.Connector != "" {
		c.checkEvent(AppFile, at+"/event", channel.Connector, channel.Event)
	}
}

func (c *checker) checkRoutingRule(at string, r RoutingRule) {
	c.checkRecordAccess(AppFile, at+"/record", r.Record, "r")
	c.checkRecordExpression(AppFile, at+"/when", r.Record, r.When)
	c.checkProcessRef(AppFile, at+"/workItem", "work_items", r.WorkItem, r.Record)
	if r.Field != "" {
		c.checkTaggedField(AppFile, at+"/field", r.Record, r.Field, "person")
	}
	for _, id := range append(append([]string(nil), r.Roles...), r.Delegate) {
		if id != "" {
			if _, ok := c.app.Roles[id]; !ok {
				c.add(AppFile, at+"/roles", CodePolicy, "There is no role named %q.", id)
			}
		}
	}
}

func (c *checker) checkResource(at string, r Resource) {
	c.checkRecordAccess(AppFile, at+"/record", r.Record, "r")
	b := r.Booking
	if !c.checkRecordAccess(AppFile, at+"/booking/type", b.Type, "r") {
		return
	}
	c.checkTaggedField(AppFile, at+"/booking/start", b.Type, b.Start, "clock")
	c.checkTaggedField(AppFile, at+"/booking/end", b.Type, b.End, "clock")
	fields, _ := c.typeFields(b.Type)
	if b.Start == b.End || fields[b.Start].Type != fields[b.End].Type {
		c.add(AppFile, at+"/booking", CodePolicy, "Booking needs two distinct clock fields of the same type.")
	}
	if !contains(c.kit.Registries.Fields[fields[b.Resource].Type].Tags, "relation") || fields[b.Resource].To != r.Record {
		c.add(AppFile, at+"/booking/resource", CodePolicy, "Booking must reference a %s resource record.", r.Record)
	}
}

func (c *checker) checkLedger(typ string, ledger *Ledger) {
	if ledger == nil {
		return
	}
	fields, _ := c.typeFields(typ)
	field := fields[ledger.Reversal]
	manifest := c.kit.Registries.Fields[field.Type]
	if !contains(manifest.Tags, "relation") || manifest.Storage != "text" || field.To != typ || field.Required {
		c.add(AppFile, "/records/"+typ+"/ledger/reversal", CodePolicy, "A ledger reversal must optionally reference one entry of the same type.")
	}
}
