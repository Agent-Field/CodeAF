package store

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// twoProducers builds one join fed by two leaves and returns the store.
func twoProducers(t *testing.T) *Store {
	t.Helper()
	graph := openTestStore(t, filepath.Join(t.TempDir(), "product.db"))
	if err := graph.Splice(RootID, Subtree{Nodes: []NodeSpec{
		{ID: "join", Brief: "assemble the comparison", Stage: 2, Needs: []Need{
			{NodeID: "wrote", Kind: FeedsInto}, {NodeID: "spoke", Kind: FeedsInto}}},
		{ID: "wrote", Parent: "join", Brief: "evaluate the vendors", Stage: 1},
		{ID: "spoke", Parent: "join", Brief: "state the constraint", Stage: 1},
	}}, Provenance{Origin: OriginUser, Intent: "compare the vendors"}); err != nil {
		t.Fatal(err)
	}
	return graph
}

// The whole defect, in one assertion. A producer whose deliverable is a file
// says so in one sentence, because that is the honest final message for such a
// leaf — and the edge used to carry the sentence. The consumer's own brief then
// told it these were "results from earlier work, which you already have and must
// not gather again", so it was told it held the material, found it held a path,
// and went and got the material: measured, nineteen turns of filesystem
// archaeology across four nodes of one report job.
//
// The test of which half a producer's answer lives in is structural and is the
// record rather than the prose: did this node leave files behind.
func TestTheEdgeCarriesTheProductAndNotTheAnnouncementOfIt(t *testing.T) {
	graph := twoProducers(t)
	dir := t.TempDir()
	report := filepath.Join(dir, "07-vendors.md")
	body := "Vendor A: 41ms p99, $0.30/M.\nVendor B: 88ms p99, $0.12/M.\nVendor C: withdrawn.\n"
	if err := os.WriteFile(report, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := graph.Complete(mustClaim(t, graph, "wrote", "worker"),
		"The evaluation is complete. The file is at: "+report); err != nil {
		t.Fatal(err)
	}
	// The other half of the same rule: a leaf that answered in prose left no
	// file, so its message IS its product and nothing is read for it.
	if err := graph.Complete(mustClaim(t, graph, "spoke", "worker"),
		"The budget ceiling for this comparison is $0.20 per million tokens."); err != nil {
		t.Fatal(err)
	}

	inputs, err := graph.DependencyInputs("join", 64<<10)
	if err != nil {
		t.Fatal(err)
	}
	if len(inputs) != 2 {
		t.Fatalf("inputs = %d, want both producers", len(inputs))
	}
	wrote, spoke := inputs[0], inputs[1]
	if wrote.NodeID != "wrote" || spoke.NodeID != "spoke" {
		t.Fatalf("producers arrived as %q, %q", wrote.NodeID, spoke.NodeID)
	}
	// bounded() trims the ends of whatever it is handed, so the file's closing
	// newline is not part of the assertion; every byte of substance is.
	if !strings.Contains(wrote.Digest, strings.TrimSpace(body)) {
		t.Fatalf("the edge carried a pointer and not the product: %q", wrote.Digest)
	}
	if !strings.Contains(wrote.Digest, report) {
		t.Fatalf("the inlined file is not named, so nothing says what the consumer is "+
			"holding: %q", wrote.Digest)
	}
	if wrote.Handle != "" {
		t.Fatalf("a product that fit was spilled to %q", wrote.Handle)
	}
	if len(wrote.Artifacts) != 1 || wrote.Artifacts[0] != report {
		t.Fatalf("artifacts = %v, want the one file it wrote", wrote.Artifacts)
	}
	if !strings.Contains(spoke.Digest, "budget ceiling") {
		t.Fatalf("the prose answer did not arrive: %q", spoke.Digest)
	}
	if strings.Contains(spoke.Digest, "what it wrote") {
		t.Fatalf("a leaf that wrote nothing was given a file block: %q", spoke.Digest)
	}

	// And the measurement every downstream budget is arithmetic over: the fan-in
	// weighs the product, not the sentence announcing it.
	fanIn, err := graph.DependencyFanIn("join")
	if err != nil {
		t.Fatal(err)
	}
	if fanIn.Count != 2 {
		t.Fatalf("measured %d dependencies, want 2", fanIn.Count)
	}
	if fanIn.Bytes < len(body) {
		t.Fatalf("measured %d bytes over a %d-byte deliverable; the consumer's whole "+
			"grant is sized from this number", fanIn.Bytes, len(body))
	}
}

// The distinction the two entry points exist for. A node that has to work from
// what fed it is handed the work. A reader deciding a shape — a sub-planner
// dividing a claimed node — is reading results to decide a shape and not to do
// the work, so it is handed the producers' accounts and the paths, and eight
// reports are not pushed into a call whose whole output is "these three parts
// are really four".
func TestAReaderDecidingAShapeGetsTheAccountAndNotTheProduct(t *testing.T) {
	graph := twoProducers(t)
	report := filepath.Join(t.TempDir(), "07-vendors.md")
	body := "Vendor A: 41ms p99.\nVendor B: 88ms p99.\n"
	if err := os.WriteFile(report, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := graph.Complete(mustClaim(t, graph, "wrote", "worker"),
		"Done. The file is at: "+report); err != nil {
		t.Fatal(err)
	}
	if err := graph.Complete(mustClaim(t, graph, "spoke", "worker"), "nothing to add"); err != nil {
		t.Fatal(err)
	}

	accounts, err := graph.DependencyAccounts("join", 64<<10)
	if err != nil {
		t.Fatal(err)
	}
	for _, input := range accounts {
		if strings.Contains(input.Digest, "Vendor A") {
			t.Fatalf("%s pushed the product into a call that only decides a shape: %q",
				input.NodeID, input.Digest)
		}
	}
	// The path still travels, so a reader that turns out to need a byte has it.
	if len(accounts[0].Artifacts) != 1 || accounts[0].Artifacts[0] != report {
		t.Fatalf("artifacts = %v; the account without the path is a dead end", accounts[0].Artifacts)
	}
	// And the same fan-in, read by the node that has to work from it.
	inputs, err := graph.DependencyInputs("join", 64<<10)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(inputs[0].Digest, "Vendor A") {
		t.Fatalf("the working consumer was handed the account too: %q", inputs[0].Digest)
	}
}

// The bound, and what happens at it. A product too large for its share is
// clipped exactly as a long summary always was, the clip says so, and every
// withheld byte is reachable through a handle that opens.
func TestAProductPastTheCeilingClipsAndSpillsToAReadableHandle(t *testing.T) {
	graph := twoProducers(t)
	dir := t.TempDir()
	huge := filepath.Join(dir, "01-trace.md")
	body := strings.Repeat("a use-after-free in parser.c, at line 4120.\n", 2000)
	if err := os.WriteFile(huge, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := graph.Complete(mustClaim(t, graph, "wrote", "worker"),
		"Done. Everything is in "+huge); err != nil {
		t.Fatal(err)
	}
	if err := graph.Complete(mustClaim(t, graph, "spoke", "worker"), "nothing to add"); err != nil {
		t.Fatal(err)
	}

	const pot = 8 << 10
	inputs, err := graph.DependencyInputs("join", pot)
	if err != nil {
		t.Fatal(err)
	}
	total := 0
	var clipped DependencyInput
	for _, input := range inputs {
		total += len(input.Digest)
		if input.NodeID == "wrote" {
			clipped = input
		}
	}
	if total > pot {
		t.Fatalf("a %d-byte pot pushed %d bytes", pot, total)
	}
	if clipped.Handle == "" {
		t.Fatal("the product was clipped and no handle was written: the rest is unreachable")
	}
	if !strings.Contains(clipped.Digest, "clipped to fit") ||
		!strings.Contains(clipped.Digest, clipped.Handle) {
		t.Fatalf("the clip does not name where the rest is: %q", clipped.Digest)
	}
	opened, err := os.ReadFile(clipped.Handle)
	if err != nil {
		t.Fatalf("the handle does not open: %v", err)
	}
	if !strings.Contains(string(opened), body[:200]) {
		t.Fatal("the handle holds something other than the product it stands for")
	}
	if len(opened) <= len(clipped.Digest) {
		t.Fatalf("the handle holds %d bytes and the prompt already had %d; nothing moved",
			len(opened), len(clipped.Digest))
	}
}

// One file, one copy, however many producers name it. Two panelists that both
// cite the shared spec used to hand their join two of it and pay twice.
func TestAFileNamedByTwoProducersIsCarriedOnce(t *testing.T) {
	graph := twoProducers(t)
	shared := filepath.Join(t.TempDir(), "spec.md")
	body := "The interface freezes at v3. No new fields.\n"
	if err := os.WriteFile(shared, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"wrote", "spoke"} {
		if err := graph.Complete(mustClaim(t, graph, id, "worker"),
			"worked from "+shared); err != nil {
			t.Fatal(err)
		}
	}
	inputs, err := graph.DependencyInputs("join", 64<<10)
	if err != nil {
		t.Fatal(err)
	}
	carried := 0
	for _, input := range inputs {
		carried += strings.Count(input.Digest, strings.TrimSpace(body))
	}
	if carried != 1 {
		t.Fatalf("the shared file was carried %d times", carried)
	}
}

// summaryPaths recovers absolute-looking words out of prose, so some of what it
// hands back is not a file at all. Nothing that is not a readable regular text
// file may reach a prompt, and none of it may be an error either: a producer
// that mentioned /usr/bin/env is a producer, not a fault.
func TestOnlyReadableTextFilesAreInlined(t *testing.T) {
	graph := twoProducers(t)
	dir := t.TempDir()
	binary := filepath.Join(dir, "image.png")
	if err := os.WriteFile(binary, []byte{0x89, 'P', 'N', 'G', 0x00, 0x01, 0x02}, 0o644); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(dir, "never-written.md")
	if err := graph.Complete(mustClaim(t, graph, "wrote", "worker"),
		fmt.Sprintf("see %s, %s and %s", binary, missing, dir)); err != nil {
		t.Fatal(err)
	}
	if err := graph.Complete(mustClaim(t, graph, "spoke", "worker"), "nothing to add"); err != nil {
		t.Fatal(err)
	}
	inputs, err := graph.DependencyInputs("join", 64<<10)
	if err != nil {
		t.Fatal(err)
	}
	for _, input := range inputs {
		if strings.Contains(input.Digest, "what it wrote") {
			t.Fatalf("%s inlined something that is not a readable text file: %q",
				input.NodeID, input.Digest)
		}
		if strings.IndexByte(input.Digest, 0) >= 0 {
			t.Fatalf("%s carried raw bytes into a prompt", input.NodeID)
		}
	}
}
