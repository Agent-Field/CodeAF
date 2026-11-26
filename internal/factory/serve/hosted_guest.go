package serve

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/acl"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/coder/websocket"
)

// DefaultHostedHeartbeat keeps a guest wire's access up to date when no interval is set.
const DefaultHostedHeartbeat = 5 * time.Second

const (
	hostedGuestClientLineCap = 256 << 10
	hostedGuestHostLineCap   = 64 << 20
	// The hosted chat client accepts WebSocket messages up to 4 MiB.
	hostedGuestWriteChunk = 4 << 20
	hostedGuestRemoved    = "Your access to this chat was removed."
	hostedGuestLongLine   = "A message to this chat was too large."
	hostedGuestBadHello   = "This chat could not read the first message."
	hostedGuestBadCall    = "This chat could not read a request."
	// A guest opening a chat that is not running starts it for its owner, and either the owner's own limit or the machine's can say no.
	// The sentence is the guest's, never the owner's ("You already have ... chats running"), and names no number or person.
	hostedGuestNoRoom = "This chat is not running, and there is no room to start it right now. Try again later."
	// The two sentences a guest reads when the organization keeps them from starting a chat that is not running
	// (chats.wake_by). Neither says how the chat comes to be stopped, because the guest has no use for the word.
	hostedGuestWakeOwner = "This chat is not running. Only its owner can start it. Ask them to open it."
	hostedGuestWakeRun   = "This chat is not running. Starting it takes permission to send messages in it. Ask its owner."
)

var errHostedGuestLineCap = errors.New(hostedGuestLongLine)

type hostedGuest struct {
	ctx          context.Context
	cancel       context.CancelFunc
	heartbeat    time.Duration
	client, host net.Conn
	// rights holds a hostedGuestRights and is replaced on every heartbeat.
	rights  atomic.Value
	writeMu sync.Mutex
}

// addGuest and closeHostedGuests use one lock so shutdown also reaches a wire still opening.
func (hs *hostedState) addGuest(p *hostedGuest) bool {
	hs.guestMu.Lock()
	defer hs.guestMu.Unlock()
	if hs.guestsClosed {
		return false
	}
	if hs.guests == nil {
		hs.guests = make(map[*hostedGuest]struct{})
	}
	hs.guests[p] = struct{}{}
	hs.guestWG.Add(1)
	return true
}

func (hs *hostedState) dropGuest(p *hostedGuest) {
	hs.guestMu.Lock()
	defer hs.guestMu.Unlock()
	delete(hs.guests, p)
	hs.guestWG.Done()
}

func (h *Handler) closeHostedGuests() {
	cfg := h.config.Hosted
	if cfg == nil || cfg.state == nil {
		return
	}
	hs := cfg.state
	hs.guestMu.Lock()
	hs.guestsClosed = true
	for guest := range hs.guests {
		guest.cancel()
	}
	hs.guestMu.Unlock()
	hs.guestWG.Wait()
}

