package anchors

import (
	"context"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Agent-Field/codeaf/internal/placeorg"
)

func derive(t *testing.T, chats []placeorg.Chat, names ...string) map[string]placeorg.AnchorSet {
	t.Helper()
	got, err := (Derived{}).Anchors(context.Background(), chats, names)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestIssueAndPullReferences(t *testing.T) {
	chats := []placeorg.Chat{{ID: "a", Title: "Fix #7", Text: "#00123 Org/Widget#123 https://github.com/Org/Widget/issues/123?view=1 https://github.com/Org/Widget/pull/42#discussion"}}
	got := derive(t, chats)["a"]
	want := placeorg.AnchorSet{"#7": 1, "#123": 1, "org/widget#123": 1, "#42": 1, "org/widget#42": 1}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("references = %v, want %v", got, want)
	}
}

func TestReferenceBoundaries(t *testing.T) {
	got := derive(t, []placeorg.Chat{{ID: "a", Text: "word#123 #123abc ##123 #0 https://example.org/team/tool/issues/456abc (#8), #999999"}})["a"]
	if !reflect.DeepEqual(got, placeorg.AnchorSet{"#8": 1, "#999999": 1}) {
		t.Fatalf("malformed references became anchors: %v", got)
	}
}

func TestIssueURLsWithoutARepositoryAndWithRouting(t *testing.T) {
	got := derive(t, []placeorg.Chat{{ID: "a", Text: "https://forge.example/issues/8?view=1 https://forge.example/team/widget/-/issues/9#note https://forge.example/team/widget/pull/10/files"}})["a"]
	want := placeorg.AnchorSet{"#8": 1, "#9": 1, "team/widget#9": 1, "#10": 1, "team/widget#10": 1}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("issue paths = %v, want %v", got, want)
	}
}

func TestWorkspaceFamilies(t *testing.T) {
	for _, tc := range []struct{ path, want string }{
		{" /srv/Orchid-553/../Orchid-554/ ", "orchid"},
		{"/srv/Orchid_abcdef0", "orchid"},
		{"/srv/Orchid-550e8400-e29b-41d4-a716-446655440000", "orchid"},
		{"/srv/mission-control", "mission-control"},
		{"/srv/project-v2", "project-v2"},
		{"/srv/worktrees-553", ""},
		{"/tmp", ""}, {"/", ""}, {"relative/project", ""}, {"", ""},
		{"/home/person", ""}, {"/Users/person", ""},
		{"/tmp/550e8400-e29b-41d4-a716-446655440000", ""},
	} {
		t.Run(tc.path, func(t *testing.T) {
			if got := workspaceFamily(tc.path); got != tc.want {
				t.Fatalf("family = %q, want %q", got, tc.want)
			}
		})
	}
	chats := []placeorg.Chat{{ID: "a", Workspace: "/srv/Orchid-553"}, {ID: "b", Workspace: "/different/Orchid-554"}}
	got := derive(t, chats)
	if got["a"]["workspace:orchid"] != 1 || !reflect.DeepEqual(got["a"], got["b"]) {
		t.Fatalf("family did not join worktrees: %v", got)
	}
}

func TestSyntheticAnchorFixture(t *testing.T) {
	var fixture struct {
		Chats          []placeorg.Chat
		ConfirmedNames []string
		Want           map[string][]string
	}
	readJSON(t, filepath.Join("testdata", "anchor-kinds.json"), &fixture)
	got := derive(t, fixture.Chats, fixture.ConfirmedNames...)
	for id, keys := range fixture.Want {
		if len(got[id]) != len(keys) {
			t.Errorf("%s: got %d anchors, want %d", id, len(got[id]), len(keys))
		}
		for _, key := range keys {
			if got[id][key] <= 0 {
				t.Errorf("%s: missing anchor %q", id, key)
			}
		}
	}
}

func TestRepositoryIdentity(t *testing.T) {
	got := derive(t, []placeorg.Chat{{ID: "a", RepoKey: " stable-id "}, {ID: "b", RepoKey: "stable-id"}, {ID: "c"}})
	want := float32(1 + math.Log(4.0/3))
	if got["a"]["repo:stable-id"] != want || got["b"]["repo:stable-id"] != want || len(got["c"]) != 0 {
		t.Fatalf("repository identity = %v", got)
	}
}

