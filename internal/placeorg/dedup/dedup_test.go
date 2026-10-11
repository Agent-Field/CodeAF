package dedup

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/placeorg"
)

var t0 = time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)

const brief = "migrate the billing service from the legacy cron runner to the new scheduler keeping idempotent retries per tenant and emit one audit event for every migrated job so support can trace failures afterwards"

func chat(id string, minute int, text string) placeorg.Chat {
	return placeorg.Chat{ID: id, Text: text, Created: t0.Add(time.Duration(minute) * time.Minute)}
}

func run(t *testing.T, chats []placeorg.Chat) map[string]string {
	t.Helper()
	got, err := New(DefaultSeed).Dedup(context.Background(), chats)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestRetriedBriefFoldsOntoTheEarlierChat(t *testing.T) {
	// The later chat is listed first to prove "earliest" reads Created, not order.
	got := run(t, []placeorg.Chat{chat("retry", 9, "Billing\n"+brief), chat("first", 1, "Billing\n"+brief)})
	if got["retry"] != "first" || len(got) != 1 {
		t.Fatalf("got %v, want retry folded onto first", got)
	}
}

func TestDifferentChatsDoNotFold(t *testing.T) {
	got := run(t, []placeorg.Chat{
		chat("a", 1, "Billing\n"+brief),
		chat("b", 2, "Garden\nplan the raised vegetable beds for spring with tomatoes beans and a drip line along the south fence"),
	})
	if len(got) != 0 {
		t.Fatalf("unrelated chats folded: %v", got)
	}
}

func TestAGroupOfFiveFoldsToOne(t *testing.T) {
	var chats []placeorg.Chat
	for i := 0; i < 5; i++ {
		chats = append(chats, chat(fmt.Sprintf("c%d", 4-i), 4-i, brief))
	}
	got := run(t, chats)
	if len(got) != 4 {
		t.Fatalf("want 4 folded, got %v", got)
	}
	for id, rep := range got {
		if rep != "c0" {
			t.Fatalf("%s folded onto %s, want c0", id, rep)
		}
	}
}

func TestOptimalBandsMatchTheReferenceShape(t *testing.T) {
	b, r := optimalBands(Threshold, Permutations)
	if b*r > Permutations || b < 2 || r < 2 {
		t.Fatalf("implausible bands %d x %d", b, r)
	}
}

func TestCancelledContextStops(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := New(1).Dedup(ctx, []placeorg.Chat{chat("a", 0, brief)}); err == nil {
		t.Fatal("want context error")
	}
}

// synthetic builds n chats of 60 random words with a tenth of them repeated.
func synthetic(n int) []placeorg.Chat {
	rng := rand.New(rand.NewSource(7))
	chats := make([]placeorg.Chat, n)
	for i := range chats {
		if i%10 == 9 {
			chats[i] = chat(fmt.Sprint(i), i, chats[i-1].Text)
			continue
		}
		w := make([]string, 60)
		for j := range w {
			w[j] = fmt.Sprintf("w%d", rng.Intn(5000))
		}
		chats[i] = chat(fmt.Sprint(i), i, strings.Join(w, " "))
	}
	return chats
}

func TestTenThousandChatsUnderTwoSeconds(t *testing.T) {
	chats := synthetic(10000)
	start := time.Now()
	got := run(t, chats)
	if el := time.Since(start); el > 2*time.Second {
		t.Fatalf("10k chats took %v, budget 2s", el)
	}
	if len(got) < 900 || len(got) > 1100 {
		t.Fatalf("folded %d, want about 1000", len(got))
	}
}

func BenchmarkTenThousandChats(b *testing.B) {
	chats := synthetic(10000)
	d := New(DefaultSeed)
	for i := 0; i < b.N; i++ {
		if _, err := d.Dedup(context.Background(), chats); err != nil {
			b.Fatal(err)
		}
	}
}

// TestFoldCountMatchesTheReferenceCorpus replays the owner's corpus (never
// committed; point PLACEORG_LAB at the directory holding corpus.jsonl and
// corpus-cc.jsonl) and expects the reference run's 631 -> 398 within 2%.
func TestFoldCountMatchesTheReferenceCorpus(t *testing.T) {
	lab := os.Getenv("PLACEORG_LAB")
	if lab == "" {
		lab = filepath.Join(os.Getenv("HOME"), "places-lab")
	}
	var chats []placeorg.Chat
	for _, name := range []string{"corpus-cc.jsonl", "corpus.jsonl"} {
		chats = append(chats, loadLab(t, filepath.Join(lab, name))...)
	}
	if len(chats) == 0 {
		t.Skip("owner corpus not present")
	}
	const wantInput, wantKept = 631, 398
	if len(chats) != wantInput {
		t.Fatalf("loaded %d chats, reference had %d", len(chats), wantInput)
	}
	kept := len(chats) - len(run(t, chats))
	if d := float64(kept-wantKept) / wantKept; d > 0.02 || d < -0.02 {
		t.Fatalf("kept %d, reference %d (%.1f%% off)", kept, wantKept, d*100)
	}
	t.Logf("kept %d of %d, reference %d", kept, len(chats), wantKept)
}

func loadLab(t *testing.T, path string) []placeorg.Chat {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []placeorg.Chat
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	for sc.Scan() {
		var j struct {
			ID, Title, Created string
			FirstUser          string `json:"first_user"`
			FirstMsg           string `json:"first_msg"`
		}
		if json.Unmarshal(sc.Bytes(), &j) != nil {
			continue
		}
		first := []rune(j.FirstUser)
		if len(first) == 0 {
			first = []rune(j.FirstMsg)
		}
		first = first[:min(len(first), 400)]
		text := strings.TrimSpace(j.Title + "\n" + string(first))
		if text == "" {
			continue
		}
		created, _ := time.Parse(time.RFC3339Nano, j.Created)
		out = append(out, placeorg.Chat{ID: j.ID, Text: text, Created: created})
	}
	return out
}
