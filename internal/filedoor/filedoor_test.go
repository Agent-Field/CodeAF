package filedoor

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

// The tests run against the REAL listener rather than httptest's handler
// harness, because half of what this package promises is about the socket: that
// it is on 127.0.0.1, that a wrong token gets the same 404 an unknown id gets,
// that Close actually ends the serving. A handler called directly proves none
// of those.

// fakeSource is a workspace in a map. It is deliberately dumber than the engine
// — it has no two-roots law of its own — because the door is supposed to add no
// law, and a fake with opinions would hide the door's own behaviour behind
// them.
type fakeSource struct {
	host      string
	files     map[string]File
	dirs      map[string][]Entry
	truncated map[string]bool
	refuse    map[string]string

	mutex    sync.Mutex
	deposits map[string][]byte
	opened   string
}

func newFake() *fakeSource {
	return &fakeSource{
		host:      "roadhouse",
		files:     map[string]File{},
		dirs:      map[string][]Entry{},
		truncated: map[string]bool{},
		refuse:    map[string]string{},
		deposits:  map[string][]byte{},
	}
}

func (f *fakeSource) Host() string { return f.host }

func (f *fakeSource) List(path string) (string, []Entry, bool, error) {
	if sentence, no := f.refuse[path]; no {
		return "", nil, false, errors.New(sentence)
	}
	entries, known := f.dirs[path]
	if !known {
		return "", nil, false, fmt.Errorf("engine: %s is not a directory this session may show", path)
	}
	resolved := "/far/workspace"
	if path != "." && path != "" {
		resolved += "/" + strings.TrimPrefix(path, "./")
	}
	return resolved, entries, f.truncated[path], nil
}

func (f *fakeSource) Fetch(path string) (File, error) {
	if sentence, no := f.refuse[path]; no {
		return File{}, errors.New(sentence)
	}
	file, known := f.files[path]
	if !known {
		return File{}, fmt.Errorf("engine: %s is not a file this session may hand over", path)
	}
	return file, nil
}

func (f *fakeSource) Open(path string) error {
	if sentence, no := f.refuse[path]; no {
		return errors.New(sentence)
	}
	f.opened = path
	return nil
}

func (f *fakeSource) Deposit(name string, data []byte) (string, error) {
	if sentence, no := f.refuse[name]; no {
		return "", errors.New(sentence)
	}
	f.mutex.Lock()
	defer f.mutex.Unlock()
	f.deposits[name] = data
	return "attachments/" + name, nil
}

func (f *fakeSource) landed(name string) ([]byte, bool) {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	data, known := f.deposits[name]
	return data, known
}

// openDoor gives a test a door that closes itself, and the client every case
// here uses: one that does NOT follow redirects, so a case can look at the 302
// the mint route answers with before deciding whether to walk it.
func openDoor(t *testing.T, source Source) (*Door, *http.Client) {
	t.Helper()
	door, err := Open(source)
	if err != nil {
		t.Fatalf("opening the door: %v", err)
	}
	t.Cleanup(func() { _ = door.Close() })
	client := &http.Client{
		Timeout:       5 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	return door, client
}

// origin is the door's own base URL, taken from the door itself rather than
// from an address it hands out: the address it hands out is an entry nonce now,
// and reading a base URL out of it would spend one every time a test wanted a
// hostname.
func origin(t *testing.T, door *Door) string {
	t.Helper()
	door.mutex.Lock()
	defer door.mutex.Unlock()
	return door.origin
}

// token reaches into the door for the secret itself. A test needs it for the
// path lane — curl's lane — which the door still honours, and to prove the
// addresses it hands out do NOT contain it.
func token(t *testing.T, door *Door) string {
	t.Helper()
	door.mutex.Lock()
	defer door.mutex.Unlock()
	return door.token
}

// walkIn does what a browser does with the address BrowseURL hands out: it
// spends the entry nonce and comes back holding the cookie the door set.
func walkIn(t *testing.T, door *Door, client *http.Client) *http.Cookie {
	t.Helper()
	answer, err := client.Get(door.BrowseURL())
	if err != nil {
		t.Fatalf("walking in: %v", err)
	}
	defer answer.Body.Close()
	if answer.StatusCode != http.StatusFound {
		t.Fatalf("the entry answered %d, wanted a 302 onto the page", answer.StatusCode)
	}
	for _, cookie := range answer.Cookies() {
		if cookie.Value != "" {
			return cookie
		}
	}
	t.Fatal("walking in set no cookie, so the page has no way to authorise itself")
	return nil
}

func TestTheDoorBindsLoopbackAndNothingElse(t *testing.T) {
	door, _ := openDoor(t, newFake())
	parsed, err := url.Parse(door.BrowseURL())
	if err != nil {
		t.Fatalf("the browse URL did not parse: %v", err)
	}
	host, port, err := net.SplitHostPort(parsed.Host)
	if err != nil {
		t.Fatalf("the browse URL has no host:port: %v", err)
	}
	if host != "127.0.0.1" {
		t.Fatalf("the door is bound to %q; a door onto somebody's far disk is loopback only", host)
	}
	if port == "0" || port == "" {
		t.Fatalf("the door reports port %q, so nothing can reach it", port)
	}
	if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
		t.Fatalf("%q is not a loopback address", host)
	}
}

