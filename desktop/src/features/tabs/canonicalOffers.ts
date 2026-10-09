// The engine's tab-group offers. The body is canonical chat ids only: titles and
// paths stay on the engine, which reads them from each chat's own record.
import { fetchEngine } from '../chat/engine-client';
import { offerMinimum, type GroupOffer } from './offerRules.ts';

const canonicalChatID = /^[0-9a-f]{16}$/;

function readOffer(value: unknown): GroupOffer | undefined {
  if (!value || typeof value !== 'object') return undefined;
  const offer = value as { basis?: unknown; key?: unknown; ids?: unknown; title?: unknown };
  if ((offer.basis !== 'repo' && offer.basis !== 'topic') || typeof offer.key !== 'string' || offer.key === '') return undefined;
  if (!Array.isArray(offer.ids) || offer.ids.length < offerMinimum || !offer.ids.every(id => typeof id === 'string' && canonicalChatID.test(id))) return undefined;
  const title = typeof offer.title === 'string' ? offer.title.trim() : '';
  return { key: offer.key, basis: offer.basis, ids: offer.ids, ...(title ? { title } : {}) };
}

/** Asks the engine which of these open chats share a repository or a subject. A failure is no offer. */
export async function canonicalGroupOffers(ids: readonly string[]): Promise<GroupOffer[]> {
  const response = await fetchEngine('/history/group-offers', { method: 'POST', body: JSON.stringify({ ids }) });
  const body = await response.json() as { offers?: unknown };
  if (!Array.isArray(body.offers)) return [];
  return body.offers.flatMap(offer => { const read = readOffer(offer); return read ? [read] : []; });
}
