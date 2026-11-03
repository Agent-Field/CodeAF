package appwire

import (
	"encoding/json"
	"time"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/workstream"
)

type WorkstreamKeptRef struct {
	Kind    string  `json:"kind" enum:"record,page,message,factory_job"`
	ID      string  `json:"id"`
	ChatID  *string `json:"chat_id,omitempty"`
	Factory *string `json:"factory,omitempty"`
}

type WorkstreamSourceAdd struct {
	Kind       string             `json:"kind" enum:"file,repo,issue,link,kept"`
	Path       *string            `json:"path,omitempty"`
	Repo       *string            `json:"repo,omitempty"`
	Number     *int               `json:"number,omitempty"`
	URL        *string            `json:"url,omitempty"`
	Ref        *WorkstreamKeptRef `json:"ref,omitempty"`
	Title      *string            `json:"title,omitempty"`
	ReadByJobs *bool              `json:"read_by_jobs,omitempty"`
}

type WorkstreamSourceItem struct {
	ID           string             `json:"id"`
	WorkstreamID string             `json:"workstream_id"`
	Kind         string             `json:"kind" enum:"file,repo,issue,link,kept"`
	Title        string             `json:"title"`
	Path         *string            `json:"path,omitempty"`
	Repo         *string            `json:"repo,omitempty"`
	Number       *int               `json:"number,omitempty"`
	URL          *string            `json:"url,omitempty"`
	Ref          *WorkstreamKeptRef `json:"ref,omitempty"`
	ReadByJobs   bool               `json:"read_by_jobs"`
	Proposed     bool               `json:"proposed"`
	Why          *string            `json:"why,omitempty"`
	AddedBy      string             `json:"added_by" enum:"you,agent"`
	AddedAt      time.Time          `json:"added_at"`
	Gone         *bool              `json:"gone,omitempty"`
}

type WorkstreamSourceListParams struct {
	WorkstreamID string `json:"workstream_id"`
}
type WorkstreamSourceListResult struct {
	Sources []WorkstreamSourceItem `json:"sources"`
}

type WorkstreamSourceAddParams struct {
	WorkstreamID string             `json:"workstream_id"`
	Kind         string             `json:"kind" enum:"file,repo,issue,link,kept"`
	Path         *string            `json:"path,omitempty"`
	Repo         *string            `json:"repo,omitempty"`
	Number       *int               `json:"number,omitempty"`
	URL          *string            `json:"url,omitempty"`
	Ref          *WorkstreamKeptRef `json:"ref,omitempty"`
	Title        *string            `json:"title,omitempty"`
	ReadByJobs   *bool              `json:"read_by_jobs,omitempty"`
}
type WorkstreamSourceAddResult struct {
	Source  WorkstreamSourceItem `json:"source"`
	Already bool                 `json:"already"`
}

type WorkstreamSourceUpdateParams struct {
	ID         string  `json:"id"`
	Title      *string `json:"title,omitempty"`
	ReadByJobs *bool   `json:"read_by_jobs,omitempty"`
	Keep       *bool   `json:"keep,omitempty"`
}
type WorkstreamSourceUpdateResult struct {
	Source WorkstreamSourceItem `json:"source"`
}
type WorkstreamSourceRemoveParams struct {
	ID string `json:"id"`
}
type WorkstreamSourceRemoveResult struct {
	Source WorkstreamSourceItem `json:"source"`
}

// Check raw source bytes before the JSON decoder can replace invalid UTF-8.
func workstreamDecodeSource(raw []byte, target any) error {
	if !utf8.Valid(raw) {
		return Errorf(CodeBadParams, "source fields must be valid UTF-8")
	}
	return json.Unmarshal(raw, target)
}

func (p *WorkstreamSourceAdd) UnmarshalJSON(raw []byte) error {
	type plain WorkstreamSourceAdd
	return workstreamDecodeSource(raw, (*plain)(p))
}

func (p *WorkstreamSourceAddParams) UnmarshalJSON(raw []byte) error {
	type plain WorkstreamSourceAddParams
	return workstreamDecodeSource(raw, (*plain)(p))
}

func (p *WorkstreamSourceUpdateParams) UnmarshalJSON(raw []byte) error {
	type plain WorkstreamSourceUpdateParams
	return workstreamDecodeSource(raw, (*plain)(p))
}

// SourceAdd preserves optional field presence for the domain's kind checks.
func (p WorkstreamSourceAdd) SourceAdd() workstream.SourceAdd {
	out := workstream.SourceAdd{Kind: p.Kind, Path: p.Path, Repo: p.Repo, Number: p.Number, URL: p.URL, Title: p.Title}
	if p.ReadByJobs != nil {
		out.ReadByJobs = *p.ReadByJobs
	}
	if p.Ref != nil {
		out.Ref = &workstream.KeptRef{Kind: p.Ref.Kind, ID: p.Ref.ID, ChatID: p.Ref.ChatID, Factory: p.Ref.Factory}
	}
	return out
}

func (p WorkstreamSourceAddParams) SourceAdd() workstream.SourceAdd {
	return (WorkstreamSourceAdd{Kind: p.Kind, Path: p.Path, Repo: p.Repo, Number: p.Number, URL: p.URL, Ref: p.Ref, Title: p.Title, ReadByJobs: p.ReadByJobs}).SourceAdd()
}

func (p WorkstreamSourceListParams) Check() error   { return workstreamID(p.WorkstreamID) }
func (p WorkstreamSourceRemoveParams) Check() error { return workstreamID(p.ID) }

func (p WorkstreamSourceAddParams) Check() error {
	if err := workstreamID(p.WorkstreamID); err != nil {
		return err
	}
	if err := workstream.ValidateSourceAdd(p.SourceAdd()); err != nil {
		return Errorf(CodeBadParams, "%s", err)
	}
	return nil
}

func (p WorkstreamSourceUpdateParams) Check() error {
	if err := workstreamID(p.ID); err != nil {
		return err
	}
	if err := workstream.ValidateSourcePatch(workstream.SourcePatch{Title: p.Title, ReadByJobs: p.ReadByJobs, Keep: p.Keep}); err != nil {
		return Errorf(CodeBadParams, "%s", err)
	}
	return nil
}
