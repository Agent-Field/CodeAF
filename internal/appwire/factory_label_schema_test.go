package appwire

import "testing"

func TestFactoryLabelEditSchema(t *testing.T) {
 defs := Schema()["$defs"].(map[string]any)
 for _, name := range []string{"in.FactoryPipelineEdit", "in.FactoryNewStep", "in.FactoryStepEdit"} {
  def, ok := defs[name].(map[string]any)
  if !ok { t.Fatalf("missing schema %s", name) }
  if def["properties"].(map[string]any)["label"] == nil { t.Errorf("%s lacks label", name) }
 }
}
