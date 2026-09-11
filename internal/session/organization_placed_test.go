package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/standing"
	"github.com/Agent-Field/aforge-v2/internal/workspace"
)

// ── THE CHAT'S FOLDER VIEW TELLS THE TRUTH ──────────────────────────────────
//
// Validator recall (2026-09-11): in a fresh conversation, "what have I got filed
// under my Alpha folder?" called `collections find` and then `show`, and the
// chat answered "Alpha folder — exists, empty" while a standing item was placed
// in Alpha. Show read the folder's references and never its placements, which
// the terminal's `aforge collections show` has printed since S27b. These tests
// hold the chat's show and find to the terminal's answer.

// folderAnswer is the part of show's and find's result these tests read.
type folderAnswer[T any] struct {
	Items  []T  `json:"items"`
	Placed []T  `json:"placed"`
	Next   *int `json:"next_offset"`
}

func askCollections[T any](t *testing.T, a *Agent, args map[string]any) folderAnswer[T] {
	t.Helper()
	raw, _ := json.Marshal(args)
	out, failed, err := a.collectionsTool(context.Background(), raw)
	if err != nil || failed {
		t.Fatalf("collections %v failed: %s %v", args, out, err)
	}
	var answer folderAnswer[T]
	if err := json.Unmarshal([]byte(out), &answer); err != nil {
		t.Fatalf("collections %v answered %s: %v", args, out, err)
	}
	return answer
}

// ownerTitles resolves references the way the door's resolver does for the two
// owners these tests keep: a standing item's title from its store, and a
// conversation by the title the test names. internal/workspaceview is the real
// one and cannot be imported here (it imports this package).
func ownerTitles(items *standing.Store, conversations map[string]string) func(context.Context, *workspace.Store, []workspace.Ref) ([]workspace.ResolvedRef, error) {
	return func(_ context.Context, _ *workspace.Store, refs []workspace.Ref) ([]workspace.ResolvedRef, error) {
		rows := make([]workspace.ResolvedRef, 0, len(refs))
		for _, ref := range refs {
			row := workspace.ResolvedRef{Ref: ref, Available: true}
			switch ref.Kind {
			case workspace.StandingKind:
				item, err := items.Get(ref.ID)
				if errors.Is(err, standing.ErrNotFound) {
					row = workspace.ResolvedRef{Ref: ref, Unavailable: "No standing order with that id is kept here."}
					break
				} else if err != nil {
					return nil, err
				}
				row.Title, row.State = item.Title(), string(item.Status)
			case workspace.ConversationKind:
				row.Title = conversations[ref.ID]
			}
			rows = append(rows, row)
		}
		return rows, nil
	}
}

// SHOW ANSWERS WITH WHAT IS PLACED BESIDE WHAT IS FILED, and each placed thing
// comes back with its kind, its id and one line the model can quote.
func TestAFolderShowsWhatIsPlacedInItBesideWhatItFiles(t *testing.T) {
	a, s, g := organizationFixture(t)
	ctx := context.Background()
	items, err := standing.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	shape := reporting(t.TempDir())
	shape.Words = "keep an eye on my inbox\nand keep reports/inbox.md current"
	work, err := items.Create(shape)
	if err != nil {
		t.Fatal(err)
	}
	placedChat := workspace.Ref{Kind: workspace.ConversationKind, ID: "chat-placed"}
	for _, ref := range []workspace.Ref{{Kind: workspace.StandingKind, ID: work.ID}, placedChat} {
		if err := s.AddPlacement(ctx, g.ID, ref); err != nil {
			t.Fatal(err)
		}
	}
	a.config.Organization.Resolve = ownerTitles(items, map[string]string{a.organizationSource().ID: "the filed chat", placedChat.ID: "the placed chat"})

	shown := askCollections[workspace.ResolvedRef](t, a, map[string]any{"action": "show", "id": g.ID})
	if len(shown.Items) != 1 || shown.Items[0].Ref != a.organizationSource() || shown.Items[0].Title != "the filed chat" {
		t.Fatalf("show's references are %+v, want this chat only", shown.Items)
	}
	if len(shown.Placed) != 2 {
		t.Fatalf("show named %d placed things, want the standing item and the placed chat: %+v", len(shown.Placed), shown.Placed)
	}
	byKind := map[workspace.Kind]workspace.ResolvedRef{}
	for _, row := range shown.Placed {
		byKind[row.Ref.Kind] = row
	}
	if row := byKind[workspace.StandingKind]; row.Ref.ID != work.ID || row.Title != "keep an eye on my inbox and keep reports/inbox.md current" || row.State != "active" {
		t.Fatalf("the placed standing item came back as %+v; want its id, its words on one line and its state", row)
	}
	if row := byKind[workspace.ConversationKind]; row.Ref != placedChat || row.Title != "the placed chat" {
		t.Fatalf("the placed conversation came back as %+v", row)
	}
	if shown.Next != nil {
		t.Fatalf("a three-row folder asked for another page at %d", *shown.Next)
	}
}