func (h *Handler) hostedGuestWire(w http.ResponseWriter, r *http.Request, item Item, owner string, v Viewer) {
	hs := h.config.Hosted.state
	heartbeat := h.config.Hosted.Heartbeat
	if heartbeat <= 0 {
		heartbeat = DefaultHostedHeartbeat
	}
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	p := &hostedGuest{ctx: ctx, cancel: cancel, heartbeat: heartbeat}
	if !hs.addGuest(p) {
		h.fail(w, http.StatusServiceUnavailable, hostedUnreachable)
		return
	}
	defer hs.dropGuest(p)
	r = r.WithContext(ctx)
	if !h.hostedPush(w, r, hs, item.ID, owner) {
		return
	}

	client, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true,
		CompressionMode:    websocket.CompressionDisabled,
	})
	if err != nil {
		return
	}
	clientConn, closeClient := hostedGuestConn(ctx, client)
	defer closeClient()
	p.client = clientConn
	p.rights.Store(hostedGuestRightsOf(v, item))
	var pumps sync.WaitGroup
	defer func() {
		cancel()
		pumps.Wait()
	}()
	pumps.Go(func() {
		p.checkHeartbeat(func() hostedGuestRights {
			return h.refreshHostedGuest(r, item.ID, owner, v.Handle)
		})
	})

	if words := h.hostedGuestWakeRefusal(ctx, hs, item.ID, owner, v, p.rights.Load().(hostedGuestRights)); words != "" {
		p.fatal(words)
		return
	}
	host, status := hs.dialGuestHost(ctx, item.ID, owner)
	if host == nil {
		switch status {
		case http.StatusNotFound:
			p.fatal(hostedGuestRemoved)
			return
		case http.StatusConflict:
			hs.bearers.revokeFor(item.ID, owner)
		case http.StatusTooManyRequests:
			p.fatal(hostedGuestNoRoom)
			return
		}
		p.fatal(hostedUnreachable)
		return
	}
	hostConn, closeHost := hostedGuestConn(ctx, host)
	defer closeHost()
	p.host = hostConn
	pumps.Go(func() {
		defer cancel()
		p.fromHost()
	})
	p.fromClient(hostedGuestLabel(v), func(method, class string) {
		h.noteHostedGuestCall(item.ID, owner, v.Handle, method, class)
	})
	cancel()
}

// hostedGuestWakeRefusal is the sentence a guest is refused with when the organization's rule (chats.wake_by) keeps them
// from starting a chat that is not running, and "" when the wire may go on. Opening a chat that is not running starts it
// for its owner, in its owner's place among the chats they may have running, and the rule is the only thing that says
// whether a guest may do that. The default rule asks for nothing and reads nothing, so an organization that never set it
// sees no extra request. A chat that stops between this read and the connection below it can still be started by the
// guest: the rule is checked when the window opens, not held for the life of the wire.
func (h *Handler) hostedGuestWakeRefusal(ctx context.Context, hs *hostedState, id, owner string, v Viewer, rights hostedGuestRights) string {
	rule := v.acc.rules().Chats.WakeByFor()
	if rule == acl.WakeByView {
		return ""
	}
	// An owner is the person, never a bot acting for them, because a bot's scope is a limit on what it may start.
	words, may := hostedGuestWakeOwner, v.Handle == owner && v.Agent == nil
	if rule == acl.WakeByRun {
		words, may = hostedGuestWakeRun, rights.verbs.Has(acl.VerbRun)
	}
	if may {
		return ""
	}
	status, _, body, err := hs.call(ctx, http.MethodGet, "/v1/sessions/"+id, owner, nil)
	if err != nil || (status != http.StatusOK && status != http.StatusNotFound) {
		// Without the chat's state the rule cannot be applied, and starting it would be the one thing it forbids.
		return hostedUnreachable
	}
	if status == http.StatusNotFound {
		return hostedGuestRemoved
	}
	var row hostedStatusFields
	if json.Unmarshal(body, &row) != nil {
		return hostedUnreachable
	}
	if row.State == hostedStateRunning {
		return ""
	}
	return words
}

func (hs *hostedState) dialGuestHost(ctx context.Context, id, owner string) (*websocket.Conn, int) {
	// The dial deadline bounds only the handshake, because a live wire may last for hours.
	ctx, cancel := context.WithTimeout(ctx, hostedCallTimeout)
	defer cancel()
	httpClient := &http.Client{Transport: hs.transport, CheckRedirect: hs.calls.CheckRedirect}
	host, response, err := websocket.Dial(ctx, hs.base.String()+"/v1/sessions/"+id+"/wire", &websocket.DialOptions{
		HTTPClient:      httpClient,
		HTTPHeader:      http.Header{"Authorization": {"Bearer " + hs.secret}, "X-Codeaf-Person": {owner}},
		CompressionMode: websocket.CompressionDisabled,
	})
	if err != nil && response != nil {
		return nil, response.StatusCode
	}
	return host, 0
}

