package backing

// One agent session: what sec-af called a harness call. A coding-agent binary
// used to be spawned for each one and told to write its answer to a file in
// the repository; here it is a loop of turns on the run's model API with four
// read-only tools, ending in one JSON answer that is checked against the
// schema the agent was given.
//
// WHAT THE OLD HARNESS DID FOR THE AUDIT IS KEPT; HOW IT DID IT IS NOT. sec-af
// relied on three things from it — a session that can read the code, an
// answer of the schema it asked for, and a second chance when the answer did
// not parse ([followUps]) — and each is here. The output file in the person's
// repository, the binary and the key it held are gone, because the audit
// promised to change nothing in the folder and codeaf holds the keys.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/santhosh-tekuri/jsonschema/v5"
)

// SessionOrder is one session: who answers, where it reads, what it is asked,
// the shape of its answer, and its bounds.
type SessionOrder struct {
	Model  string
	Thread string
	// Root is the repository, absolute; every tool reads only below it.
	Root   string
	Prompt string
	// Schema is the JSON Schema the answer must meet; nil is a free-text
	// answer, which sec-af's chain correlation asks for.
	Schema   map[string]any
	MaxTurns int
	// Wall is the session's own time bound; zero is none past the context's.
	Wall time.Duration
}

// SessionResult is how a session ended: its answer, and what it cost.
type SessionResult struct {
	// Text is the answer as the model wrote it.
	Text string
	// JSON is the answer's object when a schema was asked for and met.
	JSON json.RawMessage
	// Failed is why a session that ran did not give an answer of the shape
	// asked for; empty for one that did.
	Failed   string
	Turns    int
	Tools    int
	CostUSD  float64
	Duration time.Duration
}

const (
	// followUps is how many times an answer that did not meet its schema is
	// asked for again: the old harness's own count.
	followUps = 2
	// contextChars is how much a session's transcript may hold before it is
	// told to stop reading and answer. About a hundred thousand tokens, which
	// every model a person is likely to seat holds with room for the answer.
	contextChars = 400_000
)

// errNoAnswer is a session that ran out of turns without answering.
var errNoAnswer = errors.New("the session ended without an answer")

// RunSession runs one session to its answer. The error is for a session that
// could not run at all — the model API refused, the run was stopped; a session
// that ran and gave no usable answer says so in Failed.
func RunSession(ctx context.Context, client *Client, order SessionOrder) (SessionResult, error) {
	started := time.Now()
	if order.Wall > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, order.Wall)
		defer cancel()
	}
	maxTurns := order.MaxTurns
	if maxTurns <= 0 {
		maxTurns = 50
	}
	var validate *jsonschema.Schema
	if order.Schema != nil {
		compiled, err := compileSchema(order.Schema)
		if err != nil {
			return SessionResult{}, fmt.Errorf("the answer's schema does not compile: %w", err)
		}
		validate = compiled
	}
	tools := toolbox{root: order.Root}
	messages := []ai.Message{
		textMessage("system", sessionSystem(order)),
		textMessage("user", order.Prompt),
	}
	result := SessionResult{}
	defer func() { result.Duration = time.Since(started) }()
	size := len(order.Prompt)
	asked := 0
	answering := false
	for result.Turns < maxTurns+followUps+1 {
		// THE LAST TURN, OR A FULL CONTEXT, IS AN ANSWER. A session that is
		// still reading when its turns or its room run out is told to answer
		// with what it has, and is offered no tools to do anything else.
		if !answering && (result.Turns >= maxTurns-1 || size > contextChars) {
			answering = true
			messages = append(messages, textMessage("user", "Stop reading now and give your answer from what you have found, in the form the system message asks for."))
		}
		request := Request{Model: order.Model, Thread: order.Thread, Messages: messages}
		if !answering {
			request.Tools = tools.definitions()
		}
		response, err := client.Complete(ctx, request)
		result.Turns++
		result.CostUSD += costOf(response)
		if err != nil {
			result.Failed = err.Error()
			return result, err
		}
		reply := response.Choices[0].Message
		reply.Role = "assistant"
		messages = append(messages, reply)
		if calls := reply.ToolCalls; len(calls) > 0 && !answering {
			for _, call := range calls {
				observation := tools.run(call)
				result.Tools++
				size += len(observation) + len(call.Function.Arguments)
				messages = append(messages, ai.Message{Role: "tool", ToolCallID: call.ID,
					Content: []ai.ContentPart{{Type: "text", Text: observation}}})
			}
			continue
		}
		text := strings.TrimSpace(response.Text())
		result.Text = text
		if validate == nil {
			if text == "" {
				result.Failed = errNoAnswer.Error()
			}
			return result, nil
		}
		object, problem := checkAnswer(validate, text)
		if problem == "" {
			result.JSON, result.Failed = object, ""
			return result, nil
		}
		result.Failed = problem
		if asked == followUps {
			return result, nil
		}
		asked++
		answering = true
		messages = append(messages, textMessage("user", "Your answer does not meet the required JSON Schema: "+problem+
			"\nReply with ONLY the corrected JSON object — no prose, no code fence."))
	}
	if result.Failed == "" {
		result.Failed = errNoAnswer.Error()
	}
	return result, nil
}

