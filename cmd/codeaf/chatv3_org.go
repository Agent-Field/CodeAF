package main

// ── `codeaf chat --org https://org.example.com` ─────────────────────────────
//
// A way to reach a conversation that is not on this computer, and the first
// whose far end is an organization's server rather than a machine a person
// owns:
//
//	laptop$ codeaf chat --org https://org.example.com
//	  [chat opens; the conversation, its commands and its folder are the
//	   organization server's, kept apart from every other person's]
//
// IT IS `--at`'S SIBLING AND DIFFERS ONLY IN THE CARRIER (chatv3_at.go). Where
// `--at` pairs a device through a relay and gets a tunnel, this one signs in
// with the token the organization gave this person and gets a WebSocket onto
// the server's `/v1/hosted/sessions/<id>/wire`. From the moment a byte pipe
// exists the two doors are the same code: the same hello, the same fleet, the
// same surface, assembled by the same functions. Anything that differed above
// the pipe would be a bug.
//
// THE PROMPT LAW, INHERITED FOR THE SAME REASON: everything that can fail with
// a sentence for the person happens before the surface takes the screen, on a
// plain terminal, in plain words — and none of those words is machinery. A
// person using a hosted chat is told "<host> is unreachable.", never which
// process on which account the server could not start.
//
// THE FAR END IS LESS TRUSTED THAN A MACHINE THE PERSON OWNS, and the whole
// door is written from that sentence (contract C11):
//
//   - NOTHING THE ENGINE SENDS CAUSES A WRITE, A PROGRAM OR AN OPEN HERE. The
//     door advertises no client tools, so the engine has no way to ask this
//     computer to do anything; it hands the surface no seam that raises a
//     desktop banner in answer to what the engine says ([orgSurfaceOptions]);
//     and every remaining local effect of the surface (opening a link, saving
//     a file) waits for the person's own key or click.
//   - THE BEARER GOES IN ONE PLACE, the Authorization header of the request,
//     and in no URL, no log line and no error text ([orgDoor.say] cleans what
//     a server echoes back).
//   - AN ADDRESS THE SERVER NAMES IS NEVER FOLLOWED: a redirect is a refusal,
//     because it would carry the bearer somewhere the person never typed.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/coder/websocket"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/env"
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/tui3"
	codeupdate "github.com/Agent-Field/codeaf/internal/update"
)

// orgTokenEnv is where a person's sign-in token for the organization is read
// first. The second place is the token `codeaf attach` keeps.
const orgTokenEnv = "CODEAF_ORG_TOKEN"

// orgCallWithin bounds every plain request this door makes before the surface
// opens: listing the person's chats, making a new one, and the WebSocket's own
// opening handshake. It is the longest a person waits to be told the server is
// not there.
const orgCallWithin = 15 * time.Second

// orgReadLimit is the largest single message the wire accepts from the server.
// NetConn resets the limit to unlimited when it is made, so it is set after.
const orgReadLimit = 4 << 20

const orgSessionsPath = "/v1/hosted/sessions"

// orgLaunch is one `--org` launch as the flag parser saw it. It has the shape
// of [atLaunch], because it is the same launch down a different pipe, plus the
// flags the shared parser checks for the other doors inline.
type orgLaunch struct {
	// target is --org as typed: the organization server's address.
	target string
	// session is --session: the name of one hosted chat (hs_ and 24 hex).
	session string
	// model and level are --model and --reasoning, carried in the hello.
	model string
	level string
	// once is --once: one message, printed, no terminal ownership.
	once string
	// pick is `codeaf resume`: the same surface, opened on the session picker.
	pick bool
	// noCompact, yolo and budget are refused rather than ignored, exactly as
	// over --at: they are properties of a session that is built over there.
	noCompact bool
	yolo      bool
	budget    bool
	// oneModel and noHost are refused for the reasons --at refuses them.
	oneModel bool
	noHost   bool
	// host and at are the other two ways to reach a conversation, refused
	// beside this one.
	host string
	at   string
	// restart is filled by the surface and read after this door has closed.
	restart *codeupdate.Plan
}

// orgHostName is the name this door calls the server by in every sentence a
// person reads: the host of the address, without its scheme or any port.
func orgHostName(target string) string {
	raw := strings.TrimSpace(target)
	if u, err := url.Parse(raw); err == nil && u.Hostname() != "" {
		return u.Hostname()
	}
	if raw == "" {
		return "your organization's server"
	}
	return raw
}

