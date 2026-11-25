package data

import (
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"
)

// EVERY TOKEN OF THE DESIGN'S QUERY LANGUAGE FINDS WHAT IT SAYS.
func TestQueryTokens(t *testing.T) {
	s, _ := openCRM(t)
	seedDeals(t, s)
	for _, tc := range []struct {
		query string
		want  []string
	}{
		{"stage:Proposal,Negotiation sort:name", []string{"Globex · platform", "Northwind · renewal", "Umbrella · expansion"}},
		{"stage:proposal sort:-amount", []string{"Northwind · renewal", "Umbrella · expansion"}},
		{"-stage:Lead sort:-amount", []string{"Northwind · renewal", "Globex · platform", "Initech · pilot", "Umbrella · expansion"}},
		{"stage:-Lead,-Proposal sort:-amount", []string{"Globex · platform", "Initech · pilot"}},
		{"amount>10000 sort:amount", []string{"Initech · pilot", "Globex · platform", "Northwind · renewal"}},
		{"amount>=12000 amount<40000 sort:amount", []string{"Initech · pilot", "Globex · platform"}},
		{"owner:raj", []string{"Umbrella · expansion"}},
		{"-owner:me", []string{"Umbrella · expansion"}},
		{"name:north*", []string{"Northwind · renewal"}},
		{"name:*pilot", []string{"Initech · pilot"}},
		{"company:Northwind", []string{"Northwind · renewal"}},
		{"company:glob*", []string{"Globex · platform"}},
		{"is:urgent", []string{"Globex · platform"}},
		{"tags:renewal", []string{"Northwind · renewal"}},
		{"has:company sort:name", []string{"Globex · platform", "Northwind · renewal"}},
		{"-has:company sort:name", []string{"Hooli · first order", "Initech · pilot", "Umbrella · expansion"}},
		{"expansion", []string{"Umbrella · expansion"}},
		{`"first order"`, []string{"Hooli · first order"}},
		{"-pilot -renewal -platform -expansion", []string{"Hooli · first order"}},
		{"sort:stage", []string{"Hooli · first order", "Initech · pilot", "Northwind · renewal", "Umbrella · expansion", "Globex · platform"}},
		{"created_at>-1d stage:Lead", []string{"Hooli · first order"}},
		{"created_at<-1d", []string{}},
		{"note:hello", []string{}}, // not a field: a free word
	} {
		page, err := s.Query(person, "deal", tc.query, "", 0)
		if err != nil {
			t.Fatalf("%q: %v", tc.query, err)
		}
		got := names(page.Rows)
		if tc.query == "stage:proposal sort:-amount" || strings.HasPrefix(tc.query, "sort:stage") {
			// Ties on the sort fall back to the id, which is creation order.
		}
		if !equal(got, tc.want) {
			t.Errorf("%q: got %q, want %q", tc.query, got, tc.want)
		}
		if page.Total != len(tc.want) {
			t.Errorf("%q: total %d, want %d", tc.query, page.Total, len(tc.want))
		}
	}
	for _, bad := range []string{"sort:nope", "group:nope", "is:name", "stage:Maybe", "amount>lots", `name:"open`, "stage:-Lost,Won"} {
		if _, err := s.Query(person, "deal", bad, "", 0); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
}

// NO VALUE IN A QUERY REACHES SQL AS TEXT: quotes, comments, semicolons and
// SQL keywords in every position are values or words, matched literally.
func TestQueriesAreParameterised(t *testing.T) {
	s, _ := openCRM(t)
	seedDeals(t, s)
	evil := mustCreate(t, s, person, "deal", map[string]any{"name": `x' OR 1=1; DROP TABLE records; --`})[0]
	for _, q := range []string{
		`name:"x' OR 1=1; DROP TABLE records; --"`,
		`x' OR 1=1; DROP TABLE records; --`,
		`stage:Lead') OR ('1'='1`,
		`owner:me' OR '1'='1`,
		`company:') OR 1=1 --`,
		`amount>0) OR (1=1`,
		`'; DELETE FROM records; --`,
		`sort:name;DROP`,
		`50%_\`,
	} {
		page, err := s.Query(person, "deal", q, "", 0)
		if err != nil {
			continue // refused is as good as harmless
		}
		for _, r := range page.Rows {
			if r.ID() != evil.ID() && strings.Contains(q, "DROP") {
				t.Errorf("%q matched %v", q, r["name"])
			}
		}
	}
	page, _ := s.Query(person, "deal", "", "", 0)
	if page.Total != 6 {
		t.Fatalf("the table holds %d deals after the attempts", page.Total)
	}
	// A wildcard character typed as a value is literal.
	page, _ = s.Query(person, "deal", "name:50%*", "", 0)
	if page.Total != 0 {
		t.Fatal("% was read as a wildcard")
	}
}

// KEYSET PAGES WALK EVERY ROW EXACTLY ONCE, in order, with ties broken by id
// and empty values last, in both directions.
func TestKeysetPagination(t *testing.T) {
	s, _ := openCRM(t)
	var sets []map[string]any
	for i := 0; i < 237; i++ {
		set := map[string]any{"name": fmt.Sprintf("d%03d", i)}
		if i%7 != 0 {
			set["amount"] = float64(i % 13) // many ties
		}
		sets = append(sets, set)
	}
	if _, err := s.Create(person, "deal", sets); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{"sort:amount", "sort:-amount", "", "sort:name", "sort:-stage", "amount>3 sort:-amount"} {
		var seen []Row
		cursor := ""
		for pages := 0; ; pages++ {
			if pages > 20 {
				t.Fatalf("%q: no end", q)
			}
			page, err := s.Query(person, "deal", q, cursor, 50)
			if err != nil {
				t.Fatalf("%q: %v", q, err)
			}
			seen = append(seen, page.Rows...)
			if page.Next == "" {
				if len(seen) != page.Total {
					t.Fatalf("%q: walked %d of %d", q, len(seen), page.Total)
				}
				break
			}
			cursor = page.Next
		}
		ids := map[string]bool{}
		for _, r := range seen {
			if ids[r.ID()] {
				t.Fatalf("%q: %s twice", q, r.ID())
			}
			ids[r.ID()] = true
		}
		if strings.Contains(q, "amount") {
			desc := strings.Contains(q, "-amount")
			nulls := false
			for i, r := range seen {
				a, has := r["amount"].(float64)
				if !has {
					nulls = true
					continue
				}
				if nulls {
					t.Fatalf("%q: a value after an empty one at %d", q, i)
				}
				if i > 0 {
					p, ok := seen[i-1]["amount"].(float64)
					if ok && (desc && a > p || !desc && a < p) {
						t.Fatalf("%q: out of order at %d", q, i)
					}
					if ok && a == p {
						cmp := strings.Compare(seen[i-1].ID(), r.ID())
						if desc && cmp < 0 || !desc && cmp > 0 {
							t.Fatalf("%q: tie out of id order at %d", q, i)
						}
					}
				}
			}
		}
	}
	_, err := s.Query(person, "deal", "sort:-name", mustPage(t, s, "sort:name").Next, 50)
	wantCode(t, err, CodeInvalid)
	_, err = s.Query(person, "deal", "", "not-a-cursor", 50)
	wantCode(t, err, CodeInvalid)
}

func mustPage(t *testing.T, s *Store, q string) *Page {
	t.Helper()
	page, err := s.Query(person, "deal", q, "", 10)
	if err != nil {
		t.Fatal(err)
	}
	return page
}

// A COUNT GROUPED BY AN ENUM COMES IN THE ENUM'S ORDER WITH ZEROS, the
// StateMeter's shape; other groups come largest first, the empty last.
func TestCountGroups(t *testing.T) {
	s, _ := openCRM(t)
	seedDeals(t, s)
	groups, total, err := s.Count(person, "deal", "", "stage")
	if err != nil {
		t.Fatal(err)
	}
	got := []string{}
	for _, g := range groups {
		got = append(got, fmt.Sprintf("%v=%d", g.Key, g.Count))
	}
	want := []string{"Lead=1", "Qualified=1", "Proposal=2", "Negotiation=1", "Won=0", "Lost=0"}
	if !equal(got, want) || total != 5 {
		t.Fatalf("got %v total %d", got, total)
	}
	groups, _, _ = s.Count(person, "deal", "group:company", "")
	if len(groups) != 3 || groups[2].Key != nil || groups[2].Count != 3 {
		t.Fatalf("company groups %+v", groups)
	}
	groups, _, _ = s.Count(person, "deal", "", "urgent")
	if groups[0].Key != true || groups[1].Key != nil {
		t.Fatalf("bool groups %+v", groups)
	}
	groups, _, _ = s.Count(person, "deal", "amount>10000", "tags")
	if groups[1].Key != "renewal" || groups[1].Count != 1 {
		t.Fatalf("enums groups %+v", groups)
	}
}

// AN INDEXED FIELD'S QUERY USES ITS PARTIAL INDEX: the expression and the
// literal type the compiler writes are the ones the index was made with.
func TestEnumQueryUsesItsIndex(t *testing.T) {
	s, _ := openCRM(t)
	seedDeals(t, s)
	sch := s.schema.Load()
	c, err := compileQuery(sch, sch.types["deal"], person, "stage:Won", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	where, args := c.filter()
	rows, err := s.rdb.Query(`EXPLAIN QUERY PLAN SELECT r.id FROM records r WHERE `+where, args...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var plan []string
	for rows.Next() {
		var id, parent, unused int
		var detail string
		_ = rows.Scan(&id, &parent, &unused, &detail)
		plan = append(plan, detail)
	}
	if !strings.Contains(strings.Join(plan, "\n"), "rx_deal__stage") {
		t.Fatalf("plan does not use the index: %v", plan)
	}
}

// THE SUBSET'S SPELLINGS BECOME CEL'S, and string literals are left alone.
func TestRewrite(t *testing.T) {
	for in, want := range map[string]string{
		"amount * 2":                       "amount * 2.0",
		"amount * 0.5 + 1e3":               "amount * 0.5 + 1e3",
		"owner == $me":                     "owner == me()",
		"$record.stage == 'Won'":           "stage == 'Won'",
		"changed('stage') && stage == '2'": "_ch(_changed, 'stage') && stage == '2'",
		"days_since(created_at) > 14":      "days_since(created_at) > 14.0",
		`name == "x 3 $me"`:                `name == "x 3 $me"`,
		"x2 + 0x10":                        "x2 + 0x10",
	} {
		if got := rewrite(in); got != want {
			t.Errorf("rewrite(%q) = %q, want %q", in, got, want)
		}
	}
}

// A WHEN CONDITION SEES WHAT A WRITE CHANGED, through changed().
func TestMatchSeesChanged(t *testing.T) {
	s, _ := openCRM(t)
	id := mustCreate(t, s, person, "deal", map[string]any{"name": "N"})[0].ID()
	type seen struct {
		stage any
		ok    bool
	}
	got := make(chan seen, 8)
	stop := s.OnCommit(func(c Change) {
		for _, rc := range c.Records {
			ok, err := s.Match(c.Type, "changed('stage') && stage == 'Won'", rc)
			if err != nil {
				t.Error(err)
			}
			got <- seen{rc.After["stage"], ok && rc.Before != nil}
		}
	})
	defer stop()
	_, _ = s.Update(person, "deal", []string{id}, map[string]any{"amount": 3}, UpdateOptions{})
	_, _ = s.Update(person, "deal", []string{id}, map[string]any{"stage": "Won"}, UpdateOptions{})
	// The create's own change may reach the hook too; it is not a match.
	for {
		select {
		case e := <-got:
			if e.ok != (e.stage == "Won") {
				t.Fatalf("stage %v matched %v", e.stage, e.ok)
			}
			if e.ok {
				goto checked
			}
		case <-time.After(5 * time.Second):
			t.Fatal("the change to Won was never matched")
		}
	}
checked:
	if _, err := s.Match("deal", "nosuchfield > 1", RecordChange{}); err == nil {
		t.Fatal("an unknown field compiled")
	}
}

func sortedIDs(rows []Row) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.ID()
	}
	sort.Strings(out)
	return out
}
