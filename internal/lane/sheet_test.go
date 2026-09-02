package lane

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/home"
	"github.com/Agent-Field/aforge-v2/internal/lane/lanestub"
)

// ── THE FETCHER THE PACKAGE MAY NOT OWN ─────────────────────────────────────

// overWire is the [Fetcher] a test uses: the same shape the transport package
// will write, small enough to read in one go, and it records what it was asked
// for so that "the right URL with the right key" is a thing this file can
// assert rather than hope for.
type overWire struct {
	mu     sync.Mutex
	url    string
	bearer string
	calls  int
}

func (o *overWire) Fetch(ctx context.Context, address, bearer string) (io.ReadCloser, error) {
	o.mu.Lock()
	o.url, o.bearer, o.calls = address, bearer, o.calls+1
	o.mu.Unlock()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json")
	if strings.TrimSpace(bearer) != "" {
		request.Header.Set("Authorization", "Bearer "+bearer)
	}
	response, err := (&http.Client{Timeout: 15 * time.Second}).Do(request)
	if err != nil {
		return nil, err
	}
	if response.StatusCode != http.StatusOK {
		response.Body.Close()
		// The same reading the real transport makes (internal/provider's
		// sheetFetcher): a 404 is the base saying there is no such page, and it
		// is the one status the sheet may remember a base by.
		if response.StatusCode == http.StatusNotFound {
			return nil, fmt.Errorf("the router answered %s: %w", response.Status, ErrNoSheetHere)
		}
		return nil, fmt.Errorf("the router answered %s", response.Status)
	}
	return response.Body, nil
}

func (o *overWire) asked() (string, string, int) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.url, o.bearer, o.calls
}

// bytesFetcher answers with a body written by hand, for the cases a real router
// cannot be asked to stage: a schema that has moved, an empty sheet.
type bytesFetcher struct{ body string }

func (b bytesFetcher) Fetch(context.Context, string, string) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader(b.body)), nil
}

// scripted is three lanes with numbers stated outright, so that a decode can be
// checked against them digit for digit.
func scripted() []lanestub.Lane {
	return []lanestub.Lane{
		{Name: "Cloudflare", Profile: lanestub.Profile{
			Quant: "fp8", Context: 345_000, MaxOut: 32_000, Uptime: 100, Caches: true, Tools: true,
			PriceIn: 0.0000009, PriceOut: 0.00000132, PriceCache: 0.00000045,
			TTFTms: [4]float64{768, 892, 1037, 1749},
			Rates:  [4]float64{58, 71, 92, 121},
		}},
		{Name: "CoreWeave", Profile: lanestub.Profile{
			Quant: "fp8", Context: 943_000, MaxOut: 943_000, Uptime: 99.6, Tools: true,
			PriceIn: 0.00000014, PriceOut: 0.00000028,
			TTFTms: [4]float64{430, 1200, 4539, 12240},
			Rates:  [4]float64{24, 33, 48, 61},
		}},
		{Name: "AtlasCloud", Profile: lanestub.Profile{
			// The router has derated this one. It is the fp4 lane as well, so the
			// two facts the gate refuses on ride the same row — but the decode is
			// asserted on the column itself, which is the part that was missing.
			Status: -2,
			Quant:  "fp4", Context: 393_000, MaxOut: 65_000, Uptime: 100,
			PriceIn: 0.00000014, PriceOut: 0.00000028,
			TTFTms: [4]float64{1793, 1940, 2109, 3782},
			Rates:  [4]float64{32, 51, 80, 96},
		}},
	}
}

const scriptedModel = "deepseek/deepseek-v4-flash"

// wired is a sheet pointed at a stub router with its cache in a directory the
// test owns. Nothing here may write into the state root of whoever is running
// it.
func wired(t *testing.T) (*sheet, *lanestub.Server, *overWire) {
	t.Helper()
	stub := lanestub.New(scriptedModel, scripted()...)
	t.Cleanup(stub.Close)
	fetch := &overWire{}
	s := newSheet()
	s.wire(stub.URL(), "sk-test-key", fetch, false)
	s.cacheIn(t.TempDir())
	return s, stub, fetch
}

