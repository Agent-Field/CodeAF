package workspaceview

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/standing"
	"github.com/Agent-Field/aforge-v2/internal/workspace"
)

// ONE ROW PER RECORD, TWO FLAGS COPIED FROM TWO TABLES. Ongoing work both filed
// and placed in a folder is drawn once, at the position the person filed it,
// carrying both flags; work that is only placed follows the filed rows; and a
// membership never becomes a placement on the way through.
func TestAFolderPageJoinsFiledAndPlacedWithoutDuplicatesOrInferredAuthority(t *testing.T) {
	home := newHome(t)
	ctx := context.Background()
	chat := home.conversation("aaaaaaaaaaaaaa01", "pricing and positioning", time.Now().Add(-time.Hour))
	both := home.hold(t, "review the launch copy")
	placedOnly := home.hold(t, "keep the digest current")
	filedOnly := home.hold(t, "watch the inbox")
	folder := home.collection(t, "Marketing")

	for _, ref := range []workspace.Ref{
		{Kind: workspace.StandingKind, ID: both},
		{Kind: workspace.ConversationKind, ID: chat},
		{Kind: workspace.StandingKind, ID: filedOnly},
	} {
		must(home.collections.Add(ctx, folder, ref))
	}
	must(home.collections.AddPlacement(ctx, folder, workspace.Ref{Kind: workspace.StandingKind, ID: placedOnly}))
	must(home.collections.AddPlacement(ctx, folder, workspace.Ref{Kind: workspace.StandingKind, ID: both}))

	page, err := home.resolver().Folder(ctx, home.collections, folder, 0)
	if err != nil {
		t.Fatalf("folder: %v", err)
	}
	if page.Folder.ID != folder || page.Folder.Name != "Marketing" {
		t.Fatalf("the page names folder %+v", page.Folder)
	}
	want := []struct {
		id            string
		filed, placed bool
	}{{both, true, true}, {chat, true, false}, {filedOnly, true, false}, {placedOnly, false, true}}
	if len(page.Rows) != len(want) {
		t.Fatalf("a folder of three filed and two placed (one shared) answered %d rows: %+v", len(page.Rows), page.Rows)
	}
	for i, w := range want {
		row := page.Rows[i]
		if row.Ref.ID != w.id || row.Filed != w.filed || row.Placed != w.placed {
			t.Fatalf("row %d is %+v, want id %s filed %v placed %v", i, row, w.id, w.filed, w.placed)
		}
		if !row.Available {
			t.Fatalf("row %d lost its record: %+v", i, row)
		}
	}
	if home.reads != 1 {
		t.Fatalf("one page read the machine %d times", home.reads)
	}
	// AND NOTHING WAS INFERRED INTO THE STORE: the filed-only work is still
	// governed by no folder.
	governing, err := home.collections.GoverningCollections(ctx, workspace.Ref{Kind: workspace.StandingKind, ID: filedOnly})
	if err != nil || len(governing) != 0 {
		t.Fatalf("filing gave a folder reach: %+v %v", governing, err)
	}
}

