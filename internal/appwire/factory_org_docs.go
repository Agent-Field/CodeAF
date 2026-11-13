package appwire

import (
	"encoding/base64"
	"net/http"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ── AN ORGANIZATION'S LIVE DOCUMENTS ───────────────────────────────────────
//
// A live document is a Yjs document two people type into at once: a
// workstream page (`ws/<id>/page/<pid>`) or the text of a canvas frame
// (`canvas/<id>/frame/<fid>`). It lives on the organization's server, which
// keeps the log of updates and the latest snapshot and never parses either;
// the desk's Yjs makes every byte. The engine carries them: the read, send,
// snapshot and awareness methods are one request each to the connection,
// and factory.org_doc_follow keeps the document's stream open and relays it
// as factory.org_doc_update and factory.org_doc_awareness until
// factory.org_doc_unfollow, the connection's sign-in changes or the engine
// stops.
//
// An engine that serves them says "org_docs" in hello.

// FeatureOrgDocs is the hello word.
const FeatureOrgDocs = "org_docs"

// The events a followed document relays.
const (
	// EventFactoryOrgDocUpdate is one stamped update, or (snapshot true) the
	// state to load before the updates after it. Its data is
	// [FactoryOrgDocUpdate].
	EventFactoryOrgDocUpdate = "factory.org_doc_update"
	// EventFactoryOrgDocAwareness is one client's caret and name. Its data is
	// [FactoryOrgDocAwareness].
	EventFactoryOrgDocAwareness = "factory.org_doc_awareness"
	// EventFactoryOrgDocClosed says a followed document stopped being
	// relayed. Its data is [FactoryOrgDocClosed].
	EventFactoryOrgDocClosed = "factory.org_doc_closed"
)

// FactoryOrgDocUpdate is one update in seq order. Apply it with
// Y.applyUpdate; a snapshot is applied the same way.
type FactoryOrgDocUpdate struct {
	Connection string    `json:"connection"`
	Doc        string    `json:"doc"`
	Seq        int64     `json:"seq"`
	By         string    `json:"by"`
	At         time.Time `json:"at"`
	Update     string    `json:"update"`
	Snapshot   bool      `json:"snapshot"`
}

// FactoryOrgDocAwareness is one client's awareness state; a null State is a
// client that left or lapsed.
type FactoryOrgDocAwareness struct {
	Connection string    `json:"connection"`
	Doc        string    `json:"doc"`
	By         string    `json:"by"`
	Client     uint64    `json:"client"`
	State      *string   `json:"state"`
	At         time.Time `json:"at"`
}

// FactoryOrgDocClosed ends a follow: "removed" (the page was deleted) or
// "gone" (the document can no longer be seen, or the sign-in ended).
type FactoryOrgDocClosed struct {
	Connection string `json:"connection"`
	Doc        string `json:"doc"`
	Reason     string `json:"reason" enum:"removed,gone"`
}

// The answers, the server's shapes field for field.

// OrgDocUpdate is one stamped update.
type OrgDocUpdate struct {
	Seq    int64     `json:"seq"`
	By     string    `json:"by"`
	At     time.Time `json:"at"`
	Update string    `json:"update"`
}

// OrgDocSnapshot is the latest snapshot and the seq it covers.
type OrgDocSnapshot struct {
	Seq   int64     `json:"seq"`
	By    string    `json:"by"`
	At    time.Time `json:"at"`
	State string    `json:"state"`
}

// OrgDocAware is one client's awareness state.
type OrgDocAware struct {
	By     string    `json:"by"`
	Client uint64    `json:"client"`
	State  *string   `json:"state"`
	At     time.Time `json:"at"`
}

// OrgDocState is factory.org_doc_read's answer.
type OrgDocState struct {
	Doc       string          `json:"doc"`
	Seq       int64           `json:"seq"`
	Snapshot  *OrgDocSnapshot `json:"snapshot"`
	Updates   []OrgDocUpdate  `json:"updates"`
	Awareness []OrgDocAware   `json:"awareness"`
	CanEdit   bool            `json:"can_edit"`
	Compact   bool            `json:"compact"`
}

// OrgDocUpdated is factory.org_doc_send's answer.
type OrgDocUpdated struct {
	Doc     string    `json:"doc"`
	Seq     int64     `json:"seq"`
	By      string    `json:"by"`
	At      time.Time `json:"at"`
	Compact bool      `json:"compact"`
}

// OrgDocSnapshotKept is factory.org_doc_snapshot's answer.
type OrgDocSnapshotKept struct {
	Doc  string `json:"doc"`
	Seq  int64  `json:"seq"`
	Kept bool   `json:"kept"`
}

// ── params ─────────────────────────────────────────────────────────────────

// OrgDocParams names one document; Since is the newest seq applied.
type OrgDocParams struct {
	OrgConn
	Doc   string `json:"doc"`
	Since int64  `json:"since,omitempty"`
}

// OrgDocSendParams sends one update.
type OrgDocSendParams struct {
	OrgConn
	Doc     string `json:"doc"`
	BaseSeq int64  `json:"base_seq"`
	Update  string `json:"update"`
	Seed    bool   `json:"seed,omitempty"`
}

// OrgDocSnapshotParams posts a snapshot covering Seq.
type OrgDocSnapshotParams struct {
	OrgConn
	Doc   string `json:"doc"`
	Seq   int64  `json:"seq"`
	State string `json:"state"`
}

// OrgDocAwareParams sends this client's awareness state; null says it left.
type OrgDocAwareParams struct {
	OrgConn
	Doc    string  `json:"doc"`
	Client uint64  `json:"client"`
	State  *string `json:"state"`
}

// FactoryOrgDocs is the engine's following half.
type FactoryOrgDocs interface {
	// FactoryOrgDocFollow starts relaying doc on connection id from since;
	// following one already followed moves nothing.
	FactoryOrgDocFollow(id, doc string, since int64) error
	// FactoryOrgDocUnfollow stops it.
	FactoryOrgDocUnfollow(id, doc string)
}

// orgDocID is a document id: two kinds, each with its one child word.
var orgDocID = regexp.MustCompile(`^(ws/[A-Za-z0-9][A-Za-z0-9_.-]{0,127}/page|canvas/[A-Za-z0-9][A-Za-z0-9_.-]{0,127}/frame)/[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)

// OrgDocPath checks doc and answers its route.
func OrgDocPath(doc string) (string, error) {
	doc = strings.TrimSpace(doc)
	if !orgDocID.MatchString(doc) {
		return "", Errorf(CodeBadParams, "doc is ws/<workstream>/page/<page> or canvas/<canvas>/frame/<frame>")
	}
	return "/v1/docs/" + doc, nil
}

func orgDocBytes(field, value string, most int) error {
	if value == "" || len(value) > base64.StdEncoding.EncodedLen(most) {
		return Errorf(CodeBadParams, "%s is base64 of 1 to %d bytes", field, most)
	}
	if _, err := base64.StdEncoding.DecodeString(value); err != nil {
		return Errorf(CodeBadParams, "%s is base64 of the bytes Yjs made", field)
	}
	return nil
}

// The limits the server keeps, checked before anything is sent.
const (
	OrgDocUpdateMost    = 256 << 10
	OrgDocSnapshotMost  = 2 << 20
	OrgDocAwarenessMost = 4 << 10
)

func init() {
	eventShapes[EventFactoryOrgDocUpdate] = reflect.TypeFor[FactoryOrgDocUpdate]()
	eventShapes[EventFactoryOrgDocAwareness] = reflect.TypeFor[FactoryOrgDocAwareness]()
	eventShapes[EventFactoryOrgDocClosed] = reflect.TypeFor[FactoryOrgDocClosed]()

	methods["factory.org_doc_read"] = filled(orgMethod[OrgDocParams, OrgDocState]("factory.org_doc_read", func(p OrgDocParams) (OrgRequest, error) {
		path, err := OrgDocPath(p.Doc)
		if err != nil {
			return OrgRequest{}, err
		}
		if p.Since < 0 {
			return OrgRequest{}, Errorf(CodeBadParams, "since is the newest seq applied, 0 or more")
		}
		if p.Since > 0 {
			path += "?since=" + strconv.FormatInt(p.Since, 10)
		}
		return OrgRequest{Method: http.MethodGet, Path: path}, nil
	}), func(s *OrgDocState) {
		s.Updates, s.Awareness = nonNil(s.Updates), nonNil(s.Awareness)
	})
	methods["factory.org_doc_send"] = orgMethod[OrgDocSendParams, OrgDocUpdated]("factory.org_doc_send", func(p OrgDocSendParams) (OrgRequest, error) {
		path, err := OrgDocPath(p.Doc)
		if err != nil {
			return OrgRequest{}, err
		}
		if err := orgDocBytes("update", p.Update, OrgDocUpdateMost); err != nil {
			return OrgRequest{}, err
		}
		if p.BaseSeq < 0 {
			return OrgRequest{}, Errorf(CodeBadParams, "base_seq is the newest seq applied, 0 or more")
		}
		body := struct {
			BaseSeq int64  `json:"base_seq"`
			Update  string `json:"update"`
			Seed    bool   `json:"seed,omitempty"`
		}{p.BaseSeq, p.Update, p.Seed}
		return OrgRequest{Method: http.MethodPost, Path: path + "/updates", Body: body}, nil
	})
	methods["factory.org_doc_snapshot"] = orgMethod[OrgDocSnapshotParams, OrgDocSnapshotKept]("factory.org_doc_snapshot", func(p OrgDocSnapshotParams) (OrgRequest, error) {
		path, err := OrgDocPath(p.Doc)
		if err != nil {
			return OrgRequest{}, err
		}
		if p.Seq < 1 {
			return OrgRequest{}, Errorf(CodeBadParams, "seq is the newest update the snapshot covers")
		}
		if err := orgDocBytes("state", p.State, OrgDocSnapshotMost); err != nil {
			return OrgRequest{}, err
		}
		body := struct {
			Seq   int64  `json:"seq"`
			State string `json:"state"`
		}{p.Seq, p.State}
		return OrgRequest{Method: http.MethodPut, Path: path + "/snapshot", Body: body}, nil
	})
	methods["factory.org_doc_aware"] = orgMethod[OrgDocAwareParams, Empty]("factory.org_doc_aware", func(p OrgDocAwareParams) (OrgRequest, error) {
		path, err := OrgDocPath(p.Doc)
		if err != nil {
			return OrgRequest{}, err
		}
		if p.State != nil {
			if err := orgDocBytes("state", *p.State, OrgDocAwarenessMost); err != nil {
				return OrgRequest{}, err
			}
		}
		body := struct {
			Client uint64  `json:"client"`
			State  *string `json:"state"`
		}{p.Client, p.State}
		return OrgRequest{Method: http.MethodPost, Path: path + "/awareness", Body: body}, nil
	})
	methods["factory.org_doc_follow"] = typed(func(e Engine, p OrgDocParams) (*Empty, error) {
		f, err := factoryOrgDocsOf(e, "factory.org_doc_follow")
		if err != nil {
			return nil, err
		}
		id, err := p.id()
		if err != nil {
			return nil, err
		}
		if _, err := OrgDocPath(p.Doc); err != nil {
			return nil, err
		}
		if p.Since < 0 {
			return nil, Errorf(CodeBadParams, "since is the newest seq applied, 0 or more")
		}
		return &Empty{}, f.FactoryOrgDocFollow(id, strings.TrimSpace(p.Doc), p.Since)
	})
	methods["factory.org_doc_unfollow"] = typed(func(e Engine, p OrgDocParams) (*Empty, error) {
		f, err := factoryOrgDocsOf(e, "factory.org_doc_unfollow")
		if err != nil {
			return nil, err
		}
		id, err := p.id()
		if err != nil {
			return nil, err
		}
		if _, err := OrgDocPath(p.Doc); err != nil {
			return nil, err
		}
		f.FactoryOrgDocUnfollow(id, strings.TrimSpace(p.Doc))
		return &Empty{}, nil
	})
}

func factoryOrgDocsOf(e Engine, name string) (FactoryOrgDocs, error) {
	if _, err := factoryOrgOf(e, name); err != nil {
		return nil, err
	}
	f, ok := e.(FactoryOrgDocs)
	if !ok {
		return nil, Errorf(CodeUnknownMethod, "this engine does not serve %s", name)
	}
	return f, nil
}

func withFactoryOrgDocsFeature(hello Hello, e Engine) Hello {
	if _, err := factoryOrgDocsOf(e, "hello"); err == nil {
		hello = withFeature(hello, FeatureOrgDocs)
	}
	return hello
}
