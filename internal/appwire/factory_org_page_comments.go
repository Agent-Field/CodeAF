package appwire

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
)

// ── COMMENTS ON A PASSAGE OF AN ORGANIZATION'S PAGE (Workstreams v5) ──────
//
// A thread hangs on the words someone selected on a workstream page. The desk
// sends a Personal workstream's threads through its own Tauri commands
// (workstream_page_comment*) in exactly these shapes, so its screens never ask
// where a thread lives.
//
//	factory.org_workstream_page_comments         OrgPageCommentsParams       -> OrgPageComments      GET  /v1/workstreams/{id}/pages/{pid}/comments[?resolved=1]
//	factory.org_workstream_page_comment          OrgPageCommentParams        -> OrgPageCommentThread POST /v1/workstreams/{id}/pages/{pid}/comments
//	factory.org_workstream_page_comment_reply    OrgPageCommentReplyParams   -> OrgPageCommentThread POST /v1/workstreams/{id}/comments/{cid}/reply
//	factory.org_workstream_page_comment_resolve  OrgPageCommentResolveParams -> OrgPageCommentThread POST /v1/workstreams/{id}/comments/{cid}/resolve
//	factory.org_workstream_page_comment_seen     OrgPageCommentSeenParams    -> OrgPageCommentThread POST /v1/workstreams/{id}/comments/{cid}/seen
//	factory.org_workstream_page_comments_for_you OrgTasksParams              -> OrgPageComments      GET  /v1/workstreams/{id}/comments/for_you
//
// Every param names the workstream as `ws`, a page as `page` and a thread as
// `thread`. Anyone who can see the workstream reads its threads; anyone who
// can edit it comments, replies and resolves; seen is each person's own. The
// server keeps a thread's anchor (block, block_text, offset, quote) EXACTLY
// AS POSTED and never says whether it is still anchored: an organization's
// page is a live document only the desk reads, so the desk finds the words
// again. A change is relayed as factory.org_workstream_changed with parts
// "page" and "files".

// FeatureOrgWorkstreamPageComments is the hello word for the methods above.
const FeatureOrgWorkstreamPageComments = "org_workstream_page_comments"

// OrgPageCommentThread is the server's thread, passed through whole: the
// desk's pageCommentsModel.ts types it.
type OrgPageCommentThread = json.RawMessage

// OrgPageComments is a list of threads, oldest first.
type OrgPageComments struct {
	Threads []json.RawMessage `json:"threads"`
}

// OrgPageCommentsParams names one page; Resolved lists resolved threads too.
type OrgPageCommentsParams struct {
	OrgConn
	WS       string `json:"ws"`
	Page     string `json:"page"`
	Resolved bool   `json:"resolved,omitempty"`
}

// OrgPageCommentParams starts a thread on the words a person selected: the
// block's path, that block's whole text, the code points before the words,
// the words, and what they say about them.
type OrgPageCommentParams struct {
	OrgConn
	WS        string   `json:"ws"`
	Page      string   `json:"page"`
	Block     []uint32 `json:"block"`
	BlockText string   `json:"block_text"`
	Offset    uint64   `json:"offset"`
	Quote     string   `json:"quote"`
	Text      string   `json:"text"`
}

// OrgPageCommentReplyParams says something more in a thread.
type OrgPageCommentReplyParams struct {
	OrgConn
	WS     string `json:"ws"`
	Thread string `json:"thread"`
	Text   string `json:"text"`
}

// OrgPageCommentResolveParams resolves a thread, or with Reopen opens it again.
type OrgPageCommentResolveParams struct {
	OrgConn
	WS     string `json:"ws"`
	Thread string `json:"thread"`
	Reopen bool   `json:"reopen,omitempty"`
}

// OrgPageCommentSeenParams says the caller read a thread.
type OrgPageCommentSeenParams struct {
	OrgConn
	WS     string `json:"ws"`
	Thread string `json:"thread"`
}

// The bounds the server keeps a thread within, checked here so a call that
// cannot be kept is refused before it is sent.
const (
	orgCommentQuoteMost = 4000
	orgCommentBlockMost = 256 << 10
	orgCommentPathMost  = 32
)

func orgPageCommentsPath(ws, page string) (string, error) {
	path, err := orgPagePath(ws, page)
	if err != nil {
		return "", err
	}
	return path + "/comments", nil
}

func orgCommentThreadPath(ws, thread string) (string, error) {
	path, err := orgWorkstreamPath(ws)
	if err != nil {
		return "", err
	}
	id, err := orgSegment("thread", thread)
	if err != nil {
		return "", err
	}
	return path + "/comments/" + url.PathEscape(id), nil
}

