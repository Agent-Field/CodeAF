package appwire

import (
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// ── AN ORGANIZATION'S CANVASES ─────────────────────────────────────────────
//
// An organization's canvases live on its server, kept there by canvas-core
// (factory/serve/canvases.go). Each factory.org_canvas* method below is one
// request to the attached connection with the caller's session, built here
// from checked ids exactly as the team and workstream methods are, and the
// server's answer comes back as the server wrote it: the shapes a canvas
// speaks (a snapshot, an op record, a version, a mark) are canvas-core's own,
// which the desk already reads for a Personal canvas, so the engine passes
// them through rather than re-typing them and drifting.
//
// THE ONE REFUSAL WITH A BODY: a write into a frame another person's hand is
// on is conflict, and its detail ([OrgCanvasConflict]) names the frame, who
// holds it and every op record the writer has not seen, so the desk catches
// up and tries again without another read.
//
// An engine that serves them says "org_canvases" in hello. Whether ONE
// organization keeps canvases is its own answer: a server without canvas-core
// answers not_found.

// FeatureOrgCanvases is the hello word, exactly as the desk reads it.
const FeatureOrgCanvases = "org_canvases"

// orgCanvasAnswerMost is the most one canvas answer may be: a whole canvas's
// snapshot or op log, or one asset, is larger than a workstream.
const orgCanvasAnswerMost = 64 << 20

// OrgCanvasConflict is the detail of a conflict on factory.org_canvas_apply:
// another person's hand is on Frame. Ops are the op records after the
// request's base_seq, as canvas-core wrote them.
type OrgCanvasConflict struct {
	Reason string            `json:"reason"`
	Frame  string            `json:"frame"`
	Holder string            `json:"holder"`
	Seq    int64             `json:"seq"`
	Ops    []json.RawMessage `json:"ops"`
}

// OrgCanvasList is factory.org_canvases's answer: the canvases the caller
// can see, newest change first, each canvas-core's summary with the
// organization's owner, shares, private, my_role, seq and updated_by.
type OrgCanvasList struct {
	Canvases []json.RawMessage `json:"canvases"`
}

// OrgCanvasOps is a run of canvas-core op records and the seq they end at.
type OrgCanvasOps struct {
	Seq int64             `json:"seq"`
	Ops []json.RawMessage `json:"ops"`
}

// OrgCanvasVersions are a canvas's versions, oldest first.
type OrgCanvasVersions struct {
	Versions []json.RawMessage `json:"versions"`
}

// OrgCanvasMarks are a canvas's marks (comments), oldest first, each with
// `by`, the person:<handle> who wrote it.
type OrgCanvasMarks struct {
	Marks []json.RawMessage `json:"marks"`
}

// OrgCanvasAssetData is one asset's bytes (base64 on the wire) and type.
type OrgCanvasAssetData struct {
	Data []byte `json:"data"`
	Type string `json:"type"`
}

// OrgBytes asks the carrier for the answer's bytes and type as they came,
// for an answer that is not JSON (an asset).
type OrgBytes struct {
	Data []byte
	Type string
}

// OrgCanvasParams names one canvas.
type OrgCanvasParams struct {
	OrgConn
	ID string `json:"id"`
}

// OrgCanvasCreateParams makes a canvas. Owner left out is the caller.
type OrgCanvasCreateParams struct {
	OrgConn
	Title   string     `json:"title"`
	Owner   *OrgOwner  `json:"owner,omitempty"`
	Shares  []OrgShare `json:"shares,omitempty"`
	Private bool       `json:"private,omitempty"`
}

// OrgCanvasUpdateParams renames a canvas (edit) or moves it to or from the
// trash (full).
type OrgCanvasUpdateParams struct {
	OrgConn
	ID      string  `json:"id"`
	Title   *string `json:"title,omitempty"`
	Trashed *bool   `json:"trashed,omitempty"`
}

// OrgCanvasOpsParams reads the op records after Since.
type OrgCanvasOpsParams struct {
	OrgConn
	ID    string `json:"id"`
	Since int64  `json:"since"`
}

// OrgCanvasApplyParams writes canvas-core ops as the caller. A hand is a
// lease op {t:"lease", frame, actor:"", until:"hand"} (until "" lets go).
type OrgCanvasApplyParams struct {
	OrgConn
	ID      string            `json:"id"`
	BaseSeq int64             `json:"base_seq"`
	Ops     []json.RawMessage `json:"ops"`
}

// OrgCanvasVersionParams saves a version.
type OrgCanvasVersionParams struct {
	OrgConn
	ID    string `json:"id"`
	Label string `json:"label,omitempty"`
}

// OrgCanvasRestoreParams brings a canvas back to a version.
type OrgCanvasRestoreParams struct {
	OrgConn
	ID      string `json:"id"`
	Version string `json:"version"`
}

// OrgCanvasMarkParams adds a mark: canvas-core's NewMark {frame?, node?,
// shape?, text}.
type OrgCanvasMarkParams struct {
	OrgConn
	ID   string          `json:"id"`
	Mark json.RawMessage `json:"mark"`
}

// OrgCanvasSharesParams sets who a canvas is shared with, and optionally its
// owner and whether it is private (full).
type OrgCanvasSharesParams struct {
	OrgConn
	ID      string     `json:"id"`
	Shares  []OrgShare `json:"shares"`
	Owner   *OrgOwner  `json:"owner,omitempty"`
	Private *bool      `json:"private,omitempty"`
}

// OrgCanvasImportAsset is one asset of a canvas moving in.
type OrgCanvasImportAsset struct {
	Sha  string `json:"sha"`
	Data []byte `json:"data"`
	Type string `json:"type,omitempty"`
	Name string `json:"name,omitempty"`
}

// OrgCanvasImportParams moves a Personal canvas into the organization once:
// its canvas-core snapshot, its assets and its id on this computer.
type OrgCanvasImportParams struct {
	OrgConn
	Title       string                 `json:"title,omitempty"`
	Snapshot    json.RawMessage        `json:"snapshot"`
	Assets      []OrgCanvasImportAsset `json:"assets,omitempty"`
	FromLocalID string                 `json:"from_local_id"`
	Owner       *OrgOwner              `json:"owner,omitempty"`
	Shares      []OrgShare             `json:"shares,omitempty"`
	Private     bool                   `json:"private,omitempty"`
}

// OrgCanvasAssetParams names one asset of one canvas.
type OrgCanvasAssetParams struct {
	OrgConn
	ID  string `json:"id"`
	Sha string `json:"sha"`
}

// OrgCanvasAssetPutParams stores one asset's bytes.
type OrgCanvasAssetPutParams struct {
	OrgConn
	ID   string `json:"id"`
	Sha  string `json:"sha"`
	Data []byte `json:"data"`
	Type string `json:"type,omitempty"`
	Name string `json:"name,omitempty"`
}

var (
	orgCanvasID  = regexp.MustCompile(`^cv_[a-z0-9]{12}$`)
	orgCanvasSha = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

func orgCanvasPath(id string) (string, error) {
	id = strings.TrimSpace(id)
	if !orgCanvasID.MatchString(id) {
		return "", Errorf(CodeBadParams, "id is not a canvas id this organization gave")
	}
	return "/v1/canvases/" + id, nil
}

func orgCanvasTitle(value string) (string, error) {
	value = strings.TrimSpace(value)
	if !plainText(value, 200) {
		return "", Errorf(CodeBadParams, "title is required: one line of at most 200 characters")
	}
	return value, nil
}

func withFactoryOrgCanvasFeature(hello Hello, e Engine) Hello {
	if _, ok := e.(Factory); !ok {
		return hello
	}
	if _, ok := e.(FactoryOrg); ok {
		hello = withFeature(hello, FeatureOrgCanvases)
	}
	return hello
}

func init() {
	raw := func(name string, build func(OrgCanvasParams) (OrgRequest, error)) method {
		return orgMethod[OrgCanvasParams, json.RawMessage](name, build)
	}
	ops := func(o *OrgCanvasOps) { o.Ops = nonNil(o.Ops) }

	methods["factory.org_canvases"] = filled(orgMethod[OrgConn, OrgCanvasList]("factory.org_canvases", func(OrgConn) (OrgRequest, error) {
		return OrgRequest{Method: http.MethodGet, Path: "/v1/canvases", MaxAnswer: orgCanvasAnswerMost}, nil
	}), func(l *OrgCanvasList) { l.Canvases = nonNil(l.Canvases) })
	methods["factory.org_canvas_get"] = raw("factory.org_canvas_get", func(p OrgCanvasParams) (OrgRequest, error) {
		path, err := orgCanvasPath(p.ID)
		return OrgRequest{Method: http.MethodGet, Path: path}, err
	})
	methods["factory.org_canvas_create"] = orgMethod[OrgCanvasCreateParams, json.RawMessage]("factory.org_canvas_create", func(p OrgCanvasCreateParams) (OrgRequest, error) {
		title, err := orgCanvasTitle(p.Title)
		if err != nil {
			return OrgRequest{}, err
		}
		if p.Owner != nil {
			if err := checkOrgOwner(p.Owner); err != nil {
				return OrgRequest{}, err
			}
		}
		shares, err := orgShareList(p.Shares)
		if err != nil {
			return OrgRequest{}, err
		}
		body := struct {
			Title   string     `json:"title"`
			Owner   *OrgOwner  `json:"owner,omitempty"`
			Shares  []OrgShare `json:"shares,omitempty"`
			Private bool       `json:"private,omitempty"`
		}{title, p.Owner, shares, p.Private}
		return OrgRequest{Method: http.MethodPost, Path: "/v1/canvases", Body: body}, nil
	})
	methods["factory.org_canvas_update"] = orgMethod[OrgCanvasUpdateParams, json.RawMessage]("factory.org_canvas_update", func(p OrgCanvasUpdateParams) (OrgRequest, error) {
		path, err := orgCanvasPath(p.ID)
		if err != nil {
			return OrgRequest{}, err
		}
		if p.Title == nil && p.Trashed == nil {
			return OrgRequest{}, Errorf(CodeBadParams, "send title or trashed")
		}
		if p.Title != nil {
			t, err := orgCanvasTitle(*p.Title)
			if err != nil {
				return OrgRequest{}, err
			}
			p.Title = &t
		}
		body := struct {
			Title   *string `json:"title,omitempty"`
			Trashed *bool   `json:"trashed,omitempty"`
		}{p.Title, p.Trashed}
		return OrgRequest{Method: http.MethodPatch, Path: path, Body: body}, nil
	})
	methods["factory.org_canvas_delete"] = orgMethod[OrgCanvasParams, Empty]("factory.org_canvas_delete", func(p OrgCanvasParams) (OrgRequest, error) {
		path, err := orgCanvasPath(p.ID)
		return OrgRequest{Method: http.MethodDelete, Path: path}, err
	})
	methods["factory.org_canvas_snapshot"] = raw("factory.org_canvas_snapshot", func(p OrgCanvasParams) (OrgRequest, error) {
		path, err := orgCanvasPath(p.ID)
		return OrgRequest{Method: http.MethodGet, Path: path + "/snapshot", MaxAnswer: orgCanvasAnswerMost}, err
	})
	methods["factory.org_canvas_ops"] = filled(orgMethod[OrgCanvasOpsParams, OrgCanvasOps]("factory.org_canvas_ops", func(p OrgCanvasOpsParams) (OrgRequest, error) {
		path, err := orgCanvasPath(p.ID)
		if err != nil {
			return OrgRequest{}, err
		}
		if p.Since < 0 {
			return OrgRequest{}, Errorf(CodeBadParams, "since is the seq read up to, zero or more")
		}
		return OrgRequest{Method: http.MethodGet, Path: path + "/ops?since=" + strconv.FormatInt(p.Since, 10), MaxAnswer: orgCanvasAnswerMost}, nil
	}), ops)
	methods["factory.org_canvas_apply"] = filled(orgMethod[OrgCanvasApplyParams, OrgCanvasOps]("factory.org_canvas_apply", func(p OrgCanvasApplyParams) (OrgRequest, error) {
		path, err := orgCanvasPath(p.ID)
		if err != nil {
			return OrgRequest{}, err
		}
		if len(p.Ops) == 0 {
			return OrgRequest{}, Errorf(CodeBadParams, "ops is required: at least one canvas op")
		}
		body := struct {
			BaseSeq int64             `json:"base_seq"`
			Ops     []json.RawMessage `json:"ops"`
		}{p.BaseSeq, p.Ops}
		return OrgRequest{Method: http.MethodPost, Path: path + "/ops", Body: body, MaxAnswer: orgCanvasAnswerMost}, nil
	}), ops)
	methods["factory.org_canvas_versions"] = filled(orgMethod[OrgCanvasParams, OrgCanvasVersions]("factory.org_canvas_versions", func(p OrgCanvasParams) (OrgRequest, error) {
		path, err := orgCanvasPath(p.ID)
		return OrgRequest{Method: http.MethodGet, Path: path + "/versions"}, err
	}), func(v *OrgCanvasVersions) { v.Versions = nonNil(v.Versions) })
	methods["factory.org_canvas_version_create"] = orgMethod[OrgCanvasVersionParams, json.RawMessage]("factory.org_canvas_version_create", func(p OrgCanvasVersionParams) (OrgRequest, error) {
		path, err := orgCanvasPath(p.ID)
		if err != nil {
			return OrgRequest{}, err
		}
		if p.Label != "" && !plainText(p.Label, 200) {
			return OrgRequest{}, Errorf(CodeBadParams, "label is one line of at most 200 characters")
		}
		return OrgRequest{Method: http.MethodPost, Path: path + "/versions", Body: map[string]string{"label": strings.TrimSpace(p.Label)}}, nil
	})
	methods["factory.org_canvas_restore"] = filled(orgMethod[OrgCanvasRestoreParams, OrgCanvasOps]("factory.org_canvas_restore", func(p OrgCanvasRestoreParams) (OrgRequest, error) {
		path, err := orgCanvasPath(p.ID)
		if err != nil {
			return OrgRequest{}, err
		}
		v, err := orgSegment("version", p.Version)
		if err != nil {
			return OrgRequest{}, err
		}
		return OrgRequest{Method: http.MethodPost, Path: path + "/versions/" + url.PathEscape(v) + "/restore", MaxAnswer: orgCanvasAnswerMost}, nil
	}), ops)
	methods["factory.org_canvas_marks"] = filled(orgMethod[OrgCanvasParams, OrgCanvasMarks]("factory.org_canvas_marks", func(p OrgCanvasParams) (OrgRequest, error) {
		path, err := orgCanvasPath(p.ID)
		return OrgRequest{Method: http.MethodGet, Path: path + "/marks"}, err
	}), func(m *OrgCanvasMarks) { m.Marks = nonNil(m.Marks) })
	methods["factory.org_canvas_mark_create"] = orgMethod[OrgCanvasMarkParams, json.RawMessage]("factory.org_canvas_mark_create", func(p OrgCanvasMarkParams) (OrgRequest, error) {
		path, err := orgCanvasPath(p.ID)
		if err != nil {
			return OrgRequest{}, err
		}
		if len(p.Mark) == 0 || p.Mark[0] != '{' {
			return OrgRequest{}, Errorf(CodeBadParams, "mark is {frame?, node?, shape?, text}")
		}
		return OrgRequest{Method: http.MethodPost, Path: path + "/marks", Body: p.Mark}, nil
	})
	methods["factory.org_canvas_shares"] = orgMethod[OrgCanvasSharesParams, json.RawMessage]("factory.org_canvas_shares", func(p OrgCanvasSharesParams) (OrgRequest, error) {
		path, err := orgCanvasPath(p.ID)
		if err != nil {
			return OrgRequest{}, err
		}
		shares, err := orgShareList(p.Shares)
		if err != nil {
			return OrgRequest{}, err
		}
		if p.Owner != nil {
			if err := checkOrgOwner(p.Owner); err != nil {
				return OrgRequest{}, err
			}
		}
		body := struct {
			Shares  []OrgShare `json:"shares"`
			Owner   *OrgOwner  `json:"owner,omitempty"`
			Private *bool      `json:"private,omitempty"`
		}{shares, p.Owner, p.Private}
		return OrgRequest{Method: http.MethodPut, Path: path + "/shares", Body: body}, nil
	})
	methods["factory.org_canvas_import"] = orgMethod[OrgCanvasImportParams, json.RawMessage]("factory.org_canvas_import", func(p OrgCanvasImportParams) (OrgRequest, error) {
		if strings.TrimSpace(p.FromLocalID) == "" {
			return OrgRequest{}, Errorf(CodeBadParams, "from_local_id is required: the canvas's id on this computer")
		}
		if len(p.Snapshot) == 0 || p.Snapshot[0] != '{' {
			return OrgRequest{}, Errorf(CodeBadParams, "snapshot is the canvas as canvas-core shows it")
		}
		if p.Title != "" {
			t, err := orgCanvasTitle(p.Title)
			if err != nil {
				return OrgRequest{}, err
			}
			p.Title = t
		}
		for _, a := range p.Assets {
			if !orgCanvasSha.MatchString(a.Sha) {
				return OrgRequest{}, Errorf(CodeBadParams, "an asset is named by the sha256 of its bytes")
			}
		}
		shares, err := orgShareList(p.Shares)
		if err != nil {
			return OrgRequest{}, err
		}
		if p.Owner != nil {
			if err := checkOrgOwner(p.Owner); err != nil {
				return OrgRequest{}, err
			}
		}
		body := struct {
			Title       string                 `json:"title,omitempty"`
			Snapshot    json.RawMessage        `json:"snapshot"`
			Assets      []OrgCanvasImportAsset `json:"assets,omitempty"`
			FromLocalID string                 `json:"from_local_id"`
			Owner       *OrgOwner              `json:"owner,omitempty"`
			Shares      []OrgShare             `json:"shares,omitempty"`
			Private     bool                   `json:"private,omitempty"`
		}{p.Title, p.Snapshot, p.Assets, strings.TrimSpace(p.FromLocalID), p.Owner, shares, p.Private}
		return OrgRequest{Method: http.MethodPost, Path: "/v1/canvases/import", Body: body, MaxAnswer: orgCanvasAnswerMost}, nil
	})
	methods["factory.org_canvas_asset_get"] = orgBytesMethod("factory.org_canvas_asset_get")
	methods["factory.org_canvas_asset_put"] = orgMethod[OrgCanvasAssetPutParams, json.RawMessage]("factory.org_canvas_asset_put", func(p OrgCanvasAssetPutParams) (OrgRequest, error) {
		path, err := orgCanvasPath(p.ID)
		if err != nil {
			return OrgRequest{}, err
		}
		if !orgCanvasSha.MatchString(p.Sha) {
			return OrgRequest{}, Errorf(CodeBadParams, "sha is the sha256 of the asset's bytes, hex")
		}
		if len(p.Data) == 0 {
			return OrgRequest{}, Errorf(CodeBadParams, "data is the asset's bytes, base64")
		}
		q := ""
		if p.Name != "" {
			q = "?name=" + url.QueryEscape(p.Name)
		}
		typ := p.Type
		if typ == "" {
			typ = "application/octet-stream"
		}
		return OrgRequest{Method: http.MethodPut, Path: path + "/assets/" + p.Sha + q, RawBody: p.Data, ContentType: typ}, nil
	})
}

// orgBytesMethod is factory.org_canvas_asset_get: the asset's bytes come
// back as they are stored, not as JSON, and go to the desk in base64.
func orgBytesMethod(name string) method {
	return typed(func(e Engine, p OrgCanvasAssetParams) (*OrgCanvasAssetData, error) {
		o, err := factoryOrgOf(e, name)
		if err != nil {
			return nil, err
		}
		id, err := p.id()
		if err != nil {
			return nil, err
		}
		path, err := orgCanvasPath(p.ID)
		if err != nil {
			return nil, err
		}
		if !orgCanvasSha.MatchString(p.Sha) {
			return nil, Errorf(CodeBadParams, "sha is the sha256 of the asset's bytes, hex")
		}
		ctx, cancel := factoryCall()
		defer cancel()
		var out OrgBytes
		if err := o.FactoryOrgCall(ctx, id, OrgRequest{Method: http.MethodGet, Path: path + "/assets/" + p.Sha, MaxAnswer: orgCanvasAnswerMost}, &out); err != nil {
			return nil, err
		}
		return &OrgCanvasAssetData{Data: out.Data, Type: out.Type}, nil
	})
}
