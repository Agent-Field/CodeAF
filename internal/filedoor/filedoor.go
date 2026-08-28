// Package filedoor is the loopback door: a 127.0.0.1 HTTP listener, owned by
// one chat surface, that turns files on the FAR machine into things this
// machine's own programs can open — a browser tab, cmd+click on an OSC 8 link,
// the platform opener.
//
// ── CAPABILITIES, NOT PATHS ─────────────────────────────────────────────────
//
// A byte URL is /f/<id> and an interactive-open URL is /o/<id>; their shared id
// maps to a path in a table nobody else can write. The door cannot be asked for an arbitrary path:
// a local process that guesses URLs can reach only what the surface itself
// chose to link, and each id dies with the door. The browse page's token is
// the same idea for the listing side: /browse/ and /api/... require the one
// token minted at Open, so another local user's curl gets 404, never a listing.
//
// A WRONG TOKEN AND AN UNKNOWN ID BOTH ANSWER 404, ALWAYS THE SAME 404. A
// distinct status for "the id is real but your token is wrong" would confirm
// existence to somebody who is guessing, and a door whose refusals are
// informative is a door with a side channel in it.
//
// ── THE TOKEN IS NEVER IN AN ARGV ───────────────────────────────────────────
//
// [Door.BrowseURL] is handed to the platform opener, and the platform opener is
// exec: whatever is in that address is in /proc/<pid>/cmdline, which under the
// default hidepid every other account on this machine can read, and after that
// it is in the browser's history and in a Referer header. So the address the
// door hands out CARRIES NO TOKEN AT ALL. It is /enter/<nonce>: a single-use
// name, good once and briefly, that is spent the moment a browser walks it. In
// exchange the door sets the real token as an HttpOnly SameSite=Strict cookie
// and redirects to /browse/, with nothing secret left in the address bar. After
// that the secret lives in exactly two places — this process's memory and one
// browser's cookie jar — and no other account on the machine can read either.
//
// The path lane (/browse/<token>, /api/<token>/ls) still authorises, because a
// person holding the token and curl is the oldest way through this door and the
// simplest way to test it. Where both are offered the cookie wins.
//
// ── THE DOOR SERVES, THE SOURCE DECIDES ─────────────────────────────────────
//
// Every byte and every listing comes through [Source], which is the engine's
// law speaking (internal/remote's handOver two-roots rule). The door adds no
// judgement of its own about what may be shown: a path the source refuses is a
// sentence passed through verbatim, exactly as the surface passes the engine's
// refusals through today. That is why a refusal leaves here as 502 with the
// sentence as its whole body — the door has nothing to add to it, and rewriting
// somebody else's refusal is how a message stops being true.
//
// ── AND EVERY BYTE LEAVES SANDBOXED ─────────────────────────────────────────
//
// THE FILES ARE NOT OURS. They came off somebody else's disk, quite possibly
// written by a model, and they are served on the same 127.0.0.1 port that holds
// the capability to list that disk and to write to it. A far machine's .html or
// .svg rendered on this origin would be a document executing next to the keys:
// it could read the token out of a referrer, call /api/ls, GET every file it
// found and POST the lot anywhere. So /f/<id> answers under
// `Content-Security-Policy: sandbox`, which drops the response into an opaque
// origin where it is same-origin with nothing, and the kinds a browser actually
// executes are additionally sent as an attachment rather than rendered. What a
// person came to look at — a picture, a log, a PDF — stays inline, because
// viewing is the product.
//
// ── UPLOADS LAND IN attachments/ AND NOWHERE ELSE ───────────────────────────
//
// Dragging a file onto the browse page sends it up the same lane /attach uses,
// and it lands where an attachment lands: the far session's attachments/
// folder. The door never writes an arbitrary remote path — moving a file INTO
// the workspace proper is the conversation's job ("put attachments/x.csv next
// to the others"), because that is a write on somebody's machine and writes
// belong to the lane that already owns consent for them.
package filedoor

import (
	"bytes"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/guard"
)

// maxCrossBytes is the most one file may weigh in either direction, and it is
// the engine's own ceiling (internal/remote's maxFetchBytes) restated on the
// side of the wire that can refuse a person's drag before it costs them the
// upload. The browse page reads this number out of the template rather than
// spelling 16MB in its own JavaScript, because a limit written twice is a limit
// that drifts.
const maxCrossBytes = 16 << 20