// orgCommentWords refuses a comment with no words, or more than one may hold.
func orgCommentWords(text string) (string, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", Errorf(CodeBadParams, "a comment needs words")
	}
	if len(text) > orgTaskMost {
		return "", Errorf(CodeBadParams, "a comment is at most 16384 bytes")
	}
	return text, nil
}

func withFactoryOrgPageCommentsFeature(hello Hello, e Engine) Hello {
	if _, ok := e.(Factory); !ok {
		return hello
	}
	if _, ok := e.(FactoryOrg); ok {
		hello = withFeature(hello, FeatureOrgWorkstreamPageComments)
	}
	return hello
}

func (c *OrgPageComments) fill() { c.Threads = nonNil(c.Threads) }

func init() {
	threads := func(c *OrgPageComments) { c.fill() }
	methods["factory.org_workstream_page_comments"] = filled(orgMethod[OrgPageCommentsParams, OrgPageComments]("factory.org_workstream_page_comments", func(p OrgPageCommentsParams) (OrgRequest, error) {
		path, err := orgPageCommentsPath(p.WS, p.Page)
		if p.Resolved {
			path += "?resolved=1"
		}
		return OrgRequest{Method: http.MethodGet, Path: path}, err
	}), threads)
	methods["factory.org_workstream_page_comment"] = orgMethod[OrgPageCommentParams, OrgPageCommentThread]("factory.org_workstream_page_comment", func(p OrgPageCommentParams) (OrgRequest, error) {
		path, err := orgPageCommentsPath(p.WS, p.Page)
		if err != nil {
			return OrgRequest{}, err
		}
		text, err := orgCommentWords(p.Text)
		if err != nil {
			return OrgRequest{}, err
		}
		if strings.TrimSpace(p.Quote) == "" || len(p.Quote) > orgCommentQuoteMost {
			return OrgRequest{}, Errorf(CodeBadParams, "a comment hangs on 1–4000 bytes of the page's words")
		}
		if len(p.Block) == 0 || len(p.Block) > orgCommentPathMost || len(p.BlockText) > orgCommentBlockMost {
			return OrgRequest{}, Errorf(CodeBadParams, "block is the path of the paragraph, heading or code block the words are in")
		}
		return OrgRequest{Method: http.MethodPost, Path: path, Body: struct {
			Block     []uint32 `json:"block"`
			BlockText string   `json:"block_text"`
			Offset    uint64   `json:"offset"`
			Quote     string   `json:"quote"`
			Text      string   `json:"text"`
		}{p.Block, p.BlockText, p.Offset, p.Quote, text}}, nil
	})
	methods["factory.org_workstream_page_comment_reply"] = orgMethod[OrgPageCommentReplyParams, OrgPageCommentThread]("factory.org_workstream_page_comment_reply", func(p OrgPageCommentReplyParams) (OrgRequest, error) {
		path, err := orgCommentThreadPath(p.WS, p.Thread)
		if err != nil {
			return OrgRequest{}, err
		}
		text, err := orgCommentWords(p.Text)
		if err != nil {
			return OrgRequest{}, err
		}
		return OrgRequest{Method: http.MethodPost, Path: path + "/reply", Body: map[string]string{"text": text}}, nil
	})
	methods["factory.org_workstream_page_comment_resolve"] = orgMethod[OrgPageCommentResolveParams, OrgPageCommentThread]("factory.org_workstream_page_comment_resolve", func(p OrgPageCommentResolveParams) (OrgRequest, error) {
		path, err := orgCommentThreadPath(p.WS, p.Thread)
		if err != nil {
			return OrgRequest{}, err
		}
		return OrgRequest{Method: http.MethodPost, Path: path + "/resolve", Body: map[string]bool{"reopen": p.Reopen}}, nil
	})
	methods["factory.org_workstream_page_comment_seen"] = orgMethod[OrgPageCommentSeenParams, OrgPageCommentThread]("factory.org_workstream_page_comment_seen", func(p OrgPageCommentSeenParams) (OrgRequest, error) {
		path, err := orgCommentThreadPath(p.WS, p.Thread)
		if err != nil {
			return OrgRequest{}, err
		}
		return OrgRequest{Method: http.MethodPost, Path: path + "/seen", Body: struct{}{}}, nil
	})
	methods["factory.org_workstream_page_comments_for_you"] = filled(orgMethod[OrgTasksParams, OrgPageComments]("factory.org_workstream_page_comments_for_you", func(p OrgTasksParams) (OrgRequest, error) {
		path, err := orgWorkstreamPath(p.WS)
		return OrgRequest{Method: http.MethodGet, Path: path + "/comments/for_you"}, err
	}), threads)
}
