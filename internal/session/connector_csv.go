package session

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/approval"
	"github.com/Agent-Field/codeaf/internal/connect"
	"github.com/Agent-Field/codeaf/internal/exec/bare"
	"github.com/Agent-Field/codeaf/internal/modelsource"
	"github.com/Agent-Field/codeaf/internal/provider"
	"github.com/Agent-Field/codeaf/internal/roles"
)

// ONE READ OF ONE CONNECTED ACCOUNT, AS A CSV, FOR A SHEET.
//
// A person who set a sheet tab to pull from a connector presses Pull, and the
// app asks the engine for that account's rows as CSV text (the desk's canvas
// contract, §12.13: `plugins.read_csv {id, query, max_bytes}` → `{csv}`). No
// account in this build answers with a CSV of its own — mail, a key account's
// raw API and a served tool list each answer in their own shape — so the read
// is done the way a conversation would do it, by a model with the account's
// tools in hand, under three laws a conversation does not have:
//
//   - READ-ONLY BY CONSTRUCTION. The belt holds that one account's tools that
//     only look, and nothing else: no shell, no files, no other account. A raw
//     key call is armed for `get` alone and refuses every other verb at the
//     moment it is made. A tool the person turned off stays off. An account
//     with nothing that only reads is refused before any model is asked.
//   - REFUSED, NEVER CUT. The answer must be a whole CSV under the caller's
//     byte ceiling; one over it is refused with the ceiling named, because a
//     sheet that landed half an export reads as the whole of it.
//   - NOTHING IS LOGGED. The rows are a person's data. This file writes no
//     line about what the account said or what the model answered, and an
//     error carries the reason, never the rows.
//
// The pull is the person's own click, which is why a read the person left on
// "ask" runs here without a second question: they asked for exactly this read
// of exactly this account a moment ago.

// ConnectorCSVMaxBytes is the most one read may answer, and the ceiling a
// caller's own max_bytes is clamped to: the desk's sheet pull asks for 64 MB.
const ConnectorCSVMaxBytes = 64 << 20

// connectorCSVRounds bounds the model's rounds of tool calls. A read that has
// not settled into an answer by then is refused, not cut short.
const connectorCSVRounds = 12

// connectorCSVWait bounds the whole read. The desk gives up on the call at 90
// seconds; ending a little before that lets the refusal reach it as a reason.
const connectorCSVWait = 85 * time.Second

// connectorCSVRefusal is the line the model answers with when the query cannot
// be met by reading. Its reason is carried to the person as said.
const connectorCSVRefusal = "CANNOT:"

const connectorCSVSystem = `You read data from one of the person's connected accounts and answer with it as CSV, for a spreadsheet tab.

You have only tools that read that account. Nothing you do may change anything in it, and no other tool exists.

Use the tools to find what the request asks for, then answer with the CSV and nothing else:
- the first line is the header row, one name per column;
- one line per record, every line with the same number of fields;
- comma-separated, with a field quoted in double quotes when it holds a comma, a quote or a line break, and a quote inside a field doubled;
- no commentary, no explanation, no code fence.

A tool answer that says it was shortened holds only part of what was asked: ask again for the rest in smaller pages, the way the tool's own parameters allow. If you cannot read every record the request covers, do not answer with part of them.

If the request cannot be met by reading this account, or would need anything other than reading, answer with one line that starts with ` + "`" + connectorCSVRefusal + "`" + ` followed by the reason in plain words for the person.`

// ConnectorCSVError is a read the engine would not or could not make, with
// the reason a person can act on as its whole text: it becomes the sheet's
// "last pull failed" line as written. Code says which kind:
// [ConnectorCSVRefused], [ConnectorCSVTooLarge] or [ConnectorCSVSignedOut].
type ConnectorCSVError struct {
	Code string
	Why  string
}

func (e *ConnectorCSVError) Error() string { return e.Why }

// The kinds of [ConnectorCSVError], spelled as the app wire's error codes.
const (
	ConnectorCSVRefused   = "refused"
	ConnectorCSVTooLarge  = "too_large"
	ConnectorCSVSignedOut = "signed_out"
)

func connectorCSVRefused(format string, args ...any) error {
	return &ConnectorCSVError{Code: ConnectorCSVRefused, Why: fmt.Sprintf(format, args...)}
}

// ConnectorReader reads one connected account into a CSV. It is a model and
// the person's connected accounts, and it holds no conversation.
type ConnectorReader struct{ agent *Agent }

// ConnectorReaderConfig is what the engine hands a reader: the same model and
// key settings a [TeamProposer] takes, and the person's connections.
type ConnectorReaderConfig struct {
	Model       string
	APIKey      string
	BaseURL     string
	ProfileDir  string
	Sources     modelsource.Set
	RolesSource func(string) (string, bool)
	Connect     *connect.Manager
}

