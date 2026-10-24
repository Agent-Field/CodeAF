package clientrun

import "sort"

// Receipts returns the executor's durable call history, including calls whose
// outcome is uncertain after an interrupted dispatch. It contains no keys.
func (r *Runner) Receipts() []Receipt {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Receipt, 0, len(r.state.Receipts))
	for _, receipt := range r.state.Receipts {
		out = append(out, receipt)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Client == out[j].Client {
			return out[i].Number < out[j].Number
		}
		return out[i].Client < out[j].Client
	})
	return out
}
