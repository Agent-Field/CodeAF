package serve

import (
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

func fileNamed(t *testing.T, files []api.WorkstreamFile, name string) api.WorkstreamFile {
	t.Helper()
	for _, f := range files {
		if f.Name == name {
			return f
		}
	}
	t.Fatalf("no file %q in %+v", name, files)
	return api.WorkstreamFile{}
}

// FILES, BETWEEN TWO PEOPLE: santosh makes a workstream with a page and two
// sources the way an older client did, shares it with abir for edit, and the
// first read folds them in as files beside a Decisions document. abir makes a
// folder, a document and a link; santosh moves, renames and deletes; the
// folder's delete takes its files and its restore brings them back. mia, who
// cannot see the workstream, is told it does not exist.
func TestTwoPeopleShareAWorkstreamsFiles(t *testing.T) {
	o := newWSOrg(t)
	ws := o.create(t, "santosh", api.WorkstreamNew{
		Name: "Pantry",
		Pages: []factory.WorkstreamPage{
			{Title: "Brief", Kind: "page", BodyMD: "Recipes that have no photo"},
		},
		Sources: []factory.WorkstreamSource{
			{ID: "src_1", Kind: "url", Ref: "https://example.com/competitors", Title: "How competitors plan meals"},
			{ID: "src_2", Kind: "repo", Ref: "pantry-app/pantry", Title: "pantry"},
		},
		Decisions: []factory.WorkstreamDecision{{Text: "Ship weekly", State: "kept"}},
	})
	edit := []factory.Share{{Kind: "person", ID: "abir", Role: "edit"}}
	o.do(t, "santosh", "PUT", "/v1/workstreams/"+ws.ID+"/shares", -1, api.WorkstreamSharesPut{Shares: &edit, Rev: &ws.Rev}, 200, nil)
	path := "/v1/workstreams/" + ws.ID + "/files"

	var list api.WorkstreamFiles
	read := func(who, query string) {
		t.Helper()
		list = api.WorkstreamFiles{}
		o.do(t, who, "GET", path+query, -1, nil, 200, &list)
	}
	read("abir", "")
	brief := fileNamed(t, list.Files, "Brief")
	if brief.Kind != factory.FileDocument || brief.PageID == "" || brief.MadeBy.ID != "santosh" {
		t.Fatalf("the page folds in as a document = %+v", brief)
	}
	if slices.ContainsFunc(list.Files, func(f factory.WorkstreamFile) bool { return f.Name == "Decisions" }) {
		t.Fatalf("the fold makes no Decisions document; the decisions are the kept list: %+v", list.Files)
	}
	if l := fileNamed(t, list.Files, "How competitors plan meals"); l.Kind != factory.FileLink || l.URL != "https://example.com/competitors" {
		t.Fatalf("url source = %+v", l)
	}
	if r := fileNamed(t, list.Files, "pantry"); r.Kind != factory.FileRepo || r.Repo != "pantry-app/pantry" {
		t.Fatalf("repo source = %+v", r)
	}
	var got api.Workstream
	o.do(t, "santosh", "GET", "/v1/workstreams/"+ws.ID, -1, nil, 200, &got)
	if slices.ContainsFunc(got.Pages, func(p factory.WorkstreamPage) bool { return p.Kind == "decisions" }) {
		t.Fatalf("no decisions page is added behind it: %+v", got.Pages)
	}
	if len(got.Decisions) != 1 || got.Decisions[0].Text != "Ship weekly" {
		t.Fatalf("the kept decision stays in the list: %+v", got.Decisions)
	}
	n := len(list.Files)
	read("santosh", "")
	if len(list.Files) != n {
		t.Fatalf("a second read folds nothing again: %d then %d", n, len(list.Files))
	}

	var folder, doc, link api.WorkstreamFile
	o.do(t, "abir", "POST", path, -1, api.FileNew{Kind: "folder", Name: "Research"}, 201, &folder)
	o.do(t, "abir", "POST", path, -1, api.FileNew{Kind: "document", Name: "Interview notes", Folder: folder.ID, BodyMD: "Dana said"}, 201, &doc)
	o.do(t, "abir", "POST", path, -1, api.FileNew{Kind: "link", URL: "https://example.com/survey", Name: "Survey results",
		MadeBy: &factory.FileMaker{Kind: "afi", Name: "Afi"}}, 201, &link)
	// A document's first words are its first version, so History and Show
	// versions are never empty for a document someone wrote (L3 F-21).
	var vs api.PageVersions
	o.do(t, "santosh", "GET", "/v1/workstreams/"+ws.ID+"/pages/"+doc.PageID+"/versions", -1, nil, 200, &vs)
	if len(vs.Versions) != 1 || vs.Versions[0].Bytes != len("Dana said") || vs.Versions[0].By != "abir" || vs.LiveVersion != vs.Versions[0].Version {
		t.Fatalf("a new document's versions = %+v", vs)
	}
	if doc.PageID == "" || doc.Folder != folder.ID || doc.MadeBy.ID != "abir" {
		t.Fatalf("document = %+v", doc)
	}
	if link.MadeBy.Kind != "afi" || link.MadeBy.For != "abir" || !link.AgentMayEdit() {
		t.Fatalf("an agent's file = %+v", link.MadeBy)
	}
	o.do(t, "abir", "POST", path, -1, api.FileNew{Kind: "document", Name: "x", Folder: link.ID}, 422, nil)
	o.do(t, "abir", "POST", path, -1, api.FileNew{Kind: "blob", Name: "x"}, 422, nil)

	// Search finds a document by what it says.
	read("santosh", "?q=no+photo")
	if len(list.Files) != 1 || list.Files[0].Name != "Brief" {
		t.Fatalf("search by contents = %+v", list.Files)
	}

	// santosh moves the link into Research; a folder cannot move into itself.
	var moved api.WorkstreamFiles
	o.do(t, "santosh", "POST", path+"/move", -1, api.FilesMove{Files: []string{link.ID}, Folder: folder.ID}, 200, &moved)
	if moved.Files[0].Folder != folder.ID {
		t.Fatalf("moved = %+v", moved.Files)
	}
	o.do(t, "santosh", "POST", path+"/move", -1, api.FilesMove{Files: []string{folder.ID}, Folder: folder.ID}, 422, nil)

	// A rename is guarded by the file's rev, and renames a document's page.
	name := "Interview notes, Dana"
	o.do(t, "santosh", "PATCH", path+"/"+doc.ID, doc.Rev+5, api.FilePatch{Name: &name}, 412, nil)
	var renamed api.WorkstreamFile
	o.do(t, "santosh", "PATCH", path+"/"+doc.ID, doc.Rev, api.FilePatch{Name: &name, Edited: "person"}, 200, &renamed)
	if renamed.Name != name || !renamed.PersonEdited {
		t.Fatalf("renamed = %+v", renamed)
	}
	o.do(t, "santosh", "GET", "/v1/workstreams/"+ws.ID, -1, nil, 200, &got)
	if !slices.ContainsFunc(got.Pages, func(p factory.WorkstreamPage) bool { return p.ID == doc.PageID && p.Title == name }) {
		t.Fatalf("the page took the new name: %+v", got.Pages)
	}

	// The folder's delete takes its two files; its restore brings them back.
	o.do(t, "abir", "DELETE", path+"/"+folder.ID, -1, nil, 200, nil)
	read("santosh", "?deleted=1")
	if slices.ContainsFunc(list.Files, func(f api.WorkstreamFile) bool { return f.Folder == folder.ID || f.ID == folder.ID }) || len(list.Deleted) != 3 {
		t.Fatalf("after delete: live %+v deleted %+v", list.Files, list.Deleted)
	}
	o.do(t, "santosh", "POST", path+"/"+folder.ID+"/restore", -1, nil, 200, nil)
	read("santosh", "?deleted=1")
	if len(list.Deleted) != 0 || !slices.ContainsFunc(list.Files, func(f api.WorkstreamFile) bool { return f.ID == link.ID && f.Folder == folder.ID }) {
		t.Fatalf("after restore: live %+v deleted %+v", list.Files, list.Deleted)
	}
	o.do(t, "santosh", "POST", path+"/"+folder.ID+"/restore", -1, nil, 422, nil)

	// mia cannot see it: not found, for reading and for writing.
	o.do(t, "mia", "GET", path, -1, nil, 404, nil)
	o.do(t, "mia", "POST", path, -1, api.FileNew{Kind: "folder", Name: "Mine"}, 404, nil)
}

// A VIEWER READS, NEVER WRITES; AN UPLOAD COMES BACK AS IT WENT IN; A
// SHORTCUT SHOWS A FILE ELSEWHERE WITHOUT COPYING IT.
func TestFilesUploadsShortcutsAndViewers(t *testing.T) {
	o := newWSOrg(t)
	pantry := o.create(t, "santosh", api.WorkstreamNew{Name: "Pantry"})
	planner := o.create(t, "santosh", api.WorkstreamNew{Name: "Meal planner"})
	view := []factory.Share{{Kind: "person", ID: "abir", Role: "view"}}
	o.do(t, "santosh", "PUT", "/v1/workstreams/"+pantry.ID+"/shares", -1, api.WorkstreamSharesPut{Shares: &view, Rev: &pantry.Rev}, 200, nil)
	path := "/v1/workstreams/" + pantry.ID + "/files"

	var up api.WorkstreamFile
	o.do(t, "santosh", "POST", path, -1, api.FileNew{Kind: "upload", Name: "menu.pdf", Mime: "application/pdf", Data: []byte("%PDF-1.4 hello")}, 201, &up)
	if up.Upload == nil || up.Upload.Size != 14 || len(up.Upload.Blob) != 64 {
		t.Fatalf("upload = %+v", up.Upload)
	}
	r := httptest.NewRequest("GET", path+"/"+up.ID+"/content", nil)
	r.RemoteAddr = "192.0.2.1:1234"
	r.Header.Set("Authorization", "Bearer "+o.tokens["abir"])
	w := httptest.NewRecorder()
	o.h.ServeHTTP(w, r)
	if w.Code != 200 || w.Body.String() != "%PDF-1.4 hello" || w.Header().Get("Content-Type") != "application/pdf" {
		t.Fatalf("content = %d %q %q", w.Code, w.Body, w.Header().Get("Content-Type"))
	}
	o.do(t, "abir", "POST", path, -1, api.FileNew{Kind: "folder", Name: "Mine"}, 403, nil)
	o.do(t, "abir", "DELETE", path+"/"+up.ID, -1, nil, 403, nil)

	var short api.WorkstreamFile
	o.do(t, "santosh", "POST", "/v1/workstreams/"+planner.ID+"/files", -1,
		api.FileNew{Kind: "shortcut", Target: &factory.FileTarget{WS: pantry.ID, File: up.ID}}, 201, &short)
	if short.Name != "menu.pdf" || short.Target.File != up.ID || short.Upload != nil {
		t.Fatalf("shortcut = %+v", short)
	}
	o.do(t, "abir", "POST", "/v1/workstreams/"+planner.ID+"/files", -1,
		api.FileNew{Kind: "shortcut", Target: &factory.FileTarget{WS: pantry.ID, File: up.ID}}, 404, nil)
}
