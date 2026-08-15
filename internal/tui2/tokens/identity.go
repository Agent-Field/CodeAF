package tokens

// Identity assignment (5.16): each top-level task gets a stable pastel accent
// hashed from its id, and no two adjacent rail cards share one.
//
// Two properties matter and they pull against each other. STABILITY: a task's
// color must not change because a sibling appeared, or the rail becomes a
// disco. DISTINCTNESS: two neighbours must never share, or the peripheral
// answer to "which room am I in" is wrong. The resolution below keeps
// stability as the default and spends it only where a collision actually
// occurs, one card at a time.

// IdentityFor is the stable hash: the same task id always gets the same hue,
// forever, on every machine. FNV-1a over the id bytes — cheap, allocation-free,
// and well-distributed over eight buckets for the id shapes we mint (ULIDs,
// slugs, "wisp-parity").
func IdentityFor(taskID string) Token {
	return Identity0 + Token(identityHash(taskID)%IdentityCount)
}

// IdentityNext is the streaming form a rail render wants: the identity for
// taskID given the identity already drawn on the row above it. It returns
// [IdentityFor] unless that would collide with prev, in which case it steps by
// an odd, id-derived amount — odd so the step can never be a no-op modulo
// eight, and id-derived so the same collision always resolves the same way.
//
// Passing a prev that is not an identity token (the top row, or a row that has
// no identity) means "no neighbour above".
func IdentityNext(taskID string, prev Token) Token {
	h := identityHash(taskID) % IdentityCount
	if Identity0+Token(h) == prev {
		h = (h + identityStep(taskID)) % IdentityCount
	}
	return Identity0 + Token(h)
}

// AssignIdentities walks a rail's rows in display order and returns one
// identity per row, guaranteeing no two ADJACENT rows share. Non-adjacent
// repeats are expected and fine — eight hues cannot label forty tasks, and
// pretending otherwise would be the lie.
//
// The walk is deterministic: the same slice always produces the same
// assignment, and a row's color depends only on its own id and the row above
// it, so inserting a card can recolor at most the cards below the insertion
// that were already colliding.
func AssignIdentities(taskIDs []string) []Token {
	out := make([]Token, len(taskIDs))
	prev := Token(tokenCount) // "no neighbour above"
	for i, id := range taskIDs {
		out[i] = IdentityNext(id, prev)
		prev = out[i]
	}
	return out
}

// identityHash is FNV-1a/64. Written out rather than imported so the package
// stays standard-library-only in spirit and allocation-free in fact.
func identityHash(s string) uint64 {
	const (
		offset = 14695981039346656037
		prime  = 1099511628211
	)
	h := uint64(offset)
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= prime
	}
	return h
}

// identityStep derives an odd step in {1,3,5,7} from a second, independent
// slice of the same hash. Odd is the whole trick: gcd(odd, 8) == 1, so the step
// always lands somewhere else on the wheel, and one nudge is always enough
// because a row has exactly one neighbour above it.
func identityStep(s string) uint64 {
	return ((identityHash(s)>>32)&0x3)*2 + 1
}
