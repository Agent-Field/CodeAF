package chatlist

// Same reports whether two listings say the same thing about the chats.
//
// DurableAgo is left out on purpose: it is measured against the directory's
// clock at the moment of the read, so it differs between two reads of an
// unchanged list. DurableAt is the fact it comes from, and it is compared, so a
// turn that landed in between still counts as a change. This is how a client
// tells an unchanged answer without any help from the wire, which carries no
// version or etag.
func Same(a, b []Row) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if timeless(a[i]) != timeless(b[i]) {
			return false
		}
	}
	return true
}

// timeless is a row with the read-time-dependent age taken out.
func timeless(r Row) Row {
	r.DurableAgo = 0
	return r
}
