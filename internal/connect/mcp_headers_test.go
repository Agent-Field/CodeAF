package connect

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Contract C1 and C5: GitHub receives both annotated argument headers on a
// fresh call, including when the tool is on a later page of the list.
func TestGitHubToolsSendRepositoryHeaders(t *testing.T) {
	server := paginatedGitHubToolServer()
	const answer = "Repository found"
	const headerProtocol = "2026-07-28"
	server.AddTool(&mcp.Tool{
		Name: "get_repository",
		InputSchema: json.RawMessage(`{
			"type":"object",
			"properties":{
				"owner":{"type":"string","x-mcp-header":"Owner"},
				"repo":{"type":"string","x-mcp-header":"Repo"}
			},
			"required":["owner","repo"]
		}`),
	}, func(_ context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		headers := req.Extra.Header
		if got := headers.Get("Mcp-Protocol-Version"); got < headerProtocol {
			t.Errorf("protocol version = %q, want %s or later", got, headerProtocol)
		}
		if got := headers.Get("Mcp-Param-Owner"); got != "octocat" {
			t.Errorf("Mcp-Param-Owner = %q, want octocat", got)
		}
		if got := headers.Get("Mcp-Param-Repo"); got != "Hello-World" {
			t.Errorf("Mcp-Param-Repo = %q, want Hello-World", got)
		}
		return said(answer), nil
	})
	manager := managerForGitHubToolServer(t, server)
	got, err := manager.MCPCall(context.Background(), GitHubID, "get_repository", json.RawMessage(`{"owner":"octocat","repo":"Hello-World"}`))
	if err != nil || got != answer {
		t.Fatalf("MCPCall = %q, %v, want %q", got, err, answer)
	}
}

// Contract C3 and C4: A plain tool still returns its answer when the server
// refuses to list tools in the calling session.
func TestGitHubToolsCallWithoutSuccessfulListing(t *testing.T) {
	server := paginatedGitHubToolServer()
	var listingRefused atomic.Bool
	server.AddReceivingMiddleware(func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			if method == "tools/list" {
				listingRefused.Store(true)
				return nil, &jsonrpc.Error{Code: jsonrpc.CodeMethodNotFound, Message: "Tool listing is unavailable"}
			}
			return next(ctx, method, req)
		}
	})
	manager := managerForGitHubToolServer(t, server)
	args := json.RawMessage(`{"query":"minutes"}`)
	got, err := manager.MCPCall(context.Background(), GitHubID, "find_pages", args)
	if want := "Found: " + string(args); err != nil || got != want {
		t.Fatalf("MCPCall = %q, %v, want %q", got, err, want)
	}
	if !listingRefused.Load() {
		t.Fatal("the server did not get a listing request to refuse")
	}
}

// The SDK sorts tools by name, so find_pages fills the first page.
func paginatedGitHubToolServer() *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "example", Version: "1"}, &mcp.ServerOptions{PageSize: 1})
	server.AddTool(&mcp.Tool{
		Name:        "find_pages",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"}}}`),
	}, func(_ context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return said("Found: " + string(req.Params.Arguments)), nil
	})
	return server
}

func managerForGitHubToolServer(t *testing.T, server *mcp.Server) *Manager {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server },
		&mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true})
	standIn := httptest.NewServer(handler)
	t.Cleanup(standIn.Close)
	t.Setenv("CODEAF_GITHUB_MCP_URL", standIn.URL)
	return gitHubManager(t, &fakeGitHub{token: "shared-token", via: "token"}, false)
}
