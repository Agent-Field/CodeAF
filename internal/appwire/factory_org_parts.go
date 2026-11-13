package appwire

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// ── A WORKSTREAM'S PARTS ON AN ORGANIZATION'S SERVER ───────────────────────
//
// Each part of an organization workstream has its own route on its server
// (internal/factory/api/workstream_parts.go), and each route has one method
// here, built the same way as factory_org.go's: checked ids, one escaped path,
// the body the method was given and nothing more, the rev as If-Match. The
// engine keeps nothing; the server decides who may change what.
//
// A page's BODY is not one of these: it is a live document (factory.org_doc_*).
// These methods keep a page's place, title and kind, and its saved versions.
//
// An engine that serves them says "org_workstream_parts" in hello.

// FeatureOrgWorkstreamParts is the hello word for the methods below.
const FeatureOrgWorkstreamParts = "org_workstream_parts"

// ── answers ──

// OrgPageVersion is one saved version of a page. BodyMD and State come only
// with factory.org_workstream_page_version and the snapshot and restore
// answers; State is the live document's encoded state.
type OrgPageVersion struct {
	Version      int    `json:"version"`
	At           string `json:"at"`
	By           string `json:"by"`
	Title        string `json:"title"`
	Reason       string `json:"reason"`
	RestoredFrom int    `json:"restored_from,omitempty"`
	Bytes        int    `json:"bytes"`
	BodyMD       string `json:"body_md,omitempty"`
	State        []byte `json:"state,omitempty"`
}

// OrgPageVersions is a page's versions, newest first.
type OrgPageVersions struct {
	Versions []OrgPageVersion `json:"versions"`
}

// OrgWorkstreamPlan is a workstream's plan (the desk's own object, or null)
// and its rev, 0 before the first.
type OrgWorkstreamPlan struct {
	Plan      json.RawMessage `json:"plan"`
	Rev       int             `json:"rev"`
	UpdatedAt string          `json:"updated_at,omitempty"`
	UpdatedBy string          `json:"updated_by,omitempty"`
}

// OrgWorkstreamWakes and OrgWorkstreamSuggestions are the lists, each item the
// desk's own object with the server's id, rev, by, at, updated_by and
// updated_at on top.
type OrgWorkstreamWakes struct {
	Wakes []json.RawMessage `json:"wakes"`
}

type OrgWorkstreamSuggestions struct {
	Suggestions []json.RawMessage `json:"suggestions"`
}

// ── params ──

// OrgPageParams names one page of one workstream.
type OrgPageParams struct {
	OrgConn
	ID   string `json:"id"`
	Page string `json:"page"`
}

// OrgPageCreateParams adds a page. Index is its place among all the pages,
// the end when left out.
type OrgPageCreateParams struct {
	OrgConn
	ID     string `json:"id"`
	Title  string `json:"title"`
	Kind   string `json:"kind,omitempty"`
	Parent string `json:"parent,omitempty"`
	Index  *int   `json:"index,omitempty"`
}

// OrgPageUpdateParams renames, re-kinds or moves one page, guarded by the
// PAGE's rev. Parent "" moves it to the top.
type OrgPageUpdateParams struct {
	OrgConn
	ID     string  `json:"id"`
	Page   string  `json:"page"`
	Rev    int     `json:"rev"`
	Title  *string `json:"title,omitempty"`
	Kind   *string `json:"kind,omitempty"`
	Parent *string `json:"parent,omitempty"`
	Index  *int    `json:"index,omitempty"`
}

// OrgPageVersionParams names one version of one page.
type OrgPageVersionParams struct {
	OrgConn
	ID      string `json:"id"`
	Page    string `json:"page"`
	Version int    `json:"version"`
}

// OrgPageSnapshotParams saves a version of a page: its words, the live
// document's state, and why ("idle" or "manual").
type OrgPageSnapshotParams struct {
	OrgConn
	ID     string `json:"id"`
	Page   string `json:"page"`
	BodyMD string `json:"body_md"`
	State  []byte `json:"state,omitempty"`
	Reason string `json:"reason"`
}

// OrgPlanSetParams replaces the plan, guarded by the plan's rev (0 for the
// first one).
type OrgPlanSetParams struct {
	OrgConn
	ID   string          `json:"id"`
	Rev  int             `json:"rev"`
	Plan json.RawMessage `json:"plan"`
}

// OrgSuggestionsParams lists a workstream's suggestions, those on one page
// when Page is set.
type OrgSuggestionsParams struct {
	OrgConn
	ID   string `json:"id"`
	Page string `json:"page,omitempty"`
}