// sessionSystem is the session's working method: where it is, what it can do,
// and the one shape its answer takes.
func sessionSystem(order SessionOrder) string {
	var b strings.Builder
	fmt.Fprintf(&b, "You are one agent of a security audit of the repository at %s. ", order.Root)
	b.WriteString("You can read it with the tools read_file, list_dir, glob and grep, and nothing else: you cannot change any file, and there is no shell. ")
	b.WriteString("Paths are relative to the repository root. Do the task you are given and only that; read what you need to be sure, and stop when you are.\n\n")
	if order.Schema == nil {
		b.WriteString("When you are done, stop calling tools and reply with your answer as plain text.")
		return b.String()
	}
	schema, _ := json.Marshal(order.Schema)
	b.WriteString("When you are done, stop calling tools and reply with ONLY one JSON object that meets this JSON Schema — no prose before or after it and no code fence:\n")
	b.Write(schema)
	return b.String()
}

// checkAnswer is the object a reply holds and the problem with it, empty when
// it meets the schema. A reply is read the forgiving way the old harness read
// one: the whole of it, else what a code fence holds, else the span from its
// first brace to its last.
func checkAnswer(schema *jsonschema.Schema, text string) (json.RawMessage, string) {
	raw, ok := extractObject(text)
	if !ok {
		return nil, "the reply holds no JSON object"
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, "the JSON does not parse: " + err.Error()
	}
	if err := schema.Validate(value); err != nil {
		return nil, schemaProblem(err)
	}
	return raw, ""
}

// extractObject finds the JSON object in a reply.
func extractObject(text string) (json.RawMessage, bool) {
	text = strings.TrimSpace(text)
	if json.Valid([]byte(text)) && strings.HasPrefix(text, "{") {
		return json.RawMessage(text), true
	}
	if open := strings.Index(text, "```"); open >= 0 {
		inner := text[open+3:]
		inner = strings.TrimPrefix(inner, "json")
		if end := strings.Index(inner, "```"); end >= 0 {
			if candidate := strings.TrimSpace(inner[:end]); json.Valid([]byte(candidate)) && strings.HasPrefix(candidate, "{") {
				return json.RawMessage(candidate), true
			}
		}
	}
	first, last := strings.Index(text, "{"), strings.LastIndex(text, "}")
	if first >= 0 && last > first {
		if candidate := text[first : last+1]; json.Valid([]byte(candidate)) {
			return json.RawMessage(candidate), true
		}
	}
	return nil, false
}

// schemaProblem is a validation failure in a few lines the model can fix from.
func schemaProblem(err error) string {
	var invalid *jsonschema.ValidationError
	if !errors.As(err, &invalid) {
		return err.Error()
	}
	var lines []string
	var walk func(e *jsonschema.ValidationError)
	walk = func(e *jsonschema.ValidationError) {
		if len(e.Causes) == 0 {
			where := e.InstanceLocation
			if where == "" {
				where = "the answer"
			}
			lines = append(lines, where+": "+e.Message)
			return
		}
		for _, cause := range e.Causes {
			walk(cause)
		}
	}
	walk(invalid)
	if len(lines) > 8 {
		lines = append(lines[:8], fmt.Sprintf("and %d more", len(lines)-8))
	}
	return strings.Join(lines, "; ")
}

// compileSchema compiles one schema for validation.
func compileSchema(schema map[string]any) (*jsonschema.Schema, error) {
	raw, err := json.Marshal(schema)
	if err != nil {
		return nil, err
	}
	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource("answer.json", strings.NewReader(string(raw))); err != nil {
		return nil, err
	}
	return compiler.Compile("answer.json")
}

func textMessage(role, text string) ai.Message {
	return ai.Message{Role: role, Content: []ai.ContentPart{{Type: "text", Text: text}}}
}
