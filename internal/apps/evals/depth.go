package evals

import (
	"encoding/json"
	"path"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/schema"
)

// Depth is a static inventory of the saved draft. The simulator will measure
// whether these pieces actually work together.
type Depth struct {
	Score           int `json:"score"`
	AISteps         int `json:"ai_steps"`
	Triggers        int `json:"triggers"`
	GatedWritebacks int `json:"gated_writebacks"`
	SampleRows      int `json:"sample_rows"`
}

var staticDepthCriteria = [...]func(Depth) bool{
	func(d Depth) bool { return d.AISteps >= 2 },
	func(d Depth) bool { return d.Triggers > 0 },
	func(d Depth) bool { return d.GatedWritebacks > 0 },
	func(d Depth) bool { return d.SampleRows >= 30 },
}

const maxStaticDepth = len(staticDepthCriteria)

func measureDepth(files map[string]string) Depth {
	var depth Depth
	runs := make(map[string]schema.Run)
	for name, body := range files {
		ext := path.Ext(name)
		if path.Dir(name) != "runs" || (ext != ".yaml" && ext != ".yml") {
			continue
		}
		run, err := schema.ParseRun([]byte(body))
		if err == nil && run.Run != "" && len(run.Steps) > 0 && name == "runs/"+run.Run+ext {
			runs[run.Run] = run
			depth.countSteps(run.Steps)
		}
	}
	if app, err := schema.ParseApp([]byte(files[schema.AppFile])); err == nil {
		for _, trigger := range app.Triggers {
			if _, ok := runs[trigger.Run]; ok && strings.TrimSpace(trigger.On) != "" {
				depth.Triggers++
			}
		}
	}
	var sample schema.Sample
	if json.Unmarshal([]byte(files[schema.SampleFile]), &sample) == nil {
		for _, rows := range sample.Records {
			depth.SampleRows += len(rows)
		}
	}
	for _, criterion := range staticDepthCriteria {
		if criterion(depth) {
			depth.Score++
		}
	}
	return depth
}

func (d *Depth) countSteps(steps []schema.Step) {
	for _, step := range steps {
		if (step.AI != nil && step.AI.Prompt != "" && len(step.AI.Output) != 0) || (step.Agent != "" && step.Prompt != "") {
			d.AISteps++
		}
		if step.Tool != "" && step.Gate != nil && step.Gate.Approval != "" {
			d.GatedWritebacks++
		}
		if step.Loop != nil {
			d.countSteps(step.Steps)
		}
	}
}
