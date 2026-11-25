package serve

import (
	"errors"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/acl"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

const chatKind = "chat"

const chatMoveBusy = "stop it or wait for it to finish, then move it"

// The sentences that refuse a move for want of the right to make it. The first is what every organization has always
// read, and it is still what one that has not changed the rule reads; the second is for an organization whose policy
// lets people with manage move a chat (acl.MoveByManage), where "only the owner" would be untrue.
const (
	chatMoveOwnerOnlyWords = "Only the person who owns this chat can move it, and they need permission to manage it."
	chatMoveManageWords    = "Only the person who owns this chat, or someone with permission to manage it, can move it."
	chatMoveOwnSpaceWords  = "Only the person who owns this chat can move it into their My space."
)

type chatMoveRefusal struct {
	status int
	words  string
}

func (e chatMoveRefusal) Error() string { return e.words }

// NOT GENERIC YET: A chat's conversation and ownership stay with its person.
func (h *Handler) moveAccessChat(w http.ResponseWriter, r *http.Request, id, to string) {
	var out api.ResourceMoved
	var audience itemAudience
	var by string
	err := h.config.Store.WithResourceMove(func(move store.ResourceMove) error {
		// Re-read permissions under the writer lock because a queued move
		// may have waited behind a share change or another move.
		v, err := h.itemViewer(r)
		if err != nil {
			return err
		}
		by = v.Handle
		out, audience, err = h.moveChat(move, v, id, to)
		return err
	})
	var refusal chatMoveRefusal
	switch {
	case errors.As(err, &refusal):
		h.fail(w, refusal.status, refusal.words)
	case err != nil:
		slog.Error("could not move the chat", "chat", id, "error", err)
		h.fail(w, http.StatusInternalServerError, "The chat could not be moved.")
	default:
		h.publishAccessEvent(out.Resource.ResourceRef, by, audience)
		h.recordOrg(r, record.ResourceAccessChanged, "moved", orgObjectResource+chatKind+"/"+id, "")
		h.write(w, http.StatusOK, out)
	}
}

func (h *Handler) moveChat(move store.ResourceMove, v Viewer, id, to string) (out api.ResourceMoved, audience itemAudience, err error) {
	row, found, err := h.config.Store.Resource(chatKind, id)
	if err != nil {
		return out, audience, err
	}
	if !found || !chatLocationValid(row) {
		return out, audience, chatMoveRefusal{http.StatusNotFound, "resource not found"}
	}
	src, found, err := h.config.Store.Workstream(row.Parent.ID)
	if err != nil {
		return out, audience, err
	}
	item := h.resourceItem(row, src)
	if !found || !can(v, item, acl.VerbView) {
		return out, audience, chatMoveRefusal{http.StatusNotFound, "resource not found"}
	}
	if v.Agent != nil || !mayMoveChat(v, item) {
		words := chatMoveOwnerOnlyWords
		if v.acc.rules().Chats.MoveByFor() == acl.MoveByManage {
			words = chatMoveManageWords
		}
		return out, audience, chatMoveRefusal{http.StatusForbidden, words}
	}
	if row.Parent.ID == to {
		return out, audience, chatMoveRefusal{http.StatusConflict, "it is already there"}
	}
	dest, err := h.chatMoveDestination(v, to)
	if err != nil {
		return out, audience, err
	}
	// A My space is its person's own. Where the policy lets someone with manage move another person's chat, the only
	// My space they can name is their own, and a chat that is not theirs does not go into it: it would leave its owner's
	// workstream for a place its owner does not keep, with every grant on it dropped.
	if dest.Space && !v.owns(item) {
		return out, audience, chatMoveRefusal{http.StatusForbidden, chatMoveOwnSpaceWords}
	}

	h.agentDesk.mu.Lock()
	defer h.agentDesk.mu.Unlock()
	seat := agentSeat{row.Parent.ID, row.Owner.ID, id}
	if a := h.agentDesk.agents[seat]; a != nil && (a.state.State != api.AgentIdle || !a.runEnded()) {
		return out, audience, chatMoveRefusal{http.StatusConflict, chatMoveBusy}
	}
	if !h.chatExists(seat) {
		return out, audience, chatMoveRefusal{http.StatusNotFound, "resource not found"}
	}
	destSeat := agentSeat{dest.ID, row.Owner.ID, id}
	fromDir, toDir := h.agentDir(seat), h.agentDir(destSeat)
	if _, err := os.Lstat(toDir); err == nil {
		return out, audience, chatMoveRefusal{http.StatusConflict, "A chat folder already exists at the destination."}
	}

	before := v.acc.audienceOf(item)
	next := row
	next.Parent = workstreamResource(dest).ref
	if dest.Space {
		next.Visibility = acl.Private
	}
	previous, err := move.MoveResource(next, dest.Space)
	if err != nil {
		return out, audience, err
	}
	if err := renameChatFolder(fromDir, toDir); err != nil {
		return out, audience, errors.Join(err, move.RestoreResource(previous))
	}
	delete(h.agentDesk.agents, seat)
	delete(h.agentDesk.agents, destSeat)
	out.Dropped = []api.GrantTuple{}
	if dest.Space {
		grants, err := move.DropGrantsOn(row.Resource)
		if err != nil {
			if rollback := renameChatFolder(toDir, fromDir); rollback != nil {
				return out, audience, errors.Join(err, rollback)
			}
			return out, audience, errors.Join(err, move.RestoreResource(previous))
		}
		for _, g := range grants {
			out.Dropped = append(out.Dropped, api.GrantTuple{AccessGrant: g, Source: "grant", Live: g.Live(v.acc.time())})
		}
	}

	v.acc = h.access()
	res := h.registeredResource(next, dest)
	out.Resource = accessResourceWire(res, v)
	_, agentErr := h.chatPersona(dest, destSeat)
	usable := agentErr == nil
	out.AgentUsable = &usable
	if agentErr != nil {
		out.AgentWords = agentErr.Error()
	}
	return out, acl.Union(before, v.acc.audienceOf(res.item)), nil
}

// mayMoveChat is the organization's rule for who moves a chat. Moving always needs manage on the chat. By default the
// person must also own it, which is what the server did before the rule was a setting; with acl.MoveByManage, manage is
// enough. A bot is refused before this is asked, and a policy never lets one move a chat.
func mayMoveChat(v Viewer, item Item) bool {
	if !can(v, item, acl.VerbManage) {
		return false
	}
	return v.owns(item) || v.acc.rules().Chats.MoveByFor() == acl.MoveByManage
}

func (h *Handler) chatMoveDestination(v Viewer, id string) (factory.OrgWorkstream, error) {
	ws, found, err := h.config.Store.Workstream(id)
	if err != nil {
		return ws, err
	}
	item := workstreamItem(ws)
	if !found || ws.Space && !v.owns(item) || !can(v, item, acl.VerbView) {
		return ws, chatMoveRefusal{http.StatusNotFound, "workstream not found"}
	}
	if !can(v, item, acl.VerbEdit) {
		return ws, chatMoveRefusal{http.StatusForbidden, "You need permission to edit the destination workstream."}
	}
	if ws.ArchivedAt != nil {
		return ws, chatMoveRefusal{http.StatusConflict, workstreamArchivedWords}
	}
	return ws, nil
}

func chatLocationValid(row store.Resource) bool {
	return (row.Parent.Kind == ItemWorkstream || row.Parent.Kind == "space") &&
		safeWorkstreamFolder(row.Parent.ID) && row.Owner.Kind == api.OwnerPerson &&
		personHandle.MatchString(row.Owner.ID) && chatIDPattern.MatchString(row.ID)
}

func safeWorkstreamFolder(id string) bool {
	return strings.HasPrefix(id, workstreamIDPrefix) && id != workstreamIDPrefix && filepath.Base(id) == id
}

func renameChatFolder(from, to string) error {
	if err := os.MkdirAll(filepath.Dir(to), 0o700); err != nil {
		return err
	}
	if _, err := os.Lstat(to); !errors.Is(err, os.ErrNotExist) {
		if err == nil {
			return os.ErrExist
		}
		return err
	}
	return os.Rename(from, to)
}

// reconcileChatFolders follows a committed registry move even if the process
// stopped before the files followed it.
func (h *Handler) reconcileChatFolders() {
	st := h.config.Store
	if st == nil {
		return
	}
	rows, err := st.Resources(chatKind)
	if err != nil {
		slog.Error("could not read chats to reconcile their folders", "error", err)
		return
	}
	pending, err := st.PendingGrantDrops(chatKind)
	if err != nil {
		slog.Error("could not read interrupted chat moves", "error", err)
		return
	}
	for _, row := range rows {
		if !chatLocationValid(row) || !pending[row.ID] && h.chatExists(agentSeat{row.Parent.ID, row.Owner.ID, row.ID}) {
			continue
		}
		if err := h.reconcileChatFolder(row.ID); err != nil {
			slog.Error("could not reconcile the chat folder", "chat", row.ID, "error", err)
		}
	}
}

func (h *Handler) reconcileChatFolder(id string) error {
	return h.config.Store.WithResourceMove(func(move store.ResourceMove) error {
		row, found, err := h.config.Store.Resource(chatKind, id)
		if err != nil || !found || !chatLocationValid(row) {
			return err
		}
		dest, found, err := h.config.Store.Workstream(row.Parent.ID)
		if err != nil || !found {
			return err
		}
		destination := agentSeat{dest.ID, row.Owner.ID, id}
		moved := false
		if !h.chatExists(destination) {
			folders, err := os.ReadDir(filepath.Dir(h.sharedAgentDir(dest.ID)))
			if err != nil && !os.IsNotExist(err) {
				return err
			}
			var source string
			for _, folder := range folders {
				ws := folder.Name()
				if !folder.IsDir() || ws == dest.ID || !safeWorkstreamFolder(ws) || !h.chatExists(agentSeat{ws, row.Owner.ID, id}) {
					continue
				}
				if source != "" {
					return errors.New("more than one source chat folder exists")
				}
				source = ws
			}
			if source == "" {
				return nil
			}
			if err := renameChatFolder(h.chatDir(source, row.Owner.ID, id), h.agentDir(destination)); err != nil {
				return err
			}
			moved = true
			slog.Warn("completed an interrupted chat move", "chat", id, "from", source, "to", dest.ID)
		}
		pending, err := move.GrantDropPending(row.Resource)
		if err != nil {
			return err
		}
		if pending || moved && dest.Space && row.Visibility == acl.Private {
			_, err = move.DropGrantsOn(row.Resource)
		}
		return err
	})
}
