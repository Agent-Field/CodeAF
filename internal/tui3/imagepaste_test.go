package tui3

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// pasteText is one bracketed paste, the way the terminal delivers it.
func pasteText(t *testing.T, a *app, text string) {
	t.Helper()
	drive(t, a, tea.PasteMsg{Content: text})
}

// THE WHOLE POINT, IN ONE TEST. A terminal writes a dropped screenshot into the
// clipboard as its path; what the person is left holding must be a picture on
// the tray and a token in the sentence, not a path they have to explain.
func TestDroppingAPictureLeavesATokenAndAttachesTheFile(t *testing.T) {
	a, _, dir := attachLab(t, map[string]int{"shot.png": 12})
	typeText(t, a, "what font is")
	pasteText(t, a, filepath.Join(dir, "shot.png"))

	if got := a.input.String(); got != "what font is [image #1] " {
		t.Fatalf("the draft is %q, want the path replaced by its token", got)
	}
	if want := []string{"shot.png"}; !equalStrings(chipNames(a), want) {
		t.Fatalf("chips are %v, want %v", chipNames(a), want)
	}
	if got := a.chips[0].path; got != filepath.Join(dir, "shot.png") {
		t.Fatalf("the chip holds %q, want the dropped path", got)
	}
	// THE TRAY AND THE SENTENCE AGREE ABOUT THE NUMBER, which is the only reason
	// the number is worth showing at all.
	if strip := plain(a.chipStrip(a.width)); !strings.Contains(strip, "#1 shot.png") {
		t.Fatalf("the tray reads %q, want the picture numbered in it", strip)
	}
}

// A TERMINAL ESCAPES THE SPACES IN A DROPPED PATH, because the text it is
// writing is meant for a shell. A screenshot is the file this matters most for:
// macOS names them with four spaces in them, and a splitter that took every
// space would find five words and call none of them a picture.
func TestADroppedPathKeepsItsEscapedAndQuotedSpaces(t *testing.T) {
	name := "Screenshot 2026-08-21 at 5.21.40 PM.png"
	for _, spell := range []func(string) string{
		func(base string) string { return strings.ReplaceAll(base, " ", `\ `) },
		func(base string) string { return "'" + base + "'" },
		func(base string) string { return `"` + base + `"` },
	} {
		a, _, dir := attachLab(t, map[string]int{name: 12})
		// The escaping is on the whole path as the terminal writes it, so it is
		// applied after the join and not to a base name joined onto a directory.
		pasteText(t, a, spell(filepath.Join(dir, name)))

		if want := []string{name}; !equalStrings(chipNames(a), want) {
			t.Fatalf("%q attached %v, want %v", spell(name), chipNames(a), want)
		}
		if got := a.input.String(); got != "[image #1] " {
			t.Fatalf("%q left the draft %q", spell(name), got)
		}
	}
}

// A file:// URL is what a desktop's drag protocol carries, and several terminals
// pass it straight through. Its percent-escapes have to come off before anything
// is stat'ed: %20 is a space in a name and nothing is called "%20".
func TestADroppedFileURLIsAPath(t *testing.T) {
	a, _, dir := attachLab(t, map[string]int{"a shot.png": 12})
	pasteText(t, a, "file://"+strings.ReplaceAll(filepath.Join(dir, "a shot.png"), " ", "%20"))

	if want := []string{"a shot.png"}; !equalStrings(chipNames(a), want) {
		t.Fatalf("chips are %v, want %v", chipNames(a), want)
	}
}

// SEVERAL FILES AT ONCE ARE NUMBERED IN THE ORDER THEY LAND, and the tokens run
// left to right the way the person dropped them.
func TestDroppingTwoPicturesNumbersThemInOrder(t *testing.T) {
	a, agent, dir := attachLab(t, map[string]int{"one.png": 8, "two.png": 8})
	pasteText(t, a, filepath.Join(dir, "one.png")+"\n"+filepath.Join(dir, "two.png"))
	typeText(t, a, "spot the difference")
	drive(t, a, key("enter"))

	if agent.text != "[image #1] [image #2] spot the difference" {
		t.Fatalf("the message read %q", agent.text)
	}
	if len(agent.images) != 2 {
		t.Fatalf("the message carried %d images, want 2", len(agent.images))
	}
	if base := filepath.Base(agent.images[0].Path); base != "one.png" {
		t.Fatalf("image #1 was %q, want the first one dropped", base)
	}
	if base := filepath.Base(agent.images[1].Path); base != "two.png" {
		t.Fatalf("image #2 was %q", base)
	}
}

