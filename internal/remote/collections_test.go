package remote

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/standing"
	"github.com/Agent-Field/aforge-v2/internal/workspace"
	"github.com/Agent-Field/aforge-v2/internal/workspaceview"
)

// THE PERSON'S FOLDERS CROSS WHOLE, AND AN ENGINE WITHOUT THEM REFUSES.
//
// The folders place reads a folder on the machine that keeps it. What these hold
// the wire to is the two properties the place is built on: a page, a row read
// closely and a file's opening arrive with every fact the engine answered —
// including which rows are filed and which are placed — and an engine that was
// not built with the readings says so, which the place draws as "cannot be read
// here" and never as a machine with no folders.
func TestTheFoldersCrossTheWireWholeAndAnAbsentEngineRefuses(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	chat := workspace.Ref{Kind: workspace.ConversationKind, ID: "bbbb000000000002"}
	work := workspace.Ref{Kind: workspace.StandingKind, ID: "digest"}
	var askedFolder, askedFile string
	var askedRef workspace.Ref
	loop, err := Loopback(Hello{Version: Version}, Options{Boot: func(Hello) (*Engine, error) {
		return &Engine{Agent: &fakeAgent{model: "m"}, Collections: EngineCollections{
			Page: func(_ context.Context, id string, limit int) (workspaceview.FolderPage, error) {
				askedFolder = id
				return workspaceview.FolderPage{
					Folder: workspace.Collection{ID: id, Name: "Product"},
					Rows: []workspaceview.FolderRow{
						{ResolvedRef: workspace.ResolvedRef{Ref: chat, Title: "pricing and positioning", Available: true}, Filed: true},
						{ResolvedRef: workspace.ResolvedRef{Ref: work, Title: "the digest", State: "active", Available: true}, Placed: true},
					},
				}, nil
			},
			Item: func(_ context.Context, ref workspace.Ref) (workspaceview.FolderItem, error) {
				askedRef = ref
				return workspaceview.FolderItem{Ref: ref,
					FiledIn:  []workspace.Collection{{ID: "p", Name: "Product"}, {ID: "m", Name: "Marketing"}},
					Standing: &standing.Item{ID: ref.ID, Words: "keep the digest current", LastChecked: now},
				}, nil
			},
			File: func(_ context.Context, path string) (workspaceview.ArtifactPreview, error) {
				askedFile = path
				return workspaceview.ArtifactPreview{Path: path, Size: 12, Text: "# spec\n"}, nil
			},
		}}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = loop.Close() })
	ctx := context.Background()

	page, err := loop.Client.CollectionPage(ctx, "product-id", 10)
	if err != nil {
		t.Fatalf("page: %v", err)
	}
	if askedFolder != "product-id" || page.Folder.Name != "Product" || len(page.Rows) != 2 {
		t.Fatalf("the page crossed as %+v (asked %q)", page, askedFolder)
	}
	if !page.Rows[0].Filed || page.Rows[0].Placed || page.Rows[1].Filed || !page.Rows[1].Placed {
		t.Fatalf("the filed and placed flags did not survive the wire: %+v", page.Rows)
	}
	item, err := loop.Client.CollectionItem(ctx, work)
	if err != nil || askedRef != work || len(item.FiledIn) != 2 || item.Standing == nil || !item.Standing.LastChecked.Equal(now) {
		t.Fatalf("the item crossed as %+v, %v", item, err)
	}
	preview, err := loop.Client.CollectionFile(ctx, "/srv/code/startup/product/spec.md")
	if err != nil || askedFile != "/srv/code/startup/product/spec.md" || preview.Text != "# spec\n" {
		t.Fatalf("the file crossed as %+v, %v", preview, err)
	}

	bare, err := Loopback(Hello{Version: Version}, Options{Boot: func(Hello) (*Engine, error) {
		return &Engine{Agent: &fakeAgent{model: "m"}}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = bare.Close() })
	if _, err := bare.Client.CollectionPage(ctx, "", 0); err == nil || !strings.Contains(err.Error(), collectionsOffWord) {
		t.Fatalf("an engine with no folders answered %v", err)
	}
	if _, err := bare.Client.CollectionFile(ctx, "/etc/passwd"); err == nil {
		t.Fatal("an engine with no folders read a file")
	}
}
