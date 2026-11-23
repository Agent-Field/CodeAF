package session

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/provider"
)

// batchMedia answers GenerateImage with exactly the encoded pictures it is
// given, whatever n was asked, and counts the calls. Nothing here is a model.
type batchMedia struct {
	*scriptedMedia
	mu             sync.Mutex
	calls          int
	items          []string
	usage          *ai.Usage
	err            error
	receiptOnError bool
}

func (b *batchMedia) GenerateImage(_ context.Context, request provider.ImageRequest) (*provider.ImageResponse, error) {
	b.mu.Lock()
	b.calls++
	b.scriptedMedia.seen = append(b.scriptedMedia.seen, request)
	b.mu.Unlock()
	if b.err != nil {
		if b.receiptOnError {
			return &provider.ImageResponse{Usage: b.usage}, b.err
		}
		return nil, b.err
	}
	response := &provider.ImageResponse{Usage: b.usage}
	for _, item := range b.items {
		response.Data = append(response.Data, provider.GeneratedImage{Base64: item, MediaType: "image/png"})
	}
	return response, nil
}

func encodedPictures(t *testing.T, count int) []string {
	t.Helper()
	var out []string
	for i := 0; i < count; i++ {
		out = append(out, base64.StdEncoding.EncodeToString(pngOfSize(t, 4+i, 3)))
	}
	return out
}

type batchRig struct {
	agent     *Agent
	workspace string
	index     string
	media     *batchMedia
}

func newBatchRig(t *testing.T, items []string, usage *ai.Usage) *batchRig {
	t.Helper()
	media := &batchMedia{scriptedMedia: &scriptedMedia{}, items: items, usage: usage}
	index := filepath.Join(t.TempDir(), "artifacts.jsonl")
	agent, workspace := newMediaAgent(t, nil, func(config *Config) {
		config.Media = media
		config.ArtifactsIndex = index
	})
	return &batchRig{agent: agent, workspace: workspace, index: index, media: media}
}

func (r *batchRig) run(t *testing.T, args string) (string, bool) {
	t.Helper()
	ctx := withCallID(withRunIdentity(context.Background()), "call_sheet")
	out, bad, err := beltTool(t, r.agent, "generate_image").Execute(ctx, json.RawMessage(args))
	if err != nil {
		t.Fatalf("harness error: %v", err)
	}
	return out, bad
}

func (r *batchRig) facts(t *testing.T) []*MediaFact {
	t.Helper()
	var out []*MediaFact
	for _, row := range ReadArtifacts(r.index) {
		if row.Media != nil {
			out = append(out, row.Media)
		}
	}
	return out
}

func leftoverTemps(t *testing.T, root string) []string {
	t.Helper()
	var found []string
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err == nil && strings.HasPrefix(info.Name(), ".codeaf-image-") {
			found = append(found, path)
		}
		return nil
	})
	return found
}

func TestSingleImageStaysTheCompatibleShape(t *testing.T) {
	cost := 0.04
	rig := newBatchRig(t, encodedPictures(t, 1), &ai.Usage{Cost: &cost})
	out, bad := rig.run(t, `{"prompt":"a harbour"}`)
	if bad || rig.media.calls != 1 || rig.media.request(0).N != 1 {
		t.Fatalf("bad=%v calls=%d n=%d out=%s", bad, rig.media.calls, rig.media.request(0).N, out)
	}
	facts := rig.facts(t)
	if len(facts) != 1 {
		t.Fatalf("facts = %d", len(facts))
	}
	f := facts[0]
	if f.Batch != "" || f.VariantIndex != 0 || f.Returned != 0 || f.CostScope != "" || f.CallCostUSD != nil ||
		f.Requested != 1 || f.Charge != MediaCharged || f.CostUSD == nil || *f.CostUSD != cost {
		t.Fatalf("single fact changed shape: %+v", f)
	}
}

