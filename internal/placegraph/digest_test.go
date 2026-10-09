package placegraph

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

var digestNow = time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)

func chatWith(id, line string, age time.Duration) DigestChat {
	return DigestChat{ID: id, Title: "T" + id, Recap: &DigestRecap{Line: line, UpdatedAt: digestNow.Add(-age)}}
}

func TestRollUpQuotesRecentRecapsOnlyAndSaysNothingWithoutEvidence(t *testing.T) {
	empty := RollUp(DigestInput{Now: digestNow, Chats: []DigestChat{
		{ID: "titles-only", Title: "Fix the parser"},
		chatWith("old", "Settled the strict-mode question.", 30*time.Hour),
	}})
	if !empty.Empty() || empty.Text != "" || empty.Chats != 0 {
		t.Fatalf("a title or a day-old recap is not evidence: %+v", empty)
	}
	d := RollUp(DigestInput{Now: digestNow, Chats: []DigestChat{chatWith("a", "Moved the parser to strict mode", time.Hour), chatWith("old", "Ancient.", 25*time.Hour)}})
	if d.Text != "Moved the parser to strict mode." || d.Chats != 1 || d.Label != "Since yesterday" || !d.Since.Equal(digestNow.Add(-24*time.Hour)) {
		t.Fatalf("%+v", d)
	}
}

func TestRollUpRanksAttentionThenNewestThenIDAndIsOrderIndependent(t *testing.T) {
	a := chatWith("a", "Newest quiet.", time.Hour)
	b := chatWith("b", "Older but waiting on you.", 10*time.Hour)
	b.NeedsYou = true
	c := chatWith("c", "Running now.", 5*time.Hour)
	c.Running = true
	e := chatWith("e", "Tied.", 2*time.Hour)
	f := chatWith("f", "Tied too.", 2*time.Hour)
	one := RollUp(DigestInput{Now: digestNow, Chats: []DigestChat{a, b, c, f, e}})
	two := RollUp(DigestInput{Now: digestNow, Chats: []DigestChat{e, f, c, b, a}})
	if one.Text != two.Text {
		t.Fatalf("arrival order changed the words:\n%q\n%q", one.Text, two.Text)
	}
	var ids []string
	for _, it := range one.Items {
		ids = append(ids, it.ChatID)
	}
	if strings.Join(ids, "") != "bcaef" || one.Items[0].Attention != "needsYou" || one.Items[1].Attention != "running" {
		t.Fatalf("order %v: %+v", ids, one.Items)
	}
}

func TestRollUpDedupesDropsArchivedAndDamagedRecaps(t *testing.T) {
	dup := chatWith("a", "Once.", time.Hour)
	arch := chatWith("z", "Archived is not news.", time.Hour)
	arch.Archived = true
	future := chatWith("f", "From the future.", -time.Hour)
	zero := DigestChat{ID: "zero", Recap: &DigestRecap{Line: "No stamp."}}
	blank := chatWith("blank", "   \n\t ", time.Hour)
	waitingNoRecap := DigestChat{ID: "w", NeedsYou: true}
	d := RollUp(DigestInput{Now: digestNow, Chats: []DigestChat{dup, dup, arch, future, zero, blank, waitingNoRecap}})
	if d.Chats != 1 || d.Text != "Once." || d.Unsummarised != 1 {
		t.Fatalf("diamond once, archived/future/unstamped/blank dropped, waiting-without-recap counted: %+v", d)
	}
}

func TestRollUpIsBoundedAndCleansControlCharacters(t *testing.T) {
	var chats []DigestChat
	for i := 0; i < 40; i++ {
		chats = append(chats, chatWith(string(rune('a'+i%26))+string(rune('A'+i/26)), strings.Repeat("word ", 80)+"\x1b[31m\x00end", time.Duration(i+1)*time.Minute))
	}
	d := RollUp(DigestInput{Now: digestNow, Chats: chats})
	if n := utf8.RuneCountInString(d.Text); n > DigestTextMax || n == 0 {
		t.Fatalf("text %d chars", n)
	}
	if len(d.Items) != DigestItemsMax || d.Chats != 40 {
		t.Fatalf("items %d chats %d", len(d.Items), d.Chats)
	}
	for _, it := range d.Items {
		if utf8.RuneCountInString(it.Line) > DigestLineMax {
			t.Fatalf("line too long: %d", utf8.RuneCountInString(it.Line))
		}
		if strings.ContainsRune(it.Line, 0) || strings.ContainsRune(it.Line, 0x1b) {
			t.Fatalf("control bytes survived: %q", it.Line)
		}
	}
}

func TestRollUpSaysHowManyMoreWhenItQuotesFewer(t *testing.T) {
	var chats []DigestChat
	for i := 0; i < 6; i++ {
		chats = append(chats, chatWith(string(rune('a'+i)), "Line "+string(rune('a'+i))+".", time.Duration(i+1)*time.Minute))
	}
	d := RollUp(DigestInput{Now: digestNow, Chats: chats})
	if d.Text != "Line a. Line b. Line c. Line d. 2 more in other chats." {
		t.Fatalf("%q", d.Text)
	}
}