func TestAMintedURLServesTheBytesAndTheirKind(t *testing.T) {
	source := newFake()
	source.files["out/plot.png"] = File{Name: "plot.png", MIME: "image/png", Bytes: []byte("\x89PNG pretend")}
	door, client := openDoor(t, source)

	link, err := door.FileURL("out/plot.png")
	if err != nil {
		t.Fatalf("minting a link: %v", err)
	}
	answer, err := client.Get(link)
	if err != nil {
		t.Fatalf("walking the link: %v", err)
	}
	defer answer.Body.Close()
	if answer.StatusCode != http.StatusOK {
		t.Fatalf("the link answered %d, wanted 200", answer.StatusCode)
	}
	body, _ := io.ReadAll(answer.Body)
	if string(body) != "\x89PNG pretend" {
		t.Fatalf("the door served %q, wanted the source's bytes", body)
	}
	if got := answer.Header.Get("Content-Type"); got != "image/png" {
		t.Fatalf("Content-Type is %q, wanted the source's own MIME", got)
	}
	if got := answer.Header.Get("Content-Disposition"); !strings.HasPrefix(got, "inline") || !strings.Contains(got, "plot.png") {
		t.Fatalf("Content-Disposition is %q, wanted inline with the file's name", got)
	}
}

func TestAFileWithNoKindIsCalledOctetStream(t *testing.T) {
	source := newFake()
	source.files["notes"] = File{Name: "notes", Bytes: []byte("just words")}
	door, client := openDoor(t, source)

	link, _ := door.FileURL("notes")
	answer, err := client.Get(link)
	if err != nil {
		t.Fatalf("walking the link: %v", err)
	}
	defer answer.Body.Close()
	if got := answer.Header.Get("Content-Type"); got != "application/octet-stream" {
		t.Fatalf("Content-Type is %q, wanted the honest unknown", got)
	}
}

func TestAnUnknownIDIsFourOhFour(t *testing.T) {
	door, client := openDoor(t, newFake())
	answer, err := client.Get(origin(t, door) + "/f/0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatalf("asking for an id nobody minted: %v", err)
	}
	defer answer.Body.Close()
	if answer.StatusCode != http.StatusNotFound {
		t.Fatalf("an unminted id answered %d, wanted 404", answer.StatusCode)
	}
}

// The wrong token gets 404 on every lane, and the same 404 — never a 401, never
// a 403, because a status that distinguishes "no" from "not here" tells a
// guesser they are close.
func TestTheWrongTokenGetsTheSameNothingEverywhere(t *testing.T) {
	source := newFake()
	source.dirs["."] = []Entry{{Name: "README.md", Size: 10}}
	door, client := openDoor(t, source)
	base := origin(t, door)
	wrong := "ffffffffffffffffffffffffffffffff"

	for _, where := range []string{
		"/browse/" + wrong,
		"/api/" + wrong + "/ls?path=.",
		"/api/" + wrong + "/file?path=README.md",
	} {
		answer, err := client.Get(base + where)
		if err != nil {
			t.Fatalf("GET %s: %v", where, err)
		}
		answer.Body.Close()
		if answer.StatusCode != http.StatusNotFound {
			t.Fatalf("GET %s answered %d, wanted 404", where, answer.StatusCode)
		}
	}

	body, contentType := onePartForm(t, "sneak.txt", []byte("hello"))
	answer, err := client.Post(base+"/api/"+wrong+"/put", contentType, body)
	if err != nil {
		t.Fatalf("POST put with a wrong token: %v", err)
	}
	answer.Body.Close()
	if answer.StatusCode != http.StatusNotFound {
		t.Fatalf("put with a wrong token answered %d, wanted 404", answer.StatusCode)
	}
	if _, landed := source.landed("sneak.txt"); landed {
		t.Fatal("a wrong token still put a file on the far machine")
	}
}