// multipartSlack is the room a multipart envelope needs on top of the payload
// itself — boundaries, part headers, the filename. It exists so the body reader
// stops a genuinely oversized upload without tripping on a legal one that
// happens to sit right on the ceiling.
const multipartSlack = 1 << 20

// entryLife is how long an entry nonce is worth anything. It is minted the
// moment the surface asks for an address and walked seconds later by the
// browser that address was handed to, so the only thing a longer life buys is a
// wider window for somebody who read the address off a screen.
const entryLife = 5 * time.Minute

// maxOpenEntries is how many unspent nonces a door will hold. Asking for the
// page four times and walking none of the addresses is a person changing their
// mind, not a reason for a table to grow: past this the one closest to expiry
// goes.
const maxOpenEntries = 16

// maxLinks is how many file capabilities one door holds at once. Every redraw
// of every line that names a file mints into this table (see [Door.FileURL]),
// so a long session with a busy scrollback would otherwise grow it for as long
// as the surface lives. Four thousand is far more than a person clicks and
// small enough to be nothing.
const maxLinks = 4096

// Entry is one row of a remote listing, the door's own shape so the package
// depends on internal/remote only through [Source]'s implementor.
type Entry struct {
	Name    string
	Dir     bool
	Size    int64
	ModTime time.Time
	MIME    string
}

// File is one fetched file: its bytes and what to call them on the wire out.
type File struct {
	Name  string
	MIME  string
	Bytes []byte
}

// Source is where every listing and every byte comes from. The chat surface
// implements it over its remote client; a test implements it over a map.
type Source interface {
	// Host is the far machine's name as the person typed it — the browse
	// page's title, so a person with three doors open knows whose disk this is.
	Host() string
	// List returns one directory under the source's own law.
	List(path string) (resolved string, entries []Entry, truncated bool, err error)
	// Fetch returns one file under the same law.
	Fetch(path string) (File, error)
	// Deposit sends bytes to the far session's attachments folder and returns
	// the path they landed at, on the engine's disk.
	Deposit(name string, data []byte) (landed string, err error)
}

// Door is one running listener. Zero value is not usable; Open makes one.
type Door struct {
	source   Source
	listener net.Listener
	server   *http.Server
	origin   string
	// cookie is the name the token is kept under in the browser, and IT CARRIES
	// THIS DOOR'S PORT because a cookie jar does not: it keys on host alone, so
	// two doors open at once on 127.0.0.1 under one name would trample each
	// other's token and the older tab would start answering 404 for no reason a
	// person could see. Set once at Open and never written again.
	cookie string

	// mutex guards everything a request can read while Close is running, which
	// is all of the capability state: an id table, the path index that keeps
	// [Door.FileURL] idempotent, the unspent entry nonces, and the token
	// itself. Close empties all of them, so a request that races the shutdown
	// finds nothing rather than a stale capability.
	mutex  sync.Mutex
	token  string
	byID   map[string]string
	byPath map[string]string
	// order is the ids in the order they were minted, which is the only thing
	// [Door.forgetOldestLinks] needs to know to drop the right one.
	order  []string
	nonces map[string]time.Time
	closed bool
}

// Open starts the door on an OS-chosen 127.0.0.1 port and mints its token.
func Open(source Source) (*Door, error) {
	if source == nil {
		return nil, errors.New("filedoor: a door needs a source")
	}
	// 127.0.0.1 AND NEVER 0.0.0.0. The whole capability argument above assumes
	// the only processes that can reach this listener are processes already on
	// this machine as this person; a door bound to every interface hands the
	// far machine's files to the coffee shop.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("filedoor: %w", err)
	}
	token, err := mint()
	if err != nil {
		_ = listener.Close()
		return nil, err
	}
	address := listener.Addr().String()
	name := "aforge_door"
	if _, port, err := net.SplitHostPort(address); err == nil && port != "" {
		name += "_" + port
	}
	door := &Door{
		source:   source,
		listener: listener,
		origin:   "http://" + address,
		cookie:   name,
		token:    token,
		byID:     map[string]string{},
		byPath:   map[string]string{},
		nonces:   map[string]time.Time{},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/f/", door.serveFile)
	mux.HandleFunc("/o/", door.serveOpen)
	mux.HandleFunc("/enter/", door.serveEnter)
	mux.HandleFunc("/browse/", door.serveBrowse)
	mux.HandleFunc("/api/", door.serveAPI)
	// Anything else is not a door at all, and says so the same way a wrong
	// token does.
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) })
	door.server = &http.Server{
		Handler: mux,
		// A header read that never finishes would otherwise hold a goroutine
		// for the surface's whole lifetime; the body itself is deliberately
		// untimed, because an upload over a slow link is a legitimate slow body.
		ReadHeaderTimeout: 10 * time.Second,
	}
	guard.Go("filedoor/serve", func() { _ = door.server.Serve(listener) })
	return door, nil
}