// A PASTE THAT IS NOT PICTURES IS TEXT, and this is the case the surface sees a
// thousand times more often. One word that is not a picture and the whole paste
// goes into the box exactly as it always did.
func TestAPasteThatIsNotAllPicturesStaysText(t *testing.T) {
	a, _, dir := attachLab(t, map[string]int{"shot.png": 12})
	for _, paste := range []string{
		"look at " + filepath.Join(dir, "shot.png"),
		filepath.Join(dir, "missing.png"),
		"panic: runtime error\n\tmain.go:12",
		filepath.Join(dir, "shot.png") + " " + filepath.Join(dir, "notes.md"),
	} {
		a.input.reset()
		a.chips = nil
		pasteText(t, a, paste)
		if len(a.chips) != 0 {
			t.Fatalf("paste %q attached %v", paste, chipNames(a))
		}
		if got := a.input.String(); got != paste {
			t.Fatalf("paste %q landed as %q", paste, got)
		}
	}
}

// A SLASH COMMAND'S ARGUMENT IS A PATH AND MUST STAY ONE: /image is the one line
// on this surface whose whole job is to take one, and dropping a file on it is
// somebody using it exactly as documented.
func TestAPathDroppedOnASlashCommandStaysAPath(t *testing.T) {
	a, _, dir := attachLab(t, map[string]int{"shot.png": 12})
	typeText(t, a, "/image ")
	pasteText(t, a, filepath.Join(dir, "shot.png"))

	if got := a.input.String(); got != "/image "+filepath.Join(dir, "shot.png") {
		t.Fatalf("the draft is %q, want the path left alone", got)
	}
	if len(a.chips) != 0 {
		t.Fatalf("the drop attached %v before the command ran", chipNames(a))
	}
	drive(t, a, key("enter"))
	if want := []string{"shot.png"}; !equalStrings(chipNames(a), want) {
		t.Fatalf("the command attached %v, want %v", chipNames(a), want)
	}
}

// AN OVERSIZE PICTURE IS REFUSED AT THE DOOR, BY NAME, and the path stays in the
// box: a refusal that also swallowed what was dropped would leave the person
// with nothing to point at.
func TestADroppedPictureOverTheCeilingIsRefusedByName(t *testing.T) {
	a, _, dir := attachLab(t, map[string]int{"huge.png": maxAttachBytes + 1})
	path := filepath.Join(dir, "huge.png")
	pasteText(t, a, path)

	if len(a.chips) != 0 {
		t.Fatalf("an oversize picture was attached: %v", chipNames(a))
	}
	if got := a.input.String(); got != path {
		t.Fatalf("the draft is %q, want the path kept as text", got)
	}
	if body := strings.Join(plainRows(a), "\n"); !strings.Contains(body, "huge.png is over the 10MB image limit") {
		t.Fatalf("the refusal does not name the file:\n%s", body)
	}
}

// THE SAME FILE TWICE IS ONE PICTURE, and the second token points at the one
// that is already there rather than at a number no chip carries.
func TestDroppingTheSamePictureTwiceReusesItsNumber(t *testing.T) {
	a, _, dir := attachLab(t, map[string]int{"shot.png": 12})
	path := filepath.Join(dir, "shot.png")
	pasteText(t, a, path)
	pasteText(t, a, path)

	if len(a.chips) != 1 {
		t.Fatalf("the tray holds %v, want one picture", chipNames(a))
	}
	if got := a.input.String(); got != "[image #1] [image #1] " {
		t.Fatalf("the draft is %q, want both tokens naming the one picture", got)
	}
}

// TAKING A PICTURE OFF RENUMBERS THE SENTENCE. A draft still saying "[image #2]"
// about a picture that is now first would be the surface lying about which one
// the model will be looking at.
func TestRemovingAChipRewritesTheTokensBehindIt(t *testing.T) {
	a, _, dir := attachLab(t, map[string]int{"one.png": 8, "two.png": 8, "three.png": 8})
	pasteText(t, a, filepath.Join(dir, "one.png"))
	typeText(t, a, "and ")
	pasteText(t, a, filepath.Join(dir, "two.png"))
	typeText(t, a, "and ")
	pasteText(t, a, filepath.Join(dir, "three.png"))
	if got := a.input.String(); got != "[image #1] and [image #2] and [image #3] " {
		t.Fatalf("the draft is %q before anything came off", got)
	}

	a.removeChip(0)

	if got := a.input.String(); got != "and [image #1] and [image #2] " {
		t.Fatalf("the draft is %q, want the gone token out and the rest counted down", got)
	}
	if want := []string{"two.png", "three.png"}; !equalStrings(chipNames(a), want) {
		t.Fatalf("chips are %v, want %v", chipNames(a), want)
	}
}

