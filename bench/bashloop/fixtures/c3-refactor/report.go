package inventory

// TaxLine is the tax the shop collected across every item in stock, the
// number the quarterly report leads with.
func (s *Store) TaxLine() float64 {
	tax := 0.0
	for _, item := range s.Items {
		rate := 0.0825
		if item.Discounted {
			rate = 0.05
		}
		tax += item.Price * rate
	}
	return tax
}
