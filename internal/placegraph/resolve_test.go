package placegraph

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// noDeny is a source policy that refuses nothing, for tests about the graph.
var noDeny = SourcePolicy{Deny: []string{}}

func placeWith(t *testing.T, s *Store, name string, ctx Context, pol Policy, parents ...string) Place {
	t.Helper()
	p, _, err := s.CreatePlace(NewPlace{Name: name, Parents: parents, Context: ctx, Policy: pol})
	if err != nil {
		t.Fatalf("create %q: %v", name, err)
	}
	return p
}

func file(t *testing.T, s *Store, chat string, places ...Place) {
	t.Helper()
	for _, p := range places {
		if _, _, err := s.AddChat(chat, p.ID, AddedByYou); err != nil {
			t.Fatal(err)
		}
	}
}

func placeIDs(b *Bundle) []string {
	var out []string
	for _, p := range b.Places {
		out = append(out, fmt.Sprintf("%s@%d", p.Name, p.Level))
	}
	return out
}

func TestContextIsTheUnionOfEveryPlaceAndTwoAncestorLevels(t *testing.T) {
	s, _ := newStore(t)
	top := mk(t, s, "Top")
	codeaf := mk(t, s, "codeaf", top.ID)
	software := mk(t, s, "Software", codeaf.ID)
	parser := mk(t, s, "Config parser", software.ID)
	personal := mk(t, s, "Personal")
	file(t, s, "c1", parser, personal)

	b := snap(t, s).Resolve("c1", ResolveOptions{Sources: noDeny})
	got := strings.Join(placeIDs(b), ",")
	// Top is three levels above Config parser: past the design's two.
	if want := "Config parser@0,Personal@0,Software@1,codeaf@2"; got != want {
		t.Fatalf("places = %s, want %s", got, want)
	}
	for _, p := range b.Places {
		if p.Name == "codeaf" && (!p.Inherited || len(p.Through) != 1 || p.Through[0] != parser.ID) {
			t.Fatalf("codeaf must be inherited through Config parser: %+v", p)
		}
		if p.Level == 0 && (p.Inherited || len(p.Through) != 0) {
			t.Fatalf("a filed place is not inherited: %+v", p)
		}
	}
	if b.Counts.Places != 4 {
		t.Fatalf("counts = %+v", b.Counts)
	}
}

func TestAPlaceFiledDirectlyAndAlsoAnAncestorArrivesAtLevelZero(t *testing.T) {
	s, _ := newStore(t)
	codeaf := mk(t, s, "codeaf")
	parser := mk(t, s, "Config parser", codeaf.ID)
	file(t, s, "c1", parser, codeaf)
	b := snap(t, s).Resolve("c1", ResolveOptions{Sources: noDeny})
	if got := strings.Join(placeIDs(b), ","); got != "Config parser@0,codeaf@0" {
		t.Fatalf("places = %s", got)
	}
	if b.Places[1].Inherited || b.Places[1].Through != nil {
		t.Fatalf("codeaf is filed here, not inherited: %+v", b.Places[1])
	}
}

// The design's own example (6e): "Release is under Software and Marketing, so
// codeaf decides."
func TestReleaseUnderSoftwareAndMarketingIsDecidedByCodeaf(t *testing.T) {
	s, _ := newStore(t)
	codeaf := placeWith(t, s, "codeaf", Context{}, Policy{Model: "Pro"})
	software := placeWith(t, s, "Software", Context{}, Policy{Model: "Pro"}, codeaf.ID)
	marketing := placeWith(t, s, "Marketing", Context{}, Policy{Model: "Flash"}, codeaf.ID)
	release := placeWith(t, s, "Release", Context{}, Policy{}, software.ID, marketing.ID)
	file(t, s, "c1", release)

	b := snap(t, s).Resolve("c1", ResolveOptions{Sources: noDeny})
	if len(b.Policy) != 1 {
		t.Fatalf("policy = %+v", b.Policy)
	}
	d := b.Policy[0]
	if d.Field != PolicyModel || d.Outcome != PolicyDecided || d.DecidedBy != codeaf.ID || d.Value != "Pro" {
		t.Fatalf("decision = %+v", d)
	}
	if len(d.Wanted) != 3 || d.Wanted[0].PlaceID != software.ID || d.Wanted[1].PlaceID != marketing.ID {
		t.Fatalf("wanted (nearest first) = %+v", d.Wanted)
	}
}

