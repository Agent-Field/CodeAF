package serve

import (
	"reflect"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
)

// THE PIPELINE ANSWER SAYS WHICH LINES ARE THE FACTORY'S: a read the factory's
// rules added to carries them as house_rules beside the merged lists, and a
// read with none says nothing about them.
func TestThePipelineAnswerNamesTheFactorysOwnRules(t *testing.T) {
	house := factory.HouseRules{Rules: []string{"no new dependencies without asking"}, AskFirst: []string{"touch auth/"}, Never: []string{}}
	p := wirePipeline(pipeline.WithHouseRules(pipeline.Defaults(), house))
	if p.HouseRules == nil || !reflect.DeepEqual(*p.HouseRules, house) {
		t.Fatalf("house rules %#v", p.HouseRules)
	}
	if p.Start == nil || !reflect.DeepEqual(p.Start.AskFirst, []string{"touch auth/"}) {
		t.Fatalf("start %#v", p.Start)
	}
	if bare := wirePipeline(pipeline.Defaults()); bare.HouseRules != nil {
		t.Fatalf("a read with no house rules named some: %#v", bare.HouseRules)
	}
}
