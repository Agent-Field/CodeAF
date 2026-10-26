package knowledge

import (
	"encoding/json"
	"errors"
	"fmt"
)

// Provider wire types stay at this boundary. Invalid primitive/reference types
// fail the batch instead of silently discarding saved source material.
type modelEvidence struct{ Source, Quote string }
type modelClaim struct {
	LocalID                       string `json:"local_id"`
	Text, Scope, Topic, Kind, Key string
	Evidence                      []modelEvidence
	Entities                      []string
}
type modelNote struct {
	ID, Title, Scope string
	Folder, Claims   []string
	Links            []struct{ Title, Scope, Reason string }
}
type modelRelation struct{ From, To, Kind, Reason string }
type modelAlias struct{ From, To string }
type modelParagraph struct {
	Text   string
	Claims []string
}
type modelSection struct {
	Heading    string
	Paragraphs []modelParagraph
}
type modelQuestion struct {
	Question, Why string
	Claims        []string
	SearchTerms   []string `json:"search_terms"`
}
type modelConflict struct {
	Claims []string
	Reason string
}
type modelCheck struct {
	ID, Reason          string
	Supported, Grounded bool
}

func validateCompilerResponse(role string, result document) error {
	raw, err := json.Marshal(result)
	if err != nil {
		return err
	}
	var target any
	required := ""
	switch role {
	case "update":
		target = &struct {
			Claims    []modelClaim
			Notes     []modelNote
			Relations []modelRelation
			Aliases   []modelAlias
		}{}
		required = "claims"
	case "discover", "organize_subjects":
		target = &struct {
			Subjects []struct {
				Title, Scope, Purpose, CreationReason string
				ExistingSubjectID                     string `json:"existing_subject_id"`
				Aliases, Notes, Folder                []string
			}
		}{}
		required = "subjects"
	case "understand":
		target = &struct {
			Sections  []modelSection
			Questions []modelQuestion
			Conflicts []modelConflict
		}{}
		required = "sections"
	case "review_understanding":
		target = &struct{ Checks, Questions, Conflicts []modelCheck }{}
	case "resolve_questions":
		target = &struct {
			Matches []struct {
				ID, Relation, Reason string
				Claims               []string
			}
		}{}
		required = "matches"
	case "curate_questions":
		target = &struct{ Keep []struct{ ID, Reason string } }{}
		required = "keep"
	case "frontier":
		target = &struct{ Selected []struct{ ID, Reason string } }{}
		required = "selected"
	case "learner_step":
		target = &struct {
			Action, Query, Source string
			Start, Length         int
			Sections              []struct {
				Heading    string
				Paragraphs []struct {
					Text           string
					Claims, Blocks []string
				}
			}
			Gaps []string
		}{}
		required = "action"
	case "review_guide":
		target = &struct {
			Checks []struct {
				ID, Reason        string
				Supported, Useful bool
			}
		}{}
	case "investigate":
		target = &struct {
			Answers []struct {
				Question, Heading, Text string
				Claims                  []string
				Evidence                []modelEvidence
			}
		}{}
		required = "answers"
	case "review_answers":
		target = &struct {
			Checks []struct {
				Question, Reason    string
				Supported, Complete bool
			}
		}{}
	case "compose":
		target = &struct {
			Notes []struct {
				ID       string
				Sections []modelSection
			}
		}{}
		required = "notes"
	case "review_articles":
		target = &struct{ Checks []modelCheck }{}
	case "audit_omissions":
		target = &struct {
			Omissions []struct{ Source, Text, Quote, Reason string }
		}{}
		required = "omissions"
	case "recall":
		target = &struct {
			Answer    string
			Citations []string
			Abstain   bool
		}{}
		required = "answer"
	case "query":
		target = &struct{ Terms, Notes []string }{}
		required = "terms"
	default:
		return fmt.Errorf("unknown native compiler role %q", role)
	}
	if err = json.Unmarshal(raw, target); err != nil {
		return errors.New("knowledge provider result has invalid field or reference types")
	}
	if required != "" {
		value, ok := result[required]
		if !ok || value == nil {
			return errors.New("knowledge provider result is missing its required collection")
		}
	}
	return nil
}

func validateCompilerReferences(role string, payload, response document) error {
	if role != "discover" && role != "organize_subjects" {
		return nil
	}
	existing, notes := index(items(payload["existing_subjects"])), index(items(payload["catalog"]))
	for _, subject := range items(response["subjects"]) {
		target := textValue(subject["existing_subject_id"])
		if target != "" && existing[target] == nil {
			return errors.New("knowledge organization referenced an uninspected existing page")
		}
		if flag(payload["reuse_only"]) && target == "" {
			return errors.New("knowledge reuse-only organization proposed a new page")
		}
		for _, id := range stringsValue(subject["notes"]) {
			if notes[id] == nil {
				return errors.New("knowledge organization referenced an uninspected evidence note")
			}
		}
	}
	return nil
}