// THE TOP LEVEL IS THE FOLDERS NOTHING FILES. A folder filed under two parents
// is found under both and not at the top; a folder that is only PLACED in another
// is still at the top, because a placement is about rules and not about where a
// person finds something.
func TestTheTopLevelIsFoldersNoMembershipContains(t *testing.T) {
	home := newHome(t)
	ctx := context.Background()
	startup := home.collection(t, "Startup")
	product := home.collection(t, "Product")
	marketing := home.collection(t, "Marketing")
	archive := home.collection(t, "Archive")
	must(home.collections.Add(ctx, startup, workspace.Ref{Kind: workspace.CollectionKind, ID: product}))
	must(home.collections.Add(ctx, startup, workspace.Ref{Kind: workspace.CollectionKind, ID: marketing}))
	must(home.collections.Add(ctx, archive, workspace.Ref{Kind: workspace.CollectionKind, ID: marketing}))
	must(home.collections.AddPlacement(ctx, startup, workspace.Ref{Kind: workspace.CollectionKind, ID: archive}))

	page, err := home.resolver().Folder(ctx, home.collections, "", 0)
	if err != nil {
		t.Fatalf("top level: %v", err)
	}
	var names []string
	for _, row := range page.Rows {
		if row.Ref.Kind != workspace.CollectionKind || row.Filed || row.Placed || !row.Available {
			t.Fatalf("a top-level row reads %+v", row)
		}
		names = append(names, row.Title)
	}
	if !reflect.DeepEqual(names, []string{"Startup", "Archive"}) {
		t.Fatalf("the top level is %v", names)
	}
	if home.reads != 0 {
		t.Fatalf("listing folders read the machine %d times", home.reads)
	}
	for _, parent := range []string{startup, archive} {
		inside, err := home.resolver().Folder(ctx, home.collections, parent, 0)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, row := range inside.Rows {
			found = found || row.Ref.ID == marketing
		}
		if !found {
			t.Fatalf("Marketing is not found inside the folder %q it is filed in", inside.Folder.Name)
		}
	}
}

// A MISSING FOLDER IS NOT AN EMPTY ONE, and a missing record keeps its row.
func TestAFolderPageSaysWhatIsMissingRatherThanHidingIt(t *testing.T) {
	home := newHome(t)
	ctx := context.Background()
	folder := home.collection(t, "Product")
	gone := filepath.Join(home.root, "deleted-spec.md")
	must(home.collections.Add(ctx, folder, workspace.Ref{Kind: workspace.ConversationKind, ID: "ffffffffffffff01"}))
	must(home.collections.Add(ctx, folder, workspace.Ref{Kind: workspace.ArtifactKind, ID: gone}))

	page, err := home.resolver().Folder(ctx, home.collections, folder, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Rows) != 2 || page.Rows[0].Available || page.Rows[1].Available {
		t.Fatalf("missing records read as %+v", page.Rows)
	}
	if page.Rows[0].Unavailable == "" || page.Rows[1].Unavailable == "" {
		t.Fatalf("a missing record says nothing about why: %+v", page.Rows)
	}
	if _, err := home.resolver().Folder(ctx, home.collections, "0123456789abcdef0123456789abcdef", 0); !errors.Is(err, workspace.ErrNotFound) {
		t.Fatalf("a folder that is not there answered %v", err)
	}
}

// A PAGE IS BOUNDED, and says so when it left rows out.
func TestAFolderPageStopsAtItsLimitAndSaysThereIsMore(t *testing.T) {
	home := newHome(t)
	ctx := context.Background()
	folder := home.collection(t, "Big")
	for i := 0; i < 5; i++ {
		must(home.collections.Add(ctx, folder, workspace.Ref{Kind: workspace.ArtifactKind, ID: filepath.Join(home.root, "f"+string(rune('a'+i))+".md")}))
	}
	page, err := home.resolver().Folder(ctx, home.collections, folder, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Rows) != 3 || !page.More {
		t.Fatalf("a limit of three answered %d rows, more=%v", len(page.Rows), page.More)
	}
}