// OrgWakeAddParams and OrgSuggestionAddParams add one; the object is the
// desk's own.
type OrgWakeAddParams struct {
	OrgConn
	ID   string          `json:"id"`
	Wake json.RawMessage `json:"wake"`
}

type OrgSuggestionAddParams struct {
	OrgConn
	ID         string          `json:"id"`
	Suggestion json.RawMessage `json:"suggestion"`
}

// OrgWakeUpdateParams and OrgSuggestionUpdateParams merge into one, one level
// deep (a key sent replaces, null removes), guarded by its rev.
type OrgWakeUpdateParams struct {
	OrgConn
	ID     string          `json:"id"`
	WakeID string          `json:"wake_id"`
	Rev    int             `json:"rev"`
	Wake   json.RawMessage `json:"wake"`
}

type OrgSuggestionUpdateParams struct {
	OrgConn
	ID           string          `json:"id"`
	SuggestionID string          `json:"suggestion_id"`
	Rev          int             `json:"rev"`
	Suggestion   json.RawMessage `json:"suggestion"`
}

// OrgWorkstreamJobParams names one factory job (its number on that server)
// of one workstream.
type OrgWorkstreamJobParams struct {
	OrgConn
	ID  string `json:"id"`
	Job int    `json:"job"`
}

// ── checks ──

func orgPagePath(id, page string) (string, error) {
	base, err := orgWorkstreamPath(id)
	if err != nil {
		return "", err
	}
	page, err = orgSegment("page", page)
	if err != nil {
		return "", err
	}
	return base + "/pages/" + url.PathEscape(page), nil
}

// orgObject refuses anything but one JSON object.
func orgObject(field string, raw json.RawMessage) (json.RawMessage, error) {
	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) != nil || obj == nil {
		return nil, Errorf(CodeBadParams, "%s must be one JSON object", field)
	}
	return raw, nil
}

func orgRev(rev int, first bool) (string, error) {
	if rev < 0 || rev == 0 && !first {
		return "", Errorf(CodeBadParams, "rev is required: the rev it was read at")
	}
	return strconv.Itoa(rev), nil
}

// ── the methods ──

func withFactoryOrgPartsFeature(hello Hello, e Engine) Hello {
	if _, ok := e.(Factory); !ok {
		return hello
	}
	if _, ok := e.(FactoryOrg); ok {
		hello = withFeature(hello, FeatureOrgWorkstreamParts)
	}
	return hello
}

