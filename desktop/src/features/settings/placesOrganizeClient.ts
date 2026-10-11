// The Settings client for one organising run. It speaks ARCHITECTURE §14 and
// holds no view state: a 409 means a run is already going, and the caller
// reads that run instead of starting a second one.

import { EngineError, engineJson } from '../chat/engine-client.ts';
import { oneReceiptId, parseOrganizeSnapshot, type OrganizeMode, type OrganizeSnapshot } from './placesOrganizeModel.ts';

export class OrganizeError extends Error {
  readonly status: number;
  readonly unreachable: boolean;
  constructor(message: string, status = 0, unreachable = false) {
    super(message);
    this.name = 'OrganizeError';
    this.status = status;
    this.unreachable = unreachable;
  }
}

export type OrganizeRequest = { method: 'GET' | 'POST'; body?: unknown; signal?: AbortSignal };
export type OrganizeTransport = (path: string, request: OrganizeRequest) => Promise<unknown>;

async function liveTransport(path: string, request: OrganizeRequest): Promise<unknown> {
  try {
    return await engineJson(path, {
      method: request.method,
      body: request.body === undefined ? undefined : JSON.stringify(request.body),
      signal: request.signal,
    });
  } catch (error) {
    if (error instanceof OrganizeError) throw error;
    if (error instanceof EngineError) throw new OrganizeError(error.message, error.status, error.unreachable);
    throw new OrganizeError(error instanceof Error ? error.message : 'The engine request failed.');
  }
}

export function createOrganizeClient(transport: OrganizeTransport = liveTransport) {
  const read = async (signal?: AbortSignal) => parseOrganizeSnapshot(await transport('/places/organize', { method: 'GET', signal }));
  const start = async (mode: OrganizeMode): Promise<OrganizeSnapshot> => {
    try {
      await transport('/places/organize', { method: 'POST', body: { mode } });
    } catch (error) {
      // A run already going answers 409. The status read is that run.
      if (!(error instanceof OrganizeError) || error.status !== 409) throw error;
    }
    return read();
  };
  return {
    read,
    start,
    cancel: async (): Promise<OrganizeSnapshot> => {
      await transport('/places/organize/cancel', { method: 'POST', body: {} });
      return read();
    },
    /** skip is the plan indexes the person unchecked. The reply must name one receipt. */
    accept: async (planId: string, skip: readonly number[]): Promise<string> => {
      const body = await transport(`/places/proposals/${encodeURIComponent(planId)}/accept`, { method: 'POST', body: { skip: [...skip] } });
      return oneReceiptId(body);
    },
    decline: async (planId: string): Promise<void> => {
      await transport(`/places/proposals/${encodeURIComponent(planId)}/decline`, { method: 'POST', body: {} });
    },
    /** Undo of the accept. One receipt, the one accept returned. */
    undo: async (receiptId: string): Promise<void> => {
      await transport('/places/undo', { method: 'POST', body: { receipts: [receiptId] } });
    },
  };
}

export const organizeClient = createOrganizeClient();