// THE SELECTED ROW, READ CLOSELY: where else it is filed, only placements under
// placed-in, and the rules its own run would read.
func TestAnItemSaysWhereItIsFiledPlacedAndWhichRulesReachIt(t *testing.T) {
	home := newHome(t)
	ctx := context.Background()
	product := home.collection(t, "Product")
	marketing := home.collection(t, "Marketing")
	rule, err := home.standing.Create(standing.Item{
		Words: "product reports never quote contact details", Workspace: home.workspace,
		When: standing.When{Kind: standing.WhenHold}, Scope: &standing.Scope{CollectionIDs: []string{product}},
	})
	must(err)
	work := home.hold(t, "keep the digest current")
	ref := workspace.Ref{Kind: workspace.StandingKind, ID: work}
	must(home.collections.Add(ctx, marketing, ref))
	must(home.collections.AddPlacement(ctx, product, ref))

	item, err := home.resolver().Item(ctx, home.collections, ref)
	if err != nil {
		t.Fatal(err)
	}
	if len(item.Errors) != 0 {
		t.Fatalf("a readable item reported errors %v", item.Errors)
	}
	if len(item.FiledIn) != 1 || item.FiledIn[0].ID != marketing {
		t.Fatalf("filed in %+v", item.FiledIn)
	}
	if len(item.GovernedBy) != 1 || item.GovernedBy[0].ID != product {
		t.Fatalf("placed in %+v — a membership reached the governing list", item.GovernedBy)
	}
	if item.Standing == nil || item.Standing.ID != work {
		t.Fatalf("the ongoing work itself is %+v", item.Standing)
	}
	if len(item.Rules) != 1 || item.Rules[0].ID != rule.ID {
		t.Fatalf("the rules reaching it are %+v", item.Rules)
	}

	// AND THE FOLDER ITSELF LISTS THE RULE WRITTEN FOR IT, and Marketing — where
	// the work is only filed — lists none.
	folderItem, err := home.resolver().Item(ctx, home.collections, workspace.Ref{Kind: workspace.CollectionKind, ID: product})
	if err != nil {
		t.Fatal(err)
	}
	if len(folderItem.Rules) != 1 || folderItem.Rules[0].ID != rule.ID {
		t.Fatalf("Product's own rules are %+v", folderItem.Rules)
	}
	marketingItem, err := home.resolver().Item(ctx, home.collections, workspace.Ref{Kind: workspace.CollectionKind, ID: marketing})
	if err != nil {
		t.Fatal(err)
	}
	if len(marketingItem.Rules) != 0 {
		t.Fatalf("Marketing lists another folder's rule: %+v", marketingItem.Rules)
	}
}

// A FILE IS READ ONLY WHEN A FOLDER NAMES IT, and a binary one is said to be
// binary rather than drawn.
func TestAnArtifactPreviewReadsOnlyWhatAFolderNames(t *testing.T) {
	home := newHome(t)
	ctx := context.Background()
	folder := home.collection(t, "Product")
	spec := filepath.Join(home.root, "spec.md")
	must(os.WriteFile(spec, []byte("# Product spec\n- Offline support\n"), 0o600))
	secret := filepath.Join(home.root, "secret.txt")
	must(os.WriteFile(secret, []byte("not filed\n"), 0o600))
	picture := filepath.Join(home.root, "logo.bin")
	must(os.WriteFile(picture, []byte{0x89, 'P', 'N', 'G', 0, 1, 2}, 0o600))
	must(home.collections.Add(ctx, folder, workspace.Ref{Kind: workspace.ArtifactKind, ID: spec}))
	must(home.collections.Add(ctx, folder, workspace.Ref{Kind: workspace.ArtifactKind, ID: picture}))

	preview, err := home.resolver().Artifact(ctx, home.collections, spec)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(preview.Text, "# Product spec") || preview.Binary || preview.Truncated {
		t.Fatalf("the spec previewed as %+v", preview)
	}
	if _, err := home.resolver().Artifact(ctx, home.collections, secret); !errors.Is(err, ErrNotFiled) {
		t.Fatalf("a file no folder names was read: %v", err)
	}
	binary, err := home.resolver().Artifact(ctx, home.collections, picture)
	if err != nil || !binary.Binary || binary.Text != "" {
		t.Fatalf("a binary file previewed as %+v %v", binary, err)
	}
}

// hold makes one ongoing item through the standing store's own door.
func (f *fixture) hold(t *testing.T, words string) string {
	t.Helper()
	item, err := f.standing.Create(standing.Item{
		Words: words, Workspace: f.workspace,
		When:  standing.When{Kind: standing.WhenFile, Glob: "inbox/*"},
		Does:  standing.Action{Kind: standing.ActionTask, Brief: words},
		Rails: standing.Rails{MaxPerDay: 1},
	})
	if err != nil {
		t.Fatalf("the fixture item would not be made: %v", err)
	}
	return item.ID
}
