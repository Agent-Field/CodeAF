package inventory

// ItemPrice is what one item costs a customer with tax added.
func (s *Store) ItemPrice(i Item) float64 {
	rate := 0.0825
	if i.Discounted {
		rate = 0.05
	}
	return i.Price * (1 + rate)
}
