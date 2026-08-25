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

// origin recovers the door's own base URL from a browse URL, which is the only
// address the door hands out whole.
func origin(t *testing.T, door *Door) string {
	t.Helper()
	parsed, err := url.Parse(door.BrowseURL())
	if err != nil {
		t.Fatalf("the browse URL did not parse: %v", err)
	}
	return parsed.Scheme + "://" + parsed.Host
}

func token(t *testing.T, door *Door) string {
	t.Helper()
	parsed, err := url.Parse(door.BrowseURL())
	if err != nil {
		t.Fatalf("the browse URL did not parse: %v", err)
	}
	return strings.TrimPrefix(parsed.Path, "/browse/")
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
	answer, err := client.Get(door.BrowseURL())
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
	if !strings.Contains(text, token(t, door)) {
		t.Fatal("the page cannot authorise itself: its token is not in it")
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
	if !strings.Contains(where, "/f/") {
		t.Fatalf("the redirect went to %q, wanted /f/<id>", where)
	}
	// The minted id is the SAME one FileURL hands the surface, because both go
	// through the one path index.
	link, _ := door.FileURL("out/log.txt")
	if !strings.HasSuffix(link, strings.TrimPrefix(where, base)) {
		t.Fatalf("the page's id (%q) and the surface's link (%q) disagree", where, link)
	}
	served, err := client.Get(base + strings.TrimPrefix(where, base))
	if err != nil {
		t.Fatalf("walking the redirect: %v", err)
	}
	defer served.Body.Close()
	body, _ := io.ReadAll(served.Body)
	if string(body) != "the whole log" {
		t.Fatalf("the redirect served %q, wanted the file", body)
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