// NewConnectorReader builds the reader the way [NewTeamProposer] builds its
// agent: a session client and nothing else of a conversation.
func NewConnectorReader(settings ConnectorReaderConfig) (*ConnectorReader, error) {
	if settings.Connect == nil {
		return nil, errors.New("connections could not be read")
	}
	if strings.TrimSpace(settings.Model) == "" {
		return nil, errors.New("no model is set to read the connector with")
	}
	config := Config{Model: settings.Model, APIKey: settings.APIKey, BaseURL: settings.BaseURL, ProfileDir: settings.ProfileDir,
		Sources: settings.Sources, RolesSource: settings.RolesSource, Connect: settings.Connect}
	config.bindUsageLedger()
	client, err := newProviderClient(config, config.Model)
	if err != nil {
		return nil, err
	}
	a := &Agent{config: config, model: config.Model, connect: newConnectHub(config)}
	a.installSessionClient(client, config)
	a.manageClient(config)
	return &ConnectorReader{agent: a}, nil
}

// ReadCSV is one read of the account `id`, as CSV, answering what `query` asks
// for; maxBytes bounds the answer and is clamped to [ConnectorCSVMaxBytes].
func (r *ConnectorReader) ReadCSV(ctx context.Context, id, query string, maxBytes int64) (string, error) {
	return r.agent.readConnectorCSV(ctx, id, query, maxBytes)
}

func (a *Agent) readConnectorCSV(ctx context.Context, id, query string, maxBytes int64) (string, error) {
	if maxBytes <= 0 || maxBytes > ConnectorCSVMaxBytes {
		maxBytes = ConnectorCSVMaxBytes
	}
	if a.connect == nil {
		return "", errors.New("connections could not be read")
	}
	service, known := a.service(id)
	if !known {
		return "", connectorCSVRefused("there is no %s connector in Settings › Connectors", strings.TrimSpace(id))
	}
	if !service.Connected {
		return "", &ConnectorCSVError{Code: ConnectorCSVSignedOut, Why: service.Name + " is signed out; sign in again in Settings › Connectors"}
	}
	ctx, cancel := context.WithTimeout(ctx, connectorCSVWait)
	defer cancel()

	tools, err := a.connectorReadTools(ctx, service)
	if err != nil {
		return "", err
	}
	if len(tools) == 0 {
		return "", connectorCSVRefused("%s has nothing turned on that only reads, so it cannot be read into a sheet", service.Name)
	}
	definitions, err := toolDefinitions(tools)
	if err != nil {
		return "", err
	}
	byName := make(map[string]bare.Tool, len(tools))
	for _, tool := range tools {
		byName[tool.Name] = tool
	}

	ask := strings.TrimSpace(query)
	if ask == "" {
		ask = "The main records this account holds, the ones a person would most likely want in a sheet."
	}
	messages := []ai.Message{
		{Role: "system", Content: []ai.ContentPart{{Type: "text", Text: connectorCSVSystem}}},
		{Role: "user", Content: []ai.ContentPart{{Type: "text", Text: "Account: " + service.Name + "\nRequest: " + ask}}},
	}
	for round := 0; round < connectorCSVRounds; round++ {
		response, called, err := a.callRole(withDetachedUsage(ctx), roles.RoleWorker, a.Model(), messages, ai.WithTools(definitions))
		if err != nil {
			return "", err
		}
		// EVERY ROUND IS PAID FOR and banked detached: a person pressed Pull on
		// a sheet, not a turn of any conversation.
		a.addDetachedUsageAs(response, called, 1, string(roles.RoleWorker))
		if len(response.Choices) == 0 {
			return "", connectorCSVRefused("the model returned no answer")
		}
		message := response.Choices[0].Message
		// A REPLY CUT AT THE MODEL'S CEILING IS NEVER A SHEET. Cut on a row
		// boundary it would parse cleanly and land as the whole export.
		if provider.FinishReason(response) == "length" {
			return "", connectorCSVRefused("the answer from %s was longer than the model could write in one reply; ask for fewer rows or columns", service.Name)
		}
		if len(message.ToolCalls) == 0 {
			return connectorCSVAnswer(messageContentText(message), maxBytes)
		}
		messages = append(messages, message)
		for _, call := range message.ToolCalls {
			messages = append(messages, ai.Message{Role: "tool", ToolCallID: call.ID,
				Content: []ai.ContentPart{{Type: "text", Text: a.runConnectorReadTool(ctx, byName, call)}}})
		}
	}
	return "", connectorCSVRefused("the read of %s did not finish in %d rounds", service.Name, connectorCSVRounds)
}