// FileURL mints (or reuses) the capability URL for one remote path, for OSC 8
// links and for the open flow. Idempotent per path for the door's lifetime.
//
// Idempotence is not tidiness: the surface calls this every time it redraws a
// line that names a file, so a fresh id per call would grow the table by one
// entry per repaint and give the same file a different URL in every scrollback
// line — which is exactly the sort of thing that makes a person believe a link
// they clicked once has stopped working.
func (d *Door) FileURL(path string) (string, error) {
	if path == "" {
		return "", errors.New("filedoor: a link needs a path")
	}
	d.mutex.Lock()
	defer d.mutex.Unlock()
	if d.closed {
		return "", errors.New("filedoor: the door is closed")
	}
	id, known := d.byPath[path]
	if !known {
		minted, err := mint()
		if err != nil {
			return "", err
		}
		id = minted
		d.byID[id] = path
		d.byPath[path] = id
		d.order = append(d.order, id)
		d.forgetOldestLinks()
	}
	return d.origin + "/f/" + id, nil
}

// OpenURL mints the capability used by an interactive click. It is separate
// from FileURL because callers which asked for bytes must keep receiving bytes,
// while a terminal click asks the surface OS to open its cached named copy.
func (d *Door) OpenURL(path string) (string, error) {
	url, err := d.FileURL(path)
	if err != nil {
		return "", err
	}
	return strings.Replace(url, "/f/", "/o/", 1), nil
}

// forgetOldestLinks keeps the id table under [maxLinks] by dropping the ids
// minted longest ago. THE OLDEST LINK IS THE RIGHT ONE TO LOSE: it is the one
// furthest up a scrollback nobody is scrolling to any more, and a forgotten id
// answers the same 404 an id nobody minted answers — the door's existing idea
// of a capability that has ended, not a new kind of failure. Caller holds the
// mutex.
func (d *Door) forgetOldestLinks() {
	for len(d.order) > maxLinks {
		oldest := d.order[0]
		d.order = d.order[1:]
		target, known := d.byID[oldest]
		if !known {
			continue
		}
		delete(d.byID, oldest)
		if d.byPath[target] == oldest {
			delete(d.byPath, target)
		}
	}
}

// BrowseURL is the address to hand a browser, and THERE IS NO TOKEN IN IT. It
// is /enter/<nonce>, spent once by whichever browser walks it first; see the
// package header for why a token that reaches an argv is a token another
// account on this machine already has.
func (d *Door) BrowseURL() string {
	nonce, err := mint()
	if err != nil {
		return ""
	}
	now := time.Now()
	d.mutex.Lock()
	defer d.mutex.Unlock()
	if d.closed || d.token == "" {
		return ""
	}
	for old, deadline := range d.nonces {
		if now.After(deadline) {
			delete(d.nonces, old)
		}
	}
	for len(d.nonces) >= maxOpenEntries {
		var oldest string
		var soonest time.Time
		for candidate, deadline := range d.nonces {
			if oldest == "" || deadline.Before(soonest) {
				oldest, soonest = candidate, deadline
			}
		}
		delete(d.nonces, oldest)
	}
	d.nonces[nonce] = now.Add(entryLife)
	return d.origin + "/enter/" + nonce
}

// Close stops the listener and forgets every id, every unspent nonce, and the
// token.
func (d *Door) Close() error {
	d.mutex.Lock()
	if d.closed {
		d.mutex.Unlock()
		return nil
	}
	d.closed = true
	d.token = ""
	d.byID = map[string]string{}
	d.byPath = map[string]string{}
	d.order = nil
	d.nonces = map[string]time.Time{}
	server := d.server
	d.mutex.Unlock()
	if server == nil {
		return nil
	}
	// Close, not Shutdown: a door dies with the surface that owned it, and a
	// graceful drain would mean the surface's exit waits on a browser tab
	// somebody left open on the other side of the room.
	if err := server.Close(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// mint is the one place a secret is made here: sixteen bytes of crypto/rand as
// hex. The token, every file id and every entry nonce come out of it, because
// they are the same kind of thing — an unguessable name for a capability.
func mint() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("filedoor: %w", err)
	}
	return hex.EncodeToString(raw), nil
}

