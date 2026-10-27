package knowledge_test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/knowledge"
)

const settingsGuideText = "Resetting settings preserves saved conversation history."

func answerIntegrationFixture(t *testing.T, answer string, edit func(map[string]any) map[string]any, core ...[]string) (*knowledge.Vault, func()) {
	t.Helper()
	return answerIntegrationFixtureWithUnderstanding(t, answer, edit, nil, core...)
}

func answerIntegrationFixtureWithUnderstanding(t *testing.T, answer string, edit, understanding func(map[string]any) map[string]any, core ...[]string) (*knowledge.Vault, func()) {
	t.Helper()
	v := knowledge.Open(t.TempDir())
	if err := v.StartBuild(); err != nil {
		t.Fatal(err)
	}
	withKnowledgeModel(t, func(p map[string]any) map[string]any {
		if p["integration_policy"] != nil || p["merge_policy"] != nil || p["coverage_policy"] != nil {
			return edit(p)
		}
		if messages, ok := p["messages"].([]any); ok {
			claims := []any{}
			for _, raw := range messages {
				s := raw.(map[string]any)
				text := string([]rune(s["text"].(string))[:min(900, len([]rune(s["text"].(string))))])
				claims = append(claims, map[string]any{"local_id": s["id"], "text": text, "scope": "Cedar", "topic": "Settings", "kind": "fact", "evidence": []any{map[string]any{"source": s["id"], "quote": text}}})
			}
			response := map[string]any{"claims": claims}
			if understanding != nil {
				return understanding(response)
			}
			return response
		}
		if catalog, ok := p["catalog"].([]any); ok {
			notes := []any{}
			for _, raw := range catalog {
				notes = append(notes, raw.(map[string]any)["id"])
			}
			s := map[string]any{"title": "Settings and history", "scope": "Cedar", "notes": notes}
			if existing, ok := p["existing_subjects"].([]any); ok && len(existing) > 0 {
				s["existing_subject_id"] = existing[0].(map[string]any)["id"]
			}
			return map[string]any{"subjects": []any{s}}
		}
		if paragraphs, ok := p["paragraphs"].([]any); ok {
			if len(paragraphs) > 0 && paragraphs[0].(map[string]any)["heading"] != nil {
				return edit(p)
			}
			checks := []any{}
			for _, raw := range paragraphs {
				checks = append(checks, map[string]any{"id": raw.(map[string]any)["id"], "supported": true, "useful": true})
			}
			conflicts := []any{}
			rawConflicts, _ := p["conflicts"].([]any)
			for _, raw := range rawConflicts {
				conflicts = append(conflicts, map[string]any{"id": raw.(map[string]any)["id"], "supported": true})
			}
			return map[string]any{"checks": checks, "questions": []any{map[string]any{"id": "Q0", "grounded": true}}, "conflicts": conflicts}
		}
		if answers, ok := p["answers"].([]any); ok {
			checks := []any{}
			for _, raw := range answers {
				checks = append(checks, map[string]any{"question": raw.(map[string]any)["question"], "supported": true, "complete": true})
			}
			return map[string]any{"checks": checks}
		}
		if questions, ok := p["questions"].([]any); ok && p["subject"] == nil {
			q := questions[0].(map[string]any)
			if p["per_subject"] != nil {
				return map[string]any{"keep": []any{map[string]any{"id": q["id"], "reason": "Understand settings preservation"}}}
			}
			if p["budget"] != nil {
				return map[string]any{"selected": []any{map[string]any{"id": q["id"], "reason": "Settings preservation"}}}
			}
			if p["sources"] != nil {
				refs := q["claims"].([]any)
				return map[string]any{"answers": []any{map[string]any{"question": q["id"], "heading": "Settings reset at revision abc123", "text": answer, "claims": refs, "evidence": []any{}}}}
			}
			return map[string]any{"matches": []any{}}
		}
		refs := []any{}
		for _, raw := range p["claims"].([]any) {
			refs = append(refs, raw.(map[string]any)["id"])
		}
		paragraphs := []any{map[string]any{"text": settingsGuideText, "claims": refs}}
		if len(core) > 0 {
			paragraphs = []any{}
			for _, text := range core[0] {
				paragraphs = append(paragraphs, map[string]any{"text": text, "claims": refs})
			}
		}
		sections := []any{map[string]any{"heading": "Reset settings", "paragraphs": paragraphs}}
		if len(core) > 0 {
			sections = []any{}
			for start := 0; start < len(paragraphs); start += 3 {
				heading := "Reset settings"
				if start > 0 {
					heading = fmt.Sprintf("History partitions %d", start/3)
				}
				sections = append(sections, map[string]any{"heading": heading, "paragraphs": paragraphs[start:min(len(paragraphs), start+3)]})
			}
		}
		response := map[string]any{"action": "write", "sections": sections, "questions": []any{map[string]any{"question": "How does Cedar preserve history during a settings reset?", "claims": refs, "search_terms": []any{"Cedar", "settings", "history"}}}}
		if understanding != nil {
			return understanding(response)
		}
		return response
	})
	source := "Cedar settings reset preserves saved history."
	if len(core) > 0 {
		source += " " + strings.Join(core[0], " ")
	}
	if err := v.Capture(knowledge.Source{ID: "baseline", Session: "chat", Role: "user", TS: "2026-10-26T12:00:00Z", Text: source}); err != nil {
		t.Fatal(err)
	}
	for pass := 0; pass < 10; pass++ {
		if err := v.Flush(context.Background(), "fixture", true); err != nil {
			t.Fatal(err)
		}
		status, err := v.Status()
		if err != nil {
			t.Fatal(err)
		}
		if status.Pending == 0 && !status.NeedsUpdate {
			break
		}
	}
	added := 0
	return v, func() {
		t.Helper()
		added++
		if err := v.Capture(knowledge.Source{ID: fmt.Sprintf("added-%d", added), Session: "chat", Role: "user", TS: "2026-10-26T13:00:00Z", Text: "Cedar settings history report: " + answer}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestKnowledgeMergesAdditionsWithoutDroppingExistingKnowledge(t *testing.T) {
	core := []string{}
	for i := 0; i < 12; i++ {
		core = append(core, fmt.Sprintf("Preserve history partition %d during a settings reset.", i))
	}
	detail := "Reconnect provider credentials after resetting settings."
	reviews := 0
	v, add := answerIntegrationFixture(t, detail, func(p map[string]any) map[string]any {
		if p["merge_policy"] != nil {
			refs := []any{}
			for _, raw := range p["paragraphs"].([]any) {
				refs = append(refs, raw.(map[string]any)["id"])
			}
			texts := []string{}
			for _, raw := range p["paragraphs"].([]any) {
				texts = append(texts, raw.(map[string]any)["text"].(string))
			}
			text := strings.Join(texts, " ")
			if p["previous_draft"] == nil {
				text += strings.Repeat(" repeated", 901)
			}
			return map[string]any{"sections": []any{map[string]any{"heading": "Reset settings", "paragraphs": []any{map[string]any{"text": text, "inputs": refs}}}}}
		}
		if p["coverage_policy"] != nil {
			reviews++
			coverage := []any{}
			for _, raw := range p["paragraphs"].([]any) {
				coverage = append(coverage, map[string]any{"id": raw.(map[string]any)["id"], "preserved": true})
			}
			return map[string]any{"checks": []any{map[string]any{"id": "R0", "supported": true, "useful": true}}, "coverage": coverage}
		}
		return map[string]any{"decisions": []any{map[string]any{"id": "A0", "action": "integrate", "section": "S0"}}}
	}, core)
	add()
	if err := v.Flush(context.Background(), "fixture", true); err != nil {
		t.Fatal(err)
	}
	p := settingsGuide(t, v)
	if len(p.Sections) != 4 || len(p.Sections[0].Paragraphs) != 1 || reviews != 1 {
		t.Fatalf("additions were trimmed instead of merged with coverage review: %+v reviews=%d", p.Sections, reviews)
	}
	texts := []string{}
	for _, section := range p.Sections {
		for _, paragraph := range section.Paragraphs {
			texts = append(texts, paragraph.Text)
		}
	}
	for _, fact := range append(core, detail) {
		if !strings.Contains(strings.Join(texts, " "), fact) {
			t.Fatalf("existing fact lost: %s", fact)
		}
	}
	if len(p.Sections[0].Paragraphs[0].Evidence) != 2 || len(p.Sections[0].Paragraphs[0].Claims) != 2 {
		t.Fatalf("merged paragraph lost original or new support: %+v", p.Sections[0].Paragraphs[0])
	}
}

func TestKnowledgePreservesOversizedCoreBeforeIntegratingAdditions(t *testing.T) {
	core := []string{}
	for i := 0; i < 13; i++ {
		core = append(core, fmt.Sprintf("Preserve history partition %d during a settings reset.", i))
	}
	detail := "Reconnect provider credentials after resetting settings."
	reviews, selections := 0, 0
	v, add := answerIntegrationFixture(t, detail, func(p map[string]any) map[string]any {
		if p["merge_policy"] == nil && p["coverage_policy"] == nil && p["integration_policy"] == nil {
			selections++
			ids := []any{}
			for _, raw := range p["paragraphs"].([]any) {
				id := raw.(map[string]any)["id"]
				if (selections == 1 && id != "P12") || (selections > 1 && id != "P0") {
					ids = append(ids, id)
				}
			}
			return map[string]any{"sections": []any{map[string]any{"heading": "Reset settings", "paragraphs": ids}}}
		}
		if p["merge_policy"] != nil {
			refs := []any{}
			for _, raw := range p["paragraphs"].([]any) {
				refs = append(refs, raw.(map[string]any)["id"])
			}
			texts := []string{}
			for _, raw := range p["paragraphs"].([]any) {
				texts = append(texts, raw.(map[string]any)["text"].(string))
			}
			text := strings.Join(texts, " ")
			if p["previous_draft"] == nil {
				text += strings.Repeat(" repeated", 901)
			}
			return map[string]any{"sections": []any{map[string]any{"heading": "Reset settings", "paragraphs": []any{map[string]any{"text": text, "inputs": refs}}}}}
		}
		if p["coverage_policy"] != nil {
			reviews++
			coverage := []any{}
			for _, raw := range p["paragraphs"].([]any) {
				coverage = append(coverage, map[string]any{"id": raw.(map[string]any)["id"], "preserved": true})
			}
			return map[string]any{"checks": []any{map[string]any{"id": "R0", "supported": true, "useful": true}}, "coverage": coverage}
		}
		return map[string]any{"decisions": []any{map[string]any{"id": "A0", "action": "integrate", "section": "S0"}}}
	}, core)
	add()
	if err := v.Flush(context.Background(), "fixture", true); err != nil {
		t.Fatal(err)
	}
	p := settingsGuide(t, v)
	if len(p.Sections) != 1 || len(p.Sections[0].Paragraphs) != 1 || reviews != 1 {
		t.Fatalf("additions were trimmed instead of merged with coverage review: %+v reviews=%d", p.Sections, reviews)
	}
	texts := []string{}
	for _, section := range p.Sections {
		for _, paragraph := range section.Paragraphs {
			texts = append(texts, paragraph.Text)
		}
	}
	for _, fact := range append(core, detail) {
		if !strings.Contains(strings.Join(texts, " "), fact) {
			t.Fatalf("existing fact lost: %s", fact)
		}
	}
	if len(p.Sections[0].Paragraphs[0].Evidence) != 2 || len(p.Sections[0].Paragraphs[0].Claims) != 2 {
		t.Fatalf("merged paragraph lost original or new support: %+v", p.Sections[0].Paragraphs[0])
	}
}

func TestKnowledgeRejectsUnsupportedMergesAndReusesFailedReviewProgress(t *testing.T) {
	core := []string{}
	for i := 0; i < 12; i++ {
		core = append(core, fmt.Sprintf("Preserve history partition %d during a settings reset.", i))
	}
	reviews := 0
	v, add := answerIntegrationFixture(t, "Reconnect provider credentials after resetting settings.", func(p map[string]any) map[string]any {
		if p["merge_policy"] != nil {
			refs := []any{}
			for _, raw := range p["paragraphs"].([]any) {
				refs = append(refs, raw.(map[string]any)["id"])
			}
			return map[string]any{"sections": []any{map[string]any{"heading": "Reset settings", "paragraphs": []any{map[string]any{"text": "Delete all saved history.", "inputs": refs}}}}}
		}
		if p["coverage_policy"] != nil {
			reviews++
			coverage := []any{}
			for _, raw := range p["paragraphs"].([]any) {
				coverage = append(coverage, map[string]any{"id": raw.(map[string]any)["id"], "preserved": false, "reason": "The preservation rule is missing"})
			}
			return map[string]any{"checks": []any{map[string]any{"id": "R0", "supported": false, "useful": false}}, "coverage": coverage}
		}
		return map[string]any{"decisions": []any{map[string]any{"id": "A0", "action": "integrate", "section": "S0"}}}
	}, core)
	before := settingsGuide(t, v)
	add()
	if err := v.Flush(context.Background(), "fixture", true); err == nil {
		t.Fatal("unsupported merge entered the knowledge base")
	}
	after := settingsGuide(t, v)
	if after.Revision != before.Revision || knowledge.PageMarkdown(after) != knowledge.PageMarkdown(before) || reviews != 1 {
		t.Fatal("rejected merge changed published knowledge or exceeded bounded repair attempts")
	}
	if err := v.Flush(context.Background(), "fixture", true); err == nil || reviews != 1 {
		t.Fatalf("retry repeated paid rejected work: err=%v reviews=%d", err, reviews)
	}
}

func TestKnowledgeWordTargetNeverTrimsReviewedUsefulMeaning(t *testing.T) {
	core := []string{}
	for i := 0; i < 12; i++ {
		core = append(core, fmt.Sprintf("Partition %d retains %s", i, strings.Repeat("supported history ", 31)))
	}
	detail := "Reconnect credentials and retain " + strings.Repeat("useful provider history ", 60)
	reviews := 0
	v, add := answerIntegrationFixture(t, detail, func(p map[string]any) map[string]any {
		if p["merge_policy"] != nil {
			refs, texts := []any{}, []string{}
			for _, raw := range p["paragraphs"].([]any) {
				v := raw.(map[string]any)
				refs = append(refs, v["id"])
				texts = append(texts, v["text"].(string))
			}
			return map[string]any{"sections": []any{map[string]any{"heading": "Reset settings", "paragraphs": []any{map[string]any{"text": strings.Join(texts, " "), "inputs": refs}}}}}
		}
		if p["coverage_policy"] != nil {
			reviews++
			coverage := []any{}
			for _, raw := range p["paragraphs"].([]any) {
				coverage = append(coverage, map[string]any{"id": raw.(map[string]any)["id"], "preserved": true})
			}
			return map[string]any{"checks": []any{map[string]any{"id": "R0", "supported": true, "useful": true}}, "coverage": coverage}
		}
		return map[string]any{"decisions": []any{map[string]any{"id": "A0", "action": "integrate", "section": "S0"}}}
	}, core)
	add()
	if err := v.Flush(context.Background(), "fixture", true); err != nil {
		t.Fatal(err)
	}
	page := settingsGuide(t, v)
	texts := []string{}
	for _, section := range page.Sections {
		for _, p := range section.Paragraphs {
			texts = append(texts, p.Text)
		}
	}
	text := strings.Join(texts, " ")
	for _, fact := range append(core, detail) {
		if !strings.Contains(text, fact) {
			t.Fatalf("word target discarded useful meaning: %s", fact)
		}
	}
	if reviews != 1 || len(strings.Fields(text)) <= 900 {
		t.Fatalf("preservation review was bypassed: reviews=%d words=%d", reviews, len(strings.Fields(text)))
	}
}

func TestKnowledgeOversizedIntegrationRetainsPublicationWithoutPaidReview(t *testing.T) {
	core := []string{}
	for i := 0; i < 13; i++ {
		core = append(core, fmt.Sprintf("Partition %d: %s", i, strings.Repeat("a", 2500)))
	}
	edits := 0
	v, add := answerIntegrationFixture(t, "Reconnect provider credentials after resetting settings.", func(p map[string]any) map[string]any {
		if p["integration_policy"] != nil || p["merge_policy"] != nil || p["coverage_policy"] != nil {
			edits++
			return map[string]any{}
		}
		ids := []any{}
		for _, raw := range p["paragraphs"].([]any) {
			ids = append(ids, raw.(map[string]any)["id"])
		}
		return map[string]any{"sections": []any{map[string]any{"heading": "Reset settings", "paragraphs": ids[:min(12, len(ids))]}}}
	}, core)
	before := settingsGuide(t, v)
	add()
	if err := v.Flush(context.Background(), "fixture", true); err == nil || !strings.Contains(err.Error(), "safe review bounds") {
		t.Fatalf("oversized input was not stopped before review: %v", err)
	}
	after := settingsGuide(t, v)
	if edits != 0 || after.Revision != before.Revision || knowledge.PageMarkdown(after) != knowledge.PageMarkdown(before) {
		t.Fatal("oversized maintenance purchased review or replaced publication")
	}
}

func TestKnowledgePreservedCoreUsesCurrentConflictMetadata(t *testing.T) {
	updated, inspected := false, false
	oldClaim := ""
	v, add := answerIntegrationFixtureWithUnderstanding(t, "Resetting settings removes saved conversation history.", func(p map[string]any) map[string]any {
		paragraph := p["existing_sections"].([]any)[0].(map[string]any)["paragraphs"].([]any)[0].(map[string]any)
		basis := paragraph["evidence_basis"].(map[string]any)
		inspected = true
		if basis["unresolved_conflict"] != true {
			t.Error("current contradiction was hidden by previous published metadata")
		}
		return map[string]any{"decisions": []any{map[string]any{"id": "A0", "action": "integrate", "section": "S0"}}}
	}, func(response map[string]any) map[string]any {
		if claims, ok := response["claims"].([]any); ok {
			if updated {
				response["relations"] = []any{map[string]any{"from": "new:" + claims[0].(map[string]any)["local_id"].(string), "to": oldClaim, "kind": "contradicts", "reason": "Opposing settings preservation rules"}}
			}
		} else if !updated {
			oldClaim = response["sections"].([]any)[0].(map[string]any)["paragraphs"].([]any)[0].(map[string]any)["claims"].([]any)[0].(string)
		}
		return response
	})
	updated = true
	add()
	if err := v.Flush(context.Background(), "fixture", true); err != nil {
		t.Fatal(err)
	}
	if !inspected {
		t.Fatal("updated guide was not inspected")
	}
}

func settingsGuide(t *testing.T, v *knowledge.Vault) knowledge.Page {
	t.Helper()
	s, err := v.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range s.Pages {
		if p.Kind == "subject" {
			return p
		}
	}
	t.Fatal("settings guide disappeared")
	return knowledge.Page{}
}

func TestKnowledgeKeepsDuplicateResetReportsAsEvidence(t *testing.T) {
	edits := 0
	report := "The settings reset on Pine at revision abc123 kept all saved history."
	v, add := answerIntegrationFixture(t, report, func(p map[string]any) map[string]any {
		edits++
		sections := p["existing_sections"].([]any)
		if len(sections) != 1 || sections[0].(map[string]any)["heading"] != "Reset settings" {
			t.Errorf("editor did not inspect the existing guide: %v", sections)
		}
		return map[string]any{"decisions": []any{map[string]any{"id": "A0", "action": "covered", "section": "S0", "reason": "The procedure is already explained; the report is evidence"}}}
	})
	before := settingsGuide(t, v)
	add()
	if err := v.Flush(context.Background(), "fixture", true); err != nil {
		t.Fatal(err)
	}
	after := settingsGuide(t, v)
	if len(after.Sections) != 1 || len(after.Sections[0].Paragraphs) != 1 || after.Sections[0].Heading != "Reset settings" || after.Sections[0].Paragraphs[0].Text != settingsGuideText {
		t.Fatalf("event report cluttered the durable settings guide: %+v", after.Sections)
	}
	if after.ID != before.ID || len(after.Evidence) != 2 || edits != 1 {
		t.Fatalf("integration lost identity, evidence or editorial checking: id=%s sources=%d edits=%d", after.ID, len(after.Evidence), edits)
	}
	hits, err := v.Search("abc123", 10)
	if err != nil || len(hits) == 0 {
		t.Fatalf("linked report evidence disappeared: %v %v", hits, err)
	}
	for _, h := range hits {
		if h.Page.Kind == "subject" && strings.Contains(knowledge.PageMarkdown(h.Page), "abc123") {
			t.Fatal("transient report remains in guide prose")
		}
	}
}

func TestKnowledgeIntegratesUsefulDetailIntoItsExistingSection(t *testing.T) {
	detail := "Reconnect the model provider after resetting settings because credentials are stored there."
	edits := 0
	v, add := answerIntegrationFixture(t, detail, func(p map[string]any) map[string]any {
		edits++
		return map[string]any{"decisions": []any{map[string]any{"id": "A0", "action": "integrate", "section": "S0", "reason": "An additional condition for this procedure", "text": "Invented replacement must never enter the guide"}}}
	})
	before := settingsGuide(t, v)
	add()
	if err := v.Flush(context.Background(), "fixture", true); err != nil {
		t.Fatal(err)
	}
	after := settingsGuide(t, v)
	if after.ID != before.ID || len(after.Sections) != 1 || after.Sections[0].Heading != "Reset settings" || len(after.Sections[0].Paragraphs) != 2 {
		t.Fatalf("new detail did not update its existing home: %+v", after)
	}
	if after.Sections[0].Paragraphs[0].Text != settingsGuideText || after.Sections[0].Paragraphs[1].Text != detail || len(after.Sections[0].Paragraphs[1].Evidence) == 0 {
		t.Fatalf("integration changed reviewed prose or lost citations: %+v", after.Sections)
	}
	if err := v.Flush(context.Background(), "fixture", true); err != nil {
		t.Fatal(err)
	}
	if edits != 1 {
		t.Fatalf("unchanged guide repeated editorial work: %d", edits)
	}
}

func TestKnowledgeOnlyCreatesSectionsForDistinctDurableKnowledge(t *testing.T) {
	for _, action := range []string{"new_section", "evidence_only"} {
		t.Run(action, func(t *testing.T) {
			v, add := answerIntegrationFixture(t, "Recovery uses the saved credential backup.", func(p map[string]any) map[string]any {
				heading := ""
				if action == "new_section" {
					heading = "Recover credentials"
				}
				return map[string]any{"decisions": []any{map[string]any{"id": "A0", "action": action, "heading": heading}}}
			})
			add()
			if err := v.Flush(context.Background(), "fixture", true); err != nil {
				t.Fatal(err)
			}
			p := settingsGuide(t, v)
			want := 1
			if action == "new_section" {
				want = 2
			}
			if len(p.Sections) != want || len(p.Evidence) != 2 || p.Sections[0].Paragraphs[0].Text != settingsGuideText {
				t.Fatalf("editorial decision changed existing knowledge or lost evidence: %+v", p)
			}
			if action == "new_section" && p.Sections[1].Heading != "Recover credentials" {
				t.Fatalf("new concept heading was not used: %+v", p.Sections)
			}
		})
	}
}

func TestKnowledgeInvalidIntegrationKeepsPublicationAndRetries(t *testing.T) {
	for _, mistake := range []string{"omitted", "unknown section", "repeated answer", "unknown answer", "unknown action"} {
		t.Run(mistake, func(t *testing.T) {
			invalid := true
			v, add := answerIntegrationFixture(t, "Reconnect credentials after the settings reset.", func(p map[string]any) map[string]any {
				d := map[string]any{"id": "A0", "action": "integrate", "section": "S0"}
				decisions := []any{d}
				if invalid {
					switch mistake {
					case "omitted":
						decisions = []any{}
					case "unknown section":
						d["section"] = "S999"
					case "repeated answer":
						decisions = append(decisions, d)
					case "unknown answer":
						d["id"] = "A999"
					case "unknown action":
						d["action"] = "rewrite_everything"
					}
				}
				return map[string]any{"decisions": decisions}
			})
			before := settingsGuide(t, v)
			add()
			if err := v.Flush(context.Background(), "fixture", true); err == nil {
				t.Fatal("invalid editorial plan was accepted")
			}
			after := settingsGuide(t, v)
			if after.Revision != before.Revision || knowledge.PageMarkdown(after) != knowledge.PageMarkdown(before) {
				t.Fatal("failed editorial work changed visible knowledge")
			}
			invalid = false
			if err := v.Flush(context.Background(), "fixture", true); err != nil {
				t.Fatalf("invalid response poisoned the retry: %v", err)
			}
			if len(settingsGuide(t, v).Sections[0].Paragraphs) != 2 {
				t.Fatal("retry lost the useful new detail")
			}
		})
	}
}

func TestKnowledgeHumanOverridesDoNotPurchaseHiddenIntegration(t *testing.T) {
	for _, action := range []string{"edit", "delete"} {
		t.Run(action, func(t *testing.T) {
			edits := 0
			v, add := answerIntegrationFixture(t, "Reconnect credentials after resetting settings.", func(p map[string]any) map[string]any {
				edits++
				return map[string]any{"decisions": []any{map[string]any{"id": "A0", "action": "integrate", "section": "S0"}}}
			})
			before := settingsGuide(t, v)
			var err error
			if action == "edit" {
				err = v.EditPage(before.ID, before.Revision, "My settings procedure.")
			} else {
				err = v.DeletePage(before.ID, before.Revision)
			}
			if err != nil {
				t.Fatal(err)
			}
			add()
			if err := v.Flush(context.Background(), "fixture", true); err != nil {
				t.Fatal(err)
			}
			if edits != 0 {
				t.Fatalf("hidden generated prose purchased %d editorial requests", edits)
			}
			after, err := v.Read(before.ID)
			if action == "delete" {
				if !os.IsNotExist(err) {
					t.Fatalf("deleted guide was restored: %v", err)
				}
			} else if err != nil || knowledge.PageMarkdown(after) != "My settings procedure." || !after.HumanEdited {
				t.Fatalf("human procedure changed: %+v %v", after, err)
			}
		})
	}
}
