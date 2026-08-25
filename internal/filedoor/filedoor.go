// Package filedoor is the loopback door: a 127.0.0.1 HTTP listener, owned by
// one chat surface, that turns files on the FAR machine into things this
// machine's own programs can open — a browser tab, cmd+click on an OSC 8 link,
// the platform opener.
//
// ── CAPABILITIES, NOT PATHS ─────────────────────────────────────────────────
//
// A file URL is /f/<id>, where the id is minted here and maps to a path in a
// table nobody else can write. The door cannot be asked for an arbitrary path:
// a local process that guesses URLs can reach only what the surface itself
// chose to link, and each id dies with the door. The browse page's token is
// the same idea for the listing side: /browse/<token> and /api/... require the
// one token minted at Open, so another local user's curl gets 404, never a
// listing.
//
// A WRONG TOKEN AND AN UNKNOWN ID BOTH ANSWER 404, ALWAYS THE SAME 404. A
// distinct status for "the id is real but your token is wrong" would confirm
// existence to somebody who is guessing, and a door whose refusals are
// informative is a door with a side channel in it.
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

	// mutex guards everything a request can read while Close is running, which
	// is all of the capability state: an id table, the path index that keeps
	// [Door.FileURL] idempotent, and the token itself. Close empties all three,
	// so a request that races the shutdown finds nothing rather than a stale
	// capability.
	mutex  sync.Mutex
	token  string
	byID   map[string]string
	byPath map[string]string
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
	door := &Door{
		source:   source,
		listener: listener,
		origin:   "http://" + listener.Addr().String(),
		token:    token,
		byID:     map[string]string{},
		byPath:   map[string]string{},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/f/", door.serveFile)
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
	}
	return d.origin + "/f/" + id, nil
}

// BrowseURL is the browse page's own address, token included.
func (d *Door) BrowseURL() string {
	d.mutex.Lock()
	defer d.mutex.Unlock()
	if d.closed {
		return ""
	}
	return d.origin + "/browse/" + d.token
}

// Close stops the listener and forgets every id and the token.
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
// hex. Both the token and every file id come out of it, because they are the
// same kind of thing — an unguessable name for a capability.
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
	w.Header().Set("Content-Type", kind)
	name := file.Name
	if name == "" {
		name = path.Base(target)
	}
	// Inline, because VIEWING IS THE PRODUCT: an image, a log, a PDF should
	// appear in the tab the person just opened. The browser still saves what it
	// cannot render, so "download" needs no separate route.
	w.Header().Set("Content-Disposition", mime.FormatMediaType("inline", map[string]string{"filename": name}))
	// ServeContent rather than a plain Write so a range request works — a video
	// scrubbed in the tab, a PDF viewer asking for its last page first. The
	// modification time is left zero on purpose: the door is not a cache, and
	// an id that outlives an edit on the far machine must not be revalidated
	// against a stamp this side invented.
	http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(file.Bytes))
}

// serveBrowse hands out the page itself, once the token checks out.
func (d *Door) serveBrowse(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimPrefix(r.URL.Path, "/browse/")
	if !d.authorised(strings.TrimSuffix(token, "/")) {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// The page carries its own token so every call it makes afterwards is
	// authorised by having been handed this page, and nothing on it has to ask
	// the person for a secret they never chose.
	if err := browsePage.Execute(w, browseData{
		Host:     d.source.Host(),
		Token:    strings.TrimSuffix(token, "/"),
		MaxBytes: maxCrossBytes,
	}); err != nil {
		// The header is already out by now, so there is nowhere honest to put
		// this; the truncated page is its own report.
		return
	}
}

// serveAPI routes the three calls the page makes. The token is a path segment
// rather than a header because the page is plain HTML with no fetch wrapper
// worth the name, and a URL the page can build with string concatenation is one
// less moving part between here and a listing.
func (d *Door) serveAPI(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/")
	token, call, found := strings.Cut(rest, "/")
	if !found || !d.authorised(token) {
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

// listing is what /api/<token>/ls answers with: the path as the source resolved
// it, the rows, and whether the source stopped counting.
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
// saw in a listing and is redirected to /f/<id>. The page could not be given
// the power to name a path on /f/ without giving it to everything else on this
// machine too, so the mint stays behind the token and the byte lane stays the
// one it was.
func (d *Door) serveMint(w http.ResponseWriter, r *http.Request) {
	where := r.URL.Query().Get("path")
	if where == "" {
		http.NotFound(w, r)
		return
	}
	url, err := d.FileURL(where)
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
