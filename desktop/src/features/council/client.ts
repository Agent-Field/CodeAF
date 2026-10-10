import { placesTransport, type PlacesTransport } from '../places/client.ts';
import { object, integer, strings, invalid } from '../decisions/client.ts';

export type Council = { id: string; places: [string, string]; topic: string; chatId: string; sessionFile?: string; label: string; turns: number; cap: number; spend: number; capUSD: number; state: string; outcome?: string; openedAt: string; closedAt?: string };
function council(v: unknown): Council {
  if (!object(v) || !['id', 'topic', 'chatId', 'label', 'state', 'openedAt'].every(k => typeof v[k] === 'string')
    || !strings(v.places) || v.places.length !== 2 || !integer(v.turns) || !integer(v.cap)
    || ![v.spend, v.capUSD].every(n => typeof n === 'number' && Number.isFinite(n) && n >= 0)
    || !['sessionFile', 'outcome', 'closedAt'].every(k => v[k] === undefined || typeof v[k] === 'string')) return invalid('discussion');
  return v as Council;
}
/** One line of a discussion. `speaker` is the place's name, "person" for the person, absent for a line the journal wrote before it recorded who spoke. */
export type CouncilMessage = { speaker?: string; text: string; at?: string };
function message(v: unknown): CouncilMessage {
  if (!object(v) || typeof v.text !== 'string' || !['speaker', 'at'].every(k => v[k] === undefined || typeof v[k] === 'string')) return invalid('discussion message');
  return v as CouncilMessage;
}
/** Only the engine can list or steer discussions; this door runs no model. */
export function createCouncilClient(transport: PlacesTransport = placesTransport) {
  return {
    list: async (place?: string, signal?: AbortSignal): Promise<{ councils: Council[] }> => {
      const v = await transport(`/councils${place === undefined ? '' : `?place=${encodeURIComponent(place)}`}`, { method: 'GET', signal });
      if (!object(v) || !Array.isArray(v.councils)) return invalid('discussion list');
      v.councils.forEach(council);
      return v as { councils: Council[] };
    },
    steer: async (id: string, text: string, signal?: AbortSignal): Promise<Council> => council(await transport(`/councils/${encodeURIComponent(id)}/steer`, { method: 'POST', body: { text }, signal })),
    messages: async (id: string, signal?: AbortSignal): Promise<CouncilMessage[]> => {
      const v = await transport(`/councils/${encodeURIComponent(id)}/messages`, { method: 'GET', signal });
      if (!object(v) || !Array.isArray(v.messages)) return invalid('discussion messages');
      return v.messages.map(message);
    },
    /** Holds the discussion before its next turn: the person has started typing. */
    pause: async (id: string, signal?: AbortSignal): Promise<Council> => council(await transport(`/councils/${encodeURIComponent(id)}/pause`, { method: 'POST', body: {}, signal })),
    /** Lets a paused discussion go on: the person cleared what they were typing. */
    resume: async (id: string, signal?: AbortSignal): Promise<Council> => council(await transport(`/councils/${encodeURIComponent(id)}/resume`, { method: 'POST', body: {}, signal })),
  };
}
export type CouncilClient = ReturnType<typeof createCouncilClient>;
