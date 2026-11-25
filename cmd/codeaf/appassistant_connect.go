package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/schema"
	"github.com/Agent-Field/codeaf/internal/connect"
)

type appMail struct {
	To      string `json:"to"`
	CC      string `json:"cc,omitempty"`
	Subject string `json:"subject"`
	Body    string `json:"body"`
}

func (b *appAssistantBelt) connectors(app schema.App, manager *connect.Manager) {
	b.mailTools(app, manager)
	b.slackTools(app, manager)
	b.calendarTools(app, manager)
	b.mcpTools(app, manager)
}

func (b *appAssistantBelt) mailTools(app schema.App, manager *connect.Manager) {
	gmail := app.Capabilities.Connectors["gmail"]
	if manager != nil && manager.Connected("google") {
		b.connector(app, manager, "gmail", "search", "google", "gmail_search", "Search mail.", false,
			`{"query":{"type":"string"},"limit":{"type":"integer"}}`, []string{"query"},
			func(ctx context.Context, client *http.Client, raw json.RawMessage) (any, error) {
				var args struct {
					Query string `json:"query"`
					Limit int    `json:"limit,omitempty"`
				}
				if err := appAssistantDecode(raw, &args); err != nil {
					return nil, err
				}
				return connect.GmailSearch(ctx, client, args.Query, args.Limit)
			})
		b.connector(app, manager, "gmail", "read", "google", "gmail_read", "Read one message by ID.", false,
			`{"id":{"type":"string"}}`, []string{"id"},
			func(ctx context.Context, client *http.Client, raw json.RawMessage) (any, error) {
				var args struct {
					ID string `json:"id"`
				}
				if err := appAssistantDecode(raw, &args); err != nil {
					return nil, err
				}
				return connect.GmailRead(ctx, client, args.ID)
			})
	}
	const fields = `{"to":{"type":"string"},"cc":{"type":"string"},"subject":{"type":"string"},"body":{"type":"string"}}`
	required := []string{"to", "subject", "body"}
	send := func(ctx context.Context, client *http.Client, raw json.RawMessage) (any, error) {
		var email appMail
		if err := appAssistantDecode(raw, &email); err != nil {
			return nil, err
		}
		return connect.GmailSend(ctx, client, email.To, email.CC, email.Subject, email.Body)
	}
	if manager != nil && manager.Connected("google") {
		b.connector(app, manager, "gmail", "send", "google", "gmail_send", "Request approval to send this exact email.", true, fields, required, send)
	}
	if !slices.Contains(gmail.Can, "draft") {
		return
	}
	canSend := slices.Contains(gmail.Can, "send") && slices.Contains(gmail.Approve, "send")
	description := "Prepare an email draft in this conversation."
	if canSend {
		description = "Draft an email and put this exact message in the Inbox for approval before sending."
	}
	b.add("gmail_draft", description, canSend, fields, required, "", func(ctx context.Context, raw json.RawMessage) (any, error) {
		var email appMail
		if err := appAssistantDecode(raw, &email); err != nil {
			return nil, err
		}
		live, _, err := b.load()
		if err != nil {
			return nil, err
		}
		granted := live.Capabilities.Connectors["gmail"]
		if !slices.Contains(granted.Can, "draft") {
			return nil, errors.New("This app cannot draft email.")
		}
		if !canSend {
			return email, nil
		}
		// Both grants must still exist after the person answers. An app that
		// can only draft never gains send authority by calling the draft tool.
		return callAppConnector(ctx, manager, granted, "send", "google", "gmail_send", true, raw, send)
	})
}

func (b *appAssistantBelt) slackTools(app schema.App, manager *connect.Manager) {
	if manager == nil || !manager.Connected("slack") {
		return
	}
	b.slackReads(app, manager)
	request := func(ctx context.Context, client *http.Client, raw json.RawMessage) (any, error) {
		var args struct {
			Channel  string `json:"channel"`
			Text     string `json:"text"`
			ThreadTS string `json:"thread_ts,omitempty"`
		}
		if err := appAssistantDecode(raw, &args); err != nil {
			return nil, err
		}
		return connect.SlackSend(ctx, client, args.Channel, args.Text, args.ThreadTS)
	}
	const fields = `{"channel":{"type":"string"},"text":{"type":"string"},"thread_ts":{"type":"string"}}`
	for _, verb := range []string{"post", "send", "reply"} {
		b.connector(app, manager, "slack", verb, "slack", "slack_"+verb, "Request approval for a Slack message.", true, fields, []string{"channel", "text"}, request)
	}
}

