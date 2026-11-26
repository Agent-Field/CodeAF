package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type appMCPRequest struct {
	App     string          `json:"app"`
	Agent   string          `json:"agent"`
	Harness string          `json:"harness,omitempty"`
	Kind    string          `json:"kind"`
	Target  string          `json:"target,omitempty"`
	Args    json.RawMessage `json:"args,omitempty"`
}

type appMCPTool struct {
	ID    string          `json:"id"`
	Kind  string          `json:"kind"`
	Label string          `json:"label"`
	Input json.RawMessage `json:"input"`
}

func runAppMCP(args []string) error {
	flags := flag.NewFlagSet("codeaf app mcp", flag.ContinueOnError)
	app := flags.String("app", "", "The kept app ID")
	agent := flags.String("agent", "", "The agent ID chosen by the app owner")
	harness := flags.String("harness", "codeaf", "The external agent harness name")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || strings.TrimSpace(*app) == "" || strings.TrimSpace(*agent) == "" {
		return errors.New("usage: codeaf app mcp --app <app> --agent <agent>")
	}
	scope := appMCPRequest{App: *app, Agent: *agent, Harness: *harness}
	call := func(ctx context.Context, kind, id string, args json.RawMessage) (json.RawMessage, error) {
		ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		p := scope
		p.Kind, p.Target, p.Args = kind, id, args
		raw, err := json.Marshal(p)
		if err != nil {
			return nil, err
		}
		return requestAppCommand(ctx, "_mcp", raw)
	}
	catalog, err := call(context.Background(), "register", "", nil)
	if err != nil {
		return err
	}
	var tools []appMCPTool
	if err := json.Unmarshal(catalog, &tools); err != nil {
		return err
	}
	server := mcp.NewServer(&mcp.Implementation{Name: "codeaf-app", Version: "1"}, nil)
	for _, tool := range tools {
		id := sha256.Sum256([]byte(tool.ID))
		server.AddTool(&mcp.Tool{
			Name:  tool.Kind + "_" + hex.EncodeToString(id[:8]),
			Title: tool.Label, Description: tool.Label + " (" + tool.ID + ")",
			InputSchema: tool.Input,
			Annotations: &mcp.ToolAnnotations{ReadOnlyHint: tool.Kind == "query" || tool.Kind == "look"},
		}, func(ctx context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			raw, err := call(ctx, tool.Kind, tool.ID, request.Params.Arguments)
			if err != nil {
				return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}}}, nil
			}
			if tool.Kind == "look" {
				content, err := appMCPLookImage(raw)
				if err != nil {
					return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}}}, nil
				}
				return &mcp.CallToolResult{Content: content}, nil
			}
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(raw)}}}, nil
		})
	}
	return server.Run(context.Background(), &mcp.StdioTransport{})
}

func appMCPLookImage(raw json.RawMessage) ([]mcp.Content, error) {
	var page appMCPImage
	if err := json.Unmarshal(raw, &page); err != nil || len(page.PNG) == 0 || len(page.PNG) > 3<<20 {
		return nil, errors.New("the desk returned an invalid preview")
	}
	problems, _ := json.Marshal(page.Problems)
	return []mcp.Content{&mcp.ImageContent{Data: page.PNG, MIMEType: "image/png"}, &mcp.TextContent{Text: string(problems)}}, nil
}