// BACKSPACE OVER AN EMPTY-OF-WORDS BOX DROPS THE LAST PICTURE, and its token
// goes with it — the tray and the sentence are one thing.
func TestBackspaceDropsTheLastPictureAndItsToken(t *testing.T) {
	a, _, dir := attachLab(t, map[string]int{"one.png": 8, "two.png": 8})
	pasteText(t, a, filepath.Join(dir, "one.png")+" "+filepath.Join(dir, "two.png"))
	a.dropChip()

	if got := a.input.String(); got != "[image #1] " {
		t.Fatalf("the draft is %q, want only the first picture's token left", got)
	}
}

// A DROPPED PICTURE REACHES THE MODEL AS PIXELS, AND ITS TOKEN REACHES IT AS
// WORDS. This is the whole contract in one pass over a real HTTP body: the
// message the provider receives is an array of parts, the text part is the
// person's sentence with `[image #1]` still in it, and the image part carries
// the file's bytes base64'd under the right media type — in that order, so the
// number in the sentence and the picture's place in the message are the same
// number.
func TestADroppedPicturesTokenAndBytesBothReachTheWire(t *testing.T) {
	var mu sync.Mutex
	var bodies []map[string]json.RawMessage
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]json.RawMessage
		if err := json.Unmarshal(raw, &body); err == nil {
			mu.Lock()
			bodies = append(bodies, body)
			mu.Unlock()
		}
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, `data: {"id":"x","choices":[{"index":0,"delta":{"role":"assistant","content":"a screenshot"}}]}`+"\n\n")
		io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	dir := t.TempDir()
	path := filepath.Join(dir, "shot.png")
	pixels := []byte("PIXELS-NOT-A-PATH")
	if err := os.WriteFile(path, pixels, 0o644); err != nil {
		t.Fatal(err)
	}

	agent, err := session.New(session.Config{
		Workspace:      dir,
		Model:          "vendor/sees",
		APIKey:         "test",
		BaseURL:        server.URL,
		System:         "SYSTEM",
		SupportsImages: func(string) bool { return true },
	})
	if err != nil {
		t.Fatalf("session.New: %v", err)
	}
	defer agent.Close()

	// The surface, driven exactly as a person drives it: type, drop, send.
	a := newApp(context.Background(), Options{Agent: agent, Workspace: dir})
	a.width, a.height = 80, 24
	a.entries = nil
	a.welcome = welcome{spent: true}
	typeText(t, a, "what font is")
	pasteText(t, a, path)
	drive(t, a, key("enter"))

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		seen := len(bodies)
		mu.Unlock()
		if seen > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(bodies) == 0 {
		t.Fatal("the provider saw no request — the message never went out")
	}
	var messages []struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(bodies[0]["messages"], &messages); err != nil {
		t.Fatalf("decode messages: %v", err)
	}
	last := messages[len(messages)-1]
	if last.Role != "user" {
		t.Fatalf("the last wire message is a %q", last.Role)
	}
	// Decoding as an ARRAY is half the assertion: a message flattened to a bare
	// string is a message with no picture in it.
	var parts []struct {
		Type     string `json:"type"`
		Text     string `json:"text"`
		ImageURL struct {
			URL string `json:"url"`
		} `json:"image_url"`
	}
	if err := json.Unmarshal(last.Content, &parts); err != nil {
		t.Fatalf("the user message is not content parts: %v — %s", err, last.Content)
	}
	if len(parts) != 2 {
		t.Fatalf("the message carried %d parts, want the words and the picture", len(parts))
	}
	if parts[0].Type != "text" || parts[0].Text != "what font is [image #1]" {
		t.Fatalf("the words reached the model as %q (%s)", parts[0].Text, parts[0].Type)
	}
	if parts[1].Type != "image_url" {
		t.Fatalf("the second part is a %q, want the picture", parts[1].Type)
	}
	want := "data:image/png;base64," + base64.StdEncoding.EncodeToString(pixels)
	if parts[1].ImageURL.URL != want {
		t.Fatalf("the picture reached the model as %q", parts[1].ImageURL.URL)
	}
}
