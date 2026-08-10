package sanitize

import "strings"

// writeSGR copies an already-verified SGR sequence (the full "ESC '[' ...
// 'm'" bytes, exactly as scanCSIBody validated it) to out, remapping any
// ANSI-16 color parameters through t along the way. For the identity table
// — every caller until Wave 2's token layer exists — this is a single
// array comparison and then a raw copy: no parsing, no allocation beyond
// the output buffer already in use.
func writeSGR(out *strings.Builder, seq string, t Table) {
	if t.IsIdentity() {
		out.WriteString(seq)
		return
	}
	// seq is "ESC '[' <params> 'm'"; params is everything between them.
	params := seq[2 : len(seq)-1]
	out.WriteString(seq[:2])
	writeRemappedParams(out, params, t)
	out.WriteByte('m')
}

// writeRemappedParams rewrites the semicolon-separated SGR parameter list,
// leaving every token untouched except a bare 16-color code (30-37, 40-47,
// 90-97, 100-107), which is looked up in t and re-emitted in the same
// family (foreground stays foreground, background stays background) at
// whatever index the table names — including across the standard/bright
// boundary. The extended-color introducers 38 and 48 are recognized and
// their whole group (";5;N" or ";2;r;g;b") is copied through opaquely: the
// palette this wave has nothing to say about 256-color or truecolor tool
// output, only the classic 16.
func writeRemappedParams(out *strings.Builder, params string, t Table) {
	first := true
	for len(params) > 0 {
		var token string
		if idx := strings.IndexByte(params, ';'); idx >= 0 {
			token, params = params[:idx], params[idx+1:]
		} else {
			token, params = params, ""
		}
		if !first {
			out.WriteByte(';')
		}
		first = false

		code, ok := parseSGRCode(token)
		if !ok {
			out.WriteString(token)
			continue
		}
		switch code {
		case 38, 48:
			// Extended color: copy this token and its whole argument
			// group through untouched, then fall out to the normal loop
			// for whatever (if anything) follows the group.
			out.WriteString(token)
			params = copyExtendedColorGroup(out, params)
			continue
		}
		if newCode, remapped := remapSGRCode(code, t); remapped {
			writeSGRCode(out, newCode)
			continue
		}
		out.WriteString(token)
	}
}

// copyExtendedColorGroup copies the remainder of a 38/48 group (the ";5;N"
// or ";2;r;g;b" that follows the introducer already written by the
// caller) straight through, unparsed, and returns whatever params is left
// after the group. It never remaps these tokens — a bare "5" or "2" here
// is a color-space selector, not an ANSI-16 code, and must not collide
// with this package's remap table.
func copyExtendedColorGroup(out *strings.Builder, params string) string {
	// Peek the selector without consuming params permanently in case there
	// turns out to be nothing there (a bare "38" with no follow-up, which
	// is malformed but must not panic).
	selector, rest, hasSelector := cutToken(params)
	if !hasSelector {
		return params
	}
	switch selector {
	case "5":
		out.WriteByte(';')
		out.WriteString(selector)
		token, rest2, ok := cutToken(rest)
		if ok {
			out.WriteByte(';')
			out.WriteString(token)
			rest = rest2
		}
		return rest
	case "2":
		out.WriteByte(';')
		out.WriteString(selector)
		for range 3 {
			token, rest2, ok := cutToken(rest)
			if !ok {
				break
			}
			out.WriteByte(';')
			out.WriteString(token)
			rest = rest2
		}
		return rest
	default:
		// Not a recognized selector; treat 38/48 as a bare (unremapped)
		// token and let the caller's loop resume on the rest normally.
		return params
	}
}

// cutToken splits the next ';'-delimited token off params, reporting
// whether there was anything to cut.
func cutToken(params string) (token, rest string, ok bool) {
	if params == "" {
		return "", "", false
	}
	if idx := strings.IndexByte(params, ';'); idx >= 0 {
		return params[:idx], params[idx+1:], true
	}
	return params, "", true
}

// parseSGRCode parses a small non-negative decimal token (SGR codes never
// exceed three digits) without pulling in strconv's broader error surface.
// An empty token (two consecutive ';', or a leading one — both mean "0" in
// real SGR grammar) is reported as not-ok so the caller copies it through
// unchanged rather than guessing.
func parseSGRCode(token string) (int, bool) {
	if token == "" || len(token) > 3 {
		return 0, false
	}
	n := 0
	for i := 0; i < len(token); i++ {
		c := token[i]
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int(c-'0')
	}
	return n, true
}

// remapSGRCode maps a literal SGR color code through t, if it is one of
// the 16 classic colors in any of their four families (30-37/40-47
// standard fg/bg, 90-97/100-107 bright fg/bg). Anything else (bold,
// underline, reset, ...) is reported not-remapped so the caller leaves it
// alone.
func remapSGRCode(code int, t Table) (int, bool) {
	family, index, ok := decomposeSGRCode(code)
	if !ok {
		return 0, false
	}
	newIndex := t[index]
	return composeSGRCode(family, newIndex), true
}

type sgrFamily int

const (
	sgrForeground sgrFamily = iota
	sgrBackground
)

func decomposeSGRCode(code int) (sgrFamily, int, bool) {
	switch {
	case code >= 30 && code <= 37:
		return sgrForeground, code - 30, true
	case code >= 90 && code <= 97:
		return sgrForeground, code - 90 + 8, true
	case code >= 40 && code <= 47:
		return sgrBackground, code - 40, true
	case code >= 100 && code <= 107:
		return sgrBackground, code - 100 + 8, true
	default:
		return 0, 0, false
	}
}

func composeSGRCode(family sgrFamily, index int) int {
	// A caller-supplied Table is trusted plumbing (Wave 2's token layer,
	// not untrusted input), but defensively clamp anyway: an out-of-range
	// entry falls back to the nearest valid color rather than emitting a
	// nonsense SGR code.
	if index < 0 {
		index = 0
	}
	if index > 15 {
		index = 15
	}
	bright := index >= 8
	base := index
	if bright {
		base = index - 8
	}
	switch {
	case family == sgrForeground && !bright:
		return 30 + base
	case family == sgrForeground && bright:
		return 90 + base
	case family == sgrBackground && !bright:
		return 40 + base
	default: // background, bright
		return 100 + base
	}
}

func writeSGRCode(out *strings.Builder, code int) {
	// SGR codes handled here are always 2-3 digits (30-107); a tiny
	// manual itoa avoids pulling in strconv on the render hot path.
	switch {
	case code >= 100:
		out.WriteByte(byte('0' + code/100))
		code %= 100
		out.WriteByte(byte('0' + code/10))
		out.WriteByte(byte('0' + code%10))
	case code >= 10:
		out.WriteByte(byte('0' + code/10))
		out.WriteByte(byte('0' + code%10))
	default:
		out.WriteByte(byte('0' + code))
	}
}
