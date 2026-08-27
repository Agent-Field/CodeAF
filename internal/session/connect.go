package session

// The accounts a person already has, reachable from a conversation.
//
// Three mechanisms live here and the belt (tools_connect.go) is the fourth,
// sitting on top of them:
//
//   - THE SEAM. [connectHub] is the narrow slice of internal/connect this
//     package touches: which services exist, which are connected, how to start
//     connecting one, and an authorized client for one that is. Everything below
//     it — where the credentials live, what a refresh is, which endpoint a
//     mailbox search hits — is that package's business and never this one's.
//   - THE ASK. Connecting an account is a thing done on somebody's behalf with
//     their credentials, so it is a QUESTION, and it is asked exactly the way
//     the approval gate asks one (consent.go): the tool call blocks in its own
//     goroutine, an event goes out, and an answer, a clock or the end of the turn
//     releases it. SILENCE IS A NO. A five-minute clock that approved would be a
//     clock that connected somebody's mail because they went to lunch.
//   - THE ARMING. The tools an account brings are not on the belt until the
//     account is connected, and they arrive by APPENDING to the belt rather than
//     by rebuilding it (see [Agent.armFamily]).
//
// ── WHY CONNECTING IS NEVER JOURNALED ──
//
// For consent's reason, and one more. The session file is the record of what was
// DONE in this conversation; a connected account is a fact about the MACHINE
// that outlives every conversation on it, and a resume that replayed the
// question would be asking again about something already answered elsewhere. So
// the events carry the question and the outcome, and the transcript keeps the
// two things that are true afterwards: the tool result the model read, and the
// tools it was holding from the next turn on.

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/connect"
	"github.com/Agent-Field/aforge-v2/internal/exec/bare"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// connectAskTimeout is how long a connect question waits for the person, and
// connectAuthCeiling is how long the attempt itself may then take.
//
// Five minutes each, and generous on purpose: unlike an approval prompt, which
// interrupts somebody watching a call they just asked for, this question can
// arrive while they are reading the answer to something else, and what is behind
// it — the mailbox search they wanted — is worth a blocked tool call. The
// ceiling is a CEILING rather than a deadline anybody is counting: an attempt
// nobody finished has to end somewhere, or the call blocks for the life of the
// session.
//
// They are vars only so the tests can run both clocks out in a millisecond.
// NOTHING IN A BUILD WRITES EITHER.
var (
	connectAskTimeout  = 5 * time.Minute
	connectAuthCeiling = 5 * time.Minute
)

// connectStatus is one service as this package sees it. It is a copy of
// [connect.Status] rather than the thing itself so that the seam below is
// implementable by a test without a Google account: everything this package does
// with a service is spell its name, say whether it is connected, and say who it
// is connected as.
type connectStatus struct {
	ID        string
	Name      string
	Blurb     string
	Connected bool
	Account   string
	// Auth is which of the two ways this account is connected, in
	// internal/connect's words: "browser" for a trip through a sign-in page,
	// "key" for a key the person already holds. It is the one fact the belt
	// needs about the difference, because the two are asked about
	// differently and bring different tools.
	Auth string
	// Address is where a key-connected service answers. It goes into the
	// description of the tool that calls it, so the model knows what its
	// paths are relative to.
	Address string
	// Blank is the label for the one thing a browser service has to ask before
	// it knows where to open. Empty is the ordinary browser trip.
	Blank string
}

// keyed reports whether this account is one the person connects with a key.
func (c connectStatus) keyed() bool { return c.Auth == connect.AuthKey }

// asks reports whether this browser-connected account needs one typed answer
// before its page can open.
func (c connectStatus) asks() bool { return c.Blank != "" }