func hostedGuestConn(ctx context.Context, conn *websocket.Conn) (net.Conn, func()) {
	tunnel := websocket.NetConn(ctx, conn, websocket.MessageBinary)
	// NetConn clears the message cap. The line cap is independent because a message may carry many lines.
	conn.SetReadLimit(hostedGuestHostLineCap + 1)
	// Close even when neither a read nor a write is in progress on this leg.
	stop := context.AfterFunc(ctx, func() { _ = conn.CloseNow() })
	return tunnel, func() {
		stop()
		_ = conn.CloseNow()
	}
}

func (p *hostedGuest) fromHost() {
	reader := bufio.NewReader(p.host)
	for {
		line, err := readHostedGuestLine(reader, hostedGuestHostLineCap)
		if err != nil || p.writeClient(line) != nil {
			return
		}
	}
}

func (p *hostedGuest) fromClient(label string, forwarding func(method, class string)) {
	reader := bufio.NewReader(p.client)
	var hello bool
	for {
		line, err := readHostedGuestLine(reader, hostedGuestClientLineCap)
		if errors.Is(err, errHostedGuestLineCap) {
			p.fatal(hostedGuestLongLine)
		}
		if err != nil {
			return
		}
		rights := p.rights.Load().(hostedGuestRights)
		if !rights.verbs.Has(acl.VerbView) {
			// The heartbeat owns the final line when it takes access away.
			<-p.ctx.Done()
			return
		}
		var frame remote.Frame
		err = json.Unmarshal(line, &frame)
		if !hello {
			if err != nil || frame.Kind != "hello" || !rewriteHostedGuestHello(&frame, label) {
				p.fatal(hostedGuestBadHello)
				return
			}
			hello = true
		} else {
			if err != nil || frame.Kind != "call" || strings.TrimSpace(frame.Method) == "" {
				p.fatal(hostedGuestBadCall)
				return
			}
			class, words := hostedGuestDecision(frame, rights)
			if words != "" {
				// A result always has an ID, including when the caller used zero.
				result := struct {
					Kind  string `json:"kind"`
					ID    uint64 `json:"id"`
					Error string `json:"error"`
				}{"result", frame.ID, words}
				raw, _ := json.Marshal(result)
				if p.writeClient(append(raw, '\n')) != nil {
					return
				}
				continue
			}
			forwarding(frame.Method, class)
		}
		// Re-encoding makes the engine receive exactly the frame whose method was checked.
		raw, err := json.Marshal(frame)
		if err != nil {
			return
		}
		if _, err := p.host.Write(append(raw, '\n')); err != nil {
			return
		}
	}
}

func hostedGuestRefusal(class string, verbs acl.VerbSet) string {
	switch class {
	case acl.VerbView:
		return ""
	case acl.VerbRun:
		if !verbs.Has(class) {
			return hostedGuestNeedsRun
		}
	case acl.VerbSteer:
		if !verbs.Has(class) {
			return hostedGuestNeedsSteer
		}
	case acl.VerbAnswer:
		if !verbs.Has(class) {
			return hostedGuestNeedsAnswer
		}
	default:
		return hostedGuestOwnerOnly
	}
	return ""
}

// hostedGuestLabel is the name every other window of a chat reads for a guest's window: "typing from santosh". It is
// the person as this server knows them and never a word the guest's own computer chose, so a guest cannot appear to be
// the owner or another member. A bot reads as its creator's bot, because that is whose rights it holds.
func hostedGuestLabel(v Viewer) string {
	if v.Agent != nil {
		return v.Handle + "'s bot"
	}
	return v.Handle
}

