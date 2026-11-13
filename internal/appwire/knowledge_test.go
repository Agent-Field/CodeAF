package appwire

import (
	"encoding/json"
	"testing"
)

func TestLibraryMethodsAreAbsentOnAnOlderEngine(t *testing.T) {
	for _, name := range []string{"knowledge.snapshot", "knowledge.read", "knowledge.search", "knowledge.recall", "knowledge.edit", "knowledge.delete", "knowledge.build"} {
		m, ok := methods[name]
		if !ok {
			t.Fatalf("Library method %s is not registered", name)
		}
		if _, err := m.call(&fakeEngine{}, json.RawMessage(`{"id":"guide","revision":"r1","query":"relay","question":"port?"}`)); codeOf(err) != CodeUnknownMethod {
			t.Fatalf("%s: want unavailable, got %v", name, err)
		}
	}
}

func TestLibraryModelTravelsThroughTheExistingSettingsDoor(t *testing.T) {
	e := &fakeEngine{}
	_, err := methods["settings.set"].call(e, json.RawMessage(`{"knowledge_model":"deepseek/deepseek-v4.1-flash"}`))
	if err != nil || e.patch.KnowledgeModel == nil || *e.patch.KnowledgeModel != "deepseek/deepseek-v4.1-flash" {
		t.Fatalf("knowledge model patch was lost: %v", err)
	}
	if _, err = methods["settings.set"].call(e, json.RawMessage(`{"knowledge_model":true}`)); codeOf(err) != CodeBadParams {
		t.Fatal("a non-model value crossed the settings boundary")
	}
}
