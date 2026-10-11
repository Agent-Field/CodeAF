/** Selecting a draft pill changes no store until a settled, uninterrupted answer exists. */
export function canFileChoice(snapshot: { running: boolean; entries: readonly { Role: string; Answer?: boolean; Interrupted?: boolean }[] } | undefined): boolean {
  return !!snapshot && !snapshot.running && snapshot.entries.some(entry => entry.Role === 'assistant' && entry.Answer && !entry.Interrupted);
}

/** A hand choice replaces the primary home while preserving shortcuts the organiser supplied. */
export function filingOptions(places: readonly { id: string; link?: boolean }[], selected: string): { addedBy: 'you'; moveFrom?: string } {
  const home = places.find(place => !place.link);
  return { addedBy: 'you', ...(home && home.id !== selected ? { moveFrom: home.id } : {}) };
}