func TestFourVariantsAreFourFilesOneRequestOneBatch(t *testing.T) {
	cost := 0.2
	items := encodedPictures(t, 4)
	rig := newBatchRig(t, items, &ai.Usage{Cost: &cost})
	out, bad := rig.run(t, `{"prompt":"a harbour at dusk","n":4}`)
	if bad {
		t.Fatal(out)
	}
	if rig.media.calls != 1 || rig.media.request(0).N != 4 {
		t.Fatalf("calls=%d n=%d; want one request for 4", rig.media.calls, rig.media.request(0).N)
	}
	for _, item := range items {
		if strings.Contains(out, item[:40]) {
			t.Fatal("the result carries base64")
		}
	}
	facts := rig.facts(t)
	if len(facts) != 4 {
		t.Fatalf("facts = %d, want 4", len(facts))
	}
	paths, batch := map[string]bool{}, facts[0].Batch
	seen := map[int]bool{}
	for _, f := range facts {
		paths[f.Canonical] = true
		seen[f.VariantIndex] = true
		if f.Batch == "" || f.Batch != batch || f.Requested != 4 || f.Returned != 4 || f.Tool != "call_sheet" ||
			f.CostScope != MediaCostScopeCall || f.CostUSD != nil || f.CallCostUSD == nil || *f.CallCostUSD != cost ||
			f.Charge != MediaCharged || f.Model != "paint/model" || f.Width == 0 || f.SHA256 == "" {
			t.Fatalf("variant fact = %+v", f)
		}
	}
	if len(paths) != 4 || len(seen) != 4 || !seen[1] || !seen[4] {
		t.Fatalf("paths=%v variants=%v", paths, seen)
	}
	if usage := rig.agent.Usage(); math.Abs(usage.CostUSD-cost) > 1e-9 {
		t.Fatalf("charged %v, want the one reply's %v", usage.CostUSD, cost)
	}
	if temps := leftoverTemps(t, rig.workspace); len(temps) != 0 {
		t.Fatalf("temp files left: %v", temps)
	}
}

func TestTenVariantsAndTheUpperBound(t *testing.T) {
	rig := newBatchRig(t, encodedPictures(t, 10), nil)
	if out, bad := rig.run(t, `{"prompt":"sheet","n":10}`); bad || len(rig.facts(t)) != 10 {
		t.Fatalf("n=10: bad=%v facts=%d %s", bad, len(rig.facts(t)), out)
	}
	for _, args := range []string{`{"prompt":"x","n":11}`, `{"prompt":"x","n":-1}`, `{"prompt":"x","n":2.5}`} {
		before := rig.media.calls
		out, bad := rig.run(t, args)
		if !bad || !strings.HasPrefix(out, "Invalid arguments") || rig.media.calls != before {
			t.Fatalf("%s: bad=%v calls %d->%d out=%s", args, bad, before, rig.media.calls, out)
		}
	}
}

func TestFewerReturnedThanAskedIsRecordedExactly(t *testing.T) {
	rig := newBatchRig(t, encodedPictures(t, 2), nil)
	out, bad := rig.run(t, `{"prompt":"sheet","n":4}`)
	if bad || !strings.Contains(out, "Saved 2 of 4") || !strings.Contains(out, "provider returned 2") {
		t.Fatalf("bad=%v %s", bad, out)
	}
	if rig.media.calls != 1 {
		t.Fatalf("calls = %d; fewer outputs must not trigger more calls", rig.media.calls)
	}
	for _, f := range rig.facts(t) {
		if f.Requested != 4 || f.Returned != 2 {
			t.Fatalf("fact = %+v", f)
		}
		if f.Charge != MediaChargeUnknown || f.CallCostUSD != nil {
			t.Fatalf("invented a charge: %+v", f)
		}
	}
}

