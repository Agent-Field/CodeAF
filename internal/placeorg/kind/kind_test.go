package kind

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/placeorg"
)

func chat(id, msg string, replies int) placeorg.Chat {
	return placeorg.Chat{ID: id, Text: msg, Replies: replies}
}

func kindsOf(t *testing.T, chats []placeorg.Chat, rep map[string]string) map[string]placeorg.Kind {
	t.Helper()
	got, err := New().Kinds(context.Background(), chats, rep)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestAnUnansweredShortMessageIsAProbe(t *testing.T) {
	got := kindsOf(t, []placeorg.Chat{chat("a", "hi", 0), chat("b", "hi", 2)}, nil)
	if got["a"] != placeorg.KindProbe || got["b"] != placeorg.KindConversation {
		t.Fatalf("got %v", got)
	}
}

func TestALongUnansweredMessageIsStillAConversation(t *testing.T) {
	long := strings.Repeat("describe the migration plan ", 4)
	if got := kindsOf(t, []placeorg.Chat{chat("a", long, 0)}, nil); got["a"] != placeorg.KindConversation {
		t.Fatalf("got %v", got)
	}
}

func TestACannedOpeningSharedByThreeChatsIsAProbe(t *testing.T) {
	chats := []placeorg.Chat{
		chat("a", "ping the server now", 1), chat("b", "ping the box", 1), chat("c", "ping the host again", 1),
		chat("d", "ping pong table rules", 1), chat("e", "tidy the garden notes", 1),
	}
	got := kindsOf(t, chats, nil)
	for _, id := range []string{"a", "b", "c"} {
		if got[id] != placeorg.KindProbe {
			t.Errorf("%s = %s, want probe", id, got[id])
		}
	}
	if got["e"] != placeorg.KindConversation {
		t.Errorf("e = %s", got["e"])
	}
}

func TestQuestionsSharingAnOpeningAreNotCanned(t *testing.T) {
	var chats []placeorg.Chat
	for i, q := range []string{"can you fix it ?", "can you ship it ?", "can you read it ?", "can you move it ?"} {
		chats = append(chats, chat(fmt.Sprint(i), q, 1))
	}
	for id, k := range kindsOf(t, chats, nil) {
		if k != placeorg.KindConversation {
			t.Errorf("%s = %s", id, k)
		}
	}
}

func TestALiteralRequestRepeatedAcrossChatsIsAProbe(t *testing.T) {
	chats := []placeorg.Chat{
		chat("a", "Echo exactly: PONG_1. Skip tools.", 1), chat("b", "echo \"beta\" back to me", 1),
		chat("c", "echo with code zx9", 1), chat("d", "echo the plan for friday", 1),
	}
	got := kindsOf(t, chats, nil)
	for _, id := range []string{"a", "b", "c"} {
		if got[id] != placeorg.KindProbe {
			t.Errorf("%s = %s, want probe", id, got[id])
		}
	}
	if got["d"] != placeorg.KindConversation {
		t.Errorf("d = %s", got["d"])
	}
}

func TestDuplicatesFollowTheRepresentativeMap(t *testing.T) {
	chats := []placeorg.Chat{chat("a", "plan the garden", 1), chat("b", "plan the garden", 1)}
	got := kindsOf(t, chats, map[string]string{"a": "a", "b": "a"})
	if got["a"] != placeorg.KindConversation || got["b"] != placeorg.KindDuplicate {
		t.Fatalf("got %v", got)
	}
}

func TestADuplicateDoesNotMakeAnOpeningCanned(t *testing.T) {
	chats := []placeorg.Chat{chat("a", "water the ferns", 1), chat("b", "water the ferns", 1), chat("c", "water the ferns", 1)}
	got := kindsOf(t, chats, map[string]string{"a": "a", "b": "a", "c": "a"})
	if got["a"] != placeorg.KindConversation {
		t.Fatalf("got %v", got)
	}
}

func TestTheTitleIsNotTheFirstMessage(t *testing.T) {
	c := placeorg.Chat{ID: "a", Title: "a quick greeting", Text: "a quick greeting\nhi", Replies: 1}
	if m := FirstMessage(c); m != "hi" {
		t.Fatalf("first message %q", m)
	}
}

func TestACancelledContextStopsTheStage(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := New().Kinds(ctx, []placeorg.Chat{chat("a", "hi", 0)}, nil); err == nil {
		t.Fatal("want the context error")
	}
}

// TestTheSourceHoldsNoListOfSubjects is the owner's rule as a test: the
// words his reference regex spells out, and the subjects of his library, may
// not appear anywhere in this package's non-test source. The words are
// assembled here from fragments so this file is not itself the list.
func TestTheSourceHoldsNoListOfSubjects(t *testing.T) {
	banned := []string{"rep" + "ly", "respo" + "nd", "sa" + "y", "cou" + "nt", "hel" + "lo", "he" + "y", "te" + "st", "li" + "st files", "ali" + "ve", "sing" + "le word", "ex" + "actly"}
	files, _ := filepath.Glob("*.go")
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, w := range banned {
			if m := regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(w) + `\b`).Find(src); m != nil {
				t.Errorf("%s spells %q", f, m)
			}
		}
	}
}

