/**
 * Typed client for the untouched-place suggestion (internal/desktopbridge/places_stale.go, design 6d "Too many places").
 *
 * It is its own module on purpose: it adds two routes beside the Places client and shares only its transport and its
 * error type. The engine decides which places are due (60 days untouched) and how long "Not now" hides one (30 days);
 * this client carries those numbers as the engine sent them and decides nothing of its own. A snooze is written by the
 * engine to a file every window reads, never to this window's storage.
 */

import { PlacesError, placesTransport, type PlacesTransport } from './client.ts';

/** One place the engine offers for merging or archiving. */
export type StalePlace = {
  id: string;
  name: string;
  /** ISO instant of the newest sign of life: opened, created, or spoken in inside it. */
  touchedAt: string;
  /** Whole days from `touchedAt` to the engine's clock. */
  daysUntouched: number;
};

/** The engine's answer: the places due, longest untouched first, and the two figures it applied. */
export type StaleSuggestions = { revision: number; readAt: string; afterDays: number; snoozeDays: number; places: StalePlace[] };
export type StaleSnoozed = { ok: true; placeId: string; until: string };

const isObject = (v: unknown): v is Record<string, unknown> => !!v && typeof v === 'object' && !Array.isArray(v);
const isInt = (v: unknown): v is number => typeof v === 'number' && Number.isSafeInteger(v) && v >= 0;
const bad = (what: string): never => { throw new PlacesError(`The engine returned an invalid ${what}.`); };

function suggestions(v: unknown): StaleSuggestions {
  if (!isObject(v) || !isInt(v.revision) || typeof v.readAt !== 'string' || !isInt(v.afterDays) || !isInt(v.snoozeDays) || !Array.isArray(v.places)) return bad('untouched-place list');
  for (const p of v.places) {
    if (!isObject(p) || typeof p.id !== 'string' || !p.id || typeof p.name !== 'string' || typeof p.touchedAt !== 'string' || !isInt(p.daysUntouched)) bad('untouched place');
  }
  return v as StaleSuggestions;
}

export type StaleClient = ReturnType<typeof createStaleClient>;

export function createStaleClient(transport: PlacesTransport = placesTransport) {
  return {
    /** The places due for a suggestion. A bridge without the routes answers 404, which callers treat as "no suggestions". */
    list: async (signal?: AbortSignal) => suggestions(await transport('/places/stale', { method: 'GET', signal })),
    /** "Not now": hide this place's suggestion for the engine's snooze period. Moves no revision and has no receipt, so it has no Undo. */
    snooze: async (placeId: string): Promise<StaleSnoozed> => {
      const v = await transport(`/places/${encodeURIComponent(placeId)}/stale-snooze`, { method: 'POST', body: {} });
      if (!isObject(v) || v.ok !== true || typeof v.placeId !== 'string' || typeof v.until !== 'string') return bad('snooze answer');
      return v as StaleSnoozed;
    },
  };
}