func TestAProviderRefusalOfNIsReportedAndNotRetried(t *testing.T) {
	rig := newBatchRig(t, nil, nil)
	rig.media.err = os.ErrInvalid
	out, bad := rig.run(t, `{"prompt":"sheet","n":4}`)
	if !bad || rig.media.calls != 1 || !strings.Contains(out, "nothing was retried") {
		t.Fatalf("bad=%v calls=%d %s", bad, rig.media.calls, out)
	}
}

func TestAChargeOnAnUnusableReplyIsAccountedOnce(t *testing.T) {
	cost := 0.07
	for name, items := range map[string][]string{
		"malformed": {"!!not base64!!"},
		"empty":     nil,
	} {
		t.Run(name, func(t *testing.T) {
			rig := newBatchRig(t, items, &ai.Usage{Cost: &cost})
			if _, bad := rig.run(t, `{"prompt":"a harbour"}`); !bad {
				t.Fatal("an unusable reply must fail")
			}
			if usage := rig.agent.Usage(); math.Abs(usage.CostUSD-cost) > 1e-9 {
				t.Fatalf("session cost = %v, want %v once", usage.CostUSD, cost)
			}
		})
	}
}

func TestOneBadVariantKeepsTheOthersAndTheChargeIsOnce(t *testing.T) {
	cost := 0.3
	items := encodedPictures(t, 3)
	items[1] = "!!not base64!!"
	rig := newBatchRig(t, items, &ai.Usage{Cost: &cost})
	out, bad := rig.run(t, `{"prompt":"sheet","n":3}`)
	if bad || !strings.Contains(out, "Saved 2 of 3") || !strings.Contains(out, "Image 2 was not saved") {
		t.Fatalf("bad=%v %s", bad, out)
	}
	facts := rig.facts(t)
	if len(facts) != 2 || facts[0].VariantIndex+facts[1].VariantIndex != 4 || facts[0].Returned != 3 {
		t.Fatalf("facts = %+v %+v", facts[0], facts[1])
	}
	if usage := rig.agent.Usage(); math.Abs(usage.CostUSD-cost) > 1e-9 {
		t.Fatalf("session cost = %v, want %v once, not per picture", usage.CostUSD, cost)
	}
}

func TestAReplyTooLargeToKeepIsRefusedAfterTheChargeAndBeforeDecoding(t *testing.T) {
	cost := 0.5
	// 25 MiB decoded each: under the per-picture cap, over the total.
	big := strings.Repeat("A", (25<<20)/3*4)
	rig := newBatchRig(t, []string{big, big, big}, &ai.Usage{Cost: &cost})
	out, bad := rig.run(t, `{"prompt":"sheet","n":3}`)
	if !bad || !strings.Contains(out, "none were saved") || !strings.Contains(out, "provider-reported charge was recorded") {
		t.Fatalf("bad=%v %s", bad, out)
	}
	if len(rig.facts(t)) != 0 || rig.media.calls != 1 {
		t.Fatal("a refused reply must record nothing and retry nothing")
	}
	if usage := rig.agent.Usage(); math.Abs(usage.CostUSD-cost) > 1e-9 {
		t.Fatalf("session cost = %v, want %v", usage.CostUSD, cost)
	}
	if why := imageReplyRefusal([]provider.GeneratedImage{{Base64: strings.Repeat("A", (40<<20)/3*4)}, {Base64: "AAAA"}}); why == "" {
		t.Fatal("one oversize picture in a reply of two must be refused")
	}
	if why := imageReplyRefusal([]provider.GeneratedImage{{Base64: strings.Repeat("A", (40<<20)/3*4)}}); why != "" {
		t.Fatal("a single picture keeps its old, larger allowance")
	}
}