func rewriteHostedGuestHello(frame *remote.Frame, label string) bool {
	var fields map[string]json.RawMessage
	if json.Unmarshal(frame.Payload, &fields) != nil || fields == nil {
		return false
	}
	var window struct {
		ClientID string `json:"clientID"`
	}
	if json.Unmarshal(frame.Payload, &window) != nil {
		return false
	}
	// The engine accepts case variants of its JSON fields, so discard those too.
	// Remote is what the door that authenticated a viewer of one person's own computers stamps on its hello, and surface is
	// the name the other windows read for this one: neither is ever a guest's to say.
	replaced := []string{"launch", "model", "level", "workspace", "session", "new", "join", "holdInterruptedWork", "window", "back", "clientID", "remote", "surface"}
	for field := range fields {
		for _, name := range replaced {
			if strings.EqualFold(field, name) {
				delete(fields, field)
				break
			}
		}
	}
	// The allow-list protects this wire. Setting watch would hide transcripts and most other reads.
	fields["back"] = json.RawMessage("true")
	fields["clientID"], _ = json.Marshal("guest:" + window.ClientID)
	if label != "" {
		fields["surface"], _ = json.Marshal(label)
	}
	frame.Payload, _ = json.Marshal(fields)
	return true
}

// readHostedGuestLine bounds the accumulated bytes even when the peer never sends a newline.
func readHostedGuestLine(reader *bufio.Reader, limit int) ([]byte, error) {
	var line []byte
	for {
		chunk, err := reader.ReadSlice('\n')
		if len(chunk) > limit-len(line) {
			return nil, errHostedGuestLineCap
		}
		if err == nil {
			if line == nil {
				return chunk, nil
			}
			return append(line, chunk...), nil
		}
		if err != bufio.ErrBufferFull {
			return nil, err
		}
		if len(chunk) == limit-len(line) {
			// A line at the limit still needs room for its newline.
			return nil, errHostedGuestLineCap
		}
		line = append(line, chunk...)
	}
}

func (p *hostedGuest) writeClient(line []byte) error {
	p.writeMu.Lock()
	defer p.writeMu.Unlock()
	if err := p.ctx.Err(); err != nil {
		return err
	}
	// A large engine line can use several messages while keeping the write lock for the entire line.
	for len(line) != 0 {
		chunk := min(len(line), hostedGuestWriteChunk)
		if _, err := p.client.Write(line[:chunk]); err != nil {
			return err
		}
		line = line[chunk:]
	}
	return nil
}

func (p *hostedGuest) fatal(words string) {
	// One complete line goes before the close. A peer that stops reading cannot keep the wire alive.
	_ = p.client.SetWriteDeadline(time.Now().Add(p.heartbeat))
	p.writeMu.Lock()
	defer p.writeMu.Unlock()
	defer p.client.SetWriteDeadline(time.Time{})
	if p.ctx.Err() == nil {
		_ = json.NewEncoder(p.client).Encode(remote.Frame{Kind: "fatal", Error: words})
		p.cancel()
	}
}

func hostedGuestVerbs(v Viewer, item Item) acl.VerbSet {
	v = hostedWithoutOperator(v)
	return v.acc.rules().Verbs(v.caller(), v.acc.item(item), v.acc.time())
}

// hostedGuestRights is what one guest wire may do right now: the verbs the policy gives the person on the chat, and the
// organization's rule for who approves a tool call. Both are read again on every heartbeat, so a policy change reaches a
// window that is already open, in the same beat that takes a verb away.
type hostedGuestRights struct {
	verbs     acl.VerbSet
	approvals string
}

func hostedGuestRightsOf(v Viewer, item Item) hostedGuestRights {
	return hostedGuestRights{verbs: hostedGuestVerbs(v, item), approvals: v.acc.rules().Chats.ApprovalsByFor()}
}

// hostedGuestDecision classifies one call from a guest and answers the sentence the guest is refused with, or "" when
// the call may reach the chat. The method table decides almost everything; the two doors that carry a tool approval are
// the exception, because approving is not one method: the older frame is ResolveConsent, and the answer to ANY question
// the chat asks, an allow among them, is ResolveQuestion.
func hostedGuestDecision(frame remote.Frame, rights hostedGuestRights) (class, refusal string) {
	class = hostedGuestMethods[frame.Method]
	if frame.Method == remote.MethodConsent && rights.approvals == acl.ApprovalsByAnswer {
		// One allow or one refusal of the call the chat is waiting on. Allowing a tool for good has its own frame,
		// ResolveConsentRemember, which the table leaves with the owner under every setting.
		class = acl.VerbAnswer
	}
	if words := hostedGuestRefusal(class, rights.verbs); words != "" {
		return class, words
	}
	if frame.Method == remote.MethodQuestionResolve && !hostedGuestMayAnswer(frame, rights) {
		return class, hostedGuestOwnerOnly
	}
	return class, ""
}

