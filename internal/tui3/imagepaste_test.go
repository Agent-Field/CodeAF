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

func TestUnknownSlashProseCanStillAttachAnImage(t *testing.T) {
	a, _, dir := attachLab(t, map[string]int{"shot.png": 12})
	a.input.setText("/api/v1 is broken ")
	a.input.end()
	pasteText(t, a, filepath.Join(dir, "shot.png"))
	if len(a.chips) != 1 || !strings.Contains(a.input.String(), "[image #1]") {
		t.Fatalf("unknown slash prose left image as text: %q %v", a.input.String(), chipNames(a))
	}
}

// P1: a raw-space picture paste becomes one numbered chip and one draft token.
func TestARawSpacePicturePasteBecomesOneNumberedChip(t *testing.T) {
	name := "Screen Shot 2026-08-21 at 5.21.40 PM.png"
	a, _, dir := attachLab(t, map[string]int{name: 12})
	pasteText(t, a, filepath.Join(dir, name))

	if got := a.input.String(); got != "[image #1] " {
		t.Fatalf("the raw-space path left the draft %q", got)
	}
	if want := []string{name}; !equalStrings(chipNames(a), want) {
		t.Fatalf("chips are %v, want %v", chipNames(a), want)
	}
	if strip := plain(a.chipStrip(a.width)); !strings.Contains(strip, "#1 "+name) {
		t.Fatalf("the tray reads %q, want the numbered picture", strip)
	}
}

// An ordinary file dropped on the terminal takes the same visible tray road as
// /attach. In a hosted conversation that tray is what makes the bytes cross
// when the person sends, instead of leaving a local path in the draft.
func TestDroppingAnOrdinaryFileAttachesItInsteadOfPastingItsPath(t *testing.T) {
	a, _, dir := attachLab(t, map[string]int{"server.log": 12})
	pasteText(t, a, filepath.Join(dir, "server.log"))
	if got := a.input.String(); got != "" {
		t.Fatalf("the local path remained in the draft: %q", got)
	}
	if want := []string{"server.log"}; !equalStrings(chipNames(a), want) {
		t.Fatalf("chips are %v, want %v", chipNames(a), want)
	}
	if !a.chips[0].file {
		t.Fatal("the dropped log became a picture chip")
	}
}

func TestDroppingAFolderUsesTheAttachRefusal(t *testing.T) {
	a, _, dir := attachLab(t, nil)
	pasteText(t, a, dir)
	if len(a.chips) != 0 || a.input.String() != "" {
		t.Fatalf("the folder became draft or tray content: %q %v", a.input.String(), chipNames(a))
	}
	if said := strings.Join(plainRows(a), "\n"); !strings.Contains(said, filepath.Base(dir)+" is a folder · attach a file") {
		t.Fatalf("the folder refusal was not shown:\n%s", said)
	}
}

// A TERMINAL ESCAPES THE SPACES IN A DROPPED PATH, because the text it is
// writing is meant for a shell. A screenshot is the file this matters most for:
// macOS names them with four spaces in them, and a splitter that took every
// space would find five words and call none of them a picture.
func TestADroppedPathKeepsItsEscapedAndQuotedSpaces(t *testing.T) {
	for _, tc := range []struct {
		name  string
		spell func(string) string
	}{
		{"backslashed spaces", func(base string) string { return strings.ReplaceAll(base, " ", `\ `) }},
		{"single quotes", func(base string) string { return "'" + base + "'" }},
		{"double quotes", func(base string) string { return `"` + base + `"` }},
		{"macOS narrow no-break space", func(base string) string { return strings.ReplaceAll(base, " ", `\ `) }},
	} {
		name := "Screenshot 2026-08-21 at 5.21.40 PM.png"
		if tc.name == "macOS narrow no-break space" {
			name = "Screenshot 2026-08-21 at 5.21.40\u202fPM.png"
		}
		a, _, dir := attachLab(t, map[string]int{name: 12})
		// The escaping is on the whole path as the terminal writes it, so it is
		// applied after the join and not to a base name joined onto a directory.
		pasteText(t, a, tc.spell(filepath.Join(dir, name)))

		if want := []string{name}; !equalStrings(chipNames(a), want) {
			t.Fatalf("%s attached %v, want %v", tc.name, chipNames(a), want)
		}
		if got := a.input.String(); got != "[image #1] " {
			t.Fatalf("%s left the draft %q", tc.name, got)
		}
	}
}

