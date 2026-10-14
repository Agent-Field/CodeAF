package sample

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/records"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

var leadingVerbs = map[string]bool{"review": true, "update": true, "check": true, "improve": true, "prepare": true, "revise": true, "adjust": true, "organize": true, "complete": true, "confirm": true, "refresh": true, "align": true, "polish": true}

func studioApp() *spec.App {
	return &spec.App{ID: "studio", Name: "Studio desk", Records: map[string]*spec.RecordType{
		"client": {Label: "Client", Title: "name", Fields: map[string]*spec.Field{
			"name":       {Type: spec.FieldText},
			"email":      {Type: spec.FieldEmail},
			"joined_on":  {Type: spec.FieldDate},
			"birth_date": {Type: spec.FieldDate},
		}},
		"brand": {Label: "Brand", Title: "name", Fields: map[string]*spec.Field{
			"name":        {Type: spec.FieldText},
			"website_url": {Type: spec.FieldText},
			"notes":       {Type: spec.FieldLongtext},
		}},
		"project": {Label: "Project", Title: "title", Fields: map[string]*spec.Field{
			"title":      {Type: spec.FieldText},
			"brand":      {Type: spec.FieldRef, To: "brand"},
			"status":     {Type: spec.FieldEnum, Values: []string{"Idea", "Brief", "In progress", "Review", "Done"}},
			"budget":     {Type: spec.FieldMoney},
			"price":      {Type: spec.FieldMoney, Label: "Unit price"},
			"rating":     {Type: spec.FieldNumber},
			"due_on":     {Type: spec.FieldDate},
			"started_at": {Type: spec.FieldDatetime},
			"summary":    {Type: spec.FieldLongtext},
		}},
	}}
}

func TestSampleTextReadsLikeRealData(t *testing.T) {
	app := studioApp()
	now := sampleNow()
	rows, err := GenerateAt(context.Background(), app, DefaultCount, 7, now)
	if err != nil {
		t.Fatal(err)
	}
	for typ, list := range rows {
		if len(list) < 6 {
			t.Fatalf("%s has %d rows; a main list needs at least six", typ, len(list))
		}
		schema := app.Records[typ]
		for _, row := range list {
			title := row.Fields[schema.Title].(string)
			if first := strings.ToLower(strings.Fields(title)[0]); leadingVerbs[first] || strings.Contains(title, ": ") {
				t.Errorf("title reads as a chore: %q", title)
			}
			for name, f := range schema.Fields {
				text, ok := row.Fields[name].(string)
				if ok && textual(f.Type) && (strings.Contains(text, "_") || strings.ContainsAny(text, "0123456789")) {
					t.Errorf("%s.%s has an id or counter: %q", typ, name, text)
				}
				if ok && (f.Type == spec.FieldDate || f.Type == spec.FieldDatetime) {
					at, _ := time.Parse(time.RFC3339, text)
					if f.Type == spec.FieldDate {
						at, _ = time.Parse("2006-01-02", text)
					}
					if at.Before(now.AddDate(0, 0, -60)) || at.After(now.AddDate(0, 0, 60)) {
						t.Errorf("%s.%s is %s, too far from now", typ, name, text)
					}
				}
			}
		}
	}
	for _, row := range rows["client"] {
		name := row.Fields["name"].(string)
		want := strings.ToLower(strings.ReplaceAll(name, " ", ".")) + "@"
		if email := row.Fields["email"].(string); !strings.HasPrefix(email, want) || !strings.HasSuffix(email, ".example") {
			t.Errorf("email %q does not belong to %q", email, name)
		}
	}
	for i, row := range rows["project"] {
		brand := rows["brand"][i%len(rows["brand"])].Fields["name"].(string)
		if title := row.Fields["title"].(string); !strings.HasSuffix(title, " — "+brand) {
			t.Errorf("project %q is not named after its brand %q", title, brand)
		}
		if summary := row.Fields["summary"].(string); !strings.Contains(summary, row.Fields["title"].(string)) {
			t.Errorf("summary does not mention its record: %q", summary)
		}
		if budget, price, rating := row.Fields["budget"].(float64), row.Fields["price"].(float64), row.Fields["rating"].(float64); budget < 1000 || price > 1000 || rating < 1 || rating > 5 {
			t.Errorf("magnitudes ignore field meaning: budget %v price %v rating %v", budget, price, rating)
		}
		if due, _ := time.Parse("2006-01-02", row.Fields["due_on"].(string)); !due.After(now) {
			t.Errorf("due date should be upcoming: %v", row.Fields["due_on"])
		}
		if started, _ := time.Parse(time.RFC3339, row.Fields["started_at"].(string)); !started.Before(now) {
			t.Errorf("start time should be past: %v", row.Fields["started_at"])
		}
	}
}

