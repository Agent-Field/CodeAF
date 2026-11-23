package appwire

import (
	"context"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
	teamstore "github.com/Agent-Field/codeaf/internal/teams"
)

// FeatureCrewsOrganize advertises both read-only suggestion passes.
const FeatureCrewsOrganize = "crews.organize"

// CodeNoNamingModel lets the sheet retain its folder suggestions alone.
const CodeNoNamingModel = "no_naming_model"

// OrganizeAsk chooses the free folder pass or one naming-role request.
type OrganizeAsk struct {
	Pass string `json:"pass"`
}

// OrganizeNew is a suggested crew; Folder is present only in the folder pass.
type OrganizeNew struct {
	Name   string   `json:"name"`
	Folder string   `json:"folder,omitempty"`
	Agents []string `json:"agents"`
	Why    string   `json:"why"`
}

// OrganizeAddition adds agents to an existing crew without changing its name.
type OrganizeAddition struct {
	Crew   string   `json:"crew"`
	Agents []string `json:"agents"`
	Why    string   `json:"why"`
}

// OrganizeReply contains suggestions only. CostUSD is absent when usage did not
// publish a price; it is never estimated from prompt size or catalogue prices.
type OrganizeReply struct {
	AgentsUnassigned *int                   `json:"agents_unassigned,omitempty"`
	CrewsOpen        *int                   `json:"crews_open,omitempty"`
	New              []OrganizeNew          `json:"new"`
	Additions        []OrganizeAddition     `json:"additions"`
	Quiet            *[]teamstore.QuietCrew `json:"quiet,omitempty"`
	CostUSD          *float64               `json:"cost_usd,omitempty"`
	Model            string                 `json:"model,omitempty"`
}

// CrewsOrganizer serves either pass without applying or saving suggestions.
type CrewsOrganizer interface {
	CrewsOrganize(context.Context, OrganizeAsk) (*OrganizeReply, error)
}

func withCrewsOrganizeFeature(h Hello, e Engine) Hello {
	if _, ok := e.(CrewsOrganizer); ok {
		return withFeature(h, FeatureCrewsOrganize)
	}
	return h
}

// CheckOrganizeAsk refuses a missing or unknown pass before reading anything.
func CheckOrganizeAsk(p OrganizeAsk) error {
	if p.Pass != "folders" && p.Pass != "model" {
		return Errorf(CodeBadParams, "pass is folders or model")
	}
	return nil
}

func init() {
	methods[FeatureCrewsOrganize] = typed(func(e Engine, p OrganizeAsk) (*OrganizeReply, error) {
		door, ok := e.(CrewsOrganizer)
		if !ok {
			return nil, Errorf(CodeUnknownMethod, "this engine does not serve crews.organize")
		}
		if err := CheckOrganizeAsk(p); err != nil {
			return nil, err
		}
		wait := session.OrganizeWait
		if p.Pass == "folders" {
			wait = 2 * time.Second
		}
		ctx, cancel := context.WithTimeout(context.Background(), wait)
		defer cancel()
		return door.CrewsOrganize(ctx, p)
	})
}
