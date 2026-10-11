package nest

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/placeorg"
)

type overlapFixture struct {
	Cases []struct {
		Name  string `json:"name"`
		Chats []struct {
			ID       string `json:"id"`
			Fine     int    `json:"fine"`
			Mid      int    `json:"mid"`
			Coarse   int    `json:"coarse"`
			WantFine int    `json:"wantFine"`
			WantMid  int    `json:"wantMid"`
			WantRoot int    `json:"wantRoot"`
			Inbox    bool   `json:"inbox"`
		} `json:"chats"`
	} `json:"cases"`
}

func TestOverlapFixture(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "overlap.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fixture overlapFixture
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	for _, tc := range fixture.Cases {
		t.Run(tc.Name, func(t *testing.T) {
			corpus, fine, mid, coarse := labelsOf(tc.Chats)
			tree, err := New().Nest(context.Background(), corpus, fine, mid, coarse)
			if err != nil {
				t.Fatal(err)
			}
			assertNested(t, tree, tc.Chats)
		})
	}
}

func labelsOf(chats []struct {
	ID       string `json:"id"`
	Fine     int    `json:"fine"`
	Mid      int    `json:"mid"`
	Coarse   int    `json:"coarse"`
	WantFine int    `json:"wantFine"`
	WantMid  int    `json:"wantMid"`
	WantRoot int    `json:"wantRoot"`
	Inbox    bool   `json:"inbox"`
}) (*placeorg.Corpus, []int, []int, []int) {
	corpus := &placeorg.Corpus{Chats: make([]placeorg.Chat, len(chats))}
	fine := make([]int, len(chats))
	mid := make([]int, len(chats))
	coarse := make([]int, len(chats))
	for i, chat := range chats {
		corpus.Chats[i] = placeorg.Chat{ID: chat.ID}
		fine[i], mid[i], coarse[i] = chat.Fine, chat.Mid, chat.Coarse
	}
	return corpus, fine, mid, coarse
}

func assertNested(t *testing.T, tree *placeorg.Tree, chats []struct {
	ID       string `json:"id"`
	Fine     int    `json:"fine"`
	Mid      int    `json:"mid"`
	Coarse   int    `json:"coarse"`
	WantFine int    `json:"wantFine"`
	WantMid  int    `json:"wantMid"`
	WantRoot int    `json:"wantRoot"`
	Inbox    bool   `json:"inbox"`
}) {
	t.Helper()
	gotRoot := map[string]string{}
	wantRoot := map[string]string{}
	gotMid := map[string]string{}
	wantMid := map[string]string{}
	gotFine := map[string]string{}
	wantFine := map[string]string{}
	for _, chat := range chats {
		if chat.Inbox {
			if _, ok := tree.Home(chat.ID); ok {
				t.Errorf("chat %s has a home and should be in Now", chat.ID)
			}
			continue
		}
		chain := chainOf(t, tree, chat.ID)
		if len(chain) != 3 {
			t.Fatalf("chat %s path = %v, want root, mid, leaf", chat.ID, chain)
		}
		for _, id := range chain {
			node, _ := tree.Node(id)
			if node.Name != "" {
				t.Errorf("place %s has name %q", id, node.Name)
			}
		}
		if node, _ := tree.Node(chain[2]); len(node.Members) == 0 || !contains(node.Members, chat.ID) {
			t.Errorf("leaf %s does not list %s", chain[2], chat.ID)
		}
		if node, _ := tree.Node(chain[1]); len(node.Members) != 0 {
			t.Errorf("mid %s lists members, and a member is a chat whose home is that place", chain[1])
		}
		gotRoot[chat.ID], wantRoot[chat.ID] = chain[0], strconv.Itoa(chat.WantRoot)
		gotMid[chat.ID], wantMid[chat.ID] = chain[1], strconv.Itoa(chat.WantMid)
		gotFine[chat.ID], wantFine[chat.ID] = chain[2], strconv.Itoa(chat.WantFine)
	}
	if !samePartition(gotRoot, wantRoot) || !samePartition(gotMid, wantMid) || !samePartition(gotFine, wantFine) {
		t.Fatalf("parent map mismatch\nroot got %v want %v\nmid got %v want %v\nfine got %v want %v", gotRoot, wantRoot, gotMid, wantMid, gotFine, wantFine)
	}
}