func TestTitlesCarryNoTypePrefixAndTypesDoNotShareATemplate(t *testing.T) {
	for _, app := range []*spec.App{studioApp(), bugTrackerApp(), inventoryApp()} {
		rows, err := GenerateAt(context.Background(), app, DefaultCount, 7, sampleNow())
		if err != nil {
			t.Fatal(err)
		}
		lead := map[int]map[string]string{}
		for typ, list := range rows {
			schema := app.Records[typ]
			for i, row := range list {
				title := row.Fields[schema.Title].(string)
				for _, prefix := range []string{label(schema.Label, typ), typ} {
					if strings.HasPrefix(strings.ToLower(title), strings.ToLower(prefix)+":") {
						t.Errorf("title repeats its type as a prefix: %q", title)
					}
				}
				if kindOf(typ, schema).name != kindItem {
					continue
				}
				first := strings.Fields(title)[0]
				if lead[i] == nil {
					lead[i] = map[string]string{}
				}
				if other, ok := lead[i][first]; ok {
					t.Errorf("row %d of %s and %s start alike: %q", i, typ, other, title)
				}
				lead[i][first] = typ
			}
		}
	}
}

func TestStatusSpreadShowsEveryStateAndFavoursTheMiddle(t *testing.T) {
	rows, err := GenerateAt(context.Background(), studioApp(), DefaultCount, 7, sampleNow())
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for _, row := range rows["project"] {
		counts[row.Fields["status"].(string)]++
	}
	values := studioApp().Records["project"].Fields["status"].Values
	middle := 0
	for i, value := range values {
		if counts[value] == 0 {
			t.Errorf("state %q never appears: %v", value, counts)
		}
		if i > 0 && i < len(values)-1 {
			middle += counts[value]
		}
	}
	if middle*2 <= DefaultCount {
		t.Errorf("most rows should be mid-pipeline: %v", counts)
	}
}

type vocabModel struct {
	reply string
	calls atomic.Int32
}

func (m *vocabModel) Complete(_ context.Context, call svc.ModelCall) (svc.ModelReply, error) {
	m.calls.Add(1)
	return svc.ModelReply{Text: m.reply}, nil
}

func gigApp() *spec.App {
	return &spec.App{ID: "gigs", Name: "Band gigs", Records: map[string]*spec.RecordType{
		"venue": {Label: "Venue", Title: "name", Fields: map[string]*spec.Field{
			"name":  {Type: spec.FieldText},
			"email": {Type: spec.FieldEmail},
			"site":  {Type: spec.FieldURL},
		}},
		"gig": {Label: "Gig", Title: "name", Fields: map[string]*spec.Field{
			"name":      {Type: spec.FieldText},
			"venue":     {Type: spec.FieldRef, To: "venue"},
			"status":    {Type: spec.FieldEnum, Values: []string{"Enquiry", "Confirmed", "Played", "Paid"}},
			"fee":       {Type: spec.FieldMoney},
			"doors_at":  {Type: spec.FieldDatetime},
			"paid_on":   {Type: spec.FieldDate},
			"deposit":   {Type: spec.FieldBool},
			"notes":     {Type: spec.FieldLongtext},
			"fee_twice": {Type: spec.FieldNumber, Computed: "fee * 2.0"},
			"owner":     {Type: spec.FieldPerson},
		}},
	}}
}

