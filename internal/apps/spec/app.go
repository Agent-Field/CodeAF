package spec

import "encoding/json"

// App is app.json: record types, capabilities, triggers, commands, budget.
type App struct {
	ID           string                 `json:"id"`
	Name         string                 `json:"name"`
	Description  string                 `json:"description,omitempty"`
	Space        string                 `json:"space,omitempty"`
	Version      int                    `json:"version,omitempty"`
	Kit          int                    `json:"kit,omitempty"`
	Agent        AgentRef               `json:"agent"`
	Records      map[string]*RecordType `json:"records"`
	Capabilities Capabilities           `json:"capabilities"`
	Triggers     []Trigger              `json:"triggers,omitempty"`
	Commands     []Command              `json:"commands,omitempty"`
	Budget       Budget                 `json:"budget"`
}

// AgentRef names the app's agent and the file that defines it.
type AgentRef struct {
	Name  string `json:"name"`
	File  string `json:"file"`
	Model string `json:"model,omitempty"`
}

// RecordType is one typed record collection of the app.
type RecordType struct {
	Label     string            `json:"label,omitempty"`
	Plural    string            `json:"plural,omitempty"`
	Title     string            `json:"title"`
	Fields    map[string]*Field `json:"fields"`
	Constants map[string]any    `json:"constants,omitempty"`
	Freeze    []Freeze          `json:"freeze,omitempty"`
}

// FieldType is the stored type of a field (S-A2 field types).
type FieldType string

const (
	FieldText     FieldType = "text"
	FieldLongtext FieldType = "longtext"
	FieldNumber   FieldType = "number"
	FieldMoney    FieldType = "money"
	FieldDate     FieldType = "date"
	FieldDatetime FieldType = "datetime"
	FieldDuration FieldType = "duration"
	FieldEnum     FieldType = "enum"
	FieldEnums    FieldType = "enums"
	FieldBool     FieldType = "bool"
	FieldPerson   FieldType = "person"
	FieldPeople   FieldType = "people"
	FieldRef      FieldType = "ref"
	FieldRefs     FieldType = "refs"
	FieldEmail    FieldType = "email"
	FieldPhone    FieldType = "phone"
	FieldURL      FieldType = "url"
	FieldFile     FieldType = "file"
	FieldFiles    FieldType = "files"
	FieldPlace    FieldType = "place"
)

// Field is one typed field. Ref and Refs name the target type in To; that is
// how relations are declared.
type Field struct {
	Type     FieldType       `json:"type"`
	Label    string          `json:"label,omitempty"`
	Required bool            `json:"required,omitempty"`
	Unique   bool            `json:"unique,omitempty"`
	Index    bool            `json:"index,omitempty"`
	Values   []string        `json:"values,omitempty"`
	Default  json.RawMessage `json:"default,omitempty"`
	To       string          `json:"to,omitempty"`
	Currency string          `json:"currency,omitempty"`
	Masked   bool            `json:"masked,omitempty"`
	Computed string          `json:"computed,omitempty"`
	AI       string          `json:"ai,omitempty"`
	Blind    *Blind          `json:"blind,omitempty"`
}

// Blind hides each person's value until they submit theirs.
type Blind struct {
	Until string `json:"until"`
	Per   string `json:"per,omitempty"`
}

// Freeze makes matching records read-only. Not enforced in wave 1.
type Freeze struct {
	When     string   `json:"when,omitempty"`
	Except   []string `json:"except,omitempty"`
	Unfreeze string   `json:"unfreeze,omitempty"`
	Period   string   `json:"period,omitempty"`
	Field    string   `json:"field,omitempty"`
	After    string   `json:"after,omitempty"`
}

// Access is a record capability: read or read-write.
type Access string

const (
	AccessRead      Access = "r"
	AccessReadWrite Access = "rw"
)

// Capabilities is everything the app may do. A capability not listed here
// does not exist for the app (rule 05).
type Capabilities struct {
	Records    map[string]Access    `json:"records"`
	Connectors map[string]Connector `json:"connectors,omitempty"`
	AI         []string             `json:"ai,omitempty"`
	Tools      []string             `json:"tools,omitempty"`
}

// Connector is one declared connector use; Approve lists outbound verbs.
type Connector struct {
	As       string   `json:"as"`
	Account  string   `json:"account,omitempty"`
	Can      []string `json:"can"`
	Approve  []string `json:"approve,omitempty"`
	Channels []string `json:"channels,omitempty"`
}

// TriggerKind is what starts a run automatically. Manual starts need no
// trigger: any run file may be started with runs.start.
type TriggerKind string

const (
	TriggerSchedule      TriggerKind = "schedule"
	TriggerRecordCreated TriggerKind = "record.created"
	TriggerRecordChanged TriggerKind = "record.changed"
	TriggerRecordDeleted TriggerKind = "record.deleted"
	TriggerConnector     TriggerKind = "connector"
)

// Trigger starts Run when On fires and When (CEL) holds.
type Trigger struct {
	ID        string         `json:"id"`
	On        TriggerKind    `json:"on"`
	Run       string         `json:"run"`
	Type      string         `json:"type,omitempty"`
	When      string         `json:"when,omitempty"`
	Cron      string         `json:"cron,omitempty"`
	Connector string         `json:"connector,omitempty"`
	Event     string         `json:"event,omitempty"`
	Params    map[string]any `json:"params,omitempty"`
}

// Command is a ⌘K / RecordPage action: it starts Run or runs Do.
type Command struct {
	ID      string         `json:"id"`
	Label   string         `json:"label"`
	For     string         `json:"for,omitempty"`
	Key     string         `json:"key,omitempty"`
	Confirm string         `json:"confirm,omitempty"`
	Run     string         `json:"run,omitempty"`
	Params  map[string]any `json:"params,omitempty"`
	Do      []Action       `json:"do,omitempty"`
}

// Budget is the app's monthly spend cap in USD.
type Budget struct {
	Month    float64  `json:"month"`
	Currency string   `json:"currency,omitempty"`
	PerRun   float64  `json:"per_run,omitempty"`
	AtCap    string   `json:"at_cap,omitempty"`
	Models   []string `json:"models,omitempty"`
}
