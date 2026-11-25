package serve

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/acl"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// ── A PERSON'S OWN CHATS, KEPT AND RUN BY THE ORGANIZATION'S SERVER ────────
//
// Beside the one Afi conversation every workstream gives each person, a
// person may keep chats of their own with the workstream's agent: in their
// own space (myspace.go) that is how they work, and in a shared workstream it
// is a side conversation. Each chat is a seat of its own (workstream_agent.go
// agentSeat.chat), so it has the same rules Afi's pane has:
//
//   - WHAT IS SAID IS PRIVATE TO ITS PERSON. Nobody else reads it or hears its
//     events; what the agent puts in files and pages is the workstream's. In
//     a shared workstream the chat's title, owner, state and last activity
//     are seen only by its owner and those it is shared with (the chat is
//     private unless the policy says otherwise); in a space, nobody else's.
//   - IT RUNS HERE, never on a laptop, whatever the operator chose for Afi's
//     pane, so closing the laptop mid-answer loses nothing.
//   - IT SPENDS THE ORGANIZATION'S KEY, posted to the workstream under its
//     person, and stops at the workstream's budget or, in a space, at the
//     person's monthly limit.
//
// A chat lives in its own folder under its person's: meta.json (its title and
// when it was made) beside the conversation's lines.

// chatIDPattern is a chat's id as this server makes it.
var chatIDPattern = regexp.MustCompile(`^ch_[0-9A-Za-z]{26}$`)