// A consistent reply with deliberate faults: an unknown enum value, a ref to
// a venue that was not written, a real-looking email domain, an absolute
// date, a computed value, an unknown type and an unknown field.
const gigReply = "```json\n" + `{"types":{
"venue":[
 {"name":"The Lantern Room","email":"bookings@lanternroom.example","site":"https://www.lanternroom.example"},
 {"name":"Kestrel Hall","email":"events@kestrelhall.com","site":"http://kestrelhall.example"},
 {"name":"Dockside Social","email":"hello@dockside.example","site":"https://www.dockside.example/live"},
 {"name":"Old Mill Tap","email":"gigs@oldmilltap.example","site":"https://www.oldmilltap.example"}],
"gig":[
 {"name":"Autumn residency night","venue":"The Lantern Room","status":"paid","fee":450,"doors_at":"-20d 19:30","paid_on":"-12d","deposit":true,"notes":"Played two sets; fee settled by transfer.","fee_twice":1,"mood":"great"},
 {"name":"Kestrel Hall wedding","venue":"kestrel hall","status":"Confirmed","fee":"1,200","doors_at":"+9d 18:00","paid_on":null,"deposit":true,"notes":"Deposit received; set list due next week."},
 {"name":"Dockside open mic","venue":"Dockside Social","status":"Pencilled","fee":0,"doors_at":"+3d","deposit":false,"notes":"Waiting on a date from the promoter."},
 {"name":"Old Mill charity night","venue":"The Roundhouse","status":"Played","fee":300,"doors_at":"2026-01-01T20:00:00Z","paid_on":"tomorrow","deposit":"yes","notes":"Invoice sent the morning after."}],
"ghost":[{"name":"Nope"},{"name":"Nope"},{"name":"Nope"},{"name":"Nope"}]}}` + "\n```"

func TestModelRowsAreCoercedFieldByFieldAndAskedOnce(t *testing.T) {
	model := &vocabModel{reply: gigReply}
	service := New(svc.Config{}, &svc.Services{Model: model})
	service.Now = sampleNow
	def := &spec.Bundle{App: gigApp(), Plan: &spec.Plan{Ask: "Track my band's gigs and who has paid"}}
	rows, err := service.Generate(context.Background(), def)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Generate(context.Background(), def); err != nil || model.calls.Load() != 1 {
		t.Fatalf("rows should be asked once per definition: %d calls, %v", model.calls.Load(), err)
	}
	if len(rows["gig"]) != 4 || len(rows["venue"]) != 4 || rows["ghost"] != nil {
		t.Fatalf("row counts should follow the model's rows: %d gigs, %d venues", len(rows["gig"]), len(rows["venue"]))
	}
	for typ, list := range rows {
		for _, row := range list {
			if err := records.ValidateFields(def.App.Records[typ], row.Fields); err != nil {
				t.Errorf("%s: %v", typ, err)
			}
		}
	}
	now := sampleNow()
	first, second, third, fourth := rows["gig"][0].Fields, rows["gig"][1].Fields, rows["gig"][2].Fields, rows["gig"][3].Fields
	// The consistent row arrives whole: status, past dates, note and relation agree.
	want := map[string]any{"name": "Autumn residency night", "venue": rows["venue"][0].ID, "status": "Paid", "fee": 450.0,
		"doors_at": "2026-09-23T19:30:00.000000000Z", "paid_on": "2026-10-01", "deposit": true, "notes": "Played two sets; fee settled by transfer."}
	for name, value := range want {
		if first[name] != value {
			t.Errorf("gig.%s = %v, want %v", name, first[name], value)
		}
	}
	if first["fee_twice"] != 900.0 || first["mood"] != nil || first["owner"] != svc.Owner {
		t.Errorf("computed, unknown and member fields must not come from the model: %v", first)
	}
	if second["venue"] != rows["venue"][1].ID || second["fee"] != 1200.0 || second["status"] != "Confirmed" {
		t.Errorf("ref titles resolve without case and money strings parse: %v", second)
	}
	if doors, _ := time.Parse(time.RFC3339, third["doors_at"].(string)); doors.Hour() < workdayStart || doors.Hour() >= workdayStart+workdayHours || !doors.After(now) {
		t.Errorf("an offset without a time gets a working hour: %v", third["doors_at"])
	}
	if !slices.Contains(def.App.Records["gig"].Fields["status"].Values, third["status"].(string)) {
		t.Errorf("an unknown enum value must be replaced: %v", third["status"])
	}
	// Faulty values are dropped one field at a time; the rest of the row stays.
	if fourth["name"] != "Old Mill charity night" || fourth["notes"] != "Invoice sent the morning after." || fourth["status"] != "Played" {
		t.Errorf("good fields of a faulty row were lost: %v", fourth)
	}
	if id, _ := fourth["venue"].(string); !strings.HasPrefix(id, "sample_venue_") {
		t.Errorf("an unknown ref title must fall back to a real target: %v", fourth["venue"])
	}
	if doors, err := time.Parse(time.RFC3339, fourth["doors_at"].(string)); err != nil || doors.Before(now.AddDate(0, 0, -60)) {
		t.Errorf("an absolute date must be replaced by one near now: %v", fourth["doors_at"])
	}
	if _, ok := fourth["deposit"].(bool); !ok {
		t.Errorf("a non-boolean must be replaced: %v", fourth["deposit"])
	}
	venue := rows["venue"][1].Fields
	if email := venue["email"].(string); !strings.HasSuffix(email, ".example") {
		t.Errorf("a real-looking email domain must be replaced: %q", email)
	}
	if site := venue["site"].(string); !strings.HasPrefix(site, "https://") {
		t.Errorf("a non-https link must be replaced: %q", site)
	}
}