// TestTheSheetDecodesEveryLaneTheRouterPublished is the prior arriving intact.
// Every number below is a number some later decision is made on, so every one
// of them is checked rather than sampled.
func TestTheSheetDecodesEveryLaneTheRouterPublished(t *testing.T) {
	s, _, fetch := wired(t)
	if err := s.Refresh(context.Background(), scriptedModel); err != nil {
		t.Fatalf("refreshing the sheet: %v", err)
	}
	address, bearer, calls := fetch.asked()
	if !strings.HasSuffix(address, "/models/"+scriptedModel+"/endpoints") {
		t.Fatalf("the sheet was fetched from %q", address)
	}
	if bearer != "sk-test-key" || calls != 1 {
		t.Fatalf("the router was asked %d times as %q", calls, bearer)
	}

	rows := s.Rows(scriptedModel)
	if len(rows) != 3 {
		t.Fatalf("the sheet published %d lanes rather than three", len(rows))
	}
	by := map[string]Row{}
	for _, row := range rows {
		if row.ID.Model != scriptedModel {
			t.Fatalf("a row was filed under %q", row.ID.Model)
		}
		by[row.ID.Lane] = row
	}

	cloudflare, ok := by["Cloudflare"]
	if !ok {
		t.Fatal("Cloudflare was not in the sheet")
	}
	if cloudflare.TTFTp50 != 768 || cloudflare.TTFTp75 != 892 || cloudflare.TTFTp90 != 1037 || cloudflare.TTFTp99 != 1749 {
		t.Fatalf("Cloudflare's first-token percentiles read %+v", cloudflare)
	}
	if cloudflare.Ratep50 != 58 || cloudflare.Ratep75 != 71 || cloudflare.Ratep90 != 92 || cloudflare.Ratep99 != 121 {
		t.Fatalf("Cloudflare's throughput percentiles read %+v", cloudflare)
	}
	facts := cloudflare.Facts
	if !facts.Tools || facts.Quant != "fp8" || facts.Context != 345_000 || facts.MaxOut != 32_000 {
		t.Fatalf("Cloudflare's gate facts read %+v", facts)
	}
	if facts.Uptime5m != 100 || !facts.Caches {
		t.Fatalf("Cloudflare's availability read %+v", facts)
	}
	// The prices are dollars PER TOKEN, which is the unit the router publishes
	// and the unit this package keeps. A factor of a million here is the whole
	// bill.
	if facts.PriceIn != 0.0000009 || facts.PriceOut != 0.00000132 || facts.PriceCache != 0.00000045 {
		t.Fatalf("Cloudflare's tariff read %+v", facts)
	}
	if atlas := by["AtlasCloud"]; atlas.Facts.Tools || atlas.Facts.Quant != "fp4" {
		t.Fatalf("AtlasCloud's facts read %+v", atlas.Facts)
	}
	// THE ROUTER'S OWN HEALTH WORD ARRIVES, which it did not until wave 2b: the
	// column was published, undecoded, and a lane the router had marked down was
	// invisible to the gate that should have refused it.
	if atlas := by["AtlasCloud"]; atlas.Facts.Status != -2 {
		t.Fatalf("AtlasCloud's status read %d, want the router's own -2", atlas.Facts.Status)
	}
	if cloudflare.Facts.Status != 0 {
		t.Fatalf("a healthy lane's status read %d, want zero", cloudflare.Facts.Status)
	}
	if core := by["CoreWeave"]; core.Facts.Uptime5m != 99.6 || core.Facts.Caches {
		t.Fatalf("CoreWeave's facts read %+v", core.Facts)
	}
	// The router's own slug for a lane has nowhere to live on a Row, so the
	// sheet keeps it beside them.
	if tag := s.Tag(ID{Model: scriptedModel, Lane: "CoreWeave"}); tag != "coreweave" {
		t.Fatalf("CoreWeave's slug read %q", tag)
	}
}

