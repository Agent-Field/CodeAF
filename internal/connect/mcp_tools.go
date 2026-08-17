package connect

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/oauth2"
)

// Asking a connected service what it can do, and asking it to do one thing.
//
// ── THE TWO METHODS ARE THE WHOLE SURFACE ──
//
// [Manager.MCPTools] answers with a list a belt can be built from.
// [Manager.MCPCall] runs one and answers with text a model can read. Nothing
// else about the arrangement leaves this package: no session, no connection, no
// vocabulary from somebody else's protocol. A caller that wanted to hold a
// connection open would be holding a thing it cannot renew, cannot reconnect and
// cannot reason about the lifetime of, so it is not offered one.
//
// ── A CONNECTION PER ASK ──
//
// Each call opens a connection, does its one thing and closes it. That is the
// right trade for a program where a person's chat asks for a tool now and again:
// a held-open connection would have to survive sleep, a changed network and a
// renewed key, and it would have to do that in a package whose whole design is
// that a caller holds nothing but a [Manager]. The cost is one round-trip of
// setting up, on top of a call that is already crossing the internet.
//
// WHAT IS NOT PAID TWICE IS THE TOOL LIST. See [Manager.MCPTools].

const (
	// mcpCallTimeout is the outer backstop on one exchange with a tool server.
	// The real deadline is the context the caller passes; this only stops a
	// call nobody bounded from hanging for the life of the process. It is more
	// generous than [clientTimeout] because the work behind one of these calls
	// is somebody else's search, not one fetch.
	mcpCallTimeout = 2 * time.Minute
	// mcpClientVersion is what aforge calls this version of itself when it
	// says hello. It is machinery, seen only by the service.
	mcpClientVersion = "1"
)

var (
	mcpToolsMu sync.Mutex
	// mcpToolsHeld is the tool list each service gave, remembered for the life
	// of the process.
	mcpToolsHeld = map[string][]MCPTool{}
)

// MCPTools lists what one connected service can do, as its own tools.
//
// THE LIST IS FETCHED ONCE PER RUN. It costs a connection and a round-trip, the
// answer is the same for every call in a sitting, and a belt is rebuilt far more
// often than a service adds a tool. A person who connects a service, uses it,
// and finds a new tool missing has only to start aforge again — which is a much
// smaller surprise than every turn of a conversation paying for a list that has
// not changed since the one before.
//
// A service that is not connected is an error and not an empty list: a caller
// that got no tools would arm nothing and say nothing, and the person would be
// left wondering why the thing they connected does nothing.
func (m *Manager) MCPTools(ctx context.Context, service string) ([]MCPTool, error) {
	plug, err := m.server(service)
	if err != nil {
		return nil, err
	}
	if held, ok := heldTools(plug.service.ID); ok {
		return held, nil
	}
	session, err := m.open(ctx, plug)
	if err != nil {
		return nil, err
	}
	defer func() { _ = session.Close() }()

	var tools []MCPTool
	// The iterator asks for the next page as it goes, so a service with more
	// tools than fit in one answer is read whole.
	for tool, err := range session.Tools(ctx, nil) {
		if err != nil {
			return nil, fmt.Errorf("%s: %w", plug.service.Name, err)
		}
		if tool == nil || strings.TrimSpace(tool.Name) == "" {
			continue
		}
		tools = append(tools, asTool(tool))
	}
	holdTools(plug.service.ID, tools)
	return append([]MCPTool(nil), tools...), nil
}