func TestUnusableModelRowsFallBackToTheGenerator(t *testing.T) {
	def := &spec.Bundle{App: gigApp()}
	want, _ := GenerateAt(context.Background(), def.App, DefaultCount, 1, sampleNow())
	for name, reply := range map[string]string{
		"not rows": `{"severity":"S1"}`,
		"not json": "Sure! Here are some rows.",
		"too few":  `{"types":{"gig":[{"name":"Only one gig"}]}}`,
	} {
		t.Run(name, func(t *testing.T) {
			service := New(svc.Config{}, &svc.Services{Model: &vocabModel{reply: reply}})
			service.Now = sampleNow
			got, err := service.Generate(context.Background(), def)
			if err != nil || !reflect.DeepEqual(got, want) {
				t.Fatalf("unusable rows should give the deterministic sample: %v", err)
			}
		})
	}
}

func TestFallbackNamesPeopleAndKeepsWorkingHours(t *testing.T) {
	app := &spec.App{ID: "clinic", Records: map[string]*spec.RecordType{
		"therapist": {Label: "Therapist", Title: "name", Fields: map[string]*spec.Field{"name": {Type: spec.FieldText}}},
		"keyholder": {Label: "Keyholder", Title: "name", Fields: map[string]*spec.Field{
			"name": {Type: spec.FieldText}, "phone": {Type: spec.FieldPhone}, "contact": {Type: spec.FieldText}}},
		"animal": {Label: "Animal", Title: "tag_number", Fields: map[string]*spec.Field{"tag_number": {Type: spec.FieldText, Unique: true}}},
		"visit": {Label: "Visit", Title: "summary", Fields: map[string]*spec.Field{
			"summary": {Type: spec.FieldText}, "starts": {Type: spec.FieldDatetime}, "hours": {Type: spec.FieldNumber},
			"notes": {Type: spec.FieldLongtext}, "follow_up": {Type: spec.FieldLongtext}}},
	}}
	rows, err := GenerateAt(context.Background(), app, DefaultCount, 1, sampleNow())
	if err != nil {
		t.Fatal(err)
	}
	for _, typ := range []string{"therapist", "keyholder"} {
		for i, row := range rows[typ] {
			if row.Fields["name"] != personName(i) {
				t.Errorf("%s should be a person, got %q", typ, row.Fields["name"])
			}
		}
	}
	if tag := rows["animal"][0].Fields["tag_number"]; tag != "Tag 1001" {
		t.Errorf("a number title uses the field's own noun: %q", tag)
	}
	for _, row := range rows["visit"] {
		at, _ := time.Parse(time.RFC3339, row.Fields["starts"].(string))
		if at.Hour() < workdayStart || at.Hour() >= workdayStart+workdayHours || at.Second() != 0 {
			t.Errorf("datetime outside working hours: %v", row.Fields["starts"])
		}
		if hours := row.Fields["hours"].(float64); hours > 8 {
			t.Errorf("one entry of %v hours", hours)
		}
		if row.Fields["notes"] == row.Fields["follow_up"] {
			t.Errorf("two notes of one row are identical: %v", row.Fields["notes"])
		}
	}
}

// blockingModel holds calls for one app until released; others answer at once.
type blockingModel struct {
	app     string
	release chan struct{}
	calls   atomic.Int32
}

func (m *blockingModel) Complete(ctx context.Context, call svc.ModelCall) (svc.ModelReply, error) {
	m.calls.Add(1)
	if call.App == m.app {
		select {
		case <-m.release:
		case <-ctx.Done():
			return svc.ModelReply{}, ctx.Err()
		}
	}
	return svc.ModelReply{}, context.DeadlineExceeded
}

