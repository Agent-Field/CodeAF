package main

import "sort"

// presets are paragraphs written the way an owner would type them: a few
// loose sentences, no schema words.
var presets = map[string]string{
	"crm": "A CRM for our sales pipeline: companies, deals and contacts. The agent drafts follow-ups " +
		"when a deal goes quiet for a week; I approve every email before it sends.",
	"tracker": "An issue tracker for our small product team. Issues have a title, a description, a priority, " +
		"a status (open, in progress, in review, done), an assignee and a due date, and belong to a project. " +
		"I want a board by status, a list I can filter by assignee, and the agent should summarise what " +
		"changed this week.",
	"inventory": "Inventory for my bike shop. Products with SKU, name, category, supplier, price, cost and how many " +
		"we have in stock, plus a reorder level. Suppliers with contact details. I log stock movements " +
		"(received, sold, adjusted). Show me what is running low and let the agent draft a reorder " +
		"email to the supplier, which I approve before it goes out.",
	"booking": "Appointments for my physiotherapy clinic. Clients with phone and email, services with a " +
		"duration and price, and bookings that tie a client to a service at a date and time with a " +
		"status (booked, confirmed, done, cancelled, no-show). I want to see upcoming bookings by day, " +
		"and the agent should draft a reminder to each client the day before, which I approve.",
}

func presetNames() []string {
	names := make([]string, 0, len(presets))
	for name := range presets {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
