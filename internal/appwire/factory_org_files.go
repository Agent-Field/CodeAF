package appwire

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// ── A WORKSTREAM'S FILES ON AN ORGANIZATION'S SERVER ───────────────────────
//
// Files is a workstream's shared drive (internal/factory/api/workstream_files.go
// has the server's routes, factory/workstream_file.go the shape). Each method
// here is one request on one route, built the way factory_org_parts.go builds
// its own: checked ids, one escaped path, the body it was given and nothing
// more, the file's rev as If-Match. The engine keeps nothing.
//
// A document IS one of the workstream's pages: its words are the page's live
// document (factory.org_doc_* at ws/<id>/page/<page_id>), its versions the
// page's (factory.org_workstream_page_versions).
//
// An engine that serves them says "org_workstream_files" in hello.

// FeatureOrgWorkstreamFiles is the hello word for the methods below.
const FeatureOrgWorkstreamFiles = "org_workstream_files"

// orgFileAnswerMost is the most a files answer or an upload's bytes may be:
// an upload may be 25 MiB, which base64 grows by a third.
const orgFileAnswerMost = 36 << 20

// OrgWorkstreamFile is one file, field for field the server's.
type OrgWorkstreamFile = factory.WorkstreamFile

// OrgWorkstreamFiles is a workstream's files: the live ones, Recently deleted
// when asked for, and the parent's, read-only, when it has one.
type OrgWorkstreamFiles struct {
	Files   []OrgWorkstreamFile `json:"files"`
	Deleted []OrgWorkstreamFile `json:"deleted,omitempty"`
	From    *OrgFilesFrom       `json:"from,omitempty"`
}

// OrgFilesFrom is the parent's files as a child reads them.
type OrgFilesFrom struct {
	ID    string              `json:"id"`
	Name  string              `json:"name"`
	Files []OrgWorkstreamFile `json:"files"`
}

// OrgFilesParams lists one workstream's files: those matching Query by name
// or contents when set, and Recently deleted too when Deleted.
type OrgFilesParams struct {
	OrgConn
	ID      string `json:"id"`
	Query   string `json:"query,omitempty"`
	Deleted bool   `json:"deleted,omitempty"`
}

// OrgFileParams names one file of one workstream.
type OrgFileParams struct {
	OrgConn
	ID   string `json:"id"`
	File string `json:"file"`
}

// OrgFileCreateParams makes one file; kind decides which fields it needs
// (api.FileNew). Data is an upload's bytes, base64 on the wire.
type OrgFileCreateParams struct {
	OrgConn
	ID       string              `json:"id"`
	Kind     string              `json:"kind"`
	Name     string              `json:"name,omitempty"`
	Folder   string              `json:"folder,omitempty"`
	FromChat string              `json:"from_chat,omitempty"`
	FromTask string              `json:"from_task,omitempty"`
	MadeBy   *factory.FileMaker  `json:"made_by,omitempty"`
	BodyMD   string              `json:"body_md,omitempty"`
	CanvasID string              `json:"canvas_id,omitempty"`
	URL      string              `json:"url,omitempty"`
	Repo     string              `json:"repo,omitempty"`
	Data     []byte              `json:"data,omitempty"`
	Mime     string              `json:"mime,omitempty"`
	Target   *factory.FileTarget `json:"target,omitempty"`
}

// OrgFileUpdateParams renames a file, changes its task links, or says its
// contents were just changed ("person" or "agent"), guarded by the FILE's rev.
type OrgFileUpdateParams struct {
	OrgConn
	ID       string    `json:"id"`
	File     string    `json:"file"`
	Rev      int       `json:"rev"`
	Name     *string   `json:"name,omitempty"`
	FromTask *string   `json:"from_task,omitempty"`
	UsedBy   *[]string `json:"used_by,omitempty"`
	Edited   string    `json:"edited,omitempty"`
}

// OrgFileMoveParams moves files into Folder ("" is the top), all or none.
type OrgFileMoveParams struct {
	OrgConn
	ID     string   `json:"id"`
	Files  []string `json:"files"`
	Folder string   `json:"folder"`
	// To sends the files to another of the organization's workstreams
	// (Bring along), each still itself; Folder is then one of its folders.
	To string `json:"to,omitempty"`
}

// OrgFileContent is an upload's bytes (base64 on the wire) and type.
type OrgFileContent struct {
	Data []byte `json:"data"`
	Type string `json:"type"`
}

func orgFilePath(id, file string) (string, error) {
	base, err := orgWorkstreamPath(id)
	if err != nil {
		return "", err
	}
	file, err = orgSegment("file", file)
	if err != nil {
		return "", err
	}
	return base + "/files/" + url.PathEscape(file), nil
}

func withFactoryOrgFilesFeature(hello Hello, e Engine) Hello {
	if _, ok := e.(Factory); !ok {
		return hello
	}
	if _, ok := e.(FactoryOrg); ok {
		hello = withFeature(hello, FeatureOrgWorkstreamFiles)
	}
	return hello
}