func TestTheBrowsePageComesBackWholeAndAlone(t *testing.T) {
	door, client := openDoor(t, newFake())
	answer, err := client.Get(origin(t, door) + "/browse/" + token(t, door))
	if err != nil {
		t.Fatalf("asking for the browse page: %v", err)
	}
	defer answer.Body.Close()
	if answer.StatusCode != http.StatusOK {
		t.Fatalf("the browse page answered %d, wanted 200", answer.StatusCode)
	}
	page, _ := io.ReadAll(answer.Body)
	text := string(page)
	if !strings.Contains(text, "roadhouse — aforge files") {
		t.Fatal("the page does not say whose disk it is looking at")
	}
	// THE TOKEN IS NOT IN THE DOCUMENT. The page authorises itself with the
	// cookie the door set on the way in, so there is nothing here for a
	// rendered file, an extension or a saved copy of the page to read.
	if strings.Contains(text, token(t, door)) {
		t.Fatal("the page carries the token in its own body")
	}
	// NO EXTERNAL ASSETS. A door onto a private workspace does not open a
	// connection to anywhere else, and this is the test that keeps a helpful
	// CDN line from being added later.
	for _, forbidden := range []string{"http://", "https://", "//cdn", "src=\"/", "href=\"/"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("the page reaches outside itself: found %q", forbidden)
		}
	}
}

func TestListingArrivesAsTheSourceSawIt(t *testing.T) {
	source := newFake()
	when := time.Date(2026, 8, 24, 9, 30, 0, 0, time.UTC)
	source.dirs["."] = []Entry{
		{Name: "out", Dir: true, ModTime: when},
		{Name: "notes.md", Size: 412, ModTime: when, MIME: "text/markdown"},
	}
	source.truncated["."] = true
	door, client := openDoor(t, source)

	answer, err := client.Get(origin(t, door) + "/api/" + token(t, door) + "/ls?path=.")
	if err != nil {
		t.Fatalf("asking for a listing: %v", err)
	}
	defer answer.Body.Close()
	if answer.StatusCode != http.StatusOK {
		t.Fatalf("ls answered %d, wanted 200", answer.StatusCode)
	}
	var got listing
	if err := json.NewDecoder(answer.Body).Decode(&got); err != nil {
		t.Fatalf("the listing did not decode: %v", err)
	}
	if got.Path != "/far/workspace" {
		t.Fatalf("the listing names %q, wanted the path the source resolved", got.Path)
	}
	if !got.Truncated {
		t.Fatal("the source stopped counting and the listing did not say so")
	}
	if len(got.Entries) != 2 {
		t.Fatalf("the listing has %d rows, wanted 2", len(got.Entries))
	}
	if got.Entries[0].Name != "out" || !got.Entries[0].Dir {
		t.Fatalf("the first row is %+v, wanted the directory", got.Entries[0])
	}
	file := got.Entries[1]
	if file.Name != "notes.md" || file.Size != 412 || file.MIME != "text/markdown" {
		t.Fatalf("the file row is %+v, wanted the source's own numbers", file)
	}
	if file.MTime != when.Format(time.RFC3339) {
		t.Fatalf("the file's time is %q, wanted %q", file.MTime, when.Format(time.RFC3339))
	}
}

