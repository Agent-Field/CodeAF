package session

import "sort"

// ── WHICH MODELS THE FACTORY'S JOBS USED ────────────────────────────────────
//
// Every call made for a factory job carries the job on its ledger row
// ([UsageLine.FactoryJob]), so the factory's spending by model is a fold of
// this machine's ledger: no transcript is opened. It counts only what this
// machine ran; a job run on another computer is on that computer's ledger.

// FactoryModelUsage is one model's calls for factory jobs. Priced counts the
// rows that carried dollars, Unpriced the rows that carried none (a plan, a
// local model, a model with no published price) and Unbilled the rows whose
// provider receipt never came. USD is only the priced rows' dollars. Billing
// is the one way every row of the model said it was paid ("metered",
// "included"), and "" when the rows said nothing or disagreed.
type FactoryModelUsage struct {
	Model    string
	Billing  string
	Calls    int
	Input    int64
	Output   int64
	USD      float64
	Priced   int
	Unpriced int
	Unbilled int
}

// FactoryUsage is the factory jobs' usage over a range of local days: all
// jobs by model, and each job, by its ledger name, by model.
type FactoryUsage struct {
	Models []FactoryModelUsage
	Jobs   map[string][]FactoryModelUsage
}

// FactoryUsageByModel folds the factory-job rows of lines whose local day is
// within from..to (ISO days, both included). Models are sorted by dollars,
// then tokens, then id.
func FactoryUsageByModel(lines []UsageLine, from, to string) FactoryUsage {
	all := modelFold{}
	jobs := map[string]modelFold{}
	for _, line := range lines {
		if line.FactoryJob == "" || line.Day < from || line.Day > to {
			continue
		}
		all.add(line)
		if jobs[line.FactoryJob] == nil {
			jobs[line.FactoryJob] = modelFold{}
		}
		jobs[line.FactoryJob].add(line)
	}
	out := FactoryUsage{Models: all.sorted(), Jobs: make(map[string][]FactoryModelUsage, len(jobs))}
	for job, fold := range jobs {
		out.Jobs[job] = fold.sorted()
	}
	return out
}

// modelFold is one fold's models by id, with the billing words each said.
type modelFold map[string]*modelTally

type modelTally struct {
	FactoryModelUsage
	billings map[string]bool
}

func (f modelFold) add(line UsageLine) {
	t := f[line.Model]
	if t == nil {
		t = &modelTally{FactoryModelUsage: FactoryModelUsage{Model: line.Model}, billings: map[string]bool{}}
		f[line.Model] = t
	}
	t.Calls += max(line.Calls, 1)
	t.Input += int64(line.Input)
	t.Output += int64(line.Output)
	t.billings[line.Billing] = true
	switch {
	case line.Unbilled:
		t.Unbilled++
	case line.USD > 0:
		t.USD += line.USD
		t.Priced++
	default:
		t.Unpriced++
	}
}

func (f modelFold) sorted() []FactoryModelUsage {
	out := make([]FactoryModelUsage, 0, len(f))
	for _, t := range f {
		u := t.FactoryModelUsage
		if len(t.billings) == 1 {
			for word := range t.billings {
				u.Billing = word
			}
		}
		out = append(out, u)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.USD != b.USD {
			return a.USD > b.USD
		}
		if a.Input+a.Output != b.Input+b.Output {
			return a.Input+a.Output > b.Input+b.Output
		}
		return a.Model < b.Model
	})
	return out
}
