package serve

import (
	"encoding/json"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/live"
)

// commentOrg is santosh's workstream shared with abir at role, with one page.
func commentOrg(t *testing.T, role string) (*wsOrg, string, string) {
	t.Helper()
	o, path := taskOrg(t, role)
	var ws api.Workstream
	o.do(t, "santosh", "POST", path+"/pages", -1, api.PageNew{Title: "Launch plan"}, 201, &ws)
	return o, path, ws.Pages[len(ws.Pages)-1].ID
}

var launchPassage = api.PageCommentNew{Block: []uint32{2}, BlockText: "We ship the café build on Friday.", Offset: 12, Quote: "café build", Text: "Which build?"}

func (o *wsOrg) threads(t *testing.T, who, path, page, query string) []api.PageCommentThread {
	t.Helper()
	var out api.PageComments
	o.do(t, who, "GET", path+"/pages/"+page+"/comments"+query, -1, nil, 200, &out)
	return out.Threads
}

func (o *wsOrg) forYou(t *testing.T, who, path string) []api.PageCommentThread {
	t.Helper()
	var out api.PageComments
	o.do(t, who, "GET", path+"/comments/for_you", -1, nil, 200, &out)
	return out.Threads
}

// VIEW READS, EDIT COMMENTS, and the anchor comes back exactly as it was
// posted: the server never checks it against the page.
func TestViewReadsPassageCommentsAndEditWritesThem(t *testing.T) {
	o, path, page := commentOrg(t, "view")
	var made api.PageCommentThread
	o.do(t, "santosh", "POST", path+"/pages/"+page+"/comments", -1, launchPassage, 201, &made)
	if made.ID == "" || made.WS == "" || made.PageID != page || made.Quote != "café build" || made.Offset != 12 || len(made.Block) != 1 || made.Block[0] != 2 ||
		made.BlockText != launchPassage.BlockText || len(made.Comments) != 1 || made.Comments[0].By.ID != "santosh" || made.Unread {
		t.Fatalf("made %+v", made)
	}
	if got := o.threads(t, "abir", path, page, ""); len(got) != 1 || got[0].ID != made.ID || !got[0].Unread {
		t.Fatalf("abir reads %+v", got)
	}
	o.do(t, "abir", "POST", path+"/pages/"+page+"/comments", -1, launchPassage, 403, nil)
	o.do(t, "abir", "POST", path+"/comments/"+made.ID+"/reply", -1, api.PageCommentReply{Text: "the second"}, 403, nil)
	o.do(t, "abir", "POST", path+"/comments/"+made.ID+"/resolve", -1, api.PageCommentResolve{}, 403, nil)
	o.do(t, "mia", "GET", path+"/pages/"+page+"/comments", -1, nil, 404, nil)

	// Seen is abir's own, and view is enough for it.
	var seen api.PageCommentThread
	o.do(t, "abir", "POST", path+"/comments/"+made.ID+"/seen", -1, nil, 200, &seen)
	if seen.Unread || len(o.forYou(t, "abir", path)) != 0 {
		t.Fatalf("seen %+v", seen)
	}
}

// A post that contradicts itself is refused; the page itself is never asked.
func TestAPassageCommentIsCheckedOnlyAgainstItself(t *testing.T) {
	o, path, page := commentOrg(t, "edit")
	bad := launchPassage
	bad.Offset = 13
	o.do(t, "santosh", "POST", path+"/pages/"+page+"/comments", -1, bad, 422, nil)
	bad = launchPassage
	bad.Text = "  "
	o.do(t, "santosh", "POST", path+"/pages/"+page+"/comments", -1, bad, 422, nil)
	bad = launchPassage
	bad.Block = nil
	o.do(t, "santosh", "POST", path+"/pages/"+page+"/comments", -1, bad, 422, nil)
	o.do(t, "santosh", "POST", path+"/pages/pg_nope/comments", -1, launchPassage, 404, nil)
	// The page's mirror is empty, and the words are kept anyway.
	o.do(t, "santosh", "POST", path+"/pages/"+page+"/comments", -1, launchPassage, 201, nil)
}

// Resolving closes a thread for everyone, a reply opens it again, and each
// person's unread is their own.
func TestAReplyReopensAThreadAndUnreadIsPerPerson(t *testing.T) {
	o, path, page := commentOrg(t, "edit")
	var made, got api.PageCommentThread
	o.do(t, "santosh", "POST", path+"/pages/"+page+"/comments", -1, launchPassage, 201, &made)
	if len(o.forYou(t, "santosh", path)) != 0 || len(o.forYou(t, "abir", path)) != 1 {
		t.Fatal("a comment is for everyone but the person who wrote it")
	}
	o.do(t, "abir", "POST", path+"/comments/"+made.ID+"/resolve", -1, api.PageCommentResolve{}, 200, &got)
	if got.Resolved == nil || got.Resolved.By != "abir" {
		t.Fatalf("resolved %+v", got)
	}
	if len(o.threads(t, "santosh", path, page, "")) != 0 || len(o.threads(t, "santosh", path, page, "?resolved=1")) != 1 || len(o.forYou(t, "abir", path)) != 0 {
		t.Fatal("a resolved thread is listed only with resolved=1, and is for nobody")
	}
	var replied api.PageCommentThread
	o.do(t, "abir", "POST", path+"/comments/"+made.ID+"/reply", -1, api.PageCommentReply{Text: "Friday's, the second one"}, 201, &replied)
	if got := replied; got.Resolved != nil || len(got.Comments) != 2 || got.Comments[1].By.ID != "abir" || got.Unread {
		t.Fatalf("replied %+v", got)
	}
	if mine := o.forYou(t, "santosh", path); len(mine) != 1 || mine[0].ID != made.ID {
		t.Fatalf("santosh's comments for you: %+v", mine)
	}
	if len(o.forYou(t, "abir", path)) != 0 {
		t.Fatal("abir's own reply is read the moment it is written")
	}
	o.do(t, "santosh", "POST", path+"/comments/"+made.ID+"/resolve", -1, api.PageCommentResolve{Reopen: true}, 200, nil)
	o.do(t, "santosh", "POST", path+"/comments/pc_nope/reply", -1, api.PageCommentReply{Text: "hm"}, 404, nil)

	// A document's facts count its open threads.
	var files api.WorkstreamFiles
	o.do(t, "santosh", "GET", path+"/files", -1, nil, 200, &files)
	counted := false
	for _, f := range files.Files {
		if f.PageID == page {
			counted = f.OpenComments == 1
		}
	}
	if !counted {
		t.Fatalf("the document does not count its open thread: %+v", files.Files)
	}
}

// Every change is told as the page's and the files' change.
func TestAPassageCommentIsToldAsThePagesAndTheFilesChange(t *testing.T) {
	o, path, page := commentOrg(t, "edit")
	o.do(t, "abir", "POST", path+"/pages/"+page+"/comments", -1, launchPassage, 201, nil)
	events, _, err := live.Read(o.st.Root())
	if err != nil {
		t.Fatal(err)
	}
	parts := map[string]string{}
	for _, e := range events[len(events)-2:] {
		var got api.WorkstreamEvent
		if err := json.Unmarshal(e.Data, &got); err != nil {
			t.Fatal(err)
		}
		parts[got.Part] = got.PartID
	}
	if parts[api.PartPage] != page || len(parts) != 2 {
		t.Fatalf("told %+v", parts)
	}
	if _, ok := parts[api.PartFiles]; !ok {
		t.Fatalf("the files were not told: %+v", parts)
	}
}