// An unknown time is nothing, not 1970: the emptiness law, on the wire.
func TestATimeNobodyKnowsIsNotDrawnAsNineteenSeventy(t *testing.T) {
	source := newFake()
	source.dirs["."] = []Entry{{Name: "mystery.bin", Size: 3}}
	door, client := openDoor(t, source)

	answer, err := client.Get(origin(t, door) + "/api/" + token(t, door) + "/ls?path=.")
	if err != nil {
		t.Fatalf("asking for a listing: %v", err)
	}
	defer answer.Body.Close()
	var got listing
	if err := json.NewDecoder(answer.Body).Decode(&got); err != nil {
		t.Fatalf("the listing did not decode: %v", err)
	}
	if got.Entries[0].MTime != "" {
		t.Fatalf("an unknown time rendered as %q, wanted nothing", got.Entries[0].MTime)
	}
}

func TestTheFileCallMintsAnIDAndSendsTheBrowserToIt(t *testing.T) {
	source := newFake()
	source.files["out/log.txt"] = File{Name: "log.txt", MIME: "text/plain; charset=utf-8", Bytes: []byte("the whole log")}
	door, client := openDoor(t, source)
	base := origin(t, door)

	answer, err := client.Get(base + "/api/" + token(t, door) + "/file?path=" + url.QueryEscape("out/log.txt"))
	if err != nil {
		t.Fatalf("asking for a file by path: %v", err)
	}
	answer.Body.Close()
	if answer.StatusCode != http.StatusFound {
		t.Fatalf("the file call answered %d, wanted a 302 onto the byte lane", answer.StatusCode)
	}
	where := answer.Header.Get("Location")
	if !strings.Contains(where, "/o/") {
		t.Fatalf("the redirect went to %q, wanted /o/<id>", where)
	}
	// The minted id is the SAME one OpenURL hands the surface, because both go
	// through the one path index.
	link, _ := door.OpenURL("out/log.txt")
	if !strings.HasSuffix(link, strings.TrimPrefix(where, base)) {
		t.Fatalf("the page's id (%q) and the surface's link (%q) disagree", where, link)
	}
	served, err := client.Get(base + strings.TrimPrefix(where, base))
	if err != nil {
		t.Fatalf("walking the redirect: %v", err)
	}
	defer served.Body.Close()
	if source.opened != "out/log.txt" {
		t.Fatalf("the redirect opened %q", source.opened)
	}
}

func TestAnUploadLandsAndTheAnswerSaysWhere(t *testing.T) {
	source := newFake()
	door, client := openDoor(t, source)

	body, contentType := onePartForm(t, "rows.csv", []byte("a,b\n1,2\n"))
	answer, err := client.Post(origin(t, door)+"/api/"+token(t, door)+"/put", contentType, body)
	if err != nil {
		t.Fatalf("sending a file over: %v", err)
	}
	defer answer.Body.Close()
	if answer.StatusCode != http.StatusOK {
		text, _ := io.ReadAll(answer.Body)
		t.Fatalf("put answered %d (%s), wanted 200", answer.StatusCode, strings.TrimSpace(string(text)))
	}
	var said struct {
		Landed string `json:"landed"`
	}
	if err := json.NewDecoder(answer.Body).Decode(&said); err != nil {
		t.Fatalf("the answer did not decode: %v", err)
	}
	if said.Landed != "attachments/rows.csv" {
		t.Fatalf("the answer says %q, wanted the path the source reported", said.Landed)
	}
	kept, landed := source.landed("rows.csv")
	if !landed {
		t.Fatal("the file never reached the source")
	}
	if string(kept) != "a,b\n1,2\n" {
		t.Fatalf("the source kept %q, wanted the bytes that were sent", kept)
	}
}

// A drop of something enormous is refused HERE, before the person spends the
// upload, and the sentence says the ceiling rather than a status code.
func TestAnUploadOverTheCeilingIsRefusedWithTheSentence(t *testing.T) {
	source := newFake()
	door, client := openDoor(t, source)

	body, contentType := onePartForm(t, "enormous.bin", bytes.Repeat([]byte("x"), maxCrossBytes+1))
	answer, err := client.Post(origin(t, door)+"/api/"+token(t, door)+"/put", contentType, body)
	if err != nil {
		t.Fatalf("sending something enormous: %v", err)
	}
	defer answer.Body.Close()
	if answer.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("an oversized upload answered %d, wanted 413", answer.StatusCode)
	}
	said, _ := io.ReadAll(answer.Body)
	if !strings.Contains(string(said), "16MB") || !strings.Contains(string(said), "enormous.bin") {
		t.Fatalf("the refusal reads %q, wanted the name and the ceiling", strings.TrimSpace(string(said)))
	}
	if _, landed := source.landed("enormous.bin"); landed {
		t.Fatal("something over the ceiling crossed anyway")
	}
}

