package spec

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

type board08Deal struct {
	ID     string `json:"id"`
	Fields struct {
		Name      string `json:"name"`
		Company   string `json:"company"`
		Stage     string `json:"stage"`
		Owner     string `json:"owner"`
		NextStep  string `json:"next_step"`
		Amount    int    `json:"amount"`
		LastTouch string `json:"last_touch"`
	} `json:"fields"`
}

type board08Sample struct {
	Company []struct {
		ID     string `json:"id"`
		Fields struct {
			Name string `json:"name"`
		} `json:"fields"`
	} `json:"company"`
	Deal []board08Deal `json:"deal"`
}

func board08(t *testing.T, data []byte) (board08Sample, map[string]string) {
	t.Helper()
	var sample board08Sample
	if err := json.Unmarshal(data, &sample); err != nil {
		t.Fatal(err)
	}
	names := map[string]string{}
	for _, company := range sample.Company {
		names[company.ID] = company.Fields.Name
	}
	return sample, names
}

// Board 08, frame 01: the stat strip and the open-deal count are sums over
// the seeded deals, never labels.
func TestGoldenCRMBoard08StageCounts(t *testing.T) {
	_, data := GoldenCRM()
	sample, _ := board08(t, data)
	b, err := Load(golden)
	if err != nil {
		t.Fatal(err)
	}
	stages := []string{"Lead", "Talking", "Proposal", "Won", "Lost"}
	if got := b.App.Records["deal"].Fields["stage"].Values; !reflect.DeepEqual(got, stages) {
		t.Fatalf("stages = %v, want %v", got, stages)
	}
	counts := map[string]int{}
	for _, deal := range sample.Deal {
		counts[deal.Fields.Stage]++
	}
	want := map[string]int{"Lead": 14, "Talking": 9, "Proposal": 5, "Won": 23, "Lost": 11}
	if !reflect.DeepEqual(counts, want) {
		t.Fatalf("stage counts = %v, want %v", counts, want)
	}
	if open := counts["Lead"] + counts["Talking"] + counts["Proposal"]; open != 28 {
		t.Fatalf("open deals = %d, want 28", open)
	}
}

// The list's default order is id order, so the board's nine visible rows are
// the first nine deals by id as well as by position in the file.
func TestGoldenCRMBoard08FirstRows(t *testing.T) {
	_, data := GoldenCRM()
	sample, companies := board08(t, data)
	for i, want := range []struct {
		company, deal, stage, owner, next string
		amount                            int
		age                               time.Duration
	}{
		{"Globex", "Annual renewal · 120 seats", "Proposal", "me", "Send revised quote", 48000, 24 * time.Hour},
		{"Northwind", "Team plan · 40 seats", "Talking", "crm-agent", "Follow-up drafted · waiting on you", 14400, 2 * time.Hour},
		{"Initech", "Pilot for the ops team", "Proposal", "mona", "Security review call Thu", 9000, 3 * time.Hour},
		{"Hooli", "Expansion · EU region", "Talking", "jm", "Book a demo with their CTO", 22500, 24 * time.Hour},
		{"Umbrella", "Starter plan", "Lead", "crm-agent", "Follow-up drafted · quiet 7d", 3600, 7 * 24 * time.Hour},
		{"Vandelay", "Import tooling seats", "Talking", "raj", "Send case study", 6800, 2 * 24 * time.Hour},
		{"Acme Retail", "Store managers rollout", "Lead", "crm-agent", "Follow-up drafted · quiet 8d", 12000, 8 * 24 * time.Hour},
		{"Stark Labs", "Research team · 15 seats", "Lead", "mona", "Intro call Mon", 5400, 4 * 24 * time.Hour},
		{"Pied Piper", "Annual · 25 seats", "Proposal", "me", "Waiting on legal", 10500, 5 * 24 * time.Hour},
	} {
		t.Run(want.company, func(t *testing.T) {
			got := sample.Deal[i]
			f := got.Fields
			if companies[f.Company] != want.company || f.Name != want.deal || f.Stage != want.stage ||
				f.Owner != want.owner || f.NextStep != want.next || f.Amount != want.amount {
				t.Fatalf("row %d = %+v", i+1, got)
			}
			if i > 0 && got.ID <= sample.Deal[i-1].ID {
				t.Fatalf("id %s does not sort after %s", got.ID, sample.Deal[i-1].ID)
			}
			touched, err := time.Parse(time.RFC3339, f.LastTouch)
			if err != nil {
				t.Fatal(err)
			}
			// An age is shown in its largest whole unit: 7d10m still reads "7d".
			if age := GoldenCRMAsOf.Sub(touched); age < want.age || age >= want.age+time.Hour {
				t.Fatalf("age = %s, want %s", age, want.age)
			}
		})
	}
	if rest := sample.Deal[9:]; rest[0].ID <= sample.Deal[8].ID {
		t.Fatalf("%s sorts before the board rows", rest[0].ID)
	}
}

func TestGoldenCRMSampleAtKeepsAges(t *testing.T) {
	for _, test := range []struct {
		name      string
		now       time.Time
		touch     string
		closeDate string
	}{
		{"as written", GoldenCRMAsOf, "2026-10-13T09:50:00Z", "2026-10-31"},
		{"thirty-six hours on", GoldenCRMAsOf.Add(36 * time.Hour), "2026-10-14T21:50:00Z", "2026-11-01"},
		{"a year on, another zone", GoldenCRMAsOf.AddDate(1, 0, 0).In(time.FixedZone("", 5*3600)), "2027-10-13T09:50:00Z", "2027-10-31"},
	} {
		t.Run(test.name, func(t *testing.T) {
			data, err := GoldenCRMSampleAt(test.now)
			if err != nil {
				t.Fatal(err)
			}
			var rows map[string][]goldenRow
			if err := json.Unmarshal(data, &rows); err != nil {
				t.Fatal(err)
			}
			northwind := rows["deal"][1]
			if northwind.Fields["last_touch"] != test.touch || northwind.Fields["close_on"] != test.closeDate {
				t.Fatalf("northwind = %+v", northwind.Fields)
			}
			if len(rows["deal"]) != 62 || rows["company"][0].Fields["name"] != "Northwind" {
				t.Fatalf("rows changed: %d deals, first company %v", len(rows["deal"]), rows["company"][0].Fields["name"])
			}
		})
	}
}