// P3: every terminal spelling resolves without changing multi-file order.
func TestEveryTerminalPasteSpellingResolves(t *testing.T) {
	for _, tc := range []struct {
		name  string
		files map[string]int
		paste func(string) string
		want  []string
	}{
		{
			name:  "raw spaces",
			files: map[string]int{"Screen Shot.png": 8},
			paste: func(dir string) string { return filepath.Join(dir, "Screen Shot.png") },
			want:  []string{"Screen Shot.png"},
		},
		{
			name:  "kitty bare percent escapes",
			files: map[string]int{"Screen Shot.png": 8},
			paste: func(dir string) string { return strings.ReplaceAll(filepath.Join(dir, "Screen Shot.png"), " ", "%20") },
			want:  []string{"Screen Shot.png"},
		},
		{
			name:  "VTE quoted path with a trailing newline",
			files: map[string]int{"Screen Shot.png": 8},
			paste: func(dir string) string { return "'" + filepath.Join(dir, "Screen Shot.png") + "\n'" },
			want:  []string{"Screen Shot.png"},
		},
		{
			name:  "VTE apostrophe spelling",
			files: map[string]int{"owner's shot.png": 8},
			paste: func(dir string) string {
				return "'" + strings.ReplaceAll(filepath.Join(dir, "owner's shot.png"), "'", `'\''`) + "'"
			},
			want: []string{"owner's shot.png"},
		},
		{
			name:  "Ghostty space-joined files",
			files: map[string]int{"one.png": 8, "two.png": 8},
			paste: func(dir string) string { return filepath.Join(dir, "one.png") + " " + filepath.Join(dir, "two.png") },
			want:  []string{"one.png", "two.png"},
		},
		{
			name:  "newline-separated pictures",
			files: map[string]int{"one.png": 8, "two.png": 8},
			paste: func(dir string) string { return filepath.Join(dir, "one.png") + "\n" + filepath.Join(dir, "two.png") },
			want:  []string{"one.png", "two.png"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, _, dir := attachLab(t, tc.files)
			pasteText(t, a, tc.paste(dir))
			if !equalStrings(chipNames(a), tc.want) {
				t.Fatalf("chips are %v, want %v", chipNames(a), tc.want)
			}
		})
	}
}

// P7: the shell-word parser splits ASCII IFS and preserves every other rune.
func TestPastedWordsMatchesTheShellsSeparators(t *testing.T) {
	for _, tc := range []struct {
		text string
		want []string
	}{
		{`/tmp/Screen\ Shot.png`, []string{"/tmp/Screen Shot.png"}},
		{`'/tmp/Screen Shot.png'`, []string{"/tmp/Screen Shot.png"}},
		{`"C:\Users\me\My Shot.png"`, []string{`C:\Users\me\My Shot.png`}},
		{"/tmp/Screenshot\\ 5.21.40\u202fPM.png", []string{"/tmp/Screenshot 5.21.40\u202fPM.png"}},
		{"/a/one.png\t/a/two.png\r\n/a/three.png\v/a/four.png\f/a/five.png", []string{"/a/one.png", "/a/two.png", "/a/three.png", "/a/four.png", "/a/five.png"}},
	} {
		if got := pastedWords(tc.text); !equalStrings(got, tc.want) {
			t.Errorf("pastedWords(%q) = %#v, want %#v", tc.text, got, tc.want)
		}
	}
}

