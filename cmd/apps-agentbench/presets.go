package main

import "slices"

// Each task gets an independent seeded app and a fresh thread. The delete
// task starts with Ana already present: it never depends on the create task
// winning a race, or on a model carrying mistakes into the next measurement.
type task struct {
	ID, Text, Operation, Type, Target string
	Set                               map[string]any
}

// Field alternatives account for names chosen by different Builder models.
// They select existing fields only; the bench never grants the agent tools or
// changes a schema to make an unsupported request succeed.
var presets = map[string][]task{
	"crm": {
		{"add-contact", "Add a contact Ana Lima at Acme, email ana@acme.test.", "create", "contact", "Ana Lima", map[string]any{"email": "ana@acme.test", "company": "@company"}},
		{"quiet-deals", "Which deals have been quiet for two weeks? Name them and say when we last contacted them.", "read", "deal", "", nil},
		{"pipeline", "Summarise the pipeline by stage, including deal counts and values.", "read", "deal", "", nil},
		{"win-acme", "Move the Acme deal to Won.", "update", "deal", "Acme deal", map[string]any{"stage|status": "Won"}},
		{"delete-contact", "Delete the test contact Ana Lima, email ana@acme.test, that I added earlier.", "delete", "contact", "Ana Lima", nil},
	},
	"tracker": {
		{"add-issue", "Add an issue titled Fix login timeout in the Website project, priority High.", "create", "issue", "Fix login timeout", map[string]any{"priority": "High", "project": "@project"}},
		{"overdue", "Which issues are overdue? Include their assignee and due date.", "read", "issue", "", nil},
		{"status-summary", "Summarise our issues by status, with counts and titles.", "read", "issue", "", nil},
		{"finish-login", "Mark the Login bug issue as Done.", "update", "issue", "Login bug", map[string]any{"status": "Done"}},
		{"assign-login", "Assign the Login bug issue to me.", "update", "issue", "Login bug", map[string]any{"assignee": "me"}},
	},
	"inventory": {
		{"add-product", "Add a product Test Bell, SKU BELL-TEST, price 12, stock 5, reorder level 2, supplier Bike Supply.", "create", "product", "Test Bell", map[string]any{"sku": "BELL-TEST", "price": float64(12), "stock|quantity|stock_quantity|in_stock": float64(5), "reorder_level|reorder_point": float64(2), "supplier": "@supplier"}},
		{"low-stock", "Which products are at or below their reorder level? Include stock and reorder levels.", "read", "product", "", nil},
		{"stock-value", "What is the total retail value of our stock? Break it down by product.", "read", "product", "", nil},
		{"set-stock", "We counted ten Road Tyres in stock. Set their stock to 10.", "update", "product", "Road Tyre", map[string]any{"stock|quantity|stock_quantity|in_stock": float64(10)}},
		{"set-price", "Set the retail price of Road Tyre to 12.", "update", "product", "Road Tyre", map[string]any{"price": float64(12)}},
	},
	"booking": {
		{"add-client", "Add a client Ana Lima, email ana@clinic.test, phone +1 416 555 0123.", "create", "client", "Ana Lima", map[string]any{"email": "ana@clinic.test", "phone": "+1 416 555 0123"}},
		{"upcoming", "List upcoming bookings, with the client's name, service and appointment time.", "read", "booking", "", nil},
		{"booking-summary", "Summarise bookings by status, with counts and client names.", "read", "booking", "", nil},
		{"cancel-booking", "Cancel the Ana consultation booking.", "update", "booking", "Ana consultation", map[string]any{"status": "Cancelled"}},
		{"update-email", "Change the email for client Jo Silva to jo.new@clinic.test.", "update", "client", "Jo Silva", map[string]any{"email": "jo.new@clinic.test"}},
	},
}

func presetNames() []string {
	names := make([]string, 0, len(presets))
	for name := range presets {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}