// TestARowTheSchemaMovedUnderIsSkippedAndTheRestArrive is why the rows are
// decoded one at a time. A third party's schema drifts, and a sheet that went
// from seventeen lanes to none because one of them grew a field would be a
// router steering blind on the day of a deployment nobody told us about.
func TestARowTheSchemaMovedUnderIsSkippedAndTheRestArrive(t *testing.T) {
	const body = `{"data":{"id":"m","endpoints":[
		{"provider_name":"Good","tag":"good","quantization":"fp8","context_length":128000,
		 "max_completion_tokens":8192,"pricing":{"prompt":"0.0000002","completion":"0.0000008"},
		 "supports_tool_choice":{"function":true},"uptime_last_5m":99.9,
		 "latency_last_30m":{"p50":700,"p75":900,"p90":1100,"p99":2000},
		 "throughput_last_30m":{"p50":60,"p75":70,"p90":80,"p99":90}},
		{"provider_name":"Moved","context_length":{"tokens":128000}},
		{"tag":"nameless","latency_last_30m":{"p50":100}},
		{"provider_name":"Also Good","pricing":{"prompt":"free"},
		 "latency_last_30m":{"p50":500,"p90":900},"throughput_last_30m":{"p50":40,"p90":70}}
	]}}`
	s := newSheet()
	s.wire("http://router.invalid/api/v1", "", bytesFetcher{body: body}, false)
	s.cacheIn(t.TempDir())
	if err := s.Refresh(context.Background(), "vendor/model"); err != nil {
		t.Fatalf("refreshing a sheet with one unreadable row: %v", err)
	}
	rows := s.Rows("vendor/model")
	if len(rows) != 2 {
		t.Fatalf("a bad row took %d good ones with it: %+v", 2-len(rows), rows)
	}
	if rows[0].ID.Lane != "Good" || rows[1].ID.Lane != "Also Good" {
		t.Fatalf("the readable lanes came back as %q and %q", rows[0].ID.Lane, rows[1].ID.Lane)
	}
	// A price that cannot be read is nothing rather than free: the emptiness
	// law, at the one place where guessing costs money.
	if rows[1].Facts.PriceIn != 0 {
		t.Fatalf("an unreadable price read back as %v", rows[1].Facts.PriceIn)
	}
	if rows[0].Facts.PriceOut != 0.0000008 {
		t.Fatalf("a readable price read back as %v", rows[0].Facts.PriceOut)
	}
}

// TestASheetThatDecodedToNothingLeavesWhatIsAlreadyKnown keeps a schema change
// from emptying a prior this process is still perfectly able to use.
func TestASheetThatDecodedToNothingLeavesWhatIsAlreadyKnown(t *testing.T) {
	s, _, _ := wired(t)
	if err := s.Refresh(context.Background(), scriptedModel); err != nil {
		t.Fatalf("refreshing the sheet: %v", err)
	}
	s.wire("http://router.invalid/api/v1", "", bytesFetcher{body: `{"data":{"endpoints":[]}}`}, false)
	if err := s.Refresh(context.Background(), scriptedModel); err == nil {
		t.Fatal("a sheet that named no lanes reported a successful refresh")
	}
	if len(s.Rows(scriptedModel)) != 3 {
		t.Fatal("an empty answer threw away three lanes we knew about")
	}
}

