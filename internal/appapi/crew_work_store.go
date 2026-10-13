package appapi

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Agent-Field/codeaf/internal/teams"
)

// Linked work is owned by its resource. Finish reads its state through this
// door and never stops, edits or approves that work.
type CrewItemSource interface {
	CrewItemDone(context.Context, CrewItem) (bool, error)
}

type crewWorkMeta struct {
	Items        []CrewItem `json:"items"`
	ReportPacket string     `json:"report_packet,omitempty"`
	EndAsk       *Ask       `json:"end_ask,omitempty"`
}

func readCrewWork(t teams.Team) (crewWorkMeta, error) {
	raw, err := json.Marshal(t)
	if err != nil {
		return crewWorkMeta{}, err
	}
	var envelope struct {
		Work crewWorkMeta `json:"app_work_v2"`
	}
	err = json.Unmarshal(raw, &envelope)
	if envelope.Work.Items == nil {
		envelope.Work.Items = []CrewItem{}
	}
	return envelope.Work, err
}
func writeCrewWork(t *teams.Team, m crewWorkMeta) error {
	raw, err := json.Marshal(t)
	if err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err = json.Unmarshal(raw, &fields); err != nil {
		return err
	}
	fields["app_work_v2"], err = json.Marshal(m)
	if err != nil {
		return err
	}
	raw, err = json.Marshal(fields)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, t)
}
func crewReportMarkdown(report CrewReport) string {
	var b strings.Builder
	b.WriteString("## What shipped\n\n" + report.Shipped + "\n\n## Your decisions\n\n")
	for _, text := range report.Decisions {
		b.WriteString("- " + text + "\n")
	}
	b.WriteString("\n## Files\n\n")
	for _, file := range report.Files {
		fmt.Fprintf(&b, "- %s\n", file.Path)
	}
	b.WriteString("\n## Left open\n\n")
	for _, item := range report.LeftOpen {
		b.WriteString("- " + item.Text)
		if item.HandOff != nil {
			fmt.Fprintf(&b, " (%s:%s)", item.HandOff.Kind, item.HandOff.ID)
		}
		b.WriteString("\n")
	}
	b.WriteString("\n## Cost\n\n$" + report.Cost.USD + "\n")
	return b.String()
}
func reportFromPacket(p teams.Packet, packets []teams.Packet, cost *Money) CrewReport {
	report := CrewReport{Decisions: []string{}, Files: []FileChange{}, LeftOpen: []ReportOpenItem{}, Cost: Money{USD: "0.00"}}
	if p.Report != nil {
		report.Shipped = p.Report.Done
		if p.Report.Left != "" {
			report.LeftOpen = append(report.LeftOpen, ReportOpenItem{Text: p.Report.Left})
		}
		for _, path := range p.Report.Files {
			report.Files = append(report.Files, FileChange{Path: path, State: "changed"})
		}
	}
	for _, decision := range packets {
		if decision.Kind != teams.PacketClosing && decision.State == teams.PacketDecided && decision.DecidedBy == teams.Person {
			text := decision.Question + " · " + decision.Decision
			if option, ok := decision.Option(decision.Decision); ok {
				text = decision.Question + " · " + option.Label
			}
			if decision.Reason != "" {
				text += " · " + decision.Reason
			}
			report.Decisions = append(report.Decisions, text)
		}
	}
	if cost != nil {
		report.Cost = *cost
	}
	report.Markdown = crewReportMarkdown(report)
	return report
}
