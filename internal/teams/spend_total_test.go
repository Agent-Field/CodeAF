package teams

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

// key is a conversation transcript path, an id's place on disk: the shape a
// member's Key takes and the session id a ledger line names.
func key(id string) string {
	return filepath.Join("/srv/sessions", id, "transcript.jsonl")
}

// tline is one usage ledger line on 2026-09-24 with a precise time, so a
// membership boundary or a Made lands mid-line and the total's half-open
// [JoinedAt, LeftAt) rule can be measured exactly.
func tline(at time.Time, usd float64, session, root string) string {
	return fmt.Sprintf(`{"at":%q,"day":"2026-09-24","usd":%v,"calls":1,"session":%q,"root":%q}`, at.Format(time.RFC3339), usd, session, root)
}

// cents is a float as integer cents, so totals and ByMember shares compare
// without float noise.
func cents(f float64) int { return int(f*100 + 0.5) }

// The team's total, all time, every team under it, whole conversation chains.
//
// The fixture, one day of one tree:
//
//	r (made 00:00)                  a in harbor, f a former member of harbor
//	├─ harbor (made 08:00)          b in sub, c in the closed sub, e in harbor
//	│  ├─ sub (made 09:00)          and in twins: a conversation in two teams
//	│  ├─ closed (made 08:00)
//	│  └─ twins (made 08:00)        d in other, outside harbor
//	└─ other (made 08:00)
//
// The lines and what they are:
//
//	08:30 a      member's own call            counts in harbor (joined 07:00, made 08:00)
//	07:30 a      before harbor's made         counts nowhere
//	06:30 a      before joined and made       counts nowhere
//	09:00 task1↜a    delegated by a           counts (one hop)
//	09:05 task2↜task1 sub-task of a's task    counts (the hop today's read drops)
//	10:30 f      inside [10:00, 13:00)        counts
//	13:00 f      exactly the removal          counts nowhere (half-open)
//	12:59 f      inside                       counts
//	09:30 b      sub member                   counts in sub and harbor
//	09:05 b      before joined (09:10)/made    counts nowhere
//	11:00 c      closed sub's member          counts in harbor and r
//	14:00 e      in harbor and twins          counts in both, once in r
//	09:00 d      other's member               counts in r only
func TestTeamTotalWholeChainsMembershipAndSubtree(t *testing.T) {
	dir := t.TempDir()
	at := func(h, m int) time.Time { return time.Date(2026, 9, 24, h, m, 0, 0, time.UTC) }
	must(t, Save(dir, []Team{
		{ID: "rrrrrrrrrrrr", Name: "r", Root: true, Made: at(0, 0)},
		{ID: "aaaaaaaaaaaa", Name: "harbor", Parent: "rrrrrrrrrrrr", Made: at(8, 0),
			Members:       []Member{{Key: key("0123456789abcdef"), JoinedAt: at(7, 0)}, {Key: key("3333333333333333")}},
			FormerMembers: []Member{{Key: key("2222222222222222"), JoinedAt: at(10, 0), LeftAt: at(13, 0)}}},
		{ID: "bbbbbbbbbbbb", Name: "sub", Parent: "aaaaaaaaaaaa", Made: at(9, 0),
			Members: []Member{{Key: key("1111111111111111"), JoinedAt: at(9, 10)}}},
		{ID: "cccccccccccc", Name: "closed", Parent: "aaaaaaaaaaaa", Made: at(8, 0), State: TeamClosed,
			Members: []Member{{Key: key("4444444444444444")}}},
		{ID: "eeeeeeeeeeee", Name: "twins", Parent: "aaaaaaaaaaaa", Made: at(8, 0),
			Members: []Member{{Key: key("3333333333333333")}}},
		{ID: "dddddddddddd", Name: "other", Parent: "rrrrrrrrrrrr", Made: at(8, 0),
			Members: []Member{{Key: key("5555555555555555"), JoinedAt: at(7, 0)}}},
	}))
	ledger := filepath.Join(dir, "usage.jsonl")
	writeLedger(t, ledger,
		tline(at(8, 30), 1.00, "0123456789abcdef", ""),                // a's own
		tline(at(7, 30), 2.00, "0123456789abcdef", ""),                // before made
		tline(at(6, 30), 4.00, "0123456789abcdef", ""),                // before joined and made
		tline(at(9, 0), 0.25, "aaaabbbbcccc0001", "0123456789abcdef"), // a's task
		tline(at(9, 5), 0.10, "aaaabbbbcccc0002", "aaaabbbbcccc0001"), // the task's sub-task
		tline(at(10, 30), 0.50, "2222222222222222", ""),               // f, while here
		tline(at(13, 0), 0.75, "2222222222222222", ""),                // f, exactly at LeftAt
		tline(at(12, 59), 0.25, "2222222222222222", ""),               // f, still here
		tline(at(9, 30), 3.00, "1111111111111111", ""),                // b in sub
		tline(at(9, 5), 5.00, "1111111111111111", ""),                 // b before joined
		tline(at(11, 0), 1.50, "4444444444444444", ""),                // closed sub's c
		tline(at(14, 0), 2.00, "3333333333333333", ""),                // e, in harbor and twins
		tline(at(9, 0), 0.60, "5555555555555555", ""),                 // d in other
	)

	harbor, err := TeamTotalIn(dir, ledger, "aaaaaaaaaaaa")
	must(t, err)
	if cents(harbor.USD) != 860 || harbor.Calls != 8 {
		t.Fatalf("harbor total: %+v", harbor)
	}
	want := map[string]int{
		key("0123456789abcdef"): 135, // own + task + sub-task
		key("2222222222222222"): 75,  // inside [10:00, 13:00) only
		key("1111111111111111"): 300,
		key("4444444444444444"): 150,
		key("3333333333333333"): 200,
	}
	if len(harbor.ByMember) != len(want) {
		t.Fatalf("harbor by member: %+v", harbor.ByMember)
	}
	sum := 0
	for k, v := range want {
		sum += v
		if cents(harbor.ByMember[k]) != v {
			t.Fatalf("harbor %s: %v", k, harbor.ByMember)
		}
	}
	if sum != cents(harbor.USD) {
		t.Fatalf("by member did not add up: %v vs %v", sum, harbor.USD)
	}

	sub, _ := TeamTotalIn(dir, ledger, "bbbbbbbbbbbb")
	if cents(sub.USD) != 300 || sub.Calls != 1 || cents(sub.ByMember[key("1111111111111111")]) != 300 {
		t.Fatalf("sub total: %+v", sub)
	}

	twins, _ := TeamTotalIn(dir, ledger, "eeeeeeeeeeee")
	if cents(twins.USD) != 200 {
		t.Fatalf("twins total (e in two teams counts in each): %+v", twins)
	}

	root, _ := TeamTotalIn(dir, ledger, "rrrrrrrrrrrr")
	// everything counted once: d's line belongs to the root's subtree alone.
	if cents(root.USD) != 920 || root.Calls != 9 {
		t.Fatalf("root total: %+v", root)
	}
	if cents(root.ByMember[key("3333333333333333")]) != 200 {
		t.Fatalf("e's line counted twice in the root: %+v", root.ByMember)
	}

	if none, _ := TeamTotalIn(dir, ledger, "ffffffffffff"); none.USD != 0 {
		t.Fatalf("an unknown team totals %v", none.USD)
	}

	// TeamTotalStamp says the same thing twice: a quiet ledger costs no read.
	first, err := TeamTotalIn(dir, ledger, "aaaaaaaaaaaa")
	must(t, err)
	before := spendCache.bytes()
	again, _ := TeamTotalIn(dir, ledger, "aaaaaaaaaaaa")
	if spendCache.bytes() != before {
		t.Fatal("a quiet ledger was read again")
	}
	if cents(again.USD) != cents(first.USD) || TeamTotalStamp(dir, "aaaaaaaaaaaa") == "" {
		t.Fatalf("same-stamp read: %+v %q", again, TeamTotalStamp(dir, "aaaaaaaaaaaa"))
	}
}

