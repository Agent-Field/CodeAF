package connect

import (
	"context"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
	native "github.com/Agent-Field/codeaf/internal/connect"
)

type slackAdapter struct{ clients *nativeClients }

func (a slackAdapter) Call(ctx context.Context, c svc.Credential, verb string, args map[string]any) (any, error) {
	handlers := map[string]func() (string, error){}
	client, err := a.clients.client(ctx, c, "slack")
	if err != nil {
		return nil, err
	}
	handlers["search"] = func() (string, error) {
		return native.SlackSearch(ctx, client, text(args, "query"), number(args, "max"))
	}
	handlers["read"] = func() (string, error) {
		return native.SlackReadThread(ctx, client, text(args, "channel"), text(args, "ts"))
	}
	handlers["list"] = func() (string, error) {
		return native.SlackListChannels(ctx, client, text(args, "filter"), number(args, "max"))
	}
	send := func() (string, error) {
		return native.SlackSend(ctx, client, text(args, "channel"), text(args, "text"), text(args, "thread_ts"))
	}
	handlers["send"] = send
	handlers["post"] = send
	handlers["reply"] = send
	if handler := handlers[verb]; handler != nil {
		return handler()
	}
	return nil, noVerb(verb)
}