// chatMeta is a chat's meta.json.
type chatMeta struct {
	Title string `json:"title"`
	// Agent is the named agent the chat talks to (org_agents.go), "" for Afi.
	Agent     string `json:"agent,omitempty"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

func (h *Handler) chatRoutes() {
	h.route("/v1/workstreams/{id}/chats", map[string]http.HandlerFunc{http.MethodGet: h.listAgentChats, http.MethodPost: h.createAgentChat})
}

// chatsDir is where person's chats in ws live.
func (h *Handler) chatsDir(ws, person string) string {
	return filepath.Join(h.config.Store.Root(), agentDirName, ws, "people", person, "chats")
}

// chatDir is one chat's folder.
func (h *Handler) chatDir(ws, person, chat string) string {
	return filepath.Join(h.chatsDir(ws, person), chat)
}

func readChatMeta(dir string) (chatMeta, bool) {
	raw, err := os.ReadFile(filepath.Join(dir, "meta.json"))
	if err != nil {
		return chatMeta{}, false
	}
	var m chatMeta
	return m, json.Unmarshal(raw, &m) == nil
}

func writeChatMeta(dir string, m chatMeta) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return writeFileAtomic(filepath.Join(dir, "meta.json"), raw)
}

// writeFileAtomic replaces path with data through a temporary file beside it.
func writeFileAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), path)
}

var errChatNotHere = errors.New("that chat is not one of yours here")

// chatExists confirms a named chat's metadata while the caller holds the
// desk lock if it is about to use the folder.
func (h *Handler) chatExists(seat agentSeat) bool {
	if seat.chat == "" {
		return true
	}
	_, ok := readChatMeta(h.agentDir(seat))
	return ok
}

// chatOf checks the chat a request names: "" is Afi's conversation; anything
// else must be one of person's own chats in ws. A chat that is not theirs is
// answered as gone, so an id is never a way to learn about someone else's.
// NOT GENERIC YET: Sharing a transcript does not share the chat controls.
func (h *Handler) chatOf(w http.ResponseWriter, ws factory.OrgWorkstream, person, raw string) (string, bool) {
	chat := strings.TrimSpace(raw)
	if chat == "" {
		return "", true
	}
	if person == "" || !chatIDPattern.MatchString(chat) {
		h.fail(w, http.StatusNotFound, errChatNotHere.Error())
		return "", false
	}
	if !h.chatExists(agentSeat{ws.ID, person, chat}) {
		h.fail(w, http.StatusNotFound, errChatNotHere.Error())
		return "", false
	}
	return chat, true
}

// chatTitleFrom is a title made from a chat's first message: its first line,
// cut at a word near the most a title holds.
func chatTitleFrom(text string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(text), "\n")
	line = strings.Join(strings.Fields(line), " ")
	const most = 60
	if utf8.RuneCountInString(line) <= most {
		return line
	}
	r := []rune(line)[:most]
	cut := string(r)
	if i := strings.LastIndexByte(cut, ' '); i > most/2 {
		cut = cut[:i]
	}
	return strings.TrimRight(cut, " ,.;:") + "…"
}

// chatTouched notes a new message in s's chat: its time, and its title from
// the first message when it was made with none.
func (h *Handler) chatTouched(s agentSeat, text string) {
	dir := h.chatDir(s.ws, s.person, s.chat)
	h.agentDesk.mu.Lock()
	defer h.agentDesk.mu.Unlock()
	m, ok := readChatMeta(dir)
	if !ok {
		return
	}
	if m.Title == "" {
		m.Title = chatTitleFrom(text)
	}
	m.UpdatedAt = h.clock().UTC().Format(time.RFC3339Nano)
	_ = writeChatMeta(dir, m)
}

// myChats are person's chats in ws, newest change first.
func (h *Handler) myChats(ws, person string) []api.AgentChat {
	out := []api.AgentChat{}
	if person == "" {
		return out
	}
	entries, err := os.ReadDir(h.chatsDir(ws, person))
	if err != nil {
		return out
	}
	var agents factory.OrgAgents
	for _, e := range entries {
		if !e.IsDir() || !chatIDPattern.MatchString(e.Name()) {
			continue
		}
		m, ok := readChatMeta(filepath.Join(h.chatsDir(ws, person), e.Name()))
		if !ok {
			continue
		}
		h.agentDesk.mu.Lock()
		state := h.agentOfLocked(agentSeat{ws, person, e.Name()}).state.State
		h.agentDesk.mu.Unlock()
		row := api.AgentChat{ID: e.Name(), Title: m.Title, State: state, CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt}
		if m.Agent != "" {
			if agents == nil {
				agents, _ = h.config.Store.OrgAgents()
			}
			// A deleted agent still names its chat, so the chat stays readable.
			name := "An agent that was deleted"
			if a, ok := agents[m.Agent]; ok {
				name = a.Name
			}
			row.Agent = &api.AgentRef{ID: m.Agent, Name: name}
		}
		out = append(out, row)
	}
	slices.SortFunc(out, func(a, b api.AgentChat) int {
		if c := strings.Compare(b.UpdatedAt, a.UpdatedAt); c != 0 {
			return c
		}
		return strings.Compare(b.ID, a.ID)
	})
	return out
}

// listAgentChats is GET /v1/workstreams/{id}/chats: the caller's own chats,
// and the chats of others that the caller is allowed to see, as metadata only.
func (h *Handler) listAgentChats(w http.ResponseWriter, r *http.Request) {
	ws, v, ok := h.findWorkstream(w, r)
	if !ok {
		return
	}
	me := h.orgActor(r)
	h.write(w, http.StatusOK, api.AgentChatList{Chats: h.myChats(ws.ID, me), Others: h.othersChats(ws, me, v)})
}

// othersChats are the chats in ws of everyone but me that v may see, newest
// activity first: title, owner, state and last activity only. A space's are
// nobody else's.
//
// A CHAT IS LISTED TO SOMEONE ELSE ONLY IF THE ONE DECIDER SAYS THEY MAY VIEW
// IT. Chats are private by default (the policy's default for the kind), and a
// private chat is its owner's and its grantees' alone, so its title, owner,
// state and last activity are hidden from the workstream's people and from the
// operator, because a title alone can say what the work is. A chat with no
// registry row is unknown, and unknown is hidden. An organization that wants
// every member to see each other's chat titles sets the policy's default for
// chats to owner or org; that applies to chats made after it.
func (h *Handler) othersChats(ws factory.OrgWorkstream, me string, v Viewer) []api.OtherChat {
	out := []api.OtherChat{}
	if ws.Space {
		return out
	}
	visible := h.chatsMaySee(ws, v)
	if len(visible) == 0 {
		return out
	}
	people, err := os.ReadDir(filepath.Join(h.config.Store.Root(), agentDirName, ws.ID, "people"))
	if err != nil {
		return out
	}
	nameOf := h.personNames()
	for _, p := range people {
		if !p.IsDir() || p.Name() == me {
			continue
		}
		for _, c := range h.myChats(ws.ID, p.Name()) {
			if !visible[c.ID] {
				continue
			}
			out = append(out, api.OtherChat{ID: c.ID, Workstream: ws.ID, Title: c.Title, Owner: p.Name(), OwnerName: nameOf(p.Name()), State: c.State, LastActivity: c.UpdatedAt})
		}
	}
	slices.SortFunc(out, func(a, b api.OtherChat) int {
		if c := strings.Compare(b.LastActivity, a.LastActivity); c != 0 {
			return c
		}
		return strings.Compare(b.ID, a.ID)
	})
	return out
}

// chatsMaySee is the ids of the chats registered under ws that v may view,
// asked of the one decider in a few queries rather than one per chat.
func (h *Handler) chatsMaySee(ws factory.OrgWorkstream, v Viewer) map[string]bool {
	seen := map[string]bool{}
	rows, err := h.config.Store.ResourcesIn("chat", workstreamResource(ws).ref)
	if err != nil {
		slog.Error("could not read the workstream's chats", "error", err)
		return seen
	}
	resources := make([]accessResource, 0, len(rows))
	for _, row := range rows {
		resources = append(resources, h.registeredResource(row, ws))
	}
	v.acc.prefetchResources(resources)
	for _, res := range resources {
		if can(v, res.item, acl.VerbView) {
			seen[res.ref.ID] = true
		}
	}
	return seen
}

// createAgentChat is POST /v1/workstreams/{id}/chats: a new chat of the
// caller's own, empty until its first message. Whoever can talk to the
// workstream's agent may keep chats with it.
func (h *Handler) createAgentChat(w http.ResponseWriter, r *http.Request) {
	ws, v, ok := h.findWorkstream(w, r)
	if !ok || !h.mayCreateResource(w, v) {
		return
	}
	if !v.canWrite(ws) {
		h.fail(w, http.StatusForbidden, "only its owner and those it is shared with for edit can chat in this workstream")
		return
	}
	if h.config.WorkstreamAgent == nil {
		h.fail(w, http.StatusNotFound, "this organization's server does not run agents")
		return
	}
	person := h.orgActor(r)
	if person == "" {
		h.fail(w, http.StatusForbidden, "a chat is someone's; sign in first")
		return
	}
	if ws.ArchivedAt != nil {
		h.fail(w, http.StatusConflict, workstreamArchivedWords)
		return
	}
	var body api.AgentChatNew
	if !h.decode(w, r, &body, true) {
		return
	}
	title := strings.Join(strings.Fields(body.Title), " ")
	if len(title) > api.AgentChatTitleMost {
		h.fail(w, http.StatusUnprocessableEntity, "a chat's title is at most 120 bytes")
		return
	}
	if len(h.myChats(ws.ID, person)) >= api.AgentChatsMost {
		h.fail(w, http.StatusConflict, "you keep 500 chats here already; delete one first")
		return
	}
	var ref *api.AgentRef
	if id := strings.TrimSpace(body.Agent); id != "" {
		a, ok := h.chatAgent(ws, person, id)
		if !ok {
			h.fail(w, http.StatusNotFound, "that agent is not one you can chat with here; add it under People and rules › Agents first")
			return
		}
		ref = &api.AgentRef{ID: a.ID, Name: a.Name}
	}
	row, err := h.keepNewChat(v, ws, person, title, strings.TrimSpace(body.Agent))
	if h.accessFailed(w, err) {
		return
	}
	if row.Visibility != acl.Private {
		res := h.registeredResource(row, ws)
		h.publishAccessEvent(res.ref, v.Handle, h.audienceOf(res.item))
	}
	at := row.CreatedAt.Format(time.RFC3339Nano)
	h.write(w, http.StatusCreated, api.AgentChat{ID: row.ID, Title: title, State: api.AgentIdle, CreatedAt: at, UpdatedAt: at, Agent: ref})
}

func (h *Handler) keepNewChat(v Viewer, ws factory.OrgWorkstream, person, title, agent string) (store.Resource, error) {
	now := h.clock().UTC()
	id, err := cell.NewID(now)
	if err != nil {
		return store.Resource{}, err
	}
	chat := "ch_" + id
	policy := v.acc.rules()
	row := store.Resource{Resource: Ref{Kind: "chat", ID: chat}, Parent: workstreamResource(ws).ref,
		Owner: acl.Subject{Kind: "person", ID: person}, CreatedBy: accessPrincipal(v), Label: title,
		Visibility: policy.Visibility("chat"), CreatedAt: now}
	row.CreatedBy.ID = firstNonEmpty(row.CreatedBy.ID, person)
	var grant *acl.Grant
	if row.Visibility == acl.Org {
		grant = orgDefaultGrant(policy, row.Resource, now)
	}
	at := now.Format(time.RFC3339Nano)
	dir := h.chatDir(ws.ID, person, chat)
	wrote := false
	err = h.config.Store.RegisterResource(row, grant, policy.Rev, func() error {
		wrote = true
		return writeChatMeta(dir, chatMeta{Title: title, Agent: agent, CreatedAt: at, UpdatedAt: at})
	})
	if err != nil && wrote {
		_ = os.RemoveAll(dir)
	}
	return row, err
}

// backfillChats registers older chats privately and leaves existing registrations intact.
func (h *Handler) backfillChats() {
	if h.config.Store == nil {
		return
	}
	if err := h.backfillChatResources(); err != nil {
		slog.Error("could not register older chats", "error", err)
	}
}

func (h *Handler) backfillChatResources() error {
	workstreams, err := h.config.Store.Workstreams()
	if err != nil {
		return err
	}
	rows, err := h.config.Store.Resources("chat")
	if err != nil {
		return err
	}
	kept := make(map[string]bool, len(rows))
	for _, row := range rows {
		kept[row.ID] = true
	}
	for _, ws := range workstreams {
		if !strings.HasPrefix(ws.ID, workstreamIDPrefix) || filepath.Base(ws.ID) != ws.ID {
			continue
		}
		people, err := os.ReadDir(filepath.Join(h.config.Store.Root(), agentDirName, ws.ID, "people"))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		for _, person := range people {
			handle := factory.NormalizeHandle(person.Name())
			if !person.IsDir() || !personHandle.MatchString(handle) {
				continue
			}
			if err := h.backfillPersonChats(ws, person.Name(), handle, kept); err != nil {
				return err
			}
		}
	}
	return nil
}

func (h *Handler) backfillPersonChats(ws factory.OrgWorkstream, folder, handle string, kept map[string]bool) error {
	dir := h.chatsDir(ws.ID, folder)
	chats, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, chat := range chats {
		id := chat.Name()
		if !chat.IsDir() || !chatIDPattern.MatchString(id) || kept[id] {
			continue
		}
		meta, ok := readChatMeta(filepath.Join(dir, id))
		if !ok {
			continue
		}
		at, _ := time.Parse(time.RFC3339Nano, meta.CreatedAt)
		if err := h.config.Store.BackfillResource(store.Resource{Resource: Ref{Kind: "chat", ID: id},
			Parent: workstreamResource(ws).ref, Owner: acl.Subject{Kind: "person", ID: handle},
			CreatedBy: acl.Principal{Kind: "person", ID: handle}, Label: meta.Title, CreatedAt: at, Visibility: acl.Private}); err != nil {
			return err
		}
		kept[id] = true
	}
	return nil
}

// chatAgentGone refuses a message to a chat whose agent is deleted, or no
// longer one this workstream lists.
type chatAgentGone struct{ name string }

func (e chatAgentGone) Error() string {
	if e.name == "" {
		return "the agent of this chat was deleted; start a new chat"
	}
	return e.name + " is no longer one this workstream lists, so this chat takes no more messages; start a new chat"
}

// chatPersona is the named agent seat s's chat talks to, nil for Afi's
// conversation or a chat with Afi. It is checked again on every message, so
// an agent unshared or deleted stops being used at once.
func (h *Handler) chatPersona(ws factory.OrgWorkstream, s agentSeat) (*AgentPersona, error) {
	if s.chat == "" {
		return nil, nil
	}
	m, ok := readChatMeta(h.chatDir(s.ws, s.person, s.chat))
	if !ok || m.Agent == "" {
		return nil, nil
	}
	a, usable := h.chatAgent(ws, s.person, m.Agent)
	if !usable {
		return nil, chatAgentGone{name: a.Name}
	}
	return &AgentPersona{ID: a.ID, Name: a.Name, Instructions: a.Instructions}, nil
}
