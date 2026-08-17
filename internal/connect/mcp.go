package connect

// The tool servers: the services that bring their OWN tools rather than ours.
//
// ── WHAT IS DIFFERENT ABOUT THEM, AND WHAT IS NOT ──
//
// Google (google.go) is a service this package knows by heart: aforge was told
// its addresses, was registered in its console, and every tool it offers —
// searching mail, moving an event — is a function somebody here wrote. The
// catalog (catalog.go) is the opposite extreme: hundreds of services aforge
// knows nothing about beyond where they answer and where the key goes, with one
// raw call as their whole tool surface.
//
// A tool server sits in neither place. Notion, Linear and the rest now run a
// service of their own whose entire job is to hand a program a LIST OF TOOLS —
// their names, their arguments, what each one does — and to run one when asked.
// So the tools are not written here and not absent either: they are fetched,
// per account, at the moment they are needed. What this file owns is the way in.
//
// THE REST OF THE PROGRAM SEES NOTHING NEW. A tool server is a [Plug] on the one
// registry, it appears in [Manager.Services] as a browser connection like Google,
// it is connected with [Manager.BeginAuth], forgotten with [Manager.Disconnect],
// and its keys live in the one store file beside everybody else's. No screen, no
// settings panel and no menu learns a second kind of service. What is new is two
// methods — [Manager.MCPTools] and [Manager.MCPCall] — and they are new because
// there was previously nothing that could ask a service what it can do.
//
// ── NOBODY REGISTERS ANYTHING FIRST ──
//
// A browser connection normally needs an application registered by hand in the
// vendor's developer console, which is why [Manager.Services] hides a service
// this build holds no client credential for. A tool server needs none: aforge
// introduces itself to the service at connect time, is issued an identity on the
// spot, and keeps it (mcp_registration.go). That is the whole reason these five
// services can be shipped as a list rather than as five console visits.
//
// ── THE VOCABULARY ──
//
// A person connects "Notion". They do not connect a protocol, a server or a
// grant, and no string this package puts in front of them says otherwise. The
// machinery has its names in here, where the reader is somebody maintaining it.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"golang.org/x/oauth2"
)

// authMCP is what a tool-server connection calls itself in the store file.
//
// IT IS A KIND OF ENTRY, NOT A KIND OF SERVICE. [Service.Auth] still says
// [AuthBrowser] — a person signs in in a browser, which is the only thing that
// word is for — and every screen goes on reading it exactly as it reads Google.
// This word is written beside the keys so that a later build reading the file
// can tell an entry whose renewal needs a registration (mcp_registration.go)
// from one whose renewal needs a client credential from configuration.
const authMCP = "mcp"

// MCPTool is one tool a connected service offers, in the shape a belt needs:
// flat, already decided, nothing left to interpret.
type MCPTool struct {
	// Name is what the tool is called at the far end, and what
	// [Manager.MCPCall] must be given back to run it.
	Name string
	// Description is the service's own sentence about what the tool does,
	// written for a model to read.
	Description string
	// Schema is the arguments the tool takes, as a JSON Schema object exactly
	// as the service published it. It is passed through rather than parsed:
	// this package has no opinion about anybody else's arguments.
	Schema json.RawMessage
	// ReadOnly reports that the service marked this tool as one that only
	// looks. See [MCPToolCapability] for what is done with it.
	//
	// ABSENT MEANS FALSE, AND FALSE IS THE STRICTER READING. A service that
	// says nothing about a tool has not promised that it only looks, and the
	// cost of the two mistakes is not symmetric: treating a read as an act
	// costs one confirmation, treating an act as a read costs whatever the
	// act did.
	ReadOnly bool
}

// MCPToolCapability answers which of the two generic capabilities owns one tool.
//
// ── WHY THIS IS A FUNCTION AND NOT A MAP ──
//
// [RegisterCapabilities] takes a map from tool name to capability, written at
// init from a list somebody typed. A tool server's tools are not knowable at
// init: they are fetched per account, they differ between two people's Notion
// accounts, and the service may add one tomorrow. So the map for these services
// is empty and this is the door instead — the same question, asked of a tool
// rather than of a name.
//
// It is deliberately NOT a [Manager] method: the answer depends on the tool and
// nothing else, so there is nothing for a manager to contribute.
func MCPToolCapability(tool MCPTool) string {
	if tool.ReadOnly {
		return CapabilityRead
	}
	return CapabilityAct
}

// MCPService reports whether one service brings tools of its own.
//
// It is the question a caller arming a belt has to ask, and it cannot be
// answered from [Status] alone: a tool server and Google are both browser
// connections, and what differs is where their tools come from. A service this
// answers yes for is armed from [Manager.MCPTools] and called with
// [Manager.MCPCall]; one it answers no for is armed exactly as it is today.
func (m *Manager) MCPService(service string) bool {
	_, err := m.server(service)
	return err == nil
}

// toolServer is one service that brings its own tools, as a plug.
type toolServer struct {
	service Service
	// address is where the service answers, which is the one fact the catalog
	// of these (mcp_catalog.go) carries beyond what goes on a menu. It is also
	// the identifier the sign-in is bound to, so that keys minted for one
	// service cannot be spent at another.
	address string
}

func (s *toolServer) Service() Service                         { return s.service }
func (s *toolServer) Endpoint() oauth2.Endpoint                { return oauth2.Endpoint{} }
func (s *toolServer) AuthCodeOptions() []oauth2.AuthCodeOption { return nil }

// Account answers with nothing.
//
// THE EMPTINESS LAW: the sign-in says which account it was, and the service says
// nothing further about whose it is. There is no address to show beside
// "Connected", and inventing one would be worse than showing none.
func (s *toolServer) Account(context.Context, *http.Client) (string, error) { return "", nil }

var (
	toolServerMu sync.RWMutex
	// toolServerIDs is every service that introduces itself rather than being
	// registered by hand. It is kept beside the registry rather than derived
	// from it because the one caller that has to ask ([Manager.offered]) holds
	// a [Service] and not a [Plug].
	toolServerIDs = map[string]bool{}
)

// registerToolServer puts one tool server on the registry and gives it the
// read/act pair to be judged by.
//
// The pair is registered with NO TOOL MAP, which is honest rather than lazy:
// the tools are not known until somebody connects. [MCPToolCapability] is how a
// caller gets from a fetched tool to one of the two sentences.
func registerToolServer(service Service, address string) {
	service.Auth = AuthBrowser
	if strings.TrimSpace(address) == "" {
		panic("connect: tool server " + service.ID + " has nowhere to answer")
	}
	Register(&toolServer{service: service, address: strings.TrimSpace(address)})
	RegisterGenericCapabilities(service.ID, nil)

	toolServerMu.Lock()
	defer toolServerMu.Unlock()
	toolServerIDs[normalize(service.ID)] = true
}

// registersItself reports whether a service introduces itself to its vendor at
// connect time instead of needing an application registered by hand first.
//
// It is the one question [Manager.offered] asks on behalf of this half of the
// package: a service that registers itself is offerable in every build, because
// there is nothing a person or a packager could have failed to configure.
func registersItself(service Service) bool {
	toolServerMu.RLock()
	defer toolServerMu.RUnlock()
	return toolServerIDs[normalize(service.ID)]
}

// server finds the tool server behind one id, or says plainly that this service
// is not one of them.
func (m *Manager) server(id string) (*toolServer, error) {
	plug, err := m.plug(id)
	if err != nil {
		return nil, err
	}
	found, ok := plug.(*toolServer)
	if !ok {
		return nil, fmt.Errorf("%s does not bring tools of its own", plug.Service().Name)
	}
	return found, nil
}