// MCPCall runs one of a service's own tools and answers with what it said, as
// text.
//
// THE ANSWER IS BOUNDED AT [maxToolText] AND THE CUT IS ANNOUNCED, exactly as
// every other tool answer in this package is (api.go): the reader is a model
// with a finite context, and a silently truncated answer is one it will reason
// confidently about the missing half of.
//
// A TOOL THAT REFUSES IS AN ERROR, carrying the service's own sentence. The
// distinction the protocol draws — a failure of the call against a failure of
// the tool — is not one a caller here can act on differently, and collapsing it
// keeps one shape for "this did not happen".
func (m *Manager) MCPCall(ctx context.Context, service, tool string, args json.RawMessage) (string, error) {
	plug, err := m.server(service)
	if err != nil {
		return "", err
	}
	name := strings.TrimSpace(tool)
	if name == "" {
		return "", fmt.Errorf("%s was asked to run a tool with no name", plug.service.Name)
	}
	arguments, err := callArguments(args)
	if err != nil {
		return "", fmt.Errorf("%s: %w", name, err)
	}
	session, err := m.open(ctx, plug)
	if err != nil {
		return "", err
	}
	defer func() { _ = session.Close() }()

	answer, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: arguments})
	if err != nil {
		return "", fmt.Errorf("%s: %w", plug.service.Name, err)
	}
	said := bound(squeeze(spoken(answer)))
	if answer.IsError {
		if said == "" {
			said = "it did not say why"
		}
		return "", fmt.Errorf("%s refused %s: %s", plug.service.Name, name, said)
	}
	return said, nil
}

// open connects to one tool server on the person's behalf.
//
// The connection carries the stored keys and renews them underneath itself when
// they age out, and A RENEWAL IS WRITTEN DOWN — the same [persisting] wrapper
// every other connection in this package renews through, for the same reason:
// a program that uses a new key but keeps the old one on disk works beautifully
// until it is restarted.
func (m *Manager) open(ctx context.Context, plug *toolServer) (*mcp.ClientSession, error) {
	service := plug.service
	entry, ok := m.standing(plug)
	if !ok {
		return nil, fmt.Errorf("%s is not connected", service.Name)
	}
	record, held := m.registrations().get(service.ID)
	if !held || !record.fitsServer(plug.address) {
		// The keys are there and what they were issued to is not, so there is
		// no way to renew them and no honest way to use them. Signing in again
		// writes both.
		return nil, fmt.Errorf("%s has to be connected again", service.Name)
	}
	if entry.Keys == nil || (!entry.Keys.Valid() && strings.TrimSpace(entry.Keys.RefreshToken) == "") {
		return nil, fmt.Errorf("%s has to be connected again", service.Name)
	}

	source := &persisting{
		base:  record.config().TokenSource(ctx, entry.Keys),
		store: m.store,
		id:    service.ID,
		last:  entry.Keys,
	}
	client := oauth2.NewClient(ctx, source)
	client.Timeout = mcpCallTimeout

	session, err := mcpClient().Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint:   plug.address,
		HTTPClient: client,
		// Nothing here listens for a service's own announcements: every call
		// is a question with an answer, and a stream held open for messages
		// nobody reads is a connection to keep alive for nothing.
		DisableStandaloneSSE: true,
	}, nil)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", service.Name, err)
	}
	return session, nil
}

// mcpClient is aforge as the service sees it.
func mcpClient() *mcp.Client {
	return mcp.NewClient(&mcp.Implementation{Name: mcpClientName, Version: mcpClientVersion}, nil)
}

// asTool copies one tool out of the protocol's shape into ours, so that nothing
// past this file has to know the protocol's.
func asTool(tool *mcp.Tool) MCPTool {
	out := MCPTool{
		Name:        strings.TrimSpace(tool.Name),
		Description: strings.TrimSpace(tool.Description),
		Schema:      schemaOf(tool.InputSchema),
	}
	if tool.Annotations != nil {
		out.ReadOnly = tool.Annotations.ReadOnlyHint
	}
	return out
}

// schemaOf is the tool's arguments as the service published them, passed
// through untouched.
//
// A tool that published nothing readable gets the empty object, which is the
// true statement "this takes no arguments I can describe" and is a thing a belt
// can be built from. The alternative — no schema at all — is not: a tool with no
// arguments and a tool whose arguments are unknown would look identical, and the
// second one would be called wrongly, once, by every model that met it.
func schemaOf(schema any) json.RawMessage {
	const nothing = `{"type":"object"}`
	if schema == nil {
		return json.RawMessage(nothing)
	}
	encoded, err := json.Marshal(schema)
	if err != nil || len(encoded) == 0 || string(encoded) == "null" {
		return json.RawMessage(nothing)
	}
	return json.RawMessage(encoded)
}

