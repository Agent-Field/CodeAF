package desktopbridge

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
)

type richAgent struct {
	*fakeAgent
	mu        sync.Mutex
	calls     []string
	refuse    error
	steer     chan session.Event
	filesText string
	files     []remote.WireFile
	images    []session.Image
	questions []session.Question
	held      []string
	entries   []session.DisplayEntry
}

func (a *richAgent) note(call string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.calls = append(a.calls, call)
	return a.refuse
}
func (a *richAgent) PlanNote(id, text string) error  { return a.note("note " + id + " " + text) }
func (a *richAgent) PlanAmend(id, text string) error { return a.note("amend " + id + " " + text) }
func (a *richAgent) PlanPause(id string) error       { return a.note("pause " + id) }
func (a *richAgent) PlanResume(id string) error      { return a.note("resume " + id) }
func (a *richAgent) PlanCancel(id string) error      { return a.note("cancel " + id) }
func (a *richAgent) Steer(string) (<-chan session.Event, error) {
	return a.steer, nil
}
func (a *richAgent) SubmitFiles(_ context.Context, text string, files []remote.WireFile, images []session.Image) (<-chan session.Event, error) {
	a.mu.Lock()
	a.filesText, a.files, a.images = text, files, images
	a.mu.Unlock()
	return a.events, nil
}
func (a *richAgent) OpenQuestions() []session.Question { return a.questions }
func (a *richAgent) HoldQuestion(kind session.QuestionKind, token string) {
	a.mu.Lock()
	a.held = append(a.held, string(kind)+":"+token)
	a.mu.Unlock()
}
func (a *richAgent) Transcript() []session.DisplayEntry { return a.entries }

func richFixture(t *testing.T, conn func(*Connection)) (*Bridge, *richAgent, string) {
	t.Helper()
	a := &richAgent{fakeAgent: &fakeAgent{events: make(chan session.Event, 8), model: Model}}
	b := New(testToken, func(string) (Connection, error) {
		c := Connection{Agent: a, Welcome: remote.Welcome{SessionFile: "/store/s/session.jsonl", Workspace: "/project", Model: Model, Persistent: true, Launch: &remote.LaunchShape{OneModel: true}}, Close: func() {}}
		if conn != nil {
			conn(&c)
		}
		return c, nil
	})
	t.Cleanup(b.Close)
	w := request(b, "POST", "/api/engine/sessions", "{}")
	var snapshot Snapshot
	if err := json.Unmarshal(w.Body.Bytes(), &snapshot); err != nil {
		t.Fatal(err)
	}
	return b, a, "/api/engine/sessions/" + snapshot.ID
}

func TestTaskActionsReachThePlanAgent(t *testing.T) {
	b, a, path := richFixture(t, nil)
	cases := map[string]string{
		`note|{"text":"mind the cache"}`: "note t-1 mind the cache",
		`amend|{"text":"also docs"}`:     "amend t-1 also docs",
		`pause|{}`:                       "pause t-1",
		`resume|{}`:                      "resume t-1",
		`cancel|{}`:                      "cancel t-1",
	}
	for in, want := range cases {
		action, body, _ := strings.Cut(in, "|")
		w := request(b, "POST", path+"/tasks/t-1/"+action, body)
		if w.Code != 200 || !strings.Contains(w.Body.String(), `"accepted":true`) {
			t.Fatalf("%s: %d %s", action, w.Code, w.Body.String())
		}
		if got := a.calls[len(a.calls)-1]; got != want {
			t.Fatalf("%s reached %q, want %q", action, got, want)
		}
	}
}

func TestTaskActionTextRequiredAndRefusalPassesThrough(t *testing.T) {
	b, a, path := richFixture(t, nil)
	if w := request(b, "POST", path+"/tasks/t-1/note", `{"text":"  "}`); w.Code != 400 {
		t.Fatalf("blank note: %d", w.Code)
	}
	if w := request(b, "POST", path+"/tasks/t-1/amend", `{}`); w.Code != 400 {
		t.Fatalf("missing amend: %d", w.Code)
	}
	if w := request(b, "POST", path+"/tasks/t-1/explode", `{}`); w.Code != 404 {
		t.Fatalf("unknown action: %d", w.Code)
	}
	a.refuse = errors.New("that task's run has ended")
	w := request(b, "POST", path+"/tasks/t-1/cancel", `{}`)
	if w.Code != 409 || !strings.Contains(w.Body.String(), "that task's run has ended") {
		t.Fatalf("refusal: %d %s", w.Code, w.Body.String())
	}
}