// pathFor answers the path behind a minted id, and reports miss for an id this
// door never minted or has since forgotten.
func (d *Door) pathFor(id string) (string, bool) {
	d.mutex.Lock()
	defer d.mutex.Unlock()
	if d.closed {
		return "", false
	}
	target, known := d.byID[id]
	return target, known
}

// authorised compares a request's token against the door's in constant time.
// A closed door authorises nothing, which is what makes Close's forgetting real
// rather than cosmetic.
func (d *Door) authorised(offered string) bool {
	d.mutex.Lock()
	defer d.mutex.Unlock()
	if d.closed || d.token == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(offered), []byte(d.token)) == 1
}

// carriesToken answers whether this request arrived with the door's cookie.
func (d *Door) carriesToken(r *http.Request) bool {
	cookie, err := r.Cookie(d.cookie)
	if err != nil {
		return false
	}
	return d.authorised(cookie.Value)
}

// admitted is the one question the listing lanes ask: may this request have the
// door's authority? The cookie is asked first because it is the lane a browser
// uses and the lane that keeps the secret out of every address; the token in
// the path is still honoured underneath it, for curl and for the tests.
func (d *Door) admitted(r *http.Request, offered string) bool {
	if d.carriesToken(r) {
		return true
	}
	return offered != "" && d.authorised(offered)
}

// spend consumes one entry nonce and answers the token it buys. A nonce is
// deleted whether or not it was still in time, because a name that has been
// tried is a name that has been seen.
func (d *Door) spend(nonce string) (string, bool) {
	if nonce == "" || strings.Contains(nonce, "/") {
		return "", false
	}
	d.mutex.Lock()
	defer d.mutex.Unlock()
	if d.closed || d.token == "" {
		return "", false
	}
	deadline, known := d.nonces[nonce]
	delete(d.nonces, nonce)
	if !known || time.Now().After(deadline) {
		return "", false
	}
	return d.token, true
}

// handToken puts the token where only this browser and this process can read
// it. HttpOnly keeps it away from script — including script that arrived as
// somebody else's file — SameSite=Strict keeps another site's navigation from
// spending it, and Path=/ covers both lanes the page uses. It is not marked
// Secure because a Secure cookie over plain http is a cookie the browser
// throws away, and this listener is http on loopback by design.
func (d *Door) handToken(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     d.cookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
}

