package appapi

import (
	"encoding/json"
	"net/http"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/crewagent"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

type crewAppLogEntry struct {
	CrewLogEntry
	Kind      string              `json:"kind"`
	Member    string              `json:"member"`
	App       string              `json:"app"`
	Badge     bridge.Badge        `json:"badge"`
	Changed   []svc.ChangedRecord `json:"changed"`
	Approvals []bridge.Approval   `json:"approvals"`
	ShapeThis string              `json:"shape_this,omitempty"`
	Cost      float64             `json:"cost,omitempty"`
}

func (s *Server) enrichCrewAppLog(r *http.Request, rec *replyWriter) error {
	if s.options.Agents == nil {
		return nil
	}
	receipts, err := crewagent.Receipts(s.options.Agents.home, r.PathValue("cid"))
	if err != nil {
		return err
	}
	if len(receipts) == 0 {
		return nil
	}
	var body CrewLogResponse
	if err := json.Unmarshal(rec.Bytes(), &body); err != nil {
		return err
	}
	entries := make([]any, 0, len(body.Entries))
	for _, entry := range body.Entries {
		var view any = entry
		for _, receipt := range receipts {
			if matchesCrewAppReceipt(receipt, entry) {
				view = crewAppLogEntry{CrewLogEntry: entry, Kind: "app_agent", Member: receipt.Member, App: receipt.Badge.App, Badge: receipt.Badge, Changed: receipt.Answer.Changed, Approvals: s.currentCrewApprovals(r, receipt), ShapeThis: receipt.Answer.ShapeThis, Cost: receipt.Answer.Cost}
				break
			}
		}
		entries = append(entries, view)
	}
	rec.Reset()
	return json.NewEncoder(rec).Encode(struct {
		Entries []any  `json:"entries"`
		Before  string `json:"before,omitempty"`
	}{Entries: entries, Before: body.Before})
}

func (s *Server) currentCrewApprovals(r *http.Request, receipt crewagent.LogReceipt) []bridge.Approval {
	approvals := append([]bridge.Approval{}, receipt.Answer.Approvals...)
	all := s.AppsServices()
	if all == nil || all.Approvals == nil {
		return approvals
	}
	scope := svc.Scope{App: receipt.Badge.App, Caller: appsCaller(), Data: svc.DataReal}
	for _, state := range []bridge.ApprovalState{bridge.ApprovalOpen, bridge.ApprovalApproved, bridge.ApprovalRejected} {
		page, err := all.Approvals.List(r.Context(), scope, bridge.ListApprovalsParams{State: state})
		if err != nil {
			continue
		}
		for _, current := range page.Approvals {
			for i, approval := range approvals {
				if approval.ID == current.ID {
					approvals[i] = current
				}
			}
		}
	}
	return approvals
}

func matchesCrewAppReceipt(receipt crewagent.LogReceipt, entry CrewLogEntry) bool {
	if receipt.Text != entry.Text {
		return false
	}
	if receipt.At.IsZero() {
		return true
	}
	return receipt.From == entry.From && entry.At != nil && receipt.At.Equal(*entry.At)
}
