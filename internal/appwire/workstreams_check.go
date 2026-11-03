package appwire

import (
	"bytes"
	"encoding/json"
	"math"
	"reflect"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/workspace"
	"github.com/Agent-Field/codeaf/internal/workstream"
)

// workstreamRequired distinguishes a missing required zero-valued field from
// an explicit zero, including plan version 0 and false wake flags. Editor JSON
// stays opaque; unknown additive fields stay ignored by the typed decoder.
func workstreamRequired(raw json.RawMessage, shape reflect.Type) error {
	if shape == reflect.TypeFor[json.RawMessage]() || shape == reflect.TypeFor[time.Time]() {
		return nil
	}
	if shape.Kind() == reflect.Pointer {
		shape = shape.Elem()
	}
	switch shape.Kind() {
	case reflect.Struct:
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
			return Errorf(CodeBadParams, "workstream params must be an object")
		}
		for i := 0; i < shape.NumField(); i++ {
			field := shape.Field(i)
			tag := field.Tag.Get("json")
			name, _, _ := strings.Cut(tag, ",")
			value, exists := fields[name]
			if !exists {
				if !strings.Contains(tag, ",omitempty") {
					return Errorf(CodeBadParams, "%s is required", name)
				}
				continue
			}
			if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
				return Errorf(CodeBadParams, "%s must not be null", name)
			}
			if err := workstreamRequired(value, field.Type); err != nil {
				return err
			}
		}
	case reflect.Slice:
		var values []json.RawMessage
		if err := json.Unmarshal(raw, &values); err != nil {
			return Errorf(CodeBadParams, "expected an array")
		}
		for _, value := range values {
			if err := workstreamRequired(value, shape.Elem()); err != nil {
				return err
			}
		}
	}
	return nil
}

func workstreamID(id string) error {
	if !utf8.ValidString(id) || !plainText(id, collectionIDMax) {
		return Errorf(CodeBadParams, "a workstream target needs a nonempty id without surrounding space or control characters")
	}
	return nil
}

func workstreamOptionalID(id *string) error {
	if id == nil {
		return nil
	}
	return workstreamID(*id)
}

func workstreamText(text string, min, max int, field string) error {
	if !utf8.ValidString(text) || len(text) < min || len(text) > max || (min > 0 && strings.TrimSpace(text) == "") {
		return Errorf(CodeBadParams, "%s is %d to %d bytes", field, min, max)
	}
	if strings.IndexFunc(text, func(r rune) bool { return unicode.IsControl(r) && r != '\n' && r != '\t' }) >= 0 {
		return Errorf(CodeBadParams, "%s allows only newline and tab control characters", field)
	}
	return nil
}

func workstreamLine(text string, min, max int, field string) error {
	if !singleLineText(text) {
		return Errorf(CodeBadParams, "%s must not contain control characters", field)
	}
	return workstreamText(text, min, max, field)
}

func workstreamOptionalLine(text *string, min, max int, field string) error {
	if text == nil {
		return nil
	}
	return workstreamLine(*text, min, max, field)
}

func workstreamOptionalText(text *string, min, max int, field string) error {
	if text == nil {
		return nil
	}
	return workstreamText(*text, min, max, field)
}

func workstreamEnum(word string, words ...string) error {
	for _, allowed := range words {
		if word == allowed {
			return nil
		}
	}
	return Errorf(CodeBadParams, "invalid workstream value %q", word)
}

func workstreamOptionalEnum(word *string, words ...string) error {
	if word == nil {
		return nil
	}
	return workstreamEnum(*word, words...)
}

// workstreamChecks keeps each family of checks ordered and returns the first refusal.
func workstreamChecks(checks ...error) error {
	for _, err := range checks {
		if err != nil {
			return err
		}
	}
	return nil
}

func workstreamDoc(doc json.RawMessage, required bool) error {
	if len(doc) > workstream.DocMax {
		return Errorf(CodeTooLarge, "encoded document exceeds 1 MiB")
	}
	if len(doc) == 0 && !required {
		return nil
	}
	trimmed := bytes.TrimSpace(doc)
	if !utf8.Valid(doc) || !json.Valid(doc) || len(trimmed) == 0 || trimmed[0] != '{' {
		return Errorf(CodeBadParams, "doc must be an editor JSON object")
	}
	return nil
}

func (p WorkstreamRef) Check() error {
	if err := workstreamChecks(workstreamID(p.ID), workstreamOptionalID(p.SessionID), workstreamOptionalID(p.Factory)); err != nil {
		return err
	}
	if p.Factory != nil && p.Kind != RefFactoryJob {
		return Errorf(CodeBadParams, "only a factory_job reference has a factory")
	}
	if p.SessionID != nil && p.Kind != RefTask && p.Kind != RefProcess {
		return Errorf(CodeBadParams, "only a task or process reference has a session_id")
	}
	return CheckRef(Ref{Kind: p.Kind, ID: p.ID, SessionID: workstreamString(p.SessionID), Factory: workstreamString(p.Factory)}, true)
}

