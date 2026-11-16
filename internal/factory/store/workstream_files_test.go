package store

import (
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// A WORKSTREAM'S PARENT IS READ FROM THE KEPT DOCUMENT, so a build that does
// not decode the field still answers it; and an upload's bytes are one copy
// let go only when no file names them.
func TestFilesParentAndBlobs(t *testing.T) {
	st, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.EditWorkstreams(func(all factory.OrgWorkstreams) ([]string, error) {
		all["ws_kid"] = factory.OrgWorkstream{ID: "ws_kid", Name: "Meal planner"}
		return []string{"ws_kid"}, nil
	}); err != nil {
		t.Fatal(err)
	}
	db, err := st.wsDB(true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE workstreams SET doc = json_set(doc, '$.parent', 'ws_top') WHERE id = 'ws_kid'`); err != nil {
		t.Fatal(err)
	}
	if p, err := st.WorkstreamParent("ws_kid"); err != nil || p != "ws_top" {
		t.Fatalf("parent = %q, %v", p, err)
	}
	if p, err := st.WorkstreamParent("ws_none"); err != nil || p != "" {
		t.Fatalf("no workstream's parent = %q, %v", p, err)
	}

	sha, err := st.PutBlob([]byte("bytes"))
	if err != nil {
		t.Fatal(err)
	}
	if again, _ := st.PutBlob([]byte("bytes")); again != sha {
		t.Fatalf("the same bytes twice = %s and %s", sha, again)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := st.EditWorkstreamFiles("ws_kid", now, func(files map[string]factory.WorkstreamFile) ([]string, bool, error) {
		files["fl_1"] = factory.WorkstreamFile{Kind: factory.FileUpload, Name: "a", Upload: &factory.UploadBlob{Size: 5, Blob: sha}}
		return []string{"fl_1"}, true, nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.DropUnusedBlob(sha); err != nil {
		t.Fatal(err)
	}
	if data, err := st.Blob(sha); err != nil || string(data) != "bytes" {
		t.Fatalf("a named blob stays: %q %v", data, err)
	}
	if seeded, _ := st.FilesSeeded("ws_kid"); !seeded {
		t.Fatal("the seed mark was not kept")
	}
	if _, err := st.EditWorkstreamFiles("ws_kid", now, func(files map[string]factory.WorkstreamFile) ([]string, bool, error) {
		delete(files, "fl_1")
		return []string{"fl_1"}, false, nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.DropUnusedBlob(sha); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Blob(sha); err != ErrFileGone {
		t.Fatalf("an unnamed blob is let go: %v", err)
	}
}