func (b *appAssistantBelt) slackReads(app schema.App, manager *connect.Manager) {
	b.connector(app, manager, "slack", "read_thread", "slack", "slack_read_thread", "Read one Slack thread.", false,
		`{"channel":{"type":"string"},"ts":{"type":"string"}}`, []string{"channel", "ts"},
		func(ctx context.Context, client *http.Client, raw json.RawMessage) (any, error) {
			var args struct {
				Channel string `json:"channel"`
				TS      string `json:"ts"`
			}
			if err := appAssistantDecode(raw, &args); err != nil {
				return nil, err
			}
			return connect.SlackReadThread(ctx, client, args.Channel, args.TS)
		})
	if len(app.Capabilities.Connectors["slack"].Channels) != 0 {
		return
	}
	b.connector(app, manager, "slack", "search", "slack", "slack_search", "Search Slack messages.", false,
		`{"query":{"type":"string"},"limit":{"type":"integer"}}`, []string{"query"},
		func(ctx context.Context, client *http.Client, raw json.RawMessage) (any, error) {
			var args struct {
				Query string `json:"query"`
				Limit int    `json:"limit,omitempty"`
			}
			if err := appAssistantDecode(raw, &args); err != nil {
				return nil, err
			}
			return connect.SlackSearch(ctx, client, args.Query, args.Limit)
		})
	b.connector(app, manager, "slack", "list_channels", "slack", "slack_list_channels", "List Slack channels.", false,
		`{"filter":{"type":"string"},"limit":{"type":"integer"}}`, nil,
		func(ctx context.Context, client *http.Client, raw json.RawMessage) (any, error) {
			var args struct {
				Filter string `json:"filter,omitempty"`
				Limit  int    `json:"limit,omitempty"`
			}
			if err := appAssistantDecode(raw, &args); err != nil {
				return nil, err
			}
			return connect.SlackListChannels(ctx, client, args.Filter, args.Limit)
		})
}

func (b *appAssistantBelt) calendarTools(app schema.App, manager *connect.Manager) {
	if manager == nil || !manager.Connected("google") {
		return
	}
	b.connector(app, manager, "calendar", "read", "google", "calendar_list", "Read events between two dates.", false,
		`{"from":{"type":"string"},"to":{"type":"string"}}`, []string{"from", "to"},
		func(ctx context.Context, client *http.Client, raw json.RawMessage) (any, error) {
			var args struct {
				From string `json:"from"`
				To   string `json:"to"`
			}
			if err := appAssistantDecode(raw, &args); err != nil {
				return nil, err
			}
			return connect.CalendarList(ctx, client, args.From, args.To)
		})
	for _, verb := range []string{"create", "invite"} {
		b.connector(app, manager, "calendar", verb, "google", "calendar_"+verb, "Request approval to create a calendar event.", true,
			`{"title":{"type":"string"},"start":{"type":"string"},"end":{"type":"string"},"attendees":{"type":"string"},"location":{"type":"string"},"description":{"type":"string"}}`,
			[]string{"title", "start", "end"}, func(ctx context.Context, client *http.Client, raw json.RawMessage) (any, error) {
				var args struct {
					Title       string `json:"title"`
					Start       string `json:"start"`
					End         string `json:"end"`
					Attendees   string `json:"attendees,omitempty"`
					Location    string `json:"location,omitempty"`
					Description string `json:"description,omitempty"`
				}
				if err := appAssistantDecode(raw, &args); err != nil {
					return nil, err
				}
				return connect.CalendarCreate(ctx, client, args.Title, args.Start, args.End, args.Attendees, args.Location, args.Description)
			})
	}
}

type appConnectorCall func(context.Context, *http.Client, json.RawMessage) (any, error)

func (b *appAssistantBelt) connector(app schema.App, manager *connect.Manager, connector, verb, service, tool, description string, effect bool, fields string, required []string, call appConnectorCall) {
	grant := app.Capabilities.Connectors[connector]
	if !slices.Contains(grant.Can, verb) {
		return
	}
	capability := appConnectorCapability(manager, service, tool)
	state := manager.CapabilityState(service, capability)
	if state == connect.StateOff {
		return
	}
	ask := effect || state == connect.StateAsk || slices.Contains(grant.Approve, verb)
	b.add(tool, description, ask, fields, required, "", func(ctx context.Context, raw json.RawMessage) (any, error) {
		live, _, err := b.load()
		if err != nil {
			return nil, err
		}
		return callAppConnector(ctx, manager, live.Capabilities.Connectors[connector], verb, service, tool, ask, raw, call)
	})
}

