/** The organiser owns these wire answers; the renderer validates them before drawing. */
export type Guess = { place: string; name: string; confidence: number; others?: string[] };
export type Lens = { name: string; axis: string; value: string; chats: string[] };
export const GUESS_DEBOUNCE_MS = 300;
export const GUESS_BUDGET_MS = 20;
export const NOT_UNDERSTOOD = "I did not understand that; try 'by month' or 'by project'";
const object = (value: unknown): value is Record<string, unknown> => !!value && typeof value === 'object' && !Array.isArray(value);
export function readGuess(value: unknown): Guess | undefined {
  if (!object(value) || typeof value.place !== 'string' || !value.place || typeof value.name !== 'string' || !value.name.trim()
    || !Number.isInteger(value.confidence) || Number(value.confidence) < 0 || Number(value.confidence) > 100
    || (value.others !== undefined && (!Array.isArray(value.others) || value.others.length > 2 || !value.others.every(id => typeof id === 'string')))) return undefined;
  return value as Guess;
}
export function readLenses(value: unknown): Lens[] {
  if (!object(value) || !Array.isArray(value.lenses)) throw new Error('Could not read the views for this place.');
  return value.lenses.map(lens => {
    if (!object(lens) || typeof lens.name !== 'string' || !lens.name.trim() || typeof lens.axis !== 'string' || typeof lens.value !== 'string'
      || !Array.isArray(lens.chats) || !lens.chats.every(id => typeof id === 'string')) throw new Error('Could not read the views for this place.');
    return lens as Lens;
  });
}
export function steerMessage(value: unknown): string {
  if (!object(value) || typeof value.understood !== 'boolean') throw new Error('Could not steer this place.');
  return value.understood ? '' : NOT_UNDERSTOOD;
}
export function alsoIn(row: { link?: boolean; places?: readonly { name: string; link?: boolean }[] }, inPlace?: string): string | undefined {
  if (!row.link) return undefined;
  const home = row.places?.find(place => !place.link && place.name.trim());
  return home ? `also in ${home.name}` : inPlace ? 'also in' : undefined;
}