// check refuses the flags this door cannot honour, in the words --at uses,
// because it is the same fact: the chat is built on the other side and a flag
// typed here has nowhere to land.
func (l orgLaunch) check() error {
	where := orgHostName(l.target)
	if strings.TrimSpace(l.host) != "" || strings.TrimSpace(l.at) != "" {
		return errors.New("--org reaches your organization's server, and --host and --at each reach one machine: name one of them, not two")
	}
	if l.oneModel {
		return fmt.Errorf("--one-model settles this machine's model rows; over --org the server answers them, so the two cannot be combined")
	}
	if l.noHost {
		return fmt.Errorf("--no-host keeps a conversation in this process; over --org the conversation is on %s, so the two cannot be combined", where)
	}
	var named []string
	if l.noCompact {
		named = append(named, "--no-compact")
	}
	if l.yolo {
		named = append(named, "--yolo")
	}
	if l.budget {
		named = append(named, "--max-hours/--max-cost")
	}
	if len(named) == 0 {
		return nil
	}
	return fmt.Errorf("%s cannot travel over --org: the chat is built on %s, so it cannot be set from here",
		strings.Join(named, " and "), where)
}

// ── the door: the server, the token, the chat ───────────────────────────────

// orgDoor is everything this door knows about the server before the surface
// opens: where it is, who the person is to it, and how to talk to it.
type orgDoor struct {
	// base is the server's origin, scheme and host and port, with no path.
	base string
	// host is the name every sentence uses ([orgHostName]).
	host string
	// token is the person's sign-in. It is read ONLY by [orgDoor.authorize].
	token string
	// http is the client for every plain request. It follows no redirect and
	// uses no proxy: the server may be a loopback or private address, and the
	// bearer must go only where the person typed.
	http *http.Client
	// say is where a warning from the server is shown, once, before the surface
	// opens (stderr in the product).
	say func(string)
}

// newOrgDoor checks the address, finds the person's token and makes the client.
//
// THE ADDRESS RULE IS THE FACTORY'S: https anywhere, plain http only for this
// computer, because the bearer must not cross a network in the clear
// ([factoryURL] says what an origin is, [factoryLoopback] what "this computer"
// is — both are the factory's own, so one rule covers both doors).
func newOrgDoor(target string, say func(string)) (*orgDoor, error) {
	base, err := factoryURL(target)
	if err != nil {
		return nil, fmt.Errorf("%q is not an organization address; use https://host", strings.TrimSpace(target))
	}
	if strings.HasPrefix(base, "http://") && !factoryLoopback(base) {
		return nil, fmt.Errorf("use https:// for %s: a sign-in must not cross a network in the clear", orgHostName(base))
	}
	token, ok := orgToken(base)
	d := orgDoorWith(base, token, say)
	if !ok {
		return nil, d.signInSentence()
	}
	return d, nil
}

