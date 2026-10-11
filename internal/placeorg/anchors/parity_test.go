package anchors

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/placeorg"
)

// TestReferenceRecovery uses the saved embedding row ids to select exactly
// the harness's deduplicated, non-probe corpus. This stage does not cluster,
// so anchor-hit recall is its parity metric; ARI belongs to the runner.
// The private corpus and the reference's subject dictionary stay off-tree.
func TestReferenceRecovery(t *testing.T) {
	ref := os.Getenv("PLACEORG_REF")
	if ref == "" {
		t.Skip("set PLACEORG_REF to the owner's reference directory")
	}
	var ids []string
	readJSON(t, filepath.Join(ref, "emb_row_ids.json"), &ids)
	if len(ids) == 0 {
		t.Fatal("reference has no embedding row ids")
	}
	source, err := os.ReadFile(filepath.Join(ref, "places_harness.py"))
	if err != nil {
		t.Fatal(err)
	}
	dictionary := regexp.MustCompile(`(?s)\{w for w in \[(.*?)\]`).FindSubmatch(source)
	if len(dictionary) != 2 {
		t.Fatal("cannot locate the reference's entity dictionary")
	}
	var subjects []string
	for _, match := range regexp.MustCompile(`"([^"]+)"`).FindAllSubmatch(dictionary[1], -1) {
		subjects = append(subjects, string(match[1]))
	}
	rows := map[string]placeorg.Chat{}
	corpusDir := os.Getenv("PLACEORG_CORPUS")
	if corpusDir == "" {
		corpusDir = filepath.Dir(filepath.Dir(ref))
	}
	for _, name := range []string{"corpus-cc.jsonl", "corpus.jsonl"} {
		loadReferenceChats(t, filepath.Join(corpusDir, name), rows)
	}
	chats := make([]placeorg.Chat, len(ids))
	for i, id := range ids {
		chat, ok := rows[id]
		if !ok {
			t.Fatalf("reference row %d is missing from the supplied corpus", i)
		}
		chats[i] = chat
	}
	got := derive(t, chats)
	issue := regexp.MustCompile(`#[0-9]{3,5}`)
	hits, recovered := 0, 0
	for _, chat := range chats {
		want := map[string]bool{}
		for _, key := range issue.FindAllString(chat.Text, -1) {
			want[key] = true
		}
		for _, key := range subjects {
			if strings.Contains(strings.ToLower(chat.Text), key) {
				want[key] = true
			}
		}
		for key := range want {
			hits++
			if got[chat.ID][key] > 0 {
				recovered++
			}
		}
	}
	if hits == 0 {
		t.Fatal("reference has no anchor hits")
	}
	recall := float64(recovered) / float64(hits)
	t.Logf("reference rows=%d; recovered=%d/%d anchor hits (%.2f%%); confirmed names=0", len(chats), recovered, hits, recall*100)
	if recall < 0.9 {
		t.Errorf("recall below 90%%: the proper-token cap is 5%% of documents; frequent, lowercase-only and substring subjects are not eligible without confirmed place names")
	}
}

func readJSON(t *testing.T, path string, value any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, value); err != nil {
		t.Fatal(err)
	}
}

func loadReferenceChats(t *testing.T, path string, rows map[string]placeorg.Chat) {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 4096), 16<<20)
	for scanner.Scan() {
		var row struct {
			ID        string `json:"id"`
			Title     string `json:"title"`
			FirstUser string `json:"first_user"`
			FirstMsg  string `json:"first_msg"`
			Workspace string `json:"workspace"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &row); err != nil {
			t.Fatal("invalid corpus row")
		}
		first := row.FirstUser
		if first == "" {
			first = row.FirstMsg
		}
		runes := []rune(first)
		if len(runes) > 400 {
			runes = runes[:400]
		}
		rows[row.ID] = placeorg.Chat{ID: row.ID, Title: row.Title, Text: strings.TrimSpace(row.Title + "\n" + string(runes)), Workspace: row.Workspace}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
}