// ONE PAGE RUNS FROM WHAT A FOLDER FILES INTO WHAT IS PLACED IN IT, under the
// one bound the tool declares and one next_offset, and a page never reads more
// placements than it has room to show.
func TestAShowPageContinuesFromWhatItFilesIntoWhatIsPlaced(t *testing.T) {
	a, s, g := organizationFixture(t)
	ctx := context.Background()
	for i := 1; i < 20; i++ { // the fixture's own chat is the first of 20
		if err := s.Add(ctx, g.ID, workspace.Ref{Kind: workspace.ConversationKind, ID: fmt.Sprintf("filed-%02d", i)}); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 10; i++ {
		if err := s.AddPlacement(ctx, g.ID, workspace.Ref{Kind: workspace.StandingKind, ID: fmt.Sprintf("placed-%02d", i)}); err != nil {
			t.Fatal(err)
		}
	}
	first := askCollections[workspace.ResolvedRef](t, a, map[string]any{"action": "show", "id": g.ID})
	if len(first.Items) != 20 || len(first.Placed) != organizationPageSize-20 || first.Next == nil || *first.Next != organizationPageSize {
		t.Fatalf("the first page held %d filed and %d placed, next %v; want 20, %d and %d", len(first.Items), len(first.Placed), first.Next, organizationPageSize-20, organizationPageSize)
	}
	second := askCollections[workspace.ResolvedRef](t, a, map[string]any{"action": "show", "id": g.ID, "offset": *first.Next})
	if len(second.Items) != 0 || len(second.Placed) != 5 || second.Next != nil {
		t.Fatalf("the second page held %d filed and %d placed, next %v; want 0, 5 and none", len(second.Items), len(second.Placed), second.Next)
	}
	if second.Placed[0].Ref.ID != "placed-05" || second.Placed[4].Ref.ID != "placed-09" {
		t.Fatalf("the second page did not continue where the first stopped: %+v", second.Placed)
	}
}

// A FRESH CONVERSATION FINDS THE WORK ITS FOLDER HOLDS. The item is set up the
// way a person sets it up — one card in a conversation placed in Alpha — and
// then a conversation in no folder asks find, then show, and find by the item's
// own reference, which is how the chat answers "what have I got filed under my
// Alpha folder?" and "which folder is that watch in?".
func TestAFreshConversationFindsTheWorkItsFolderHolds(t *testing.T) {
	d := newChatDoor(t, &scriptedCompleter{steps: []step{standCall("s1", inboxWork(nil)), finalText("set up")}}, nil)
	ctx := context.Background()
	alpha := d.folder(t, "Alpha")
	d.folder(t, "Beta")
	if err := d.org.AddPlacement(ctx, alpha.ID, d.chat); err != nil {
		t.Fatal(err)
	}
	d.proposeInbox(t, d.yes)
	work := d.only(t)

	later := Place{Dir: filepath.Join(t.TempDir(), "chat-later"), Workspace: d.project}
	if err := os.MkdirAll(later.Dir, 0o700); err != nil {
		t.Fatal(err)
	}
	fresh, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.Workspace, config.Place, config.SessionFile = d.project, later, later.Transcript()
		config.Organization = &Organization{Path: d.orgPath, Resolve: ownerTitles(d.store, nil)}
	})
	found := askCollections[workspace.Collection](t, fresh, map[string]any{"action": "find", "name": "alpha"})
	if len(found.Items) != 1 || found.Items[0].ID != alpha.ID {
		t.Fatalf("find by name answered %+v, want Alpha", found.Items)
	}
	shown := askCollections[workspace.ResolvedRef](t, fresh, map[string]any{"action": "show", "id": found.Items[0].ID})
	var named *workspace.ResolvedRef
	for i, row := range shown.Placed {
		if row.Ref.Kind == workspace.StandingKind {
			named = &shown.Placed[i]
		}
	}
	if named == nil || named.Ref.ID != work.ID || named.Title != work.Title() || !named.Available {
		t.Fatalf("Alpha's show answered %+v placed %+v; want the standing item %s %q", shown.Items, shown.Placed, work.ID, work.Title())
	}

	folders := askCollections[workspace.GoverningCollection](t, fresh, map[string]any{"action": "find", "ref": map[string]string{"kind": "standing", "id": work.ID}})
	if len(folders.Items) != 0 || len(folders.Placed) != 1 || folders.Placed[0].ID != alpha.ID || folders.Placed[0].Name != "Alpha" || folders.Placed[0].Depth != 0 {
		t.Fatalf("find by the item's reference answered %+v placed %+v; want no reference and Alpha, placed directly", folders.Items, folders.Placed)
	}
}

// THE REF NAMES THE STANDING KIND AND WHERE ITS ID COMES FROM. Validator
// descendants (2026-09-11): "also file that acme watch under my Archive folder"
// filed the CONVERSATION, because nothing the model read said an ongoing item is
// a kind a ref can name, or that its id is the one `stand` returned.
func TestTheCollectionsRefNamesTheStandingKind(t *testing.T) {
	var schema struct {
		Properties struct {
			Ref struct {
				Description string `json:"description"`
			} `json:"ref"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(collectionsToolSchema, &schema); err != nil {
		t.Fatal(err)
	}
	said := schema.Properties.Ref.Description
	if !strings.Contains(said, "standing: ") || !strings.Contains(said, "stand returned") {
		t.Fatalf("the collections ref says %q; it must name the standing kind and that its id is the one stand returned", said)
	}
	for _, kind := range []string{"conversation", "task", "standing", "collection", "artifact"} {
		if strings.Count(said, kind+": ") > 1 {
			t.Errorf("the collections ref gives %s more than one line: %q", kind, said)
		}
	}
}

// FILING IS `add`, IN THE PERSON'S OWN VERB. Live descendants run 02
// (2026-09-11): with the standing kind named, "also file that acme watch under
// my Archive folder, just for reference" still went to `place`, which makes
// Archive's rules reach the watch — because the belt said add "manages
// references" and nothing said which verb files. The terminal's usage has
// always said "add files a reference"; the chat's tool now says it the same way.
func TestTheCollectionsToolSaysFilingIsAdd(t *testing.T) {
	a := &Agent{config: Config{Organization: &Organization{Path: "collections.db"}}}
	for _, tool := range a.organizationTools() {
		if tool.Name == "collections" && !strings.Contains(tool.Description, "add files a reference") {
			t.Fatalf("the collections tool does not say which verb files: %q", tool.Description)
		}
	}
}