// connectHub is the narrow slice of [connect.Manager] the belt calls through.
//
// BeginAuth takes the one thing the service's address is missing, empty for an
// ordinary browser trip, then hands back the page to open and the wait for the
// person to finish with it. Those come as two values rather than as an object,
// because they are the whole of what this package does with a connection in
// progress — and a function is the one shape a test can supply without owning
// the type.
// ConnectKey is the other way in and needs no such pair: the person hands over
// a key they already hold and the account is connected or it is not, in one
// call, with nothing in the middle to report on.
//
// Request is the raw call an account opened with a key answers. It is on the
// seam rather than done here out of a client because where a service lives, and
// how its key rides on a request, are internal/connect's facts and not this
// package's — the belt hands over a method and a path and reads back text.
// The last four are what an account may be USED FOR, which is the question the
// settings sheet asks with a word per row and this package asks twice per
// conversation — once when a family is armed, once when a call is judged
// (connectcaps.go). They are on this seam rather than reached for through a
// second handle for the reason everything else here is: one door onto the
// accounts, so a belt and a panel can never disagree about what is allowed.
//
// SetCapabilityState is on it because the mid-chat "always" answer is the same
// sentence the panel writes, and it must land in the same store. A seam with the
// three reads and not the write would have forced a second place to remember
// what somebody said.
type connectHub interface {
	Services() []connectStatus
	Connected(id string) bool
	BeginAuth(ctx context.Context, id, answer string) (url string, wait func(context.Context) (connectStatus, error), err error)
	ConnectKey(ctx context.Context, id string, key string) (connectStatus, error)
	Client(ctx context.Context, id string) (*http.Client, error)
	Request(ctx context.Context, id, method, path, query, body string) (string, error)

	// The two below are the accounts that BRING THEIR OWN TOOLS (served.go):
	// one asks a connected account what it serves, the other runs one of them.
	// They are on this seam rather than reached for through a handle of their
	// own for the reason everything else here is — one door onto the accounts —
	// and they carry a context because both reach the far end.
	MCPTools(ctx context.Context, service string) ([]connect.MCPTool, error)
	MCPCall(ctx context.Context, service, tool string, args json.RawMessage) (string, error)

	Capabilities(service string) []connect.Capability
	CapabilityState(service, capability string) connect.CapabilityState
	SetCapabilityState(service, capability string, state connect.CapabilityState) error
	ToolCapability(service, tool string) string
}

// managerHub is the adapter over the real thing. It is the only code in this
// package that names internal/connect's types.
type managerHub struct{ manager *connect.Manager }

// newConnectHub resolves the seam from the config. A nil manager stays nil —
// THE NIL LAW: a typed nil wrapped in an interface would be a non-nil hub that
// refuses everything, which is exactly the belt-that-lies this package refuses
// to build.
func newConnectHub(config Config) connectHub {
	if config.connectHub != nil {
		return config.connectHub
	}
	if config.Connect == nil {
		return nil
	}
	return managerHub{manager: config.Connect}
}

func (h managerHub) Services() []connectStatus {
	live := h.manager.Services()
	services := make([]connectStatus, 0, len(live))
	for _, status := range live {
		services = append(services, asConnectStatus(status))
	}
	return services
}

// asConnectStatus is the one place internal/connect's shape becomes this
// package's, so that a field added there arrives here in one edit.
func asConnectStatus(status connect.Status) connectStatus {
	return connectStatus{
		ID:        status.ID,
		Name:      status.Name,
		Blurb:     status.Blurb,
		Connected: status.Connected,
		Account:   status.Account,
		Auth:      status.Auth,
		Address:   status.Address,
		Blank:     status.Blank,
	}
}

func (h managerHub) Connected(id string) bool { return h.manager.Connected(id) }

func (h managerHub) Client(ctx context.Context, id string) (*http.Client, error) {
	return h.manager.Client(ctx, id)
}

func (h managerHub) ConnectKey(ctx context.Context, id string, key string) (connectStatus, error) {
	status, err := h.manager.ConnectKey(ctx, id, key)
	if err != nil {
		return connectStatus{}, err
	}
	return asConnectStatus(status), nil
}

func (h managerHub) Request(ctx context.Context, id, method, path, query, body string) (string, error) {
	return h.manager.Request(ctx, id, method, path, query, body)
}

// The four capability questions are passed straight through. THE READS ARE NOT
// CACHED HERE and must not be: the panel and the conversation share one process,
// and a cache in this adapter would be exactly the drift the one-store law
// exists to prevent (connectcaps.go).
func (h managerHub) Capabilities(service string) []connect.Capability {
	return h.manager.Capabilities(service)
}

func (h managerHub) CapabilityState(service, capability string) connect.CapabilityState {
	return h.manager.CapabilityState(service, capability)
}

func (h managerHub) SetCapabilityState(service, capability string, state connect.CapabilityState) error {
	return h.manager.SetCapabilityState(service, capability, state)
}

func (h managerHub) ToolCapability(service, tool string) string {
	return h.manager.ToolCapability(service, tool)
}

