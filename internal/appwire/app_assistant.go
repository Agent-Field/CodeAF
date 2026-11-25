package appwire

import (
	"encoding/json"
	"reflect"
)

const EventAppChangeProposed = "apps.change_proposed"

// AppChangeProposed identifies a durable edit that belongs to this app's
// assistant. The desk reopens the job by draft ID.
type AppChangeProposed struct {
	App          string   `json:"app"`
	Draft        string   `json:"draft"`
	SummaryLines []string `json:"summary_lines"`
	Screen       string   `json:"screen"`
	Kind         string   `json:"kind,omitempty"`
}

type appAssistantOpener interface {
	OpenAppAssistant(app string) (string, error)
}

type appAssistantOpened struct {
	Conversation string `json:"conversation"`
}

func init() {
	eventShapes[EventAppChangeProposed] = reflect.TypeFor[AppChangeProposed]()
	methods["apps.agent_open"] = typed(func(e Engine, p appParams) (*appAssistantOpened, error) {
		app, err := required("app", p.App)
		if err != nil {
			return nil, err
		}
		door, ok := e.(appAssistantOpener)
		if !ok {
			return nil, Errorf(CodeUnknownMethod, "this engine does not open app assistants")
		}
		id, err := door.OpenAppAssistant(app)
		return &appAssistantOpened{Conversation: id}, err
	})
}

// HasAppAssistantMethod lets an app omit tools whose engine lane is absent.
func HasAppAssistantMethod(name string) bool {
	_, present := methods[name]
	return present
}

// CallAppAssistantMethod reuses the engine's existing run and change doors.
// The scoped tool supplies the app identity before entering this method.
func CallAppAssistantMethod(e Engine, name string, params json.RawMessage) (any, error) {
	switch name {
	case "runs.start", "apps.change", "apps.build_state", "apps.preview", "apps.screen":
		if method, ok := methods[name]; ok {
			return method.call(e, params)
		}
	}
	return nil, Errorf(CodeUnknownMethod, "This app action is unavailable.")
}