// runConnectorReadTool runs one call the model made, and answers the model in
// words whatever happened: a call to a tool that is not on this belt, or one
// that would act, is refused here and never run.
func (a *Agent) runConnectorReadTool(ctx context.Context, byName map[string]bare.Tool, call ai.ToolCall) string {
	tool, held := byName[call.Function.Name]
	if !held {
		return "There is no tool called " + call.Function.Name + " here; only the reading tools listed are."
	}
	args := json.RawMessage(call.Function.Arguments)
	if len(bytes.TrimSpace(args)) == 0 {
		args = json.RawMessage(`{}`)
	}
	// THE SECOND LOCK. The belt was built from tools that only read; a raw key
	// call is the one tool whose verb decides, so every call is judged again
	// here by the same reading the consent floor uses.
	if a.actsInThePersonsName(tool.Name, args) {
		return "Refused: this read may only look at the account, so " + tool.Name + " with these arguments does not run."
	}
	text, _, err := tool.Execute(ctx, args)
	if err != nil {
		return "That call failed: " + err.Error()
	}
	return text
}

// connectorReadTools is the account's tools that only look and that the
// person has left on, in the three shapes an account can bring.
func (a *Agent) connectorReadTools(ctx context.Context, service connectStatus) ([]bare.Tool, error) {
	if a.servesItsOwn(service) {
		served, err := a.connect.MCPTools(ctx, service.ID)
		if err != nil {
			return nil, err
		}
		reads := make([]connect.MCPTool, 0, len(served))
		for _, tool := range served {
			if connect.MCPToolCapability(tool) == connect.CapabilityRead && a.capabilityAllows(service.ID, connect.CapabilityRead) {
				reads = append(reads, tool)
			}
		}
		tools, _, _ := a.servedBelt(service, reads)
		return tools, nil
	}
	var reads []bare.Tool
	for _, tool := range a.liveTools(service.ID, a.familyTools(service)) {
		if rawCall(tool.Name) {
			if !a.capabilityAllows(service.ID, connect.CapabilityRead) {
				continue
			}
			tool.Description = "Make one get call to the person's own " + service.Name + " account" +
				connectorAddress(service) + ". Only get runs here: this read may not change anything. " +
				"Follow " + service.Name + "'s own published documentation for paths and parameters. Long answers are shortened and say so."
			reads = append(reads, tool)
			continue
		}
		// AN ALLOWLIST, NOT A DENYLIST. A family tool is on the read belt only
		// when the capability that owns it is declared and does not act, so a
		// tool added to a family later, and missing from the approval table,
		// is left off rather than armed by default.
		if !a.ownedByAReadingCapability(service.ID, tool.Name) ||
			approval.ActsInThePersonsName(tool.Name, json.RawMessage(`{}`)) {
			continue
		}
		reads = append(reads, tool)
	}
	return reads, nil
}

// ownedByAReadingCapability reports that a family tool's owning capability is
// declared for the account and is one that only looks.
func (a *Agent) ownedByAReadingCapability(service, tool string) bool {
	owner := a.connect.ToolCapability(service, tool)
	if owner == "" {
		return false
	}
	for _, capability := range a.connect.Capabilities(service) {
		if capability.ID == owner {
			return !capability.Acts
		}
	}
	return false
}

func connectorAddress(service connectStatus) string {
	if address := strings.TrimSpace(service.Address); address != "" {
		return "; paths are relative to " + address
	}
	return ""
}

// connectorCSVAnswer turns the model's last words into the CSV the caller
// gets, or refuses them. A code fence is taken off because models add one
// whatever they are told; nothing else is changed.
func connectorCSVAnswer(text string, maxBytes int64) (string, error) {
	text = strings.TrimSpace(text)
	if why, refused := strings.CutPrefix(text, connectorCSVRefusal); refused {
		why = strings.TrimSpace(why)
		if why == "" {
			why = "it did not say why"
		}
		return "", connectorCSVRefused("%s", clipWords(why, 500))
	}
	if strings.HasPrefix(text, "```") {
		if _, body, found := strings.Cut(text, "\n"); found {
			text = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(body), "```"))
		}
	}
	if text == "" {
		return "", connectorCSVRefused("the connector had nothing to answer with")
	}
	if !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	if int64(len(text)) > maxBytes {
		return "", &ConnectorCSVError{Code: ConnectorCSVTooLarge, Why: "the connector answered with more than " + connectorCSVSize(maxBytes)}
	}
	// A WHOLE CSV OR NOTHING. A reply that does not parse, or whose rows do not
	// all have the header's width, is refused rather than handed to a sheet
	// that would land it with its columns shifted.
	reader := csv.NewReader(strings.NewReader(text))
	rows := 0
	for {
		_, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", connectorCSVRefused("the answer was not a CSV a sheet can take (row %d)", rows+1)
		}
		rows++
	}
	if rows == 0 {
		return "", connectorCSVRefused("the connector had nothing to answer with")
	}
	return text, nil
}

// connectorCSVSize names a ceiling in the unit a person reads it in.
func connectorCSVSize(n int64) string {
	if n >= 1<<20 {
		return fmt.Sprintf("%d MB", n>>20)
	}
	if n >= 1<<10 {
		return fmt.Sprintf("%d KB", n>>10)
	}
	return fmt.Sprintf("%d bytes", n)
}