// 6f: "Model: Pro — Release wanted Flash · codeaf decided", with the chat in
// Config parser and Release and codeaf inherited.
func TestTheUsingPopoverExampleIsDecidedByTheInheritedParent(t *testing.T) {
	s, _ := newStore(t)
	codeaf := placeWith(t, s, "codeaf", Context{}, Policy{Model: "Pro"})
	parser := placeWith(t, s, "Config parser", Context{Instructions: "Keep strict mode the default for public APIs"}, Policy{}, codeaf.ID)
	release := placeWith(t, s, "Release", Context{Instructions: "Write for customers, not engineers"}, Policy{Model: "Flash"}, codeaf.ID)
	file(t, s, "c1", parser, release)

	b := snap(t, s).Resolve("c1", ResolveOptions{Sources: noDeny})
	if got := strings.Join(placeIDs(b), ","); got != "Config parser@0,Release@0,codeaf@1" {
		t.Fatalf("places = %s", got)
	}
	if d := b.Policy[0]; d.Value != "Pro" || d.DecidedBy != codeaf.ID || d.Outcome != PolicyDecided {
		t.Fatalf("decision = %+v", d)
	}
	if len(b.Instructions) != 2 || b.Instructions[0].PlaceID != parser.ID || b.Instructions[1].PlaceID != release.ID {
		t.Fatalf("instructions must be credited to their own place, nearest first: %+v", b.Instructions)
	}
}

