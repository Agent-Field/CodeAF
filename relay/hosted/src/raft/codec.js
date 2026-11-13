// This module only preserves the Go core's canonical boundary. It grants no
// authority and implements no directory or consensus decisions.
const maxUint64 = (1n << 64n) - 1n;
const encoder = new TextEncoder();

// uint64 keeps counters out of JavaScript's rounded number domain. Callers
// retain the original decimal string when handing a command to the core.
export function uint64(value) {
  if (typeof value !== 'string' || value.length > 20 || !/^(0|[1-9][0-9]*)$/.test(value)) throw new TypeError('invalid uint64');
  const n = BigInt(value);
  if (n > maxUint64) throw new TypeError('invalid uint64');
  return n;
}

function quoted(s) {
  for (const c of s) {
    const cp = c.codePointAt(0);
    if (cp >= 0xd800 && cp <= 0xdfff) throw new TypeError('invalid Unicode');
  }
  return JSON.stringify(s).replace(/[<>&\u2028\u2029]/g,
    (c) => `\\u${c.charCodeAt(0).toString(16).padStart(4, '0')}`);
}

// Go sorts map keys by UTF-8 bytes. JavaScript's default UTF-16 sort differs
// for supplementary characters, and object enumeration reorders integer keys.
function compareKeys(a, b) {
  const x = encoder.encode(a), y = encoder.encode(b);
  for (let i = 0; i < Math.min(x.length, y.length); i++) if (x[i] !== y[i]) return x[i] - y[i];
  return x.length - y.length;
}

export function encodeCanonical(value) {
  if (value === null) return 'null';
  switch (typeof value) {
    case 'string': return quoted(value);
    case 'boolean': return String(value);
    case 'bigint':
      if (value < 0n || value > maxUint64) throw new TypeError('invalid uint64');
      return quoted(String(value));
    case 'number':
      if (!Number.isSafeInteger(value) || Object.is(value, -0)) throw new TypeError('invalid integer');
      return String(value);
    case 'object':
      if (Array.isArray(value)) return `[${Array.from(value, encodeCanonical).join(',')}]`;
      if (Object.getPrototypeOf(value) !== Object.prototype && Object.getPrototypeOf(value) !== null) {
        throw new TypeError('invalid object');
      }
      return `{${Object.keys(value).sort(compareKeys).map((k) => `${quoted(k)}:${encodeCanonical(value[k])}`).join(',')}}`;
    default: throw new TypeError('invalid canonical value');
  }
}

export function decodeCanonical(text) {
  const value = JSON.parse(text);
  if (encodeCanonical(value) !== text) throw new TypeError('invalid canonical encoding');
  return value;
}