func init() {
	ws := func(w *OrgWorkstream) { w.fill() }
	raw := func(r *json.RawMessage) {}
	versions := func(v *OrgPageVersions) { v.Versions = nonNil(v.Versions) }

	// pages
	methods["factory.org_workstream_page_create"] = filled(orgMethod[OrgPageCreateParams, OrgWorkstream]("factory.org_workstream_page_create", func(p OrgPageCreateParams) (OrgRequest, error) {
		path, err := orgWorkstreamPath(p.ID)
		if err != nil {
			return OrgRequest{}, err
		}
		if !plainText(strings.TrimSpace(p.Title), 300) && strings.TrimSpace(p.Title) != "" {
			return OrgRequest{}, Errorf(CodeBadParams, "title is one line of at most 300 characters")
		}
		body := struct {
			Title  string `json:"title"`
			Kind   string `json:"kind,omitempty"`
			Parent string `json:"parent,omitempty"`
			Index  *int   `json:"index,omitempty"`
		}{strings.TrimSpace(p.Title), p.Kind, p.Parent, p.Index}
		return OrgRequest{Method: http.MethodPost, Path: path + "/pages", Body: body}, nil
	}), ws)
	methods["factory.org_workstream_page_update"] = filled(orgMethod[OrgPageUpdateParams, OrgWorkstream]("factory.org_workstream_page_update", func(p OrgPageUpdateParams) (OrgRequest, error) {
		path, err := orgPagePath(p.ID, p.Page)
		if err != nil {
			return OrgRequest{}, err
		}
		rev, err := orgRev(p.Rev, false)
		if err != nil {
			return OrgRequest{}, err
		}
		body := struct {
			Title  *string `json:"title,omitempty"`
			Kind   *string `json:"kind,omitempty"`
			Parent *string `json:"parent,omitempty"`
			Index  *int    `json:"index,omitempty"`
		}{p.Title, p.Kind, p.Parent, p.Index}
		return OrgRequest{Method: http.MethodPatch, Path: path, Body: body, IfMatch: rev}, nil
	}), ws)
	methods["factory.org_workstream_page_delete"] = filled(orgMethod[OrgPageParams, OrgWorkstream]("factory.org_workstream_page_delete", func(p OrgPageParams) (OrgRequest, error) {
		path, err := orgPagePath(p.ID, p.Page)
		return OrgRequest{Method: http.MethodDelete, Path: path}, err
	}), ws)

	// versions
	methods["factory.org_workstream_page_versions"] = filled(orgMethod[OrgPageParams, OrgPageVersions]("factory.org_workstream_page_versions", func(p OrgPageParams) (OrgRequest, error) {
		path, err := orgPagePath(p.ID, p.Page)
		return OrgRequest{Method: http.MethodGet, Path: path + "/versions"}, err
	}), versions)
	methods["factory.org_workstream_page_version"] = orgMethod[OrgPageVersionParams, OrgPageVersion]("factory.org_workstream_page_version", func(p OrgPageVersionParams) (OrgRequest, error) {
		path, err := orgPagePath(p.ID, p.Page)
		if err != nil {
			return OrgRequest{}, err
		}
		if p.Version < 1 {
			return OrgRequest{}, Errorf(CodeBadParams, "version is required")
		}
		return OrgRequest{Method: http.MethodGet, Path: path + "/versions/" + strconv.Itoa(p.Version)}, nil
	})
	methods["factory.org_workstream_page_snapshot"] = orgMethod[OrgPageSnapshotParams, OrgPageVersion]("factory.org_workstream_page_snapshot", func(p OrgPageSnapshotParams) (OrgRequest, error) {
		path, err := orgPagePath(p.ID, p.Page)
		if err != nil {
			return OrgRequest{}, err
		}
		if p.Reason != "idle" && p.Reason != "manual" {
			return OrgRequest{}, Errorf(CodeBadParams, `reason must be "idle" or "manual"`)
		}
		body := struct {
			BodyMD string `json:"body_md"`
			State  []byte `json:"state,omitempty"`
			Reason string `json:"reason"`
		}{p.BodyMD, p.State, p.Reason}
		return OrgRequest{Method: http.MethodPost, Path: path + "/versions", Body: body}, nil
	})
	methods["factory.org_workstream_page_restore"] = orgMethod[OrgPageVersionParams, OrgPageVersion]("factory.org_workstream_page_restore", func(p OrgPageVersionParams) (OrgRequest, error) {
		path, err := orgPagePath(p.ID, p.Page)
		if err != nil {
			return OrgRequest{}, err
		}
		if p.Version < 1 {
			return OrgRequest{}, Errorf(CodeBadParams, "version is required")
		}
		return OrgRequest{Method: http.MethodPost, Path: path + "/restore", Body: map[string]int{"version": p.Version}}, nil
	})

	// plan
	methods["factory.org_workstream_plan_get"] = orgMethod[OrgWorkstreamParams, OrgWorkstreamPlan]("factory.org_workstream_plan_get", func(p OrgWorkstreamParams) (OrgRequest, error) {
		path, err := orgWorkstreamPath(p.ID)
		return OrgRequest{Method: http.MethodGet, Path: path + "/plan"}, err
	})
	methods["factory.org_workstream_plan_set"] = orgMethod[OrgPlanSetParams, OrgWorkstreamPlan]("factory.org_workstream_plan_set", func(p OrgPlanSetParams) (OrgRequest, error) {
		path, err := orgWorkstreamPath(p.ID)
		if err != nil {
			return OrgRequest{}, err
		}
		rev, err := orgRev(p.Rev, true)
		if err != nil {
			return OrgRequest{}, err
		}
		plan, err := orgObject("plan", p.Plan)
		if err != nil {
			return OrgRequest{}, err
		}
		return OrgRequest{Method: http.MethodPut, Path: path + "/plan", Body: map[string]json.RawMessage{"plan": plan}, IfMatch: rev}, nil
	})

	// wakes and suggestions
	methods["factory.org_workstream_wakes"] = filled(orgMethod[OrgWorkstreamParams, OrgWorkstreamWakes]("factory.org_workstream_wakes", func(p OrgWorkstreamParams) (OrgRequest, error) {
		path, err := orgWorkstreamPath(p.ID)
		return OrgRequest{Method: http.MethodGet, Path: path + "/wakes"}, err
	}), func(w *OrgWorkstreamWakes) { w.Wakes = nonNil(w.Wakes) })
	methods["factory.org_workstream_wake_add"] = filled(orgMethod[OrgWakeAddParams, json.RawMessage]("factory.org_workstream_wake_add", func(p OrgWakeAddParams) (OrgRequest, error) {
		path, err := orgWorkstreamPath(p.ID)
		if err != nil {
			return OrgRequest{}, err
		}
		wake, err := orgObject("wake", p.Wake)
		return OrgRequest{Method: http.MethodPost, Path: path + "/wakes", Body: wake}, err
	}), raw)
	methods["factory.org_workstream_wake_update"] = filled(orgMethod[OrgWakeUpdateParams, json.RawMessage]("factory.org_workstream_wake_update", func(p OrgWakeUpdateParams) (OrgRequest, error) {
		path, err := orgWorkstreamPath(p.ID)
		if err != nil {
			return OrgRequest{}, err
		}
		id, err := orgSegment("wake_id", p.WakeID)
		if err != nil {
			return OrgRequest{}, err
		}
		rev, err := orgRev(p.Rev, false)
		if err != nil {
			return OrgRequest{}, err
		}
		wake, err := orgObject("wake", p.Wake)
		return OrgRequest{Method: http.MethodPatch, Path: path + "/wakes/" + url.PathEscape(id), Body: wake, IfMatch: rev}, err
	}), raw)
	methods["factory.org_workstream_suggestions"] = filled(orgMethod[OrgSuggestionsParams, OrgWorkstreamSuggestions]("factory.org_workstream_suggestions", func(p OrgSuggestionsParams) (OrgRequest, error) {
		path, err := orgWorkstreamPath(p.ID)
		if err != nil {
			return OrgRequest{}, err
		}
		path += "/suggestions"
		if strings.TrimSpace(p.Page) != "" {
			page, err := orgSegment("page", p.Page)
			if err != nil {
				return OrgRequest{}, err
			}
			path += "?page=" + url.QueryEscape(page)
		}
		return OrgRequest{Method: http.MethodGet, Path: path}, nil
	}), func(s *OrgWorkstreamSuggestions) { s.Suggestions = nonNil(s.Suggestions) })
	methods["factory.org_workstream_suggestion_add"] = filled(orgMethod[OrgSuggestionAddParams, json.RawMessage]("factory.org_workstream_suggestion_add", func(p OrgSuggestionAddParams) (OrgRequest, error) {
		path, err := orgWorkstreamPath(p.ID)
		if err != nil {
			return OrgRequest{}, err
		}
		s, err := orgObject("suggestion", p.Suggestion)
		return OrgRequest{Method: http.MethodPost, Path: path + "/suggestions", Body: s}, err
	}), raw)
	methods["factory.org_workstream_suggestion_update"] = filled(orgMethod[OrgSuggestionUpdateParams, json.RawMessage]("factory.org_workstream_suggestion_update", func(p OrgSuggestionUpdateParams) (OrgRequest, error) {
		path, err := orgWorkstreamPath(p.ID)
		if err != nil {
			return OrgRequest{}, err
		}
		id, err := orgSegment("suggestion_id", p.SuggestionID)
		if err != nil {
			return OrgRequest{}, err
		}
		rev, err := orgRev(p.Rev, false)
		if err != nil {
			return OrgRequest{}, err
		}
		s, err := orgObject("suggestion", p.Suggestion)
		return OrgRequest{Method: http.MethodPatch, Path: path + "/suggestions/" + url.PathEscape(id), Body: s, IfMatch: rev}, err
	}), raw)

	// jobs and unarchive
	job := func(method string) func(OrgWorkstreamJobParams) (OrgRequest, error) {
		return func(p OrgWorkstreamJobParams) (OrgRequest, error) {
			path, err := orgWorkstreamPath(p.ID)
			if err != nil {
				return OrgRequest{}, err
			}
			if p.Job < 1 {
				return OrgRequest{}, Errorf(CodeBadParams, "job is required: the job's number on that factory")
			}
			return OrgRequest{Method: method, Path: path + "/jobs/" + strconv.Itoa(p.Job)}, nil
		}
	}
	methods["factory.org_workstream_job_add"] = filled(orgMethod[OrgWorkstreamJobParams, OrgWorkstream]("factory.org_workstream_job_add", job(http.MethodPut)), ws)
	methods["factory.org_workstream_job_remove"] = filled(orgMethod[OrgWorkstreamJobParams, OrgWorkstream]("factory.org_workstream_job_remove", job(http.MethodDelete)), ws)
	methods["factory.org_workstream_unarchive"] = filled(orgMethod[OrgWorkstreamParams, OrgWorkstream]("factory.org_workstream_unarchive", func(p OrgWorkstreamParams) (OrgRequest, error) {
		path, err := orgWorkstreamPath(p.ID)
		return OrgRequest{Method: http.MethodPost, Path: path + "/unarchive"}, err
	}), ws)
}