// The source's refusal is the answer, word for word. The door is a gateway and
// has nothing to add to somebody else's no.
func TestARefusalCrossesVerbatim(t *testing.T) {
	source := newFake()
	source.files["secret"] = File{Name: "secret", Bytes: []byte("never")}
	source.refuse["secret"] = "engine: secret is outside the workspace and the session's own folder"
	door, client := openDoor(t, source)

	link, _ := door.FileURL("secret")
	answer, err := client.Get(link)
	if err != nil {
		t.Fatalf("walking a refused link: %v", err)
	}
	defer answer.Body.Close()
	if answer.StatusCode != http.StatusBadGateway {
		t.Fatalf("a refusal answered %d, wanted 502", answer.StatusCode)
	}
	said, _ := io.ReadAll(answer.Body)
	if strings.TrimSpace(string(said)) != source.refuse["secret"] {
		t.Fatalf("the refusal came out as %q, wanted it unchanged", strings.TrimSpace(string(said)))
	}
	if kind := answer.Header.Get("Content-Type"); !strings.HasPrefix(kind, "text/plain") {
		t.Fatalf("a refusal arrived as %q, wanted plain text a person can read", kind)
	}
}

func TestFileURLIsIdempotentPerPath(t *testing.T) {
	source := newFake()
	source.files["out/plot.png"] = File{Name: "plot.png", MIME: "image/png", Bytes: []byte("pretend")}
	source.files["out/other.png"] = File{Name: "other.png", MIME: "image/png", Bytes: []byte("pretend")}
	door, _ := openDoor(t, source)

	first, err := door.FileURL("out/plot.png")
	if err != nil {
		t.Fatalf("minting once: %v", err)
	}
	for i := 0; i < 5; i++ {
		again, err := door.FileURL("out/plot.png")
		if err != nil {
			t.Fatalf("minting again: %v", err)
		}
		if again != first {
			t.Fatalf("the same path minted %q and then %q; a redraw must not move a link", first, again)
		}
	}
	other, err := door.FileURL("out/other.png")
	if err != nil {
		t.Fatalf("minting a second path: %v", err)
	}
	if other == first {
		t.Fatal("two paths share one id, so an id is not a capability for one file")
	}
	if _, err := door.FileURL(""); err == nil {
		t.Fatal("a link with no path behind it was minted anyway")
	}
}

func TestOpenURLHandsTheConfirmedPathToTheSurfaceViewer(t *testing.T) {
	source := newFake()
	door, client := openDoor(t, source)
	link, err := door.OpenURL("out/report.md")
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Get(link)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("open answered %s", response.Status)
	}
	if source.opened != "out/report.md" {
		t.Fatalf("the viewer was handed %q", source.opened)
	}
}

func TestClosingEndsTheServingAndForgetsEverything(t *testing.T) {
	source := newFake()
	source.files["out/plot.png"] = File{Name: "plot.png", MIME: "image/png", Bytes: []byte("pretend")}
	door, err := Open(source)
	if err != nil {
		t.Fatalf("opening the door: %v", err)
	}
	client := &http.Client{Timeout: 5 * time.Second}
	link, err := door.FileURL("out/plot.png")
	if err != nil {
		t.Fatalf("minting a link: %v", err)
	}
	if answer, err := client.Get(link); err != nil {
		t.Fatalf("the door did not serve before it was closed: %v", err)
	} else {
		answer.Body.Close()
	}

	if err := door.Close(); err != nil {
		t.Fatalf("closing the door: %v", err)
	}
	if _, err := client.Get(link); err == nil {
		t.Fatal("the door still serves after Close")
	}
	if url := door.BrowseURL(); url != "" {
		t.Fatalf("a closed door still hands out %q", url)
	}
	if _, err := door.FileURL("out/plot.png"); err == nil {
		t.Fatal("a closed door still mints capabilities")
	}
	// Closing twice is what a surface tearing down twice does, and it must not
	// be an error either time.
	if err := door.Close(); err != nil {
		t.Fatalf("closing an already closed door: %v", err)
	}
}

