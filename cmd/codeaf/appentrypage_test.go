package main

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
)

// appPageRecord is a record of turns, each a question, a narrated call and
// its answer; a call id repeats every hundred turns, the way a provider's
// short ids can, and the record is compacted when compacted is set.
func appPageRecord(turns int, compacted bool) session.Record {
	var entries []session.DisplayEntry
	for n := range turns {
		call := "c" + strconv.Itoa(n%(turns/2+1))
		entries = append(entries,
			session.DisplayEntry{Role: "user", Text: "question " + strconv.Itoa(n)},
			session.DisplayEntry{Role: "assistant", Text: "Reading it."},
			session.DisplayEntry{Role: "tool", Tool: "bash", CallID: call, Args: `{"command":"go test"}`, Output: "ok " + strconv.Itoa(n), Answered: true},
			session.DisplayEntry{Role: "assistant", Text: "the loader passes " + strconv.Itoa(n)},
		)
	}
	record := session.Record{Entries: entries}
	if compacted {
		record.Earlier = entries[:len(entries)/2]
		record.Entries = append([]session.DisplayEntry{{Role: "note", Text: "summary"}}, entries[len(entries)/2:]...)
		record.Floor = 1
	}
	return record
}

// appWholePage is the page as Entries cut it before the facts were read
// page-first: every call's facts, then the slice. It is the oracle.
func appWholePage(id string, record session.Record, at time.Time, marks map[int]appTurnMark, before string, limit int, facts func(session.DisplayEntry) appwire.ToolFacts) ([]appwire.Entry, bool, error) {
	items := appRecordItems(record)
	all := appEntriesOf(id, items, at, marks)
	calls := map[string]session.DisplayEntry{}
	for _, item := range items {
		if item.Role == "tool" && item.CallID != "" {
			calls[item.CallID] = item
		}
	}
	for i := range all {
		if item, ok := calls[all[i].Call]; ok && all[i].Kind == appwire.EntryTool {
			all[i].ToolFacts = facts(item)
		}
	}
	if words := strings.TrimSpace(record.Unreadable); words != "" {
		all = append(all, appwire.Entry{ID: "unreadable", Agent: id, Kind: appwire.EntryNotice, At: at, Text: words})
	}
	end := len(all)
	if before != "" {
		end = -1
		for i, entry := range all {
			if entry.ID == before {
				end = i
				break
			}
		}
		if end < 0 {
			return nil, false, appwire.Errorf(appwire.CodeNotFound, "this conversation has no entry %s", before)
		}
	}
	start := max(end-limit, 0)
	return append([]appwire.Entry{}, all[start:end]...), start > 0, nil
}

// appCountedFacts names the item it was asked about, and counts the asking.
func appCountedFacts(count *int) func(session.DisplayEntry) appwire.ToolFacts {
	return func(item session.DisplayEntry) appwire.ToolFacts {
		*count++
		return appwire.ToolFacts{Tail: []string{item.CallID, item.Output}}
	}
}

// A PAGE READS THE FACTS OF ITS OWN CALLS AND NO OTHERS, and is otherwise the
// page it always was: the same ids, the same facts on the same calls, the same
// cursor and the same "more", from the newest page back to the first, over a
// plain record, a compacted one, one that stopped short and one whose newest
// turn this process sent.
func TestAPageReadsOnlyItsOwnCallsFacts(t *testing.T) {
	at := time.Unix(1_800_000_000, 0)
	short := appPageRecord(2500, false)
	short.Unreadable = "this transcript could not be read past line 9 — everything above it is on this page"
	records := map[string]session.Record{
		"plain":     appPageRecord(2500, false),
		"compacted": appPageRecord(2500, true),
		"short":     short,
	}
	marks := map[int]appTurnMark{2499: {turn: 7, clientID: "k1", text: "sent here"}}
	for name, record := range records {
		t.Run(name, func(t *testing.T) {
			var whole []appwire.Entry
			before := ""
			for {
				var paged, oracle int
				page, more, err := appEntryPage("a1", record, at, marks, before, appwire.MaxEntries, appCountedFacts(&paged))
				want, wantMore, wantErr := appWholePage("a1", record, at, marks, before, appwire.MaxEntries, appCountedFacts(&oracle))
				if err != nil || wantErr != nil {
					t.Fatalf("page before %q: %v, oracle %v", before, err, wantErr)
				}
				if !reflect.DeepEqual(page, want) || more != wantMore {
					t.Fatalf("page before %q differs from the whole-record page", before)
				}
				tools := 0
				for _, entry := range page {
					if entry.Kind == appwire.EntryTool {
						tools++
					}
				}
				if paged != tools || paged > appwire.MaxEntries {
					t.Fatalf("page before %q read %d facts for %d calls", before, paged, tools)
				}
				whole = append(page, whole...)
				if !more {
					break
				}
				before = page[0].ID
			}
			all, _, _ := appWholePage("a1", record, at, marks, "", 1<<30, appCountedFacts(new(int)))
			if !reflect.DeepEqual(whole, all) {
				t.Fatal("walking the pages back did not tell the conversation once, in order")
			}
		})
	}
	var count int
	if _, _, err := appEntryPage("a1", records["plain"], at, nil, "u999999", 10, appCountedFacts(&count)); err == nil || count != 0 {
		t.Fatalf("a cursor no entry has = %v after %d facts", err, count)
	}
}