// orgDoorWith takes an already checked origin and a sign-in held by the caller.
func orgDoorWith(base, token string, say func(string)) *orgDoor {
	return &orgDoor{
		base: base, host: orgHostName(base), token: token, say: say,
		http: &http.Client{
			Timeout:       orgCallWithin,
			Transport:     &http.Transport{Proxy: nil},
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}
}

// signInSentence is what a person is told when the server has no sign-in of
// theirs to go on, or did not accept the one it was given.
func (d *orgDoor) signInSentence() error {
	return fmt.Errorf("Sign in to %s first: set %s, or run codeaf attach %s --token <token>.", d.host, orgTokenEnv, d.base)
}

// orgToken is the person's sign-in for the server at base: the environment
// first, then the token `codeaf attach` keeps, but only when that file names
// THE SAME ORIGIN. A token for one server is never sent to another.
func orgToken(base string) (string, bool) {
	if token := strings.TrimSpace(env.Get(orgTokenEnv)); token != "" {
		return token, true
	}
	attached, ok := readFactoryAttachment()
	if !ok || strings.TrimSpace(attached.Token) == "" || !sameOrigin(attached.URL, base) {
		return "", false
	}
	return strings.TrimSpace(attached.Token), true
}

// sameOrigin is whether two origins have the same scheme, host and port, with
// the port a scheme implies counted as written.
func sameOrigin(a, b string) bool {
	ua, errA := url.Parse(a)
	ub, errB := url.Parse(b)
	if errA != nil || errB != nil || ua.Hostname() == "" {
		return false
	}
	return ua.Scheme == ub.Scheme && strings.EqualFold(ua.Hostname(), ub.Hostname()) && originPort(ua) == originPort(ub)
}

func originPort(u *url.URL) string {
	if port := u.Port(); port != "" {
		return port
	}
	if u.Scheme == "https" {
		return "443"
	}
	return "80"
}

// authorize is the ONE place the bearer is put on a request.
func (d *orgDoor) authorize(header http.Header) {
	header.Set("Authorization", "Bearer "+d.token)
}

// clean is a sentence from the server with the bearer taken out, in case a
// server that echoes what it was sent echoes that too.
func (d *orgDoor) clean(sentence string) string {
	if d.token != "" {
		sentence = strings.ReplaceAll(sentence, d.token, "")
	}
	return strings.TrimSpace(sentence)
}

// refusal is the sentence for a response that was not a success. The server's
// own {"error": "…"} sentence is shown as it wrote it, because it is already
// plain words; an answer with none (a proxy's page, an empty 401) gets one of
// ours, and a page is never printed.
func (d *orgDoor) refusal(status int, body []byte) error {
	var said struct {
		Error string `json:"error"`
	}
	var words string
	var serverSaid bool
	if json.Unmarshal(body, &said) == nil {
		serverSaid = strings.TrimSpace(said.Error) != ""
		words = d.clean(said.Error)
	}
	if words == "" {
		if status == http.StatusUnauthorized || status == http.StatusForbidden {
			words = d.signInSentence().Error()
		} else {
			words = fmt.Sprintf("%s could not answer (status %d).", d.host, status)
		}
	}
	return &orgDoorError{status: status, words: words, serverSaid: serverSaid}
}

// orgDoorError keeps the HTTP outcome for callers that also need a wire code.
type orgDoorError struct {
	status     int
	words      string
	serverSaid bool
}

func (e *orgDoorError) Error() string { return e.words }

func (d *orgDoor) unreachable() error {
	return &orgDoorError{words: fmt.Sprintf("%s is unreachable.", d.host)}
}

// call makes one request to the server and decodes a success into out.
func (d *orgDoor) call(ctx context.Context, method, path string, out any) error {
	ctx, cancel := context.WithTimeout(ctx, orgCallWithin)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, d.base+path, nil)
	if err != nil {
		return d.unreachable()
	}
	d.authorize(req.Header)
	req.Header.Set("Accept", "application/json")
	resp, err := d.http.Do(req)
	if err != nil {
		// THE TRANSPORT'S OWN ERROR IS DROPPED: it names the address and a person
		// needs one sentence, not a dial trace.
		return d.unreachable()
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return d.refusal(resp.StatusCode, body)
	}
	if out == nil || len(strings.TrimSpace(string(body))) == 0 {
		return nil
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("%s sent back something this program cannot read.", d.host)
	}
	return nil
}

// orgTime is a time the server writes as RFC 3339 text or as Unix seconds; a
// value that is neither reads as the zero time rather than failing the list.
type orgTime struct{ time.Time }

func (t *orgTime) UnmarshalJSON(data []byte) error {
	text := strings.TrimSpace(string(data))
	if text == "" || text == "null" {
		return nil
	}
	if unquoted, err := strconv.Unquote(text); err == nil {
		if parsed, err := time.Parse(time.RFC3339Nano, unquoted); err == nil {
			t.Time = parsed
		}
		return nil
	}
	if seconds, err := strconv.ParseFloat(text, 64); err == nil {
		t.Time = time.Unix(int64(seconds), 0)
	}
	return nil
}

// orgSession is one hosted chat as the server lists it.
type orgSession struct {
	ID         string  `json:"id"`
	State      string  `json:"state"`
	Created    orgTime `json:"created"`
	LastActive orgTime `json:"last_active"`
	Warning    string  `json:"warning"`
}

// latestOrgSession is the chat the person was last in: the latest
// last_active, then the latest created, then the first one listed. A row whose
// name is not a chat's name is not a chat and is passed over.
func latestOrgSession(rows []orgSession) (orgSession, bool) {
	var best orgSession
	found := false
	for _, row := range rows {
		if !appwire.ValidHostedSessionID(row.ID) {
			continue
		}
		switch {
		case !found,
			row.LastActive.After(best.LastActive.Time),
			row.LastActive.Equal(best.LastActive.Time) && row.Created.After(best.Created.Time):
			best, found = row, true
		}
	}
	return best, found
}

