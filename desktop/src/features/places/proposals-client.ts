import { PlacesError, placesTransport, validatePlacesMutation, type Mutation, type PlacesTransport, type Receipt } from './client.ts';

/** Ledger offers are read-only until the person accepts or declines. Reading never asks a model. */
export type PlaceProposal = {
  id: string; kind: 'file' | 'move' | 'create' | 'merge'; status: 'pending';
  chatIds?: string[]; placeId?: string; fromPlaceId?: string; name?: string; parentId?: string;
  reason: string; offerVersion: string;
};
export type PlacesProposals = { proposals: PlaceProposal[]; organizing: boolean };
const object = (value: unknown): value is Record<string, unknown> => Boolean(value) && typeof value === 'object' && !Array.isArray(value);
function offers(value: unknown): PlacesProposals {
  if (!object(value) || !Array.isArray(value.proposals) || typeof value.organizing !== 'boolean') throw new PlacesError('The engine returned invalid place offers.');
  for (const p of value.proposals) {
    if (!object(p) || typeof p.id !== 'string' || !/^prop_[0-9a-f]{16}$/.test(p.id) || !['file', 'move', 'create', 'merge'].includes(String(p.kind))
      || p.status !== 'pending' || typeof p.reason !== 'string' || typeof p.offerVersion !== 'string'
      || (p.chatIds !== undefined && (!Array.isArray(p.chatIds) || !p.chatIds.every(id => typeof id === 'string')))
      || ['placeId', 'fromPlaceId', 'name', 'parentId'].some(key => p[key] !== undefined && typeof p[key] !== 'string')) throw new PlacesError('The engine returned an invalid place offer.');
  }
  return value as unknown as PlacesProposals;
}
export function createProposalsClient(transport: PlacesTransport = placesTransport) {
  const root = '/places/proposals';
  return {
    read: async (signal?: AbortSignal) => offers(await transport(root, { method: 'GET', signal })),
    decline: async (id: string) => offers(await transport(`${root}/${encodeURIComponent(id)}/decline`, { method: 'POST', body: {} })),
    accept: async (proposal: PlaceProposal): Promise<Mutation> => {
      const answer = await transport(`${root}/${encodeURIComponent(proposal.id)}/accept`, { method: 'POST', body: { offerVersion: proposal.offerVersion } });
      if (!object(answer) || !Array.isArray(answer.receipts)) throw new PlacesError('The engine returned an invalid place offer receipt.');
      offers(answer.view);
      if (answer.receipts.some(receipt => !object(receipt) || typeof receipt.id !== 'string')) throw new PlacesError('The engine returned an invalid receipt.');
      const receipts = answer.receipts as Receipt[];
      // The canonical accept returns several ordinary graph receipts, in order. Keep their exact IDs for shell Undo.
      return validatePlacesMutation({ revision: receipts[receipts.length - 1]?.afterRevision ?? 0, receipts, noop: receipts.length === 0, undo: receipts.map(receipt => receipt.id) });
    },
  };
}
export const proposalsClient = createProposalsClient();
