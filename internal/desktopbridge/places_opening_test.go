package desktopbridge

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/placegraph"
	"github.com/Agent-Field/codeaf/internal/session"
)

// The shapes here are SYNTHETIC. What they copy from real saved conversations
// is the structure measured on two machines: most quick chats run in the home
// folder, and a saved chat's title is a few words while its opening message
// says what it is about.

// opening writes a saved conversation whose transcript opens with a session
// header and then the person's first message.
func (r *closedRig) opening(id, title, first, workspace string, at time.Time) {
	r.t.Helper()
	row := r.saved(id, title, nil, at, workspace)
	body := `{"type":"session","version":1,"id":"` + id + `"}` + "\n" +
		`{"type":"message","role":"user","content":` + quote(first) + `}` + "\n" +
		`{"type":"message","role":"assistant","content":"Here is what I found."}` + "\n"
	if err := os.WriteFile(row.Transcript, []byte(body), 0o600); err != nil {
		r.t.Fatal(err)
	}
}

func quote(s string) string { return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"` }

// capture answers every question "no group" and keeps what was asked.
type capture struct {
	mu    sync.Mutex
	asked []placegraph.ModelRequest
}

func (c *capture) answer(_ context.Context, q placegraph.ModelRequest) (session.PlacesAnswer, error) {
	c.mu.Lock()
	c.asked = append(c.asked, q)
	c.mu.Unlock()
	return session.PlacesAnswer{Text: `{"belong": false, "chats": [], "use": "", "name": "", "under": "root", "confidence": 5}`, Model: adviceModel}, nil
}

var quick = []string{"What is using my processor", "Haircut open this evening", "Enable a new debit card", "Resize a banner image", "Day trips to see nature", "Label the open pull requests"}

// THE HOME FOLDER IS NOT A PROJECT. Six unrelated quick chats typed in the home
// folder used to be offered, without a model, as one new place named after the
// account. They are now only ever grouped by what they are about — and the
// same six in a real project folder inside home are still offered as that
// project, so the rule drops exactly the home folder and nothing under it.
func TestTheHomeFolderIsNotAProject(t *testing.T) {
	rig := newClosedRig(t)
	rig.advice.NotProjects = []string{"/home/sam"}
	at := time.Date(2026, 10, 9, 9, 0, 0, 0, time.UTC)
	for i, title := range quick {
		rig.saved(fmt.Sprintf("h%015x", i), title, nil, at.Add(time.Duration(i)*time.Minute), "/home/sam")
	}
	c := &capture{}
	rig.agent.answer = c.answer
	v := rig.organize()
	for _, p := range v.Proposals {
		if p.Basis == placegraph.BasisFolder {
			t.Fatalf("the home folder was offered as a place: %+v", p.Proposal)
		}
	}
	if len(v.Proposals) != 0 {
		t.Fatalf("offers %+v", v.Proposals)
	}

	project := newClosedRig(t)
	project.advice.NotProjects = []string{"/home/sam"}
	for i, title := range quick {
		project.saved(fmt.Sprintf("p%015x", i), title, nil, at.Add(time.Duration(i)*time.Minute), "/home/sam/shed")
	}
	project.agent.answer = c.answer
	w := project.organize()
	if len(w.Proposals) != 1 || w.Proposals[0].Basis != placegraph.BasisFolder || w.Proposals[0].Name != "shed" || len(w.Proposals[0].ChatIDs) != 6 {
		t.Fatalf("a project folder inside home was not offered as itself: %+v", w.Proposals)
	}
}

// A SAVED CHAT IS WEIGHED ON ITS OPENING MESSAGE TOO. With no conversation
// open, the chats' first messages reach the question exactly as an open
// chat's would, read from their own transcripts.
func TestASavedChatIsWeighedOnItsOpeningMessage(t *testing.T) {
	rig := newClosedRig(t)
	at := time.Date(2026, 10, 9, 9, 0, 0, 0, time.UTC)
	for i, title := range quick {
		rig.opening(fmt.Sprintf("o%015x", i), title, "Opening number "+string(rune('A'+i))+": the whole question as the person typed it.", "/desk", at.Add(time.Duration(i)*time.Minute))
	}
	c := &capture{}
	rig.agent.answer = c.answer
	rig.organize()
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.asked) != 1 {
		t.Fatalf("asked %d questions", len(c.asked))
	}
	for i := range quick {
		if want := "Opening number " + string(rune('A'+i)); !strings.Contains(c.asked[0].User, want) {
			t.Fatalf("%q did not reach the question:\n%s", want, c.asked[0].User)
		}
	}
}

// The opening is read cheaply and safely: the first message the PERSON wrote,
// skipping the header and anything else, never past the bound, and nothing
// for a transcript that is missing or holds no message from the person.
func TestSavedOpeningReadsOnlyThePersonsFirstMessage(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	ok := write("a.jsonl", `{"type":"session","version":1}`+"\n"+
		`{"type":"message","role":"assistant","content":"hello"}`+"\n"+
		`{"type":"steer","role":"user","content":"not a message"}`+"\n"+
		`{"type":"message","role":"user","content":"  `+strings.Repeat("x", firstMessageRunes+50)+`  "}`+"\n"+
		`{"type":"message","role":"user","content":"second"}`+"\n")
	if got := savedOpening(ok); got != clipRunes(strings.Repeat("x", firstMessageRunes+50), firstMessageRunes) {
		t.Fatalf("opening %q", got)
	}
	spaced := write("spaced.jsonl", `{ "type": "message", "role": "user", "content": "Spaced JSON is valid too" }`+"\n")
	if got := savedOpening(spaced); got != "Spaced JSON is valid too" {
		t.Fatalf("spaced JSON lost: %q", got)
	}
	none := write("b.jsonl", `{"type":"session","version":1}`+"\n"+`{"type":"message","role":"assistant","content":"hello"}`+"\n")
	if got := savedOpening(none); got != "" {
		t.Fatalf("invented an opening: %q", got)
	}
	var deep strings.Builder
	for i := 0; i < openingLines+5; i++ {
		deep.WriteString(`{"type":"call","call":{}}` + "\n")
	}
	deep.WriteString(`{"type":"message","role":"user","content":"too late"}` + "\n")
	if got := savedOpening(write("c.jsonl", deep.String())); got != "" {
		t.Fatalf("read past the bound: %q", got)
	}
	if got := savedOpening(filepath.Join(dir, "missing.jsonl")); got != "" {
		t.Fatalf("missing file gave %q", got)
	}
	var cache openingCache
	if cache.of("fresh", none) != "" {
		t.Fatal("invented fresh opening")
	}
	if cache.of("fresh", ok) == "" {
		t.Fatal("cached absence hid first message")
	}
	if cache.of("id1", ok) == "" || cache.of("id1", none) == "" {
		t.Fatal("a chat's opening is remembered by its id")
	}
}