func workstreamString(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func (p WorkstreamSource) Check() error {
	if err := workstreamChecks(workstreamID(p.ID), workstreamOptionalLine(p.Label, 0, 200, "source label")); err != nil {
		return err
	}
	switch p.Kind {
	case "chat", "page", "url":
	case "message":
		if p.ChatID == nil {
			return Errorf(CodeBadParams, "message source requires chat_id")
		}
		if err := workstreamID(*p.ChatID); err != nil {
			return err
		}
	case "factory_job":
		if err := CheckRef(Ref{Kind: RefFactoryJob, ID: p.ID, Factory: workstreamString(p.Factory)}, true); err != nil {
			return err
		}
	default:
		return Errorf(CodeBadParams, "unknown source kind %q", p.Kind)
	}
	return workstreamChecks(workstreamOptionalID(p.ChatID), workstreamOptionalID(p.Factory))
}

func workstreamSource(p *WorkstreamSource) error {
	if p == nil {
		return nil
	}
	return p.Check()
}

func (p WorkstreamListParams) Check() error          { return nil }
func (p WorkstreamGetParams) Check() error           { return workstreamID(p.ID) }
func (p WorkstreamReopenParams) Check() error        { return workstreamID(p.ID) }
func (p WorkstreamPlanGetParams) Check() error       { return workstreamID(p.ID) }
func (p WorkstreamPageListParams) Check() error      { return workstreamID(p.WorkstreamID) }
func (p WorkstreamPageGetParams) Check() error       { return workstreamID(p.ID) }
func (p WorkstreamPageDeleteParams) Check() error    { return workstreamID(p.ID) }
func (p WorkstreamRecordKeepParams) Check() error    { return workstreamID(p.ID) }
func (p WorkstreamRecordDropParams) Check() error    { return workstreamID(p.ID) }
func (p WorkstreamGoAheadRevokeParams) Check() error { return workstreamID(p.ID) }
func (p WorkstreamGoAheadListParams) Check() error   { return workstreamID(p.WorkstreamID) }

func (p WorkstreamCreateParams) Check() error {
	if !utf8.ValidString(p.Name) {
		return Errorf(CodeBadParams, "name must be valid UTF-8")
	}
	if _, err := collectionName(p.Name); err != nil {
		return err
	}
	if err := workstreamChecks(workstreamOptionalText(p.Why, 0, 4000, "why"), workstreamOptionalLine(p.ClientID, 0, 128, "client_id"), workstreamOptionalID(p.OriginChatID), workstreamOptionalID(p.Model)); err != nil {
		return err
	}
	if len(p.Refs) > CollectionRefsMax {
		return Errorf(CodeBadParams, "refs takes at most %d references", CollectionRefsMax)
	}
	for _, r := range p.Refs {
		if err := r.Check(); err != nil {
			return err
		}
	}
	return nil
}

func (p WorkstreamUpdateParams) Check() error {
	if p.Patch.Name != nil {
		if !utf8.ValidString(*p.Patch.Name) {
			return Errorf(CodeBadParams, "name must be valid UTF-8")
		}
		if _, err := collectionName(*p.Patch.Name); err != nil {
			return err
		}
	}
	if p.Patch.BudgetUSD != nil && (math.IsNaN(*p.Patch.BudgetUSD) || math.IsInf(*p.Patch.BudgetUSD, 0) || *p.Patch.BudgetUSD < 0) {
		return Errorf(CodeBadParams, "budget_usd must be finite and nonnegative")
	}
	return workstreamChecks(workstreamID(p.ID), workstreamOptionalText(p.Patch.Why, 0, 4000, "why"), workstreamOptionalEnum(p.Patch.Autonomy, "propose", "small", "off"))
}
func (p WorkstreamMarkDoneParams) Check() error {
	return workstreamChecks(workstreamID(p.ID), workstreamOptionalText(p.HowItWent, 0, 4000, "how_it_went"))
}

func (p WorkstreamPlanSetParams) Check() error {
	if err := workstreamID(p.ID); err != nil {
		return err
	}
	if p.BaseVersion < 0 || p.Milestones == nil || p.Waves == nil {
		return Errorf(CodeBadParams, "plan requires nonnegative base_version and milestones and waves arrays")
	}
	for _, m := range p.Milestones {
		if m.ID != "" {
			if err := workstreamID(m.ID); err != nil {
				return err
			}
		}
		if err := workstreamLine(m.Title, 1, 200, "milestone title"); err != nil {
			return err
		}
	}
	last := 0
	for _, w := range p.Waves {
		if w.N <= last || w.Items == nil {
			return Errorf(CodeBadParams, "waves must be positive, unique, ascending and have items arrays")
		}
		last = w.N
		if err := workstreamChecks(workstreamOptionalID(w.MilestoneID), workstreamOptionalLine(w.Title, 0, 200, "wave title")); err != nil {
			return err
		}
		for _, item := range w.Items {
			if item.Job != nil && *item.Job <= 0 {
				return Errorf(CodeBadParams, "plan job must be positive")
			}
			if err := workstreamChecks(workstreamOptionalID(item.Factory), workstreamOptionalID(item.DraftID)); err != nil {
				return err
			}
			switch item.Kind {
			case "job":
				if item.Job == nil || *item.Job <= 0 || item.Factory == nil {
					return Errorf(CodeBadParams, "plan job requires factory and positive job")
				}
				if err := workstreamID(*item.Factory); err != nil {
					return err
				}
			case "draft":
				if item.DraftID == nil || item.Title == nil {
					return Errorf(CodeBadParams, "draft requires draft_id and title")
				}
				if err := workstreamChecks(workstreamID(*item.DraftID), workstreamLine(*item.Title, 1, 200, "draft title")); err != nil {
					return err
				}
				if item.Text != nil {
					if err := workstreamText(*item.Text, 0, len(*item.Text), "draft text"); err != nil {
						return err
					}
				}
			default:
				return Errorf(CodeBadParams, "unknown plan item kind %q", item.Kind)
			}
		}
	}
	return nil
}

func (p WorkstreamPageSaveParams) Check() error {
	if p.BaseVersion < 0 {
		return Errorf(CodeBadParams, "base_version must be nonnegative")
	}
	return workstreamChecks(workstreamID(p.ID), workstreamDoc(p.Doc, true))
}
func (p WorkstreamPageCreateParams) Check() error {
	return workstreamChecks(workstreamID(p.WorkstreamID), workstreamOptionalID(p.ParentID), workstreamOptionalID(p.AfterID), workstreamLine(p.Title, 1, 200, "page title"), workstreamOptionalEnum(p.Owner, "you", "agent"), workstreamDoc(p.Doc, false))
}
func (p WorkstreamPageUpdateParams) Check() error {
	return workstreamChecks(workstreamID(p.ID), workstreamOptionalLine(p.Title, 1, 200, "page title"), workstreamOptionalEnum(p.Owner, "you", "agent"))
}
func (p WorkstreamPageMoveParams) Check() error {
	return workstreamChecks(workstreamID(p.ID), workstreamOptionalID(p.ParentID), workstreamOptionalID(p.AfterID))
}
func (p WorkstreamPageHistoryParams) Check() error {
	_, err := bounded("limit", p.Limit, 1, 200, 50)
	return workstreamChecks(workstreamID(p.ID), err)
}
func (p WorkstreamPageRestoreParams) Check() error {
	if p.Version < 1 || p.BaseVersion < 0 {
		return Errorf(CodeBadParams, "restore requires positive version and nonnegative base_version")
	}
	return workstreamID(p.ID)
}
func (p WorkstreamRecordListParams) Check() error {
	return workstreamChecks(workstreamID(p.WorkstreamID), workstreamOptionalEnum(p.Kind, "decision", "question"), workstreamOptionalEnum(p.State, "proposed", "kept", "dropped", "superseded"))
}
func (p WorkstreamRecordProposeParams) Check() error {
	return workstreamChecks(workstreamID(p.WorkstreamID), workstreamEnum(p.Kind, "decision", "question"), workstreamText(p.Text, 1, 2000, "record text"), workstreamSource(p.Source))
}
func (p WorkstreamRecordSupersedeParams) Check() error {
	return workstreamChecks(workstreamID(p.ID), workstreamOptionalEnum(p.Kind, "decision", "question"), workstreamText(p.Text, 1, 2000, "record text"), workstreamSource(p.Source))
}
func (p WorkstreamGoAheadGrantParams) Check() error {
	if err := workstreamID(p.WorkstreamID); err != nil {
		return err
	}
	if math.IsNaN(p.CapUSD) || math.IsInf(p.CapUSD, 0) || p.CapUSD <= 0 || p.CapUSD > workspace.WorkstreamGoAheadCapMax || p.EndsAt.IsZero() {
		return Errorf(CodeBadParams, "go-ahead requires cap_usd greater than 0 and at most %d, and ends_at", workspace.WorkstreamGoAheadCapMax)
	}
	switch p.Scope {
	case "job":
		if p.Factory == nil || p.Job == nil || *p.Job <= 0 || p.Wave != nil || p.MilestoneID != nil {
			return Errorf(CodeBadParams, "job scope requires factory and job only")
		}
		return workstreamID(*p.Factory)
	case "wave":
		if p.Wave == nil || *p.Wave <= 0 || p.Factory != nil || p.Job != nil || p.MilestoneID != nil {
			return Errorf(CodeBadParams, "wave scope requires wave only")
		}
	case "milestone":
		if p.MilestoneID == nil || p.Factory != nil || p.Job != nil || p.Wave != nil {
			return Errorf(CodeBadParams, "milestone scope requires milestone_id only")
		}
		return workstreamID(*p.MilestoneID)
	default:
		return Errorf(CodeBadParams, "unknown go-ahead scope %q", p.Scope)
	}
	return nil
}