func TestNumberedPathsNeverOverwriteUnrelatedFiles(t *testing.T) {
	rig := newBatchRig(t, encodedPictures(t, 2), nil)
	unrelated := filepath.Join(rig.workspace, "sheet-1.png")
	if err := os.WriteFile(unrelated, []byte("precious"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, bad := rig.run(t, `{"prompt":"sheet","n":2,"path":"sheet.png"}`)
	if bad {
		t.Fatal(out)
	}
	if got, _ := os.ReadFile(unrelated); string(got) != "precious" {
		t.Fatalf("an unrelated file was overwritten: %q", got)
	}
	for _, f := range rig.facts(t) {
		if f.Canonical == canonicalFile(unrelated) {
			t.Fatal("recorded the unrelated file")
		}
		if !strings.HasPrefix(filepath.Base(f.Canonical), "sheet-") {
			t.Fatalf("path = %s", f.Canonical)
		}
	}
	if temps := leftoverTemps(t, rig.workspace); len(temps) != 0 {
		t.Fatalf("temps: %v", temps)
	}
}

func TestASingleExplicitPathStillOverwrites(t *testing.T) {
	rig := newBatchRig(t, encodedPictures(t, 1), nil)
	target := filepath.Join(rig.workspace, "draft.png")
	if err := os.WriteFile(target, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, bad := rig.run(t, `{"prompt":"p","path":"draft.png"}`); bad {
		t.Fatal(out)
	}
	if got, _ := os.ReadFile(target); string(got) == "old" || len(got) == 0 {
		t.Fatal("the explicit path was not overwritten")
	}
}

func TestEveryVariantFailingToWriteLeavesNothingAndKeepsTheCharge(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root writes anywhere")
	}
	cost := 0.1
	rig := newBatchRig(t, encodedPictures(t, 2), &ai.Usage{Cost: &cost})
	locked := filepath.Join(rig.workspace, "locked")
	if err := os.Mkdir(locked, 0o500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(locked, 0o700)
	out, bad := rig.run(t, `{"prompt":"p","n":2,"path":"locked/x.png"}`)
	if !bad || strings.Count(out, "was not saved") != 2 {
		t.Fatalf("bad=%v %s", bad, out)
	}
	if entries, _ := os.ReadDir(locked); len(entries) != 0 {
		t.Fatalf("left %d files behind", len(entries))
	}
	if usage := rig.agent.Usage(); math.Abs(usage.CostUSD-cost) > 1e-9 {
		t.Fatalf("session cost = %v", usage.CostUSD)
	}
}

func TestDefaultNamedVariantsGetDistinctClaimedNames(t *testing.T) {
	rig := newBatchRig(t, encodedPictures(t, 3), nil)
	if out, bad := rig.run(t, `{"prompt":"sheet","n":3}`); bad {
		t.Fatal(out)
	}
	names := map[string]bool{}
	for _, f := range rig.facts(t) {
		names[f.Canonical] = true
	}
	if len(names) != 3 {
		t.Fatalf("names = %v", names)
	}
}

func TestTheSchemaTeachesN(t *testing.T) {
	var schema struct {
		Properties map[string]struct {
			Type    string `json:"type"`
			Minimum int    `json:"minimum"`
			Maximum int    `json:"maximum"`
			Default int    `json:"default"`
		} `json:"properties"`
	}
	if err := json.Unmarshal([]byte(generateImageSchemaJSON), &schema); err != nil {
		t.Fatal(err)
	}
	n := schema.Properties["n"]
	if n.Type != "integer" || n.Minimum != 1 || n.Maximum != ImageVariantsMax || n.Default != 1 {
		t.Fatalf("n = %+v", n)
	}
}

func TestFailedImageResponseRetainsItsActualChargeOnce(t *testing.T) {
	cost := 0.13
	rig := newBatchRig(t, nil, &ai.Usage{Cost: &cost})
	rig.media.err = os.ErrInvalid
	rig.media.receiptOnError = true
	out, bad := rig.run(t, `{"prompt":"sheet","n":4}`)
	if !bad || rig.media.calls != 1 || rig.agent.Usage().CostUSD != cost || len(rig.facts(t)) != 0 {
		t.Fatalf("failure lost or duplicated receipt: bad%v usage%+v out%s", bad, rig.agent.Usage(), out)
	}
}
