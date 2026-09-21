package inventory

// OrderTotal is what one order costs with tax added to every line.
func (s *Store) OrderTotal(o Order) float64 {
	total := 0.0
	for _, index := range o.Items {
		item := s.Items[index]
		rate := 0.0825
		if item.Discounted {
			rate = 0.05
		}
		total += item.Price * (1 + rate)
	}
	return total
}
