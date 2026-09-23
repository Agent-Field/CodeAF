package contacts

// Book is a phone book keyed by name.
type Book map[string]string

// NewBook builds a phone book from name to phone number.
func NewBook(entries map[string]string) Book {
	book := Book{}
	for name, phone := range entries {
		book[canonical(name)] = phone
	}
	return book
}

// Lookup finds the phone number filed under a name.
func (b Book) Lookup(name string) (string, bool) {
	phone, ok := b[canonical(name)]
	return phone, ok
}