// serveEnter is the doorway [Door.BrowseURL] hands out: a nonce is spent, the
// cookie is set, and the browser is sent on to an address with no secret in it.
//
// A browser that has already been through here is let past without a nonce,
// because the surface prints that same address in its own scrollback for the
// person to click when the platform opener could not — and the second click
// must not be a 404 the person has no way to act on. Nothing is given away by
// it: a request carrying the cookie already holds everything the door has.
func (d *Door) serveEnter(w http.ResponseWriter, r *http.Request) {
	nonce := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/enter/"), "/")
	if token, spent := d.spend(nonce); spent {
		d.handToken(w, token)
	} else if !d.carriesToken(r) {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Referrer-Policy", "no-referrer")
	http.Redirect(w, r, "/browse/", http.StatusFound)
}

// serveBrowse hands out the page itself, once the request checks out.
func (d *Door) serveBrowse(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/browse/"), "/")
	if !d.admitted(r, token) {
		http.NotFound(w, r)
		return
	}
	// Somebody who arrived the old way — the token in the address, from curl or
	// from a test — leaves here holding the cookie too, so the page has ONE way
	// of authorising itself and not two.
	if token != "" {
		d.handToken(w, token)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// The page is one file and reaches nowhere (browse.go says why). This tells
	// the browser the same thing, so a helpful CDN line added later fails loudly
	// instead of quietly working, and so nothing on this page can be talked into
	// posting a listing somewhere else.
	w.Header().Set("Content-Security-Policy",
		"default-src 'none'; style-src 'unsafe-inline'; script-src 'unsafe-inline'; img-src 'self'; connect-src 'self'; form-action 'none'; base-uri 'none'")
	// No Referer leaves this page, ever. The address may hold nothing secret
	// any more, but a page that names a private workspace should not announce
	// itself to whatever a person navigates to next either.
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if err := browsePage.Execute(w, browseData{
		Host:     d.source.Host(),
		MaxBytes: maxCrossBytes,
	}); err != nil {
		// The header is already out by now, so there is nowhere honest to put
		// this; the truncated page is its own report.
		return
	}
}

// serveAPI routes the three calls the page makes. The page itself calls them
// with no token in the address at all and is authorised by its cookie; the
// older shape, /api/<token>/ls, still works, because a person with the token
// and curl is a lane this door has always had.
func (d *Door) serveAPI(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/")
	token, call, found := strings.Cut(rest, "/")
	if !found {
		token, call = "", rest
	}
	if !d.admitted(r, token) {
		http.NotFound(w, r)
		return
	}
	switch call {
	case "ls":
		d.serveList(w, r)
	case "file":
		d.serveMint(w, r)
	case "put":
		d.servePut(w, r)
	default:
		http.NotFound(w, r)
	}
}

// serveFile is the ONLY lane bytes leave by. Everything else — the browse
// page's click, the surface's opener — mints an id and comes back through here,
// so there is one place that decides what a byte on this port costs.
func (d *Door) serveFile(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/f/")
	if id == "" || strings.Contains(id, "/") {
		http.NotFound(w, r)
		return
	}
	target, known := d.pathFor(id)
	if !known {
		http.NotFound(w, r)
		return
	}
	file, err := d.source.Fetch(target)
	if err != nil {
		refuse(w, err)
		return
	}
	kind := strings.TrimSpace(file.MIME)
	if kind == "" {
		// Not a guess dressed as knowledge: octet-stream is the honest "I do
		// not know what this is", and the browser will offer to save it.
		kind = "application/octet-stream"
	}
	head := w.Header()
	// THE SANDBOX IS THE FIX, and it goes on every response without exception.
	// An empty sandbox directive puts what follows in an opaque origin: no
	// script, no forms, and — the point — not same-origin with /browse/ or
	// /api/, so a document off the far machine cannot read this port's token or
	// spend it. It costs nothing for a picture or a log. A browser that would
	// rather save a sandboxed PDF than draw it saves a PDF, which is a
	// survivable outcome; a document that can read the keys is not.
	head.Set("Content-Security-Policy", "sandbox")
	// The kind the source named is the kind it is. Sniffing is how a text file
	// full of angle brackets becomes a page.
	head.Set("X-Content-Type-Options", "nosniff")
	// Nothing this response does may carry where it came from.
	head.Set("Referrer-Policy", "no-referrer")
	head.Set("Content-Type", kind)
	name := file.Name
	if name == "" {
		name = path.Base(target)
	}
	// Inline where VIEWING IS THE PRODUCT: an image, a log, a PDF should appear
	// in the tab the person just opened. The kinds a browser executes are sent
	// as an attachment instead, so they are saved rather than run — belt as well
	// as braces, since the sandbox above has already taken their teeth out.
	disposition := "inline"
	if !viewable(kind) {
		disposition = "attachment"
	}
	head.Set("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": name}))
	// ServeContent rather than a plain Write so a range request works — a video
	// scrubbed in the tab, a PDF viewer asking for its last page first. The
	// modification time is left zero on purpose: the door is not a cache, and
	// an id that outlives an edit on the far machine must not be revalidated
	// against a stamp this side invented.
	http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(file.Bytes))
}

