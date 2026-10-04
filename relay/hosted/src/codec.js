// Bytes to text and back, the few ways the relay needs, so no module keeps its own copy.
export const hex = (bytes) => [...new Uint8Array(bytes)].map((b) => b.toString(16).padStart(2, '0')).join('');

export const sha256 = async (bytes) => new Uint8Array(await crypto.subtle.digest('SHA-256', bytes));

/** base64 is standard base64 with padding, the encoding of a Go []byte in JSON. */
export const base64 = (bytes) => btoa(String.fromCharCode(...bytes));

/** fromBase64Url decodes unpadded or padded base64url text, or answers null for text that is not. */
export function fromBase64Url(text) {
  if (!/^[A-Za-z0-9_-]*$/.test(text)) return null;
  try {
    return Uint8Array.from(atob(text.replace(/-/g, '+').replace(/_/g, '/')), (c) => c.charCodeAt(0));
  } catch {
    return null; // a length no base64 text can have
  }
}