func (h managerHub) MCPTools(ctx context.Context, service string) ([]connect.MCPTool, error) {
	return h.manager.MCPTools(ctx, service)
}

func (h managerHub) MCPCall(ctx context.Context, service, tool string, args json.RawMessage) (string, error) {
	return h.manager.MCPCall(ctx, service, tool, args)
}

func (h managerHub) BeginAuth(ctx context.Context, id, answer string) (string, func(context.Context) (connectStatus, error), error) {
	flow, err := h.manager.BeginAuth(ctx, id, answer)
	if err != nil {
		return "", nil, err
	}
	wait := func(ctx context.Context) (connectStatus, error) {
		status, err := flow.Wait(ctx)
		if err != nil {
			return connectStatus{}, err
		}
		return asConnectStatus(status), nil
	}
	return flow.URL(), wait, nil
}

// service finds one service by id, and reports whether this build has it at all.
func (a *Agent) service(id string) (connectStatus, bool) {
	if a.connect == nil {
		return connectStatus{}, false
	}
	for _, status := range a.connect.Services() {
		if strings.EqualFold(status.ID, id) {
			return status, true
		}
	}
	return connectStatus{}, false
}

// ── the ask ─────────────────────────────────────────────────────────────────

// connectAsk is one unanswered question: the channel the answer arrives on, and
// whether it needs a typed answer — a key, or the one thing the service's
// address is missing.
type connectAsk struct {
	answers  chan connectAnswer
	needsKey bool
}

// connectAnswer is what a person said. Approved with no key is a yes to an
// ordinary browser trip; approved with a typed answer is a key, or the one
// thing the service's address is missing; not approved is a no, however it was
// said.
type connectAnswer struct {
	approved bool
	key      string
}

// ResolveConnect answers one EventConnectAsk. A surface hands back the id the
// event carried and what the person said.
//
// A YES TO A QUESTION THAT WANTED A TYPED ANSWER IS NOT AN ANSWER. The account
// needs a key, or the one thing its address is missing, so there is nothing a
// bare yes could start; it is read as a decline rather than as a connection
// that then fails for a reason nobody said out loud. A surface that means yes
// to one of those sends the answer through [Agent.ResolveConnectKey].
//
// An id nobody is waiting on — a question the clock already answered, a second
// click, a turn that was interrupted — is IGNORED rather than reported, exactly
// as [Agent.ResolveConsent] ignores a late answer: the answer is simply late,
// and the surface has already seen the attempt end.
func (a *Agent) ResolveConnect(id string, approve bool) {
	ask, waiting := a.claimConnect(id)
	if !waiting {
		return
	}
	if ask.needsKey {
		approve = false
	}
	// Buffered to one and read at most once, so this never blocks and never
	// needs the lock held across it.
	ask.answers <- connectAnswer{approved: approve}
}

// ResolveConnectKey answers one EventConnectAsk that carried NeedsKey with the
// key, or the one thing the service's address is missing, that the person gave.
//
// AN EMPTY ANSWER IS A DECLINE. A surface whose question was dismissed, or
// whose field was left blank, has one thing to send and no separate word for
// "not now" — and a blank answer would be refused by the service anyway, an
// ugly sentence later for a plain no now.
//
// A typed answer sent for a question that wanted an ordinary browser trip is
// ignored: there is nothing to do with it, and connecting on the strength of it
// would connect an account by a route nobody asked about.
func (a *Agent) ResolveConnectKey(id string, key string) {
	ask, waiting := a.claimConnect(id)
	if !waiting {
		return
	}
	if !ask.needsKey {
		ask.answers <- connectAnswer{}
		return
	}
	key = strings.TrimSpace(key)
	ask.answers <- connectAnswer{approved: key != "", key: key}
}

// claimConnect takes one waiting question off the map, so that two answers to
// the same question can never both be delivered.
func (a *Agent) claimConnect(id string) (connectAsk, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	ask, waiting := a.connectAsks[id]
	if waiting {
		delete(a.connectAsks, id)
	}
	return ask, waiting
}