// harnessProbe is the owner's reference regex. It is the test's oracle only;
// the stage itself never sees it.
var harnessProbe = regexp.MustCompile(`(?i)^(re` + `ply|re` + `spond|sa` + `y|what is \d|co` + `unt the|li` + `st files|hi$|he` + `llo$|he` + `y$|ok$|te` + `st\b)`)

// TestEveryProbeTheHarnessCatchesIsCaughtOnTheOwnersCorpus runs against the
// owner's saved corpus when it is on this machine and skips elsewhere; the
// corpus is never committed.
func TestEveryProbeTheHarnessCatchesIsCaughtOnTheOwnersCorpus(t *testing.T) {
	dir := os.Getenv("PLACES_LAB_DIR")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, "places-lab")
	}
	var chats []placeorg.Chat
	oracle := map[string]bool{}
	seen := map[string]bool{}
	rep := map[string]string{}
	first := map[string]string{}
	for _, name := range []string{"corpus.jsonl", "corpus-cc.jsonl"} {
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Skipf("owner corpus not present: %v", err)
		}
		for _, line := range strings.Split(string(raw), "\n") {
			var j struct {
				ID            string      `json:"id"`
				Title         string      `json:"title"`
				FirstUser     string      `json:"first_user"`
				Turns         json.Number `json:"turns"`
				LastAssistant string      `json:"last_assistant"`
			}
			if json.Unmarshal([]byte(line), &j) != nil {
				continue
			}
			msg := strings.TrimSpace(j.FirstUser)
			if msg == "" || seen[j.ID] {
				continue
			}
			seen[j.ID] = true
			// An exact-text retry folds into its first sighting, standing in for the dedup stage.
			if r, ok := first[msg]; ok {
				rep[j.ID] = r
			} else {
				first[msg] = j.ID
			}
			// A saved assistant line proves an answer; without one, half the turns are the best estimate.
			turns, _ := j.Turns.Int64()
			replies := int(turns / 2)
			if j.LastAssistant != "" {
				replies = max(replies, 1)
			}
			chats = append(chats, placeorg.Chat{ID: j.ID, Title: j.Title, Text: strings.TrimSpace(j.Title + "\n" + msg), Replies: replies})
			oracle[j.ID] = harnessProbe.MatchString(msg) && len([]rune(msg)) <= 80
		}
	}
	got := kindsOf(t, chats, rep)
	for _, c := range chats {
		if oracle[c.ID] && rep[c.ID] == "" && got[c.ID] != placeorg.KindProbe {
			t.Errorf("harness probe %q missed (%s)", FirstMessage(c), got[c.ID])
		}
		if !oracle[c.ID] && got[c.ID] == placeorg.KindProbe {
			t.Logf("extra probe beyond the harness: %q (replies %d)", FirstMessage(c), c.Replies)
			if c.Replies > 0 {
				t.Errorf("answered conversation %q labelled a probe", FirstMessage(c))
			}
		}
	}
}