func chainOf(t *testing.T, tree *placeorg.Tree, chat string) []string {
	t.Helper()
	id, ok := tree.Home(chat)
	if !ok {
		t.Fatalf("chat %s has no home", chat)
	}
	var up []string
	seen := map[string]bool{}
	for id != "" && !seen[id] {
		seen[id] = true
		up = append(up, id)
		node, ok := tree.Node(id)
		if !ok {
			t.Fatalf("missing place %s", id)
		}
		id = node.Parent
	}
	down := make([]string, len(up))
	for i := range up {
		down[i] = up[len(up)-1-i]
	}
	return down
}

func samePartition(got, want map[string]string) bool {
	if len(got) != len(want) {
		return false
	}
	// Two chats share a group on one side exactly when they share it on the other.
	for a := range got {
		for b := range got {
			if (got[a] == got[b]) != (want[a] == want[b]) {
				return false
			}
		}
	}
	return true
}

func contains(list []string, id string) bool {
	for _, item := range list {
		if item == id {
			return true
		}
	}
	return false
}

func TestNestRejectsAMismatchAndACancelledContext(t *testing.T) {
	corpus := &placeorg.Corpus{Chats: []placeorg.Chat{{ID: "a"}, {ID: "b"}}}
	_, err := New().Nest(context.Background(), corpus, []int{0}, []int{0, 0}, []int{0, 0})
	if err == nil {
		t.Fatal("mismatched labels were nested")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = New().Nest(ctx, corpus, []int{0, 0}, []int{0, 0}, []int{0, 0})
	if err == nil {
		t.Fatal("a cancelled nest returned a tree")
	}
}

func TestNestIsDeterministic(t *testing.T) {
	corpus := &placeorg.Corpus{Chats: []placeorg.Chat{{ID: "a"}, {ID: "b"}, {ID: "c"}}}
	fine, mid, coarse := []int{0, 0, 1}, []int{3, 3, 4}, []int{1, 1, 1}
	first, err := New().Nest(context.Background(), corpus, fine, mid, coarse)
	if err != nil {
		t.Fatal(err)
	}
	second, err := New().Nest(context.Background(), corpus, fine, mid, coarse)
	if err != nil {
		t.Fatal(err)
	}
	for _, chat := range corpus.Chats {
		if chainOf(t, first, chat.ID)[0] != chainOf(t, second, chat.ID)[0] {
			t.Fatalf("two nests disagreed on %s", chat.ID)
		}
	}
}

// The owner's library is not a word list this package is allowed to carry.
// The words below are the reference harness's fixed subject list; a source
// file that names them has started special-casing one person's chats.
func TestSourcesNameNoCorpusSubjects(t *testing.T) {
	banned := []string{
		"desk", "cloudflare", "fleet", "pareto", "telegram", "carnatic",
		"openrouter", "sonnet", "wisp", "toronto",
	}
	pattern := regexp.MustCompile(`(?i)\b(?:` + strings.Join(banned, "|") + `)\b`)
	roots := []string{".", "../park", "../assemble.go"}
	for _, root := range roots {
		info, err := os.Stat(root)
		if err != nil {
			t.Fatal(err)
		}
		var files []string
		if info.IsDir() {
			entries, err := os.ReadDir(root)
			if err != nil {
				t.Fatal(err)
			}
			for _, entry := range entries {
				if strings.HasSuffix(entry.Name(), ".go") && !strings.HasSuffix(entry.Name(), "_test.go") {
					files = append(files, filepath.Join(root, entry.Name()))
				}
			}
		} else if strings.HasSuffix(root, ".go") {
			files = []string{root}
		}
		for _, file := range files {
			text, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			if found := pattern.Find(text); found != nil {
				t.Errorf("%s contains corpus subject %q", file, found)
			}
		}
	}
}
