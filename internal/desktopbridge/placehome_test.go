package desktopbridge

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
)

type homeChatWire struct {
	ChatID       string    `json:"chatId"`
	SessionFile  string    `json:"sessionFile"`
	Title        string    `json:"title"`
	Digest       string    `json:"digest"`
	State        string    `json:"state"`
	TasksRunning int       `json:"tasksRunning"`
	At           time.Time `json:"at"`
}

type homeWire struct {
	Place *struct {
		ID           string    `json:"id"`
		Name         string    `json:"name"`
		LastOpenedAt time.Time `json:"lastOpenedAt"`
	} `json:"place"`
	Breadcrumb []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"breadcrumb"`
	Since *struct {
		Text   string    `json:"text"`
		Anchor time.Time `json:"anchor"`
	} `json:"since"`
	Attention []struct {
		ChatID        string    `json:"chatId"`
		Title         string    `json:"title"`
		OriginPlaceID string    `json:"originPlaceId"`
		State         string    `json:"state"`
		StartedAt     time.Time `json:"startedAt"`
	} `json:"attention"`
	Children []struct {
		ID            string   `json:"id"`
		Name          string   `json:"name"`
		EffectiveTint string   `json:"effectiveTint"`
		NeedsYou      int      `json:"needsYou"`
		Failed        int      `json:"failed"`
		Chats         int      `json:"chats"`
		ChildPlaces   int      `json:"childPlaces"`
		AlsoIn        []string `json:"alsoIn"`
	} `json:"children"`
	Chats    []homeChatWire `json:"chats"`
	Unplaced []homeChatWire `json:"unplaced"`
}

func homeGet(t *testing.T, rig *placesRig, id string) (homeWire, map[string]json.RawMessage) {
	t.Helper()
	var raw map[string]json.RawMessage
	if code := rig.do("GET", "/places/"+id+"/home", nil, &raw); code != 200 {
		t.Fatalf("GET /places/%s/home = %d", id, code)
	}
	var home homeWire
	body, _ := json.Marshal(raw)
	if err := json.Unmarshal(body, &home); err != nil {
		t.Fatal(err)
	}
	return home, raw
}

func chatByID(chats []homeChatWire, id string) (homeChatWire, bool) {
	for _, c := range chats {
		if c.ChatID == id {
			return c, true
		}
	}
	return homeChatWire{}, false
}

func withFile(r session.SessionRow) session.SessionRow {
	if r.Dir != "" {
		r.Transcript = filepath.Join(r.Dir, "transcript.jsonl")
	}
	return r
}

func failedChat(r session.SessionRow, taskID string, started time.Time) session.SessionRow {
	r.Tasks = session.TaskRollup{Failed: 1, Rows: []session.TaskIndexEntry{{
		ID: taskID, Label: "migrate", Title: "migrate", Status: string(session.TaskFailed), StartedAt: started,
	}}}
	return r
}

func TestHomeRollsUpDescendantAttentionWithItsOrigin(t *testing.T) {
	rig := newPlacesRig(t)
	root := t.TempDir()
	soft := rig.mk("Software")
	cfg := rig.mk("Config parser", soft)
	lexer := rig.mk("Lexer", cfg)
	docs := rig.mk("Docs", soft)
	other := rig.mk("Elsewhere")
	release := rig.mk("Release", soft, other)

	started := placesEpoch.Add(-2 * time.Minute)
	need := withFile(waiting(persistChat(t, root, "need", recapAt("Needs a yes.", time.Hour)), "Allow the push?"))
	run := withFile(working(persistChat(t, root, "run", recapAt("Fixtures are updating.", time.Hour)), "task-1", "Update fixtures", started))
	fail := withFile(failedChat(persistChat(t, root, "fail", nil), "f1", started.Add(-time.Hour)))
	both := withFile(waiting(persistChat(t, root, "both", nil), "Which name?"))
	here := withFile(waiting(persistChat(t, root, "here", nil), "Look here"))
	quiet := withFile(persistChat(t, root, "quiet", recapAt("Nothing pending.", time.Hour)))
	out := withFile(waiting(persistChat(t, root, "out", nil), "Not in this tree"))
	rig.setRows(need, run, fail, both, here, quiet, out)
	rig.do("POST", "/places/"+cfg+"/members", map[string]any{"chats": []string{"need", "fail"}}, nil)
	rig.do("POST", "/places/"+lexer+"/members", map[string]any{"chats": []string{"run"}}, nil)
	rig.do("POST", "/places/"+soft+"/members", map[string]any{"chats": []string{"both", "here", "quiet"}}, nil)
	rig.do("POST", "/places/"+cfg+"/members", map[string]any{"chats": []string{"both"}}, nil)
	rig.do("POST", "/places/"+other+"/members", map[string]any{"chats": []string{"out"}}, nil)

	home, _ := homeGet(t, rig, soft)
	if home.Place == nil || home.Place.ID != soft || home.Place.Name != "Software" {
		t.Fatalf("place = %+v", home.Place)
	}
	want := map[string]struct {
		origin, state string
		started       time.Time
	}{
		"need": {cfg, "needsYou", time.Time{}},
		"run":  {lexer, "running", started},
		"fail": {cfg, "failed", started.Add(-time.Hour)},
		"both": {cfg, "needsYou", time.Time{}},
		"here": {soft, "needsYou", time.Time{}},
	}
	if len(home.Attention) != len(want) {
		t.Fatalf("attention = %+v", home.Attention)
	}
	for _, row := range home.Attention {
		w, ok := want[row.ChatID]
		if !ok {
			t.Fatalf("unexpected attention row %+v", row)
		}
		if row.OriginPlaceID != w.origin || row.State != w.state || !row.StartedAt.Equal(w.started) {
			t.Fatalf("%s = origin %s state %s started %s, want origin %s state %s started %s", row.ChatID, row.OriginPlaceID, row.State, row.StartedAt, w.origin, w.state, w.started)
		}
		if row.ChatID == "need" && row.Title != "Title need" {
			t.Fatalf("title %q", row.Title)
		}
		delete(want, row.ChatID)
	}
	gotChat := map[string]bool{}
	for _, c := range home.Chats {
		gotChat[c.ChatID] = true
	}
	for _, id := range []string{"both", "here", "quiet"} {
		if !gotChat[id] {
			t.Fatalf("direct chat %s missing from %+v", id, home.Chats)
		}
	}
	for _, id := range []string{"need", "run", "fail", "out"} {
		if gotChat[id] {
			t.Fatalf("descendant or outside chat %s is listed as filed here", id)
		}
	}

	var sawCfg, sawRelease, sawDocs bool
	for i := range home.Children {
		switch home.Children[i].ID {
		case cfg:
			sawCfg = true
			if home.Children[i].NeedsYou != 2 || home.Children[i].Failed != 1 || home.Children[i].Chats != 4 || home.Children[i].ChildPlaces != 1 {
				t.Fatalf("config parser tile = %+v", home.Children[i])
			}
			if home.Children[i].EffectiveTint == "" {
				t.Fatal("tile has no tint")
			}
		case release:
			sawRelease = true
			if len(home.Children[i].AlsoIn) != 1 || home.Children[i].AlsoIn[0] != "Elsewhere" {
				t.Fatalf("also in = %v", home.Children[i].AlsoIn)
			}
		case docs:
			sawDocs = true
			if home.Children[i].NeedsYou != 0 || home.Children[i].Failed != 0 || home.Children[i].Chats != 0 {
				t.Fatalf("quiet tile = %+v", home.Children[i])
			}
		}
	}
	if !sawCfg || !sawRelease || !sawDocs {
		t.Fatalf("tiles = %+v", home.Children)
	}

	lexerHome, _ := homeGet(t, rig, lexer)
	if len(lexerHome.Breadcrumb) != 2 || lexerHome.Breadcrumb[0].Name != "Software" || lexerHome.Breadcrumb[0].ID != soft ||
		lexerHome.Breadcrumb[1].Name != "Config parser" || lexerHome.Breadcrumb[1].ID != cfg {
		t.Fatalf("breadcrumb = %+v", lexerHome.Breadcrumb)
	}
}

func TestHomeDigestIsTheRecapLineOrNothing(t *testing.T) {
	rig := newPlacesRig(t)
	root := t.TempDir()
	id := rig.mk("Named")
	started := placesEpoch.Add(-5 * time.Minute)
	said := withFile(persistChat(t, root, "said", recapAt("Decided lower-case everywhere", time.Hour)))
	said.At = placesEpoch.Add(-time.Hour)
	empty := withFile(persistChat(t, root, "empty", map[string]any{
		"line": "  ", "discussed": "The long account that must not be the digest.",
		"updatedAt": placesEpoch.Format(time.RFC3339Nano), "messages": 4,
	}))
	empty.At = placesEpoch.Add(-2 * time.Hour)
	none := withFile(persistChat(t, root, "none", nil))
	none.At = placesEpoch.Add(-3 * time.Hour)
	run := withFile(working(persistChat(t, root, "run", recapAt("Four tasks are out.", time.Hour)), "t1", "Update fixtures", started))
	run.At = placesEpoch
	rig.setRows(said, empty, none, run)
	rig.do("POST", "/places/"+id+"/members", map[string]any{"chats": []string{"said", "empty", "none", "run"}}, nil)

	home, raw := homeGet(t, rig, id)
	if _, ok := raw["unplaced"]; ok {
		t.Fatalf("a place home has no unplaced list: %s", raw["unplaced"])
	}
	var chats []map[string]json.RawMessage
	if err := json.Unmarshal(raw["chats"], &chats); err != nil {
		t.Fatal(err)
	}
	byID := map[string]map[string]json.RawMessage{}
	for _, c := range chats {
		var id string
		json.Unmarshal(c["chatId"], &id)
		byID[id] = c
	}
	if string(byID["said"]["digest"]) != `"Decided lower-case everywhere"` {
		t.Fatalf("digest = %s", byID["said"]["digest"])
	}
	if _, ok := byID["said"]["state"]; ok {
		t.Fatalf("an idle chat has no state: %s", byID["said"]["state"])
	}
	if _, ok := byID["empty"]["digest"]; ok {
		t.Fatalf("a blank recap is nothing, not the long account: %s", byID["empty"]["digest"])
	}
	if _, ok := byID["none"]["digest"]; ok {
		t.Fatalf("a chat with no recap has no digest: %s", byID["none"]["digest"])
	}
	runChat, ok := chatByID(home.Chats, "run")
	if !ok || runChat.Digest != "Four tasks are out." || runChat.State != "running" || runChat.TasksRunning != 1 || runChat.SessionFile == "" || runChat.Title != "Title run" {
		t.Fatalf("running chat = %+v", runChat)
	}
	if runChat.Digest == runChat.Title {
		t.Fatal("the digest is the recap, not the title")
	}
	if len(home.Breadcrumb) != 0 {
		t.Fatalf("a top-level place has no ancestor: %+v", home.Breadcrumb)
	}
}

func TestSinceIsOmittedWhenNothingChanged(t *testing.T) {
	rig := newPlacesRig(t)
	root := t.TempDir()
	id := rig.mk("Visited")
	fresh := withFile(persistChat(t, root, "fresh", recapAt("Newest recap. And a second.", time.Hour)))
	fresh.At = placesEpoch.Add(-2 * time.Hour)
	mid := withFile(persistChat(t, root, "mid", recapAt("Middle recap.", time.Hour)))
	mid.At = placesEpoch.Add(-3 * time.Hour)
	bare := withFile(persistChat(t, root, "bare", nil))
	bare.At = placesEpoch
	rig.setRows(fresh, mid, bare)
	rig.do("POST", "/places/"+id+"/members", map[string]any{"chats": []string{"fresh", "mid", "bare"}}, nil)

	if _, raw := homeGet(t, rig, id); raw["since"] != nil {
		t.Fatalf("a place never opened has no since: %s", raw["since"])
	}
	if code := rig.do("POST", "/places/"+id+"/visit", nil, nil); code != 200 {
		t.Fatalf("visit = %d", code)
	}
	home, raw := homeGet(t, rig, id)
	if raw["since"] != nil {
		t.Fatalf("nothing has changed since the visit: %s", raw["since"])
	}
	if home.Place == nil || home.Place.LastOpenedAt.IsZero() {
		t.Fatal("the visit did not stamp the place")
	}
	visited := home.Place.LastOpenedAt
	bare.At = visited.Add(3 * time.Hour)
	fresh.At = visited.Add(2 * time.Hour)
	mid.At = visited.Add(time.Hour)
	rig.setRows(fresh, mid, bare)

	home, raw = homeGet(t, rig, id)
	if home.Since == nil || home.Since.Text != "Newest recap. And a second." {
		t.Fatalf("since = %+v (raw %s)", home.Since, raw["since"])
	}
	if !home.Since.Anchor.Equal(visited) {
		t.Fatalf("anchor %s, visit %s", home.Since.Anchor, visited)
	}
	if home.Since.Text == "Middle recap." || len(home.Since.Text) > len("Newest recap. And a second.") {
		t.Fatalf("since kept more than two sentences: %q", home.Since.Text)
	}
	rig.advance(4 * time.Hour)
	rig.do("POST", "/places/"+id+"/visit", nil, nil)
	if _, raw = homeGet(t, rig, id); raw["since"] != nil {
		t.Fatalf("a visit after the chats omits since: %s", raw["since"])
	}
}

func TestRootHomeListsUnplacedChats(t *testing.T) {
	rig := newPlacesRig(t)
	root := t.TempDir()
	id := rig.mk("Filed")
	filed := withFile(persistChat(t, root, "filed", recapAt("Filed work.", time.Hour)))
	filed.At = placesEpoch.Add(-2 * time.Hour)
	loose := withFile(persistChat(t, root, "loose", recapAt("Unplaced work.", time.Hour)))
	loose.At = placesEpoch
	bare := withFile(persistChat(t, root, "bare", nil))
	bare.At = placesEpoch.Add(-time.Hour)
	rig.setRows(filed, loose, bare)
	rig.do("POST", "/places/"+id+"/members", map[string]any{"chats": []string{"filed"}}, nil)

	home, raw := homeGet(t, rig, "root")
	if _, ok := raw["place"]; ok {
		t.Fatalf("All places is not a stored place: %s", raw["place"])
	}
	if _, ok := raw["since"]; ok {
		t.Fatalf("All places has no visit to be since: %s", raw["since"])
	}
	if _, ok := raw["chats"]; ok {
		t.Fatalf("All places lists unplaced chats, not a chats array: %s", raw["chats"])
	}
	if string(raw["breadcrumb"]) != "[]" {
		t.Fatalf("breadcrumb = %s", raw["breadcrumb"])
	}
	if len(home.Unplaced) != 2 {
		t.Fatalf("unplaced = %+v", home.Unplaced)
	}
	if home.Unplaced[0].ChatID != "loose" || home.Unplaced[0].Digest != "Unplaced work." || home.Unplaced[0].SessionFile == "" {
		t.Fatalf("newest unplaced = %+v", home.Unplaced[0])
	}
	if home.Unplaced[1].ChatID != "bare" {
		t.Fatalf("order = %+v", home.Unplaced)
	}
	var bareRaw map[string]json.RawMessage
	var list []map[string]json.RawMessage
	json.Unmarshal(raw["unplaced"], &list)
	for _, c := range list {
		var cid string
		json.Unmarshal(c["chatId"], &cid)
		if cid == "filed" {
			t.Fatal("a filed chat is not unplaced")
		}
		if cid == "bare" {
			bareRaw = c
		}
	}
	if _, ok := bareRaw["digest"]; ok {
		t.Fatalf("unplaced chat with no recap has no digest: %s", bareRaw["digest"])
	}
	found := false
	for _, child := range home.Children {
		if child.ID == id && child.Name == "Filed" && child.Chats == 1 {
			found = true
		}
	}
	if !found {
		t.Fatalf("top-level tiles = %+v", home.Children)
	}
	if _, placeRaw := homeGet(t, rig, id); placeRaw["unplaced"] != nil {
		t.Fatalf("a place home must not list unplaced: %s", placeRaw["unplaced"])
	}
	if code := rig.do("GET", "/places/now/home", nil, nil); code != 404 {
		t.Fatalf("now has no home page: %d", code)
	}
	if code := rig.do("GET", "/places/pl_00000000000000ff/home", nil, nil); code != 404 {
		t.Fatalf("missing place = %d", code)
	}
}
