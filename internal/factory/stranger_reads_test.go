package factory

import "testing"

func TestStrangerBriefWriterStillWorks(t *testing.T) {
	it := writerJob()
	it.Tier = TierStranger
	if skip := it.BeginBrief(BriefTriggerStep, "stranger-brief", briefNow); skip != "" {
		t.Fatalf("brief refused: %s", skip)
	}
	_, _, kept := it.EndBrief(BriefWritten{Hash: "stranger-brief", Draft: []byte(`{"verdict":"Read the change.","verdict_evidence":[],"blocks":[]}`)}, briefNow)
	if !kept {
		t.Fatalf("brief not saved: %+v", it.BriefWriting)
	}
}