func TestProperTokensUseAllSpellingsAndDocumentFrequency(t *testing.T) {
	chats := blankChats(100)
	for i := range 3 {
		chats[i].Text = "orchid riverStone lunar-map ordinary"
	}
	chats[0].Title = "Orchid"
	for i := range 5 {
		chats[i].Text += " Boundary"
	}
	for i := range 6 {
		chats[i].Text += " Frequent"
	}
	for i := range 2 {
		chats[i].Text += " Rare"
	}
	got := derive(t, chats)
	for i, chat := range chats {
		set := got[chat.ID]
		for _, token := range []string{"orchid", "riverstone", "lunar-map"} {
			if (set[token] > 0) != (i < 3) {
				t.Errorf("chat %d: token %s = %v", i, token, set[token])
			}
		}
		if (set["boundary"] > 0) != (i < 5) || set["frequent"] != 0 || set["rare"] != 0 || set["ordinary"] != 0 {
			t.Errorf("chat %d: frequency gate = %v", i, set)
		}
	}
}

func TestFrequencyDoesNotCountOnlyCapitalisedMentions(t *testing.T) {
	chats := blankChats(100)
	chats[0].Title = "Orchid"
	for i := range 6 {
		chats[i].Text = "orchid"
	}
	for id, set := range derive(t, chats) {
		if set["orchid"] != 0 {
			t.Fatalf("%s: lowercase mentions escaped the upper frequency bound", id)
		}
	}
}

func TestConfirmedPlaceNamesMatchWholeTokens(t *testing.T) {
	chats := []placeorg.Chat{{ID: "a", Text: "orchid research-tools ORCHID"}, {ID: "b", Title: "Orchidology toolshed"}}
	got := derive(t, chats, "Orchid Research-tools")
	want := float32(1 + math.Log(3.0/2))
	if !reflect.DeepEqual(got["a"], placeorg.AnchorSet{"orchid": want, "research-tools": want}) || len(got["b"]) != 0 {
		t.Fatalf("confirmed names = %v", got)
	}
}

func TestWeightsCountDocumentsRatherThanMentions(t *testing.T) {
	chats := []placeorg.Chat{{ID: "a", Title: "#123", Text: "#123 #123 #7"}, {ID: "b", Text: "#123"}, {ID: "c"}}
	before := append([]placeorg.Chat(nil), chats...)
	got := derive(t, chats)
	if got["a"]["#123"] != float32(1+math.Log(4.0/3)) || got["a"]["#7"] != float32(1+math.Log(2)) {
		t.Fatalf("IDF = %v", got)
	}
	if !reflect.DeepEqual(chats, before) || !reflect.DeepEqual(got, derive(t, chats)) {
		t.Fatal("anchor derivation mutated its input or was nondeterministic")
	}
}

func TestEmptyCorpusAndCancellation(t *testing.T) {
	if got := derive(t, nil); len(got) != 0 {
		t.Fatalf("empty corpus = %v", got)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got, err := (Derived{}).Anchors(ctx, blankChats(10), nil)
	if got != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled derivation = %v, %v", got, err)
	}
	if _, err := (Derived{}).Anchors(ctx, nil, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled empty corpus: %v", err)
	}
}

func blankChats(n int) []placeorg.Chat {
	chats := make([]placeorg.Chat, n)
	for i := range chats {
		chats[i].ID = fmt.Sprint(i)
	}
	return chats
}

func BenchmarkTenThousandChats(b *testing.B) {
	chats := blankChats(10000)
	for i := range chats {
		chats[i].Text = fmt.Sprintf("Review Orchid-%d and #123 in team/widget#42", i%400)
		chats[i].Workspace = "/srv/Orchid-553"
	}
	b.ResetTimer()
	for range b.N {
		if _, err := (Derived{}).Anchors(context.Background(), chats, nil); err != nil {
			b.Fatal(err)
		}
	}
}