func TestADoorNeedsASource(t *testing.T) {
	if _, err := Open(nil); err == nil {
		t.Fatal("a door opened with nothing behind it")
	}
}

// ── what leaves by the byte lane cannot turn round and use the door ────────
//
// A file off the far machine is somebody else's document served on the same
// origin as the listing and the upload. These are the headers that stop it
// being a program with the keys in its pocket.

func TestServedBytesLeaveSandboxedAndUnsniffed(t *testing.T) {
	source := newFake()
	source.files["out/plot.png"] = File{Name: "plot.png", MIME: "image/png", Bytes: []byte("pretend")}
	door, client := openDoor(t, source)

	link, _ := door.FileURL("out/plot.png")
	answer, err := client.Get(link)
	if err != nil {
		t.Fatalf("walking the link: %v", err)
	}
	defer answer.Body.Close()
	for header, want := range map[string]string{
		"Content-Security-Policy": "sandbox",
		"X-Content-Type-Options":  "nosniff",
		"Referrer-Policy":         "no-referrer",
	} {
		if got := answer.Header.Get(header); got != want {
			t.Fatalf("%s is %q, wanted %q — without it a served file runs on the door's own origin", header, got, want)
		}
	}
}

// An HTML or SVG file off the far machine must not RENDER on this origin: with
// the sandbox it is in an opaque origin and same-origin with nothing, and it is
// sent as an attachment on top of that so the browser saves it instead. What a
// person came to look at still opens in the tab.
func TestAFarMachinesPageIsNeverRenderedOnTheDoorsOwnOrigin(t *testing.T) {
	source := newFake()
	source.files["report.html"] = File{Name: "report.html", MIME: "text/html; charset=utf-8", Bytes: []byte("<script>steal()</script>")}
	source.files["chart.svg"] = File{Name: "chart.svg", MIME: "image/svg+xml", Bytes: []byte("<svg/>")}
	source.files["log.txt"] = File{Name: "log.txt", MIME: "text/plain; charset=utf-8", Bytes: []byte("the whole log")}
	source.files["paper.pdf"] = File{Name: "paper.pdf", MIME: "application/pdf", Bytes: []byte("%PDF-1.7")}
	source.files["plot.png"] = File{Name: "plot.png", MIME: "image/png", Bytes: []byte("pretend")}
	door, client := openDoor(t, source)

	for path, wantInline := range map[string]bool{
		"report.html": false,
		"chart.svg":   false,
		"log.txt":     true,
		"paper.pdf":   true,
		"plot.png":    true,
	} {
		link, err := door.FileURL(path)
		if err != nil {
			t.Fatalf("minting %s: %v", path, err)
		}
		answer, err := client.Get(link)
		if err != nil {
			t.Fatalf("walking %s: %v", path, err)
		}
		answer.Body.Close()
		if got := answer.Header.Get("Content-Security-Policy"); got != "sandbox" {
			t.Fatalf("%s came back with CSP %q, wanted the sandbox", path, got)
		}
		disposition := answer.Header.Get("Content-Disposition")
		inline := strings.HasPrefix(disposition, "inline")
		if inline != wantInline {
			t.Fatalf("%s is served %q; wanted inline=%v — a document a browser executes is saved, not rendered", path, disposition, wantInline)
		}
		if !strings.Contains(disposition, path) {
			t.Fatalf("%s is served as %q, wanted its own name in it", path, disposition)
		}
	}
}

