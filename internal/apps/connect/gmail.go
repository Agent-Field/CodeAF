package connect

import (
	"context"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
	native "github.com/Agent-Field/codeaf/internal/connect"
)

type gmailAdapter struct{ clients *nativeClients }

func (a gmailAdapter) Call(ctx context.Context, c svc.Credential, verb string, args map[string]any) (any, error) {
	if verb != "search" && verb != "read" && verb != "send" {
		return nil, noVerb(verb)
	}
	client, err := a.clients.client(ctx, c, "google")
	if err != nil {
		return nil, err
	}
	switch verb {
	case "search":
		return native.GmailSearch(ctx, client, text(args, "query"), number(args, "max"))
	case "read":
		return native.GmailRead(ctx, client, text(args, "id"))
	default:
		return native.GmailSend(ctx, client, text(args, "to"), text(args, "cc"), text(args, "subject"), text(args, "body"))
	}
}