// pick is the chat this launch opens. --session names one; otherwise the
// person's most recently active one; otherwise a new one is made. A warning
// the server attaches is shown once, here, before the surface exists.
func (d *orgDoor) pick(ctx context.Context, want string) (string, error) {
	if want = strings.TrimSpace(want); want != "" {
		if !appwire.ValidHostedSessionID(want) {
			return "", fmt.Errorf("%q is not the name of a chat on %s; the names look like hs_ followed by 24 letters and digits", want, d.host)
		}
		return want, nil
	}
	var listed struct {
		Sessions []orgSession `json:"sessions"`
	}
	if err := d.call(ctx, http.MethodGet, orgSessionsPath, &listed); err != nil {
		return "", err
	}
	if row, ok := latestOrgSession(listed.Sessions); ok {
		d.warn(row.Warning)
		return row.ID, nil
	}
	var made orgSession
	if err := d.call(ctx, http.MethodPost, orgSessionsPath, &made); err != nil {
		return "", err
	}
	if !appwire.ValidHostedSessionID(made.ID) {
		return "", fmt.Errorf("%s sent back something this program cannot use.", d.host)
	}
	d.warn(made.Warning)
	return made.ID, nil
}

// warn shows a sentence the server attached to a chat, once, through say.
func (d *orgDoor) warn(sentence string) {
	if sentence = d.clean(sentence); sentence != "" && d.say != nil {
		d.say(sentence)
	}
}

// open dials the wire of one chat and answers it as a byte pipe.
//
// THE BEARER IS A HEADER ON THE UPGRADE REQUEST AND NOWHERE ELSE. The handshake
// is bounded by [orgCallWithin], but the connection itself outlives it: it ends
// when its owner closes it, so it is made on a context of its own.
func (d *orgDoor) open(ctx context.Context, id string) (net.Conn, error) {
	if !appwire.ValidHostedSessionID(id) {
		return nil, fmt.Errorf("%q is not the name of a chat on %s", id, d.host)
	}
	address, err := url.Parse(d.base)
	if err != nil {
		return nil, d.unreachable()
	}
	if address.Scheme == "https" {
		address.Scheme = "wss"
	} else {
		address.Scheme = "ws"
	}
	address.Path = orgSessionsPath + "/" + id + "/wire"
	header := http.Header{}
	d.authorize(header)
	handshake, cancel := context.WithTimeout(ctx, orgCallWithin)
	defer cancel()
	socket, response, err := websocket.Dial(handshake, address.String(), &websocket.DialOptions{
		HTTPClient:      d.http,
		HTTPHeader:      header,
		CompressionMode: websocket.CompressionDisabled,
	})
	if err != nil {
		if response != nil && response.Body != nil {
			body, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
			_ = response.Body.Close()
			return nil, d.refusal(response.StatusCode, body)
		}
		return nil, d.unreachable()
	}
	conn := websocket.NetConn(context.Background(), socket, websocket.MessageBinary)
	// AFTER NetConn, which resets the limit to unlimited on its way in.
	socket.SetReadLimit(orgReadLimit)
	return conn, nil
}

// attach opens the chat's wire and says hello on it, and answers the client and the one call that shuts it and its
// link. A WINDOW REDIALS BY ITSELF when its link drops (a laptop that slept, a network that changed, the server that
// restarted): the chat goes on running on the server whether anyone is looking, so the window comes back to it with
// nothing lost and the keyboard where it was, exactly as a window on another of the person's own computers does.
//
// THE FIRST DIAL IS NOT ROAMED, so everything that can go wrong before the surface owns the screen (no sign-in, an
// address that is down, a chat that is not theirs) is still said at once on a terminal that is still the person's. A
// headless run (roam false) is one message in a script and is never retried, because a script that waits half an hour
// for a server is worse than one that fails. A no that will not change ([orgRefusesAgain]) ends the redialling with the
// server's own words.
func (d *orgDoor) attach(ctx context.Context, id string, hello remote.Hello, roam bool) (*remote.Client, func() error, error) {
	if !roam {
		conn, err := d.open(ctx, id)
		if err != nil {
			return nil, nil, err
		}
		client, err := remote.Dial(conn, d.host, hello)
		if err != nil {
			_ = conn.Close()
			return nil, nil, err
		}
		return client, func() error {
			err := client.Close()
			if shutErr := conn.Close(); err == nil {
				err = shutErr
			}
			return err
		}, nil
	}
	client, err := remote.Roam(d.host, hello, remote.Roaming{
		Dial: func() (io.ReadWriteCloser, error) {
			conn, err := d.open(ctx, id)
			if err != nil {
				return nil, err
			}
			return conn, nil
		},
		// A server that restarts drops every window at one moment, so their first retries are spread out.
		SpreadReturns: true,
		EndsOn:        orgRefusesAgain,
	})
	if err != nil {
		return nil, nil, err
	}
	return client, client.Close, nil
}