// askConnect emits one question and waits for the person, the clock, or the end
// of the turn. Not approved is a no in all three cases, and the error is set
// only when there is nobody to ask at all. The key is empty except when the
// question wanted a typed answer and the person gave it.
func (a *Agent) askConnect(ctx context.Context, service connectStatus) (connectAnswer, error) {
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return connectAnswer{}, errAgentClosed
	}
	a.connectSeq++
	id := "connect-" + strconv.FormatUint(a.connectSeq, 10)
	ask := connectAsk{answers: make(chan connectAnswer, 1), needsKey: service.keyed() || service.asks()}
	if a.connectAsks == nil {
		a.connectAsks = make(map[string]connectAsk, 1)
	}
	a.connectAsks[id] = ask
	// The turn's hub, read under the same lock that registers the wait: a tool
	// runs inside a turn, and the turn's fan-out is where its question is seen.
	hub := a.hub
	watched := a.config.AskConsent && hub != nil
	a.mu.Unlock()

	if !watched {
		a.forgetConnect(id)
		return connectAnswer{}, errNobodyWatching
	}

	hub.send(Event{
		Kind:        EventConnectAsk,
		ConnectID:   id,
		Service:     service.ID,
		ServiceName: service.Name,
		NeedsKey:    ask.needsKey,
	})

	timer := time.NewTimer(connectAskTimeout)
	defer timer.Stop()

	select {
	case answer := <-ask.answers:
		return answer, nil
	case <-timer.C:
		a.forgetConnect(id)
		// SILENCE IS A NO. Nothing is connected, and the model is told the
		// person did not answer rather than told they refused — those are
		// different sentences and only one of them is true.
		return connectAnswer{}, nil
	case <-ctx.Done():
		a.forgetConnect(id)
		return connectAnswer{}, ctx.Err()
	}
}

// forgetConnect drops a question nobody will answer, so a late resolve does not
// deliver into a channel with no reader and the map does not grow for the life
// of the session.
func (a *Agent) forgetConnect(id string) {
	a.mu.Lock()
	delete(a.connectAsks, id)
	a.mu.Unlock()
}

// PendingConnect lists the connect questions still waiting for an answer, oldest
// first. It is [Agent.PendingConsent] for the other question: a surface
// redrawing itself mid-turn — a resize, a reattach — needs to know a question is
// outstanding without having kept the event.
func (a *Agent) PendingConnect() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	ids := make([]string, 0, len(a.connectAsks))
	for id := range a.connectAsks {
		ids = append(ids, id)
	}
	for i := 1; i < len(ids); i++ {
		for j := i; j > 0 && connectAskOlder(ids[j], ids[j-1]); j-- {
			ids[j], ids[j-1] = ids[j-1], ids[j]
		}
	}
	return ids
}

// connectAskOlder orders two ask ids by the counter inside them. The ids are
// strings because that is what the surface hands back, and a string sort would
// put "connect-10" before "connect-2".
func connectAskOlder(left, right string) bool {
	return connectAskSeq(left) < connectAskSeq(right)
}

func connectAskSeq(id string) uint64 {
	seq, err := strconv.ParseUint(strings.TrimPrefix(id, "connect-"), 10, 64)
	if err != nil {
		return 0
	}
	return seq
}

// ── the arming ──────────────────────────────────────────────────────────────

// beltTools and beltDefinitions are how the turn reads the belt. They take
// armMu, copy the slice HEADER, and let go: arming never writes into an array
// anybody is holding, so a walk over what these return needs no lock and can
// never see a tool half-appended.
func (a *Agent) beltTools() []bare.Tool {
	a.armMu.Lock()
	defer a.armMu.Unlock()
	return a.tools
}

func (a *Agent) beltDefinitions() []ai.ToolDefinition {
	a.armMu.Lock()
	defer a.armMu.Unlock()
	return a.definitions
}