func init() {
	list := func(l *OrgWorkstreamFiles) {
		l.Files = nonNil(l.Files)
		if l.From != nil {
			l.From.Files = nonNil(l.From.Files)
		}
	}
	one := func(f *OrgWorkstreamFile) { f.UsedBy = nonNil(f.UsedBy) }

	methods["factory.org_workstream_files"] = filled(orgMethod[OrgFilesParams, OrgWorkstreamFiles]("factory.org_workstream_files", func(p OrgFilesParams) (OrgRequest, error) {
		path, err := orgWorkstreamPath(p.ID)
		if err != nil {
			return OrgRequest{}, err
		}
		q := url.Values{}
		if s := strings.TrimSpace(p.Query); s != "" {
			q.Set("q", s)
		}
		if p.Deleted {
			q.Set("deleted", "1")
		}
		path += "/files"
		if len(q) > 0 {
			path += "?" + q.Encode()
		}
		return OrgRequest{Method: http.MethodGet, Path: path, MaxAnswer: orgFileAnswerMost}, nil
	}), list)
	methods["factory.org_workstream_file_create"] = filled(orgMethod[OrgFileCreateParams, OrgWorkstreamFile]("factory.org_workstream_file_create", func(p OrgFileCreateParams) (OrgRequest, error) {
		path, err := orgWorkstreamPath(p.ID)
		if err != nil {
			return OrgRequest{}, err
		}
		if strings.TrimSpace(p.Kind) == "" {
			return OrgRequest{}, Errorf(CodeBadParams, "kind is required")
		}
		name := strings.TrimSpace(p.Name)
		if name != "" && !plainText(name, 300) {
			return OrgRequest{}, Errorf(CodeBadParams, "name is one line of at most 300 characters")
		}
		body := struct {
			Kind     string              `json:"kind"`
			Name     string              `json:"name,omitempty"`
			Folder   string              `json:"folder,omitempty"`
			FromChat string              `json:"from_chat,omitempty"`
			FromTask string              `json:"from_task,omitempty"`
			MadeBy   *factory.FileMaker  `json:"made_by,omitempty"`
			BodyMD   string              `json:"body_md,omitempty"`
			CanvasID string              `json:"canvas_id,omitempty"`
			URL      string              `json:"url,omitempty"`
			Repo     string              `json:"repo,omitempty"`
			Data     []byte              `json:"data,omitempty"`
			Mime     string              `json:"mime,omitempty"`
			Target   *factory.FileTarget `json:"target,omitempty"`
		}{p.Kind, name, p.Folder, p.FromChat, p.FromTask, p.MadeBy, p.BodyMD, p.CanvasID, p.URL, p.Repo, p.Data, p.Mime, p.Target}
		return OrgRequest{Method: http.MethodPost, Path: path + "/files", Body: body}, nil
	}), one)
	methods["factory.org_workstream_file_update"] = filled(orgMethod[OrgFileUpdateParams, OrgWorkstreamFile]("factory.org_workstream_file_update", func(p OrgFileUpdateParams) (OrgRequest, error) {
		path, err := orgFilePath(p.ID, p.File)
		if err != nil {
			return OrgRequest{}, err
		}
		rev, err := orgRev(p.Rev, false)
		if err != nil {
			return OrgRequest{}, err
		}
		if p.Edited != "" && p.Edited != "person" && p.Edited != "agent" {
			return OrgRequest{}, Errorf(CodeBadParams, `edited is "person" or "agent"`)
		}
		body := struct {
			Name     *string   `json:"name,omitempty"`
			FromTask *string   `json:"from_task,omitempty"`
			UsedBy   *[]string `json:"used_by,omitempty"`
			Edited   string    `json:"edited,omitempty"`
		}{p.Name, p.FromTask, p.UsedBy, p.Edited}
		return OrgRequest{Method: http.MethodPatch, Path: path, Body: body, IfMatch: rev}, nil
	}), one)
	methods["factory.org_workstream_file_move"] = filled(orgMethod[OrgFileMoveParams, OrgWorkstreamFiles]("factory.org_workstream_file_move", func(p OrgFileMoveParams) (OrgRequest, error) {
		path, err := orgWorkstreamPath(p.ID)
		if err != nil {
			return OrgRequest{}, err
		}
		if len(p.Files) == 0 {
			return OrgRequest{}, Errorf(CodeBadParams, "files names at least one file to move")
		}
		for _, f := range p.Files {
			if _, err := orgSegment("files", f); err != nil {
				return OrgRequest{}, err
			}
		}
		if p.To != "" {
			if _, err := orgWorkstreamPath(p.To); err != nil {
				return OrgRequest{}, err
			}
		}
		body := struct {
			Files  []string `json:"files"`
			Folder string   `json:"folder"`
			To     string   `json:"to,omitempty"`
		}{p.Files, p.Folder, p.To}
		return OrgRequest{Method: http.MethodPost, Path: path + "/files/move", Body: body}, nil
	}), list)
	methods["factory.org_workstream_file_delete"] = filled(orgMethod[OrgFileParams, OrgWorkstreamFile]("factory.org_workstream_file_delete", func(p OrgFileParams) (OrgRequest, error) {
		path, err := orgFilePath(p.ID, p.File)
		return OrgRequest{Method: http.MethodDelete, Path: path}, err
	}), one)
	methods["factory.org_workstream_file_restore"] = filled(orgMethod[OrgFileParams, OrgWorkstreamFile]("factory.org_workstream_file_restore", func(p OrgFileParams) (OrgRequest, error) {
		path, err := orgFilePath(p.ID, p.File)
		return OrgRequest{Method: http.MethodPost, Path: path + "/restore"}, err
	}), one)
	methods["factory.org_workstream_file_content"] = typed(func(e Engine, p OrgFileParams) (*OrgFileContent, error) {
		o, err := factoryOrgOf(e, "factory.org_workstream_file_content")
		if err != nil {
			return nil, err
		}
		id, err := p.id()
		if err != nil {
			return nil, err
		}
		path, err := orgFilePath(p.ID, p.File)
		if err != nil {
			return nil, err
		}
		ctx, cancel := factoryCall()
		defer cancel()
		var out OrgBytes
		if err := o.FactoryOrgCall(ctx, id, OrgRequest{Method: http.MethodGet, Path: path + "/content", MaxAnswer: orgFileAnswerMost}, &out); err != nil {
			return nil, err
		}
		return &OrgFileContent{Data: out.Data, Type: out.Type}, nil
	})
}
