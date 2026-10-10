import { placesTransport, validatePlacesMutation, type PlacesTransport, type Mutation } from '../client.ts';
import { object, integer, strings, invalid } from '../../decisions/client.ts';
import type { KnowsLine as StoredLine } from './model.ts';

export type KnowsLine = StoredLine & { editedAt?: string; sourceWords?: string };
export type KnowsAnswer = { revision: number; lines: KnowsLine[]; stillTrue: string[] };
export type KnowsMutation = Mutation & { line?: KnowsLine; ask?: { a: KnowsLine; b: KnowsLine } };
export type KnowsWrite = { text: string; supersedes?: string; ifRevision?: number };
const enc = encodeURIComponent;

function line(v: unknown): KnowsLine {
  if (!object(v) || !['id', 'placeId', 'text'].every(k => typeof v[k] === 'string') || !object(v.source)
    || !['you-wrote', 'said-in-chat', 'learned', 'file'].includes(v.source.kind as string)) return invalid('knowledge line');
  for (const key of ['createdAt', 'editedAt', 'lastUsedAt', 'replacedBy', 'replacedAt', 'askedStillTrueAt', 'sourceWords']) {
    if (v[key] !== undefined && typeof v[key] !== 'string') return invalid('knowledge line');
  }
  for (const key of ['chatId', 'at', 'path']) if (v.source[key] !== undefined && typeof v.source[key] !== 'string') return invalid('knowledge source');
  if (v.source.answers !== undefined && !integer(v.source.answers)) return invalid('knowledge source');
  return v as KnowsLine;
}
function mutation(v: unknown): KnowsMutation {
  validatePlacesMutation(v);
  if (!object(v)) return invalid('knowledge receipt');
  if (v.line !== undefined) line(v.line);
  if (v.ask !== undefined) {
    if (!object(v.ask)) return invalid('knowledge conflict');
    line(v.ask.a); line(v.ask.b);
  }
  return v as KnowsMutation;
}

/** Generation guards travel unchanged; a conflict must be resolved by the caller. */
export function createKnowsClient(transport: PlacesTransport = placesTransport) {
  const path = (place: string, id?: string) => `/places/${enc(place)}/knows${id === undefined ? '' : `/${enc(id)}`}`;
  return {
    list: async (place: string, signal?: AbortSignal): Promise<KnowsAnswer> => {
      const v = await transport(path(place), { method: 'GET', signal });
      if (!object(v) || !integer(v.revision) || !Array.isArray(v.lines) || !strings(v.stillTrue)) return invalid('knowledge list');
      v.lines.forEach(line);
      return v as KnowsAnswer;
    },
    add: async (place: string, body: KnowsWrite, signal?: AbortSignal) => mutation(await transport(path(place), { method: 'POST', body, signal })),
    edit: async (place: string, id: string, body: KnowsWrite, signal?: AbortSignal) => mutation(await transport(path(place, id), { method: 'PATCH', body, signal })),
    remove: async (place: string, id: string, ifRevision?: number, signal?: AbortSignal) => mutation(await transport(path(place, id), { method: 'DELETE', body: { ifRevision }, signal })),
    confirm: async (place: string, id: string, ifRevision?: number, signal?: AbortSignal) => mutation(await transport(`${path(place, id)}/still-true`, { method: 'POST', body: { yes: true, ifRevision }, signal })),
  };
}
export type KnowsClient = ReturnType<typeof createKnowsClient>;