// The page tells the browser to send no Referer anywhere, and every file it
// opens is opened with no opener and no referrer — the two ways a rendered
// document used to be able to read the address it was opened from.
func TestTheBrowsePageLeaksNeitherReferrerNorOpener(t *testing.T) {
	door, client := openDoor(t, newFake())
	answer, err := client.Get(origin(t, door) + "/browse/" + token(t, door))
	if err != nil {
		t.Fatalf("asking for the browse page: %v", err)
	}
	defer answer.Body.Close()
	if got := answer.Header.Get("Referrer-Policy"); got != "no-referrer" {
		t.Fatalf("the browse page answered Referrer-Policy %q, wanted no-referrer", got)
	}
	page, _ := io.ReadAll(answer.Body)
	text := string(page)
	if !strings.Contains(text, `<meta name="referrer" content="no-referrer">`) {
		t.Fatal("the page does not tell the browser to send no referrer")
	}
	if !strings.Contains(text, `"noopener noreferrer"`) {
		t.Fatal("the page opens files with a referrer, which is the address the token used to be in")
	}
	if strings.Contains(text, `"noopener"`) {
		t.Fatal("a bare noopener is still in the page")
	}
}

// ── the token never reaches an argv ────────────────────────────────────────

func TestTheAddressHandedToTheOpenerCarriesNoToken(t *testing.T) {
	door, _ := openDoor(t, newFake())
	link := door.BrowseURL()
	if strings.Contains(link, token(t, door)) {
		t.Fatal("BrowseURL carries the token, so exec carries it, so /proc carries it")
	}
	if !strings.Contains(link, "/enter/") {
		t.Fatalf("BrowseURL is %q, wanted an /enter/<nonce> address", link)
	}
	// Two asks are two different nonces: one spent address must not spend
	// another person's.
	if again := door.BrowseURL(); again == link {
		t.Fatal("two asks got the same entry nonce")
	}
}

func TestWalkingInSetsTheCookieAndSpendsTheNonce(t *testing.T) {
	door, client := openDoor(t, newFake())
	link := door.BrowseURL()

	answer, err := client.Get(link)
	if err != nil {
		t.Fatalf("walking in: %v", err)
	}
	answer.Body.Close()
	if answer.StatusCode != http.StatusFound {
		t.Fatalf("the entry answered %d, wanted a 302", answer.StatusCode)
	}
	where := answer.Header.Get("Location")
	if where != "/browse/" {
		t.Fatalf("the entry sent the browser to %q, wanted /browse/ with nothing secret in it", where)
	}
	cookies := answer.Cookies()
	if len(cookies) != 1 {
		t.Fatalf("the entry set %d cookies, wanted the one", len(cookies))
	}
	cookie := cookies[0]
	if cookie.Value != token(t, door) {
		t.Fatal("the cookie does not carry the door's token, so the page cannot authorise itself")
	}
	if !cookie.HttpOnly {
		t.Fatal("the cookie is readable by script, which is the whole thing it must not be")
	}
	if cookie.SameSite != http.SameSiteStrictMode {
		t.Fatalf("the cookie is SameSite %v, wanted Strict", cookie.SameSite)
	}
	if cookie.Path != "/" {
		t.Fatalf("the cookie is scoped to %q, wanted the whole door", cookie.Path)
	}
	// THE NONCE IS SPENT. A second walk by anybody who read the address off a
	// screen — or out of /proc, where it is harmless now — gets nothing.
	second, err := client.Get(link)
	if err != nil {
		t.Fatalf("walking in twice: %v", err)
	}
	second.Body.Close()
	if second.StatusCode != http.StatusNotFound {
		t.Fatalf("a spent entry answered %d, wanted 404", second.StatusCode)
	}
}