// callArguments reads what the caller wants to pass to a tool.
//
// NOTHING IS A LEGITIMATE ARGUMENT LIST and becomes no arguments at all rather
// than an empty object, because some tools distinguish the two. Anything that is
// not a JSON object is refused here rather than at the far end, where the
// refusal would be somebody else's sentence about a mistake made in this
// process.
func callArguments(args json.RawMessage) (any, error) {
	trimmed := strings.TrimSpace(string(args))
	if trimmed == "" || trimmed == "null" {
		return nil, nil
	}
	if !json.Valid([]byte(trimmed)) {
		return nil, fmt.Errorf("the arguments are not readable")
	}
	if trimmed[0] != '{' {
		return nil, fmt.Errorf("the arguments must be an object")
	}
	return json.RawMessage(trimmed), nil
}

// spoken renders what a tool said as the text a model reads.
//
// A PIECE THAT IS NOT TEXT IS NAMED, NOT CARRIED. An image or a sound comes back
// as bytes that would be nonsense in a conversation and would spend the whole of
// the caller's budget saying nothing; a line saying what was there is the honest
// and useful thing.
func spoken(answer *mcp.CallToolResult) string {
	if answer == nil {
		return ""
	}
	var said []string
	for _, piece := range answer.Content {
		switch content := piece.(type) {
		case *mcp.TextContent:
			said = append(said, content.Text)
		case *mcp.ImageContent:
			said = append(said, "[an image]")
		case *mcp.AudioContent:
			said = append(said, "[a sound]")
		case *mcp.ResourceLink:
			said = append(said, linkText(content))
		case *mcp.EmbeddedResource:
			said = append(said, resourceText(content))
		}
	}
	joined := strings.Join(said, "\n")
	if strings.TrimSpace(joined) != "" {
		return joined
	}
	// Some tools answer only in a structured value, with nothing said in
	// words. It is passed through as it stands rather than summarised.
	if answer.StructuredContent != nil {
		if encoded, err := json.MarshalIndent(answer.StructuredContent, "", "  "); err == nil {
			return string(encoded)
		}
	}
	return joined
}

// linkText names something the tool pointed at rather than sent.
func linkText(link *mcp.ResourceLink) string {
	name := strings.TrimSpace(link.Title)
	if name == "" {
		name = strings.TrimSpace(link.Name)
	}
	if name == "" {
		return link.URI
	}
	return name + " — " + link.URI
}

// resourceText renders something the tool sent along with its answer: its words
// if it has any, and otherwise a line saying what and where it was.
func resourceText(embedded *mcp.EmbeddedResource) string {
	if embedded.Resource == nil {
		return ""
	}
	if text := embedded.Resource.Text; strings.TrimSpace(text) != "" {
		return text
	}
	kind := strings.TrimSpace(embedded.Resource.MIMEType)
	if kind == "" {
		kind = "a file"
	}
	return "[" + kind + " at " + embedded.Resource.URI + "]"
}

// heldTools answers with the list this process already fetched for one service.
// The copy is deliberate: a caller that sorts or trims what it was handed must
// not reorder the list for everybody else.
func heldTools(id string) ([]MCPTool, bool) {
	mcpToolsMu.Lock()
	defer mcpToolsMu.Unlock()
	held, ok := mcpToolsHeld[id]
	if !ok {
		return nil, false
	}
	return append([]MCPTool(nil), held...), true
}

// holdTools remembers one service's list for the rest of the run.
//
// AN EMPTY LIST IS NOT REMEMBERED. A service that answered with no tools at all
// has almost certainly not finished waking up, or has answered a question it did
// not understand; remembering that would turn a bad minute into a dead service
// for the rest of the session.
func holdTools(id string, tools []MCPTool) {
	if len(tools) == 0 {
		return
	}
	mcpToolsMu.Lock()
	defer mcpToolsMu.Unlock()
	mcpToolsHeld[id] = append([]MCPTool(nil), tools...)
}

// forgetTools drops what was remembered, for a test that wants a fresh process's
// behaviour without being one.
func forgetTools() {
	mcpToolsMu.Lock()
	defer mcpToolsMu.Unlock()
	clear(mcpToolsHeld)
}
