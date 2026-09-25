package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/teams"
)

// A CONFLICT BETWEEN MEMBERS OF TWO SIBLING SUB-TEAMS LANDS AT THEIR COMMON
// PARENT'S MANAGER, WHICH RULES, AND BOTH PARTIES RECEIVE THE RULING, end to
// end through the engine's own [bootEngine], with nothing set that a person
// does not set. Three conversations run on the engine: @web in the sub-team
// front, @api in its sibling back, and @boss, the manager of harbor, which
// both sit under (each sub-team's own manager is a conversation nobody has
// open). The model endpoint is scripted to do what each would: @web raises the
// conflict with `team_raise`; @boss, woken by the packet, rules on it with
// `team_decide`. Nobody types to @boss or @api, and the person is never
// asked: both parties are woken by the ruling, marked as one.
func TestTeamConflictBetweenSiblingSubTeamsIsRuledByTheirCommonManager(t *testing.T) {
	root := resolvedTempDir(t)
	_, workspace := wakeEnvironment(t, root)
	script := newConflictModel(t)
	t.Setenv("CODEAF_BASE_URL", script.server.URL)

	boot := func() (*remote.Engine, string) {
		t.Helper()
		engine, err := bootEngine(remote.Hello{Version: remote.Version, Workspace: workspace, New: true}, "", "")
		if err != nil {
			t.Fatalf("the engine door did not open: %v", err)
		}
		t.Cleanup(func() { _ = engine.Agent.Close() })
		file := strings.TrimSpace(engine.SessionFile)
		if file == "" {
			t.Fatal("the engine named no transcript")
		}
		return engine, file
	}
	web, webFile := boot()
	_, apiFile := boot()
	_, bossFile := boot()
	if webFile == apiFile || apiFile == bossFile {
		t.Fatal("the engine handed two hellos one conversation")
	}

	lead := filepath.Join(root, "elsewhere", "lead.jsonl")
	chief := filepath.Join(root, "elsewhere", "chief.jsonl")
	harbor, front, back := teams.NewID(), teams.NewID(), teams.NewID()
	member := func(file, word, handle string) teams.Member {
		return teams.Member{Key: filepath.Clean(file), File: file, Where: workspace, Word: word, Handle: handle}
	}
	err := teams.Update("", func(f *teams.File) error {
		f.Teams = append(f.Teams,
			teams.Team{ID: harbor, Name: "harbor"},
			teams.Team{ID: front, Name: "front", Parent: harbor},
			teams.Team{ID: back, Name: "back", Parent: harbor})
		for _, add := range []struct {
			team    string
			members []teams.Member
			manager string
		}{
			{harbor, []teams.Member{member(bossFile, "harbor manager", "boss"), member(lead, "front lead", "lead"), member(chief, "back chief", "chief")}, bossFile},
			{front, []teams.Member{member(lead, "front lead", "lead"), member(webFile, "web frontend", "web")}, lead},
			{back, []teams.Member{member(chief, "back chief", "chief"), member(apiFile, "signup api", "api")}, chief},
		} {
			for _, m := range add.members {
				if err := f.AddMember(add.team, m); err != nil {
					return err
				}
			}
			if err := f.SetManager(add.team, filepath.Clean(add.manager)); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("make the teams: %v", err)
	}

	events, err := web.Agent.Submit(t.Context(), "RAISE the conflict over the signup form")
	if err != nil {
		t.Fatalf("the turn did not start: %v", err)
	}
	for range events {
	}

	waiting, _, err := teams.OpenPackets("", harbor)
	if err != nil || len(waiting) != 1 {
		t.Fatalf("no conflict waits on harbor's manager: %+v %v\nthe model saw:\n%s", waiting, err, script.last())
	}
	p := waiting[0]
	if p.Kind != teams.PacketConflict || p.Origin != front || len(p.Parties) != 2 || p.Parties[1].Team != back {
		t.Fatalf("the packet: %+v", p)
	}
	if mine, _, _ := teams.OpenPackets("", teams.Person); len(mine) != 0 {
		t.Fatalf("the conflict reached the person: %+v", mine)
	}

	// @boss is woken by the packet and rules; the script decides option 1.
	deadline := time.Now().Add(40 * time.Second)
	for {
		got, err := teams.PacketByID("", p.ID)
		if err == nil && got.State == teams.PacketDecided {
			if got.DecidedBy != "boss" || got.Decision != "1" {
				t.Fatalf("decided %+v", got)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("harbor's manager never ruled; the model last saw:\n%s", script.last())
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Both parties are woken with the ruling, each in its own team.
	script.waitForBoth(t, `Team traffic in "front" for you (@web)`, "◆ ruling on the conflict "+p.ID, 30*time.Second)
	script.waitForBoth(t, `Team traffic in "back" for you (@api)`, "◆ ruling on the conflict "+p.ID, 30*time.Second)
	script.waitForBoth(t, `Team traffic in "back" for you (@api)`, "JSON: the handler changes", time.Second)
}

// conflictModel answers @web's turn with `team_raise`, @boss's woken turn with
// `team_decide` on the packet it was handed, and everything else with "ok".
type conflictModel struct {
	server *httptest.Server
	mu     sync.Mutex
	seen   []recordedRequest
}

var conflictPacketID = regexp.MustCompile(`◆ conflict (p[0-9a-f]{12}) from @web, waiting on you`)

func newConflictModel(t *testing.T) *conflictModel {
	t.Helper()
	m := &conflictModel{}
	raise := `{"question":"which shape does the signup form send?","parties":["back/@api"],"context":"the form posts JSON",` +
		`"options":[{"label":"JSON","consequence":"the handler changes"},{"label":"form data","consequence":"the form changes"}],"recommend":"1","reason":"the other endpoints take JSON"}`
	m.server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if !strings.HasSuffix(request.URL.Path, "/chat/completions") {
			http.Error(writer, "no catalog for a test", http.StatusServiceUnavailable)
			return
		}
		raw, _ := io.ReadAll(request.Body)
		var envelope recordedRequest
		if err := json.Unmarshal(raw, &envelope); err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)
			return
		}
		m.mu.Lock()
		m.seen = append(m.seen, envelope)
		m.mu.Unlock()
		all := strings.Join(envelope.texts(), "\n")
		switch {
		case len(envelope.Tools) == 0:
			writeText(writer, envelope.Stream, "ok")
		case strings.Contains(all, "RAISE the conflict") && !strings.Contains(all, "Raised conflict") && !strings.Contains(all, "could not be raised"):
			writeToolCall(writer, envelope.Stream, "r1", "team_raise", raise)
		case conflictPacketID.MatchString(all) && !strings.Contains(all, "Decided p"):
			id := conflictPacketID.FindStringSubmatch(all)[1]
			writeToolCall(writer, envelope.Stream, "d1", "team_decide", `{"packet":"`+id+`","answer":"1","reason":"the other endpoints take JSON"}`)
		default:
			writeText(writer, envelope.Stream, "ok")
		}
	}))
	t.Cleanup(m.server.Close)
	return m
}

// waitForBoth waits until one request carried both a and b.
func (m *conflictModel) waitForBoth(t *testing.T, a, b string, within time.Duration) {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		m.mu.Lock()
		for _, request := range m.seen {
			if request.messageContaining(a) != "" && request.messageContaining(b) != "" {
				m.mu.Unlock()
				return
			}
		}
		m.mu.Unlock()
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("no request carried both %q and %q; the last was:\n%s", a, b, m.last())
}

func (m *conflictModel) last() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.seen) == 0 {
		return "(nothing)"
	}
	return m.seen[len(m.seen)-1].allText()
}
