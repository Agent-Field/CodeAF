package serve

import (
	"encoding/json"
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
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// ── A PERSON'S OWN CHATS, KEPT AND RUN BY THE ORGANIZATION'S SERVER ────────
//
// Beside the one Afi conversation every workstream gives each person, a
// person may keep chats of their own with the workstream's agent: in their
// own space (myspace.go) that is how they work, and in a shared workstream it
// is a side conversation. Each chat is a seat of its own (workstream_agent.go
// agentSeat.chat), so it has the same rules Afi's pane has:
//
//   - IT IS PRIVATE TO ITS PERSON. Nobody else lists it, reads it or hears its
//     events; what the agent puts in files and pages is the workstream's.
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
	Title     string `json:"title"`
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

// chatOf checks the chat a request names: "" is Afi's conversation; anything
// else must be one of person's own chats in ws. A chat that is not theirs is
// answered as gone, so an id is never a way to learn about someone else's.
func (h *Handler) chatOf(w http.ResponseWriter, ws factory.OrgWorkstream, person, raw string) (string, bool) {
	chat := strings.TrimSpace(raw)
	if chat == "" {
		return "", true
	}
	if person == "" || !chatIDPattern.MatchString(chat) {
		h.fail(w, http.StatusNotFound, "that chat is not one of yours here")
		return "", false
	}
	if _, ok := readChatMeta(h.chatDir(ws.ID, person, chat)); !ok {
		h.fail(w, http.StatusNotFound, "that chat is not one of yours here")
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
		out = append(out, api.AgentChat{ID: e.Name(), Title: m.Title, State: state, CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt})
	}
	slices.SortFunc(out, func(a, b api.AgentChat) int {
		if c := strings.Compare(b.UpdatedAt, a.UpdatedAt); c != 0 {
			return c
		}
		return strings.Compare(b.ID, a.ID)
	})
	return out
}

// listAgentChats is GET /v1/workstreams/{id}/chats: the caller's own chats.
func (h *Handler) listAgentChats(w http.ResponseWriter, r *http.Request) {
	ws, _, ok := h.findWorkstream(w, r)
	if !ok {
		return
	}
	h.write(w, http.StatusOK, api.AgentChatList{Chats: h.myChats(ws.ID, h.orgActor(r))})
}

// createAgentChat is POST /v1/workstreams/{id}/chats: a new chat of the
// caller's own, empty until its first message. Whoever can talk to the
// workstream's agent may keep chats with it.
func (h *Handler) createAgentChat(w http.ResponseWriter, r *http.Request) {
	ws, v, ok := h.findWorkstream(w, r)
	if !ok {
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
		h.fail(w, http.StatusConflict, "This workstream is archived.")
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
	now := h.clock().UTC()
	id, err := cell.NewID(now)
	if h.failed(w, err) {
		return
	}
	chat := "ch_" + id
	at := now.Format(time.RFC3339Nano)
	if h.failed(w, writeChatMeta(h.chatDir(ws.ID, person, chat), chatMeta{Title: title, CreatedAt: at, UpdatedAt: at})) {
		return
	}
	h.write(w, http.StatusCreated, api.AgentChat{ID: chat, Title: title, State: api.AgentIdle, CreatedAt: at, UpdatedAt: at})
}