// P7: alternate paste readings stay ordered and duplicate readings are removed.
func TestPasteReadingsAreLiteralFirstAndDeduplicated(t *testing.T) {
	for _, tc := range []struct {
		text string
		want [][]string
	}{
		{
			text: "/tmp/Screen Shot.png",
			want: [][]string{{"/tmp/Screen", "Shot.png"}, {"/tmp/Screen Shot.png"}},
		},
		{
			text: "/tmp/Screen%20Shot.png",
			want: [][]string{{"/tmp/Screen%20Shot.png"}, {"/tmp/Screen Shot.png"}},
		},
		{
			text: "/tmp/one.png\n/tmp/two.png",
			want: [][]string{{"/tmp/one.png", "/tmp/two.png"}, {"/tmp/one.png\n/tmp/two.png"}},
		},
		{
			text: "look at /tmp/shot.png",
			want: [][]string{{"look", "at", "/tmp/shot.png"}},
		},
		{
			text: "/tmp/server.log is full of these errors",
			want: [][]string{{"/tmp/server.log", "is", "full", "of", "these", "errors"}, {"/tmp/server.log is full of these errors"}},
		},
	} {
		got := pasteReadings(tc.text)
		if len(got) != len(tc.want) {
			t.Fatalf("pasteReadings(%q) = %#v, want %#v", tc.text, got, tc.want)
		}
		for i := range got {
			if !equalStrings(got[i], tc.want[i]) {
				t.Errorf("pasteReadings(%q)[%d] = %#v, want %#v", tc.text, i, got[i], tc.want[i])
			}
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
	a, _, dir := attachLab(t, map[string]int{"Screen Shot.png": 12})
	path := filepath.Join(dir, "Screen Shot.png")
	typeText(t, a, "/image ")
	pasteText(t, a, path)

	if got := a.input.String(); got != "/image "+path {
		t.Fatalf("the draft is %q, want the path left alone", got)
	}
	if len(a.chips) != 0 {
		t.Fatalf("the drop attached %v before the command ran", chipNames(a))
	}
	drive(t, a, key("enter"))
	if want := []string{"Screen Shot.png"}; !equalStrings(chipNames(a), want) {
		t.Fatalf("the command attached %v, want %v", chipNames(a), want)
	}
}

// AN OVERSIZE PICTURE IS REFUSED AT THE DOOR, BY NAME, and the path stays in the
// box: a refusal that also swallowed what was dropped would leave the person
// with nothing to point at.
func TestADroppedPictureOverTheCeilingIsRefusedByName(t *testing.T) {
	a, _, dir := attachLab(t, map[string]int{"huge picture.png": maxAttachBytes + 1})
	path := filepath.Join(dir, "huge picture.png")
	pasteText(t, a, path)

	if len(a.chips) != 0 {
		t.Fatalf("an oversize picture was attached: %v", chipNames(a))
	}
	if got := a.input.String(); got != path {
		t.Fatalf("the draft is %q, want the path kept as text", got)
	}
	if body := strings.Join(plainRows(a), "\n"); !strings.Contains(body, "huge picture.png is over the 10MB image limit") {
		t.Fatalf("the refusal does not name the file:\n%s", body)
	}
}

// P5: the drop road applies the ordinary-file ceiling only over a connection.
func TestALargeDroppedFileIsLocalOnly(t *testing.T) {
	for _, tc := range []struct {
		name string
		host string
		want int
	}{
		{"local", "", 1},
		{"hosted", "devbox", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, _, dir := fileLab(t, tc.host, map[string]int{"large dump.bin": 20 * 1024 * 1024})
			path := filepath.Join(dir, "large dump.bin")
			pasteText(t, a, path)
			if len(a.chips) != tc.want {
				t.Fatalf("the tray holds %d chips, want %d", len(a.chips), tc.want)
			}
			body := strings.Join(plainRows(a), "\n")
			if tc.host == "" && strings.Contains(body, "over the 16MB file limit") {
				t.Fatalf("a local drop was refused:\n%s", body)
			}
			if tc.host != "" && !strings.Contains(body, "large dump.bin is 20MB and over the 16MB file limit") {
				t.Fatalf("the hosted refusal did not name the file:\n%s", body)
			}
		})
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
