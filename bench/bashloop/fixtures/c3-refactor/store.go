// Package inventory prices a small shop's goods, orders and reports.
package inventory

// Item is one thing the shop sells.
type Item struct {
	Name       string
	Price      float64
	Discounted bool
}

// Order is one purchase: the indexes of the items bought, in order.
type Order struct {
	ID    string
	Items []int
}

// Store holds the shop's items and the orders placed against them.
type Store struct {
	Items  []Item
	Orders []Order
}