func TestVocabularyCallsDoNotBlockOtherAppsAndFailuresAreCached(t *testing.T) {
	model := &blockingModel{app: "slow", release: make(chan struct{})}
	service := New(svc.Config{}, &svc.Services{Model: model})
	service.Now = sampleNow
	slow, fast := studioApp(), studioApp()
	slow.ID, fast.ID = "slow", "fast"
	started := make(chan struct{})
	finished := make(chan error, 1)
	go func() {
		close(started)
		_, err := service.Generate(context.Background(), &spec.Bundle{App: slow})
		finished <- err
	}()
	<-started
	for model.calls.Load() == 0 {
		time.Sleep(time.Millisecond)
	}
	done := make(chan error, 1)
	go func() {
		_, err := service.Generate(context.Background(), &spec.Bundle{App: fast})
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a slow vocabulary call blocked another app")
	}
	close(model.release)
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	before := model.calls.Load()
	for _, app := range []*spec.App{slow, fast} {
		if _, err := service.Generate(context.Background(), &spec.Bundle{App: app}); err != nil {
			t.Fatal(err)
		}
	}
	if model.calls.Load() != before {
		t.Fatalf("a timed-out vocabulary call was asked again: %d → %d calls", before, model.calls.Load())
	}
}

func TestFallbackItemTitlesUseTheirOwnNounWithATimeQualifier(t *testing.T) {
	rows, err := GenerateAt(context.Background(), bugTrackerApp(), DefaultCount, 7, sampleNow())
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows["release"] {
		title := row.Fields["name"].(string)
		words := strings.Fields(title)
		if !strings.HasSuffix(title, " release") || !qualifierWord(words[0]) {
			t.Errorf("fallback title should be <time qualifier> release: %q", title)
		}
	}
}

func qualifierWord(word string) bool {
	for _, q := range titleQualifiers {
		if q == word {
			return true
		}
	}
	return false
}

// Apps the builder wrote whose titles are enums, people or dates, and whose
// unique fields hold fewer values than a preview has rows. They are valid, so
// the sample must fill them.
func TestSampleFillsValidAppsWithNarrowTitlesAndUniqueFields(t *testing.T) {
	for _, file := range []string{"hiring-team.json", "herd-log.json", "vendor-onboarding.json"} {
		t.Run(file, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("testdata", file))
			if err != nil {
				t.Fatal(err)
			}
			var app spec.App
			if err := json.Unmarshal(data, &app); err != nil {
				t.Fatal(err)
			}
			rows, err := GenerateAt(context.Background(), &app, DefaultCount, 1, sampleNow())
			if err != nil {
				t.Fatalf("valid app failed the sample: %v", err)
			}
			for typ, list := range rows {
				if len(list) == 0 {
					t.Errorf("%s has no rows", typ)
				}
				for _, row := range list {
					if err := records.ValidateFields(app.Records[typ], row.Fields); err != nil {
						t.Errorf("%s: %v", typ, err)
					}
				}
			}
		})
	}
	unique := &spec.App{ID: "seats", Records: map[string]*spec.RecordType{
		"seat": {Label: "Seat", Title: "row", Fields: map[string]*spec.Field{
			"row":    {Type: spec.FieldEnum, Values: []string{"Front", "Middle", "Back"}, Unique: true},
			"window": {Type: spec.FieldBool},
			"city":   {Type: spec.FieldText, Unique: true},
		}},
		"ticket": {Label: "Ticket", Title: "name", Fields: map[string]*spec.Field{
			"name": {Type: spec.FieldText},
			"seat": {Type: spec.FieldRef, To: "seat", Unique: true},
		}},
	}}
	rows, err := GenerateAt(context.Background(), unique, 25, 1, sampleNow())
	if err != nil {
		t.Fatal(err)
	}
	if len(rows["seat"]) != 3 || len(rows["ticket"]) != 3 {
		t.Errorf("rows should cap at the unique value space: %d seats, %d tickets", len(rows["seat"]), len(rows["ticket"]))
	}
}

