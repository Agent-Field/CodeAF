// The Home notes card (Components, "Notes (plain for now)") and Places 6e: instructions are plain prose on the Home.
// A note is not a second store. The engine keeps one instructions string, so a committed note becomes another paragraph of it.

/** The notes line, spelled the way the component specimen spells it, including the ellipsis. */
export const notePlaceholder = 'Add a note, or drop a file or link…';

const chatDrag = 'application/x-codeaf-chat';
const placeDrag = 'application/x-codeaf-place';

/** The card is on the page when there is prose, or when Write instructions opened an empty one. */
export function instructionsVisible(instructions: string | undefined, opened: boolean): boolean {
  return opened || !!instructions?.trim();
}

/**
 * What a blur should write. The same text writes nothing. Whitespace typed into an empty card writes nothing.
 * Clearing real prose writes '' so the engine drops it and the card can leave the page.
 */
export function instructionWrite(previous: string, next: string): string | undefined {
  if (next === previous) return undefined;
  if (!previous.trim() && !next.trim()) return undefined;
  return next.trim() ? next : '';
}

/** A note committed on blur or Enter. Blank adds nothing. The paragraph break matches how the store joins two places' prose. */
export function withNote(instructions: string, note: string): string | undefined {
  const extra = note.trim();
  if (!extra) return undefined;
  const base = instructions.trim();
  return base ? `${base}\n\n${extra}` : extra;
}

export type DroppedSource = { kind: 'file' | 'url'; ref: string };

/** A file or an http(s) link. A chat or a place dragged across the card belongs to the tiles, so it is refused here. */
export function droppedSource(input: { types: readonly string[]; files: readonly { name: string; path?: string }[]; text: string }): DroppedSource | undefined {
  if (input.types.includes(chatDrag) || input.types.includes(placeDrag)) return undefined;
  const file = input.files.find(item => (item.path || item.name).trim());
  if (file) return { kind: 'file', ref: (file.path || file.name).trim() };
  const line = input.text.split(/[\r\n]/).map(part => part.trim()).find(part => part && !part.startsWith('#'));
  if (line && /^https?:\/\//i.test(line)) return { kind: 'url', ref: line };
  return undefined;
}

/** Highlight the card only for a drop this field can take. text/plain is read on drop, not while the drag is still moving. */
export function dropHighlight(types: readonly string[]): boolean {
  if (types.includes(chatDrag) || types.includes(placeDrag)) return false;
  return types.includes('Files') || types.includes('text/uri-list');
}
