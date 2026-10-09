// A long paste travels as text the engine already accepts: a tagged block ahead of what the person typed.
// The composer writes it, and the sent bubble reads it back into a card.

/** A paste over this many lines becomes a card instead of filling the field. */
export const PASTE_CARD_LINES = 12;

export type Pasted = { lines: number; text: string };

const BLOCK = /^<pasted-text lines="(\d+)">\n([\s\S]*?)\n<\/pasted-text>(?:\n\n|$)/;

export function countLines(text: string): number {
  return text.replace(/\n$/, '').split('\n').length;
}

export function isLongPaste(text: string): boolean {
  return countLines(text) > PASTE_CARD_LINES;
}

function block(text: string): string {
  const body = text.replace(/\n$/, '');
  return `<pasted-text lines="${countLines(body)}">\n${body}\n</pasted-text>`;
}

export function encodePasted(pastes: string[], typed: string): string {
  return [...pastes.map(block), typed].filter(Boolean).join('\n\n');
}

/** The leading pasted blocks of a sent message, and the text after them. */
export function splitPasted(message: string): { pastes: Pasted[]; rest: string } {
  const pastes: Pasted[] = [];
  let rest = message;
  for (let hit = BLOCK.exec(rest); hit; hit = BLOCK.exec(rest)) {
    pastes.push({ lines: Number(hit[1]), text: hit[2] });
    rest = rest.slice(hit[0].length);
  }
  return { pastes, rest };
}

/**
 * A sent message as one line of reading text: what the person typed, never the
 * raw tags. A message that was only pastes reads as the card does.
 */
export function plainMessage(message: string): string {
  const { pastes, rest } = splitPasted(message);
  if (rest.trim() || pastes.length === 0) return rest;
  const lines = pastes.reduce((sum, paste) => sum + paste.lines, 0);
  return `Pasted text · ${lines} lines`;
}
