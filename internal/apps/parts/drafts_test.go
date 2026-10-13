package parts

import (
	"context"
	"crypto/sha256"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

func draftScope(real svc.Scope, member string) svc.Scope {
	real.Data = svc.DataDraft
	real.Caller.Member = member
	return real
}

func appliedFiles(t *testing.T, s *Service, app string) map[string][32]byte {
	t.Helper()
	root := filepath.Join(s.cfg.Root, app)
	result := map[string][32]byte{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if entry.Name() == "drafts" {
				return filepath.SkipDir
			}
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		result[relative] = sha256.Sum256(body)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestDraftReadsAppliedAndKeepsSeparateWrites(t *testing.T) {
	s, real, hub := setup(t)
	ctx := context.Background()
	textDoc(t, s, real, "decks/q4")
	before := appliedFiles(t, s, real.App)
	for _, member := range []string{"mona", "raj"} {
		t.Run(member, func(t *testing.T) {
			scope := draftScope(real, member)
			part, err := s.Get(ctx, scope, bridge.GetPartParams{Doc: "decks/q4", Part: "section-1.md"})
			if err != nil || part.Version != 1 || part.Text != "# Team\nJan" {
				t.Fatalf("fallthrough=%+v error=%v", part, err)
			}
			part, err = s.Write(ctx, scope, bridge.WritePartParams{Doc: part.Doc, Part: part.Part, Text: "# Team\n" + member, BaseVersion: 1, Kind: bridge.PartVersionKindNudge})
			if err != nil || part.Version != 2 {
				t.Fatalf("write=%+v error=%v", part, err)
			}
			dir := filepath.Join(s.cfg.Root, real.App, "drafts", member)
			for _, name := range []string{"parts.db", "content/decks/q4/section-1.md"} {
				if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
	for _, member := range []string{"mona", "raj"} {
		part, err := s.Get(ctx, draftScope(real, member), bridge.GetPartParams{Doc: "decks/q4", Part: "section-1.md"})
		if err != nil || part.Text != "# Team\n"+member || part.Version != 2 {
			t.Fatalf("member=%s part=%+v error=%v", member, part, err)
		}
	}
	if got := appliedFiles(t, s, real.App); !reflect.DeepEqual(before, got) {
		t.Fatalf("draft changed applied files: before=%v after=%v", before, got)
	}
	part, err := s.Get(ctx, real, bridge.GetPartParams{Doc: "decks/q4", Part: "section-1.md"})
	if err != nil || part.Version != 1 || part.Text != "# Team\nJan" {
		t.Fatalf("applied=%+v error=%v", part, err)
	}
	if len(hub.events) != 3 {
		t.Fatalf("draft mutations published to applied document topics: %v", hub.events)
	}
}

func TestDraftMutationsStayInOverlay(t *testing.T) {
	for _, operation := range []string{"restore", "add", "remove", "reorder", "create"} {
		t.Run(operation, func(t *testing.T) {
			s, real, _ := setup(t)
			ctx := context.Background()
			textDoc(t, s, real, "decks/q4")
			scope := draftScope(real, "mona")
			before := appliedFiles(t, s, real.App)
			switch operation {
			case "restore":
				p, err := s.Restore(ctx, scope, bridge.RestorePartParams{Doc: "decks/q4", Part: "section-1.md", Version: 1})
				if err != nil || p.Version != 2 {
					t.Fatalf("restore=%+v error=%v", p, err)
				}
			case "add":
				d, err := s.Add(ctx, scope, bridge.AddPartParams{Doc: "decks/q4", Part: "section-3.md", Text: "# Draft"})
				if err != nil || len(d.Parts) != 3 {
					t.Fatalf("add=%+v error=%v", d, err)
				}
			case "remove":
				d, err := s.Remove(ctx, scope, bridge.RemovePartParams{Doc: "decks/q4", Part: "section-1.md"})
				if err != nil || len(d.Parts) != 1 {
					t.Fatalf("remove=%+v error=%v", d, err)
				}
				_, err = s.Get(ctx, scope, bridge.GetPartParams{Doc: "decks/q4", Part: "section-1.md"})
				requireCode(t, err, bridge.CodeNotFound)
				p, err := s.Get(ctx, scope, bridge.GetPartParams{Doc: "decks/q4", Part: "section-1.md", Version: 1})
				if err != nil || p.Version != 1 {
					t.Fatalf("removed history=%+v error=%v", p, err)
				}
			case "reorder":
				d, err := s.Reorder(ctx, scope, bridge.ReorderDocParams{Doc: "decks/q4", Parts: []string{"section-2.md", "section-1.md"}})
				if err != nil || d.Parts[0].Part != "section-2.md" {
					t.Fatalf("reorder=%+v error=%v", d, err)
				}
			case "create":
				textDoc(t, s, scope, "decks/draft-only")
				_, err := s.CreateDoc(ctx, scope, bridge.CreateDocParams{Doc: "decks/q4", Surface: bridge.SurfaceText})
				requireCode(t, err, bridge.CodeConflict)
			}
			docs, err := s.ListDocs(ctx, scope, bridge.ListDocsParams{})
			expected := 1
			if operation == "create" {
				expected = 2
			}
			if err != nil || len(docs.Docs) != expected {
				t.Fatalf("list=%+v error=%v", docs, err)
			}
			if got := appliedFiles(t, s, real.App); !reflect.DeepEqual(before, got) {
				t.Fatal("draft mutation changed applied files")
			}
		})
	}
}

func TestDraftHistoryForksAndUntouchedPartsReadLatestApplied(t *testing.T) {
	s, real, _ := setup(t)
	ctx := context.Background()
	textDoc(t, s, real, "decks/q4")
	scope := draftScope(real, "mona")
	write := func(scope svc.Scope, id, text string, base int) {
		t.Helper()
		if _, err := s.Write(ctx, scope, bridge.WritePartParams{Doc: "decks/q4", Part: id, Text: text, BaseVersion: base, Kind: bridge.PartVersionKindNudge}); err != nil {
			t.Fatal(err)
		}
	}
	write(scope, "section-1.md", "# Draft", 1)
	write(real, "section-1.md", "# Applied", 1)
	write(real, "section-2.md", "# Latest", 1)
	for _, tt := range []struct {
		part, text string
		version    int
	}{{"section-1.md", "# Draft", 2}, {"section-2.md", "# Latest", 2}} {
		part, err := s.Get(ctx, scope, bridge.GetPartParams{Doc: "decks/q4", Part: tt.part})
		if err != nil || part.Text != tt.text || part.Version != tt.version {
			t.Fatalf("part=%+v error=%v", part, err)
		}
	}
	versions, err := s.Versions(ctx, scope, bridge.ListPartVersionsParams{Doc: "decks/q4", Part: "section-1.md"})
	if err != nil || len(versions.Versions) != 2 {
		t.Fatalf("versions=%+v error=%v", versions, err)
	}
	restored, err := s.Restore(ctx, scope, bridge.RestorePartParams{Doc: "decks/q4", Part: "section-1.md", Version: 1})
	if err != nil || restored.Version != 3 || restored.Text != "# Team\nJan" {
		t.Fatalf("restore=%+v error=%v", restored, err)
	}
}

func TestDraftShareRefused(t *testing.T) {
	s, real, _ := setup(t)
	ctx := context.Background()
	textDoc(t, s, real, "decks/q4")
	share, err := s.SetShare(ctx, real, bridge.SetShareParams{Doc: "decks/q4", On: true})
	if err != nil {
		t.Fatal(err)
	}
	scope := draftScope(real, "mona")
	_, setErr := s.SetShare(ctx, scope, bridge.SetShareParams{Doc: "decks/q4", On: false})
	_, openErr := s.OpenShare(ctx, scope, bridge.OpenShareParams{Slug: share.Slug})
	for _, err := range []error{setErr, openErr} {
		requireCode(t, err, bridge.CodeConflict)
		if err.(*bridge.Error).Message != "Share works on the applied app" {
			t.Fatalf("message=%v", err)
		}
	}
	doc, err := s.GetDoc(ctx, scope, bridge.GetDocParams{Doc: "decks/q4"})
	if err != nil || doc.Share != nil {
		t.Fatalf("draft share=%+v error=%v", doc.Share, err)
	}
}

func TestDraftMemberAndSymlinksCannotEscape(t *testing.T) {
	for _, member := range []string{"", ".", "..", "../other", "mona/other", "mona\\other"} {
		t.Run(member, func(t *testing.T) {
			s, real, _ := setup(t)
			_, err := s.ListDocs(context.Background(), draftScope(real, member), bridge.ListDocsParams{})
			requireCode(t, err, bridge.CodeInvalid)
		})
	}
	s, real, _ := setup(t)
	textDoc(t, s, real, "decks/q4")
	dir := filepath.Join(s.cfg.Root, real.App, "drafts")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(dir, "mona")); err != nil {
		t.Fatal(err)
	}
	_, err := s.ListDocs(context.Background(), draftScope(real, "mona"), bridge.ListDocsParams{})
	requireCode(t, err, bridge.CodeInvalid)
}

func TestDraftGridFormulasStayInOverlay(t *testing.T) {
	s, real, _ := setup(t)
	ctx := context.Background()
	_, err := s.CreateDoc(ctx, real, bridge.CreateDocParams{Doc: "budget", Surface: bridge.SurfaceGrid, Parts: []bridge.CreateDocPart{{Part: "sheets/q4.csv", Text: "Cost,Total\n1,2"}}})
	if err != nil {
		t.Fatal(err)
	}
	before := appliedFiles(t, s, real.App)
	scope := draftScope(real, "mona")
	extra := &bridge.PartExtra{Formulas: map[string]string{"B2": "=A2*2"}}
	_, err = s.Write(ctx, scope, bridge.WritePartParams{Doc: "budget", Part: "sheets/q4.csv", Text: "Cost,Total\n3,6", Extra: extra, BaseVersion: 1, Kind: bridge.PartVersionKindNudge})
	if err != nil {
		t.Fatal(err)
	}
	part, err := s.Get(ctx, scope, bridge.GetPartParams{Doc: "budget", Part: "sheets/q4.csv"})
	if err != nil || !reflect.DeepEqual(part.Extra, extra) {
		t.Fatalf("part=%+v error=%v", part, err)
	}
	if got := appliedFiles(t, s, real.App); !reflect.DeepEqual(before, got) {
		t.Fatal("draft grid changed applied files")
	}
}

func TestDraftCreatesOnlyInsideMemberFolder(t *testing.T) {
	s, real, _ := setup(t)
	scope := draftScope(real, "mona")
	textDoc(t, s, scope, "decks/new")
	appDir := filepath.Join(s.cfg.Root, real.App)
	err := filepath.WalkDir(appDir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		relative, err := filepath.Rel(filepath.Join(appDir, "drafts", "mona"), path)
		if err != nil {
			return err
		}
		if relative == ".." || strings.HasPrefix(relative, "../") {
			t.Fatalf("draft created file outside member folder: %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.open(context.Background(), scope)
	if err != nil {
		t.Fatal(err)
	}
	defer c.close()
	var tempStore int
	if err := c.tx.QueryRow("PRAGMA temp_store").Scan(&tempStore); err != nil || tempStore != 2 {
		t.Fatalf("temp_store=%d error=%v", tempStore, err)
	}
}

func TestDraftDiscardNeedsOnlyMemberFolder(t *testing.T) {
	s, real, _ := setup(t)
	ctx := context.Background()
	textDoc(t, s, real, "decks/q4")
	scope := draftScope(real, "mona")
	_, err := s.Write(ctx, scope, bridge.WritePartParams{Doc: "decks/q4", Part: "section-1.md", Text: "# Draft", BaseVersion: 1, Kind: bridge.PartVersionKindNudge})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(s.cfg.Root, real.App, "drafts", "mona")); err != nil {
		t.Fatal(err)
	}
	part, err := s.Get(ctx, scope, bridge.GetPartParams{Doc: "decks/q4", Part: "section-1.md"})
	if err != nil || part.Text != "# Team\nJan" || part.Version != 1 {
		t.Fatalf("after discard=%+v error=%v", part, err)
	}
}