// A LEDGER THAT APPEARS AFTER A MISSING READ IS FOLDED, NOT PANICKED: the
// first read caches the absent file (its fold carries no lines), the second
// read of the same path finds the file now present, and refresh fills that
// cached fold — the cache from a missing ledger must not be reused as a
// sheet with no line list, or the first line of the appeared file writes
// into a nil map.
func TestTeamTotalOfALedgerThatAppearsAfterAMissingRead(t *testing.T) {
	dir := t.TempDir()
	at := func(h, m int) time.Time { return time.Date(2026, 9, 24, h, m, 0, 0, time.UTC) }
	must(t, Save(dir, []Team{{ID: "aaaaaaaaaaaa", Name: "harbor",
		Members: []Member{{Key: key("0123456789abcdef")}}}}))
	ledger := filepath.Join(dir, "usage.jsonl")
	if none, err := TeamTotalIn(dir, ledger, "aaaaaaaaaaaa"); err != nil || none.USD != 0 {
		t.Fatalf("a missing ledger answered %+v, %v", none, err)
	}
	writeLedger(t, ledger, tline(at(10, 0), 1.5, "0123456789abcdef", ""))
	total, err := TeamTotalIn(dir, ledger, "aaaaaaaaaaaa")
	must(t, err)
	if cents(total.USD) != 150 || total.Calls != 1 {
		t.Fatalf("the appeared ledger totalled %+v", total)
	}
}

// THE LEGACY REMOVAL: a former member moved out before Member.LeftAt existed
// carries no boundary at all. The rule is then "unbounded above" — the plan's
// zero-LeftAt sentence — so the team keeps the whole membership: a line after
// JoinedAt counts, a line before it does not. A removal since LeftAt exists
// stays half-open [JoinedAt, LeftAt).
func TestTeamTotalLegacyFormerMemberWithoutLeftAtCountsFromJoinedAt(t *testing.T) {
	dir := t.TempDir()
	at := func(h, m int) time.Time { return time.Date(2026, 9, 24, h, m, 0, 0, time.UTC) }
	must(t, Save(dir, []Team{{ID: "aaaaaaaaaaaa", Name: "harbor", Made: at(0, 0),
		FormerMembers: []Member{
			{Key: key("2222222222222222"), JoinedAt: at(9, 0)},                    // legacy: no LeftAt
			{Key: key("6666666666666666"), JoinedAt: at(9, 0), LeftAt: at(13, 0)}, // stamped
		}}}))
	ledger := filepath.Join(dir, "usage.jsonl")
	writeLedger(t, ledger,
		tline(at(8, 0), 1.00, "2222222222222222", ""),   // before JoinedAt: nowhere
		tline(at(10, 0), 2.00, "2222222222222222", ""),  // after JoinedAt, no LeftAt: counts
		tline(at(14, 0), 4.00, "2222222222222222", ""),  // even after any plausible removal: counts
		tline(at(12, 0), 8.00, "6666666666666666", ""),  // inside [9:00, 13:00): counts
		tline(at(14, 0), 16.00, "6666666666666666", ""), // after its LeftAt: nowhere
	)
	total, err := TeamTotalIn(dir, ledger, "aaaaaaaaaaaa")
	must(t, err)
	if cents(total.USD) != 1400 || total.Calls != 3 || cents(total.ByMember[key("2222222222222222")]) != 600 {
		t.Fatalf("legacy removal totals %+v", total)
	}
}
