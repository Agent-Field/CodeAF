// STUB(mcp): replaced by the owner branch on merge.

package connect

// The three calls the session layer makes about an account that serves its own
// tools, and the type they speak in.
//
// They are HERE, and empty, so that the session half of the wave can be built,
// vetted and tested against the shape it will meet. The owner branch replaces
// this file whole: the type is the contract, the two methods reach the provider,
// and [MCPToolCapability] is the one line of it that is already true.
//
// A build carrying this stub behaves exactly as the build before it did — a
// service with no hand-written family serves nothing, and the conversation is
// told so in one sentence.

import (
	"context"
	"encoding/json"
)

// MCPTool is one tool a connected service serves, as the service describes it:
// its own name for it, its own sentence about it, the shape of its arguments,
// and whether running it only looks.
type MCPTool struct {
	Name        string
	Description string
	Schema      json.RawMessage
	ReadOnly    bool
}

// MCPTools asks one connected service what it serves.
//
// The list is the SERVICE'S, read live, and it may differ from the one the same
// service gave yesterday: nothing here or above caches it, and nothing writes it
// down.
func (m *Manager) MCPTools(ctx context.Context, service string) ([]MCPTool, error) {
	return nil, nil
}

// MCPCall runs one served tool and hands back what it answered, as text.
//
// The answer is BOUNDED, the way every other helper in this package bounds what
// it returns: the caller's description promises a model that long answers are
// shortened and say so.
func (m *Manager) MCPCall(ctx context.Context, service, tool string, args json.RawMessage) (string, error) {
	return "", errNoServedTools
}

// MCPToolCapability names the capability that governs one served tool: the
// generic pair, chosen by the one thing the service says about it.
//
// A tool that only looks is a read. EVERYTHING ELSE ACTS, including a tool that
// says nothing about itself — the safe reading of "I do not know" is the one
// that asks.
func MCPToolCapability(tool MCPTool) string {
	if tool.ReadOnly {
		return CapabilityRead
	}
	return CapabilityAct
}

// errNoServedTools is what a stub build answers a call it cannot make.
var errNoServedTools = errStub("this build serves no tools of its own")

type errStub string

func (e errStub) Error() string { return string(e) }
