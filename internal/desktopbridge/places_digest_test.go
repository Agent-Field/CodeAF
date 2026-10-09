package desktopbridge

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/placegraph"
	"github.com/Agent-Field/codeaf/internal/session"
)

// persistChat writes a real session folder: the meta.json the session package
// itself saves, with the "recap" member the History engine adds to it (the
// persisted shape of internal/session/recap.go's ConversationRecap).
func persistChat(t *testing.T, root, id string, recap map[string]any) session.SessionRow {
	t.Helper()
	dir := filepath.Join(root, id)
	if err := session.SaveMeta(dir, session.Meta{ID: id, Title: "Title " + id, Workspace: "/w", Created: placesEpoch.Add(-72 * time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if recap != nil {
		path := filepath.Join(dir, "meta.json")
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var doc map[string]any
		if err := json.Unmarshal(raw, &doc); err != nil {
			t.Fatal(err)
		}
		doc["recap"] = recap
		raw, _ = json.Marshal(doc)
		if err := os.WriteFile(path, raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	r := row(id, "Title "+id, placesEpoch.Add(-time.Hour))
	r.Dir = dir
	return r
}

func recapAt(line string, age time.Duration) map[string]any {
	return map[string]any{"line": line, "discussed": "long text", "outcome": "Merged.", "updatedAt": placesEpoch.Add(-age).Format(time.RFC3339Nano), "messages": 6, "fingerprint": "6:abc"}
}

func TestHomeRollsUpRealPersistedChildRecapsAndNothingElse(t *testing.T) {
	rig := newPlacesRig(t)
	root := t.TempDir()
	soft := rig.mk("Software")
	cfg := rig.mk("Config parser", soft)
	docs := rig.mk("Docs", soft)
	fresh := persistChat(t, root, "fresh", recapAt("Moved the parser to strict mode", 3*time.Hour))
	need := waiting(persistChat(t, root, "need", recapAt("Needs a yes to push the fix.", 20*time.Hour)), "Allow the push?")
	old := persistChat(t, root, "old", recapAt("Settled something two days ago.", 50*time.Hour))
	bare := persistChat(t, root, "bare", nil)
	shared := persistChat(t, root, "shared", recapAt("Shared by two children.", 2*time.Hour))
	rig.setRows(fresh, need, old, bare, shared)
	rig.do("POST", "/places/"+cfg+"/members", map[string]any{"chats": []string{"fresh", "need", "old", "bare", "shared"}}, nil)
	rig.do("POST", "/places/"+docs+"/members", map[string]any{"chats": []string{"shared"}}, nil)

	var home digestResponse
	rig.do("GET", "/places/"+soft, nil, &home)
	rc := home.Recap
	if rc == nil || rc.Label != "Since yesterday" || rc.Chats != 3 {
		t.Fatalf("the parent rolls up its children's recaps once each: %+v", rc)
	}
	want := "Needs a yes to push the fix. Shared by two children. Moved the parser to strict mode."
	if rc.Text != want {
		t.Fatalf("text %q, want %q", rc.Text, want)
	}
	if rc.Items[0].ChatID != "need" || rc.Items[0].Attention != "needsYou" || rc.Items[0].PlaceName != "Config parser" {
		t.Fatalf("a chat waiting on the person ranks first and is attributed to its child place: %+v", rc.Items[0])
	}
	if strings.Contains(rc.Text, "Title") || strings.Contains(rc.Text, "long text") {
		t.Fatalf("only recap lines are quoted, never titles or the long account: %q", rc.Text)
	}
	// The same words on the child's own Home, minus what lives elsewhere.
	rig.do("GET", "/places/"+docs, nil, &home)
	if home.Recap == nil || home.Recap.Text != "Shared by two children." {
		t.Fatalf("docs: %+v", home.Recap)
	}
}

func TestHomeHasNoSinceYesterdayWithoutEvidence(t *testing.T) {
	rig := newPlacesRig(t)
	root := t.TempDir()
	id := rig.mk("Quiet")
	rig.setRows(persistChat(t, root, "a", nil), persistChat(t, root, "b", recapAt("Two days old.", 49*time.Hour)))
	rig.do("POST", "/places/"+id+"/members", map[string]any{"chats": []string{"a", "b"}}, nil)
	var raw map[string]json.RawMessage
	rig.do("GET", "/places/"+id, nil, &raw)
	if _, ok := raw["recap"]; ok {
		t.Fatalf("recap must be absent, not empty: %s", raw["recap"])
	}
	if _, ok := raw["contextLine"]; ok {
		t.Fatalf("contextLine must be absent for a place that carries nothing: %s", raw["contextLine"])
	}
}

func TestSinceYesterdayAgesOutByTheClockNotByRereadingTranscripts(t *testing.T) {
	rig := newPlacesRig(t)
	root := t.TempDir()
	id := rig.mk("Clocked")
	rig.setRows(persistChat(t, root, "a", recapAt("Fresh enough.", 22*time.Hour)))
	rig.do("POST", "/places/"+id+"/members", map[string]any{"chats": []string{"a"}}, nil)
	var home digestResponse
	rig.do("GET", "/places/"+id, nil, &home)
	if home.Recap == nil || home.Recap.Text != "Fresh enough." {
		t.Fatalf("%+v", home.Recap)
	}
	rig.advance(3 * time.Hour) // the recap is now 25 hours old
	var later digestResponse
	rig.do("GET", "/places/"+id, nil, &later)
	if later.Recap != nil {
		t.Fatalf("a recap older than the window is not news: %+v", home.Recap)
	}
}

func TestSinceYesterdaySurvivesMissingArchivedOfflineAndDamagedChats(t *testing.T) {
	rig := newPlacesRig(t)
	root := t.TempDir()
	id := rig.mk("Messy")
	good := persistChat(t, root, "good", recapAt("The one good recap.", time.Hour))
	archived := persistChat(t, root, "archived", recapAt("Put away.", time.Hour))
	archived.Archived = true
	offline := row("offline", "Offline", placesEpoch) // no Dir: the world could not locate its folder
	garbage := persistChat(t, root, "garbage", nil)
	os.WriteFile(filepath.Join(garbage.Dir, "meta.json"), []byte("{not json"), 0o600)
	mismatch := persistChat(t, root, "mismatch", recapAt("Belongs to someone else.", time.Hour))
	other := persistChat(t, root, "someone-else", recapAt("Not mine.", time.Hour))
	mismatch.Dir = other.Dir // an index that points a chat at another conversation's folder
	badStamp := persistChat(t, root, "badstamp", map[string]any{"line": "Bad stamp.", "updatedAt": "yesterday-ish"})
	future := persistChat(t, root, "future", recapAt("From the future.", -48*time.Hour))
	wrongType := persistChat(t, root, "wrongtype", map[string]any{"line": 7, "updatedAt": placesEpoch.Format(time.RFC3339)})
	big := persistChat(t, root, "big", nil)
	os.WriteFile(filepath.Join(big.Dir, "meta.json"), append([]byte(`{"id":"big","recap":{"line":"x","updatedAt":"`+placesEpoch.Format(time.RFC3339)+`"},"pad":"`), append([]byte(strings.Repeat("a", recapMetaMax)), []byte(`"}`)...)...), 0o600)
	linked := persistChat(t, root, "linked", nil)
	os.Remove(filepath.Join(linked.Dir, "meta.json"))
	outside := filepath.Join(t.TempDir(), "meta.json")
	os.WriteFile(outside, []byte(`{"id":"linked","recap":{"line":"Through a symlink.","updatedAt":"`+placesEpoch.Format(time.RFC3339)+`"}}`), 0o600)
	if err := os.Symlink(outside, filepath.Join(linked.Dir, "meta.json")); err != nil {
		t.Skip("no symlinks here")
	}
	rig.setRows(good, archived, offline, garbage, mismatch, badStamp, future, wrongType, big, linked)
	ids := []string{"good", "archived", "offline", "garbage", "mismatch", "badstamp", "future", "wrongtype", "big", "linked"}
	rig.do("POST", "/places/"+id+"/members", map[string]any{"chats": ids}, nil)
	var home digestResponse
	if code := rig.do("GET", "/places/"+id, nil, &home); code != 200 {
		t.Fatalf("damage must never fail the page: %d", code)
	}
	if home.Recap == nil || home.Recap.Text != "The one good recap." || home.Recap.Chats != 1 {
		t.Fatalf("only the intact recap of a readable, active chat counts: %+v", home.Recap)
	}
}

func TestRecapReaderCachesByStatAndSeesARewrite(t *testing.T) {
	root := t.TempDir()
	now := placesEpoch
	m := newMetaRecaps(func() time.Time { return now })
	row := persistChat(t, root, "c", recapAt("First.", time.Hour))
	path := filepath.Join(row.Dir, "meta.json")
	if r := m.ReadRecap("c", row.Dir); r == nil || r.Line != "First." {
		t.Fatalf("%+v", r)
	}
	info, _ := os.Stat(path)
	// Same size, same mtime, different bytes: the stat says nothing moved, so the
	// cache answers. This proves a GET is a stat and not a read.
	raw, _ := os.ReadFile(path)
	os.WriteFile(path, []byte(strings.Replace(string(raw), "First.", "Other!", 1)), 0o600)
	os.Chtimes(path, info.ModTime(), info.ModTime())
	if r := m.ReadRecap("c", row.Dir); r == nil || r.Line != "First." {
		t.Fatalf("an unchanged stat must not cost a read: %+v", r)
	}
	later := info.ModTime().Add(time.Second)
	os.Chtimes(path, later, later)
	if r := m.ReadRecap("c", row.Dir); r == nil || r.Line != "Other!" {
		t.Fatalf("a recap rewritten by a settled turn is seen on the next read: %+v", r)
	}
	// A conversation that lost its meta stops contributing.
	os.Remove(path)
	if r := m.ReadRecap("c", row.Dir); r != nil {
		t.Fatalf("%+v", r)
	}
}

func TestRecapReaderRefusesPathsOutsideTheConversationFolder(t *testing.T) {
	root := t.TempDir()
	m := newMetaRecaps(func() time.Time { return placesEpoch })
	row := persistChat(t, root, "c", recapAt("Mine.", time.Hour))
	for _, tc := range []struct{ id, dir string }{
		{"", row.Dir}, {"c", ""}, {"../c", row.Dir}, {"c", filepath.Join(row.Dir, "..")}, {"d", row.Dir},
		{"c", row.Dir + string(filepath.Separator) + ".." + string(filepath.Separator) + "c"[0:0] + "x"},
	} {
		if r := m.ReadRecap(tc.id, tc.dir); r != nil {
			t.Errorf("(%q, %q) read a recap: %+v", tc.id, tc.dir, r)
		}
	}
}

func TestRecapCacheStaysBounded(t *testing.T) {
	root := t.TempDir()
	now := placesEpoch
	m := newMetaRecaps(func() time.Time { return now })
	for i := 0; i < recapCacheMax+40; i++ {
		id := "c" + strings.Repeat("x", 1) + time.Duration(i).String()
		r := persistChat(t, root, id, recapAt("Line.", time.Hour))
		m.ReadRecap(id, r.Dir)
		now = now.Add(time.Second)
	}
	if len(m.cache) > recapCacheMax {
		t.Fatalf("cache grew to %d", len(m.cache))
	}
}

func TestHomeReadsAtMostTheNewestChatsMetas(t *testing.T) {
	rig := newPlacesRig(t)
	root := t.TempDir()
	id := rig.mk("Busy")
	var rows []session.SessionRow
	var ids []string
	for i := 0; i < digestReadMax+20; i++ {
		name := "chat" + time.Duration(i).String()
		r := persistChat(t, root, name, recapAt("Line "+name+".", time.Hour))
		r.At = placesEpoch.Add(-time.Duration(i) * time.Minute)
		rows = append(rows, r)
		ids = append(ids, name)
	}
	counting := &countingRecaps{inner: newMetaRecaps(rig.p.now)}
	rig.p.Recaps = counting
	rig.setRows(rows...)
	rig.do("POST", "/places/"+id+"/members", map[string]any{"chats": ids}, nil)
	var home digestResponse
	rig.do("GET", "/places/"+id, nil, &home)
	if counting.n != digestReadMax || home.Recap == nil || home.Recap.Chats != digestReadMax || len(home.Recap.Items) != placegraph.DigestItemsMax {
		t.Fatalf("reads %d, recap %+v", counting.n, home.Recap)
	}
}

type countingRecaps struct {
	inner RecapReader
	n     int
}

func (c *countingRecaps) ReadRecap(id, dir string) *placegraph.DigestRecap {
	c.n++
	return c.inner.ReadRecap(id, dir)
}

func TestContextLineCountsInstructionsSourcesAndWhatIsMissing(t *testing.T) {
	rig := newPlacesRig(t)
	id := rig.mk("Context")
	dir := t.TempDir()
	file := filepath.Join(dir, "voice.md")
	os.WriteFile(file, []byte("hi"), 0o600)
	var m Mutation
	rig.do("POST", "/places/"+id, map[string]any{"instructions": "Be brief."}, &m)
	rig.do("POST", "/places/"+id+"/sources", map[string]any{"kind": "file", "ref": file}, &m)
	gone := filepath.Join(dir, "gone.md")
	os.WriteFile(gone, []byte("x"), 0o600)
	rig.do("POST", "/places/"+id+"/sources", map[string]any{"kind": "file", "ref": gone}, &m)
	os.Remove(gone)
	rig.do("POST", "/places/"+id+"/sources", map[string]any{"kind": "url", "ref": "https://codeaf.dev/docs"}, &m)
	var home digestResponse
	rig.do("GET", "/places/"+id, nil, &home)
	if home.ContextLine != "Instructions · 3 sources · 1 missing" {
		t.Fatalf("%q", home.ContextLine)
	}
	// Instructions alone, one source: singular, and no "missing" clause.
	one := rig.mk("One")
	rig.do("POST", "/places/"+one+"/sources", map[string]any{"kind": "file", "ref": file}, &m)
	rig.do("GET", "/places/"+one, nil, &home)
	if home.ContextLine != "1 source" {
		t.Fatalf("%q", home.ContextLine)
	}
	var root digestResponse
	rig.do("GET", "/places/root", nil, &root)
	if root.ContextLine != "" {
		t.Fatalf("root carries no context: %q", root.ContextLine)
	}
}

func TestRootAndNowHomesRollUpTheirOwnChats(t *testing.T) {
	rig := newPlacesRig(t)
	root := t.TempDir()
	id := rig.mk("Filed")
	filed := persistChat(t, root, "filed", recapAt("Filed work.", time.Hour))
	loose := persistChat(t, root, "loose", recapAt("Unplaced work.", 2*time.Hour))
	rig.setRows(filed, loose)
	rig.do("POST", "/places/"+id+"/members", map[string]any{"chats": []string{"filed"}}, nil)
	var all, now digestResponse
	rig.do("GET", "/places/root", nil, &all)
	rig.do("GET", "/places/now", nil, &now)
	if all.Recap == nil || all.Recap.Text != "Filed work." {
		t.Fatalf("root: %+v", all.Recap)
	}
	if now.Recap == nil || now.Recap.Text != "Unplaced work." {
		t.Fatalf("now: %+v", now.Recap)
	}
}
