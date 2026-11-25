package check_test

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/check"
	"github.com/Agent-Field/codeaf/internal/apps/schema"
)

func TestSampleReportsTheRowWithAnInvalidID(t *testing.T) {
	def := schema.Definition{Files: map[string][]byte{
		schema.SampleFile: []byte(`{"records":{"company":[{"id":"company-one","name":"Example"}]}}`),
	}}
	_, err := check.Sample(def)
	if err == nil || !strings.Contains(err.Error(), "company row 1 needs a 26-character ULID") {
		t.Fatalf("sample problem = %v", err)
	}
}

func TestSampleReportsIDsSharedByDifferentTypes(t *testing.T) {
	def := schema.Definition{Files: map[string][]byte{
		schema.SampleFile: []byte(`{"records":{"company":[{"id":"00000000000000000000000001"}],"contact":[{"id":"00000000000000000000000001"}]}}`),
	}}
	_, err := check.Sample(def)
	if err == nil || !strings.Contains(err.Error(), "Example contact row 1 reuses id") {
		t.Fatalf("sample problem = %v", err)
	}
}
