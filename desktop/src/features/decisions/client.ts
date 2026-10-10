import { PlacesError, placesTransport, type PlacesTransport } from '../places/client.ts';

export { PlacesError as DecisionsError };
export type DecisionsTransport = PlacesTransport;
export const object = (v: unknown): v is Record<string, unknown> => !!v && typeof v === 'object' && !Array.isArray(v);
export const integer = (v: unknown): v is number => typeof v === 'number' && Number.isSafeInteger(v) && v >= 0;
export const invalid = (what: string): never => { throw new PlacesError(`The engine returned an invalid ${what}.`); };
export const strings = (v: unknown): v is string[] => Array.isArray(v) && v.every(item => typeof item === 'string');
const enc = encodeURIComponent;

export type PlanTarget = { chat?: string; task?: string; place?: string };
export type PlanStep = { kind: 'hold' | 'steer' | 'start' | 'stop' | 'ask-place' | 'remember'; target: PlanTarget; text: string };
export type PlanReceipt = { planId: string; results: { step: PlanStep; status: 'done' | 'skipped' | 'failed'; line: string; answer?: string; undo?: { kind: string; target: PlanTarget; token?: string } }[] };

function target(v: unknown): v is PlanTarget {
  return object(v) && ['chat', 'task', 'place'].every(k => v[k] === undefined || typeof v[k] === 'string');
}
function step(v: unknown): v is PlanStep {
  return object(v) && ['hold', 'steer', 'start', 'stop', 'ask-place', 'remember'].includes(v.kind as string) && target(v.target) && typeof v.text === 'string';
}
function receipt(v: unknown): PlanReceipt {
  if (!object(v) || typeof v.planId !== 'string' || !Array.isArray(v.results)) return invalid('plan receipt');
  for (const r of v.results) {
    if (!object(r) || !step(r.step) || !['done', 'skipped', 'failed'].includes(r.status as string) || typeof r.line !== 'string'
      || (r.answer !== undefined && typeof r.answer !== 'string')
      || (r.undo !== undefined && (!object(r.undo) || typeof r.undo.kind !== 'string' || !target(r.undo.target) || (r.undo.token !== undefined && typeof r.undo.token !== 'string')))) return invalid('plan receipt');
  }
  return v as PlanReceipt;
}

// Decision handlers are not installed yet. Keep their success payload unknown
// until Go owns a wire contract, rather than promising store-shaped responses.
export function createDecisionsClient(transport: DecisionsTransport = placesTransport) {
  return {
    list: (placeId: string, signal?: AbortSignal): Promise<unknown> => transport(`/places/${enc(placeId)}/decisions`, { method: 'GET', signal }),
    status: (placeId: string, signal?: AbortSignal): Promise<unknown> => transport(`/places/${enc(placeId)}/decide-status`, { method: 'GET', signal }),
    get: (id: string, signal?: AbortSignal): Promise<unknown> => transport(`/decisions/${enc(id)}`, { method: 'GET', signal }),
    overturn: (id: string, body: Readonly<Record<string, unknown>>, signal?: AbortSignal): Promise<unknown> => transport(`/decisions/${enc(id)}/overturn`, { method: 'POST', body, signal }),
    setDecide: (placeId: string, body: Readonly<Record<string, unknown>>, signal?: AbortSignal): Promise<unknown> => transport(`/places/${enc(placeId)}/decide`, { method: 'PUT', body, signal }),
  };
}

/** Each answer is one request. Refusals, including 409, retain their sentence and never retry. */
export function createPlanClient(transport: DecisionsTransport = placesTransport) {
  const path = (session: string, plan: string, verb: string) => `/sessions/${enc(session)}/plan/${enc(plan)}/${verb}`;
  return {
    go: async (session: string, plan: string, signal?: AbortSignal): Promise<PlanReceipt> => receipt(await transport(path(session, plan, 'go'), { method: 'POST', body: {}, signal })),
    edit: async (session: string, plan: string, steps: readonly PlanStep[], signal?: AbortSignal): Promise<{ accepted: true }> => {
      const v = await transport(path(session, plan, 'edit'), { method: 'POST', body: { steps }, signal });
      if (!object(v) || v.accepted !== true) return invalid('plan edit');
      return { accepted: true };
    },
    cancel: async (session: string, plan: string, signal?: AbortSignal): Promise<{ cancelled: true }> => {
      const v = await transport(path(session, plan, 'cancel'), { method: 'POST', body: {}, signal });
      if (!object(v) || v.cancelled !== true) return invalid('plan cancellation');
      return { cancelled: true };
    },
  };
}
export type DecisionsClient = ReturnType<typeof createDecisionsClient>;
export type PlanClient = ReturnType<typeof createPlanClient>;
