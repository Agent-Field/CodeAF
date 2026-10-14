package store_test

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/records"
	"github.com/Agent-Field/codeaf/internal/apps/store"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

// Board 08, frame 01, asked the way the pipeline screen asks: its own list
// query, counted by stage, against the records a first launch seeds.
func TestSeededCRMMatchesBoard08(t *testing.T) {
	ctx := context.Background()
	cfg := svc.Config{Root: t.TempDir()}
	all := &svc.Services{}
	s := store.NewWithOptions(cfg, all, store.Options{})
	all.Store, all.Records = s, records.New(cfg, all)
	def, err := s.Definition(ctx, "crm")
	if err != nil {
		t.Fatal(err)
	}
	list := def.Screens["pipeline"].Body.Children[0]
	scope := svc.Scope{App: "crm", Data: svc.DataReal, Caller: svc.Caller{Member: svc.Owner}}
	seeded := time.Now()

	every, err := all.Records.Query(ctx, scope, bridge.QueryParams{Type: "deal", CountBy: "stage"})
	if err != nil {
		t.Fatal(err)
	}
	strip := map[string]int{"Lead": 14, "Talking": 9, "Proposal": 5, "Won": 23, "Lost": 11}
	if !reflect.DeepEqual(every.Counts, strip) {
		t.Fatalf("stat strip = %v, want %v", every.Counts, strip)
	}
	open, err := all.Records.Query(ctx, scope, bridge.QueryParams{Type: "deal", Query: list.Bind.Query, Limit: 9})
	if err != nil || open.Total != 28 || len(open.Rows) != 9 {
		t.Fatalf("open deals = %d, rows = %d, %v; want 28 and 9", open.Total, len(open.Rows), err)
	}
	for i, want := range []struct {
		company, stage, owner, next string
		age                         time.Duration
	}{
		{"Globex", "Proposal", "me", "Send revised quote", 24 * time.Hour},
		{"Northwind", "Talking", "crm-agent", "Follow-up drafted · waiting on you", 2 * time.Hour},
		{"Initech", "Proposal", "mona", "Security review call Thu", 3 * time.Hour},
		{"Hooli", "Talking", "jm", "Book a demo with their CTO", 24 * time.Hour},
		{"Umbrella", "Lead", "crm-agent", "Follow-up drafted · quiet 7d", 7 * 24 * time.Hour},
		{"Vandelay", "Talking", "raj", "Send case study", 2 * 24 * time.Hour},
		{"Acme Retail", "Lead", "crm-agent", "Follow-up drafted · quiet 8d", 8 * 24 * time.Hour},
		{"Stark Labs", "Lead", "mona", "Intro call Mon", 4 * 24 * time.Hour},
		{"Pied Piper", "Proposal", "me", "Waiting on legal", 5 * 24 * time.Hour},
	} {
		t.Run(want.company, func(t *testing.T) {
			f := open.Rows[i].Fields
			company, err := all.Records.Get(ctx, scope, bridge.GetRecordParams{Type: "company", ID: f["company"].(string)})
			if err != nil {
				t.Fatal(err)
			}
			if company.Fields["name"] != want.company || f["stage"] != want.stage || f["owner"] != want.owner || f["next_step"] != want.next {
				t.Fatalf("row %d = %v at %v", i+1, f, company.Fields["name"])
			}
			touched, err := time.Parse(time.RFC3339Nano, f["last_touch"].(string))
			if err != nil {
				t.Fatal(err)
			}
			// Seeded ten minutes past the unit, so the age reads the same for the next fifty.
			if age := seeded.Sub(touched); age < want.age || age >= want.age+time.Hour {
				t.Fatalf("age = %s, want %s", age, want.age)
			}
		})
	}
}