func TestTaskActionNeedsPlanAgent(t *testing.T) {
	b, _, id := fixture(t)
	if w := request(b, "POST", "/api/engine/sessions/"+id+"/tasks/t-1/pause", `{}`); w.Code != 409 {
		t.Fatalf("plain agent: %d", w.Code)
	}
}

func TestReadFileCarriesDataAndInlineFlag(t *testing.T) {
	files := map[string]remote.FetchedFile{
		"a.png":  {Name: "a.png", MIME: "image/png", Size: 3, Hash: "h1", Bytes: []byte{1, 2, 3}},
		"a.svg":  {Name: "a.svg", MIME: "image/svg+xml", Size: 2, Hash: "h2", Bytes: []byte("<s")},
		"a.html": {Name: "a.html", MIME: "text/html; charset=utf-8", Size: 1, Hash: "h3", Bytes: []byte("x")},
	}
	b, _, path := richFixture(t, func(c *Connection) {
		c.FetchFile = func(p string) (remote.FetchedFile, error) {
			if f, ok := files[p]; ok {
				return f, nil
			}
			if p == "/etc/passwd" {
				return remote.FetchedFile{}, errors.New("engine: /etc/passwd is outside this conversation's workspace")
			}
			return remote.FetchedFile{}, errors.New("engine: no such file: " + p)
		}
	})
	var got EngineFile
	w := request(b, "GET", path+"/files?path=a.png", "")
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &got) != nil {
		t.Fatalf("png: %d %s", w.Code, w.Body.String())
	}
	if got.Name != "a.png" || got.Mime != "image/png" || got.Size != 3 || got.Hash != "h1" || !got.Inline || got.DataBase64 != base64.StdEncoding.EncodeToString([]byte{1, 2, 3}) {
		t.Fatalf("png body: %+v", got)
	}
	for _, name := range []string{"a.svg", "a.html"} {
		got = EngineFile{}
		w = request(b, "GET", path+"/files?path="+name, "")
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &got) != nil || got.Inline || got.DataBase64 == "" {
			t.Fatalf("%s must be data and not inline: %d %s", name, w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), `"inline":false`) {
			t.Fatalf("inline:false must be explicit: %s", w.Body.String())
		}
	}
	if w = request(b, "GET", path+"/files?path=/etc/passwd", ""); w.Code != 403 || !strings.Contains(w.Body.String(), "outside") {
		t.Fatalf("outside: %d %s", w.Code, w.Body.String())
	}
	if w = request(b, "GET", path+"/files?path=gone.txt", ""); w.Code != 404 {
		t.Fatalf("missing: %d", w.Code)
	}
	if w = request(b, "GET", path+"/files", ""); w.Code != 400 {
		t.Fatalf("no path: %d", w.Code)
	}
}

func TestStatPathsMarksOutsideAndLimits(t *testing.T) {
	b, _, path := richFixture(t, func(c *Connection) {
		c.StatPaths = func(paths []string) ([]remote.PathFact, error) {
			out := []remote.PathFact{}
			for _, p := range paths {
				f := remote.PathFact{Path: p}
				if p == "src/main.go" {
					f.Exists, f.Size, f.ModTime = true, 12, 1700000000
				}
				out = append(out, f)
			}
			return out, nil
		}
	})
	w := request(b, "POST", path+"/files/stat", `{"paths":["src/main.go","missing.txt","/etc/passwd","../up"]}`)
	var facts []PathFact
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &facts) != nil || len(facts) != 4 {
		t.Fatalf("stat: %d %s", w.Code, w.Body.String())
	}
	if !facts[0].Exists || facts[0].Size != 12 || facts[0].ModTime != "2023-11-14T22:13:20Z" || facts[0].Outside {
		t.Fatalf("existing: %+v", facts[0])
	}
	if facts[1].Exists || facts[1].Outside {
		t.Fatalf("missing inside the workspace is not outside: %+v", facts[1])
	}
	if !facts[2].Outside || !facts[3].Outside {
		t.Fatalf("outside not marked: %+v %+v", facts[2], facts[3])
	}
	many, _ := json.Marshal(map[string][]string{"paths": make([]string, 65)})
	if w = request(b, "POST", path+"/files/stat", string(many)); w.Code != 400 {
		t.Fatalf("65 paths: %d", w.Code)
	}
}

