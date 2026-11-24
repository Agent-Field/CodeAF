package appwire

import (
	"encoding/json"
	"net/http"
	"net/url"
)

// ── AN ORGANIZATION'S LIBRARY (factory/serve org_library.go) ───────────────
//
// An organization keeps one Library on its server: pages its people added on
// purpose, which everyone in it reads and every chat the server runs can
// search. Nothing is added but by a person (Add to Library on a file, or a
// page they wrote); the person who added a page, or the operator, changes or
// removes it.
//
//	factory.org_library         OrgConn                -> {pages[]}   GET    /v1/library
//	factory.org_library_page    OrgLibraryPageParams   -> the page    GET    /v1/library/{page}
//	factory.org_library_add     OrgLibraryAddParams    -> the page    POST   /v1/library
//	factory.org_library_set     OrgLibrarySetParams    -> the page    PATCH  /v1/library/{page}
//	factory.org_library_delete  OrgLibraryPageParams   -> {}          DELETE /v1/library/{page}
//
// A page is {id, title, body? (one page only), by, by_name?, from_workstream?,
// from_name?, mine?, changeable?, created_at, updated_at}; where it came from
// is said only to a person who may see that workstream.
//
// An engine that serves them says "org_library" in hello.

// FeatureOrgLibrary is the hello word, exactly as the desk reads it.
const FeatureOrgLibrary = "org_library"

// OrgLibraryPageParams names one page.
type OrgLibraryPageParams struct {
	OrgConn
	Page string `json:"page"`
}

// OrgLibraryAddParams adds a page; with from_workstream and from_file, the
// words are that file's.
type OrgLibraryAddParams struct {
	OrgConn
	Title          string `json:"title"`
	Body           string `json:"body"`
	FromWorkstream string `json:"from_workstream,omitempty"`
	FromFile       string `json:"from_file,omitempty"`
}

// OrgLibrarySetParams changes what is sent.
type OrgLibrarySetParams struct {
	OrgConn
	Page  string  `json:"page"`
	Title *string `json:"title,omitempty"`
	Body  *string `json:"body,omitempty"`
}

func orgLibraryPath(page string) (string, error) {
	page, err := orgSegment("page", page)
	if err != nil {
		return "", err
	}
	return "/v1/library/" + url.PathEscape(page), nil
}

func withFactoryOrgLibraryFeature(hello Hello, e Engine) Hello {
	if _, ok := e.(Factory); !ok {
		return hello
	}
	if _, ok := e.(FactoryOrg); ok {
		hello = withFeature(hello, FeatureOrgLibrary)
	}
	return hello
}

func init() {
	methods["factory.org_library"] = orgMethod[OrgConn, json.RawMessage]("factory.org_library", func(OrgConn) (OrgRequest, error) {
		return OrgRequest{Method: http.MethodGet, Path: "/v1/library"}, nil
	})
	methods["factory.org_library_page"] = orgMethod[OrgLibraryPageParams, json.RawMessage]("factory.org_library_page", func(p OrgLibraryPageParams) (OrgRequest, error) {
		path, err := orgLibraryPath(p.Page)
		return OrgRequest{Method: http.MethodGet, Path: path}, err
	})
	methods["factory.org_library_add"] = orgMethod[OrgLibraryAddParams, json.RawMessage]("factory.org_library_add", func(p OrgLibraryAddParams) (OrgRequest, error) {
		body := map[string]string{"title": p.Title, "body": p.Body}
		if p.FromWorkstream != "" {
			body["from_workstream"] = p.FromWorkstream
		}
		if p.FromFile != "" {
			body["from_file"] = p.FromFile
		}
		return OrgRequest{Method: http.MethodPost, Path: "/v1/library", Body: body}, nil
	})
	methods["factory.org_library_set"] = orgMethod[OrgLibrarySetParams, json.RawMessage]("factory.org_library_set", func(p OrgLibrarySetParams) (OrgRequest, error) {
		path, err := orgLibraryPath(p.Page)
		if err != nil {
			return OrgRequest{}, err
		}
		body := map[string]*string{}
		if p.Title != nil {
			body["title"] = p.Title
		}
		if p.Body != nil {
			body["body"] = p.Body
		}
		if len(body) == 0 {
			return OrgRequest{}, Errorf(CodeBadParams, "send title or body")
		}
		return OrgRequest{Method: http.MethodPatch, Path: path, Body: body}, nil
	})
	methods["factory.org_library_delete"] = orgMethod[OrgLibraryPageParams, json.RawMessage]("factory.org_library_delete", func(p OrgLibraryPageParams) (OrgRequest, error) {
		path, err := orgLibraryPath(p.Page)
		return OrgRequest{Method: http.MethodDelete, Path: path}, err
	})
}
