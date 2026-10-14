package main

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// zooTags asks `zoo show <id>` for the component families and value types of
// an app and joins them into the family column ("a,b,v:c").
func zooTags(zoo, id string) string {
	out, err := exec.Command(zoo, "show", id).Output()
	if err != nil {
		return ""
	}
	var rec struct{ Families, Values []string }
	if json.Unmarshal(out, &rec) != nil {
		return ""
	}
	tags := append([]string(nil), rec.Families...)
	for _, v := range rec.Values {
		tags = append(tags, "v:"+v)
	}
	return strings.Join(tags, ",")
}

// stageRecords turns a row into zoo record calls: each stage reached passes,
// the first stage missed fails with the owner-visible text, and a build that
// reached Use with Check passing ends with done pass.
func stageRecords(r row) [][3]string {
	steps := []struct {
		name string
		ok   bool
	}{{"plan", r.Stages.Plan}, {"write", r.Stages.Write}, {"check", r.CheckPass}, {"preview", r.Stages.Preview},
		{"keep", r.Stages.Keep}, {"use", r.Stages.Use}}
	var out [][3]string
	for _, s := range steps {
		if !s.ok {
			return append(out, [3]string{s.name, "fail", failText(r)})
		}
		out = append(out, [3]string{s.name, "pass", ""})
	}
	return append(out, [3]string{"done", "pass", ""})
}

func failText(r row) string {
	text := r.Error
	if text == "" {
		text = "stage not reached (state " + r.State + ")"
	}
	if r.Cause != "" {
		text = "[" + r.Cause + "] " + text
	}
	return text
}

// recordZoo appends every stage of every row to the zoo ledger.
func recordZoo(zoo, run, build, model string, rows []row) error {
	for _, r := range rows {
		for _, rec := range stageRecords(r) {
			args := []string{"record", r.ID, rec[0], rec[1], "--path", "served-bridge", "--run", run + "-" + r.Road,
				"--who", "builder-loop", "--build", build, "--model", model}
			if rec[2] != "" {
				args = append(args, "--error", rec[2])
			}
			if out, err := exec.Command(zoo, args...).CombinedOutput(); err != nil {
				return fmt.Errorf("zoo record %s %s: %v: %s", r.ID, rec[0], err, out)
			}
		}
	}
	return nil
}