func TestTurnWithFilesSortsPicturesFromFiles(t *testing.T) {
	b, a, path := richFixture(t, nil)
	png := base64.StdEncoding.EncodeToString([]byte("png-bytes"))
	txt := base64.StdEncoding.EncodeToString([]byte("log line"))
	body := `{"text":"look","mode":"submit","files":[{"name":"shot.png","mime":"image/png","dataBase64":"` + png + `"},{"name":"run.log","mime":"text/plain","dataBase64":"` + txt + `"}]}`
	w := request(b, "POST", path+"/turn", body)
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.filesText != "look" || len(a.images) != 1 || a.images[0].MIME != "image/png" || string(a.images[0].Bytes) != "png-bytes" {
		t.Fatalf("pictures: %q %+v", a.filesText, a.images)
	}
	if len(a.files) != 1 || a.files[0].Name != "run.log" || string(a.files[0].Bytes) != "log line" {
		t.Fatalf("files: %+v", a.files)
	}
}

func TestTurnWithOversizeAttachmentsIs413(t *testing.T) {
	b, _, path := richFixture(t, nil)
	big := base64.StdEncoding.EncodeToString(make([]byte, 10<<20+1))
	body := `{"text":"x","files":[{"name":"big.png","mime":"image/png","dataBase64":"` + big + `"}]}`
	w := request(b, "POST", path+"/turn", body)
	if w.Code != 413 || !strings.Contains(w.Body.String(), "10MB") {
		t.Fatalf("picture: %d %s", w.Code, w.Body.String())
	}
	half := base64.StdEncoding.EncodeToString(make([]byte, 21<<19))
	one := `{"name":"f.bin","mime":"application/octet-stream","dataBase64":"` + half + `"}`
	body = `{"text":"x","files":[` + one + `,` + one + `]}`
	if w = request(b, "POST", path+"/turn", body); w.Code != 413 || !strings.Contains(w.Body.String(), "20MB") {
		t.Fatalf("total: %d %s", w.Code, w.Body.String())
	}
	huge := strings.Repeat("A", maxTurnBody+10)
	if w = request(b, "POST", path+"/turn", `{"text":"x","files":[{"name":"h","mime":"x/y","dataBase64":"`+huge+`"}]}`); w.Code != 413 {
		t.Fatalf("body: %d", w.Code)
	}
}

func TestQuestionHoldMatchesOpenQuestion(t *testing.T) {
	b, a, path := richFixture(t, nil)
	a.questions = []session.Question{{Kind: session.QuestionConsent, ID: 7, Ref: "ref-7"}, {Kind: session.QuestionTask, ID: 9}}
	if w := request(b, "POST", path+"/questions/hold", `{"kind":"consent","id":7,"ref":"ref-7"}`); w.Code != 200 {
		t.Fatalf("hold: %d %s", w.Code, w.Body.String())
	}
	if w := request(b, "POST", path+"/questions/hold", `{"kind":"task","id":9}`); w.Code != 200 {
		t.Fatalf("hold by id: %d", w.Code)
	}
	if len(a.held) != 2 || a.held[0] != "consent:ref-7" || a.held[1] != "task:9" {
		t.Fatalf("held: %v", a.held)
	}
	if w := request(b, "POST", path+"/questions/hold", `{"kind":"consent","id":8}`); w.Code != 409 {
		t.Fatalf("not open: %d", w.Code)
	}
}

func TestFaviconOnlyForContactedDomains(t *testing.T) {
	b, a, path := richFixture(t, nil)
	a.entries = []session.DisplayEntry{
		{Role: "tool", Tool: "web_fetch", Args: `{"url":"https://docs.example.org/guide"}`},
		{Role: "tool", Tool: "web_search", Args: `{"query":"x"}`, Output: "1. Title\n   https://news.example.net/a?b=1\n"},
		{Role: "tool", Tool: "bash", Args: `{"command":"curl https://sneaky.example.com"}`, Output: "https://sneaky.example.com"},
	}
	var asked []string
	b.icons.fetch = func(_ context.Context, domain string) string {
		asked = append(asked, domain)
		return "data:image/png;base64,AAAA"
	}
	get := func(domain string) string { return request(b, "GET", path+"/favicon?domain="+domain, "").Body.String() }
	if got := get("docs.example.org"); !strings.Contains(got, `"dataUrl":"data:image/png;base64,AAAA"`) {
		t.Fatalf("fetch host: %s", got)
	}
	if got := get("news.example.net"); !strings.Contains(got, "dataUrl") {
		t.Fatalf("result host: %s", got)
	}
	get("docs.example.org")
	for _, other := range []string{"sneaky.example.com", "localhost", "127.0.0.1", "other.org"} {
		if got := strings.TrimSpace(get(other)); got != "{}" {
			t.Fatalf("%s must be refused, got %s", other, got)
		}
	}
	if len(asked) != 2 {
		t.Fatalf("expected two fetches and a cache hit, got %v", asked)
	}
}