func invoiceApp() *spec.App {
	return &spec.App{ID: "billing", Records: map[string]*spec.RecordType{
		"guest": {Label: "Guest", Title: "name", Fields: map[string]*spec.Field{
			"name":     {Type: spec.FieldText, Unique: true},
			"company":  {Type: spec.FieldText},
			"headshot": {Type: spec.FieldFile},
		}},
		"invoice": {Label: "Invoice", Title: "number", Fields: map[string]*spec.Field{
			"number":     {Type: spec.FieldText, Unique: true},
			"guest":      {Type: spec.FieldRef, To: "guest"},
			"status":     {Type: spec.FieldEnum, Values: []string{"Draft", "Sent", "Paid", "Late"}, Default: json.RawMessage(`"Draft"`)},
			"issue_date": {Type: spec.FieldDate},
			"due_date":   {Type: spec.FieldDate},
			"paid_on":    {Type: spec.FieldDate},
			"date":       {Type: spec.FieldDate},
			"contract":   {Type: spec.FieldFile},
			"flagged":    {Type: spec.FieldBool},
			"streak":     {Type: spec.FieldNumber, Default: json.RawMessage(`0`)},
		}},
	}}
}

func TestInvoicesAreConsistentAndFilesLookUploaded(t *testing.T) {
	app := invoiceApp()
	now := sampleNow()
	rows, err := GenerateAt(context.Background(), app, DefaultCount, 3, now)
	if err != nil {
		t.Fatal(err)
	}
	day := func(v any) time.Time { at, _ := time.Parse("2006-01-02", v.(string)); return at }
	streaks := map[float64]bool{}
	flagged := 0
	for _, row := range rows["invoice"] {
		f := row.Fields
		issued, due := day(f["issue_date"]), day(f["due_date"])
		if issued.After(now) || day(f["date"]).After(now) {
			t.Errorf("issued or bare date in the future: %v %v", f["issue_date"], f["date"])
		}
		if !due.After(issued) {
			t.Errorf("due %v is not after issued %v", f["due_date"], f["issue_date"])
		}
		switch f["status"] {
		case "Paid":
			if f["paid_on"] == nil || day(f["paid_on"]).After(now) {
				t.Errorf("paid invoice needs a past paid date: %v", f["paid_on"])
			}
		case "Late":
			if f["paid_on"] != nil || !due.Before(now) {
				t.Errorf("late invoice must be unpaid and past due: %v", f)
			}
		default:
			if f["paid_on"] != nil {
				t.Errorf("%v invoice has a paid date", f["status"])
			}
		}
		if contract := f["contract"].(string); !strings.HasPrefix(contract, "Contract – ") || !strings.HasSuffix(contract, ".pdf") {
			t.Errorf("file does not look uploaded: %q", contract)
		}
		streaks[f["streak"].(float64)] = true
		if f["flagged"] == true {
			flagged++
		}
	}
	if len(streaks) < 2 || flagged == 0 || flagged*2 > len(rows["invoice"]) {
		t.Errorf("numbers and flags should vary, flags stay rare: %d streak values, %d flagged", len(streaks), flagged)
	}
	for _, row := range rows["guest"] {
		name := row.Fields["name"].(string)
		if orgLike(name) || strings.ContainsAny(name, ".&") || len(strings.Fields(name)) != 2 {
			t.Errorf("guest should be a person: %q", name)
		}
		if photo := row.Fields["headshot"].(string); !strings.HasSuffix(photo, ".jpg") {
			t.Errorf("headshot should be an image: %q", photo)
		}
	}
}

func TestClientWithAContactIsACompany(t *testing.T) {
	app := &spec.App{ID: "freelance", Records: map[string]*spec.RecordType{
		"client": {Label: "Client", Title: "name", Fields: map[string]*spec.Field{
			"name":    {Type: spec.FieldText, Unique: true, Label: "Client"},
			"contact": {Type: spec.FieldText},
			"email":   {Type: spec.FieldEmail},
		}},
	}}
	rows, err := GenerateAt(context.Background(), app, DefaultCount, 1, sampleNow())
	if err != nil {
		t.Fatal(err)
	}
	for i, row := range rows["client"] {
		if row.Fields["name"] != orgName(i) || row.Fields["contact"] != personName(i) {
			t.Errorf("client should be a company with a person as contact: %v", row.Fields)
		}
		if row.Fields["email"] != emailFor(personName(i), i) {
			t.Errorf("email should belong to the contact at the company: %v", row.Fields["email"])
		}
	}
}