func TestNoCommonAncestorNeedsOnePickThenRemembers(t *testing.T) {
	s, path := newStore(t)
	a := placeWith(t, s, "Work", Context{}, Policy{Permissions: "ask"})
	bb := placeWith(t, s, "Home", Context{}, Policy{Permissions: "auto"})
	file(t, s, "c1", a, bb)

	b := snap(t, s).Resolve("c1", ResolveOptions{Sources: noDeny})
	d := b.Policy[0]
	if d.Outcome != PolicyNeedsPick || d.Value != "" || d.DecidedBy != "" {
		t.Fatalf("with no common ancestor nothing may be applied: %+v", d)
	}

	book, err := OpenChoices(filepath.Join(filepath.Dir(path), "place-choices.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := book.Set("c1", PolicyPermissions, bb.ID); err != nil {
		t.Fatal(err)
	}
	picks, err := book.For("c1")
	if err != nil || len(picks) != 1 {
		t.Fatalf("picks = %+v %v", picks, err)
	}
	b = snap(t, s).Resolve("c1", ResolveOptions{Sources: noDeny, Choices: picks})
	if d := b.Policy[0]; d.Outcome != PolicyChosen || d.Value != "auto" || d.DecidedBy != DecidedByYou || d.Chosen != bb.ID {
		t.Fatalf("remembered pick = %+v", d)
	}
	// Another chat in the same two places has not been asked.
	file(t, s, "c2", a, bb)
	if d := snap(t, s).Resolve("c2", ResolveOptions{Sources: noDeny, Choices: picks}).Policy[0]; d.Outcome != PolicyNeedsPick {
		t.Fatalf("a pick is per chat: %+v", d)
	}
	// The picked place stops having an opinion: the question changed, so ask again.
	ok(t)(s.SetPolicy(bb.ID, Policy{}))
	if d := snap(t, s).Resolve("c1", ResolveOptions{Sources: noDeny, Choices: picks}).Policy[0]; d.Outcome != PolicyAgreed || d.Value != "ask" {
		t.Fatalf("with one opinion left it is agreed: %+v", d)
	}
}

func TestTwoNearestCommonAncestorsThatDisagreeNeedAPick(t *testing.T) {
	s, _ := newStore(t)
	x := placeWith(t, s, "X", Context{}, Policy{Model: "m1"})
	y := placeWith(t, s, "Y", Context{}, Policy{Model: "m2"})
	// N and M are plain groupings with no opinion; X and Y sit three levels
	// above the chat's places, outside its context, and still decide.
	n := placeWith(t, s, "N", Context{}, Policy{}, x.ID, y.ID)
	m := placeWith(t, s, "M", Context{}, Policy{}, n.ID)
	a := placeWith(t, s, "A", Context{}, Policy{Model: "a"}, m.ID)
	bb := placeWith(t, s, "B", Context{}, Policy{Model: "b"}, m.ID)
	file(t, s, "c", a, bb)
	if d := snap(t, s).Resolve("c", ResolveOptions{Sources: noDeny}).Policy[0]; d.Outcome != PolicyNeedsPick {
		t.Fatalf("two equally near deciders that disagree cannot decide: %+v", d)
	}
	ok(t)(s.SetPolicy(y.ID, Policy{Model: "m1"}))
	d := snap(t, s).Resolve("c", ResolveOptions{Sources: noDeny}).Policy[0]
	if d.Outcome != PolicyDecided || d.Value != "m1" || d.DecidedBy != x.ID {
		t.Fatalf("agreeing deciders decide, lowest id named: %+v", d)
	}
	// An opinion nearer than X and Y decides before them.
	ok(t)(s.SetPolicy(n.ID, Policy{Model: "n"}))
	if d := snap(t, s).Resolve("c", ResolveOptions{Sources: noDeny}).Policy[0]; d.DecidedBy != n.ID || d.Value != "n" {
		t.Fatalf("nearest opinion = %+v", d)
	}
}

func TestUnrelatedPlacesWithNoCommonOpinionNeedAPick(t *testing.T) {
	s, _ := newStore(t)
	x := placeWith(t, s, "X", Context{}, Policy{Model: "m1"})
	y := placeWith(t, s, "Y", Context{}, Policy{Model: "m1"})
	a := placeWith(t, s, "A", Context{}, Policy{Model: "a"}, x.ID, y.ID)
	bb := placeWith(t, s, "B", Context{}, Policy{Model: "b"}, x.ID, y.ID)
	file(t, s, "c", a, bb)
	// X and Y are themselves in the conversation's context with opinions, and
	// neither is above the other, so nobody is above every opinion.
	if d := snap(t, s).Resolve("c", ResolveOptions{Sources: noDeny}).Policy[0]; d.Outcome != PolicyNeedsPick || d.Value != "" {
		t.Fatalf("decision = %+v", d)
	}
}

func TestSourcesBeyondTheBudgetAreTrimmedAndListed(t *testing.T) {
	s, _ := newStore(t)
	dir := t.TempDir()
	var near []Source
	for i := range ContextSourceBudget {
		near = append(near, Source{ID: fmt.Sprintf("n%d", i), Kind: SourceURL, Ref: fmt.Sprintf("https://near.example/%d", i), AddedBy: AddedByYou})
	}
	folder := Source{ID: "f1", Kind: SourceFolder, Ref: dir, AddedBy: AddedByYou}
	page := Source{ID: "f2", Kind: SourceURL, Ref: "https://far.example/x", AddedBy: AddedByYou}
	parent := placeWith(t, s, "Parent", Context{Sources: []Source{folder}}, Policy{})
	parent2 := placeWith(t, s, "Parent2", Context{Sources: []Source{page}}, Policy{})
	child := placeWith(t, s, "Child", Context{Sources: near}, Policy{}, parent.ID, parent2.ID)
	file(t, s, "c", child)

	b := snap(t, s).Resolve("c", ResolveOptions{Sources: noDeny})
	if len(b.Sources) != ContextSourceBudget || b.Counts.Sources != ContextSourceBudget {
		t.Fatalf("given = %d (counts %+v)", len(b.Sources), b.Counts)
	}
	for _, src := range b.Sources {
		if src.From[0].PlaceID != child.ID {
			t.Fatalf("the nearest place's sources are spent first: %+v", src)
		}
	}
	if len(b.Trimmed) != 2 || b.Trimmed[0].Status != SourceTrimmedStatus || b.Trimmed[0].Reason == "" {
		t.Fatalf("trimmed = %+v", b.Trimmed)
	}
	if b.Trimmed[0].From[0].PlaceID != parent.ID || b.Trimmed[1].From[0].PlaceID != parent2.ID {
		t.Fatalf("trimmed must be listed with their places: %+v", b.Trimmed)
	}
}

func TestRefusedSourcesDoNotSpendTheBudget(t *testing.T) {
	s, _ := newStore(t)
	srcs := []Source{{ID: "bad", Kind: SourceURL, Ref: "ftp://x.example/", AddedBy: AddedByAI}}
	for i := range ContextSourceBudget {
		srcs = append(srcs, Source{ID: fmt.Sprintf("u%d", i), Kind: SourceURL, Ref: fmt.Sprintf("https://x.example/%d", i), AddedBy: AddedByYou})
	}
	p := placeWith(t, s, "P", Context{Sources: srcs}, Policy{})
	file(t, s, "c", p)
	b := snap(t, s).Resolve("c", ResolveOptions{Sources: noDeny})
	if len(b.Refused) != 1 || len(b.Sources) != ContextSourceBudget || len(b.Trimmed) != 0 {
		t.Fatalf("refused=%d given=%d trimmed=%d", len(b.Refused), len(b.Sources), len(b.Trimmed))
	}
}

func TestTheSameSourceFromTwoPlacesIsGivenOnceAndCreditedToBoth(t *testing.T) {
	s, _ := newStore(t)
	dir := t.TempDir()
	a := placeWith(t, s, "A", Context{Sources: []Source{{ID: "s1", Kind: SourceFolder, Ref: dir, AddedBy: AddedByYou}}}, Policy{})
	bb := placeWith(t, s, "B", Context{Sources: []Source{{ID: "s2", Kind: SourceRepo, Ref: dir + "/", Label: "proj", AddedBy: AddedByAI}}}, Policy{})
	file(t, s, "c", a, bb)
	b := snap(t, s).Resolve("c", ResolveOptions{Sources: noDeny})
	if len(b.Sources) != 1 {
		t.Fatalf("sources = %+v", b.Sources)
	}
	u := b.Sources[0]
	if len(u.From) != 2 || u.From[0].PlaceID != a.ID || u.From[1].PlaceID != bb.ID || u.From[1].AddedBy != AddedByAI {
		t.Fatalf("provenance = %+v", u.From)
	}
	if u.Label != "proj" || u.Status != SourceOK {
		t.Fatalf("source = %+v", u)
	}
}

func TestInstructionsBeyondTheBudgetAreCutNearestFirst(t *testing.T) {
	s, _ := newStore(t)
	big := strings.Repeat("é", ContextInstructionBudget/2-1) // just under the whole budget, in two-byte runes
	parent := placeWith(t, s, "Parent", Context{Instructions: "parent rule"}, Policy{})
	same := placeWith(t, s, "Copy", Context{Instructions: "parent rule"}, Policy{})
	child := placeWith(t, s, "Child", Context{Instructions: big + "\nlast line that does not fit"}, Policy{}, parent.ID)
	file(t, s, "c", child, same)
	b := snap(t, s).Resolve("c", ResolveOptions{Sources: noDeny})
	if len(b.Instructions) != 2 {
		t.Fatalf("instructions = %d", len(b.Instructions))
	}
	first := b.Instructions[0]
	if first.PlaceID != child.ID || !first.Trimmed || len(first.Text) > ContextInstructionBudget || strings.Contains(first.Text, "last line") {
		t.Fatalf("child = trimmed %v, %d bytes", first.Trimmed, len(first.Text))
	}
	if !strings.HasSuffix(first.Text, "é") {
		t.Fatal("a cut must not split a character")
	}
	second := b.Instructions[1]
	if second.PlaceID != same.ID || len(second.AlsoFrom) != 1 || second.AlsoFrom[0] != parent.ID {
		t.Fatalf("the same words are given once and credited to both: %+v", second)
	}
	total := 0
	for _, in := range b.Instructions {
		total += len(in.Text)
	}
	if total > ContextInstructionBudget {
		t.Fatalf("spent %d of %d", total, ContextInstructionBudget)
	}
}

func TestResolveIndependentOfWhichStripShowsTheTab(t *testing.T) {
	s, _ := newStore(t)
	codeaf := placeWith(t, s, "codeaf", Context{Instructions: "be brief"}, Policy{Model: "Pro"})
	marketing := placeWith(t, s, "Marketing", Context{}, Policy{})
	file(t, s, "c1", codeaf, marketing)
	file(t, s, "c2", codeaf, marketing)
	sn := snap(t, s)
	one, two := sn.Resolve("c1", ResolveOptions{Sources: noDeny}), sn.Resolve("c2", ResolveOptions{Sources: noDeny})
	two.ChatID = one.ChatID
	a, _ := json.Marshal(one)
	b, _ := json.Marshal(two)
	if string(a) != string(b) {
		t.Fatalf("the same memberships must resolve the same:\n%s\n%s", a, b)
	}
	// Pinning a place and visiting it (what a strip does) changes nothing.
	ok(t)(s.Pin(marketing.ID, -1))
	if err := s.TouchOpened(marketing.ID, time.Now()); err != nil {
		t.Fatal(err)
	}
	again := snap(t, s).Resolve("c1", ResolveOptions{Sources: noDeny})
	again.Revision = one.Revision
	c, _ := json.Marshal(again)
	if string(c) != string(a) {
		t.Fatalf("strip state changed the context:\n%s\n%s", a, c)
	}
}

func TestAnUnplacedChatUsesNothingAndSaysEmptyLists(t *testing.T) {
	s, _ := newStore(t)
	p := placeWith(t, s, "Archived", Context{Instructions: "x"}, Policy{Model: "m"})
	file(t, s, "c", p)
	ok(t)(s.Archive(p.ID))
	b := snap(t, s).Resolve("c", ResolveOptions{Sources: noDeny})
	if !b.Empty() || b.Counts != (UsingCounts{}) {
		t.Fatalf("bundle = %+v", b)
	}
	raw, _ := json.Marshal(b)
	for _, field := range []string{`"places":[]`, `"instructions":[]`, `"sources":[]`, `"trimmed":[]`, `"refused":[]`, `"policy":[]`} {
		if !strings.Contains(string(raw), field) {
			t.Fatalf("%s missing from %s", field, raw)
		}
	}
}

func TestResolveOfADeepTwoHundredPlaceGraphIsBounded(t *testing.T) {
	s, _ := newStore(t)
	prev := ""
	var chain []Place
	for i := range 200 {
		var parents []string
		if prev != "" {
			parents = []string{prev}
		}
		p := placeWith(t, s, fmt.Sprintf("L%03d", i), Context{Instructions: fmt.Sprintf("rule %d", i)}, Policy{Model: fmt.Sprintf("m%d", i%2)}, parents...)
		chain = append(chain, p)
		prev = p.ID
	}
	deepest := chain[len(chain)-1]
	file(t, s, "c", deepest, chain[0])
	sn := snap(t, s)
	start := time.Now()
	var b *Bundle
	for range 50 {
		b = sn.Resolve("c", ResolveOptions{Sources: noDeny})
	}
	if per := time.Since(start) / 50; per > 50*time.Millisecond {
		t.Fatalf("resolve took %v", per)
	}
	if len(b.Places) != 4 {
		t.Fatalf("two filed places plus two ancestor levels: %v", placeIDs(b))
	}
	// L199 (m1) and L000 (m0) disagree; L000 is an ancestor of both, so it decides.
	if d := b.Policy[0]; d.Outcome != PolicyDecided || d.DecidedBy != chain[0].ID || d.Value != "m0" {
		t.Fatalf("decision = %+v", d)
	}
}

func TestReadSnapshotSeesWhatTheStoreWroteAndNeverRepairsTheFile(t *testing.T) {
	s, path := newStore(t)
	a := mk(t, s, "A")
	file(t, s, "c", a)
	sn, err := ReadSnapshot(path)
	if err != nil || sn.Revision != snap(t, s).Revision || len(sn.ContextPlaces("c")) != 1 {
		t.Fatalf("read = %+v %v", sn, err)
	}
	if empty, err := ReadSnapshot(filepath.Join(t.TempDir(), "none.json")); err != nil || len(empty.Places) != 0 {
		t.Fatalf("a missing file is an empty graph: %v", err)
	}
	if err := os.WriteFile(path, []byte("{nope"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadSnapshot(path); !errors.Is(err, ErrInvalid) {
		t.Fatalf("damaged = %v", err)
	}
	if data, _ := os.ReadFile(path); string(data) != "{nope" {
		t.Fatal("the lock-free reader must leave a damaged file for the store")
	}
	if err := os.WriteFile(path, []byte(`{"version":99}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadSnapshot(path); !errors.Is(err, ErrUnsupportedVersion) {
		t.Fatalf("newer = %v", err)
	}
}

func TestChangesSayWhatStartedAndStoppedReachingTheChat(t *testing.T) {
	s, _ := newStore(t)
	dir := t.TempDir()
	codeaf := mk(t, s, "codeaf")
	parser := mk(t, s, "Config parser", codeaf.ID)
	release := placeWith(t, s, "Release", Context{Sources: []Source{{ID: "s", Kind: SourceFile, Ref: filepath.Join(dir, "brand-voice.md"), Label: "brand-voice.md", AddedBy: AddedByYou}}}, Policy{})
	if err := os.WriteFile(filepath.Join(dir, "brand-voice.md"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	file(t, s, "c", parser)
	before := snap(t, s).Resolve("c", ResolveOptions{Sources: noDeny})
	if Changes(nil, before) != nil {
		t.Fatal("an opening is not a change")
	}
	file(t, s, "c", release)
	after := snap(t, s).Resolve("c", ResolveOptions{Sources: noDeny})
	got := Changes(before, after)
	if len(got) != 1 || got[0].Text != "Now also using Release: brand-voice.md" || got[0].Kind != ChangeAdded {
		t.Fatalf("changes = %+v", got)
	}
	ok(t)(s.RemoveChat("c", parser.ID))
	last := snap(t, s).Resolve("c", ResolveOptions{Sources: noDeny})
	var texts []string
	for _, c := range Changes(after, last) {
		texts = append(texts, c.Text)
	}
	if strings.Join(texts, "|") != "No longer using Config parser|No longer using codeaf" {
		t.Fatalf("changes = %v", texts)
	}
	if Changes(last, last) != nil {
		t.Fatal("nothing moved, nothing said")
	}
}

func TestChoicesFromTwoWritersAreAllKept(t *testing.T) {
	path := filepath.Join(t.TempDir(), "place-choices.json")
	one, _ := OpenChoices(path)
	two, _ := OpenChoices(path)
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			book := one
			if i%2 == 1 {
				book = two
			}
			if _, err := book.Set(fmt.Sprintf("chat%d", i), PolicyModel, "pl_a"); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	for i := range 20 {
		got, err := ReadChoices(path, fmt.Sprintf("chat%d", i))
		if err != nil || len(got) != 1 {
			t.Fatalf("chat%d = %+v %v", i, got, err)
		}
	}
	if _, err := one.Set("chat0", PolicyModel, "pl_b"); err != nil {
		t.Fatal(err)
	}
	if got, _ := one.For("chat0"); len(got) != 1 || got[0].PlaceID != "pl_b" {
		t.Fatalf("a new pick replaces the old: %+v", got)
	}
	if _, err := one.Set("chat0", "colour", "pl_b"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unknown field = %v", err)
	}
}

// THE MANUAL STATES THE BUDGETS THIS FILE NAMES. A number written in two places
// drifts, and the manual is the only thing the chat knows about this program, so
// changing a budget without its page fails here.
func TestTheManualStatesTheContextBudgets(t *testing.T) {
	page, err := os.ReadFile(filepath.Join("..", "manual", "chat", "desktop-places.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		fmt.Sprintf("at most **%d sources**", ContextSourceBudget),
		fmt.Sprintf("**%d KiB** (%d bytes)", ContextInstructionBudget>>10, ContextInstructionBudget),
		fmt.Sprintf("**up to %d levels up**", ContextAncestorLevels),
	} {
		if !strings.Contains(strings.ReplaceAll(string(page), "\n", " "), want) {
			t.Errorf("desktop-places.md does not say %q", want)
		}
	}
}