// orgRefusesAgain is whether a dial the server answered with a refusal will be refused the same way again: the sign-in
// was not accepted or was taken away (401, 403) or the chat is not there for this person (404). Anything else is a
// moment's trouble (the server down, a restart, no room yet) and is tried again.
func orgRefusesAgain(err error) bool {
	var refused *orgDoorError
	if !errors.As(err, &refused) || !refused.serverSaid {
		return false
	}
	switch refused.status {
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound:
		return true
	}
	return false
}

// ── the surface ─────────────────────────────────────────────────────────────

// hello is the hello this door says on a connection. THE WORKSPACE STAYS
// EMPTY: the folder a hosted chat works in is fixed on the server, and a path
// typed here would be a path on a computer the server cannot see. A headless
// `--once` is not a window.
func (l orgLaunch) hello(window bool) remote.Hello {
	return remote.Hello{
		Headless:  l.once != "",
		Workspace: "",
		Session:   "",
		Model:     l.model,
		Level:     l.level,
		Window:    window && l.once == "",
	}
}

// orgSurfaceOptions is the surface's options with what this door must not
// hand an untrusted engine taken out. It is the structural half of contract
// C11: the surface raises a desktop banner for an automation's news, and the
// news is whatever the engine says, so a surface built for this door has no
// notifier to call. Nothing the person does is lost — the list of automations
// still reads — only the engine's power to start a program here by speaking.
func orgSurfaceOptions(options tui3.Options) tui3.Options {
	options.Automations.Claim = nil
	return options
}

// openChatV3Org is the launch.
func openChatV3Org(launch orgLaunch) error {
	if err := launch.check(); err != nil {
		return err
	}
	if launch.pick && launch.once != "" {
		return fmt.Errorf(`codeaf resume opens the session picker; for one headless message use: codeaf chat --org %s --once "text"`, strings.TrimSpace(launch.target))
	}
	// EVERY LINE BEFORE THE SURFACE GOES TO STDERR, so that `--once` piped into
	// a file is the reply and nothing else.
	door, err := newOrgDoor(launch.target, func(line string) { fmt.Fprintln(os.Stderr, line) })
	if err != nil {
		return err
	}
	ctx := context.Background()
	id, err := door.pick(ctx, launch.session)
	if err != nil {
		return err
	}
	client, shutBoot, err := door.attach(ctx, id, launch.hello(true), launch.once == "")
	if err != nil {
		return err
	}
	closeLink := func() { _ = shutBoot() }
	defer func() { closeLink() }()

	// NO CLIENT TOOLS ARE ADVERTISED, on this connection or any other: this
	// function never calls ClientTools.Set and never installs a handler, so the
	// engine has no verb that runs anything here (C11).
	agent := client.Agent()
	if launch.once != "" {
		return runHostOnce(agent, launch.once)
	}
	// ANOTHER CONVERSATION IS ANOTHER WEBSOCKET TO THE SAME CHAT'S WIRE, with
	// the same bearer, and says no window of its own for [besideHello]'s reason.
	fleet := newEngineFleet(door.host, client, shutBoot, func(ask engineAsk) (*engineConn, error) {
		beside, shut, err := door.attach(ctx, id, remote.Hello{
			Workspace: "",
			Session:   ask.session,
			New:       ask.mint,
			Model:     launch.model,
			Level:     launch.level,
		}, true)
		if err != nil {
			return nil, err
		}
		return &engineConn{client: beside, shut: shut}, nil
	})
	closeLink = fleet.closeAll
	options, _ := hostOptions(fleet, client.Welcome(), launch.pick)
	options = orgSurfaceOptions(options)
	options.Restart = launch.restart
	return runSurface(context.Background(), options)
}