// TestACachedSheetIsReadBackWithoutTheNetwork is the property a cold process
// depends on: the first request of a session is routed on a prior that cost
// nothing and waited for nothing.
func TestACachedSheetIsReadBackWithoutTheNetwork(t *testing.T) {
	s, stub, _ := wired(t)
	directory := s.dir
	if err := s.Refresh(context.Background(), scriptedModel); err != nil {
		t.Fatalf("refreshing the sheet: %v", err)
	}
	if stub.Sheets(scriptedModel) != 1 {
		t.Fatalf("the router was asked %d times for one refresh", stub.Sheets(scriptedModel))
	}

	// A second process: the same cache directory, and nothing at all that can
	// open a connection.
	next := newSheet()
	next.cacheIn(directory)
	rows := next.Rows(scriptedModel)
	if len(rows) != 3 {
		t.Fatalf("a cold process read %d lanes back from disk", len(rows))
	}
	if rows[0].TTFTp50 != 768 || rows[0].Facts.PriceOut != 0.00000132 {
		t.Fatalf("the cache lost the numbers: %+v", rows[0])
	}
	if next.Tag(ID{Model: scriptedModel, Lane: "Cloudflare"}) != "cloudflare" {
		t.Fatal("the cache lost the router's own slug for a lane")
	}
	if stub.Sheets(scriptedModel) != 1 {
		t.Fatal("reading the cache went to the network")
	}
	if err := next.Refresh(context.Background(), scriptedModel); err != ErrNoSheet {
		t.Fatalf("a sheet with no client reported %v rather than saying it cannot fetch", err)
	}

	// And the file itself carries the moment it was true, which is what lets a
	// beat decide whether it is worth fetching at all.
	data, err := os.ReadFile(cachePathIn(directory, scriptedModel))
	if err != nil {
		t.Fatalf("reading the cache file: %v", err)
	}
	var cached cachedSheet
	if err := json.Unmarshal(data, &cached); err != nil {
		t.Fatalf("the cache file did not decode: %v", err)
	}
	if cached.At.IsZero() || len(cached.Lanes) != 3 || cached.Model != scriptedModel {
		t.Fatalf("the cache file read back as %+v", cached)
	}
}

// TestAModelNobodyHasFetchedPublishesNothing is the empty state, which is a
// normal state and not an error.
func TestAModelNobodyHasFetchedPublishesNothing(t *testing.T) {
	s, _, _ := wired(t)
	if rows := s.Rows("nobody/knows"); rows != nil {
		t.Fatalf("an unfetched model published %d lanes", len(rows))
	}
	if rows := s.Rows(""); rows != nil {
		t.Fatal("a nameless model published lanes")
	}
}

// TestTheBeatFetchesWhatIsStaleAndLeavesWhatIsFresh is the whole reason the
// cache carries a stamp: opening a session a minute after closing one should
// cost nothing.
func TestTheBeatFetchesWhatIsStaleAndLeavesWhatIsFresh(t *testing.T) {
	// A HOME OF ITS OWN, FIRST. [Beat] primes the DEFAULT ledger from every
	// reading, and that ledger writes through a store rooted at AFORGE_HOME —
	// so a test that leaves the state root alone folds this file's scripted
	// lanes into the belief file of whoever ran it, and reads them back on
	// their next real session. The neighbour below has always done this; this
	// one did not, and the junk in a developer's `~/.aforge/v3/lanes.json` came
	// from here.
	t.Setenv(home.EnvVar, t.TempDir())
	t.Cleanup(Default().Reset)
	Default().Reset()
	const other = "qwen/qwen3.5-9b"
	s, stub, _ := wired(t)
	stub.Model(other, scripted()...)
	if err := s.Refresh(context.Background(), scriptedModel); err != nil {
		t.Fatalf("refreshing the sheet: %v", err)
	}

	ctx, stop := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		Beat(ctx, s, []string{scriptedModel, other}, time.Hour)
		close(done)
	}()
	waitFor(t, func() bool { return stub.Sheets(other) == 1 })
	stop()
	<-done

	if got := stub.Sheets(scriptedModel); got != 1 {
		t.Fatalf("the beat re-fetched a sheet that was a moment old: %d readings", got)
	}
	if age, ok := s.freshness(scriptedModel, time.Now()); !ok || age > time.Minute {
		t.Fatalf("the sheet's own age read as %v (known: %v)", age, ok)
	}
	if _, ok := s.freshness("nobody/knows", time.Now()); ok {
		t.Fatal("a sheet nobody fetched claimed an age")
	}
}

