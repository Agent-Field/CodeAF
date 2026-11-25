package schema

import (
	"encoding/json"
	"fmt"
)

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
	Kind            string            `json:"kind"`
	Record          string            `json:"record"`
	When            string            `json:"when,omitempty"`
	Transition      string            `json:"transition,omitempty"`
	Candidates      []string          `json:"candidates,omitempty"`
	Matches         map[string]string `json:"matches,omitempty"`
	OwnerField      string            `json:"ownerField,omitempty"`
	Priority        int               `json:"priority,omitempty"`
	Statement       string            `json:"statement,omitempty"`
	MakerCannotSign bool              `json:"makerCannotSign,omitempty"`
	WorkItem        string            `json:"workItem,omitempty"`
	Strategy        string            `json:"strategy,omitempty"`
	Field           string            `json:"field,omitempty"`
	Skill           string            `json:"skill,omitempty"`
	Order           string            `json:"order,omitempty"`
	Roles           []string          `json:"roles,omitempty"`
	Delegate        string            `json:"delegate,omitempty"`
}

type Resource struct {
	Record  string  `json:"record,omitempty"`
	Label   string  `json:"label"`
	Booking Booking `json:"booking"`
}

type Booking struct {
	Type      string `json:"type"`
	Resource  string `json:"resource"`
	Start     string `json:"start,omitempty"`
	End       string `json:"end,omitempty"`
	Overlap   string `json:"overlap,omitempty"`
	Day       string `json:"day,omitempty"`
	MaxPerDay int    `json:"maxPerDay,omitempty"`
}

type Sequence struct {
	Prefix  string `json:"prefix"`
	Pad     int    `json:"pad,omitempty"`
	Padding int    `json:"padding,omitempty"`
}

type Ledger struct {
	On       bool   `json:"-"`
	Reversal string `json:"reversal"`
}

func (l *Ledger) UnmarshalJSON(raw []byte) error {
	var on bool
	if err := json.Unmarshal(raw, &on); err == nil {
		*l = Ledger{On: on}
		return nil
	}
	type declaration Ledger
	var entry declaration
	if err := json.Unmarshal(raw, &entry); err != nil {
		return err
	}
	*l = Ledger(entry)
	l.On = true
	return nil
}

func (l Ledger) MarshalJSON() ([]byte, error) {
	if l.Reversal == "" {
		return json.Marshal(l.On)
	}
	return json.Marshal(map[string]string{"reversal": l.Reversal})
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
	if def, ok := c.app.Records[r.Record]; !ok || def.Uses {
		c.add(AppFile, at+"/record", CodePolicy, "The app must define the record type a rule governs.")
	}
	if r.Transition != "" {
		l, _ := c.lifecycle(r.Record)
		_, exists := l.Transitions[r.Transition]
		if def := c.app.Records[r.Record].Lifecycle; def != nil {
			for _, tr := range def.Transitions {
				exists = exists || tr.ID == r.Transition
			}
		}
		if !exists {
			c.add(AppFile, at+"/transition", CodePolicy, "There is no %s transition named %q.", r.Record, r.Transition)
		}
	}
	c.checkRecordExpression(AppFile, at+"/when", r.Record, r.When)
	c.checkProcessRef(AppFile, at+"/workItem", "work_items", r.WorkItem, r.Record)
	if r.Field != "" {
		fields, _ := c.typeFields(r.Record)
		field := fields[r.Field]
		switch {
		case r.Strategy == "linked-owner":
			if field.Type != "ref" || field.Masked {
				c.add(AppFile, at+"/field", CodePolicy, "Linked owner needs a visible single record reference.")
			} else {
				c.checkRecordAccess(AppFile, at+"/field", field.To, "r")
				c.checkRuleField(at+"/ownerField", field.To, r.OwnerField, "person")
			}
		case len(r.Matches) > 0:
			c.checkRuleField(at+"/field", r.Record, r.Field, "text", "enum", "person")
		default:
			c.checkRuleField(at+"/field", r.Record, r.Field, "person")
		}
	}
	for _, id := range append(append([]string(nil), r.Roles...), r.Delegate) {
		if id != "" {
			if _, ok := c.app.Roles[id]; !ok {
				c.add(AppFile, at+"/roles", CodePolicy, "There is no role named %q.", id)
			}
		}
	}
}

func (c *checker) checkRuleField(at, record, name string, types ...string) {
	fields, _ := c.typeFields(record)
	field := fields[name]
	if field.Masked || !contains(types, field.Type) {
		c.add(AppFile, at, CodePolicy, "%s.%s must be a visible %v field.", record, name, types)
	}
}

func (c *checker) checkResource(at string, r Resource) {
	b := r.Booking
	if !c.checkRecordAccess(AppFile, at+"/booking/type", b.Type, "r") {
		return
	}
	if def, ok := c.app.Records[b.Type]; !ok || def.Uses {
		c.add(AppFile, at+"/booking/type", CodePolicy, "The app must define the booking record type.")
	}
	fields, _ := c.typeFields(b.Type)
	if b.MaxPerDay > 0 {
		c.checkRuleField(at+"/booking/day", b.Type, b.Day, "date", "datetime")
	} else {
		c.checkRuleField(at+"/booking/start", b.Type, b.Start, "date", "datetime")
		c.checkRuleField(at+"/booking/end", b.Type, b.End, "date", "datetime")
		if b.Start == b.End || fields[b.Start].Type != fields[b.End].Type {
			c.add(AppFile, at+"/booking", CodePolicy, "Booking needs two distinct clock fields of the same type.")
		}
	}
	if r.Record == "" {
		c.checkRuleField(at+"/booking/resource", b.Type, b.Resource, "person")
		return
	}
	c.checkRecordAccess(AppFile, at+"/record", r.Record, "r")
	c.checkRuleField(at+"/booking/resource", b.Type, b.Resource, "ref")
	if fields[b.Resource].To != r.Record {
		c.add(AppFile, at+"/booking/resource", CodePolicy, "Booking must reference a %s resource record.", r.Record)
	}
}

func (c *checker) checkLedger(typ string, ledger *Ledger) {
	if ledger == nil {
		return
	}
	if _, ok := c.lifecycle(typ); ledger.On && ok {
		c.add(AppFile, "/records/"+typ+"/ledger", CodePolicy, "A ledger cannot also have a lifecycle.")
	}
	if ledger.Reversal == "" {
		return
	}
	field := c.app.Records[typ].Fields[ledger.Reversal]
	manifest := c.kit.Registries.Fields[field.Type]
	if !contains(manifest.Tags, "relation") || manifest.Storage != "text" || field.To != typ || field.Required {
		c.add(AppFile, "/records/"+typ+"/ledger/reversal", CodePolicy, "A ledger reversal must optionally reference one entry of the same type.")
	}
}