// The cookie is the page's own lane: no token in any address it builds, and the
// door still answers. The path lane keeps working alongside it, because a
// person with curl and the token is how this door was first opened.
func TestTheCookieAuthorisesWithNothingInTheAddress(t *testing.T) {
	source := newFake()
	source.dirs["."] = []Entry{{Name: "README.md", Size: 10}}
	door, client := openDoor(t, source)
	base := origin(t, door)
	cookie := walkIn(t, door, client)

	ask := func(where string, carry *http.Cookie) *http.Response {
		t.Helper()
		request, err := http.NewRequest(http.MethodGet, base+where, nil)
		if err != nil {
			t.Fatalf("building a request for %s: %v", where, err)
		}
		if carry != nil {
			request.AddCookie(carry)
		}
		answer, err := client.Do(request)
		if err != nil {
			t.Fatalf("GET %s: %v", where, err)
		}
		return answer
	}

	answer := ask("/api/ls?path=.", cookie)
	defer answer.Body.Close()
	if answer.StatusCode != http.StatusOK {
		t.Fatalf("the cookie's own listing answered %d, wanted 200", answer.StatusCode)
	}
	var got listing
	if err := json.NewDecoder(answer.Body).Decode(&got); err != nil {
		t.Fatalf("the listing did not decode: %v", err)
	}
	if len(got.Entries) != 1 {
		t.Fatalf("the listing has %d rows, wanted the one", len(got.Entries))
	}

	page := ask("/browse/", cookie)
	page.Body.Close()
	if page.StatusCode != http.StatusOK {
		t.Fatalf("the page behind the cookie answered %d, wanted 200", page.StatusCode)
	}

	// Back-compat, unbroken: the token in the path is still a way in.
	old := ask("/api/"+token(t, door)+"/ls?path=.", nil)
	old.Body.Close()
	if old.StatusCode != http.StatusOK {
		t.Fatalf("the path lane answered %d, wanted 200", old.StatusCode)
	}

	// And nothing without either gets anything, on the same 404 as ever.
	for _, where := range []string{"/api/ls?path=.", "/browse/"} {
		bare := ask(where, nil)
		bare.Body.Close()
		if bare.StatusCode != http.StatusNotFound {
			t.Fatalf("GET %s with no cookie and no token answered %d, wanted 404", where, bare.StatusCode)
		}
	}
	wrong := ask("/api/ls?path=.", &http.Cookie{Name: cookie.Name, Value: "ffffffffffffffffffffffffffffffff"})
	wrong.Body.Close()
	if wrong.StatusCode != http.StatusNotFound {
		t.Fatalf("a wrong cookie answered %d, wanted 404", wrong.StatusCode)
	}
}

// A cookie jar keys on host and not on port, so two doors at once would share
// one name if the name did not carry the port. The older tab answering 404 for
// no visible reason is what that would look like.
func TestTwoDoorsDoNotShareOneCookieName(t *testing.T) {
	first, _ := openDoor(t, newFake())
	second, _ := openDoor(t, newFake())
	if first.cookie == second.cookie {
		t.Fatalf("both doors keep their token under %q", first.cookie)
	}
}

// ── the id table has an end ────────────────────────────────────────────────
//
// Every redraw of a line that names a file mints into it, so an unbounded table
// is a session that grows for as long as it is open. The oldest link is the one
// that goes, and a forgotten id answers the 404 an unminted id answers.
func TestTheIDTableStopsGrowing(t *testing.T) {
	door, client := openDoor(t, newFake())

	firstLink, err := door.FileURL("out/0")
	if err != nil {
		t.Fatalf("minting the first link: %v", err)
	}
	for i := 1; i < maxLinks+16; i++ {
		if _, err := door.FileURL(fmt.Sprintf("out/%d", i)); err != nil {
			t.Fatalf("minting link %d: %v", i, err)
		}
	}
	door.mutex.Lock()
	ids, paths, order := len(door.byID), len(door.byPath), len(door.order)
	door.mutex.Unlock()
	if ids > maxLinks || paths > maxLinks || order > maxLinks {
		t.Fatalf("the table holds %d ids, %d paths and %d in order; the cap is %d", ids, paths, order, maxLinks)
	}
	// The oldest one is gone, and it is gone the way an id nobody minted is
	// gone: 404, not some new kind of failure.
	answer, err := client.Get(firstLink)
	if err != nil {
		t.Fatalf("walking a forgotten link: %v", err)
	}
	answer.Body.Close()
	if answer.StatusCode != http.StatusNotFound {
		t.Fatalf("a forgotten link answered %d, wanted 404", answer.StatusCode)
	}
}

// onePartForm builds the multipart body the browse page's XHR sends: one part
// called "file", with a filename.
func onePartForm(t *testing.T, name string, data []byte) (io.Reader, string) {
	t.Helper()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	part, err := form.CreateFormFile("file", name)
	if err != nil {
		t.Fatalf("building the form: %v", err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatalf("writing the form: %v", err)
	}
	if err := form.Close(); err != nil {
		t.Fatalf("closing the form: %v", err)
	}
	return &body, form.FormDataContentType()
}