// TestTheBeatStopsWhenItIsToldTo keeps a background reading from outliving the
// session that wanted it.
func TestTheBeatStopsWhenItIsToldTo(t *testing.T) {
	s, _, _ := wired(t)
	ctx, stop := context.WithCancel(context.Background())
	stop()
	done := make(chan struct{})
	go func() {
		Beat(ctx, s, []string{scriptedModel}, time.Millisecond)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("a cancelled beat kept beating")
	}
}

// TestTheCacheFileIsOneFilePerModel keeps a model id's slash from becoming a
// directory nobody meant to make.
func TestTheCacheFileIsOneFilePerModel(t *testing.T) {
	path := cachePathIn("/tmp/lanes", "deepseek/deepseek-v4-flash")
	if strings.Count(strings.TrimPrefix(path, "/tmp/lanes/"), "/") != 0 {
		t.Fatalf("a model id made a directory: %q", path)
	}
	if !strings.HasSuffix(path, ".json") {
		t.Fatalf("the cache file is %q", path)
	}
}

// TestThePriorFitsTheSheetsOwnSpread is the arithmetic that turns two published
// percentiles into a log-normal, checked against the sheet this build measured.
func TestThePriorFitsTheSheetsOwnSpread(t *testing.T) {
	got := fit(768, 1037)
	if math.Abs(math.Exp(got.X)-768) > 1e-9 {
		t.Fatalf("the median read back as %.4f", math.Exp(got.X))
	}
	want := math.Log(1037/768.0) / 1.2816
	if math.Abs(math.Sqrt(got.P)-want) > 1e-9 {
		t.Fatalf("the spread fitted to %.6f rather than %.6f", math.Sqrt(got.P), want)
	}
	// A sheet that says nothing about spread gets an honestly wide prior rather
	// than a confident one.
	if flat := fit(700, 700); math.Abs(math.Sqrt(flat.P)-defaultSpread) > 1e-12 {
		t.Fatalf("a sheet with no spread fitted %.4f", math.Sqrt(flat.P))
	}
	if (fit(0, 100)).Known() {
		t.Fatal("a lane the sheet published no median for was given a prior")
	}
}

// waitFor polls until something is true, or fails the test. It is how a test
// waits on a beat that is running in a goroutine the test does not own.
func waitFor(t *testing.T, done func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if done() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("waited three seconds for something that never happened")
}

// TestTheBeatPrimesTheBeliefFromEveryReading is the seam between the two halves
// of a prior: the sheet fetching one, and the ledger holding it.
//
// A REFRESH THAT NOBODY PRIMES FROM IS A PRIOR NOTHING READS. The chooser asks
// the LEDGER and never the sheet, so a build that fetched on a beat and primed
// somewhere else would work exactly until the two drifted — and then be blind on
// the first call of every process, with no symptom but slowness. The two are one
// act ([Beat]) for that reason, and this holds it.
func TestTheBeatPrimesTheBeliefFromEveryReading(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	t.Cleanup(Default().Reset)
	Default().Reset()
	s, _, _ := wired(t)

	if believed := Default().Ledger().Beliefs(scriptedModel); len(believed) != 0 {
		t.Fatalf("the ledger believed %d lanes before anything was read", len(believed))
	}
	ctx, stop := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		Beat(ctx, s, []string{scriptedModel}, time.Hour)
		close(done)
	}()
	waitFor(t, func() bool { return len(Default().Ledger().Beliefs(scriptedModel)) > 0 })
	stop()
	<-done

	believed := Default().Ledger().Beliefs(scriptedModel)
	if len(believed) != len(s.Rows(scriptedModel)) {
		t.Fatalf("the beat read %d lanes and the belief holds %d", len(s.Rows(scriptedModel)), len(believed))
	}
	for _, belief := range believed {
		if !belief.TTFT.Known() || !belief.Rate.Known() {
			t.Fatalf("%s was primed with no timing at all: %+v", belief.ID.Lane, belief)
		}
		if !belief.Quality.Known() {
			t.Fatalf("%s was primed with no quality prior, so the gate has nothing to ask", belief.ID.Lane)
		}
	}
}