// hostedGuestMayAnswer is whether a guest who already holds the answer verb may send this ResolveQuestion. An answer that
// is not to a tool approval always may. One that is waits for the owner unless the organization let answerers approve,
// and even then only the two plain answers (allow once, refuse) pass. The payload is read with the engine's own decoder
// into the engine's own type, so this cannot read an answer differently from the way the chat will.
func hostedGuestMayAnswer(frame remote.Frame, rights hostedGuestRights) bool {
	var args remote.QuestionArgs
	if len(frame.Payload) != 0 && json.Unmarshal(frame.Payload, &args) != nil {
		// The chat cannot read it either and refuses it by itself.
		return true
	}
	answer := args.Answer
	if !strings.EqualFold(strings.TrimSpace(string(answer.Kind)), string(session.QuestionConsent)) {
		return true
	}
	if rights.approvals != acl.ApprovalsByAnswer || answer.Dismissed || answer.Revises || answer.Clarify {
		return false
	}
	switch answer.FirstKey() {
	case "1", "3":
		return true
	}
	return false
}

func (p *hostedGuest) checkHeartbeat(refresh func() hostedGuestRights) {
	ticker := time.NewTicker(p.heartbeat)
	defer ticker.Stop()
	for {
		select {
		case <-p.ctx.Done():
			return
		case <-ticker.C:
			rights := refresh()
			p.rights.Store(rights)
			if !rights.verbs.Has(acl.VerbView) {
				p.fatal(hostedGuestRemoved)
				return
			}
		}
	}
}

func (h *Handler) refreshHostedGuest(r *http.Request, id, owner, asker string) hostedGuestRights {
	r = authRequest(r)
	if !h.allowed(r) {
		return hostedGuestRights{}
	}
	v, err := h.itemViewer(r)
	if err != nil || v.Handle != asker || !h.teamAllows(v.Handle) || r.Context().Value(authErrorKey{}).(*authErrorState).err != nil {
		return hostedGuestRights{}
	}
	st := h.config.Store
	if call, _ := agentCallOf(r); call.bot != "" {
		bot, _, state, err := st.FindBotToken(sessionHash(bearer(r)), h.now())
		if err != nil || state != store.BotTokenLive || bot.ID != call.bot || bot.Creator != v.Handle {
			return hostedGuestRights{}
		}
		// The stored scope and the policy bundle can both change while the bot is attached.
		v.Agent.Only = botScopeVerbs(v.acc.rules(), bot.Scope)
	}
	row, found, err := st.Resource(store.SessionKind, id)
	if err != nil || !found || row.Owner.ID != owner || !hostedRegistration(row) {
		return hostedGuestRights{}
	}
	return hostedGuestRightsOf(v, h.resourceItem(row, factory.OrgWorkstream{}))
}

func (h *Handler) noteHostedGuestCall(id, owner, asker, method, class string) {
	// Attribution is approximate because the next model call can follow any accepted words.
	if class == acl.VerbRun || method == remote.MethodSteer || method == remote.MethodQuestionResolve || method == remote.MethodConsent {
		h.config.Hosted.state.noteAsker(id, asker)
	}
	// The chat checks the keyboard for prompts and steer. A guest with run can use Take to drive.
	switch method {
	case remote.MethodSubmit, remote.MethodSubmitBash, remote.MethodSubmitImage, remote.MethodSubmitFiles, remote.MethodFollowUp, remote.MethodSteer:
		h.recordHostedAudit(hostedAuditEntry{Person: owner, Chat: id, Kind: "prompt", Asker: asker})
	}
}
