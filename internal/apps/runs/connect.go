package runs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/Agent-Field/codeaf/internal/connect"
)

// ConnectedTools runs app-declared operations through the profile's existing
// connector manager. It keeps policy with dispatch so adapters cannot acquire
// an operation through a different service name.
type ConnectedTools struct {
	Apps    func(string) (App, error)
	Manager func() (*connect.Manager, error)
}

type connectedOperation struct {
	service  string
	verb     string
	native   string
	alias    string
	outbound bool
}

func operation(name string) (connectedOperation, error) {
	service, verb, ok := strings.Cut(name, ".")
	op := connectedOperation{service: service, verb: verb, outbound: true}
	if !ok || !validID(service) || verb == "" {
		return op, errors.New("a tool needs a connected service and operation")
	}
	switch name {
	case "gmail.search", "gmail.read", "calendar.list":
		op.native, op.alias, op.outbound = "google", "read", false
	case "gmail.send":
		op.native = "google"
	case "calendar.create":
		op.native, op.alias = "google", "write"
	case "slack.search", "slack.read_thread", "slack.list_channels":
		op.native, op.alias, op.outbound = "slack", "read", false
	case "slack.send", "slack.post":
		op.native, op.verb, op.alias = "slack", "send", "post"
	}
	return op, nil
}

func (c ConnectedTools) check(call Call) (connectedOperation, *connect.Manager, bool, error) {
	op, err := operation(call.Name)
	if err != nil {
		return op, nil, false, err
	}
	app, err := c.Apps(call.App)
	if err != nil {
		return op, nil, false, err
	}
	allowed, ask := app.allowsTool(op.service, op.verb, op.alias)
	allowed = allowed || slices.Contains(app.Capabilities.Tools, call.Name)
	if !allowed || call.As != "me" {
		return op, nil, false, fmt.Errorf("app %s cannot use %s", call.App, call.Name)
	}
	scope := app.Capabilities.Connectors[op.service]
	if len(scope.Channels) != 0 {
		// Searches can span an entire account. Require the exact
		// channel value the app declared for a scoped operation.
		id, _ := call.Args["channel"].(string)
		if op.native != "slack" || (op.verb != "read_thread" && op.verb != "send") || !slices.Contains(scope.Channels, id) {
			return op, nil, false, fmt.Errorf("app %s cannot use %s in this channel", call.App, call.Name)
		}
	}
	manager, err := c.Manager()
	if err != nil {
		return op, nil, false, err
	}
	service, capability := op.native, ""
	if service != "" {
		capability = manager.ToolCapability(service, op.service+"_"+op.verb)
	} else {
		service = op.service
		// Unknown MCP side effects require approval for every call.
		capability = connect.MCPToolCapability(connect.MCPTool{Name: op.verb})
	}
	state := manager.CapabilityState(service, capability)
	if state == connect.StateOff {
		return op, nil, false, fmt.Errorf("%s is off in Connections", call.Name)
	}
	return op, manager, ask || op.outbound || state == connect.StateAsk, nil
}

func (c ConnectedTools) Check(call Call) (bool, error) {
	_, _, ask, err := c.check(call)
	return ask, err
}

func (c ConnectedTools) Call(ctx context.Context, call Call) (Reply, error) {
	op, manager, ask, err := c.check(call)
	if err != nil {
		return Reply{}, err
	}
	if (ask || Outbound(call.Name)) && !call.approved {
		return Reply{}, errors.New("approval is required before calling " + call.Name)
	}
	if op.native == "" {
		args, err := json.Marshal(call.Args)
		if err != nil {
			return Reply{}, err
		}
		output, err := manager.MCPCall(ctx, op.service, op.verb, args)
		return connectorResult(output, err, true)
	}
	client, err := manager.Client(ctx, op.native)
	if err != nil {
		return Reply{}, err
	}
	arg := func(name string) string {
		value, _ := call.Args[name].(string)
		return value
	}
	count := func() int {
		switch n := call.Args["max"].(type) {
		case json.Number:
			value, _ := n.Int64()
			return int(value)
		case int:
			return n
		case int64:
			return int(n)
		case float64:
			return int(n)
		default:
			return 0
		}
	}()
	var output string
	switch op.service + "." + op.verb {
	case "gmail.search":
		output, err = connect.GmailSearch(ctx, client, arg("q"), count)
	case "gmail.read":
		output, err = connect.GmailRead(ctx, client, arg("id"))
	case "gmail.send":
		draft, err := mailDraft(call.Args)
		if err != nil {
			return Reply{}, err
		}
		output, err = connect.GmailSend(ctx, client, draft.To, draft.CC, draft.Subject, draft.Body)
		return connectorResult(output, err, true)
	case "calendar.list":
		output, err = connect.CalendarList(ctx, client, arg("from"), arg("to"))
	case "calendar.create":
		output, err = connect.CalendarCreate(ctx, client, arg("title"), arg("start"), arg("end"), arg("attendees"), arg("location"), arg("description"))
	case "slack.search":
		output, err = connect.SlackSearch(ctx, client, arg("q"), count)
	case "slack.read_thread":
		output, err = connect.SlackReadThread(ctx, client, arg("channel"), arg("ts"))
	case "slack.list_channels":
		output, err = connect.SlackListChannels(ctx, client, arg("filter"), count)
	case "slack.send":
		output, err = connect.SlackSend(ctx, client, arg("channel"), arg("text"), arg("thread_ts"))
	}
	return connectorResult(output, err, op.outbound)
}

func connectorResult(output string, err error, mutates bool) (Reply, error) {
	if err != nil && mutates {
		err = fmt.Errorf("%w: %v", ErrOutcomeUnknown, err)
	}
	return Reply{Output: output}, err
}

type outboundMail struct {
	To      string `json:"to"`
	CC      string `json:"cc"`
	Subject string `json:"subject"`
	Body    string `json:"body"`
}

func mailDraft(args map[string]any) (outboundMail, error) {
	var message outboundMail
	raw, err := json.Marshal(args)
	if draft, ok := args["draft"]; ok {
		if text, ok := draft.(string); ok {
			raw = []byte(text)
		} else {
			raw, err = json.Marshal(draft)
		}
	}
	if err != nil || json.Unmarshal(raw, &message) != nil || strings.TrimSpace(message.To) == "" || strings.TrimSpace(message.Body) == "" {
		return outboundMail{}, errors.New("Gmail send needs to and body, either in args or a structured draft")
	}
	return message, nil
}
