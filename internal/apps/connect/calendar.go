package connect

import (
	"context"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
	native "github.com/Agent-Field/codeaf/internal/connect"
)

type calendarAdapter struct{ clients *nativeClients }

func (a calendarAdapter) Call(ctx context.Context, c svc.Credential, verb string, args map[string]any) (any, error) {
	if verb != "list" && verb != "create" {
		return nil, noVerb(verb)
	}
	client, err := a.clients.client(ctx, c, "google")
	if err != nil {
		return nil, err
	}
	if verb == "list" {
		return native.CalendarList(ctx, client, text(args, "from"), text(args, "to"))
	}
	return native.CalendarCreate(ctx, client, text(args, "title"), text(args, "start"), text(args, "end"), text(args, "attendees"), text(args, "location"), text(args, "description"))
}