// armFamily puts one service's tools on the belt, and reports what it added.
//
// ── THE APPEND LAW ──
//
// ARMED TOOLS GO AT THE TAIL AND NOTHING ELSE MOVES. The definition block rides
// at the front of every request, ahead of the whole transcript, so a definition
// that shifts position invalidates the prompt cache for everything behind it —
// which is the entire conversation (internal/exec's tools.go, which paid for
// this lesson twice). Appending costs exactly one invalidation, at the back,
// once per family, and that is the price this design accepted in advance.
//
// It is also why nothing here retires: the two connect tools stay on the belt
// for the life of the session, and a model that asks for a family it already
// holds is answered in one cheap line at the END of the transcript, which is
// free. Removing them would rewrite the block instead.
//
// An empty answer means everything asked for was already there.
func (a *Agent) armFamily(tools []bare.Tool) ([]string, error) {
	a.armMu.Lock()
	defer a.armMu.Unlock()

	held := make(map[string]bool, len(a.tools))
	for _, tool := range a.tools {
		held[tool.Name] = true
	}
	var arriving []bare.Tool
	var names []string
	for _, tool := range tools {
		if held[tool.Name] {
			continue
		}
		arriving = append(arriving, tool)
		names = append(names, tool.Name)
	}
	if len(arriving) == 0 {
		return nil, nil
	}
	definitions, err := toolDefinitions(arriving)
	if err != nil {
		return nil, err
	}
	// Fresh arrays, exact length: the old ones are still being walked by
	// whatever took a snapshot a moment ago, and appending into spare capacity
	// would write into the array they are reading.
	grownTools := make([]bare.Tool, 0, len(a.tools)+len(arriving))
	grownTools = append(append(grownTools, a.tools...), arriving...)
	grownDefinitions := make([]ai.ToolDefinition, 0, len(a.definitions)+len(definitions))
	grownDefinitions = append(append(grownDefinitions, a.definitions...), definitions...)
	a.tools = grownTools
	a.definitions = grownDefinitions
	return names, nil
}

// NoteConnected is the other door: an account connected from the SURFACE, with
// no tool call waiting on it — /connect while the conversation sits idle.
//
// It does the two things the tool path does after a successful attempt, and
// neither of them is a turn: the family goes on the belt, and one line goes onto
// the AMBIENT queue (agent.go's [Agent.enqueueAmbientNote]), so the model reads
// it whenever the person next says something. A connected account is not news
// anybody is standing there waiting for an answer about — the person is looking
// at the surface that just told them — so it must not start a paid turn.
//
// A service this build does not know, or a session with the feature absent, does
// nothing at all rather than queueing a line about a thing that cannot be used.
func (a *Agent) NoteConnected(service, account string) {
	status, known := a.service(service)
	if !known {
		return
	}
	// AN ACCOUNT THAT BRINGS ITS OWN TOOLS HAS TO BE ASKED WHAT IT BRINGS, and
	// asking reaches the far end. This door is called from a surface redrawing
	// itself, so the ask goes on a goroutine of its own and the note follows it
	// when it lands — which costs nothing, because an ambient note is read
	// whenever the person next says something and nobody is waiting on it (see
	// [Agent.enqueueAmbientNote]). The tool path does the same work inline,
	// where there is a turn to hold it.
	if a.servesItsOwn(status) {
		go a.noteServed(status, account)
		return
	}
	// The capabilities the person has turned off take their tools with them
	// here too (connectcaps.go): this door and the tool call's door must put the
	// same belt on, or a person would get a different set of hands depending on
	// which of the two connected the account.
	tools := a.liveTools(status.ID, a.familyTools(status))
	if _, err := a.armFamily(tools); err != nil {
		return
	}
	a.enqueueAmbientNote(connectedNote(status.Name, account, len(tools) > 0))
}

// noteServed is [Agent.NoteConnected] for an account whose tools have to be
// asked for. The whole arming reply becomes the note, rather than
// [connectedNote]'s shorter line, because it is the one that NAMES what arrived
// — and with a served account that is the only place those names exist.
func (a *Agent) noteServed(status connectStatus, account string) {
	ctx, cancel := context.WithTimeout(context.Background(), mcpFetchCeiling)
	defer cancel()
	a.enqueueAmbientNote(a.armServed(ctx, status, connectedLine(status.Name, account), ""))
}

// connectedLine is the first half of every sentence about an account that has
// just been picked up: what it is, and who it is held as.
func connectedLine(name, account string) string {
	line := name + " is connected"
	if account = strings.TrimSpace(account); account != "" {
		line += " as " + account
	}
	return line
}

// connectedNote is the line the model reads. It says the two things that are
// now true — the account is connected, and the tools are in hand — and it says
// them the way a person would.
//
// armed is false where this build has no tools for the account, or where the
// person has turned off everything it can do. The note then stops at the
// connection, because the second half of the sentence would be a promise of
// hands the next turn will not have.
func connectedNote(name, account string, armed bool) string {
	line := name + " is connected"
	if account = strings.TrimSpace(account); account != "" {
		line += " as " + account
	}
	if !armed {
		return line + ", and there is nothing it can be used for in this conversation."
	}
	return line + ". Its tools are in your tool list from this turn on."
}
