package appwire

import (
	"reflect"

	"github.com/Agent-Field/codeaf/internal/apps/builder"
)

const EventAppChangeProposed = "apps.change_proposed"

// AppChangeProposed carries the persisted card in the live event.
type AppChangeProposed = builder.Proposal

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