// A CONVERSATION READ TWICE UNCHANGED IS PARSED ONCE, and a turn appended
// between pages is on the next page.
func TestEntriesPagesFromOneParseUntilTheJournalGrows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transcript.jsonl")
	var journal strings.Builder
	journal.WriteString(`{"type":"session","version":1,"id":"n1","cwd":"/tmp/lab"}` + "\n")
	turn := func(n int) {
		journal.WriteString(`{"type":"message","role":"user","content":"question ` + strconv.Itoa(n) + `"}` + "\n")
		journal.WriteString(`{"type":"message","role":"assistant","content":"answer ` + strconv.Itoa(n) + `"}` + "\n")
	}
	for n := range 300 {
		turn(n)
	}
	if err := os.WriteFile(path, []byte(journal.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	cache := session.NewRecordCache(session.RecordCacheLimits{Now: func() time.Time { return time.Now().Add(time.Hour) }})
	page := func(before string) []appwire.Entry {
		var got []appwire.Entry
		err := cache.WithRecord(path, func(record session.Record) error {
			var err error
			got, _, err = appEntryPage("a1", record, time.Time{}, nil, before, 100, appCountedFacts(new(int)))
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	newest := page("")
	older := page(newest[0].ID)
	if older[len(older)-1].ID == newest[0].ID || cache.Stats().Parses != 1 {
		t.Fatalf("two pages: %+v", cache.Stats())
	}
	turn(300)
	if err := os.WriteFile(path, []byte(journal.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	if latest := page(""); latest[len(latest)-1].Text != "answer 300" {
		t.Fatalf("the appended turn is missing: %q", latest[len(latest)-1].Text)
	}
}

// A READING REFUSED AT THE GATE IS A BUSY ANSWER, not an empty conversation.
func TestABusyReadingIsSaidAsBusy(t *testing.T) {
	err := appEntriesBusy(session.ErrRecordReadBusy)
	var wire *appwire.Error
	if !errors.As(err, &wire) || wire.Code != appwire.CodeBusy {
		t.Fatalf("a refused reading = %v", err)
	}
}

// BenchmarkAppEntryPage is the newest page and an older one, of a record of
// 10k and 100k entries, with the facts read page-first and, for comparison,
// for every call. facts/op is how many calls' facts each page read.
func BenchmarkAppEntryPage(b *testing.B) {
	missing := filepath.Join(b.TempDir(), "no-such-output.log")
	facts := func(count *int) func(session.DisplayEntry) appwire.ToolFacts {
		return func(item session.DisplayEntry) appwire.ToolFacts {
			*count++
			// The real facts look for the call's output file; most are gone.
			_, _ = os.Stat(missing)
			return appwire.ToolFacts{Tail: []string{item.Output}}
		}
	}
	for _, size := range []int{10_000, 100_000} {
		record := appPageRecord(size/4, false)
		for _, way := range []struct {
			name string
			page func(string, session.Record, time.Time, map[int]appTurnMark, string, int, func(session.DisplayEntry) appwire.ToolFacts) ([]appwire.Entry, bool, error)
		}{{"pagefirst", appEntryPage}, {"whole", appWholePage}} {
			b.Run(strconv.Itoa(size/1000)+"k/"+way.name, func(b *testing.B) {
				count := 0
				b.ReportAllocs()
				for b.Loop() {
					newest, _, _ := way.page("a1", record, time.Time{}, nil, "", appwire.MaxEntries, facts(&count))
					_, _, _ = way.page("a1", record, time.Time{}, nil, newest[0].ID, appwire.MaxEntries, facts(&count))
				}
				b.ReportMetric(float64(count)/float64(b.N)/2, "facts/page")
			})
		}
	}
}