// serveOpen turns an OSC-8 click into a local viewer handoff. The URL is
// reached by the terminal on the surface machine, so opening the cached copy
// here preserves which machine owns both the bytes and the screen.
func (d *Door) serveOpen(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/o/")
	if id == "" || strings.Contains(id, "/") {
		http.NotFound(w, r)
		return
	}
	target, known := d.pathFor(id)
	if !known {
		http.NotFound(w, r)
		return
	}
	opener, ok := d.source.(interface{ Open(string) error })
	if !ok {
		http.NotFound(w, r)
		return
	}
	if err := opener.Open(target); err != nil {
		refuse(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = fmt.Fprintf(w, "%s opened from %s\n", path.Base(target), d.source.Host())
}

// viewable answers whether a kind is one the door will let a browser RENDER
// rather than save. IT IS AN ALLOWLIST AND NOT A LIST OF THE DANGEROUS ONES,
// because the dangerous list is whatever a browser learns to execute next while
// the safe list is short and stays short: pictures, words, sound, moving
// pictures, a document. An SVG is not a picture for this purpose — it is a
// program with a canvas — and neither is anything XML enough to be one.
func viewable(kind string) bool {
	media, _, err := mime.ParseMediaType(kind)
	if err != nil {
		return false
	}
	media = strings.ToLower(strings.TrimSpace(media))
	switch media {
	case "image/svg+xml", "image/svg", "text/html", "text/xml", "application/xhtml+xml", "application/xml":
		return false
	case "application/pdf":
		return true
	}
	switch {
	case strings.HasPrefix(media, "image/"),
		strings.HasPrefix(media, "text/"),
		strings.HasPrefix(media, "audio/"),
		strings.HasPrefix(media, "video/"):
		return true
	}
	return false
}

// listing is what /api/ls answers with: the path as the source resolved it, the
// rows, and whether the source stopped counting.
type listing struct {
	Path      string    `json:"path"`
	Entries   []listRow `json:"entries"`
	Truncated bool      `json:"truncated"`
}

// listRow is [Entry] on the wire. The time is a string rather than a stamp so
// the page can print an empty cell for a file whose time nobody knows, which is
// the emptiness law: unknown renders as nothing, never as 1970.
type listRow struct {
	Name  string `json:"name"`
	Dir   bool   `json:"dir"`
	Size  int64  `json:"size"`
	MTime string `json:"mtime"`
	MIME  string `json:"mime"`
}

func (d *Door) serveList(w http.ResponseWriter, r *http.Request) {
	where := r.URL.Query().Get("path")
	if where == "" {
		where = "."
	}
	resolved, entries, truncated, err := d.source.List(where)
	if err != nil {
		refuse(w, err)
		return
	}
	answer := listing{Path: resolved, Entries: make([]listRow, 0, len(entries)), Truncated: truncated}
	for _, entry := range entries {
		row := listRow{Name: entry.Name, Dir: entry.Dir, Size: entry.Size, MIME: entry.MIME}
		if !entry.ModTime.IsZero() {
			row.MTime = entry.ModTime.Format(time.RFC3339)
		}
		answer.Entries = append(answer.Entries, row)
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(answer)
}

// serveMint is how the page opens a file: it asks for an id for a path it just
// saw in a listing and is redirected to /o/<id>. The page could not be given
// the power to name a path on /f/ without giving it to everything else on this
// machine too, so the mint stays behind the token and the byte lane stays the
// one it was.
func (d *Door) serveMint(w http.ResponseWriter, r *http.Request) {
	where := r.URL.Query().Get("path")
	if where == "" {
		http.NotFound(w, r)
		return
	}
	url, err := d.OpenURL(where)
	if err != nil {
		refuse(w, err)
		return
	}
	http.Redirect(w, r, url, http.StatusFound)
}

func (d *Door) servePut(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "an upload is a POST", http.StatusMethodNotAllowed)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxCrossBytes+multipartSlack)
	reader, err := r.MultipartReader()
	if err != nil {
		http.Error(w, "that upload did not arrive as a form", http.StatusBadRequest)
		return
	}
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			http.Error(w, "that upload did not arrive whole", http.StatusBadRequest)
			return
		}
		if part.FormName() != "file" {
			_ = part.Close()
			continue
		}
		name := path.Base(part.FileName())
		if name == "" || name == "." || name == "/" {
			name = "upload"
		}
		// One byte past the ceiling is enough to know, and it means an
		// oversized drop is refused after 16MB rather than after however many
		// gigabytes the person actually dropped.
		data, err := io.ReadAll(io.LimitReader(part, maxCrossBytes+1))
		_ = part.Close()
		if err != nil {
			http.Error(w, "that upload did not arrive whole", http.StatusBadRequest)
			return
		}
		if len(data) > maxCrossBytes {
			// The engine's sentence, said on this side of the wire so the
			// person hears it before the bytes are spent.
			http.Error(w, fmt.Sprintf("%s is bigger than %dMB and the most one file may cross this connection is %dMB",
				name, maxCrossBytes>>20, maxCrossBytes>>20), http.StatusRequestEntityTooLarge)
			return
		}
		landed, err := d.source.Deposit(name, data)
		if err != nil {
			refuse(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]string{"landed": landed})
		return
	}
	http.Error(w, "that upload carried no file", http.StatusBadRequest)
}

// refuse passes a source's refusal through with nothing added. 502 because the
// door is a gateway and the answer came from the machine behind it: the failure
// is not this listener's, and the sentence is not this listener's to reword.
func refuse(w http.ResponseWriter, err error) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusBadGateway)
	_, _ = io.WriteString(w, err.Error())
}
