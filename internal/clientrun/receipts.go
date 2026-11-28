package clientrun

import "sort"

// Receipts returns retained replay watermarks and in-flight calls, including
// uncertain dispatches. Chat history belongs to Log, not this bounded set.
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