func appConnectorCapability(manager *connect.Manager, service, tool string) string {
	switch tool {
	case "slack_post", "slack_reply":
		tool = "slack_send"
	case "calendar_invite":
		tool = "calendar_create"
	}
	return manager.ToolCapability(service, tool)
}

func callAppConnector(ctx context.Context, manager *connect.Manager, grant schema.Connector, verb, service, tool string, approved bool, raw json.RawMessage, call appConnectorCall) (any, error) {
	if !slices.Contains(grant.Can, verb) || (slices.Contains(grant.Approve, verb) && !approved) {
		return nil, errors.New("This app cannot use that connector action.")
	}
	if len(grant.Channels) != 0 {
		var destination struct {
			Channel string `json:"channel"`
		}
		if service != "slack" || tool == "slack_search" || tool == "slack_list_channels" ||
			json.Unmarshal(raw, &destination) != nil || !slices.Contains(grant.Channels, destination.Channel) {
			return nil, errors.New("This app cannot use that connector action in this channel.")
		}
	}
	if manager == nil || !appConnectorAccount(manager, service, grant.Account) {
		return nil, errors.New("The app's connector account is unavailable.")
	}
	state := manager.CapabilityState(service, appConnectorCapability(manager, service, tool))
	if state == connect.StateOff || state == connect.StateAsk && !approved {
		return nil, errors.New("The connector settings do not allow this call.")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	client, err := manager.Client(ctx, service)
	if err != nil {
		return nil, err
	}
	return call(ctx, client, raw)
}

func appConnectorAccount(manager *connect.Manager, service, account string) bool {
	if account == "" {
		return manager.Connected(service)
	}
	for _, status := range manager.Services() {
		if status.ID == service && status.Connected && status.Account == account {
			return true
		}
	}
	return false
}

func (b *appAssistantBelt) mcpTools(app schema.App, manager *connect.Manager) {
	if manager == nil {
		return
	}
	explicit := func(app schema.App, name string) bool {
		service, verb, found := strings.Cut(name, ".")
		if !found || verb == "" {
			return false
		}
		switch service {
		case "gmail", "slack", "calendar", "google":
			// The dedicated connector adapters enforce their account and channel restrictions.
			return false
		}
		grant, listed := app.Capabilities.Connectors[service]
		if listed {
			return len(grant.Channels) == 0 && slices.Contains(grant.Can, verb) && appConnectorAccount(manager, service, grant.Account)
		}
		return slices.Contains(app.Capabilities.Tools, name) && manager.Connected(service)
	}
	tools := append([]string(nil), app.Capabilities.Tools...)
	for service, grant := range app.Capabilities.Connectors {
		for _, verb := range grant.Can {
			tools = append(tools, service+"."+verb)
		}
	}
	tools = slices.DeleteFunc(tools, func(name string) bool { return !explicit(app, name) })
	slices.Sort(tools)
	tools = slices.Compact(tools)
	if len(tools) == 0 {
		return
	}
	// Unknown connector effects require approval even when the verb looks read-only.
	b.add("connector_call", "Request approval to call an app connector.", true, `{"tool":%s,"arguments":{"type":"object"}}`, []string{"tool", "arguments"}, appEnum(tools),
		func(ctx context.Context, raw json.RawMessage) (any, error) {
			var args struct {
				Tool      string          `json:"tool"`
				Arguments json.RawMessage `json:"arguments"`
			}
			if err := appAssistantDecode(raw, &args); err != nil {
				return nil, err
			}
			live, _, err := b.load()
			if err != nil {
				return nil, err
			}
			if !slices.Contains(tools, args.Tool) || !explicit(live, args.Tool) {
				return nil, errors.New("This app cannot use that connector action.")
			}
			service, verb, _ := strings.Cut(args.Tool, ".")
			ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			tools, err := manager.MCPTools(ctx, service)
			if err != nil {
				return nil, err
			}
			i := slices.IndexFunc(tools, func(tool connect.MCPTool) bool { return tool.Name == verb })
			if i < 0 || manager.CapabilityState(service, connect.MCPToolCapability(tools[i])) == connect.StateOff {
				return nil, errors.New("This connector action is unavailable.")
			}
			return manager.MCPCall(ctx, service, verb, args.Arguments)
		})
}