func TestFaviconResponseRules(t *testing.T) {
	ok := httptest.NewRecorder()
	ok.Header().Set("Content-Type", "image/x-icon")
	ok.WriteString("icon")
	if got := iconDataURL(ok.Result()); !strings.HasPrefix(got, "data:image/x-icon;base64,") {
		t.Fatalf("icon: %q", got)
	}
	html := httptest.NewRecorder()
	html.Header().Set("Content-Type", "text/html")
	html.WriteString("<html>")
	svg := httptest.NewRecorder()
	svg.Header().Set("Content-Type", "image/svg+xml")
	svg.WriteString("<svg/>")
	big := httptest.NewRecorder()
	big.Header().Set("Content-Type", "image/png")
	big.WriteString(strings.Repeat("x", faviconMaxBytes+1))
	for name, rec := range map[string]*httptest.ResponseRecorder{"html": html, "svg": svg, "big": big} {
		if got := iconDataURL(rec.Result()); got != "" {
			t.Fatalf("%s must be refused", name)
		}
	}
	if err := publicOnly("tcp", "127.0.0.1:443", nil); err == nil {
		t.Fatal("loopback must be refused")
	}
	if err := publicOnly("tcp", "10.0.0.5:443", nil); err == nil {
		t.Fatal("private must be refused")
	}
	if err := publicOnly("tcp", "93.184.216.34:443", nil); err != nil {
		t.Fatal(err)
	}
}

func TestEveryNamedKindIsMapped(t *testing.T) {
	want := map[session.EventKind]string{
		session.EventCaption: "caption", session.EventSteerAccepted: "steerAccepted", session.EventSteerConsumed: "steerConsumed",
		session.EventSteerFellThrough: "steerFellThrough", session.EventToolAnnounced: "toolAnnounced", session.EventToolForming: "toolForming",
		session.EventToolFinished: "toolFinished", session.EventToolOutput: "toolOutput", session.EventRetrying: "retrying",
		session.EventNotice: "notice", session.EventCompacting: "compacting", session.EventCompacted: "compacted",
		session.EventTaskProposal: "taskProposal", session.EventTaskUpdate: "taskUpdate", session.EventTaskPhase: "taskPhase",
		session.EventJobUpdate: "jobUpdate", session.EventQuestionWithdrawn: "questionWithdrawn", session.EventQuestionAnswered: "questionAnswered",
		session.EventTitleChanged: "titleChanged", session.EventTextDelta: "text", session.EventQuestion: "question",
		session.EventNudge: "other",
	}
	for kind, name := range want {
		if got := eventKind(kind); got != name {
			t.Errorf("kind %d is %q, want %q", kind, got, name)
		}
	}
	raw := map[session.EventKind]int{session.EventCaption: 47, session.EventSteerAccepted: 41, session.EventToolOutput: 55, session.EventTaskPhase: 44}
	for kind, number := range raw {
		if int(kind) != number {
			t.Errorf("kind %s is %d on the wire, the UI expects %d", want[kind], int(kind), number)
		}
	}
}

func recordTexts(s *conversation) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var texts []string
	for _, r := range s.records {
		if r.Event != nil {
			texts = append(texts, r.Event.Kind+":"+r.Event.Text)
		}
	}
	return texts
}

func TestFallenThroughSteerPublishesItsFollowUpTurn(t *testing.T) {
	b, a, path := richFixture(t, nil)
	a.steer = make(chan session.Event, 8)
	if w := request(b, "POST", path+"/turn", `{"text":"first"}`); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if w := request(b, "POST", path+"/turn", `{"text":"correction","mode":"steer"}`); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	s := b.sessions[strings.TrimPrefix(path, "/api/engine/sessions/")]
	// The turn ends before the steer lands; both streams see the first turn.
	a.events <- session.Event{Kind: session.EventTextDelta, Text: "first answer"}
	a.events <- session.Event{Kind: session.EventTurnDone}
	close(a.events)
	a.steer <- session.Event{Kind: session.EventTextDelta, Text: "first answer"}
	a.steer <- session.Event{Kind: session.EventTurnDone}
	a.steer <- session.Event{Kind: session.EventSteerFellThrough}
	a.steer <- session.Event{Kind: session.EventTextDelta, Text: "follow-up answer"}
	a.steer <- session.Event{Kind: session.EventTurnDone}
	close(a.steer)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		texts := strings.Join(recordTexts(s), "|")
		if strings.Contains(texts, "text:follow-up answer") {
			if strings.Count(texts, "text:first answer") != 1 {
				t.Fatalf("the first turn must not be published twice: %s", texts)
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("follow-up turn was drained unpublished: %v", recordTexts(s))
}
