package desktopbridge

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Agent-Field/codeaf/internal/placegraph"
)

// changesOn lists the placeChange payloads on the ring of the conversation
// with this bridge token.
func (r *usingRig) changesOn(token string) []PlaceChange {
	r.t.Helper()
	r.b.mu.Lock()
	s := r.b.sessions[token]
	r.b.mu.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []PlaceChange
	for _, rec := range s.records {
		if rec.Event != nil && rec.Event.Kind == "placeChange" {
			out = append(out, *rec.Event.PlaceChange)
		}
	}
	return out
}

func (r *usingRig) placeOf(id string) placegraph.Place {
	r.t.Helper()
	snap, err := r.p.Store.Snapshot()
	if err != nil {
		r.t.Fatal(err)
	}
	pl, found := snap.Place(id)
	if !found {
		r.t.Fatalf("no place %s", id)
	}
	return pl
}

// placeWithFiles is a place listing one file source per name.
func (r *usingRig) placeWithFiles(name string, files ...string) string {
	r.t.Helper()
	id := r.policyPlace(name, placegraph.Policy{})
	dir := r.t.TempDir()
	c := r.placeOf(id).Context
	for _, f := range files {
		path := filepath.Join(dir, f)
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			r.t.Fatal(err)
		}
		src, err := placegraph.NewSource(placegraph.SourceFile, path, placegraph.AddedByYou, r.door.Sources)
		if err != nil {
			r.t.Fatal(err)
		}
		c, _ = placegraph.WithSource(c, src)
	}
	if _, err := r.p.Store.SetContext(id, c); err != nil {
		r.t.Fatal(err)
	}
	return id
}

func (r *usingRig) fileUnder(place string, chats ...string) Mutation {
	r.t.Helper()
	var m Mutation
	if code := r.do("POST", "/places/"+place+"/members", map[string]any{"chats": chats}, &m); code != 200 {
		r.t.Fatalf("file: %d", code)
	}
	return m
}

func TestAddingAPlacePublishesOneLineOnThatConversationOnly(t *testing.T) {
	r := newUsingRig(t)
	one, _, _ := r.open(nil)
	r.chat, r.file = "fedcba9876543210", filepath.Join(t.TempDir(), "fedcba9876543210", "session.jsonl")
	two, _, _ := r.open(nil)
	place := r.placeWithFiles("Release", "brand-voice.md")

	m := r.fileUnder(place, "0123456789abcdef")

	got := r.changesOn(one)
	if len(got) != 1 || !got[0].Added || got[0].PlaceName != "Release" || got[0].PlaceID != place || got[0].Undo != m.Undo[0] {
		t.Fatalf("one heard %+v, undo %v", got, m.Undo)
	}
	if !reflect.DeepEqual(got[0].Sources, []string{"brand-voice.md"}) {
		t.Fatalf("sources %v", got[0].Sources)
	}
	if other := r.changesOn(two); len(other) != 0 {
		t.Fatalf("the other conversation heard %+v", other)
	}
}

func TestTheLineNamesOnlyNewlyContributedSources(t *testing.T) {
	r := newUsingRig(t)
	token, _, _ := r.open(nil)
	first := r.placeWithFiles("Brand", "a.md")
	r.fileChat(first)
	// The second place lists its own file and a.md's twin: the chat already has a.md.
	shared := r.placeWithFiles("Release", "b.md", "c.md", "d.md", "e.md", "f.md")
	c, _ := placegraph.WithSource(r.placeOf(shared).Context, r.placeOf(first).Context.Sources[0])
	if _, err := r.p.Store.SetContext(shared, c); err != nil {
		t.Fatal(err)
	}

	r.fileUnder(shared, r.chat)

	got := r.changesOn(token)
	if len(got) != 1 {
		t.Fatalf("heard %+v", got)
	}
	want := []string{"b.md", "c.md", "d.md", "2 more"}
	if !reflect.DeepEqual(got[0].Sources, want) {
		t.Fatalf("sources %v, want %v", got[0].Sources, want)
	}
}

func TestRemovalLineHasNoSources(t *testing.T) {
	r := newUsingRig(t)
	token, _, _ := r.open(nil)
	place := r.placeWithFiles("Release", "brand-voice.md")
	r.fileChat(place)

	var m Mutation
	if code := r.do("POST", "/places/"+place+"/members/remove", map[string]any{"chats": []string{r.chat}}, &m); code != 200 {
		t.Fatalf("remove: %d", code)
	}

	got := r.changesOn(token)
	if len(got) != 1 || got[0].Added || len(got[0].Sources) != 0 || got[0].Sources == nil || got[0].Undo != m.Undo[0] {
		t.Fatalf("heard %+v", got)
	}
}
