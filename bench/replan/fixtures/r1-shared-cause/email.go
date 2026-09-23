package contacts

// SameEmail reports whether two addresses name the same mailbox.
func SameEmail(a, b string) bool {
	return canonical(a) == canonical(b)
}
