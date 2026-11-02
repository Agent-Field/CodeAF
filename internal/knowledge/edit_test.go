package knowledge_test

import (
	"errors"
	"github.com/Agent-Field/codeaf/internal/knowledgefixture"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/knowledge"
)

func editableVault(t *testing.T) *knowledge.Vault {
	t.Helper()
	v := knowledge.Open(t.TempDir())
	if err := v.StartBuild(); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(v.Directory(), "knowledge")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	fixture := `{"relations":[],"sources":{"s":{"id":"s","session":"chat","text":"Original generated knowledge","role":"user","ts":"2026-10-25T12:00:00Z"}},"claims":[{"id":"c","scope":"Project","text":"Original generated knowledge","kind":"fact","status":"active","evidence":[{"source":"s","quote":"Original"}]}],"notes":[{"id":"page","title":"Architecture","scope":"Project","folder":["Project"],"claims":["c"],"links":[{"id":"other","kind":"related","reason":"Related reading"}]},{"id":"other","title":"Other","scope":"Project","folder":["Project"],"claims":[],"sections":[{"heading":"Related","paragraphs":[{"text":"Architecture","claims":[],"evidence":[],"inline_links":[{"start":0,"end":12,"id":"page"}]}]}],"links":[{"id":"page","kind":"mention","reason":"Mentioned"}]}]}`
	if err := knowledgefixture.Write(filepath.Join(filepath.Dir(dir), "knowledge.sqlite"), []byte(fixture), 0600); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestHumanKnowledgeEditsSurviveReopenAndKeepProvenanceHonest(t *testing.T) {
	v := editableVault(t)
	page, err := v.Read("page")
	if err != nil {
		t.Fatal(err)
	}
	if page.Revision == "" || !strings.Contains(knowledge.PageMarkdown(page), "Original generated knowledge") {
		t.Fatal("page is not editable")
	}
	body := "## Human correction\n\nUse [[other|Other]] for revised architecture."
	if err := v.EditPage(page.ID, page.Revision, body); err != nil {
		t.Fatal(err)
	}
	updated, err := v.Read(page.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !updated.HumanEdited || knowledge.PageMarkdown(updated) != body || updated.Title != page.Title {
		t.Fatalf("manual page lost identity/body: %+v", updated)
	}
	if len(updated.Evidence) != 1 || len(updated.Sections[0].Paragraphs[0].Evidence) != 0 || len(updated.Sections[0].Paragraphs[0].Claims) != 0 {
		t.Fatal("human prose inherited generated evidence")
	}
	hits, err := v.Search("revised architecture", 10)
	if err != nil || len(hits) == 0 || hits[0].Page.ID != "page" {
		t.Fatalf("edited prose not searchable: %v %v", hits, err)
	}
	if err := v.EditPage(page.ID, page.Revision, "stale overwrite"); !errors.Is(err, knowledge.ErrPageChanged) {
		t.Fatalf("stale save accepted: %v", err)
	}
	if err := v.DeletePage(page.ID, page.Revision); !errors.Is(err, knowledge.ErrPageChanged) {
		t.Fatalf("stale delete accepted: %v", err)
	}
}

func TestDeletedKnowledgePageDisappearsFromRecallAndGraph(t *testing.T) {
	v := editableVault(t)
	page, err := v.Read("page")
	if err != nil {
		t.Fatal(err)
	}
	if err = v.DeletePage(page.ID, page.Revision); err != nil {
		t.Fatal(err)
	}
	if _, err = v.Read(page.ID); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("deleted page still readable: %v", err)
	}
	hits, err := v.Search("Original generated knowledge", 10)
	if err != nil || len(hits) != 0 {
		t.Fatalf("deleted page still searchable: %v %v", hits, err)
	}
	other, err := v.Read("other")
	if err != nil {
		t.Fatal(err)
	}
	if len(other.Links) != 0 || len(other.Sections[0].Paragraphs[0].InlineLinks) != 0 {
		t.Fatal("deleted destination still linked")
	}
	if _, err = os.Stat(filepath.Join(v.Directory(), "knowledge.sqlite")); err != nil {
		t.Fatal("deletion removed original generated evidence")
	}
}

func TestHumanKnowledgeClearsOldLinksAndKeepsCodeExamplesLiteral(t *testing.T) {
	v := editableVault(t)
	page, err := v.Read("page")
	if err != nil {
		t.Fatal(err)
	}
	body := "Show `[[other]]` as syntax.\n\n```md\n[[other]]\n```"
	if err = v.EditPage(page.ID, page.Revision, body); err != nil {
		t.Fatal(err)
	}
	updated, err := v.Read(page.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.Links) != 0 {
		t.Fatal("manual prose inherited removed generated links")
	}
	for _, section := range updated.Sections {
		for _, para := range section.Paragraphs {
			if len(para.InlineLinks) != 0 {
				t.Fatal("code examples became live graph links")
			}
		}
	}
	other, err := v.Read("other")
	if err != nil {
		t.Fatal(err)
	}
	if len(other.Backlinks) != 0 {
		t.Fatal("removed manual reference left a stale backlink")
	}
}
