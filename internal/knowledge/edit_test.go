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

func TestLibrarySettlementKeepsTheChosenAccountAsAHumanEdit(t *testing.T) {
	v := knowledge.Open(t.TempDir())
	fixture := `{"relations":[],"sources":{"a":{"id":"a","session":"chat-a","role":"user","text":"Use 7443","ts":"2026-10-25T12:00:00Z"},"b":{"id":"b","session":"chat-b","role":"user","text":"Use 8443","ts":"2026-10-26T12:00:00Z"}},"claims":[{"id":"old","text":"Use 7443","kind":"fact","status":"active","evidence":[{"source":"a","quote":"Use 7443"}]},{"id":"new","text":"Use 8443","kind":"fact","status":"active","evidence":[{"source":"b","quote":"Use 8443"}]}],"notes":[{"id":"relay","title":"Relay","kind":"subject","claims":["old","new"],"conflicts":[{"claims":["old","new"],"reason":"Port","state":"unresolved"}],"sections":[{"heading":"Port","paragraphs":[{"text":"Use 7443","claims":["old"]},{"text":"Use 8443","claims":["new"]}]}]}]}`
	if err := knowledgefixture.Write(filepath.Join(v.Directory(), "knowledge.sqlite"), []byte(fixture), 0600); err != nil {
		t.Fatal(err)
	}
	page, err := v.ReadDetail("relay")
	if err != nil {
		t.Fatal(err)
	}
	if err = v.SettlePage("relay", page.Revision, page.Conflicts[0].ID, "new", ""); err != nil {
		t.Fatal(err)
	}
	settled, err := v.ReadDetail("relay")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(knowledge.PageMarkdown(settled.Page), "7443") || !strings.Contains(knowledge.PageMarkdown(settled.Page), "8443") || len(settled.Resolutions) != 1 {
		t.Fatal("settlement did not retain the chosen words and replacement history")
	}
	if err = v.SettlePage("relay", page.Revision, page.Conflicts[0].ID, "old", ""); !errors.Is(err, knowledge.ErrPageChanged) {
		t.Fatalf("stale resolution accepted: %v", err)
	}
}

func TestLibraryEditorReceivesCanonicalWikiLinks(t *testing.T) {
	v := editableVault(t)
	page, err := v.ReadDetail("other")
	if err != nil {
		t.Fatal(err)
	}
	if page.EditableMarkdown != "## Related\n\n[[page|Architecture]]" {
		t.Fatalf("editable text lost its wiki reference: %q", page.EditableMarkdown)
	}
}

func TestLibrarySettlementLeavesOtherDisagreementsAvailable(t *testing.T) {
	v := knowledge.Open(t.TempDir())
	raw, err := os.ReadFile("testdata/library-conflict.json")
	if err != nil {
		t.Fatal(err)
	}
	fixture := strings.Replace(string(raw), `"conflicts":[`, `"conflicts":[{"claims":["old","new","third"],"reason":"Another question","state":"unresolved"},`, 1)
	fixture = strings.Replace(fixture, `"claims":[{"id":"old"`, `"claims":[{"id":"third","text":"Another account","kind":"fact","status":"active","evidence":[]},{"id":"old"`, 1)
	if err := knowledgefixture.Write(filepath.Join(v.Directory(), "knowledge.sqlite"), []byte(fixture), 0600); err != nil {
		t.Fatal(err)
	}
	page, err := v.ReadDetail("relay")
	if err != nil {
		t.Fatal(err)
	}
	var chosen string
	for _, c := range page.Conflicts {
		if c.Reason == "Port" {
			chosen = c.ID
		}
	}
	if err := v.SettlePage(page.ID, page.Revision, chosen, "new", ""); err != nil {
		t.Fatal(err)
	}
	updated, err := v.ReadDetail(page.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.Conflicts) != 1 || updated.Conflicts[0].Reason != "Another question" {
		t.Fatalf("unrelated disagreement lost: %+v", updated.Conflicts)
	}
	if err := v.SettlePage(updated.ID, updated.Revision, updated.Conflicts[0].ID, "third", ""); err != nil {
		t.Fatal(err)
	}
}

func TestLibraryChangesRetainReplacedClaimsAndTheirSource(t *testing.T) {
	v := knowledge.Open(t.TempDir())
	raw, err := os.ReadFile("testdata/library-conflict.json")
	if err != nil {
		t.Fatal(err)
	}
	fixture := strings.Replace(string(raw), `"relations":[]`, `"relations":[{"from":"new","to":"old","kind":"supersedes","reason":"Human correction"}]`, 1)
	if err := knowledgefixture.Write(filepath.Join(v.Directory(), "knowledge.sqlite"), []byte(fixture), 0600); err != nil {
		t.Fatal(err)
	}
	page, err := v.ReadDetail("relay")
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Changes) != 1 || page.Changes[0].Before != "old" || page.Changes[0].After != "new" || page.Sources["a"].Session != "chat-a" {
		t.Fatal("replacement history or its source was lost")
	}
}

func TestLibraryChoiceRetiresTheOtherAccountAcrossSharedReferences(t *testing.T) {
	v := knowledge.Open(t.TempDir())
	raw, err := os.ReadFile("testdata/library-conflict.json")
	if err != nil {
		t.Fatal(err)
	}
	fixture := strings.Replace(string(raw), `"notes":[`, `"notes":[{"id":"mirror","title":"Earlier notes","claims":["old","new"]},`, 1)
	if err := knowledgefixture.Write(filepath.Join(v.Directory(), "knowledge.sqlite"), []byte(fixture), 0600); err != nil {
		t.Fatal(err)
	}
	page, err := v.ReadDetail("relay")
	if err != nil {
		t.Fatal(err)
	}
	if err := v.SettlePage(page.ID, page.Revision, page.Conflicts[0].ID, "old", ""); err != nil {
		t.Fatal(err)
	}
	mirror, err := v.ReadDetail("mirror")
	if err != nil {
		t.Fatal(err)
	}
	for _, claim := range mirror.ClaimDetails {
		if claim.ID == "new" && claim.Status == "active" {
			t.Fatal("the rejected account can still answer recall through an older shared reference")
		}
	}
}

func TestLibraryChoiceHistoryKeepsTheReplacedSourceReachable(t *testing.T) {
	v := knowledge.Open(t.TempDir())
	raw, err := os.ReadFile("testdata/library-conflict.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := knowledgefixture.Write(filepath.Join(v.Directory(), "knowledge.sqlite"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	page, err := v.ReadDetail("relay")
	if err != nil {
		t.Fatal(err)
	}
	if err := v.SettlePage(page.ID, page.Revision, page.Conflicts[0].ID, "new", ""); err != nil {
		t.Fatal(err)
	}
	page, err = v.ReadDetail("relay")
	if err != nil {
		t.Fatal(err)
	}
	if page.Sources["a"].Session != "chat-a" {
		t.Fatal("the replaced account lost its source chat")
	}
}
