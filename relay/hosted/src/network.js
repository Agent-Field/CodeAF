// networkOf names the network a caller's address belongs to, so a per-address limit cannot be dodged
// by moving within one's own block. A home or office is given a whole /64 of IPv6 addresses to
// rotate through, so an IPv6 address counts as its /64 prefix; an IPv4 address is one network by
// itself; an IPv4-mapped IPv6 address (::ffff:a.b.c.d) is that IPv4 address; and text that is no
// address at all is its own network, unchanged.

const GROUP = /^[0-9a-f]{1,4}$/;
const OCTET = /^(0|[1-9]\d{0,2})$/;

/** networkOf answers the string a limiter counts a caller by. */
export function networkOf(address) {
  const groups = groupsOf(address);
  if (!groups) return address;
  const mapped = groups.slice(0, 5).every((g) => g === 0) && groups[5] === 0xffff;
  if (mapped) return [groups[6] >> 8, groups[6] & 255, groups[7] >> 8, groups[7] & 255].join('.');
  return groups.slice(0, 4).map((g) => g.toString(16)).join(':') + '::/64';
}

// groupsOf reads an IPv6 address (with or without a zone id, compressed or whole, with or without a
// dotted IPv4 tail) as its eight 16-bit groups, and answers null for anything else, IPv4 included.
function groupsOf(address) {
  const text = address.split('%')[0].toLowerCase();
  if (!text.includes(':')) return null;
  const tail = dottedTail(text);
  if (tail === null) return null;
  const halves = tail.split('::');
  if (halves.length > 2) return null;
  const [head, rest] = halves.map((half) => (half ? half.split(':') : []));
  const missing = 8 - head.length - (rest?.length ?? 0);
  if (halves.length === 1 ? missing !== 0 : missing < 1) return null;
  const words = [...head, ...Array(halves.length === 2 ? missing : 0).fill('0'), ...(rest ?? [])];
  return words.every((w) => GROUP.test(w)) ? words.map((w) => parseInt(w, 16)) : null;
}

// dottedTail rewrites a trailing dotted IPv4 (as in ::ffff:192.0.2.1) as its two hex groups, and
// answers null when that tail is not a valid IPv4 address.
function dottedTail(text) {
  const at = text.lastIndexOf(':') + 1;
  const last = text.slice(at);
  if (!last.includes('.')) return text;
  const octets = last.split('.');
  if (octets.length !== 4 || !octets.every((o) => OCTET.test(o) && Number(o) < 256)) return null;
  const [a, b, c, d] = octets.map(Number);
  return text.slice(0, at) + ((a << 8) | b).toString(16) + ':' + ((c << 8) | d).toString(16);
}
